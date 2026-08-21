// Package config declares tgpager's configuration once and derives decoding,
// validation, defaults and documentation from that declaration.
package config

import (
	"iter"
	"slices"
	"time"

	"github.com/go-faster/errors"
	"github.com/go-faster/figureout"
	"github.com/go-faster/figureout/schema/docs"
	"github.com/go-faster/figureout/schema/jsonschema"
	"github.com/go-faster/figureout/source/env"
	"github.com/go-faster/figureout/source/yaml"
)

// EnvPrefix prefixes every environment variable, e.g. TGPAGER_PEER.
const EnvPrefix = "TGPAGER_"

// Telegram identifies the application to Telegram. Obtain a pair at
// https://my.telegram.org.
type Telegram struct {
	AppID   int
	AppHash string
	Session string
}

// Call tunes how a page is placed.
type Call struct {
	RingTimeout    time.Duration
	ConnectTimeout time.Duration
	Attempts       int
	RetryDelay     time.Duration
}

// Webhook is the Alertmanager listener.
type Webhook struct {
	Addr      string
	Token     figureout.OptionalOf[string]
	QueueSize int
}

// OpenAITTS is an OpenAI-compatible /audio/speech endpoint. BaseURL selects
// the vendor: OpenAI, OpenRouter, Azure or a local compatible server.
type OpenAITTS struct {
	BaseURL      string
	APIKey       string
	Model        string
	Voice        string
	Format       string
	Instructions string
	Speed        figureout.OptionalOf[float64]
	Dialect      TTSDialect
}

// TTSDialect selects where instructions go on the wire. Everything else about
// an OpenAI-compatible endpoint is shared.
type TTSDialect string

// Dialects.
const (
	DialectOpenAI     TTSDialect = "openai"
	DialectOpenRouter TTSDialect = "openrouter"
)

// AllValues implements [figureout.EnumValuer].
func (TTSDialect) AllValues() iter.Seq[TTSDialect] {
	return slices.Values([]TTSDialect{DialectOpenAI, DialectOpenRouter})
}

// CommandTTS synthesizes by running a local binary, such as piper or espeak-ng.
type CommandTTS struct {
	Name         string
	Args         []string
	OutputFormat string
}

// TTSProvider is the selected provider. Exactly one variant is populated.
type TTSProvider struct {
	OpenAI  *OpenAITTS
	Command *CommandTTS
}

// TTS turns an alert into speech played after the tone. The whole section is
// optional: absent means pages play the audio file alone.
type TTS struct {
	Provider TTSProvider
	Template string
	Cache    string
	Repeat   int
	Timeout  time.Duration
}

// Config is the tgpager configuration.
type Config struct {
	Telegram  Telegram
	Webhook   Webhook
	Call      Call
	TTS       figureout.OptionalOf[TTS]
	Peer      string
	Audio     string
	PeerCache string
	Debug     bool
}

// TelegramDescriptor describes [Telegram].
var TelegramDescriptor = figureout.MustDerive(
	func(c *Telegram, s *figureout.Schema[Telegram]) {
		figureout.Explicit(s, &c.AppID, "app_id").
			Doc("Telegram application ID.").InRange(1, 1<<31-1)
		figureout.Explicit(s, &c.AppHash, "app_hash", figureout.Secret()).
			Doc("Telegram application hash.").NonEmpty()
		figureout.Value(s, &c.Session, "session").
			Doc("Path to the session file. Holds credentials; keep it private.").
			NonEmpty().ApplyDefault("session.json")
	},
)

// CallDescriptor describes [Call].
var CallDescriptor = figureout.MustDerive(
	func(c *Call, s *figureout.Schema[Call]) {
		figureout.Value(s, &c.RingTimeout, "ring_timeout").
			Doc("How long an unanswered call keeps ringing.").
			AtLeast(time.Second).ApplyDefault(45 * time.Second)
		figureout.Value(s, &c.ConnectTimeout, "connect_timeout").
			Doc("How long an accepted call may take to negotiate media.").
			AtLeast(time.Second).ApplyDefault(30 * time.Second)
		figureout.Value(s, &c.Attempts, "attempts").
			Doc("How many times to place a call before giving up.").
			InRange(1, 100).ApplyDefault(3)
		figureout.Value(s, &c.RetryDelay, "retry_delay").
			Doc("Delay between call attempts.").
			AtLeast(0).ApplyDefault(10 * time.Second)
	},
)

// WebhookDescriptor describes [Webhook].
var WebhookDescriptor = figureout.MustDerive(
	func(c *Webhook, s *figureout.Schema[Webhook]) {
		figureout.Value(s, &c.Addr, "addr").
			Doc("HTTP listen address.").NonEmpty().ApplyDefault(":8080")
		figureout.Optional(s, &c.Token, "token", figureout.Secret()).
			Doc("Bearer token required from Alertmanager. Unset means unauthenticated.")
		figureout.Value(s, &c.QueueSize, "queue_size").
			Doc("How many pages may wait to be placed.").
			InRange(1, 10000).ApplyDefault(100)
	},
)

