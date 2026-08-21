package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadDefaults(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram:
  app_id: 12345
  app_hash: deadbeef
peer: "@oncall"
audio: tone.ogg
`))
	require.NoError(t, err)

	require.Equal(t, 12345, cfg.Telegram.AppID)
	require.Equal(t, "session.json", cfg.Telegram.Session)
	require.Equal(t, ":8080", cfg.Webhook.Addr)
	require.Equal(t, 100, cfg.Webhook.QueueSize)
	require.Equal(t, 45*time.Second, cfg.Call.RingTimeout)
	require.Equal(t, 3, cfg.Call.Attempts)
	require.Equal(t, "peers.bolt", cfg.PeerCache)

	_, ok := cfg.Webhook.Token.Value()
	require.False(t, ok, "token must default to unset")
}

func TestLoadEnvOverridesFile(t *testing.T) {
	t.Setenv(EnvPrefix+"PEER", "@from-env")
	t.Setenv(EnvPrefix+"CALL_ATTEMPTS", "7")
	t.Setenv(EnvPrefix+"WEBHOOK_TOKEN", "s3cret")

	cfg, _, err := Load(writeYAML(t, `
telegram:
  app_id: 1
  app_hash: h
peer: "@from-file"
audio: tone.ogg
call:
  attempts: 2
`))
	require.NoError(t, err)

	require.Equal(t, "@from-env", cfg.Peer, "env must win over file")
	require.Equal(t, 7, cfg.Call.Attempts)

	token, ok := cfg.Webhook.Token.Value()
	require.True(t, ok)
	require.Equal(t, "s3cret", token)
}

func TestLoadValidates(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing peer", "telegram: {app_id: 1, app_hash: h}\naudio: a.ogg\n"},
		{"missing audio", "telegram: {app_id: 1, app_hash: h}\npeer: \"@x\"\n"},
		{"empty app hash", "telegram: {app_id: 1, app_hash: \"\"}\npeer: \"@x\"\naudio: a.ogg\n"},
		{"zero app id", "telegram: {app_id: 0, app_hash: h}\npeer: \"@x\"\naudio: a.ogg\n"},
		{"attempts out of range", "telegram: {app_id: 1, app_hash: h}\npeer: \"@x\"\naudio: a.ogg\ncall: {attempts: 0}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Load(writeYAML(t, tt.body))
			require.Error(t, err)
		})
	}
}

func TestLoadTTS(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram: {app_id: 1, app_hash: h}
peer: "@x"
audio: tone.ogg
tts:
  provider:
    type: openai
    base_url: https://openrouter.ai/api/v1
    model: openai/gpt-4o-mini-tts
    instructions: Speak urgently and clearly.
    dialect: openrouter
    speed: 0.9
  repeat: 2
`))
	require.NoError(t, err)

	tts, ok := cfg.TTS.Value()
	require.True(t, ok)
	require.NotNil(t, tts.Provider.OpenAI)
	require.Nil(t, tts.Provider.Command, "only the selected variant is populated")
	require.Equal(t, "Speak urgently and clearly.", tts.Provider.OpenAI.Instructions)
	require.Equal(t, DialectOpenRouter, tts.Provider.OpenAI.Dialect)
	require.Equal(t, 2, tts.Repeat)
	require.Equal(t, 10*time.Second, tts.Timeout, "shared default applies")

	speed, ok := tts.Provider.OpenAI.Speed.Value()
	require.True(t, ok)
	require.InDelta(t, 0.9, speed, 0.001)
}

func TestLoadTTSAbsentMeansDisabled(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram: {app_id: 1, app_hash: h}
peer: "@x"
audio: tone.ogg
`))
	require.NoError(t, err)

	_, ok := cfg.TTS.Value()
	require.False(t, ok, "no tts section means speech is off")
}

func TestLoadTTSCommandVariant(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram: {app_id: 1, app_hash: h}
peer: "@x"
audio: tone.ogg
tts:
  provider:
    type: command
    name: piper
    args: ["--model", "en.onnx", "--output_file", "{{output}}"]
`))
	require.NoError(t, err)

	tts, ok := cfg.TTS.Value()
	require.True(t, ok)
	require.NotNil(t, tts.Provider.Command)
	require.Nil(t, tts.Provider.OpenAI)
	require.Equal(t, "piper", tts.Provider.Command.Name)
	require.Equal(t, "wav", tts.Provider.Command.OutputFormat)
}

func TestLoadTTSRejectsBadDialect(t *testing.T) {
	_, _, err := Load(writeYAML(t, `
telegram: {app_id: 1, app_hash: h}
peer: "@x"
audio: tone.ogg
tts:
  provider:
    type: openai
    model: m
    dialect: azure
`))
	require.Error(t, err, "an unknown dialect would silently drop instructions")
}

func TestLoadTTSCacheDefaults(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram: {app_id: 1, app_hash: h}
peer: "@x"
audio: tone.ogg
tts:
  provider: {type: command, name: piper}
`))
	require.NoError(t, err)

	tts, ok := cfg.TTS.Value()
	require.True(t, ok)
	require.Equal(t, "tts-cache", tts.Cache.Dir)
	require.Equal(t, 30*24*time.Hour, tts.Cache.TTL, "the cache must not grow forever by default")
	require.Equal(t, int64(256<<20), tts.Cache.MaxBytes)
}

func TestLoadTTSCacheUnbounded(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram: {app_id: 1, app_hash: h}
peer: "@x"
audio: tone.ogg
tts:
  provider: {type: command, name: piper}
  cache:
    dir: /var/cache/tgpager
    ttl: 0s
    max_bytes: 0
`))
	require.NoError(t, err)

	tts, _ := cfg.TTS.Value()
	require.Equal(t, "/var/cache/tgpager", tts.Cache.Dir)
	require.Zero(t, tts.Cache.TTL, "zero must be expressible")
	require.Zero(t, tts.Cache.MaxBytes)
}

func TestVoiceMode(t *testing.T) {
	tests := []struct {
		mode           VoiceMode
		calls          bool
		sendsOnSuccess bool
		sendsOnFailure bool
	}{
		{VoiceOff, true, false, false},
		{VoiceFallback, true, false, true},
		{VoiceAlways, true, true, true},
		{VoiceOnly, false, true, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			require.Equal(t, tt.calls, tt.mode.Calls())
			require.Equal(t, tt.sendsOnSuccess, tt.mode.Sends(false))
			require.Equal(t, tt.sendsOnFailure, tt.mode.Sends(true))
		})
	}
}

const minimalConfig = `
telegram:
  app_id: 1
  app_hash: h
peer: "@oncall"
audio: tone.ogg
`

func TestVoiceDefaults(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, minimalConfig))
	require.NoError(t, err)
	require.Equal(t, VoiceOff, cfg.Voice.Mode, "voice must be off unless asked for")
	require.Equal(t, 60*time.Second, cfg.Voice.Timeout)
	require.Equal(t, 3, cfg.Voice.Attempts)
	require.Equal(t, 2*time.Second, cfg.Voice.RetryDelay)
}

func TestVoiceModeFromConfig(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, minimalConfig+"voice:\n  mode: fallback\n"))
	require.NoError(t, err)
	require.Equal(t, VoiceFallback, cfg.Voice.Mode)
}

func TestVoiceModeRejectsUnknown(t *testing.T) {
	_, _, err := Load(writeYAML(t, minimalConfig+"voice:\n  mode: shout\n"))
	require.Error(t, err)
}
