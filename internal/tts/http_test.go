package tts

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCommandDrivesHTTPServer shows a self-hosted model server — Style-Bert-VITS2,
// GPT-SoVITS, a Piper HTTP wrapper — reached through the command provider, with
// no tgpager code specific to any of them.
func TestCommandDrivesHTTPServer(t *testing.T) {
	for _, bin := range []string{"sh", "curl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed", bin)
		}
	}

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("text")
		w.Header().Set("Content-Type", "audio/wav")
		// A minimal RIFF header is enough: the provider must not care what the
		// model is, only that bytes came back.
		_, _ = w.Write(append([]byte("RIFF\x24\x00\x00\x00WAVE"), make([]byte, 2048)...))
	}))
	defer srv.Close()

	c, err := NewCommand(CommandOptions{
		Name: "sh",
		Args: []string{
			"-c",
			`exec curl -sf --get --data-urlencode "text=$1" "$2/voice" -o "$3"`,
			"sh", TextPlaceholder, srv.URL, OutputPlaceholder,
		},
		Format: "wav",
	})
	require.NoError(t, err)

	audio, err := c.Synthesize(t.Context(), "Alert. Postgres replication lag.")
	require.NoError(t, err)

	require.Equal(t, "Alert. Postgres replication lag.", got, "text must reach the model")
	require.Equal(t, "wav", audio.Format)
	require.Equal(t, []byte("RIFF"), audio.Data[:4])
	require.Greater(t, len(audio.Data), 1000)
}

// TestCommandHTTPServerDownStillFails confirms the failure surfaces as an
// error, which is what Speaker turns into a fallback rather than a lost page.
func TestCommandHTTPServerDown(t *testing.T) {
	for _, bin := range []string{"sh", "curl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed", bin)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := NewCommand(CommandOptions{
		Name:   "sh",
		Args:   []string{"-c", `exec curl -sf "$1/voice" -o "$2"`, "sh", srv.URL, OutputPlaceholder},
		Format: "wav",
	})
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "text")
	require.Error(t, err, "a 500 from the model server must not look like success")
}
