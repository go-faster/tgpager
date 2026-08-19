package tgcall

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePeerTarget(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  peerTarget
	}{
		{"username", "durov", peerTarget{kind: peerUsername, value: "durov"}},
		{"username at", "@durov", peerTarget{kind: peerUsername, value: "durov"}},
		{"username trimmed", "  @durov  ", peerTarget{kind: peerUsername, value: "durov"}},
		{"username with digits", "durov42", peerTarget{kind: peerUsername, value: "durov42"}},

		{"phone plus", "+13115552368", peerTarget{kind: peerPhone, value: "+13115552368"}},
		{"phone bare", "13115552368", peerTarget{kind: peerPhone, value: "13115552368"}},

		{"deeplink short", "t.me/durov", peerTarget{kind: peerDeeplink, value: "t.me/durov"}},
		{"deeplink https", "https://t.me/durov", peerTarget{kind: peerDeeplink, value: "https://t.me/durov"}},
		{"deeplink tg", "tg:resolve?domain=durov", peerTarget{kind: peerDeeplink, value: "tg:resolve?domain=durov"}},

		{"id", "id:1234567", peerTarget{kind: peerID, value: "1234567", userID: 1234567}},
		{"id negative", "id:-100500", peerTarget{kind: peerID, value: "-100500", userID: -100500}},
		{
			"id with access hash",
			"id:1234567:9876543210",
			peerTarget{
				kind: peerID, value: "1234567:9876543210",
				userID: 1234567, accessHash: 9876543210, hasAccessHash: true,
			},
		},
		{
			"id with zero access hash",
			"id:1234567:0",
			peerTarget{kind: peerID, value: "1234567:0", userID: 1234567, hasAccessHash: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePeerTarget(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParsePeerTargetErrors(t *testing.T) {
	for _, input := range []string{
		"",
		"   ",
		"@",
		"id:",
		"id:abc",
		"id:0",
		"id:123:abc",
	} {
		t.Run(input, func(t *testing.T) {
			_, err := parsePeerTarget(input)
			require.Error(t, err)
		})
	}
}
