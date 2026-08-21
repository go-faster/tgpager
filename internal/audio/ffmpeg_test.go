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
	}, File(sineFile(t, "0.5")), WithLogger(zaptest.NewLogger(t)))
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
	}, File(sineFile(t, "0.5")), WithLogger(zaptest.NewLogger(t)))

	require.ErrorIs(t, err, boom)
}

func TestFFmpegStreamerMissingFile(t *testing.T) {
	requireFFmpeg(t)

	err := NewFFmpeg().Stream(t.Context(), func(*rtp.Packet) error {
		return nil
	}, File(filepath.Join(t.TempDir(), "nope.ogg")), WithLogger(zaptest.NewLogger(t)))

	require.Error(t, err)
}

func TestFFmpegStreamerCancelled(t *testing.T) {
	requireFFmpeg(t)

	ctx, cancel := context.WithCancel(t.Context())
	err := NewFFmpeg().Stream(ctx, func(*rtp.Packet) error {
		cancel()
		return nil
	}, File(sineFile(t, "5")), WithLogger(zaptest.NewLogger(t)))

	require.Error(t, err, "canceling mid-stream must stop ffmpeg")
}

type errStub string

func (e errStub) Error() string { return string(e) }

func TestFFmpegStreamerMissingFileReportsReason(t *testing.T) {
	requireFFmpeg(t)

	err := NewFFmpeg().Stream(t.Context(), func(*rtp.Packet) error {
		return nil
	}, File(filepath.Join(t.TempDir(), "nope.ogg")), WithLogger(zaptest.NewLogger(t)))

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

func TestConcatFilter(t *testing.T) {
	tests := []struct {
		name           string
		inputs, repeat int
		want           string
	}{
		{"one file once", 1, 1, "[0:a]concat=n=1:v=0:a=1[out]"},
		{"one file thrice", 1, 3, "[0:a][0:a][0:a]concat=n=3:v=0:a=1[out]"},
		{"tone and speech", 2, 1, "[0:a][1:a]concat=n=2:v=0:a=1[out]"},
		{"tone and speech twice", 2, 2, "[0:a][1:a][0:a][1:a]concat=n=4:v=0:a=1[out]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, concatFilter(tt.inputs, tt.repeat))
		})
	}
}

func TestSpecValidate(t *testing.T) {
	require.NoError(t, File("a.ogg").Validate())
	require.Error(t, Spec{}.Validate(), "no segments")
	require.Error(t, Spec{Segments: []string{"a"}, Repeat: 0}.Validate(), "zero repeat")
	require.Error(t, Spec{Segments: []string{""}, Repeat: 1}.Validate(), "empty path")
}

// TestFFmpegStreamerComposes is the property the whole design rests on: tone
// and speech differ in codec and sample rate, yet arrive as one continuous RTP
// stream with no sequence discontinuity.
func TestFFmpegStreamerComposes(t *testing.T) {
	requireFFmpeg(t)

	tone := sineFile(t, "0.2")
	speech := mp3File(t, "0.3")

	var pkts []rtp.Packet
	var mu sync.Mutex
	err := NewFFmpeg().Stream(t.Context(), func(p *rtp.Packet) error {
		mu.Lock()
		defer mu.Unlock()
		pkts = append(pkts, *p)
		return nil
	}, Spec{Segments: []string{tone, speech}, Repeat: 2}, WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)

	require.NotEmpty(t, pkts)
	for i, p := range pkts {
		require.EqualValues(t, opusPayloadType, p.PayloadType, "packet %d", i)
		require.Equal(t, pkts[0].SSRC, p.SSRC, "packet %d: one call is one stream", i)
		if i > 0 {
			require.Equal(t, pkts[i-1].SequenceNumber+1, p.SequenceNumber,
				"packet %d: composing must not restart the sequence", i)
		}
	}

	// Two rounds of ~0.5s at 20ms per packet; allow codec padding either way.
	require.Greater(t, len(pkts), 40, "expected roughly a second of audio")
}

// mp3File renders a tone as MP3 at a different sample rate, the shape a TTS
// provider returns.
func mp3File(t *testing.T, seconds string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "speech.mp3")
	cmd := exec.CommandContext(t.Context(), "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=300:duration="+seconds,
		"-ar", "24000", "-c:a", "libmp3lame", path,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "render speech: %s", out)
	return path
}
