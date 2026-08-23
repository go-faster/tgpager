package tgcall

import (
	"context"
	"testing"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap/zaptest"
)

// textInvoker answers the single RPC a text message needs.
type textInvoker struct {
	sends int
	err   func(n int) error
	sent  []string
}

func (i *textInvoker) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	req, ok := input.(*tg.MessagesSendMessageRequest)
	if !ok {
		return errors.Errorf("unexpected request type %T", input)
	}
	i.sends++
	if i.err != nil {
		if err := i.err(i.sends); err != nil {
			return err
		}
	}
	i.sent = append(i.sent, req.Message)

	out, ok := output.(*tg.UpdatesBox)
	if !ok {
		return errors.Errorf("unexpected response type %T", output)
	}
	out.Updates = &tg.Updates{}
	return nil
}

func textClient(t *testing.T, inv tg.Invoker) *Client {
	t.Helper()
	c := New(1, "hash", "session.json",
		WithLogger(zaptest.NewLogger(t)),
		WithVoiceRetry(3, time.Millisecond),
	)
	m, err := newMetrics(metricnoop.NewMeterProvider())
	require.NoError(t, err)
	c.metrics = m
	c.sender = message.NewSender(tg.NewClient(inv))
	c.peerUser = &tg.InputUser{UserID: 42, AccessHash: 7}
	return c
}

func TestSendText(t *testing.T) {
	inv := &textInvoker{}
	c := textClient(t, inv)

	require.NoError(t, c.SendText(t.Context(), "firing alert, disk full"))
	require.Equal(t, []string{"firing alert, disk full"}, inv.sent)
}

func TestSendTextEmpty(t *testing.T) {
	inv := &textInvoker{}
	c := textClient(t, inv)

	require.Error(t, c.SendText(t.Context(), ""))
	require.Zero(t, inv.sends, "an empty message must not reach Telegram")
}

func TestSendTextUnresolvedPeer(t *testing.T) {
	c := textClient(t, &textInvoker{})
	c.peerUser = nil

	require.Error(t, c.SendText(t.Context(), "text"))
}

func TestSendTextRetries(t *testing.T) {
	inv := &textInvoker{err: func(n int) error {
		if n == 1 {
			return errors.New("transient")
		}
		return nil
	}}
	c := textClient(t, inv)

	require.NoError(t, c.SendText(t.Context(), "text"))
	require.Equal(t, 2, inv.sends)
}

// TestSendStopsOnPermanent covers the reason the retry budget exists at all:
// it must not be spent on a refusal that cannot change between attempts.
func TestSendStopsOnPermanent(t *testing.T) {
	for _, name := range []string{
		"VOICE_MESSAGES_FORBIDDEN",
		"CHAT_WRITE_FORBIDDEN",
		"USER_IS_BLOCKED",
		"PEER_ID_INVALID",
	} {
		t.Run(name, func(t *testing.T) {
			inv := &textInvoker{err: func(int) error { return tgerr.New(400, name) }}
			c := textClient(t, inv)

			require.ErrorContains(t, c.SendText(t.Context(), "text"), name)
			require.Equal(t, 1, inv.sends, "a permanent refusal must be tried once")
		})
	}
}

func TestSendVoiceStopsOnVoiceForbidden(t *testing.T) {
	inv := &invoker{sendErr: func(int) error {
		return tgerr.New(400, "VOICE_MESSAGES_FORBIDDEN")
	}}
	c := voiceClient(t, inv)

	err := c.SendVoice(t.Context(), voiceFile(t, 2048), time.Second)
	require.Error(t, err)
	require.True(t, VoiceForbidden(err), "the caller must be able to tell this apart")
	require.Equal(t, 1, inv.sends)
}

func TestVoiceForbidden(t *testing.T) {
	require.True(t, VoiceForbidden(tgerr.New(400, "VOICE_MESSAGES_FORBIDDEN")))
	require.True(t, VoiceForbidden(
		errors.Wrap(tgerr.New(400, "VOICE_MESSAGES_FORBIDDEN"), "send voice message")))
	require.False(t, VoiceForbidden(tgerr.New(400, "CHAT_WRITE_FORBIDDEN")))
	require.False(t, VoiceForbidden(errors.New("transient")))
	require.False(t, VoiceForbidden(nil))
}
