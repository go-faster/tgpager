package tgcall

import (
	"context"
	"testing"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func testClient(t *testing.T, attempts int) *Client {
	t.Helper()
	return New(1, "hash", "session.json",
		WithLogger(zaptest.NewLogger(t)),
		WithRetry(attempts, time.Millisecond),
	)
}

func TestClientRetry(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name      string
		attempts  int
		failUntil int
		wantCalls int
		wantErr   bool
	}{
		{"first try succeeds", 3, 0, 1, false},
		{"succeeds on last try", 3, 2, 3, false},
		{"exhausts attempts", 3, 99, 3, true},
		{"single attempt failure", 1, 99, 1, true},
		{"attempts floored to one", 0, 99, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(t, tt.attempts)

			var calls int
			err := c.retry(t.Context(), c.attempts, c.retryDelay, func(context.Context, *zap.Logger) error {
				calls++
				if calls <= tt.failUntil {
					return errBoom
				}
				return nil
			})

			require.Equal(t, tt.wantCalls, calls)
			if tt.wantErr {
				require.ErrorIs(t, err, errBoom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestClientRetryStopsOnCancel(t *testing.T) {
	c := testClient(t, 5)
	ctx, cancel := context.WithCancel(t.Context())

	var calls int
	err := c.retry(ctx, c.attempts, c.retryDelay, func(context.Context, *zap.Logger) error {
		calls++
		cancel()
		return errors.New("boom")
	})

	require.Error(t, err)
	require.Equal(t, 1, calls, "canceled context must not burn remaining attempts")
}

func TestRetryWait(t *testing.T) {
	const base = 10 * time.Second

	tests := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"plain error keeps the base delay", errors.New("boom"), base},
		{"nil keeps the base delay", nil, base},
		{"flood wait wins when longer", tgerr.New(420, "FLOOD_WAIT_30"), 30 * time.Second},
		{"base wins when flood wait is shorter", tgerr.New(420, "FLOOD_WAIT_5"), base},
		{"premium flood wait", tgerr.New(420, "FLOOD_PREMIUM_WAIT_60"), 60 * time.Second},
		{"wrapped flood wait", errors.Wrap(tgerr.New(420, "FLOOD_WAIT_30"), "send"), 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, retryWait(tt.err, base, zaptest.NewLogger(t)))
		})
	}
}
