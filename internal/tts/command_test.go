package tts

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// helperEnv activates [TestHelperProcess] in a re-executed test binary.
const helperEnv = "TGPAGER_TTS_HELPER"

// helperOptions runs this test binary as a stand-in speech program.
//
// A shell would be simpler to write and wrong to rely on: an output path such
// as C:\Users\...\speech.wav carries backslashes that sh treats as escapes, so
// these tests only passed where sh happened to be POSIX.
func helperOptions(t *testing.T, mode string, args ...string) CommandOptions {
	t.Helper()
	t.Setenv(helperEnv, "1")

	return CommandOptions{
		Name:   os.Args[0],
		Args:   append([]string{"-test.run=^TestHelperProcess$", "--", mode}, args...),
		Format: "wav",
	}
}

// TestHelperProcess is not a test. It stands in for a speech binary when the
// environment marks it, and exits before the test framework writes anything of
// its own to stdout.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("not a helper invocation")
	}

	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "helper: no mode")
		os.Exit(2)
	}

	switch mode, rest := args[0], args[1:]; mode {
	case "stdout":
		// Speak whatever arrives on stdin, the way piper does.
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "outfile":
		// rest: <text> <output>
		if err := os.WriteFile(rest[1], []byte(rest[0]), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "helper:", err)
			os.Exit(1)
		}
	case "fetch":
		// rest: <url> <text> <output>
		if err := helperFetch(rest[0], rest[1], rest[2]); err != nil {
			fmt.Fprintln(os.Stderr, "helper:", err)
			os.Exit(1)
		}
	case "fail":
		fmt.Fprintln(os.Stderr, "no voice model")
		os.Exit(1)
	case "hang":
		time.Sleep(time.Minute)
	case "empty":
	}
	os.Exit(0)
}

func helperFetch(base, text, out string) error {
	req, err := http.NewRequest(http.MethodGet, base+"/voice", http.NoBody)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("text", text)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("model server: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(out, body, 0o600)
}

func TestCommandReadsStdout(t *testing.T) {
	c, err := NewCommand(helperOptions(t, "stdout"))
	require.NoError(t, err)

	audio, err := c.Synthesize(t.Context(), "spoken text")
	require.NoError(t, err)
	require.Equal(t, []byte("spoken text"), audio.Data, "stdin is piped, stdout is captured")
	require.Equal(t, "wav", audio.Format)
}

func TestCommandSubstitutesPlaceholders(t *testing.T) {
	c, err := NewCommand(helperOptions(t, "outfile", TextPlaceholder, OutputPlaceholder))
	require.NoError(t, err)

	audio, err := c.Synthesize(t.Context(), "hello there")
	require.NoError(t, err)
	require.Equal(t, []byte("hello there"), audio.Data, "text in, output file read back")
}

func TestCommandFailureCarriesStderr(t *testing.T) {
	c, err := NewCommand(helperOptions(t, "fail"))
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "text")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no voice model", "the reason must survive")
}

func TestCommandTimesOut(t *testing.T) {
	opts := helperOptions(t, "hang")
	opts.Timeout = 50 * time.Millisecond

	c, err := NewCommand(opts)
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "text")
	require.Error(t, err, "a wedged binary must not hang the page")
}

func TestCommandEmptyOutputIsAnError(t *testing.T) {
	c, err := NewCommand(helperOptions(t, "empty"))
	require.NoError(t, err)

	_, err = c.Synthesize(t.Context(), "text")
	require.Error(t, err, "silent audio would be a silent page")
}

func TestNewCommandRequiresName(t *testing.T) {
	_, err := NewCommand(CommandOptions{})
	require.Error(t, err)
}
