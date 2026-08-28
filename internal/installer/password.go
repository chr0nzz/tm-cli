package installer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/state"
)

const tmContainer = "traefik-manager"

var unitPathFor = func(unit string) string { return "/etc/systemd/system/" + unit + ".service" }

type PasswordResetOptions struct {
	Password   string
	Random     bool
	DisableOTP bool
}

type resetPlan struct {
	Cmd          *exec.Cmd
	SettingsPath string
	Owner        string
}

func (in *Installer) ResetPassword(ctx context.Context, st *state.State, opts PasswordResetOptions) error {
	if err := in.prepare(st); err != nil {
		return err
	}
	if st.Mode.IsAgent() {
		return fmt.Errorf("agents have no password, the API key lives in %s", secretsLocation(st))
	}
	if err := in.checkAdminPasswordEnv(ctx, st); err != nil {
		return err
	}
	plan, err := in.planFor(ctx, st, resetArgs(opts))
	if err != nil {
		return err
	}
	if plan.Owner != "" {
		in.UI.Info("running Traefik Manager's own reset as " + plan.Owner)
	}
	if !opts.Random {
		plan.Cmd.Stdin = strings.NewReader(opts.Password + "\n")
	} else {
		plan.Cmd.Stdin = bytes.NewReader(nil)
	}
	in.UI.Step("Resetting the Traefik Manager password")
	if err := host.Run(plan.Cmd); err != nil {
		return fmt.Errorf("reset-password: %w", err)
	}
	return nil
}

func (in *Installer) SupportsChosenPassword(ctx context.Context, st *state.State) (bool, error) {
	plan, err := in.planFor(ctx, st, []string{"reset-password", "--help"})
	if err != nil {
		return false, err
	}
	plan.Cmd.Stdin = bytes.NewReader(nil)
	out, err := host.Output(plan.Cmd)
	if err != nil {
		return false, fmt.Errorf("could not ask Traefik Manager which reset options it supports: %w", err)
	}
	return strings.Contains(out, "--stdin"), nil
}

func resetArgs(opts PasswordResetOptions) []string {
	args := []string{"reset-password"}
	if !opts.Random {
		args = append(args, "--stdin")
	}
	if opts.DisableOTP {
		args = append(args, "--disable-otp")
	}
	return args
}

func (in *Installer) planFor(ctx context.Context, st *state.State, args []string) (resetPlan, error) {
	if st.Mode == answers.ModeTMNative {
		return in.nativeResetPlan(ctx, st, args)
	}
	if status, _ := containerStatus(ctx, tmContainer); status != "running" {
		return resetPlan{}, fmt.Errorf("the %s container is not running (%s), so its reset command cannot be used: start it with tm start, or follow the manual reset at https://traefik-manager.xyzlab.dev/reset-password", tmContainer, status)
	}
	docker := append([]string{"exec", "-i", tmContainer, "flask"}, args...)
	return resetPlan{Cmd: host.DockerCommand(ctx, docker...)}, nil
}

func (in *Installer) nativeResetPlan(ctx context.Context, st *state.State, args []string) (resetPlan, error) {
	a := &st.Answers
	settings := nativeSettingsPath(a)
	if !host.Exists(settings) {
		return resetPlan{}, fmt.Errorf("no settings file at %s: pass --dir, or check the SETTINGS_PATH in /etc/systemd/system/%s.service", settings, nativeUnit)
	}
	flask := filepath.Join(a.Native.InstallDir, "venv", "bin", "flask")
	if !host.Exists(flask) {
		return resetPlan{}, fmt.Errorf("no flask binary at %s: run tm update to rebuild the virtualenv", flask)
	}
	owner, err := host.FileOwner(settings)
	if err != nil {
		return resetPlan{}, err
	}
	full := append([]string{"env", "HOME=" + a.Native.InstallDir, "SETTINGS_PATH=" + settings, flask}, args...)
	cmd := host.RunAs(ctx, owner, full[0], full[1:]...)
	cmd.Dir = a.Native.InstallDir
	return resetPlan{Cmd: cmd, SettingsPath: settings, Owner: owner}, nil
}

func nativeSettingsPath(a *answers.Answers) string {
	return filepath.Join(a.Native.DataDir, "manager.yml")
}

func (in *Installer) checkAdminPasswordEnv(ctx context.Context, st *state.State) error {
	set, where := in.adminPasswordEnv(ctx, st)
	if !set {
		return nil
	}
	return fmt.Errorf("ADMIN_PASSWORD is set in %s and overrides the stored password, so a new one would not let you in: change or remove that variable first", where)
}

func (in *Installer) adminPasswordEnv(ctx context.Context, st *state.State) (bool, string) {
	if st.Mode == answers.ModeTMNative {
		data, err := host.ReadFile(unitPathFor(nativeUnit))
		if err != nil {
			return false, ""
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			v, ok := strings.CutPrefix(line, "Environment=")
			if !ok {
				continue
			}
			v = strings.Trim(v, `"`)
			if k, val, ok := strings.Cut(v, "="); ok && k == "ADMIN_PASSWORD" && strings.TrimSpace(val) != "" {
				return true, nativeUnit + ".service"
			}
		}
		return false, ""
	}
	out, err := host.Output(host.DockerCommand(ctx, "exec", tmContainer, "printenv", "ADMIN_PASSWORD"))
	if err != nil {
		return false, ""
	}
	if strings.TrimSpace(out) == "" {
		return false, ""
	}
	return true, "the " + tmContainer + " container environment"
}
