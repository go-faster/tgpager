package tgcall

import (
	"context"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/calls"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

const (
	defaultRingTimeout    = 45 * time.Second
	defaultConnectTimeout = 30 * time.Second
	defaultAttempts       = 3
	defaultRetryDelay     = 10 * time.Second
)

type Client struct {
	lg      *zap.Logger
	appID   int
	appHash string
	session string
	client  *telegram.Client
	calls   *calls.Client
	sender  *message.Sender
	api     *tg.Client
	peers   *peers.Manager

	peer        string
	peerUser    tg.InputUserClass
	peerStorage peers.Storage

	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	tracer         trace.Tracer
	metrics        *metrics

	ringTimeout    time.Duration
	connectTimeout time.Duration
	attempts       int
	retryDelay     time.Duration
}

type Option func(*Client)

func WithLogger(lg *zap.Logger) Option {
	return func(c *Client) { c.lg = lg }
}

// WithPeer sets the call target: a @username, phone number or t.me link,
// resolved once the client is connected.
func WithPeer(peer string) Option {
	return func(c *Client) { c.peer = peer }
}

// WithPeerStorage persists resolved access hashes, so a peer stays callable
// across restarts without re-resolving it.
func WithPeerStorage(st peers.Storage) Option {
	return func(c *Client) { c.peerStorage = st }
}

// WithRingTimeout bounds how long an unanswered call keeps ringing.
func WithRingTimeout(d time.Duration) Option {
	return func(c *Client) { c.ringTimeout = d }
}

// WithConnectTimeout bounds how long an accepted call may take to negotiate media.
func WithConnectTimeout(d time.Duration) Option {
	return func(c *Client) { c.connectTimeout = d }
}

// WithRetry sets how many times [Client.Ring] places a call before giving up,
// and how long it waits between attempts.
func WithRetry(attempts int, delay time.Duration) Option {
	return func(c *Client) {
		c.attempts = attempts
		c.retryDelay = delay
	}
}

func New(appID int, appHash, session string, opts ...Option) *Client {
	c := &Client{
		lg:             zap.NewNop(),
		appID:          appID,
		appHash:        appHash,
		session:        session,
		ringTimeout:    defaultRingTimeout,
		connectTimeout: defaultConnectTimeout,
		attempts:       defaultAttempts,
		retryDelay:     defaultRetryDelay,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.attempts < 1 {
		c.attempts = 1
	}
	if c.tracerProvider == nil {
		c.tracerProvider = noop.NewTracerProvider()
	}
	if c.meterProvider == nil {
		c.meterProvider = metricnoop.NewMeterProvider()
	}
	c.tracer = c.tracerProvider.Tracer(InstrumentationName)
	return c
}

func (c *Client) init() error {
	dispatcher := tg.NewUpdateDispatcher()
	m, err := newMetrics(c.meterProvider)
	if err != nil {
		return errors.Wrap(err, "create metrics")
	}
	c.metrics = m

	// gotd instruments its own MTProto invocations when given a provider.
	c.client = telegram.NewClient(c.appID, c.appHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{Path: c.session},
		Logger:         zapToGotdLog(c.lg),
		UpdateHandler:  dispatcher,
		TracerProvider: c.tracerProvider,
	})
	c.api = c.client.API()
	c.calls = calls.NewClient(c.api, calls.Options{
		Logger: zapToGotdLog(c.lg.Named("calls")),
	})
	c.calls.Register(dispatcher)
	c.sender = message.NewSender(c.api)
	c.peers = peers.Options{
		Storage: c.peerStorage,
		Logger:  zapToGotdLog(c.lg.Named("peers")),
	}.Build(c.api)
	return nil
}

func (c *Client) Run(ctx context.Context, f func(ctx context.Context) error) error {
	if err := c.init(); err != nil {
		return err
	}
	return c.client.Run(ctx, func(ctx context.Context) error {
		if err := c.authenticate(ctx); err != nil {
			return err
		}
		if err := c.resolvePeer(ctx); err != nil {
			return err
		}
		return f(ctx)
	})
}

// AuthFlow runs interactive authentication and exits, populating the session file.
func (c *Client) AuthFlow(ctx context.Context) error {
	if err := c.init(); err != nil {
		return err
	}
	return c.client.Run(ctx, c.authenticate)
}

func (c *Client) authenticate(ctx context.Context) error {
	flow := auth.NewFlow(terminalAuth{}, auth.SendCodeOptions{})
	if err := c.client.Auth().IfNecessary(ctx, flow); err != nil {
		return errors.Wrap(err, "authenticate")
	}
	return nil
}
