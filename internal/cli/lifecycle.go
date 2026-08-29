package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/installer"
	"github.com/chr0nzz/tm-cli/internal/state"
	"github.com/chr0nzz/tm-cli/internal/ui"
	"path/filepath"
)

func init() {
	register(newStatusCmd)
	register(newUpdateCmd)
	register(newLogsCmd)
	register(func() *cobra.Command { return newControlCmd("restart", "Restart the install, or one service") })
	register(func() *cobra.Command { return newControlCmd("start", "Start the install, or one service") })
	register(func() *cobra.Command { return newControlCmd("stop", "Stop the install, or one service") })
	register(newPasswordCmd)
	register(newUninstallCmd)
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is installed, whether it runs, and how to reach it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			return newInstaller(u, false).Status(ctx, st)
		},
	}
}

func newUpdateCmd() *cobra.Command {
	var channel string
	var force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update to the latest images, code, or agent binary and restart",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			inst := newInstaller(u, false)
			inst.Force = force
			if channel != "" {
				if err := inst.SwitchChannel(ctx, st, channel); err != nil {
					return err
				}
			}
			if err := inst.Update(ctx, st); err != nil {
				return err
			}
			if err := st.Save(); err != nil {
				u.Warn("could not save tm state: " + err.Error())
			}
			return inst.Status(ctx, st)
		},
	}
	cmd.Flags().StringVar(&channel, "channel", "", "switch release channel: stable or beta")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall dependencies and rebuild assets even when nothing changed")
	_ = cmd.Flags().MarkHidden("channel")
	return cmd
}

func newLogsCmd() *cobra.Command {
	var noFollow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Show logs (follows by default)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			service := ""
			if len(args) == 1 {
				service = args[0]
			}
			err = newInstaller(u, false).Logs(ctx, st, service, !noFollow, lines)
			if ctx.Err() != nil {
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&noFollow, "no-follow", false, "print and exit instead of following")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of lines to show")
	return cmd
}

func newControlCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " [service]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			service := ""
			if len(args) == 1 {
				service = args[0]
			}
			return newInstaller(u, false).Control(ctx, st, action, service)
		},
	}
}

func newPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "password",
		Short: "Print the auto-generated temporary password from the logs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			pw, err := newInstaller(u, false).Password(ctx, st)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, pw)
			return nil
		},
	}
	cmd.AddCommand(newPasswordResetCmd())
	return cmd
}

func newPasswordResetCmd() *cobra.Command {
	var random, fromStdin, disableOTP, yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Set a new Traefik Manager password",
		Long: `Asks for a new password and sets it, so you can log straight in.

Runs Traefik Manager's own reset command, as the user that owns its settings file.
With --random it generates a temporary password instead and forces a change at next
login, which also leaves the /setup page open until a password is set there.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if random && fromStdin {
				return errors.New("--random cannot be combined with --stdin")
			}
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			inst := newInstaller(u, yes)
			opts := installer.PasswordResetOptions{Random: random, DisableOTP: disableOTP}
			if !random {
				ok, err := inst.SupportsChosenPassword(ctx, st)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("this Traefik Manager is too old to set a chosen password: run tm update first, or use tm password reset --random for a temporary one")
				}
				pw, err := collectPassword(ctx, fromStdin, yes)
				if err != nil {
					return handleAbort(err)
				}
				opts.Password = pw
			} else if !yes {
				ok, err := confirmReset(st.Mode)
				if err != nil || !ok {
					return handleAbort(err)
				}
			}
			if err := inst.ResetPassword(ctx, st, opts); err != nil {
				return err
			}
			u.Blank()
			if random {
				u.Warn("this form leaves /setup open until a password is set there, see https://traefik-manager.xyzlab.dev/reset-password")
			} else {
				u.OK("log in with the new password, no forced change and /setup stays closed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&random, "random", false, "generate a temporary password and force a change at next login")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "read the new password from standard input")
	cmd.Flags().BoolVar(&disableOTP, "disable-otp", false, "also turn two-factor authentication off")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func newUninstallCmd() *cobra.Command {
	var purge, yes, self bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the install (keeps data unless --purge)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			if self {
				return handleAbort(uninstallSelf(u, yes))
			}
			st, err := resolveState(cmd, u)
			if err != nil {
				if errors.Is(err, state.ErrNotFound) {
					return fmt.Errorf("%w. tm itself is removed with tm uninstall --self", err)
				}
				return err
			}
			where := installLocation(st)
			what := "services and tm-owned files"
			if purge {
				what = "services, configs, backups, certificates, data and volumes"
			}
			u.Warn(fmt.Sprintf("this removes the %s install at %s: %s", st.Mode, where, what))
			if !yes {
				if !ui.Interactive() {
					return errors.New("refusing to uninstall without --yes in a non-interactive session")
				}
				ok, err := confirm("Continue?", false)
				if err != nil {
					return handleAbort(err)
				}
				if !ok {
					return Exit(0)
				}
			}
			inst := newInstaller(u, yes)
			if err := inst.Uninstall(ctx, st, installer.UninstallOptions{Purge: purge}); err != nil {
				return err
			}
			u.Done("Uninstalled")
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also remove configs, backups, certificates, data dirs, and volumes")
	cmd.Flags().BoolVar(&self, "self", false, "remove the tm binary itself, leaving any installs running")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func uninstallSelf(u *ui.UI, yes bool) error {
	exe := host.Executable()
	if exe == "" {
		return errors.New("could not work out where tm is installed")
	}
	known, _ := state.Registry()
	u.Warn("this removes tm itself from " + exe)
	if len(known) > 0 {
		u.Blank()
		u.Info("these installs stay running and keep working, they just stop being managed by tm:")
		for _, p := range known {
			line := p
			if st, err := state.Load(p); err == nil {
				line = fmt.Sprintf("%-22s %s", st.Mode, installLocation(st))
			}
			u.Line("%s", ui.MutedStyle.Render(line))
		}
		u.Blank()
		u.Info("uninstall them first with tm uninstall if that is what you meant")
	}
	if !yes {
		if !ui.Interactive() {
			return errors.New("refusing to remove tm without --yes in a non-interactive session")
		}
		ok, err := confirm("Remove tm?", false)
		if err != nil {
			return err
		}
		if !ok {
			return Exit(0)
		}
	}
	if err := host.Remove(exe, false); err != nil {
		return fmt.Errorf("remove %s: %w", exe, err)
	}
	u.OK(exe + " removed")
	reg := state.RegistryPath()
	if host.Exists(reg) {
		if err := host.Remove(filepath.Dir(reg), true); err != nil {
			u.Warn("could not remove " + filepath.Dir(reg) + ": " + err.Error())
		} else {
			u.OK(filepath.Dir(reg) + " removed")
		}
	}
	u.Done("tm removed")
	return nil
}
