package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chr0nzz/tm-cli/internal/ghrelease"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

func init() { register(newSelfUpdateCmd) }

func newSelfUpdateCmd() *cobra.Command {
	var version string
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update tm itself from GitHub releases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			u := ui.New()
			arch, err := host.Arch()
			if err != nil {
				return err
			}
			target := "latest"
			if version != "" {
				target = ghrelease.NormalizeVersion(version)
			}
			if target == "latest" {
				latest, err := ghrelease.LatestVersion(ctx, ghrelease.Repo)
				if err != nil {
					return fmt.Errorf("look up latest release: %w", err)
				}
				if strings.TrimPrefix(latest, "v") == build.Version {
					u.OK("tm " + build.Version + " is already the latest release")
					return nil
				}
				target = latest
				u.Info("latest release is " + latest + ", installed is " + build.Version)
			}
			exe := host.Executable()
			u.Step("Downloading tm " + target)
			if err := ghrelease.Download(ctx, ghrelease.Repo, target, "tm-linux-"+arch, exe, 0o755); err != nil {
				return err
			}
			u.OK("tm updated at " + exe)
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "install this release instead of the latest")
	return cmd
}
