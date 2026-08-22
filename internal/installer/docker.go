package installer

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/host"
	"github.com/chr0nzz/traefik-stack/internal/render"
	"github.com/chr0nzz/traefik-stack/internal/state"
)

func (in *Installer) installDocker(ctx context.Context, a *answers.Answers, out *render.Output, st *state.State) error {
	in.UI.Step("Creating directory structure at " + a.Dir)
	privileged := !host.IsRoot() && host.NeedsPrivilege(a.Dir)
	if err := in.writeOutput(a, out, st, nil); err != nil {
		return err
	}
	if privileged {
		if err := host.Chown(a.Dir, host.CurrentUser()+":", true); err != nil {
			return err
		}
	}
	in.UI.Step("Pulling images")
	if err := in.compose(ctx, a.Dir, "pull"); err != nil {
		return err
	}
	in.UI.Step("Starting services")
	if err := in.compose(ctx, a.Dir, "up", "-d"); err != nil {
		return err
	}
	in.UI.OK("Services started")
	if a.Mode == answers.ModeFull && a.CrowdSec.Mode == answers.CrowdSecInstall && a.CrowdSec.MachineID != "" {
		in.registerCrowdSecMachine(ctx, a.CrowdSec.MachineID, a.Secrets[answers.SecretCrowdSecMachinePassword])
	}
	if a.Mode == answers.ModeFull || a.Mode == answers.ModeTMDocker {
		in.TempPassword = in.fetchPasswordDocker(ctx)
	}
	return nil
}

func (in *Installer) composeCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	full := append([]string{}, in.Compose[1:]...)
	full = append(full, "-f", filepath.Join(dir, "docker-compose.yml"))
	full = append(full, args...)
	cmd := host.Command(ctx, in.Compose[0], full...)
	cmd.Dir = dir
	return cmd
}

func (in *Installer) compose(ctx context.Context, dir string, args ...string) error {
	return host.Run(in.composeCmd(ctx, dir, args...))
}

func (in *Installer) composeOutput(ctx context.Context, dir string, args ...string) (string, error) {
	return host.Output(in.composeCmd(ctx, dir, args...))
}

func (in *Installer) ensureCompose() error {
	if len(in.Compose) > 0 {
		return nil
	}
	cmd, err := host.ComposeCommand()
	if err != nil {
		return err
	}
	in.Compose = cmd
	return nil
}

func (in *Installer) registerCrowdSecMachine(ctx context.Context, id, password string) {
	in.UI.Step("Registering CrowdSec machine for alerts")
	for i := 0; i < 30; i++ {
		if _, err := host.Output(host.DockerCommand(ctx, "exec", "crowdsec", "cscli", "lapi", "status")); err == nil {
			break
		}
		if !sleep(ctx, 2*time.Second) {
			return
		}
	}
	if _, err := host.Output(host.DockerCommand(ctx, "exec", "crowdsec", "cscli", "machines", "add", id, "--password", password, "--force")); err == nil {
		in.UI.OK(fmt.Sprintf("CrowdSec machine '%s' registered (enables the Alerts view)", id))
		return
	}
	in.UI.Warn("Could not auto-register the CrowdSec machine. Run manually once CrowdSec is up:")
	in.UI.Code(fmt.Sprintf("docker exec crowdsec cscli machines add %s --password '%s' --force", id, password))
}

var passwordRe = regexp.MustCompile(`AUTO-GENERATED[\s\S]{0,400}?Password:\s*(\S+)`)

func (in *Installer) fetchPasswordDocker(ctx context.Context) string {
	in.UI.Step("Waiting for Traefik Manager to generate temporary password")
	for i := 0; i < 20; i++ {
		logs, _ := host.Output(host.DockerCommand(ctx, "logs", "traefik-manager"))
		if m := passwordRe.FindStringSubmatch(logs); m != nil {
			in.UI.OK("Temporary password retrieved")
			return m[1]
		}
		if !sleep(ctx, 1500*time.Millisecond) {
			break
		}
	}
	in.UI.Warn("Could not retrieve temporary password. Check: docker logs traefik-manager")
	return ""
}

func (in *Installer) fetchPasswordNative(ctx context.Context) string {
	in.UI.Step("Waiting for Traefik Manager to generate temporary password")
	for i := 0; i < 20; i++ {
		logs, _ := host.Journalctl(ctx, "traefik-manager", 50)
		if m := passwordRe.FindStringSubmatch(logs); m != nil {
			in.UI.OK("Temporary password retrieved")
			return m[1]
		}
		if !sleep(ctx, 1500*time.Millisecond) {
			break
		}
	}
	in.UI.Warn("Could not retrieve temporary password. Check: sudo journalctl -u traefik-manager")
	return ""
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func containerStatus(ctx context.Context, name string) (status, image string) {
	out, err := host.Output(host.DockerCommand(ctx, "inspect", "--format", "{{.State.Status}} {{.Config.Image}}", name))
	if err != nil {
		return "absent", ""
	}
	parts := strings.SplitN(strings.TrimSpace(out), " ", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

func serviceNames(a *answers.Answers) []string {
	var names []string
	switch a.Mode {
	case answers.ModeFull:
		names = []string{"traefik", "traefik-manager"}
	case answers.ModeTMDocker:
		names = []string{"traefik-manager"}
	case answers.ModeAgentDocker:
		names = []string{"traefik-manager-agent"}
	case answers.ModeAgentDockerTraefik:
		names = []string{"traefik", "traefik-manager-agent"}
	}
	if a.Restart.Method == answers.RestartProxy && (a.Mode == answers.ModeAgentDockerTraefik || (!a.Mode.IsAgent() && a.Mounts.StaticConfig)) {
		names = append(names, "socket-proxy")
	}
	if a.CrowdSec.Mode == answers.CrowdSecInstall {
		names = append(names, "crowdsec")
	}
	return names
}
