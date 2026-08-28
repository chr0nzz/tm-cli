package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/installer"
	"github.com/chr0nzz/tm-cli/internal/state"
	"github.com/chr0nzz/tm-cli/internal/ui"
	"github.com/chr0nzz/tm-cli/internal/wizard"
)

func init() {
	register(newReconfigureCmd)
	register(newAddCmd)
}

func newReconfigureCmd() *cobra.Command {
	var section string
	var yes bool
	cmd := &cobra.Command{
		Use:   "reconfigure",
		Short: "Re-run the wizard with the current settings and apply the changes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			if !ui.Interactive() {
				return errors.New("reconfigure needs a terminal")
			}
			tty, err := ui.OpenTTY()
			if err != nil {
				return err
			}
			return handleAbort(runReconfigure(ctx, u, newInstaller(u, yes), st, section, tty))
		},
	}
	cmd.Flags().StringVar(&section, "section", "", "open one section only (see tm reconfigure --list)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "overwrite files modified outside tm without asking")
	cmd.Flags().Bool("list", false, "list the sections for this install")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		list, _ := cmd.Flags().GetBool("list")
		if !list {
			return nil
		}
		u := ui.New()
		st, err := resolveState(cmd, u)
		if err != nil {
			return err
		}
		for _, s := range wizard.Sections(st.Mode) {
			fmt.Fprintf(os.Stdout, "%-12s %s\n", s.ID, s.Label)
		}
		return Exit(0)
	}
	return cmd
}

func newAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <component>",
		Short: "Add a component to an existing install (crowdsec)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			section := ""
			switch strings.ToLower(args[0]) {
			case "crowdsec":
				if st.Mode == answers.ModeTMDocker || st.Mode == answers.ModeTMNative {
					return fmt.Errorf("crowdsec is not available for %s installs", st.Mode)
				}
				section = "crowdsec"
			default:
				return fmt.Errorf("unknown component %q (available: crowdsec)", args[0])
			}
			if !ui.Interactive() {
				return errors.New("add needs a terminal")
			}
			tty, err := ui.OpenTTY()
			if err != nil {
				return err
			}
			return handleAbort(runReconfigure(ctx, u, newInstaller(u, false), st, section, tty))
		},
	}
}

func runReconfigure(ctx context.Context, u *ui.UI, inst *installer.Installer, st *state.State, section string, tty *os.File) error {
	if st.Adopted {
		u.Info("this install was adopted from an older setup; settings that could not be read from its files use defaults, check them in the review")
	}
	u.Step("Reconfigure " + string(st.Mode))
	err := inst.Reconfigure(ctx, st, func(a *answers.Answers) error {
		if section != "" {
			if err := wizard.RunSection(ctx, u, a, section, tty); err != nil {
				return err
			}
			return wizard.Review(ctx, u, a, tty)
		}
		return wizard.Run(ctx, u, a, tty)
	})
	if err != nil {
		return err
	}
	u.Done("Reconfigured")
	return inst.Status(ctx, st)
}
