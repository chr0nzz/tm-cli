package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

var (
	lookPath = exec.LookPath
	geteuid  = os.Geteuid
)

func IsRoot() bool { return geteuid() == 0 }

func CurrentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}

func HasCommand(name string) bool {
	_, err := lookPath(name)
	return err == nil
}

func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func Privileged(ctx context.Context, name string, args ...string) *exec.Cmd {
	if IsRoot() {
		return Command(ctx, name, args...)
	}
	if !HasCommand("sudo") {
		cmd := Command(ctx, name, args...)
		cmd.Err = fmt.Errorf("sudo is required for %s", filepath.Base(name))
		return cmd
	}
	return Command(ctx, "sudo", append([]string{name}, args...)...)
}

func RunAs(ctx context.Context, user, name string, args ...string) *exec.Cmd {
	if user == "" || user == CurrentUser() {
		return Command(ctx, name, args...)
	}
	if !HasCommand("sudo") {
		cmd := Command(ctx, name, args...)
		cmd.Err = fmt.Errorf("sudo is required to run %s as %s", filepath.Base(name), user)
		return cmd
	}
	return Command(ctx, "sudo", append([]string{"-u", user, name}, args...)...)
}

func Run(cmd *exec.Cmd) error {
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return wrapErr(cmd, err, "")
	}
	return nil
}

func Output(cmd *exec.Cmd) (string, error) {
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, wrapErr(cmd, err, s)
	}
	return s, nil
}

func wrapErr(cmd *exec.Cmd, err error, out string) error {
	name := displayName(cmd)
	if cmd.Err != nil && errors.Is(err, cmd.Err) {
		return err
	}
	if out != "" {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return fmt.Errorf("%s: %w", name, err)
}

func displayName(cmd *exec.Cmd) string {
	args := cmd.Args
	if len(args) == 0 {
		return filepath.Base(cmd.Path)
	}
	if filepath.Base(args[0]) != "sudo" {
		return filepath.Base(args[0])
	}
	for _, a := range args[1:] {
		if !strings.HasPrefix(a, "-") {
			return filepath.Base(a)
		}
	}
	return "sudo"
}

func Executable() string {
	p, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func SudoPreflight(ctx context.Context, reasons []string, log func(string)) error {
	if IsRoot() {
		return nil
	}
	if !HasCommand("sudo") {
		if len(reasons) > 0 {
			return fmt.Errorf("sudo is required for: %s", strings.Join(reasons, ", "))
		}
		return errors.New("sudo is required")
	}
	if log != nil {
		if len(reasons) > 0 {
			log("this install uses sudo for: " + strings.Join(reasons, ", "))
		} else {
			log("this install uses sudo for privileged steps")
		}
	}
	if err := Run(Command(ctx, "sudo", "-v")); err != nil {
		return fmt.Errorf("sudo authentication failed: %w", err)
	}
	return nil
}
