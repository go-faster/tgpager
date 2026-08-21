package tts

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap/zaptest"
)

func recordSpans(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	return rec, sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
}

func spanNames(rec *tracetest.SpanRecorder) []string {
	var out []string
	for _, s := range rec.Ended() {
		out = append(out, s.Name())
	}
	return out
}

func TestOpenAITracing(t *testing.T) {
	rec, tp := recordSpans(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fake-audio-bytes"))
	}))
	defer srv.Close()

	o, err := NewOpenAI(OpenAIOptions{
		BaseURL: srv.URL, Model: "m", Voice: "alloy", TracerProvider: tp,
	})
	require.NoError(t, err)

	_, err = o.Synthesize(t.Context(), "some alert text")
	require.NoError(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "tts.openai.Synthesize", spans[0].Name())

	attrs := map[string]any{}
	for _, a := range spans[0].Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	require.Equal(t, "openai", attrs["tts.provider"])
	require.Equal(t, "m", attrs["tts.model"])
	require.EqualValues(t, 200, attrs["http.response.status_code"])
	require.EqualValues(t, len("fake-audio-bytes"), attrs["tts.audio_bytes"])
	require.EqualValues(t, len("some alert text"), attrs["tts.text_bytes"])
	require.NotContains(t, attrs, "tts.text", "the text itself must never be on a span")
}

func TestOpenAITracingRecordsFailure(t *testing.T) {
	rec, tp := recordSpans(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	o, err := NewOpenAI(OpenAIOptions{BaseURL: srv.URL, Model: "m", TracerProvider: tp})
	require.NoError(t, err)

	_, err = o.Synthesize(t.Context(), "text")
	require.Error(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 1)
	require.NotEmpty(t, spans[0].Events(), "the error must be recorded")

	attrs := map[string]any{}
	for _, a := range spans[0].Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	require.EqualValues(t, 429, attrs["http.response.status_code"], "the status must be visible")
}

func TestCommandTracing(t *testing.T) {
	rec, tp := recordSpans(t)

	opts := helperOptions(t, "stdout")
	opts.TracerProvider = tp

	c, err := NewCommand(opts)
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "spoken")
	require.NoError(t, err)

	spans := rec.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "tts.command.Synthesize", spans[0].Name())
}

// TestSpeakerTracingNestsProvider is the shape that makes a trace readable:
// the provider call is a child of the page's speech span.
func TestSpeakerTracingNestsProvider(t *testing.T) {
	rec, tp := recordSpans(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("audio"))
	}))
	defer srv.Close()

	synth, err := NewOpenAI(OpenAIOptions{BaseURL: srv.URL, Model: "m", TracerProvider: tp})
	require.NoError(t, err)

	cache, err := NewCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	s, err := NewSpeaker(SpeakerOptions{
		Synthesizer:    synth,
		Cache:          cache,
		Logger:         zaptest.NewLogger(t),
		TracerProvider: tp,
		Tone:           "tone.ogg",
		Repeat:         1,
	})
	require.NoError(t, err)

	s.Speak(t.Context(), testPayload())

	require.ElementsMatch(t, []string{"tts.openai.Synthesize", "tts.Speak"}, spanNames(rec))

	var parent, child sdktrace.ReadOnlySpan
	for _, sp := range rec.Ended() {
		if sp.Name() == "tts.Speak" {
			parent = sp
		} else {
			child = sp
		}
	}
	require.Equal(t, parent.SpanContext().SpanID(), child.Parent().SpanID(),
		"the provider span must hang off the speech span")
}
