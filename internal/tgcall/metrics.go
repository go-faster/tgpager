package tgcall

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationName identifies this package's telemetry.
const InstrumentationName = "github.com/go-faster/tgpager/internal/tgcall"

type metrics struct {
	attempts metric.Int64Counter
	duration metric.Float64Histogram
	voice    metric.Int64Counter
}

func newMetrics(mp metric.MeterProvider) (*metrics, error) {
	meter := mp.Meter(InstrumentationName)

	attempts, err := meter.Int64Counter("tgpager.call.attempts",
		metric.WithDescription("Telegram call attempts by outcome"),
	)
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("tgpager.call.duration",
		metric.WithDescription("Duration of a call attempt"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	voice, err := meter.Int64Counter("tgpager.voice.messages",
		metric.WithDescription("Voice messages sent by outcome"),
	)
	if err != nil {
		return nil, err
	}
	return &metrics{attempts: attempts, duration: duration, voice: voice}, nil
}

func outcome(err error) attribute.KeyValue {
	if err != nil {
		return attribute.String("outcome", "failure")
	}
	return attribute.String("outcome", "success")
}

// WithTracerProvider enables tracing of calls and of the underlying MTProto
// invocations, which gotd instruments itself.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *Client) { c.tracerProvider = tp }
}

// WithMeterProvider enables call metrics.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *Client) { c.meterProvider = mp }
}
