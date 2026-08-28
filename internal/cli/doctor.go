package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chr0nzz/tm-cli/internal/ui"
)

func init() { register(newDoctorCmd) }

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the install for common problems",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			st, err := resolveState(cmd, u)
			if err != nil {
				return err
			}
			u.Step("Checking " + string(st.Mode) + " install")
			failed, err := newInstaller(u, false).Doctor(ctx, st)
			if err != nil {
				return err
			}
			u.Blank()
			if failed > 0 {
				u.Warn(fmt.Sprintf("%d check(s) failed", failed))
				return Exit(1)
			}
			u.OK("all checks passed")
			return nil
		},
	}
}
