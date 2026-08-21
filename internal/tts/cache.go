package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-faster/errors"
)

// Cache stores synthesized audio on disk, addressed by its content.
//
// Alertmanager resends a firing alert every repeat_interval, so without this
// the same sentence is re-synthesized, and re-billed, forever.
type Cache struct {
	dir      string
	ttl      time.Duration
	maxBytes int64
	now      func() time.Time
}

// CacheOptions bounds what the cache keeps.
//
// A pager runs for years and every distinct alert sentence leaves a file
// behind, so an unbounded cache is a slow disk leak. Zero disables either
// limit.
type CacheOptions struct {
	Dir      string
	TTL      time.Duration
	MaxBytes int64
}

// Cache defaults. Audio is small, so these are generous.
const (
	DefaultCacheTTL      = 30 * 24 * time.Hour
	DefaultCacheMaxBytes = 256 << 20
)

func NewCache(dir string) (*Cache, error) {
	return NewCacheWith(CacheOptions{Dir: dir})
}

func NewCacheWith(opts CacheOptions) (*Cache, error) {
	if opts.Dir == "" {
		return nil, errors.New("cache dir is required")
	}
	if opts.TTL < 0 {
		return nil, errors.Errorf("cache ttl must not be negative, got %s", opts.TTL)
	}
	if opts.MaxBytes < 0 {
		return nil, errors.Errorf("cache max bytes must not be negative, got %d", opts.MaxBytes)
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, errors.Wrapf(err, "create %s", opts.Dir)
	}
	return &Cache{
		dir:      opts.Dir,
		ttl:      opts.TTL,
		maxBytes: opts.MaxBytes,
		now:      time.Now,
	}, nil
}

// key identifies audio by everything that changes how it sounds.
func key(fingerprint, text string) string {
	sum := sha256.Sum256([]byte(fingerprint + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) path(fingerprint, text, format string) string {
	return filepath.Join(c.dir, key(fingerprint, text)+"."+format)
}

// Lookup reports the path of previously synthesized audio, and marks it as
// recently used so eviction sees it as live.
func (c *Cache) Lookup(fingerprint, text, format string) (string, bool) {
	path := c.path(fingerprint, text, format)

	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return "", false
	}
	now := c.now()
	if c.ttl > 0 && now.Sub(info.ModTime()) > c.ttl {
		_ = os.Remove(path)
		return "", false
	}
	// Touch so eviction is by last use rather than by creation.
	_ = os.Chtimes(path, now, now)
	return path, true
}

// Store writes audio and returns its path. The write is atomic, so a crash
// mid-write cannot leave a truncated file that later looks like a hit.
func (c *Cache) Store(fingerprint, text string, audio Audio) (string, error) {
	path := c.path(fingerprint, text, audio.Format)

	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return "", errors.Wrap(err, "create temp file")
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(audio.Data); err != nil {
		return "", errors.Wrap(err, "write audio")
	}
	if err := tmp.Close(); err != nil {
		return "", errors.Wrap(err, "close audio")
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", errors.Wrap(err, "commit audio")
	}

	// Best effort: a full disk is a reason to page, not to fail paging.
	_ = c.evict()
	return path, nil
}

type entry struct {
	path string
	size int64
	used time.Time
}

// evict drops expired entries, then the least recently used until the cache
// fits. It runs after a store, which is the only time the cache grows.
func (c *Cache) evict() error {
	if c.ttl == 0 && c.maxBytes == 0 {
		return nil
	}

	items, total, err := c.entries()
	if err != nil {
		return err
	}

	if c.maxBytes == 0 || total <= c.maxBytes {
		return nil
	}
	// Oldest use first.
	slices.SortFunc(items, func(a, b entry) int { return a.used.Compare(b.used) })
	for _, it := range items {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(it.path); err != nil {
			continue
		}
		total -= it.size
	}
	return nil
}

// entries lists live cache files, removing expired ones as it goes.
func (c *Cache) entries() (items []entry, total int64, _ error) {
	dir, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, 0, errors.Wrap(err, "read cache dir")
	}

	now := c.now()
	for _, d := range dir {
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(c.dir, d.Name())
		if c.ttl > 0 && now.Sub(info.ModTime()) > c.ttl {
			_ = os.Remove(path)
			continue
		}
		items = append(items, entry{path: path, size: info.Size(), used: info.ModTime()})
		total += info.Size()
	}
	return items, total, nil
}
