// Package tgcall places Telegram calls and streams audio into them.
package tgcall

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

type terminalAuth struct{}

func (terminalAuth) Phone(ctx context.Context) (string, error) {
	return prompt(ctx, "Telegram phone: ")
}

func (terminalAuth) Password(ctx context.Context) (string, error) {
	return prompt(ctx, "Telegram 2FA password: ")
}

func (terminalAuth) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	return prompt(ctx, "Telegram code: ")
}

func (terminalAuth) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	answer, err := prompt(ctx, fmt.Sprintf("Accept Telegram terms of service? %s [y/N]: ", tos.Text))
	if err != nil {
		return err
	}
	if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
		return nil
	}
	return errors.New("terms of service rejected")
}

func (terminalAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up not implemented")
}

func prompt(ctx context.Context, text string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	fmt.Fprint(os.Stderr, text)
	value, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", errors.Wrap(err, "read prompt")
	}
	return strings.TrimSpace(value), nil
}
