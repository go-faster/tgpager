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

func TestOpenAIInstructionsDialect(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		want    string
	}{
		{
			"openai puts it top level", DialectOpenAI,
			`{"model":"m","input":"text","instructions":"Speak urgently."}`,
		},
		{
			"openrouter nests it", DialectOpenRouter,
			`{"model":"m","input":"text","provider":{"options":{"openai":{"instructions":"Speak urgently."}}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := NewOpenAI(OpenAIOptions{
				Model:        "m",
				Format:       "",
				Instructions: "Speak urgently.",
				Dialect:      tt.dialect,
			})
			require.NoError(t, err)
			o.opts.Format = "" // keep the fixture focused on instructions

			body, err := json.Marshal(o.buildRequest("text"))
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(body))
		})
	}
}

func TestOpenAISendsInstructionsOverTheWire(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		_, _ = w.Write([]byte("audio"))
	}))
	defer srv.Close()

	o, err := NewOpenAI(OpenAIOptions{
		BaseURL:      srv.URL,
		Model:        "m",
		Instructions: "Speak urgently and clearly.",
		Speed:        0.9,
		Dialect:      DialectOpenRouter,
	})
	require.NoError(t, err)

	_, err = o.Synthesize(t.Context(), "text")
	require.NoError(t, err)

	provider, ok := raw["provider"].(map[string]any)
	require.True(t, ok, "openrouter dialect must nest instructions")
	options := provider["options"].(map[string]any)
	openai := options["openai"].(map[string]any)
	require.Equal(t, "Speak urgently and clearly.", openai["instructions"])
	require.NotContains(t, raw, "instructions", "must not also send it top level")
	require.InDelta(t, 0.9, raw["speed"], 0.001)
}

func TestOpenAIValidatesSpeedAndDialect(t *testing.T) {
	_, err := NewOpenAI(OpenAIOptions{Model: "m", Speed: 9})
	require.Error(t, err, "speed out of range")

	_, err = NewOpenAI(OpenAIOptions{Model: "m", Speed: 0.1})
	require.Error(t, err)

	_, err = NewOpenAI(OpenAIOptions{Model: "m", Dialect: "azure"})
	require.Error(t, err, "an unknown dialect would silently drop instructions")

	_, err = NewOpenAI(OpenAIOptions{Model: "m", Speed: 1.5})
	require.NoError(t, err)
}

// TestOpenAIFingerprintCoversDelivery guards the cache: a changed instruction
// or speed must not serve the previous recording.
func TestOpenAIFingerprintCoversDelivery(t *testing.T) {
	base := OpenAIOptions{Model: "m", Voice: "alloy"}

	mk := func(mutate func(*OpenAIOptions)) string {
		opts := base
		mutate(&opts)
		o, err := NewOpenAI(opts)
		require.NoError(t, err)
		return o.Fingerprint()
	}

	original := mk(func(*OpenAIOptions) {})
	require.NotEqual(t, original, mk(func(o *OpenAIOptions) { o.Instructions = "Speak calmly." }))
	require.NotEqual(t, original, mk(func(o *OpenAIOptions) { o.Speed = 1.5 }))
	require.NotEqual(t, original, mk(func(o *OpenAIOptions) { o.Dialect = DialectOpenRouter }))
}
