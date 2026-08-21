package tgcall

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap/zaptest"
)

// invoker answers the two RPCs a voice message needs: the part upload and the
// send itself.
type invoker struct {
	sends   int
	parts   int
	sendErr func(n int) error
	media   []*tg.MessagesSendMediaRequest
}

func (i *invoker) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	if _, ok := input.(*tg.UploadSaveFilePartRequest); ok {
		i.parts++
	}
	if req, ok := input.(*tg.MessagesSendMediaRequest); ok {
		i.sends++
		if i.sendErr != nil {
			if err := i.sendErr(i.sends); err != nil {
				return err
			}
		}
		i.media = append(i.media, req)
	}

	switch out := output.(type) {
	case *tg.BoolBox:
		out.Bool = &tg.BoolTrue{}
		return nil
	case *tg.UpdatesBox:
		out.Updates = &tg.Updates{}
		return nil
	default:
		return errors.Errorf("unexpected response type %T", output)
	}
}

func voiceClient(t *testing.T, inv tg.Invoker) *Client {
	t.Helper()
	c := New(1, "hash", "session.json",
		WithLogger(zaptest.NewLogger(t)),
		WithRetry(3, time.Millisecond),
		WithVoiceRetry(3, time.Millisecond),
	)
	m, err := newMetrics(metricnoop.NewMeterProvider())
	require.NoError(t, err)
	c.metrics = m
	c.sender = message.NewSender(tg.NewClient(inv))
	c.peerUser = &tg.InputUser{UserID: 42, AccessHash: 7}
	return c
}

func voiceFile(t *testing.T, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "voice.ogg")
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o600))
	return path
}

func TestSendVoice(t *testing.T) {
	inv := &invoker{}
	c := voiceClient(t, inv)

	require.NoError(t, c.SendVoice(t.Context(), voiceFile(t, 2048), 7*time.Second))
	require.Len(t, inv.media, 1)

	media, ok := inv.media[0].Media.(*tg.InputMediaUploadedDocument)
	require.True(t, ok, "voice message must be an uploaded document")
	require.Equal(t, "audio/ogg", media.MimeType)

	// A big file arrives as a plain attachment whatever the attributes say.
	_, small := media.File.(*tg.InputFile)
	require.True(t, small, "must upload as a small file, got %T", media.File)

	var attr *tg.DocumentAttributeAudio
	for _, a := range media.Attributes {
		if audio, ok := a.(*tg.DocumentAttributeAudio); ok {
			attr = audio
		}
	}
	require.NotNil(t, attr, "missing audio attribute")
	require.True(t, attr.Voice, "without this flag it is a file attachment, not a voice message")
	require.Equal(t, 7, attr.Duration)

	peer, ok := inv.media[0].Peer.(*tg.InputPeerUser)
	require.True(t, ok)
	require.Equal(t, int64(42), peer.UserID)
	require.Equal(t, int64(7), peer.AccessHash)
}

func TestSendVoiceRetries(t *testing.T) {
	inv := &invoker{sendErr: func(n int) error {
		if n < 3 {
			return errors.New("flood wait")
		}
		return nil
	}}
	c := voiceClient(t, inv)

	require.NoError(t, c.SendVoice(t.Context(), voiceFile(t, 2048), time.Second))
	require.Equal(t, 3, inv.sends)
	require.Equal(t, 1, inv.parts, "a retried send must not re-upload the file")
}

func TestSendVoiceGivesUp(t *testing.T) {
	inv := &invoker{sendErr: func(int) error { return errors.New("nope") }}
	c := voiceClient(t, inv)

	require.Error(t, c.SendVoice(t.Context(), voiceFile(t, 2048), time.Second))
	require.Equal(t, 3, inv.sends)
}

func TestSendVoiceRejectsBigFile(t *testing.T) {
	inv := &invoker{}
	c := voiceClient(t, inv)

	err := c.SendVoice(t.Context(), voiceFile(t, maxVoiceBytes+1), time.Second)
	require.Error(t, err)
	require.Empty(t, inv.media, "must refuse before sending the wrong thing")
}

func TestSendVoiceMissingFile(t *testing.T) {
	c := voiceClient(t, &invoker{})
	require.Error(t, c.SendVoice(t.Context(), filepath.Join(t.TempDir(), "nope.ogg"), time.Second))
}

func TestSendVoiceUnresolvedPeer(t *testing.T) {
	c := voiceClient(t, &invoker{})
	c.peerUser = nil
	require.Error(t, c.SendVoice(t.Context(), voiceFile(t, 16), time.Second))
}

func TestVoiceSeconds(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want int
	}{
		{"rounds down", 7400 * time.Millisecond, 7},
		{"rounds up", 7600 * time.Millisecond, 8},
		{"never zero", 100 * time.Millisecond, 1},
		{"never negative", -time.Second, 1},
		{"exact", 12 * time.Second, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, voiceSeconds(tt.in))
		})
	}
}

func TestInputPeer(t *testing.T) {
	tests := []struct {
		name    string
		user    tg.InputUserClass
		want    tg.InputPeerClass
		wantErr bool
	}{
		{
			name: "user",
			user: &tg.InputUser{UserID: 1, AccessHash: 2},
			want: &tg.InputPeerUser{UserID: 1, AccessHash: 2},
		},
		{
			name: "self",
			user: &tg.InputUserSelf{},
			want: &tg.InputPeerSelf{},
		},
		{
			name: "from message",
			user: &tg.InputUserFromMessage{Peer: &tg.InputPeerSelf{}, MsgID: 3, UserID: 4},
			want: &tg.InputPeerUserFromMessage{Peer: &tg.InputPeerSelf{}, MsgID: 3, UserID: 4},
		},
		{
			name:    "unresolved",
			user:    nil,
			wantErr: true,
		},
		{
			name:    "empty",
			user:    &tg.InputUserEmpty{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{peerUser: tt.user}
			got, err := c.inputPeer()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
