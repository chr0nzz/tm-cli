package installer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/ghrelease"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/render"
	"github.com/chr0nzz/tm-cli/internal/state"
)

const traefikUnit = "traefik"

func (in *Installer) installFullNative(ctx context.Context, a *answers.Answers, out *render.Output, st *state.State) error {
	version, err := in.installTraefikBinary(ctx)
	if err != nil {
		return err
	}
	st.TraefikVersion = version
	in.TraefikVersion = version
	in.UI.Step("Installing Traefik Manager")
	if err := in.cloneOrPullNative(ctx, a); err != nil {
		return err
	}
	if err := in.buildNativeVenv(ctx, a.Native.InstallDir); err != nil {
		return err
	}
	if err := in.installCrowdSecPackage(ctx, a); err != nil {
		return err
	}
	if err := in.writeOutput(a, out, st, nil); err != nil {
		return err
	}
	in.registerCrowdSecNative(ctx, a)
	in.reloadCrowdSec(ctx, a)
	if err := in.ensureNativeUser(ctx); err != nil {
		return err
	}
	owned := []string{
		a.Native.InstallDir,
		a.Native.DataDir,
		answers.NativeTraefikConfigDir,
		answers.NativeTraefikStateDir,
		answers.NativeTraefikLogDir,
	}
	for _, dir := range owned {
		if err := host.Chown(dir, nativeUser+":", true); err != nil {
			return err
		}
	}
	in.UI.OK("Directories owned by " + nativeUser)
	if err := host.Systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := host.Systemctl(ctx, "enable", "--now", traefikUnit); err != nil {
		return err
	}
	in.UI.OK("Traefik service enabled and started")
	if a.Mounts.StaticConfig {
		if err := host.Systemctl(ctx, "enable", "--now", restartPathUnit); err != nil {
			return err
		}
		in.UI.OK("Traefik restart watcher enabled (" + restartPathUnit + ")")
	}
	if err := host.Systemctl(ctx, "enable", "--now", nativeUnit); err != nil {
		return err
	}
	in.UI.OK("Traefik Manager service enabled and started")
	in.TempPassword = in.fetchPasswordNative(ctx)
	return nil
}

func (in *Installer) installTraefikBinary(ctx context.Context) (string, error) {
	arch, err := host.Arch()
	if err != nil {
		return "", err
	}
	in.UI.Step("Installing Traefik")
	version, err := ghrelease.LatestVersion(ctx, ghrelease.TraefikRepo)
	if err != nil {
		return "", err
	}
	data, err := ghrelease.FetchTraefikBinary(ctx, version, arch)
	if err != nil {
		return "", err
	}
	if err := host.WriteFile(answers.TraefikBinaryPath, data, 0o755); err != nil {
		return "", err
	}
	in.UI.OK("Traefik " + version + " installed to " + answers.TraefikBinaryPath)
	return version, nil
}

func (in *Installer) updateFullNative(ctx context.Context, st *state.State) error {
	if err := in.updateNative(ctx, &st.Answers); err != nil {
		return err
	}
	return in.updateTraefikNative(ctx, st)
}

func (in *Installer) updateTraefikNative(ctx context.Context, st *state.State) error {
	in.UI.Step("Checking for a new Traefik release")
	latest, err := ghrelease.LatestVersion(ctx, ghrelease.TraefikRepo)
	if err != nil {
		return err
	}
	if latest == st.TraefikVersion {
		in.UI.OK("Traefik " + latest + " is already the newest release")
		return nil
	}
	arch, err := host.Arch()
	if err != nil {
		return err
	}
	data, err := ghrelease.FetchTraefikBinary(ctx, latest, arch)
	if err != nil {
		return err
	}
	prev := answers.TraefikBinaryPath + ".prev"
	if host.Exists(answers.TraefikBinaryPath) {
		if err := host.Run(host.Privileged(ctx, "cp", "-p", answers.TraefikBinaryPath, prev)); err != nil {
			return err
		}
	}
	if err := host.WriteFile(answers.TraefikBinaryPath, data, 0o755); err != nil {
		return err
	}
	if err := host.Systemctl(ctx, "restart", traefikUnit); err != nil {
		return in.rollbackTraefik(ctx, st, latest, err)
	}
	if !in.waitTraefikReady(ctx, &st.Answers, 30*time.Second) {
		return in.rollbackTraefik(ctx, st, latest, errors.New("the ping endpoint did not answer within 30s"))
	}
	if st.TraefikVersion == "" {
		in.UI.OK("Traefik updated to " + latest)
	} else {
		in.UI.OK("Traefik updated from " + st.TraefikVersion + " to " + latest)
	}
	st.TraefikVersion = latest
	return nil
}

func (in *Installer) rollbackTraefik(ctx context.Context, st *state.State, latest string, cause error) error {
	prev := answers.TraefikBinaryPath + ".prev"
	if !host.Exists(prev) {
		return fmt.Errorf("traefik %s is not healthy and no previous binary was kept to roll back to: %w", latest, cause)
	}
	in.UI.Warn("Traefik " + latest + " is not healthy, restoring the previous binary")
	if err := host.Run(host.Privileged(ctx, "cp", "-p", prev, answers.TraefikBinaryPath)); err != nil {
		return fmt.Errorf("traefik %s is not healthy and the rollback copy failed (%v): %w", latest, err, cause)
	}
	if err := host.Systemctl(ctx, "restart", traefikUnit); err != nil {
		return fmt.Errorf("traefik %s is not healthy and the restart on the previous binary failed (%v): %w", latest, err, cause)
	}
	if !in.waitTraefikReady(ctx, &st.Answers, 30*time.Second) {
		return fmt.Errorf("traefik %s is not healthy and the restored binary is not answering either, check tm logs traefik: %w", latest, cause)
	}
	in.UI.OK("Previous Traefik binary restored and healthy")
	return fmt.Errorf("traefik %s failed its health check, so the previous binary was restored and is running again: %w", latest, cause)
}

func (in *Installer) waitTraefikReady(ctx context.Context, a *answers.Answers, timeout time.Duration) bool {
	url := "http://127.0.0.1:" + a.Network.TraefikAPIPort + "/ping"
	deadline := time.Now().Add(timeout)
	for {
		if probe(ctx, url, "").OK {
			return true
		}
		if time.Now().After(deadline) || !sleep(ctx, time.Second) {
			return false
		}
	}
}

func staticDeclaresPlugins(path string) bool {
	data, err := host.ReadFile(path)
	if err != nil {
		return false
	}
	var doc struct {
		Experimental struct {
			Plugins map[string]any `yaml:"plugins"`
		} `yaml:"experimental"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return len(doc.Experimental.Plugins) > 0
}

var pluginsStorageDir = filepath.Join(answers.NativeTraefikStateDir, "plugins-storage")
