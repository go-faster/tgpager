package server

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// InstrumentationName identifies this package's telemetry.
const InstrumentationName = "github.com/go-faster/tgpager/internal/server"

type metrics struct {
	webhooks metric.Int64Counter
	queued   metric.Int64Counter
}

func newMetrics(mp metric.MeterProvider) (*metrics, error) {
	meter := mp.Meter(InstrumentationName)

	webhooks, err := meter.Int64Counter("tgpager.webhooks.received",
		metric.WithDescription("Alertmanager webhooks by result"),
	)
	if err != nil {
		return nil, err
	}
	queued, err := meter.Int64Counter("tgpager.pages.queued",
		metric.WithDescription("Pages accepted onto the call queue"),
	)
	if err != nil {
		return nil, err
	}
	return &metrics{webhooks: webhooks, queued: queued}, nil
}

func resultAttr(result string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("result", result))
}

// WithMeterProvider enables webhook metrics.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(s *Server) { s.meterProvider = mp }
}
