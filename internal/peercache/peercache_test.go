package peercache

import (
	"path/filepath"
	"testing"

	"github.com/gotd/td/telegram/peers"
	"github.com/stretchr/testify/require"
)

func testStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "peers.bolt"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}

func TestStorageSaveFind(t *testing.T) {
	ctx := t.Context()
	s := testStorage(t)
	key := peers.Key{Prefix: "users", ID: 1234567}

	_, found, err := s.Find(ctx, key)
	require.NoError(t, err)
	require.False(t, found, "empty storage must not report a hit")

	require.NoError(t, s.Save(ctx, key, peers.Value{AccessHash: -9876543210}))

	value, found, err := s.Find(ctx, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(-9876543210), value.AccessHash)

	_, found, err = s.Find(ctx, peers.Key{Prefix: "channels", ID: 1234567})
	require.NoError(t, err)
	require.False(t, found, "prefix must be part of the key")
}

func TestStoragePhone(t *testing.T) {
	ctx := t.Context()
	s := testStorage(t)
	key := peers.Key{Prefix: "users", ID: 42}

	_, _, found, err := s.FindPhone(ctx, "+13115552368")
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, s.Save(ctx, key, peers.Value{AccessHash: 777}))
	require.NoError(t, s.SavePhone(ctx, "+13115552368", key))

	gotKey, gotValue, found, err := s.FindPhone(ctx, "+13115552368")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, key, gotKey)
	require.Equal(t, int64(777), gotValue.AccessHash)
}

func TestStorageContactsHash(t *testing.T) {
	ctx := t.Context()
	s := testStorage(t)

	hash, err := s.GetContactsHash(ctx)
	require.NoError(t, err)
	require.Zero(t, hash)

	require.NoError(t, s.SaveContactsHash(ctx, 1234))
	hash, err = s.GetContactsHash(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1234), hash)
}

func TestStoragePersistsAcrossReopen(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "peers.bolt")
	key := peers.Key{Prefix: "users", ID: 1}

	first, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, first.Save(ctx, key, peers.Value{AccessHash: 99}))
	require.NoError(t, first.Close())

	second, err := Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, second.Close()) }()

	value, found, err := second.Find(ctx, key)
	require.NoError(t, err)
	require.True(t, found, "access hash must survive a restart")
	require.Equal(t, int64(99), value.AccessHash)
}

func TestPeerKeyRoundTrip(t *testing.T) {
	for _, key := range []peers.Key{
		{Prefix: "users", ID: 1},
		{Prefix: "users", ID: -100500},
		{Prefix: "", ID: 7},
	} {
		got, err := parsePeerKey(peerKey(key))
		require.NoError(t, err)
		require.Equal(t, key, got)
	}
}
