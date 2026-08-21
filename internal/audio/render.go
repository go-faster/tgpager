package audio

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"go.uber.org/zap"

	"github.com/go-faster/errors"
)

// voiceBitrate keeps a rendered voice message small, for the phone network a
// woken-up callee is actually on. Speech at 32kbps mono Opus is transparent.
const voiceBitrate = "32k"

// Renderer encodes a [Spec] into a file rather than into a call.
type Renderer interface {
	Render(ctx context.Context, spec Spec, path string, opts ...StreamOption) error
}

// Render encodes spec into a single Ogg/Opus file, the only container Telegram
// accepts for a voice message.
//
// It composes exactly as [FFmpegStreamer.Stream] does, so a spec that plays
// correctly in a call renders correctly to a file.
func (f *FFmpegStreamer) Render(ctx context.Context, spec Spec, path string, opts ...StreamOption) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if path == "" {
		return errors.New("no output path")
	}
	lg := newStreamOptions(opts).logger

	args := append([]string{"-hide_banner", "-y"}, composeArgs(spec)...)
	args = append(args,
		"-vn",
		"-c:a", "libopus",
		// Opus has a mode tuned for speech, and every segment here is speech
		// or a tone.
		"-application", "voip",
		"-b:a", voiceBitrate,
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", "1",
		"-f", "ogg",
		path,
	)

	lg.Debug("Rendering audio", zap.Strings("args", args))

	// #nosec G204 -- ffmpeg path and the input files are operator-supplied config.
	cmd := exec.CommandContext(ctx, f.ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	return finishFFmpeg(cmd.Run(), stderr.Bytes(), lg)
}
