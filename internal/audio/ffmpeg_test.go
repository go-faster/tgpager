package audio

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// sineFile renders a short tone to disk, so the test needs no binary fixture.
func sineFile(t *testing.T, seconds string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tone.ogg")
	cmd := exec.CommandContext(t.Context(), "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+seconds,
		"-c:a", "libopus", path,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "render tone: %s", out)
	return path
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
}

func TestFFmpegStreamer(t *testing.T) {
	requireFFmpeg(t)

	var (
		mu   sync.Mutex
		pkts []rtp.Packet
	)
	err := NewFFmpeg().Stream(t.Context(), func(p *rtp.Packet) error {
		mu.Lock()
		defer mu.Unlock()
		pkts = append(pkts, *p)
		return nil
	}, sineFile(t, "0.5"), WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)

	require.NotEmpty(t, pkts, "expected RTP packets")

	first := pkts[0]
	require.EqualValues(t, opusPayloadType, first.PayloadType)

	for i, p := range pkts {
		require.EqualValues(t, opusPayloadType, p.PayloadType, "packet %d", i)
		require.Equal(t, first.SSRC, p.SSRC, "packet %d: ssrc must be stable", i)
		require.NotEmpty(t, p.Payload, "packet %d: empty payload", i)

		if i == 0 {
			continue
		}
		require.Equal(t, pkts[i-1].SequenceNumber+1, p.SequenceNumber, "packet %d: sequence gap", i)
	}

	// One 20ms Opus frame per packet at 48kHz is 960 samples.
	const samplesPerFrame = sampleRate / 1000 * frameDurationMs
	require.Equal(t, uint32(samplesPerFrame), pkts[1].Timestamp-pkts[0].Timestamp)
}

func TestFFmpegStreamerWriteError(t *testing.T) {
	requireFFmpeg(t)

	boom := errStub("write failed")
	err := NewFFmpeg().Stream(t.Context(), func(*rtp.Packet) error {
		return boom
	}, sineFile(t, "0.5"), WithLogger(zaptest.NewLogger(t)))

	require.ErrorIs(t, err, boom)
}

func TestFFmpegStreamerMissingFile(t *testing.T) {
	requireFFmpeg(t)

	err := NewFFmpeg().Stream(t.Context(), func(*rtp.Packet) error {
		return nil
	}, filepath.Join(t.TempDir(), "nope.ogg"), WithLogger(zaptest.NewLogger(t)))

	require.Error(t, err)
}

func TestFFmpegStreamerCancelled(t *testing.T) {
	requireFFmpeg(t)

	ctx, cancel := context.WithCancel(t.Context())
	err := NewFFmpeg().Stream(ctx, func(*rtp.Packet) error {
		cancel()
		return nil
	}, sineFile(t, "5"), WithLogger(zaptest.NewLogger(t)))

	require.Error(t, err, "cancelling mid-stream must stop ffmpeg")
}

type errStub string

func (e errStub) Error() string { return string(e) }

func TestFFmpegStreamerMissingFileReportsReason(t *testing.T) {
	requireFFmpeg(t)

	err := NewFFmpeg().Stream(t.Context(), func(*rtp.Packet) error {
		return nil
	}, filepath.Join(t.TempDir(), "nope.ogg"), WithLogger(zaptest.NewLogger(t)))

	require.Error(t, err)
	require.Contains(t, err.Error(), "No such file or directory",
		"ffmpeg stderr must reach the error, not just the debug log")
}

func TestLastLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{"empty", "", 2, ""},
		{"blank only", "\n  \n", 2, ""},
		{"single", "boom", 2, "boom"},
		{"trailing newline", "a\nb\n", 2, "a; b"},
		{"truncates to last n", "a\nb\nc\nd", 2, "c; d"},
		{"skips blanks", "a\n\n\nb\n", 2, "a; b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, lastLines([]byte(tt.input), tt.n))
		})
	}
}
