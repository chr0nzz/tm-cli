package installer

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/host"
	"github.com/chr0nzz/traefik-stack/internal/render"
	"github.com/chr0nzz/traefik-stack/internal/state"
)

const (
	nativeRepo      = "https://github.com/chr0nzz/traefik-manager.git"
	nativeUser      = "traefik-manager"
	nativeUnit      = "traefik-manager"
	restartPathUnit = "traefik-restart.path"
)

func (in *Installer) installNative(ctx context.Context, a *answers.Answers, out *render.Output, st *state.State) error {
	in.UI.Step("Installing Traefik Manager")
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
			if err := host.Run(host.Privileged(ctx, "git", "clone", "--branch", "main", nativeRepo, dir)); err != nil {
				return err
			}
			if err := host.Chown(dir, host.CurrentUser(), true); err != nil {
				return err
			}
		} else if err := host.Run(host.Command(ctx, "git", "clone", "--branch", "main", nativeRepo, dir)); err != nil {
			return err
		}
	}
	in.UI.OK("Repository cloned to " + dir)
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
	if a.Native.ServiceUser {
		if host.UserExists(nativeUser) {
			in.UI.OK("System user " + nativeUser + " already exists")
		} else {
			if err := host.AddSystemUser(ctx, nativeUser); err != nil {
				return err
			}
			in.UI.OK("System user " + nativeUser + " created")
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
	steps := [][]string{
		{"env", home, "git", "-C", dir, "pull"},
		{"env", home, filepath.Join(dir, "venv", "bin", "pip"), "install", "-q", "--no-cache-dir", "-r", filepath.Join(dir, "requirements.txt"), "gunicorn"},
		{"env", home, "bash", filepath.Join(dir, "scripts", "setup-assets.sh")},
	}
	for _, step := range steps {
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
