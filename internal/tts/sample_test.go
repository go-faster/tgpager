package tts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/go-faster/tgpager/internal/alertmanager"
)

// TestGenerateSample writes a playable file of exactly what a callee hears.
// Run with: go test ./internal/tts -run Sample -sample-dir <dir>
func TestGenerateSample(t *testing.T) {
	dir := os.Getenv("SAMPLE_DIR")
	if dir == "" {
		t.Skip("set SAMPLE_DIR to generate samples")
	}
	for _, bin := range []string{"ffmpeg", "espeak-ng"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed", bin)
		}
	}

	tone := filepath.Join(dir, "tone.ogg")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=0.35",
		"-af", "afade=t=out:st=0.25:d=0.1", "-c:a", "libopus", tone)

	payload := alertmanager.WebhookPayload{
		Status:            "firing",
		Version:           "4",
		GroupKey:          "g",
		CommonLabels:      map[string]string{"alertname": "PostgresReplicationLag", "severity": "critical"},
		CommonAnnotations: map[string]string{"summary": "replica is 900 seconds behind primary"},
	}

	variants := []struct {
		name  string
		speed string
		voice string
	}{
		{"default", "175", "en"},
		{"slower", "140", "en"},
		{"slower-gb", "140", "en-gb"},
		{"urgent", "200", "en"},
	}

	for i, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			synth, err := NewCommand(CommandOptions{
				Name: "espeak-ng",
				Args: []string{
					"-v", v.voice, "-s", v.speed,
					"-w", OutputPlaceholder, TextPlaceholder,
				},
				Format: "wav",
			})
			require.NoError(t, err)

			cache, err := NewCache(filepath.Join(t.TempDir(), "cache"))
			require.NoError(t, err)

			speaker, err := NewSpeaker(SpeakerOptions{
				Synthesizer: synth,
				Cache:       cache,
				Logger:      zaptest.NewLogger(t),
				Tone:        tone,
				Repeat:      2,
			})
			require.NoError(t, err)

			tmpl, err := NewTemplate("")
			require.NoError(t, err)
			text, err := tmpl.Render(payload)
			require.NoError(t, err)
			t.Logf("spoken text: %q", text)

			spec := speaker.Speak(t.Context(), payload)
			require.Len(t, spec.Segments, 2, "speech must have been synthesized")

			// Same concat the call uses, written to a file instead of RTP.
			out := filepath.Join(dir, "sample-"+strconv.Itoa(i+1)+"-"+v.name+".ogg")
			args := []string{"-hide_banner", "-loglevel", "error", "-y"}
			for _, seg := range spec.Segments {
				args = append(args, "-i", seg)
			}
			args = append(args,
				"-filter_complex", concatFilterFor(len(spec.Segments), spec.Repeat),
				"-map", "[out]", "-c:a", "libopus", "-ar", "48000", "-ac", "1", out)
			run(t, "ffmpeg", args...)
			t.Logf("wrote %s", out)
		})
	}
}

func concatFilterFor(inputs, repeat int) string {
	var refs string
	n := 0
	for range repeat {
		for i := range inputs {
			refs += "[" + strconv.Itoa(i) + ":a]"
			n++
		}
	}
	return refs + "concat=n=" + strconv.Itoa(n) + ":v=0:a=1[out]"
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput()
	require.NoError(t, err, "%s: %s", name, out)
}
