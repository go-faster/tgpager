package tts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// fakeClock lets eviction be tested without sleeping.
func atTime(t *testing.T, c *Cache, at time.Time) {
	t.Helper()
	c.now = func() time.Time { return at }
}

func TestCacheExpiresByTTL(t *testing.T) {
	c, err := NewCacheWith(CacheOptions{Dir: filepath.Join(t.TempDir(), "cache"), TTL: time.Hour})
	require.NoError(t, err)

	start := time.Now()
	atTime(t, c, start)
	_, err = c.Store("fp", "text", Audio{Data: []byte("audio"), Format: "mp3"})
	require.NoError(t, err)

	_, ok := c.Lookup("fp", "text", "mp3")
	require.True(t, ok, "fresh entry")

	atTime(t, c, start.Add(2*time.Hour))
	_, ok = c.Lookup("fp", "text", "mp3")
	require.False(t, ok, "expired entry must not be served")

	_, err = os.Stat(c.path("fp", "text", "mp3"))
	require.Error(t, err, "expired entry must be removed, not just ignored")
}

func TestCacheLookupRefreshesUse(t *testing.T) {
	c, err := NewCacheWith(CacheOptions{Dir: filepath.Join(t.TempDir(), "cache"), TTL: time.Hour})
	require.NoError(t, err)

	start := time.Now()
	atTime(t, c, start)
	_, err = c.Store("fp", "text", Audio{Data: []byte("audio"), Format: "mp3"})
	require.NoError(t, err)

	// Used again just before expiry, so it should survive past the original deadline.
	atTime(t, c, start.Add(50*time.Minute))
	_, ok := c.Lookup("fp", "text", "mp3")
	require.True(t, ok)

	atTime(t, c, start.Add(100*time.Minute))
	_, ok = c.Lookup("fp", "text", "mp3")
	require.True(t, ok, "a recently used entry must not expire on creation time")
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c, err := NewCacheWith(CacheOptions{
		Dir:      filepath.Join(t.TempDir(), "cache"),
		MaxBytes: 300,
	})
	require.NoError(t, err)

	start := time.Now()
	blob := Audio{Data: make([]byte, 100), Format: "mp3"}

	for i, text := range []string{"one", "two", "three"} {
		atTime(t, c, start.Add(time.Duration(i)*time.Minute))
		_, err = c.Store("fp", text, blob)
		require.NoError(t, err)
	}

	// Re-use "one" so it is no longer the oldest.
	atTime(t, c, start.Add(10*time.Minute))
	_, ok := c.Lookup("fp", "one", "mp3")
	require.True(t, ok)

	// A fourth entry pushes past the limit.
	atTime(t, c, start.Add(11*time.Minute))
	_, err = c.Store("fp", "four", blob)
	require.NoError(t, err)

	_, ok = c.Lookup("fp", "two", "mp3")
	require.False(t, ok, "least recently used must go first")

	for _, kept := range []string{"one", "three", "four"} {
		_, ok := c.Lookup("fp", kept, "mp3")
		require.True(t, ok, "%s should have survived", kept)
	}
}

func TestCacheUnboundedByDefault(t *testing.T) {
	c, err := NewCacheWith(CacheOptions{Dir: filepath.Join(t.TempDir(), "cache")})
	require.NoError(t, err)

	for _, text := range []string{"a", "b", "c"} {
		_, err = c.Store("fp", text, Audio{Data: make([]byte, 1000), Format: "mp3"})
		require.NoError(t, err)
	}
	for _, text := range []string{"a", "b", "c"} {
		_, ok := c.Lookup("fp", text, "mp3")
		require.True(t, ok, "zero limits must keep everything")
	}
}

func TestCacheIgnoresPartialWrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	c, err := NewCacheWith(CacheOptions{Dir: dir, MaxBytes: 100})
	require.NoError(t, err)

	// A temp file from an interrupted write must not be counted or evicted as
	// if it were a cache entry.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".tmp-leftover"), make([]byte, 500), 0o600))

	items, total, err := c.entries()
	require.NoError(t, err)
	require.Empty(t, items)
	require.Zero(t, total)
}

func TestNewCacheRejectsNegativeLimits(t *testing.T) {
	dir := t.TempDir()
	_, err := NewCacheWith(CacheOptions{Dir: dir, TTL: -time.Hour})
	require.Error(t, err)

	_, err = NewCacheWith(CacheOptions{Dir: dir, MaxBytes: -1})
	require.Error(t, err)
}
