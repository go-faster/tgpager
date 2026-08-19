package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"

	"github.com/go-faster/tgpager/internal/alertmanager"
	"go.uber.org/zap"
)

type CallRequest struct {
	GroupKey string
}

// maxBodySize bounds an Alertmanager payload. The webhook is unauthenticated
// until a token is set, so the body is capped before it is buffered.
const maxBodySize = 1 << 20

type Server struct {
	lg       *zap.Logger
	token    string
	queue    chan CallRequest
	mu       sync.Mutex
	inflight map[string]struct{}
	handler  http.Handler
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
		lg.Warn("Rejected unauthorized webhook")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	payload, err := alertmanager.DecodeReader(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
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
		lg.Warn("Invalid alertmanager payload", zap.Error(err))
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if !payload.IsFiring() {
		lg.Debug("Skipping non-firing alert")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	req := CallRequest{
		GroupKey: payload.GroupKey,
	}
	if !s.enqueue(req) {
		lg.Info("Dropping duplicate/inflight call", zap.String("groupKey", req.GroupKey))
	}

	w.WriteHeader(http.StatusAccepted)
}
