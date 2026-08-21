package tts

import (
	"github.com/go-faster/errors"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/go-faster/tgpager/internal/config"
)

// BuildOptions carries what a Speaker needs beyond configuration.
type BuildOptions struct {
	Logger         *zap.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	// Tone plays before speech and is what a failed synthesis falls back to.
	Tone string
}

// Build turns configuration into a Speaker. An absent tts section yields a
// Speaker that plays the tone alone.
func Build(cfg config.Config, opts BuildOptions) (*Speaker, error) {
	speaker := SpeakerOptions{
		Logger:         opts.Logger,
		TracerProvider: opts.TracerProvider,
		MeterProvider:  opts.MeterProvider,
		Tone:           opts.Tone,
		Repeat:         1,
	}

	ttsCfg, enabled := cfg.TTS.Value()
	if !enabled {
		speaker.Synthesizer = Disabled{}
		return NewSpeaker(speaker)
	}

	synth, err := newSynthesizer(ttsCfg, opts.TracerProvider)
	if err != nil {
		return nil, err
	}
	cache, err := NewCacheWith(CacheOptions{
		Dir:      ttsCfg.Cache.Dir,
		TTL:      ttsCfg.Cache.TTL,
		MaxBytes: ttsCfg.Cache.MaxBytes,
	})
	if err != nil {
		return nil, err
	}
	tmpl, err := NewTemplate(ttsCfg.Template)
	if err != nil {
		return nil, err
	}

	speaker.Synthesizer = synth
	speaker.Cache = cache
	speaker.Template = tmpl
	speaker.Repeat = ttsCfg.Repeat
	return NewSpeaker(speaker)
}

func newSynthesizer(cfg config.TTS, tp trace.TracerProvider) (Synthesizer, error) {
	switch p := cfg.Provider; {
	case p.OpenAI != nil:
		speed, _ := p.OpenAI.Speed.Value()
		return NewOpenAI(OpenAIOptions{
			BaseURL:        p.OpenAI.BaseURL,
			APIKey:         p.OpenAI.APIKey,
			Model:          p.OpenAI.Model,
			Voice:          p.OpenAI.Voice,
			Format:         p.OpenAI.Format,
			Instructions:   p.OpenAI.Instructions,
			Speed:          speed,
			Dialect:        Dialect(p.OpenAI.Dialect),
			Timeout:        cfg.Timeout,
			TracerProvider: tp,
		})
	case p.Command != nil:
		return NewCommand(CommandOptions{
			Name:           p.Command.Name,
			Args:           p.Command.Args,
			Format:         p.Command.OutputFormat,
			Timeout:        cfg.Timeout,
			TracerProvider: tp,
		})
	default:
		return nil, errors.New("tts is configured with no provider")
	}
}
