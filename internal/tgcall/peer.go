package tgcall

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// resolvePeer looks up the configured target once per run and caches the
// resulting InputUser, which carries the account-scoped access hash.
func (c *Client) resolvePeer(ctx context.Context) error {
	p, err := c.peers.Resolve(ctx, c.peer)
	if err != nil {
		return errors.Wrapf(err, "resolve peer %q", c.peer)
	}

	user, ok := p.(peers.User)
	if !ok {
		return errors.Errorf("peer %q is a %T, calls require a user", c.peer, p)
	}
	if _, isBot := user.ToBot(); isBot {
		return errors.Errorf("peer %q is a bot, calls require a user", c.peer)
	}

	c.peerUser = user.InputUser()
	c.lg.Info("Resolved peer",
		zap.String("peer", c.peer),
		zap.Int64("user_id", user.ID()),
	)
	return nil
}

func (c *Client) inputUser() (tg.InputUserClass, error) {
	if c.peerUser == nil {
		return nil, errors.New("peer is not resolved")
	}
	return c.peerUser, nil
}
