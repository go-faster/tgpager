package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretResolve(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token"), []byte("from-file\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crlf"), []byte("from-file\r\n"), 0o600))
	t.Setenv("TGPAGER_TEST_SECRET", "from-env")

	tests := []struct {
		name    string
		secret  Secret
		want    string
		wantErr bool
	}{
		{"unset", Secret{}, "", false},
		{"literal", Secret{Value: "literal"}, "literal", false},
		{"env", Secret{Env: "TGPAGER_TEST_SECRET"}, "from-env", false},
		{"env unset is empty", Secret{Env: "TGPAGER_TEST_ABSENT"}, "", false},
		{"relative file", Secret{File: "token"}, "from-file", false},
		{"absolute file", Secret{File: filepath.Join(dir, "token")}, "from-file", false},
		{"strips crlf", Secret{File: "crlf"}, "from-file", false},
		{"missing file", Secret{File: "nope"}, "", true},
		{"two spellings", Secret{Value: "a", Env: "TGPAGER_TEST_SECRET"}, "", true},
		{"three spellings", Secret{Value: "a", Env: "b", File: "c"}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.secret
			err := s.resolve(dir)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, s.Value)
			require.Empty(t, s.Env, "resolve must leave one spelling behind")
			require.Empty(t, s.File)
		})
	}
}

// TestSecretSpellingsInConfig is the property that matters: the same credential
// written three ways reaches the same place.
func TestSecretSpellingsInConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hash.txt"), []byte("from-file\n"), 0o600))
	t.Setenv("TGPAGER_TEST_APP_HASH", "from-env")

	tests := []struct {
		name string
		body string
		want string
	}{
		{"scalar", `app_hash: "literal"`, "literal"},
		{"object value", "app_hash:\n    value: literal", "literal"},
		{"env", "app_hash:\n    env: TGPAGER_TEST_APP_HASH", "from-env"},
		{"file", "app_hash:\n    file: hash.txt", "from-file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, "config.yaml")
			body := "telegram:\n  app_id: 1\n  " + tt.body + "\npeer: \"@oncall\"\naudio: tone.ogg\n"
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

			cfg, _, err := Load(path)
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.Telegram.AppHash.Value)
		})
	}
}

func TestSecretFileIsRelativeToConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token.txt"), []byte("shhh"), 0o600))
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
telegram:
  app_id: 1
  app_hash: h
peer: "@oncall"
audio: tone.ogg
webhook:
  token:
    file: token.txt
`), 0o600))

	cfg, _, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "shhh", cfg.Webhook.Token.Value,
		"a relative secret path must resolve against the config file, not the working directory")
}

func TestAppHashRequired(t *testing.T) {
	_, _, err := Load(writeYAML(t, `
telegram:
  app_id: 1
peer: "@oncall"
audio: tone.ogg
`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "app_hash")
}

func TestSecretFileMissingIsAnError(t *testing.T) {
	_, _, err := Load(writeYAML(t, `
telegram:
  app_id: 1
  app_hash:
    file: absent.txt
peer: "@oncall"
audio: tone.ogg
`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "telegram.app_hash")
}

func TestBotTokenSpellings(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bot.txt"), []byte("123:from-file\n"), 0o600))
	t.Setenv("TGPAGER_TEST_BOT_TOKEN", "123:from-env")

	tests := []struct {
		name string
		body string
		want string
	}{
		{"scalar", `bot_token: "123:literal"`, "123:literal"},
		{"env", "bot_token:\n    env: TGPAGER_TEST_BOT_TOKEN", "123:from-env"},
		{"file", "bot_token:\n    file: bot.txt", "123:from-file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, "config.yaml")
			body := "telegram:\n  app_id: 1\n  app_hash: h\n  " + tt.body +
				"\npeer: \"@oncall\"\naudio: tone.ogg\nvoice:\n  mode: only\n"
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

			cfg, _, err := Load(path)
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.Telegram.BotToken.Value)
		})
	}
}

// TestBotTokenRejectsCallingModes guards the one combination that cannot work:
// Telegram reserves calls for users, so a bot asked to place one fails at every
// page rather than at startup.
func TestBotTokenRejectsCallingModes(t *testing.T) {
	for _, mode := range []VoiceMode{VoiceOff, VoiceFallback, VoiceAlways} {
		t.Run(string(mode), func(t *testing.T) {
			_, _, err := Load(writeYAML(t, `
telegram:
  app_id: 1
  app_hash: h
  bot_token: "123:abc"
peer: "@oncall"
audio: tone.ogg
voice:
  mode: `+string(mode)+"\n"))
			require.Error(t, err)
			require.Contains(t, err.Error(), "bots cannot place calls")
		})
	}
}

func TestBotTokenAllowsVoiceOnly(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram:
  app_id: 1
  app_hash: h
  bot_token: "123:abc"
peer: "@oncall"
audio: tone.ogg
voice:
  mode: only
`))
	require.NoError(t, err)
	require.Equal(t, "123:abc", cfg.Telegram.BotToken.Value)
	require.False(t, cfg.Voice.Mode.Calls())
}

func TestNoBotTokenLeavesCallingModesAlone(t *testing.T) {
	cfg, _, err := Load(writeYAML(t, `
telegram:
  app_id: 1
  app_hash: h
peer: "@oncall"
audio: tone.ogg
voice:
  mode: fallback
`))
	require.NoError(t, err)
	require.Empty(t, cfg.Telegram.BotToken.Value)
	require.True(t, cfg.Voice.Mode.Calls())
}
