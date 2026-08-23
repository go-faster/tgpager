package tgcall

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/gotd/td/tgerr"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// SendText sends the alert as a plain text message.
//
// It is the lowest common denominator: it needs no upload, no rendering and
// no permission beyond being able to message the peer at all.
func (c *Client) SendText(ctx context.Context, text string) (err error) {
	ctx, span := c.tracer.Start(ctx, "tgcall.SendText")
	defer func() {
		c.metrics.text.Add(ctx, 1, metric.WithAttributes(outcome(err)))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if text == "" {
		return errors.New("text is empty")
	}
	peer, err := c.inputPeer()
	if err != nil {
		return err
	}

	// The text itself never reaches a span or a log: alert labels routinely
	// carry hostnames and customer identifiers.
	return c.retry(ctx, c.voiceAttempts, c.voiceRetryDelay, func(ctx context.Context, lg *zap.Logger) error {
		lg.Info("Sending text message", zap.Int("bytes", len(text)))
		if _, err := c.sender.To(peer).Text(ctx, text); err != nil {
			return errors.Wrap(err, "send text message")
		}
		return nil
	})
}

// VoiceForbidden reports whether the peer's privacy settings refuse voice
// messages, which only a Telegram Premium account can set.
//
// Worth distinguishing: it is a decision by the callee rather than a fault,
// it will not change by trying again, and the text fallback exists for it.
func VoiceForbidden(err error) bool {
	return tgerr.Is(err, "VOICE_MESSAGES_FORBIDDEN")
}

// permanent reports whether retrying err can only fail the same way.
//
// Retrying a refusal wastes the send budget and buries the real reason under
// identical log lines.
func permanent(err error) bool {
	return VoiceForbidden(err) ||
		tgerr.Is(err,
			"CHAT_WRITE_FORBIDDEN",
			"USER_IS_BLOCKED",
			"PEER_ID_INVALID",
			"YOU_BLOCKED_USER",
		)
}
