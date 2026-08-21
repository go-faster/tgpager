package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestWebhookToCallEndToEnd runs the real HTTP server and asserts an
// Alertmanager POST reaches the call queue, including the dedup behavior.
func TestWebhookToCallEndToEnd(t *testing.T) {
	srv := New(10, WithLogger(zaptest.NewLogger(t)), WithToken("tok"))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	post := func(body string) int {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/alertmanager", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	const firing = `{"version":"4","groupKey":"grp","status":"firing",
		"alerts":[{"status":"firing","labels":{"alertname":"CPU","severity":"critical"},
		"annotations":{"summary":"hot"},"startsAt":"2024-01-01T00:00:00Z"}]}`

	require.Equal(t, http.StatusAccepted, post(firing))

	select {
	case req := <-srv.Queue():
		require.Equal(t, "grp", req.GroupKey)
		// A resend while the page is in flight must not queue a second call.
		require.Equal(t, http.StatusAccepted, post(firing))
		select {
		case <-srv.Queue():
			t.Fatal("in-flight group must not enqueue twice")
		case <-time.After(50 * time.Millisecond):
		}
		srv.Done(req.GroupKey)
	case <-time.After(time.Second):
		t.Fatal("firing alert never reached the call queue")
	}

	// Resolved alerts must not page at all.
	require.Equal(t, http.StatusAccepted, post(`{"version":"4","groupKey":"g2","status":"resolved","alerts":[]}`))
	select {
	case <-srv.Queue():
		t.Fatal("resolved alert must not page")
	case <-time.After(50 * time.Millisecond):
	}
}
