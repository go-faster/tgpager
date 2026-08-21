package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/go-faster/errors"
)

// Cache stores synthesized audio on disk, addressed by its content.
//
// Alertmanager resends a firing alert every repeat_interval, so without this
// the same sentence is re-synthesized, and re-billed, forever.
type Cache struct {
	dir string
}

func NewCache(dir string) (*Cache, error) {
	if dir == "" {
		return nil, errors.New("cache dir is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errors.Wrapf(err, "create %s", dir)
	}
	return &Cache{dir: dir}, nil
}

// key identifies audio by everything that changes how it sounds.
func key(fingerprint, text string) string {
	sum := sha256.Sum256([]byte(fingerprint + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) path(fingerprint, text, format string) string {
	return filepath.Join(c.dir, key(fingerprint, text)+"."+format)
}

// Lookup reports the path of previously synthesized audio.
func (c *Cache) Lookup(fingerprint, text, format string) (string, bool) {
	path := c.path(fingerprint, text, format)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, true
	}
	return "", false
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
	return path, nil
}
