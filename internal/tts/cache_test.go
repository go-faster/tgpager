package tts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheRoundTrip(t *testing.T) {
	c, err := NewCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	_, ok := c.Lookup("fp", "text", "mp3")
	require.False(t, ok)

	path, err := c.Store("fp", "text", Audio{Data: []byte("audio"), Format: "mp3"})
	require.NoError(t, err)

	hit, ok := c.Lookup("fp", "text", "mp3")
	require.True(t, ok)
	require.Equal(t, path, hit)

	data, err := os.ReadFile(hit)
	require.NoError(t, err)
	require.Equal(t, []byte("audio"), data)
}

func TestCacheKeySeparatesInputs(t *testing.T) {
	require.NotEqual(t, key("fp", "a"), key("fp", "b"), "different text")
	require.NotEqual(t, key("voice-a", "t"), key("voice-b", "t"), "different voice")
	require.Equal(t, key("fp", "t"), key("fp", "t"), "stable")

	// Guards against naive concatenation, where fingerprint+text could collide.
	require.NotEqual(t, key("ab", "c"), key("a", "bc"))
}

func TestCacheIgnoresEmptyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	c, err := NewCache(dir)
	require.NoError(t, err)

	// A truncated file from a crash must not read back as a hit.
	require.NoError(t, os.WriteFile(c.path("fp", "text", "mp3"), nil, 0o600))

	_, ok := c.Lookup("fp", "text", "mp3")
	require.False(t, ok)
}

func TestNewCacheRequiresDir(t *testing.T) {
	_, err := NewCache("")
	require.Error(t, err)
}
