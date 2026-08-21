package tgcall

import (
	"context"
	"os"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// maxVoiceBytes is the small-file upload limit.
//
// Above it gotd uploads an InputFileBig, which Telegram delivers as a plain
// document whatever attributes it carries: the voice message would silently
// arrive as a file attachment. Refusing is better than sending the wrong thing.
const maxVoiceBytes = constant.UploadMaxSmallSize

// SendVoice uploads path to the configured peer as a voice message.
//
// It is a record, not a page: unlike a call it survives being missed, and
// unlike a call it will not wake anybody.
func (c *Client) SendVoice(ctx context.Context, path string, dur time.Duration) (err error) {
	ctx, span := c.tracer.Start(ctx, "tgcall.SendVoice", trace.WithAttributes(
		attribute.Float64("tgpager.voice.duration", dur.Seconds()),
	))
	defer func() {
		c.metrics.voice.Add(ctx, 1, metric.WithAttributes(outcome(err)))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	peer, err := c.inputPeer()
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return errors.Wrap(err, "stat voice message")
	}
	if info.Size() > maxVoiceBytes {
		return errors.Errorf("voice message is %d bytes, over the %d byte small-file limit",
			info.Size(), maxVoiceBytes)
	}
	span.SetAttributes(attribute.Int64("tgpager.voice.bytes", info.Size()))

	// Built once rather than per attempt: the promise caches the uploaded
	// file, so a retry after a failed send does not re-upload it.
	upload := message.FromPath(path)

	return c.retry(ctx, func(ctx context.Context, lg *zap.Logger) error {
		lg.Info("Sending voice message",
			zap.Int64("bytes", info.Size()),
			zap.Duration("duration", dur),
		)
		return c.sendVoiceOnce(ctx, peer, upload, dur)
	})
}

func (c *Client) sendVoiceOnce(ctx context.Context, peer tg.InputPeerClass, upload message.UploadOption, dur time.Duration) error {
	b := c.sender.To(peer)

	file, err := b.Upload(upload).AsInputFile(ctx)
	if err != nil {
		return errors.Wrap(err, "upload voice message")
	}
	if _, ok := file.(*tg.InputFile); !ok {
		return errors.Errorf("uploaded as %T, a voice message must be a small file", file)
	}

	if _, err := b.Media(ctx, message.Voice(file).DurationSeconds(voiceSeconds(dur))); err != nil {
		return errors.Wrap(err, "send voice message")
	}
	return nil
}

// voiceSeconds rounds to at least one second: a voice message reporting zero
// renders as though it were empty.
func voiceSeconds(d time.Duration) int {
	if s := int(d.Round(time.Second).Seconds()); s > 0 {
		return s
	}
	return 1
}
