package tts

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-faster/errors"
)

// CommandOptions configures synthesis by running a local binary, such as
// piper or espeak-ng.
//
// A hosted provider is unreachable exactly when the network is the thing that
// broke, which is when a page matters most. A local binary always answers.
type CommandOptions struct {
	Name string
	Args []string
	// Format of the audio the command produces.
	Format  string
	Timeout time.Duration
}

// Placeholders substituted in Args.
const (
	// TextPlaceholder is replaced by the text to speak. Without it, text is
	// written to the command's stdin instead.
	TextPlaceholder = "{{text}}"
	// OutputPlaceholder is replaced by a temporary file the command must
	// write. Without it, audio is read from the command's stdout.
	OutputPlaceholder = "{{output}}"
)

// Command is a Synthesizer backed by a local executable.
type Command struct {
	opts CommandOptions
}

var _ Synthesizer = (*Command)(nil)

func NewCommand(opts CommandOptions) (*Command, error) {
	if opts.Name == "" {
		return nil, errors.New("command name is required")
	}
	if opts.Format == "" {
		opts.Format = "wav"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	return &Command{opts: opts}, nil
}

func (c *Command) Fingerprint() string {
	return strings.Join(append([]string{"command", c.opts.Name}, c.opts.Args...), "|")
}

func (c *Command) Format() string { return c.opts.Format }

func (c *Command) Synthesize(ctx context.Context, text string) (Audio, error) {
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "tgpager-tts-")
	if err != nil {
		return Audio{}, errors.Wrap(err, "temp dir")
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, "speech."+c.opts.Format)

	var (
		args      = make([]string, 0, len(c.opts.Args))
		wantsText bool
		wantsFile bool
	)
	for _, a := range c.opts.Args {
		if strings.Contains(a, TextPlaceholder) {
			wantsText = true
			a = strings.ReplaceAll(a, TextPlaceholder, text)
		}
		if strings.Contains(a, OutputPlaceholder) {
			wantsFile = true
			a = strings.ReplaceAll(a, OutputPlaceholder, out)
		}
		args = append(args, a)
	}

	// #nosec G204 -- the command and its arguments are operator-supplied config.
	cmd := exec.CommandContext(ctx, c.opts.Name, args...)
	if !wantsText {
		cmd.Stdin = strings.NewReader(text)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stderr = &stderr
	if !wantsFile {
		cmd.Stdout = &stdout
	}

	if err := cmd.Run(); err != nil {
		if reason := lastLine(stderr.Bytes()); reason != "" {
			return Audio{}, errors.Wrapf(err, "%s: %s", c.opts.Name, reason)
		}
		return Audio{}, errors.Wrap(err, c.opts.Name)
	}

	data := stdout.Bytes()
	if wantsFile {
		// #nosec G304 -- out is a path this function just created in its own temp dir.
		if data, err = os.ReadFile(out); err != nil {
			return Audio{}, errors.Wrap(err, "read command output")
		}
	}

	audio := Audio{Data: data, Format: c.opts.Format}
	if err := audio.validate(); err != nil {
		return Audio{}, err
	}
	return audio, nil
}

func lastLine(b []byte) string {
	var last string
	for line := range strings.SplitSeq(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			last = line
		}
	}
	return last
}
