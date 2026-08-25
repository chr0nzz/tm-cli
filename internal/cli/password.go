package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/ui"
)

const (
	bom              = "\ufeff"
	minPasswordChars = 8
	maxPasswordBytes = 72
)

func passwordError(pw string) error {
	if pw == "" {
		return errors.New("a password is required")
	}
	if len(pw) < minPasswordChars {
		return fmt.Errorf("password must be at least %d characters", minPasswordChars)
	}
	if len(pw) > maxPasswordBytes {
		return fmt.Errorf("password must be %d bytes or fewer, and accented or non-Latin characters take more than one byte each", maxPasswordBytes)
	}
	return nil
}

func readPasswordStdin(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	pw := strings.TrimSuffix(strings.SplitN(strings.TrimPrefix(string(data), bom), "\n", 2)[0], "\r")
	if err := passwordError(pw); err != nil {
		return "", err
	}
	return pw, nil
}

func collectPassword(ctx context.Context, fromStdin, yes bool) (string, error) {
	if fromStdin {
		return readPasswordStdin(os.Stdin)
	}
	if !ui.Interactive() {
		return "", errors.New("no terminal available: pass --stdin to read the password from standard input, or --random for a temporary one")
	}
	tty, err := ui.OpenTTY()
	if err != nil {
		return "", err
	}
	var pw, confirm string
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("New password").EchoMode(huh.EchoModePassword).Value(&pw).
			Description("At least 8 characters. It is not shown or stored by tm.").
			Validate(passwordError),
		huh.NewInput().Title("Confirm password").EchoMode(huh.EchoModePassword).Value(&confirm).
			Validate(func(s string) error {
				if s != pw {
					return errors.New("the two entries do not match")
				}
				return nil
			}),
	)).WithTheme(ui.Theme()).WithInput(tty).WithOutput(os.Stdout).WithShowHelp(false)
	if err := form.RunWithContext(ctx); err != nil {
		return "", err
	}
	return pw, nil
}

func confirmReset(mode answers.Mode) (bool, error) {
	if !ui.Interactive() {
		return false, errors.New("refusing to reset without --yes in a non-interactive session")
	}
	return confirm("This replaces the current Traefik Manager password. Continue?", false)
}
