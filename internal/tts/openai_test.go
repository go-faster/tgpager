package tts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAISynthesize(t *testing.T) {
	var got speechRequest
	var auth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/audio/speech", r.URL.Path)
		auth = r.Header.Get("Authorization")
		require.NoError(t, decodeJSON(r, &got))
		_, _ = w.Write([]byte("fake-mp3-bytes"))
	}))
	defer srv.Close()

	o, err := NewOpenAI(OpenAIOptions{
		BaseURL: srv.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "openai/gpt-4o-mini-tts",
		Voice:   "alloy",
	})
	require.NoError(t, err)

	audio, err := o.Synthesize(t.Context(), "disk is full")
	require.NoError(t, err)

	require.Equal(t, []byte("fake-mp3-bytes"), audio.Data)
	require.Equal(t, "mp3", audio.Format)
	require.Equal(t, "Bearer sk-test", auth)
	require.Equal(t, "disk is full", got.Input)
	require.Equal(t, "openai/gpt-4o-mini-tts", got.Model)
	require.Equal(t, "alloy", got.Voice)
	require.Equal(t, "mp3", got.ResponseFormat)
}

func TestOpenAIErrorCarriesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
	}))
	defer srv.Close()

	o, err := NewOpenAI(OpenAIOptions{BaseURL: srv.URL, Model: "m"})
	require.NoError(t, err)

	_, err = o.Synthesize(t.Context(), "text")
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded", "the reason must survive")
	require.Contains(t, err.Error(), "429")
}

func TestOpenAITimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	o, err := NewOpenAI(OpenAIOptions{BaseURL: srv.URL, Model: "m", Timeout: 30 * time.Millisecond})
	require.NoError(t, err)

	_, err = o.Synthesize(t.Context(), "text")
	require.Error(t, err, "a hanging provider must not hang the page")
}

func TestOpenAIEmptyBodyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	o, err := NewOpenAI(OpenAIOptions{BaseURL: srv.URL, Model: "m"})
	require.NoError(t, err)

	_, err = o.Synthesize(t.Context(), "text")
	require.Error(t, err, "silent audio would be a silent page")
}

func TestNewOpenAIDefaults(t *testing.T) {
	_, err := NewOpenAI(OpenAIOptions{})
	require.Error(t, err, "model is required")

	o, err := NewOpenAI(OpenAIOptions{Model: "m"})
	require.NoError(t, err)
	require.Equal(t, defaultOpenAIBaseURL, o.opts.BaseURL)
	require.Equal(t, "mp3", o.Format())

	o, err = NewOpenAI(OpenAIOptions{Model: "m", BaseURL: "https://openrouter.ai/api/v1/"})
	require.NoError(t, err)
	require.Equal(t, "https://openrouter.ai/api/v1", o.opts.BaseURL, "trailing slash must not double up")
}

func TestOpenAIFingerprintSeparatesVoices(t *testing.T) {
	a, err := NewOpenAI(OpenAIOptions{Model: "m", Voice: "alloy"})
	require.NoError(t, err)
	b, err := NewOpenAI(OpenAIOptions{Model: "m", Voice: "echo"})
	require.NoError(t, err)

	require.NotEqual(t, a.Fingerprint(), b.Fingerprint(),
		"a voice change must not serve cached audio in the old voice")
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
