package tts

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not installed", name)
	}
}

func TestCommandReadsStdout(t *testing.T) {
	requireCommand(t, "cat")

	c, err := NewCommand(CommandOptions{Name: "cat", Format: "wav"})
	require.NoError(t, err)

	audio, err := c.Synthesize(t.Context(), "spoken text")
	require.NoError(t, err)
	require.Equal(t, []byte("spoken text"), audio.Data, "stdin is piped, stdout is captured")
	require.Equal(t, "wav", audio.Format)
}

func TestCommandSubstitutesPlaceholders(t *testing.T) {
	requireCommand(t, "sh")

	c, err := NewCommand(CommandOptions{
		Name:   "sh",
		Args:   []string{"-c", `printf '%s' "$1" > ` + OutputPlaceholder, "sh", TextPlaceholder},
		Format: "wav",
	})
	require.NoError(t, err)

	audio, err := c.Synthesize(t.Context(), "hello there")
	require.NoError(t, err)
	require.Equal(t, []byte("hello there"), audio.Data, "text in, output file read back")
}

func TestCommandFailureCarriesStderr(t *testing.T) {
	requireCommand(t, "sh")

	c, err := NewCommand(CommandOptions{
		Name: "sh",
		Args: []string{"-c", "echo 'no voice model' >&2; exit 1"},
	})
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "text")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no voice model", "the reason must survive")
}

func TestCommandTimesOut(t *testing.T) {
	requireCommand(t, "sleep")

	c, err := NewCommand(CommandOptions{Name: "sleep", Args: []string{"60"}, Timeout: 30 * time.Millisecond})
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "text")
	require.Error(t, err, "a wedged binary must not hang the page")
}

func TestCommandEmptyOutputIsAnError(t *testing.T) {
	requireCommand(t, "true")

	c, err := NewCommand(CommandOptions{Name: "true"})
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "text")
	require.Error(t, err, "silent audio would be a silent page")
}

func TestNewCommandRequiresName(t *testing.T) {
	_, err := NewCommand(CommandOptions{})
	require.Error(t, err)
}
