package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestServer_HandleAlertmanager_Firing(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	body := `{
		"version": "4",
		"groupKey": "{}:{alertname=\"Test\"}",
		"status": "firing",
		"alerts": [{"status": "firing", "labels": {}, "annotations": {}}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	select {
	case cr := <-srv.Queue():
		require.Equal(t, "{}:{alertname=\"Test\"}", cr.GroupKey)
		srv.Done(cr.GroupKey)
	default:
		t.Fatal("expected a call request to be enqueued")
	}
}

func TestServer_HandleAlertmanager_Resolved_Skipped(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	body := `{
		"version": "4",
		"groupKey": "{}:{alertname=\"Test\"}",
		"status": "resolved",
		"alerts": [{"status": "resolved", "labels": {}, "annotations": {}}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	select {
	case <-srv.Queue():
		t.Fatal("should not enqueue resolved alerts")
	default:
	}
}

func TestServer_HandleAlertmanager_Duplicate(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	body := `{
		"version": "4",
		"groupKey": "dup:key",
		"status": "firing",
		"alerts": [{"status": "firing", "labels": {}, "annotations": {}}]
	}`

	send := func() {
		req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code)
	}

	send()
	send()

	// First should be enqueued.
	select {
	case cr := <-srv.Queue():
		require.Equal(t, "dup:key", cr.GroupKey)
	default:
		t.Fatal("expected first call request")
	}

	// Second should be dropped as duplicate.
	select {
	case <-srv.Queue():
		t.Fatal("second should be dropped")
	default:
	}
}

func TestServer_HandleAlertmanager_InvalidJSON(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServer_HandleAlertmanager_MissingFields(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	body := `{"alerts": []}`
	req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServer_ConcurrentEnqueue(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(100, WithLogger(lg))

	body := `{
		"version": "4",
		"groupKey": "concurrent:key",
		"status": "firing",
		"alerts": [{"status": "firing", "labels": {}, "annotations": {}}]
	}`

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			require.Equal(t, http.StatusAccepted, rec.Code)
		}()
	}
	wg.Wait()

	select {
	case cr := <-srv.Queue():
		require.Equal(t, "concurrent:key", cr.GroupKey)
		srv.Done(cr.GroupKey)
	default:
		t.Fatal("expected at least one call request")
	}
}

func TestServer_DoneRemovesInflight(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	body := `{
		"version": "4",
		"groupKey": "done:key",
		"status": "firing",
		"alerts": [{"status": "firing", "labels": {}, "annotations": {}}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	select {
	case cr := <-srv.Queue():
		srv.Done(cr.GroupKey)
	default:
		t.Fatal("expected call request")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusAccepted, rec2.Code)

	select {
	case cr := <-srv.Queue():
		require.Equal(t, "done:key", cr.GroupKey)
		srv.Done(cr.GroupKey)
	default:
		t.Fatal("expected second call request after done")
	}
}

func TestServer_UnknownEndpoint(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServer_ReadBodyError(t *testing.T) {
	lg := zaptest.NewLogger(t)
	srv := New(10, WithLogger(lg))

	req := httptest.NewRequest(http.MethodPost, "/alertmanager", &errorReader{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

type errorReader struct{}

func (e *errorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
