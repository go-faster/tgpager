package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
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
	// Instructions steer delivery, for example "Speak urgently and clearly".
	// Ignored by older models such as tts-1.
	Instructions string
	// Speed multiplies playback, 0.25 to 4.0. Zero leaves it to the provider.
	Speed float64
	// Dialect selects where Instructions goes on the wire.
	Dialect        Dialect
	Timeout        time.Duration
	Client         *http.Client
	TracerProvider trace.TracerProvider
}

// Dialect names a wire variation between OpenAI-compatible endpoints.
//
// Everything else about the endpoint is shared, but instructions are top level
// for OpenAI and nested under provider options for OpenRouter, so the caller
// says which one it is talking to rather than the code guessing from a URL.
type Dialect string

const (
	// DialectOpenAI puts instructions at the top level.
	DialectOpenAI Dialect = "openai"
	// DialectOpenRouter nests instructions under provider.options.openai.
	DialectOpenRouter Dialect = "openrouter"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultOpenAIFormat  = "mp3"
	defaultTimeout       = 10 * time.Second
	// maxAudioBytes bounds an untrusted response body.
	maxAudioBytes = 32 << 20
)

// OpenAI is a Synthesizer backed by an OpenAI-compatible speech endpoint.
type OpenAI struct {
	opts   OpenAIOptions
	cl     *http.Client
	tracer trace.Tracer
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
	switch opts.Dialect {
	case "":
		opts.Dialect = DialectOpenAI
	case DialectOpenAI, DialectOpenRouter:
	default:
		return nil, errors.Errorf("unknown dialect %q", opts.Dialect)
	}
	if opts.Speed != 0 && (opts.Speed < 0.25 || opts.Speed > 4) {
		return nil, errors.Errorf("speed must be between 0.25 and 4.0, got %v", opts.Speed)
	}
	cl := opts.Client
	if cl == nil {
		cl = &http.Client{}
	}
	if opts.TracerProvider == nil {
		opts.TracerProvider = noop.NewTracerProvider()
	}
	return &OpenAI{
		opts:   opts,
		cl:     cl,
		tracer: opts.TracerProvider.Tracer(InstrumentationName),
	}, nil
}

// Fingerprint covers everything that changes how the audio sounds, so a
// changed voice, speed or instruction never serves the old recording.
func (o *OpenAI) Fingerprint() string {
	return strings.Join([]string{
		"openai", o.opts.BaseURL, o.opts.Model, o.opts.Voice, o.opts.Format,
		o.opts.Instructions, strconv.FormatFloat(o.opts.Speed, 'f', -1, 64),
		string(o.opts.Dialect),
	}, "|")
}

func (o *OpenAI) Format() string { return o.opts.Format }

type speechRequest struct {
	Model          string           `json:"model"`
	Input          string           `json:"input"`
	Voice          string           `json:"voice,omitempty"`
	ResponseFormat string           `json:"response_format,omitempty"`
	Instructions   string           `json:"instructions,omitempty"`
	Speed          float64          `json:"speed,omitempty"`
	Provider       *providerOptions `json:"provider,omitempty"`
}

type providerOptions struct {
	Options struct {
		OpenAI struct {
			Instructions string `json:"instructions,omitempty"`
		} `json:"openai"`
	} `json:"options"`
}

func (o *OpenAI) buildRequest(text string) speechRequest {
	req := speechRequest{
		Model:          o.opts.Model,
		Input:          text,
		Voice:          o.opts.Voice,
		ResponseFormat: o.opts.Format,
		Speed:          o.opts.Speed,
	}
	if o.opts.Instructions == "" {
		return req
	}
	if o.opts.Dialect == DialectOpenRouter {
		var p providerOptions
		p.Options.OpenAI.Instructions = o.opts.Instructions
		req.Provider = &p
		return req
	}
	req.Instructions = o.opts.Instructions
	return req
}

func (o *OpenAI) Synthesize(ctx context.Context, text string) (audio Audio, err error) {
	// The text itself never reaches a span: alert labels carry hostnames and
	// customer identifiers.
	ctx, span := o.tracer.Start(ctx, "tts.openai.Synthesize", trace.WithAttributes(
		attribute.String("tts.provider", "openai"),
		attribute.String("tts.model", o.opts.Model),
		attribute.String("tts.voice", o.opts.Voice),
		attribute.String("tts.dialect", string(o.opts.Dialect)),
		attribute.String("server.address", o.opts.BaseURL),
		attribute.Int("tts.text_bytes", len(text)),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	ctx, cancel := context.WithTimeout(ctx, o.opts.Timeout)
	defer cancel()

	body, err := json.Marshal(o.buildRequest(text))
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
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

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

	span.SetAttributes(attribute.Int("tts.audio_bytes", len(data)))

	audio = Audio{Data: data, Format: o.opts.Format}
	if err := audio.validate(); err != nil {
		return Audio{}, err
	}
	return audio, nil
}
