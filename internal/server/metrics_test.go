package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap/zaptest"
)

// counterByResult collects tgpager.webhooks.received into result -> value.
func counterByResult(t *testing.T, reader metric.Reader) map[string]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "tgpager.webhooks.received" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			for _, dp := range sum.DataPoints {
				result, ok := dp.Attributes.Value("result")
				require.True(t, ok, "missing result attribute")
				got[result.AsString()] += dp.Value
			}
		}
	}
	return got
}

func TestServerMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	srv := New(10,
		WithLogger(zaptest.NewLogger(t)),
		WithToken("tok"),
		WithMeterProvider(metric.NewMeterProvider(metric.WithReader(reader))),
	)

	post := func(auth, body string) {
		req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		srv.ServeHTTP(httptest.NewRecorder(), req)
	}

	const firing = `{"version":"4","groupKey":"g","status":"firing","alerts":[]}`

	post("Bearer tok", firing)                                                            // queued
	post("Bearer tok", firing)                                                            // duplicate, still in flight
	post("Bearer nope", firing)                                                           // unauthorized
	post("Bearer tok", `{not json`)                                                       // malformed
	post("Bearer tok", `{"version":"4","groupKey":"g2","status":"resolved","alerts":[]}`) // not firing
	post("Bearer tok", `{"version":"","groupKey":"","status":"","alerts":[]}`)            // invalid

	require.Equal(t, map[string]int64{
		"queued":       1,
		"duplicate":    1,
		"unauthorized": 1,
		"malformed":    1,
		"not_firing":   1,
		"invalid":      1,
	}, counterByResult(t, reader))
}
