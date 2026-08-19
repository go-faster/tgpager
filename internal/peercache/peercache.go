// Package peercache persists resolved Telegram peers in bbolt, so access
// hashes survive restarts and a peer stays callable without re-resolving it.
package peercache

import (
	"context"
	"encoding/binary"
	"strconv"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/peers"
	bolt "go.etcd.io/bbolt"
)

var (
	peersBucket  = []byte("peers")
	phonesBucket = []byte("phones")
	metaBucket   = []byte("meta")

	contactsHashKey = []byte("contacts_hash")
)

// Storage is a bbolt-backed [peers.Storage].
type Storage struct {
	db    *bolt.DB
	owned bool
}

var _ peers.Storage = (*Storage)(nil)

// Open opens, creating if needed, a peer cache at path. The returned Storage
// owns the database and closes it on [Storage.Close].
func Open(path string) (*Storage, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, errors.Wrapf(err, "open %s", path)
	}
	return &Storage{db: db, owned: true}, nil
}

// New wraps an existing database. Closing the Storage does not close db.
func New(db *bolt.DB) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Close() error {
	if !s.owned {
		return nil
	}
	return s.db.Close()
}

func (s *Storage) Save(_ context.Context, key peers.Key, value peers.Value) error {
	return s.put(peersBucket, peerKey(key), i64b(value.AccessHash))
}

func (s *Storage) Find(_ context.Context, key peers.Key) (peers.Value, bool, error) {
	raw, err := s.get(peersBucket, peerKey(key))
	if err != nil || raw == nil {
		return peers.Value{}, false, err
	}
	hash, err := b64i(raw)
	if err != nil {
		return peers.Value{}, false, err
	}
	return peers.Value{AccessHash: hash}, true, nil
}

func (s *Storage) SavePhone(_ context.Context, phone string, key peers.Key) error {
	return s.put(phonesBucket, []byte(phone), peerKey(key))
}

func (s *Storage) FindPhone(ctx context.Context, phone string) (peers.Key, peers.Value, bool, error) {
	raw, err := s.get(phonesBucket, []byte(phone))
	if err != nil || raw == nil {
		return peers.Key{}, peers.Value{}, false, err
	}
	key, err := parsePeerKey(raw)
	if err != nil {
		return peers.Key{}, peers.Value{}, false, err
	}
	value, found, err := s.Find(ctx, key)
	if err != nil || !found {
		return peers.Key{}, peers.Value{}, false, err
	}
	return key, value, true, nil
}

func (s *Storage) GetContactsHash(_ context.Context) (int64, error) {
	raw, err := s.get(metaBucket, contactsHashKey)
	if err != nil || raw == nil {
		return 0, err
	}
	return b64i(raw)
}

func (s *Storage) SaveContactsHash(_ context.Context, hash int64) error {
	return s.put(metaBucket, contactsHashKey, i64b(hash))
}

func (s *Storage) put(bucket, key, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return errors.Wrapf(err, "create bucket %s", bucket)
		}
		return b.Put(key, value)
	})
}

func (s *Storage) get(bucket, key []byte) (value []byte, _ error) {
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		// bbolt values are only valid for the life of the transaction.
		if raw := b.Get(key); raw != nil {
			value = append([]byte(nil), raw...)
		}
		return nil
	})
	return value, err
}

// peerKey encodes a [peers.Key]. The prefix is a fixed-width kind tag, so it
// cannot collide with the decimal ID that follows.
func peerKey(k peers.Key) []byte {
	return []byte(k.Prefix + ":" + strconv.FormatInt(k.ID, 10))
}

func parsePeerKey(raw []byte) (peers.Key, error) {
	s := string(raw)
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != ':' {
			continue
		}
		id, err := strconv.ParseInt(s[i+1:], 10, 64)
		if err != nil {
			return peers.Key{}, errors.Wrapf(err, "parse peer key %q", s)
		}
		return peers.Key{Prefix: s[:i], ID: id}, nil
	}
	return peers.Key{}, errors.Errorf("malformed peer key %q", s)
}

func i64b(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func b64i(b []byte) (int64, error) {
	if len(b) != 8 {
		return 0, errors.Errorf("malformed int64 value of %d bytes", len(b))
	}
	return int64(binary.BigEndian.Uint64(b)), nil
}
