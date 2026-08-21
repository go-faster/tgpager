package tts

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/go-faster/tgpager/internal/audio"
)

// TestEndToEndSpeakAndStream runs the real path: synthesize with a local
// binary, compose tone plus speech, and encode to Opus RTP with ffmpeg.
func TestEndToEndSpeakAndStream(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "espeak-ng"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed", bin)
		}
	}

	dir := t.TempDir()
	tone := filepath.Join(dir, "tone.ogg")
	out, err := exec.CommandContext(t.Context(), "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=0.3", "-c:a", "libopus", tone).CombinedOutput()
	require.NoError(t, err, "render tone: %s", out)

	synth, err := NewCommand(CommandOptions{
		Name:   "espeak-ng",
		Args:   []string{"-w", OutputPlaceholder, TextPlaceholder},
		Format: "wav",
	})
	require.NoError(t, err)

	cache, err := NewCache(filepath.Join(dir, "cache"))
	require.NoError(t, err)

	speaker, err := NewSpeaker(SpeakerOptions{
		Synthesizer: synth,
		Cache:       cache,
		Logger:      zaptest.NewLogger(t),
		Tone:        tone,
		Repeat:      2,
	})
	require.NoError(t, err)

	spec := speaker.Speak(t.Context(), testPayload())
	require.Len(t, spec.Segments, 2, "speech must have been synthesized")
	require.FileExists(t, spec.Segments[1])

	var pkts []rtp.Packet
	require.NoError(t, audio.NewFFmpeg().Stream(t.Context(), func(p *rtp.Packet) error {
		pkts = append(pkts, *p)
		return nil
	}, spec, audio.WithLogger(zaptest.NewLogger(t))))

	require.NotEmpty(t, pkts, "the call must carry audio")
	for i, p := range pkts {
		require.Equal(t, pkts[0].SSRC, p.SSRC, "packet %d", i)
		if i > 0 {
			require.Equal(t, pkts[i-1].SequenceNumber+1, p.SequenceNumber,
				"packet %d: tone and speech must be one continuous stream", i)
		}
	}
	t.Logf("streamed %d RTP packets of tone+speech, repeated twice", len(pkts))
}
