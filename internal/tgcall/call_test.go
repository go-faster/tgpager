package tgcall

import (
	"context"
	"testing"
	"time"

	"github.com/go-faster/errors"
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
			err := c.retry(t.Context(), func(context.Context, *zap.Logger) error {
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
	err := c.retry(ctx, func(context.Context, *zap.Logger) error {
		calls++
		cancel()
		return errors.New("boom")
	})

	require.Error(t, err)
	require.Equal(t, 1, calls, "cancelled context must not burn remaining attempts")
}
