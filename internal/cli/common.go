package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/host"
	"github.com/chr0nzz/traefik-stack/internal/installer"
	"github.com/chr0nzz/traefik-stack/internal/state"
	"github.com/chr0nzz/traefik-stack/internal/ui"
)

var allowUnverified bool

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func adoptDockerSudo() {
	if host.DockerInstalled() && !host.DockerRunning() && !host.InDockerGroup() && !host.IsRoot() && host.DockerRunsWithSudo() {
		host.UseDockerSudo(true)
	}
}

func newInstaller(u *ui.UI, yes bool) *installer.Installer {
	adoptDockerSudo()
	in := installer.New(u, build.Version)
	in.Yes = yes
	in.AllowUnverified = allowUnverified
	in.Confirm = func(prompt string, def bool) (bool, error) {
		if !ui.Interactive() {
			return def, nil
		}
		return confirm(prompt, def)
	}
	return in
}

func confirm(prompt string, def bool) (bool, error) {
	tty, err := ui.OpenTTY()
	if err != nil {
		return def, nil
	}
	v := def
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(prompt).Affirmative("Yes").Negative("No").Value(&v),
	)).WithTheme(ui.Theme()).WithInput(tty).WithOutput(os.Stdout).WithShowHelp(false)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, installer.ErrAborted
		}
		return false, err
	}
	return v, nil
}

func installLocation(st *state.State) string {
	switch st.Mode {
	case answers.ModeAgentBinary:
		return answers.AgentBinaryPath + " and its systemd unit"
	case answers.ModeTMNative:
		if st.Dir != "" {
			return st.Dir
		}
		return st.Answers.Native.InstallDir
	default:
		return st.Dir
	}
}

func resolveState(cmd *cobra.Command, u *ui.UI) (*state.State, error) {
	dir, _ := cmd.Flags().GetString("dir")
	st, err := state.Resolve(dir)
	if err != nil {
		var amb *state.AmbiguousError
		if errors.As(err, &amb) {
			u.Error("several installs are known, pick one with --dir:")
			for _, c := range amb.Candidates {
				u.Line("%s", ui.MutedStyle.Render(fmt.Sprintf("%-22s %s", c.Mode, c.Dir)))
			}
			return nil, Exit(1)
		}
		if errors.Is(err, state.ErrNotFound) {
			return nil, fmt.Errorf("no tm install found here: run tm install, or pass --dir <install dir>")
		}
		return nil, err
	}
	return st, nil
}

func handleAbort(err error) error {
	if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, installer.ErrAborted) || errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr)
		return Exit(130)
	}
	return err
}
