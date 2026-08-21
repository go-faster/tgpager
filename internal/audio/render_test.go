package audio

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestFFmpegRender(t *testing.T) {
	requireFFmpeg(t)

	out := filepath.Join(t.TempDir(), "voice.ogg")
	err := NewFFmpeg().Render(t.Context(), File(sineFile(t, "1")), out, WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)

	info, err := os.Stat(out)
	require.NoError(t, err)
	require.NotZero(t, info.Size())

	dur, err := OggDuration(out)
	require.NoError(t, err)
	require.InDelta(t, time.Second.Seconds(), dur.Seconds(), 0.05)
}

// TestFFmpegRenderComposes is the property that makes rendering safe to reuse:
// the same spec that streams into a call renders to a file, across codecs.
func TestFFmpegRenderComposes(t *testing.T) {
	requireFFmpeg(t)

	out := filepath.Join(t.TempDir(), "voice.ogg")
	spec := Spec{Segments: []string{sineFile(t, "0.5"), mp3File(t, "0.5")}, Repeat: 2}
	require.NoError(t, NewFFmpeg().Render(t.Context(), spec, out, WithLogger(zaptest.NewLogger(t))))

	dur, err := OggDuration(out)
	require.NoError(t, err)
	require.InDelta(t, 2*time.Second.Seconds(), dur.Seconds(), 0.2)
}

// TestOggDurationMatchesFFprobe pins the granule arithmetic to the reference
// implementation, rather than to my reading of the specification.
func TestOggDurationMatchesFFprobe(t *testing.T) {
	requireFFmpeg(t)
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}

	out := filepath.Join(t.TempDir(), "voice.ogg")
	spec := Spec{Segments: []string{sineFile(t, "0.7")}, Repeat: 3}
	require.NoError(t, NewFFmpeg().Render(t.Context(), spec, out))

	dur, err := OggDuration(out)
	require.NoError(t, err)

	// ffprobe reports the container duration, which still includes the
	// pre-skip that OggDuration subtracts: a handful of milliseconds.
	require.InDelta(t, ffprobeSeconds(t, out), dur.Seconds(), 0.02)
}

func ffprobeSeconds(t *testing.T, path string) float64 {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "ffprobe", "-hide_banner", "-v", "error",
		"-show_entries", "format=duration", "-of", "json", path)
	out, err := cmd.Output()
	require.NoError(t, err)

	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	require.NoError(t, json.Unmarshal(out, &probe))
	seconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
	require.NoError(t, err)
	return seconds
}

func TestFFmpegRenderRejectsBadSpec(t *testing.T) {
	require.Error(t, NewFFmpeg().Render(t.Context(), Spec{}, "out.ogg"))
	require.Error(t, NewFFmpeg().Render(t.Context(), File("in.ogg"), ""))
}

func TestFFmpegRenderMissingInput(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	err := NewFFmpeg().Render(t.Context(), File(filepath.Join(dir, "nope.ogg")), filepath.Join(dir, "out.ogg"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "No such file or directory")
}
