// Package config declares tgpager's configuration once and derives decoding,
// validation, defaults and documentation from that declaration.
package config

import (
	"time"

	"github.com/go-faster/errors"
	"github.com/go-faster/figureout"
	"github.com/go-faster/figureout/schema/docs"
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

// Config is the tgpager configuration.
type Config struct {
	Telegram  Telegram
	Webhook   Webhook
	Call      Call
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

// Descriptor describes [Config].
var Descriptor = figureout.MustDerive(
	func(c *Config, s *figureout.Schema[Config]) {
		figureout.Object(s, &c.Telegram, "telegram", TelegramDescriptor)
		figureout.Object(s, &c.Webhook, "webhook", WebhookDescriptor)
		figureout.Object(s, &c.Call, "call", CallDescriptor)

		figureout.Explicit(s, &c.Peer, "peer").
			Doc("Call target: @username, phone, t.me link, or id:<user-id>[:<access-hash>].").
			NonEmpty()
		figureout.Explicit(s, &c.Audio, "audio").
			Doc("Audio file played into the call.").NonEmpty()
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
