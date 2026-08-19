package tgcall

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type peerKind string

const (
	peerUsername peerKind = "username"
	peerPhone    peerKind = "phone"
	peerDeeplink peerKind = "deeplink"
	peerID       peerKind = "id"
)

// peerTarget is a parsed -peer value.
type peerTarget struct {
	kind  peerKind
	value string

	userID     int64
	accessHash int64
	// hasAccessHash distinguishes an explicit hash from a zero one, and decides
	// whether the target resolves offline.
	hasAccessHash bool
}

// parsePeerTarget classifies a call target. Supported forms:
//
//	@durov, durov                username
//	+13115552368                 phone
//	t.me/durov, tg:resolve?...   deeplink
//	id:1234567                   user ID, needs a cached access hash
//	id:1234567:9876543210        user ID with explicit access hash
func parsePeerTarget(s string) (peerTarget, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return peerTarget{}, errors.New("peer is empty")
	}

	if rest, ok := strings.CutPrefix(s, "id:"); ok {
		return parsePeerID(rest)
	}
	if isDeeplink(s) {
		return peerTarget{kind: peerDeeplink, value: s}, nil
	}
	if isPhone(s) {
		return peerTarget{kind: peerPhone, value: s}, nil
	}

	username := strings.TrimPrefix(s, "@")
	if username == "" {
		return peerTarget{}, errors.Errorf("invalid username %q", s)
	}
	return peerTarget{kind: peerUsername, value: username}, nil
}

func parsePeerID(s string) (peerTarget, error) {
	idPart, hashPart, hasHash := strings.Cut(s, ":")

	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return peerTarget{}, errors.Wrapf(err, "parse user id %q", idPart)
	}
	if id == 0 {
		return peerTarget{}, errors.New("user id must not be zero")
	}
	t := peerTarget{kind: peerID, value: s, userID: id}

	if hasHash {
		hash, err := strconv.ParseInt(hashPart, 10, 64)
		if err != nil {
			return peerTarget{}, errors.Wrapf(err, "parse access hash %q", hashPart)
		}
		t.accessHash = hash
		t.hasAccessHash = true
	}
	return t, nil
}

func isDeeplink(s string) bool {
	lower := strings.ToLower(s)
	for _, prefix := range []string{"t.me/", "http://", "https://", "tg:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// isPhone matches gotd's own heuristic: usernames may not start with a digit,
// so a leading digit or plus means a phone number.
func isPhone(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c == '+' || (c >= '0' && c <= '9')
}

// resolvePeer looks up the configured target once per run and caches the
// resulting InputUser, which carries the account-scoped access hash.
func (c *Client) resolvePeer(ctx context.Context) error {
	t, err := parsePeerTarget(c.peer)
	if err != nil {
		return err
	}

	// An explicit access hash is already everything a call needs.
	if t.kind == peerID && t.hasAccessHash {
		c.peerUser = &tg.InputUser{UserID: t.userID, AccessHash: t.accessHash}
		c.lg.Info("Using peer id", zap.Int64("user_id", t.userID))
		return nil
	}

	user, err := c.resolveUser(ctx, t)
	if err != nil {
		return errors.Wrapf(err, "resolve peer %q as %s", c.peer, t.kind)
	}
	if _, isBot := user.ToBot(); isBot {
		return errors.Errorf("peer %q is a bot, calls require a user", c.peer)
	}
	// Neither the API docs nor TDLib forbid calling your own account, so this
	// is left to the server to accept or reject rather than blocked here.
	if user.Self() {
		c.lg.Warn("Peer is this account; Telegram may reject a call to self")
	}

	c.peerUser = user.InputUser()
	name, _ := user.Username()
	c.lg.Info("Resolved peer",
		zap.String("peer", c.peer),
		zap.String("kind", string(t.kind)),
		zap.String("username", name),
		zap.Int64("user_id", user.ID()),
	)
	return nil
}

func (c *Client) resolveUser(ctx context.Context, t peerTarget) (peers.User, error) {
	switch t.kind {
	case peerPhone:
		return c.peers.ResolvePhone(ctx, t.value)
	case peerID:
		return c.peers.ResolveUserID(ctx, t.userID)
	case peerUsername:
		return asUser(c.peers.ResolveDomain(ctx, t.value))
	case peerDeeplink:
		return asUser(c.peers.ResolveDeeplink(ctx, t.value))
	default:
		return peers.User{}, errors.Errorf("unsupported peer kind %q", t.kind)
	}
}

func asUser(p peers.Peer, err error) (peers.User, error) {
	if err != nil {
		return peers.User{}, err
	}
	user, ok := p.(peers.User)
	if !ok {
		return peers.User{}, errors.Errorf("resolved a %T, calls require a user", p)
	}
	return user, nil
}

func (c *Client) inputUser() (tg.InputUserClass, error) {
	if c.peerUser == nil {
		return nil, errors.New("peer is not resolved")
	}
	return c.peerUser, nil
}
