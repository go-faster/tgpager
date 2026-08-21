package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-faster/errors"
)

// OpenAIOptions configures an OpenAI-compatible /audio/speech provider.
//
// The endpoint is shared by OpenAI, OpenRouter and Azure, so BaseURL is what
// selects the vendor. There is no per-vendor code.
type OpenAIOptions struct {
	BaseURL string
	APIKey  string
	Model   string
	Voice   string
	Format  string
	Timeout time.Duration
	Client  *http.Client
}

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultOpenAIFormat  = "mp3"
	defaultTimeout       = 10 * time.Second
	// maxAudioBytes bounds an untrusted response body.
	maxAudioBytes = 32 << 20
)

// OpenAI is a Synthesizer backed by an OpenAI-compatible speech endpoint.
type OpenAI struct {
	opts OpenAIOptions
	cl   *http.Client
}

var _ Synthesizer = (*OpenAI)(nil)

func NewOpenAI(opts OpenAIOptions) (*OpenAI, error) {
	if opts.Model == "" {
		return nil, errors.New("model is required")
	}
	if opts.BaseURL == "" {
		opts.BaseURL = defaultOpenAIBaseURL
	}
	opts.BaseURL = strings.TrimSuffix(opts.BaseURL, "/")
	if opts.Format == "" {
		opts.Format = defaultOpenAIFormat
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	cl := opts.Client
	if cl == nil {
		cl = &http.Client{}
	}
	return &OpenAI{opts: opts, cl: cl}, nil
}

func (o *OpenAI) Fingerprint() string {
	return strings.Join([]string{"openai", o.opts.BaseURL, o.opts.Model, o.opts.Voice, o.opts.Format}, "|")
}

func (o *OpenAI) Format() string { return o.opts.Format }

type speechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

func (o *OpenAI) Synthesize(ctx context.Context, text string) (Audio, error) {
	ctx, cancel := context.WithTimeout(ctx, o.opts.Timeout)
	defer cancel()

	body, err := json.Marshal(speechRequest{
		Model:          o.opts.Model,
		Input:          text,
		Voice:          o.opts.Voice,
		ResponseFormat: o.opts.Format,
	})
	if err != nil {
		return Audio{}, errors.Wrap(err, "encode request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.opts.BaseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return Audio{}, errors.Wrap(err, "create request")
	}
	req.Header.Set("Content-Type", "application/json")
	if o.opts.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.opts.APIKey)
	}

	resp, err := o.cl.Do(req)
	if err != nil {
		return Audio{}, errors.Wrap(err, "synthesize")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// The error body is small and not the audio; it explains the failure.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return Audio{}, errors.Errorf("synthesize: %s: %s",
			resp.Status, strings.TrimSpace(string(detail)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioBytes))
	if err != nil {
		return Audio{}, errors.Wrap(err, "read audio")
	}

	audio := Audio{Data: data, Format: o.opts.Format}
	if err := audio.validate(); err != nil {
		return Audio{}, err
	}
	return audio, nil
}
