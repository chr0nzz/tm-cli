package installer

import (
	"context"
	"fmt"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/ghrelease"
	"github.com/chr0nzz/traefik-stack/internal/host"
	"github.com/chr0nzz/traefik-stack/internal/render"
	"github.com/chr0nzz/traefik-stack/internal/state"
)

const agentUnit = "tma"

func (in *Installer) installAgentBinary(ctx context.Context, a *answers.Answers, out *render.Output, st *state.State) error {
	if err := in.downloadAgentBinary(ctx); err != nil {
		return err
	}
	in.UI.Step("Installing systemd service")
	if err := in.writeOutput(a, out, st, nil); err != nil {
		return err
	}
	if err := host.Systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := host.Systemctl(ctx, "enable", "--now", agentUnit); err != nil {
		return err
	}
	in.UI.OK("Service enabled and started")
	return nil
}

func (in *Installer) downloadAgentBinary(ctx context.Context) error {
	arch, err := host.Arch()
	if err != nil {
		return err
	}
	asset := "tma-linux-" + arch
	in.UI.Step("Downloading TMA binary")
	if in.AllowUnverified {
		verified, err := ghrelease.DownloadAllowUnverified(ctx, ghrelease.AgentRepo, "latest", asset, answers.AgentBinaryPath, 0o755)
		if err != nil {
			return fmt.Errorf("download %s: %w", asset, err)
		}
		in.UI.OK(asset + " installed to " + answers.AgentBinaryPath)
		if !verified {
			in.UI.Warn("that release publishes no SHA256SUMS, so the binary was not checksum-verified")
		}
		return nil
	}
	if err := ghrelease.Download(ctx, ghrelease.AgentRepo, "latest", asset, answers.AgentBinaryPath, 0o755); err != nil {
		return fmt.Errorf("download %s: %w (pass --allow-unverified to install it without a checksum)", asset, err)
	}
	in.UI.OK(asset + " installed to " + answers.AgentBinaryPath)
	return nil
}

func (in *Installer) updateAgentBinary(ctx context.Context) error {
	if err := in.downloadAgentBinary(ctx); err != nil {
		return err
	}
	if err := host.Systemctl(ctx, "restart", agentUnit); err != nil {
		return err
	}
	in.UI.OK("Agent updated and restarted")
	return nil
}
