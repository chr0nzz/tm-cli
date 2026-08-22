package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/chr0nzz/traefik-stack/internal/installer"
	"github.com/chr0nzz/traefik-stack/internal/ui"
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
	return &cobra.Command{
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
			if err := inst.Update(ctx, st); err != nil {
				return err
			}
			return inst.Status(ctx, st)
		},
	}
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
	return &cobra.Command{
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
}

func newUninstallCmd() *cobra.Command {
	var purge, yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the install (keeps data unless --purge)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
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
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
