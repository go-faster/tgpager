package tts

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/go-faster/tgpager/internal/alertmanager"
)

type fakeSynth struct {
	audio  Audio
	err    error
	delay  time.Duration
	calls  int
	lastIn string
}

func (f *fakeSynth) Synthesize(ctx context.Context, text string) (Audio, error) {
	f.calls++
	f.lastIn = text
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return Audio{}, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return Audio{}, f.err
	}
	return f.audio, nil
}

func (f *fakeSynth) Fingerprint() string { return "fake" }
func (f *fakeSynth) Format() string      { return "mp3" }

func testPayload() alertmanager.WebhookPayload {
	return alertmanager.WebhookPayload{
		Status:            "firing",
		GroupKey:          "g",
		Version:           "4",
		CommonLabels:      map[string]string{"alertname": "CPUHigh", "severity": "critical"},
		CommonAnnotations: map[string]string{"summary": "CPU above 90 percent"},
	}
}

func newSpeaker(t *testing.T, synth Synthesizer) *Speaker {
	t.Helper()
	cache, err := NewCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	s, err := NewSpeaker(SpeakerOptions{
		Synthesizer: synth,
		Cache:       cache,
		Logger:      zaptest.NewLogger(t),
		Tone:        "tone.ogg",
		Repeat:      2,
	})
	require.NoError(t, err)
	return s
}

// TestSpeakFallsBackOnError is the property the whole package exists for: a
// dead provider must still produce a playable page.
func TestSpeakFallsBackOnError(t *testing.T) {
	s := newSpeaker(t, &fakeSynth{err: errors.New("provider is down")})

	spec := s.Speak(t.Context(), testPayload())

	require.Equal(t, []string{"tone.ogg"}, spec.Segments, "must still play the tone")
	require.Equal(t, 2, spec.Repeat)
	require.NoError(t, spec.Validate(), "the fallback must be playable")
}

// TestSpeakFallsBackOnHang covers the provider that never answers rather than
// the one that fails fast.
func TestSpeakFallsBackOnHang(t *testing.T) {
	s := newSpeaker(t, &fakeSynth{delay: time.Hour, audio: Audio{Data: []byte("x"), Format: "mp3"}})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	spec := s.Speak(ctx, testPayload())
	require.Equal(t, []string{"tone.ogg"}, spec.Segments)
	require.NoError(t, spec.Validate())
}

func TestSpeakComposesToneAndSpeech(t *testing.T) {
	synth := &fakeSynth{audio: Audio{Data: []byte("audio-bytes"), Format: "mp3"}}
	s := newSpeaker(t, synth)

	spec := s.Speak(t.Context(), testPayload())

	require.Len(t, spec.Segments, 2)
	require.Equal(t, "tone.ogg", spec.Segments[0], "tone comes first, to wake them")
	require.FileExists(t, spec.Segments[1])
	require.Equal(t, 2, spec.Repeat)
	require.Contains(t, synth.lastIn, "CPUHigh")
	require.Contains(t, synth.lastIn, "critical")
}

func TestSpeakUsesCache(t *testing.T) {
	synth := &fakeSynth{audio: Audio{Data: []byte("audio-bytes"), Format: "mp3"}}
	s := newSpeaker(t, synth)

	first := s.Speak(t.Context(), testPayload())
	second := s.Speak(t.Context(), testPayload())

	require.Equal(t, first.Segments, second.Segments)
	require.Equal(t, 1, synth.calls, "identical alerts must not be re-synthesized")
}

func TestSpeakDisabledPlaysToneOnly(t *testing.T) {
	s, err := NewSpeaker(SpeakerOptions{Tone: "tone.ogg", Repeat: 1})
	require.NoError(t, err)

	spec := s.Speak(t.Context(), testPayload())
	require.Equal(t, []string{"tone.ogg"}, spec.Segments)
}

func TestNewSpeakerRequiresToneAndCache(t *testing.T) {
	_, err := NewSpeaker(SpeakerOptions{})
	require.Error(t, err, "a tone is mandatory: it is the fallback")

	_, err = NewSpeaker(SpeakerOptions{Synthesizer: &fakeSynth{}, Tone: "t.ogg"})
	require.Error(t, err, "a provider without a cache would re-bill every resend")
}

func TestPreflightWarmsUp(t *testing.T) {
	// Fails the first call, as a lazily loading model server does, then answers.
	synth := &warmingSynth{failFor: 1, audio: Audio{Data: []byte("x"), Format: "mp3"}}
	s := newSpeaker(t, synth)

	require.NoError(t, s.Preflight(t.Context()), "must retry past the cold start")
	require.Equal(t, 2, synth.calls)
}

func TestPreflightGivesUp(t *testing.T) {
	synth := &warmingSynth{failFor: 99}
	s := newSpeaker(t, synth)

	err := s.Preflight(t.Context())
	require.Error(t, err)
	require.Equal(t, PreflightAttempts, synth.calls, "bounded, not a spin")
}

func TestPreflightDisabledIsNoop(t *testing.T) {
	s, err := NewSpeaker(SpeakerOptions{Tone: "tone.ogg"})
	require.NoError(t, err)
	require.NoError(t, s.Preflight(t.Context()), "nothing to check without a provider")
}

func TestPreflightStopsOnCancel(t *testing.T) {
	synth := &warmingSynth{failFor: 99}
	s := newSpeaker(t, synth)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.Error(t, s.Preflight(ctx))
	require.Less(t, synth.calls, PreflightAttempts, "a canceled context must not burn attempts")
}

// TestPreflightFailureStillPages is the guarantee that matters: a provider that
// never warmed up must not stop a page from happening.
func TestPreflightFailureStillPages(t *testing.T) {
	s := newSpeaker(t, &warmingSynth{failFor: 99})

	require.Error(t, s.Preflight(t.Context()))

	spec := s.Speak(t.Context(), testPayload())
	require.Equal(t, []string{"tone.ogg"}, spec.Segments)
	require.NoError(t, spec.Validate())
}

type warmingSynth struct {
	failFor int
	calls   int
	audio   Audio
}

func (w *warmingSynth) Synthesize(ctx context.Context, _ string) (Audio, error) {
	w.calls++
	if ctx.Err() != nil {
		return Audio{}, ctx.Err()
	}
	if w.calls <= w.failFor {
		return Audio{}, errors.New("model is still loading")
	}
	return w.audio, nil
}

func (w *warmingSynth) Fingerprint() string { return "warming" }
func (w *warmingSynth) Format() string      { return "mp3" }
