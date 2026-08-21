// Package server serves the Alertmanager webhook.
package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap"

	"github.com/go-faster/tgpager/internal/alertmanager"
)

type CallRequest struct {
	GroupKey string
	// Payload is carried through so the call can say what fired.
	Payload alertmanager.WebhookPayload
}

// maxBodySize bounds an Alertmanager payload. The webhook is unauthenticated
// until a token is set, so the body is capped before it is buffered.
const maxBodySize = 1 << 20

type Server struct {
	lg            *zap.Logger
	token         string
	meterProvider metric.MeterProvider
	metrics       *metrics
	queue         chan CallRequest
	mu            sync.Mutex
	inflight      map[string]struct{}
	handler       http.Handler
}

type Option func(*Server)

func WithLogger(lg *zap.Logger) Option {
	return func(s *Server) {
		s.lg = lg
	}
}

// WithToken requires callers to present the token as a bearer credential.
func WithToken(token string) Option {
	return func(s *Server) {
		s.token = token
	}
}

func New(queueSize int, opts ...Option) *Server {
	s := &Server{
		lg:       zap.NewNop(),
		queue:    make(chan CallRequest, queueSize),
		inflight: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.token == "" {
		s.lg.Warn("Webhook is unauthenticated, anyone who can reach it can place calls")
	}
	if s.meterProvider == nil {
		s.meterProvider = metricnoop.NewMeterProvider()
	}
	// Counters only fail on a bad instrument name, which is a programming
	// error rather than anything a running server can recover from.
	m, err := newMetrics(s.meterProvider)
	if err != nil {
		panic(err)
	}
	s.metrics = m
	mux := http.NewServeMux()
	mux.HandleFunc("POST /alertmanager", s.handleAlertmanager)
	s.handler = mux
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) Queue() <-chan CallRequest {
	return s.queue
}

func (s *Server) Done(groupKey string) {
	s.mu.Lock()
	delete(s.inflight, groupKey)
	s.mu.Unlock()
}

// authorized reports whether r carries the configured bearer token. With no
// token configured every request is allowed.
func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func (s *Server) enqueue(req CallRequest) bool {
	s.mu.Lock()
	_, ok := s.inflight[req.GroupKey]
	if ok {
		s.mu.Unlock()
		return false
	}
	s.inflight[req.GroupKey] = struct{}{}
	s.mu.Unlock()

	select {
	case s.queue <- req:
		return true
	default:
		s.Done(req.GroupKey)
		return false
	}
}

func (s *Server) handleAlertmanager(w http.ResponseWriter, r *http.Request) {
	lg := s.lg.With(zap.String("remote", r.RemoteAddr))

	if !s.authorized(r) {
		s.metrics.webhooks.Add(r.Context(), 1, resultAttr("unauthorized"))
		lg.Warn("Rejected unauthorized webhook")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	payload, err := alertmanager.DecodeReader(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		s.metrics.webhooks.Add(r.Context(), 1, resultAttr("malformed"))
		lg.Warn("Failed to decode alertmanager payload", zap.Error(err))
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	lg.Info("Received alertmanager webhook",
		zap.String("groupKey", payload.GroupKey),
		zap.String("status", payload.Status),
		zap.Int("alerts", len(payload.Alerts)),
	)

	if err := payload.Validate(); err != nil {
		s.metrics.webhooks.Add(r.Context(), 1, resultAttr("invalid"))
		lg.Warn("Invalid alertmanager payload", zap.Error(err))
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if !payload.IsFiring() {
		s.metrics.webhooks.Add(r.Context(), 1, resultAttr("not_firing"))
		lg.Debug("Skipping non-firing alert")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	req := CallRequest{
		GroupKey: payload.GroupKey,
		Payload:  payload,
	}
	if !s.enqueue(req) {
		s.metrics.webhooks.Add(r.Context(), 1, resultAttr("duplicate"))
		lg.Info("Dropping duplicate/inflight call", zap.String("groupKey", req.GroupKey))
	} else {
		s.metrics.webhooks.Add(r.Context(), 1, resultAttr("queued"))
		s.metrics.queued.Add(r.Context(), 1)
	}

	w.WriteHeader(http.StatusAccepted)
}
