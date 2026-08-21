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
