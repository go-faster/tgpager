// Package tts synthesizes alert text into speech, with a static fallback so a
// page never depends on a third party being reachable.
package tts

import (
	"context"

	"github.com/go-faster/errors"
)

// Audio is synthesized speech in whatever format the provider returned.
// ffmpeg normalizes it later, so providers need not agree on a codec.
type Audio struct {
	Data   []byte
	Format string
}

func (a Audio) validate() error {
	switch {
	case len(a.Data) == 0:
		return errors.New("provider returned no audio")
	case a.Format == "":
		return errors.New("provider returned no format")
	}
	return nil
}

// Synthesizer turns text into speech.
type Synthesizer interface {
	Synthesize(ctx context.Context, text string) (Audio, error)
	// Fingerprint identifies the provider and its voice settings, so cached
	// audio from a different model or voice is never reused.
	Fingerprint() string
	// Format is the audio format Synthesize returns, known before the call so
	// the cache can be consulted first.
	Format() string
}

// Disabled is the no-op provider: pages play the static file only.
type Disabled struct{}

var _ Synthesizer = Disabled{}

// ErrDisabled reports that no provider is configured.
var ErrDisabled = errors.New("tts is disabled")

func (Disabled) Synthesize(context.Context, string) (Audio, error) {
	return Audio{}, ErrDisabled
}

func (Disabled) Fingerprint() string { return "disabled" }

func (Disabled) Format() string { return "" }
