package tgcall

import (
	"context"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/calls"
	"github.com/pion/rtp"
	"go.uber.org/zap"
)

// Call is a placed outgoing call that the callee has already accepted.
type Call struct {
	conn *calls.Conn
	lg   *zap.Logger
}

// WriteRTP writes an Opus RTP packet to the call's audio track.
func (c *Call) WriteRTP(pkt *rtp.Packet) error {
	return c.conn.AudioTrack().WriteRTP(pkt)
}

// waitConnected blocks until ICE and DTLS are up.
//
// The timeout is not just a safety net: gotd replays an already-fired connect
// to a late [calls.Conn.OnConnected], but does not replay a disconnect, so a
// call that drops before we register would otherwise never signal.
func (c *Call) waitConnected(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := make(chan error, 1)
	trySend := func(err error) {
		select {
		case res <- err:
		default:
		}
	}
	c.conn.OnConnected(func() { trySend(nil) })
	c.conn.OnDisconnected(func() { trySend(errors.New("disconnected before connect")) })

	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "wait connected")
	case err := <-res:
		return err
	}
}

// Ring places a call to the configured peer and, once connected, invokes fn.
// Unanswered, declined and dropped calls are retried.
func (c *Client) Ring(ctx context.Context, fn func(ctx context.Context, call *Call) error) error {
	return c.retry(ctx, func(ctx context.Context, lg *zap.Logger) error {
		return c.ringOnce(ctx, lg, fn)
	})
}

// retry runs attempt up to c.attempts times, pausing c.retryDelay between tries.
// A cancelled ctx stops it immediately rather than burning the remaining attempts.
func (c *Client) retry(ctx context.Context, attempt func(context.Context, *zap.Logger) error) error {
	var lastErr error
	for n := 1; n <= c.attempts; n++ {
		lg := c.lg.With(zap.Int("attempt", n), zap.Int("attempts", c.attempts))

		if n > 1 {
			select {
			case <-ctx.Done():
				return errors.Wrap(ctx.Err(), "retry")
			case <-time.After(c.retryDelay):
			}
		}

		err := attempt(ctx, lg)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		lg.Warn("Call attempt failed", zap.Error(err))
		lastErr = err
	}
	return errors.Wrapf(lastErr, "all %d call attempts failed", c.attempts)
}

func (c *Client) ringOnce(ctx context.Context, lg *zap.Logger, fn func(context.Context, *Call) error) error {
	call, err := c.placeCall(ctx, lg)
	if err != nil {
		return err
	}
	// Cleanup must outlive a cancelled ctx, otherwise the call stays up.
	defer func() {
		if err := c.hangup(context.WithoutCancel(ctx)); err != nil {
			lg.Error("Hangup failed", zap.Error(err))
		}
	}()

	if err := call.waitConnected(ctx, c.connectTimeout); err != nil {
		return err
	}
	lg.Info("Call connected")

	return fn(ctx, call)
}

func (c *Client) placeCall(ctx context.Context, lg *zap.Logger) (*Call, error) {
	peer, err := c.inputUser()
	if err != nil {
		return nil, err
	}

	lg = lg.With(zap.String("peer", c.peer))
	lg.Info("Requesting call", zap.Duration("ring_timeout", c.ringTimeout))

	// calls.Request blocks until the callee accepts, so the ring timeout
	// bounds how long we let it keep ringing.
	ringCtx, cancel := context.WithTimeout(ctx, c.ringTimeout)
	defer cancel()

	conn, err := c.calls.Request(ringCtx, peer)
	if err != nil {
		return nil, errors.Wrap(err, "request call")
	}
	return &Call{conn: conn, lg: lg}, nil
}

func (c *Client) hangup(ctx context.Context) error {
	return c.calls.Discard(ctx, calls.DiscardHangup)
}
