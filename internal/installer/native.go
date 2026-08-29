package installer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/render"
	"github.com/chr0nzz/tm-cli/internal/state"
)

const (
	nativeRepo      = "https://github.com/chr0nzz/traefik-manager.git"
	nativeUser      = "traefik-manager"
	nativeUnit      = "traefik-manager"
	restartPathUnit = "traefik-restart.path"
)

func (in *Installer) cloneOrPullNative(ctx context.Context, a *answers.Answers) error {
	dir := a.Native.InstallDir
	if host.IsDir(dir) {
		in.UI.Warn(dir + " already exists. Pulling latest changes.")
		if err := host.Run(host.Command(ctx, "git", "-C", dir, "pull")); err != nil {
			return err
		}
	} else {
		if err := host.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return err
		}
		if host.NeedsPrivilege(filepath.Dir(dir)) {
			if err := host.Run(host.Privileged(ctx, "git", "clone", "--branch", a.GitBranch(), nativeRepo, dir)); err != nil {
				return err
			}
			if err := host.Chown(dir, host.CurrentUser(), true); err != nil {
				return err
			}
		} else if err := host.Run(host.Command(ctx, "git", "clone", "--branch", a.GitBranch(), nativeRepo, dir)); err != nil {
			return err
		}
	}
	in.UI.OK("Repository cloned to " + dir)
	return nil
}

func (in *Installer) ensureNativeUser(ctx context.Context) error {
	if host.UserExists(nativeUser) {
		in.UI.OK("System user " + nativeUser + " already exists")
		return nil
	}
	if err := host.AddSystemUser(ctx, nativeUser); err != nil {
		return err
	}
	in.UI.OK("System user " + nativeUser + " created")
	return nil
}

func (in *Installer) installNative(ctx context.Context, a *answers.Answers, out *render.Output, st *state.State) error {
	in.UI.Step("Installing Traefik Manager")
	dir := a.Native.InstallDir
	if err := in.cloneOrPullNative(ctx, a); err != nil {
		return err
	}
	if err := in.buildNativeVenv(ctx, dir); err != nil {
		return err
	}
	if err := host.MkdirAll(filepath.Join(a.Native.DataDir, "backups"), 0o755); err != nil {
		return err
	}
	in.UI.OK("Data directories created at " + a.Native.DataDir)
	if a.Mounts.StaticConfig && a.Restart.Method == answers.RestartPoisonPill {
		signalDir := filepath.Dir(a.Restart.SignalFile)
		if err := host.MkdirAll(signalDir, 0o755); err != nil {
			return err
		}
		in.UI.OK("Signal directory created at " + signalDir)
	}
	if err := in.installCrowdSecPackage(ctx, a); err != nil {
		return err
	}
	if a.Native.ServiceUser {
		if err := in.ensureNativeUser(ctx); err != nil {
			return err
		}
		if err := host.Chown(dir, nativeUser+":", true); err != nil {
			return err
		}
		if err := host.Chown(a.Native.DataDir, nativeUser+":", true); err != nil {
			return err
		}
		if a.Mounts.StaticConfig && a.Restart.Method == answers.RestartPoisonPill {
			if err := host.Chown(filepath.Dir(a.Restart.SignalFile), nativeUser+":", true); err != nil {
				return err
			}
		}
		if a.Mounts.StaticConfig && a.Restart.Method == answers.RestartSocket {
			if err := host.AddUserToGroup(ctx, nativeUser, "docker"); err != nil {
				in.UI.Warn("could not add " + nativeUser + " to the docker group: " + err.Error())
			}
		}
	}
	if err := in.writeOutput(a, out, st, nil); err != nil {
		return err
	}
	in.registerCrowdSecNative(ctx, a)
	in.reloadCrowdSec(ctx, a)
	if err := host.Systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if a.Mounts.StaticConfig && a.Restart.TraefikSystemd {
		if err := host.Systemctl(ctx, "enable", "--now", restartPathUnit); err != nil {
			return err
		}
		in.UI.OK("Traefik restart watcher enabled (" + restartPathUnit + ")")
	}
	if err := host.Systemctl(ctx, "enable", "--now", nativeUnit); err != nil {
		return err
	}
	in.UI.OK("Service enabled and started")
	in.TempPassword = in.fetchPasswordNative(ctx)
	return nil
}

func (in *Installer) buildNativeVenv(ctx context.Context, dir string) error {
	venv := filepath.Join(dir, "venv")
	if err := host.Run(host.Command(ctx, "python3", "-m", "venv", venv)); err != nil {
		return fmt.Errorf("create venv: %w", err)
	}
	if err := host.Run(host.Command(ctx, filepath.Join(venv, "bin", "pip"), "install", "-q", "-r", filepath.Join(dir, "requirements.txt"), "gunicorn")); err != nil {
		return fmt.Errorf("install python dependencies: %w", err)
	}
	in.UI.OK("Python dependencies installed")
	if err := host.Run(host.Command(ctx, "bash", filepath.Join(dir, "scripts", "setup-assets.sh"))); err != nil {
		return fmt.Errorf("build assets: %w", err)
	}
	in.UI.OK("Vendor assets and Tailwind CSS built")
	return nil
}

func (in *Installer) updateNative(ctx context.Context, a *answers.Answers) error {
	dir := a.Native.InstallDir
	in.UI.Step("Updating Traefik Manager in " + dir)
	owner := host.CurrentUser()
	if a.Native.ServiceUser {
		owner = nativeUser
	}
	home := "HOME=" + dir
	branch := a.GitBranch()
	before := in.gitHead(ctx, owner, home, dir)
	pull := [][]string{
		{"env", home, "git", "-C", dir, "fetch", "origin", branch},
		{"env", home, "git", "-C", dir, "checkout", branch},
		{"env", home, "git", "-C", dir, "pull"},
	}
	for _, step := range pull {
		if err := host.Run(host.RunAs(ctx, owner, step[0], step[1:]...)); err != nil {
			return err
		}
	}
	after := in.gitHead(ctx, owner, home, dir)
	if !in.Force && before != "" && before == after {
		in.UI.OK("Traefik Manager is already up to date, nothing to rebuild")
		in.UI.Info("run tm update --force to reinstall the dependencies and rebuild the assets anyway")
		return nil
	}
	build := [][]string{
		{"env", home, filepath.Join(dir, "venv", "bin", "pip"), "install", "-q", "--no-cache-dir", "-r", filepath.Join(dir, "requirements.txt"), "gunicorn"},
		{"env", home, "bash", filepath.Join(dir, "scripts", "setup-assets.sh")},
	}
	for _, step := range build {
		if err := host.Run(host.RunAs(ctx, owner, step[0], step[1:]...)); err != nil {
			return err
		}
	}
	if owner != host.CurrentUser() {
		if err := host.Chown(dir, owner+":", true); err != nil {
			return err
		}
	}
	if err := host.Systemctl(ctx, "restart", nativeUnit); err != nil {
		return err
	}
	in.UI.OK("Traefik Manager updated and restarted")
	return nil
}

func (in *Installer) gitHead(ctx context.Context, owner, home, dir string) string {
	out, err := host.Output(host.RunAs(ctx, owner, "env", home, "git", "-C", dir, "rev-parse", "HEAD"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