// OpenAITTSDescriptor describes [OpenAITTS].
var OpenAITTSDescriptor = figureout.MustDerive(
	func(c *OpenAITTS, s *figureout.Schema[OpenAITTS]) {
		figureout.Value(s, &c.BaseURL, "base_url").
			Doc("Base URL of the speech endpoint, without a trailing /audio/speech.").
			NonEmpty().ApplyDefault("https://api.openai.com/v1")
		figureout.Value(s, &c.APIKey, "api_key", figureout.Secret()).
			Doc("Bearer token for the speech endpoint.")
		figureout.Explicit(s, &c.Model, "model").
			Doc("Speech model, for example openai/gpt-4o-mini-tts.").NonEmpty()
		figureout.Value(s, &c.Voice, "voice").
			Doc("Voice name, as understood by the model.").ApplyDefault("alloy")
		// Variants share the tts.provider path, so a field name repeated across
		// them would be one path with two aliases: setting the "command" name
		// would silently configure openai. Names are kept distinct instead.
		figureout.Value(s, &c.Format, "format").
			Doc("Audio format to request.").NonEmpty().ApplyDefault("mp3")
		figureout.Value(s, &c.Instructions, "instructions").
			Doc(`How to deliver the line, for example "Speak urgently and clearly". ` +
				"Ignored by older models such as tts-1.")
		figureout.Optional(s, &c.Speed, "speed").
			Doc("Playback multiplier. Unset leaves it to the provider.").
			InRange(0.25, 4)
		figureout.Enum(s, &c.Dialect, "dialect").
			Doc("Where instructions go on the wire: top level for openai, nested for openrouter.").
			ApplyDefault(DialectOpenAI)
	},
)

// CommandTTSDescriptor describes [CommandTTS].
var CommandTTSDescriptor = figureout.MustDerive(
	func(c *CommandTTS, s *figureout.Schema[CommandTTS]) {
		figureout.Explicit(s, &c.Name, "name").
			Doc("Executable to run, for example piper.").NonEmpty()
		figureout.Value(s, &c.Args, "args").
			Doc(`Arguments. {{text}} is replaced by the text to speak, otherwise ` +
				`it is written to stdin; {{output}} is replaced by a temporary ` +
				`file to write, otherwise audio is read from stdout.`)
		figureout.Value(s, &c.OutputFormat, "output_format").
			Doc("Audio format the command produces.").NonEmpty().ApplyDefault("wav")
	},
)

// TTSDescriptor describes [TTS].
var TTSDescriptor = figureout.MustDerive(
	func(c *TTS, s *figureout.Schema[TTS]) {
		figureout.OneOf(s, &c.Provider, "provider",
			figureout.Discriminator("type"),
			figureout.Variant("openai", &c.Provider.OpenAI, OpenAITTSDescriptor),
			figureout.Variant("command", &c.Provider.Command, CommandTTSDescriptor),
		).Doc("Speech provider. Omit to page with the audio file alone.")

		figureout.Value(s, &c.Template, "template").
			Doc("Go template rendered into the spoken sentence.")
		figureout.Value(s, &c.Cache, "cache").
			Doc("Directory holding synthesized audio, reused across resends.").
			NonEmpty().ApplyDefault("tts-cache")
		figureout.Value(s, &c.Repeat, "repeat").
			Doc("How many times to play tone and speech, so a groggy callee gets a second chance.").
			InRange(1, 10).ApplyDefault(3)
		figureout.Value(s, &c.Timeout, "timeout").
			Doc("How long to wait for synthesis before paging without speech.").
			AtLeast(time.Second).ApplyDefault(10 * time.Second)
	},
)

// Descriptor describes [Config].
var Descriptor = figureout.MustDerive(
	func(c *Config, s *figureout.Schema[Config]) {
		figureout.Object(s, &c.Telegram, "telegram", TelegramDescriptor)
		figureout.Object(s, &c.Webhook, "webhook", WebhookDescriptor)
		figureout.Object(s, &c.Call, "call", CallDescriptor)
		figureout.OptionalObject(s, &c.TTS, "tts", TTSDescriptor)

		figureout.Explicit(s, &c.Peer, "peer").
			Doc("Call target: @username, phone, t.me link, or id:<user-id>[:<access-hash>].").
			NonEmpty()
		figureout.Explicit(s, &c.Audio, "audio").
			Doc("Audio file played into the call, and the tone before speech.").NonEmpty()
		figureout.Value(s, &c.PeerCache, "peer_cache").
			Doc("Path to the peer access hash cache. Account-scoped.").
			NonEmpty().ApplyDefault("peers.bolt")
		figureout.Value(s, &c.Debug, "debug").
			Doc("Enable debug logging.").ApplyDefault(false)
	},
)

// Load resolves configuration from an optional YAML file, then the
// environment, which wins.
func Load(path string) (Config, *figureout.Report, error) {
	return Descriptor.Resolve(
		yaml.File(path, yaml.Optional()),
		env.Current(env.Prefix(EnvPrefix)),
	)
}

// Reference renders the Markdown configuration reference from [Descriptor].
func Reference() ([]byte, error) {
	page, _, err := docs.Generate(Descriptor,
		docs.Title("tgpager configuration"),
		docs.ForSource(yaml.File("")),
		docs.ForSource(env.Current(env.Prefix(EnvPrefix))),
	)
	if err != nil {
		return nil, errors.Wrap(err, "generate reference")
	}
	return page, nil
}

// JSONSchema renders the JSON Schema for [Descriptor], for editor completion
// and for validating a configuration file in CI.
func JSONSchema() ([]byte, error) {
	schema, _, err := jsonschema.Generate(Descriptor,
		jsonschema.Title("tgpager configuration"),
		jsonschema.Semantic(),
		jsonschema.ForSource(yaml.Source),
	)
	if err != nil {
		return nil, errors.Wrap(err, "generate json schema")
	}
	return schema, nil
}
