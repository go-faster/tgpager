package tgcall

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"
)

func TestBotSessionError(t *testing.T) {
	for _, tt := range []struct {
		name    string
		user    *tg.User
		wantErr bool
	}{
		{"Nil", nil, false},
		{"User", &tg.User{ID: 1}, false},
		{"Bot", &tg.User{ID: 1, Bot: true}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := botSessionError("session.json", tt.user)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, "bots cannot place calls")
			require.ErrorContains(t, err, "session.json")
		})
	}
}

func TestWithCalls(t *testing.T) {
	require.True(t, New(1, "hash", "session.json").callsEnabled,
		"calls must be enabled unless the caller says otherwise")
	require.False(t, New(1, "hash", "session.json", WithCalls(false)).callsEnabled)
}

func TestVerifySelfSkippedWithoutCalls(t *testing.T) {
	// No client is built, so a nil dereference is the assertion: the check
	// must not reach the network when calls are off.
	c := New(1, "hash", "session.json", WithCalls(false))
	require.NoError(t, c.verifySelf(t.Context()))
}
