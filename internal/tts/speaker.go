package tts

import (
	"context"
	"time"

	"github.com/go-faster/errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"

	"github.com/go-faster/tgpager/internal/alertmanager"
	"github.com/go-faster/tgpager/internal/audio"
)

// InstrumentationName identifies this package's telemetry.
const InstrumentationName = "github.com/go-faster/tgpager/internal/tts"

// Speaker turns an alert into what the call plays.
//
// It is the piece that guarantees a page happens: every failure below resolves
// to the static file rather than to an error.
type Speaker struct {
	synth    Synthesizer
	tmpl     *Template
	cache    *Cache
	lg       *zap.Logger
	tracer   trace.Tracer
	requests metric.Int64Counter
	fallback metric.Int64Counter
	duration metric.Float64Histogram

	tone   string
	repeat int
}

type SpeakerOptions struct {
	Synthesizer    Synthesizer
	Template       *Template
	Cache          *Cache
	Logger         *zap.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider

	// Tone plays before the speech and is what a page falls back to.
	Tone   string
	Repeat int
}

func NewSpeaker(opts SpeakerOptions) (*Speaker, error) {
	if opts.Synthesizer == nil {
		opts.Synthesizer = Disabled{}
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}
	if opts.TracerProvider == nil {
		opts.TracerProvider = noop.NewTracerProvider()
	}
	if opts.MeterProvider == nil {
		opts.MeterProvider = metricnoop.NewMeterProvider()
	}
	if opts.Repeat < 1 {
		opts.Repeat = 1
	}
	if _, disabled := opts.Synthesizer.(Disabled); !disabled && opts.Cache == nil {
		return nil, errors.New("cache is required when a provider is configured")
	}
	if opts.Tone == "" {
		return nil, errors.New("tone is required: it is what a failed synthesis falls back to")
	}
	if opts.Template == nil {
		tmpl, err := NewTemplate("")
		if err != nil {
			return nil, err
		}
		opts.Template = tmpl
	}

	meter := opts.MeterProvider.Meter(InstrumentationName)
	requests, err := meter.Int64Counter("tgpager.tts.requests",
		metric.WithDescription("Speech synthesis attempts by result"))
	if err != nil {
		return nil, err
	}
	fallback, err := meter.Int64Counter("tgpager.tts.fallbacks",
		metric.WithDescription("Pages that played the static file because synthesis failed"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("tgpager.tts.duration",
		metric.WithDescription("Speech synthesis duration"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}

	return &Speaker{
		synth:    opts.Synthesizer,
		tmpl:     opts.Template,
		cache:    opts.Cache,
		lg:       opts.Logger,
		tracer:   opts.TracerProvider.Tracer(InstrumentationName),
		requests: requests,
		fallback: fallback,
		duration: duration,
		tone:     opts.Tone,
		repeat:   opts.Repeat,
	}, nil
}

// Speak returns what the call should play for this alert: the tone followed by
// synthesized speech, repeated. If anything at all goes wrong it returns the
// tone alone, because a degraded page beats no page.
func (s *Speaker) Speak(ctx context.Context, payload alertmanager.WebhookPayload) audio.Spec {
	fallback := audio.Spec{Segments: []string{s.tone}, Repeat: s.repeat}

	if _, disabled := s.synth.(Disabled); disabled {
		return fallback
	}

	speech, err := s.speech(ctx, payload)
	if err != nil {
		// Deliberately not fatal: the page still happens.
		s.fallback.Add(ctx, 1)
		s.lg.Warn("Speech synthesis failed, paging with the static file", zap.Error(err))
		return fallback
	}

	return audio.Spec{Segments: []string{s.tone, speech}, Repeat: s.repeat}
}

func (s *Speaker) speech(ctx context.Context, payload alertmanager.WebhookPayload) (path string, err error) {
	ctx, span := s.tracer.Start(ctx, "tts.Speak")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// The rendered text never reaches a span or a log: alert labels routinely
	// carry hostnames and customer identifiers.
	text, err := s.tmpl.Render(payload)
	if err != nil {
		s.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "template_error")))
		return "", err
	}
	span.SetAttributes(attribute.Int("tts.text_bytes", len(text)))

	fingerprint := s.synth.Fingerprint()
	if hit, ok := s.cache.Lookup(fingerprint, text, s.synth.Format()); ok {
		s.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "hit")))
		span.SetAttributes(attribute.Bool("tts.cached", true))
		return hit, nil
	}

	start := time.Now()
	out, err := s.synth.Synthesize(ctx, text)
	s.duration.Record(ctx, time.Since(start).Seconds())
	if err != nil {
		s.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "error")))
		return "", err
	}
	span.SetAttributes(
		attribute.Bool("tts.cached", false),
		attribute.Int("tts.audio_bytes", len(out.Data)),
	)

	path, err = s.cache.Store(fingerprint, text, out)
	if err != nil {
		s.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "error")))
		return "", err
	}
	s.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "miss")))
	return path, nil
}
