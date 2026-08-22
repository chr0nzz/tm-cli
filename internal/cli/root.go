package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/chr0nzz/traefik-stack/internal/ui"
)

type Build struct {
	Version string
	Commit  string
	Date    string
}

var build Build

func Main(version, commit, date string) int {
	build = Build{Version: version, Commit: commit, Date: date}
	root := newRoot()
	if err := root.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			return ee.code
		}
		ui.New().Error(err.Error())
		return 1
	}
	return 0
}

type exitError struct {
	code int
}

func (e *exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func Exit(code int) error { return &exitError{code: code} }

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "tm",
		Short:         "Install and manage Traefik Manager, Traefik, and the TM agent",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: false,
		},
	}
	root.PersistentFlags().String("dir", "", "install directory (overrides auto-detection)")
	root.PersistentFlags().BoolVar(&allowUnverified, "allow-unverified", false, "install a downloaded binary even when the release publishes no SHA256SUMS")
	root.AddCommand(newVersionCmd())
	for _, f := range commandFactories {
		root.AddCommand(f())
	}
	return root
}

var commandFactories []func() *cobra.Command

func register(f func() *cobra.Command) {
	commandFactories = append(commandFactories, f)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tm version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := "tm " + build.Version
			if build.Commit != "" {
				s += " (" + build.Commit
				if build.Date != "" {
					s += ", " + build.Date
				}
				s += ")"
			}
			fmt.Fprintln(os.Stdout, s)
			return nil
		},
	}
}
