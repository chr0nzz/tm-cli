package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/state"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

func nativeState(t *testing.T, dir string) *state.State {
	t.Helper()
	a := answers.Defaults(answers.ModeTMNative)
	a.Native.InstallDir = filepath.Join(dir, "opt")
	a.Native.DataDir = filepath.Join(dir, "data")
	a.Finalize()
	if err := os.MkdirAll(filepath.Join(a.Native.InstallDir, "venv", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(a.Native.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return state.New(a, "test", "")
}

func TestResetArgs(t *testing.T) {
	cases := []struct {
		opts PasswordResetOptions
		want string
	}{
		{PasswordResetOptions{}, "reset-password --stdin"},
		{PasswordResetOptions{DisableOTP: true}, "reset-password --stdin --disable-otp"},
		{PasswordResetOptions{Random: true}, "reset-password"},
		{PasswordResetOptions{Random: true, DisableOTP: true}, "reset-password --disable-otp"},
	}
	for _, c := range cases {
		if got := strings.Join(resetArgs(c.opts), " "); got != c.want {
			t.Errorf("resetArgs(%+v) = %q, want %q", c.opts, got, c.want)
		}
	}
}

func TestNativeResetPlanPassesSettingsPathAndRunsInTheInstallDir(t *testing.T) {
	dir := t.TempDir()
	st := nativeState(t, dir)
	settings := filepath.Join(st.Answers.Native.DataDir, "manager.yml")
	flask := filepath.Join(st.Answers.Native.InstallDir, "venv", "bin", "flask")
	if err := os.WriteFile(settings, []byte("password_hash: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flask, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	in := &Installer{UI: ui.NewPlain(os.Stderr)}
	plan, err := in.planFor(context.Background(), st, resetArgs(PasswordResetOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(plan.Cmd.Args, " ")
	if !strings.Contains(line, "SETTINGS_PATH="+settings) {
		t.Errorf("SETTINGS_PATH must be explicit, sudo -u strips the unit environment: %s", line)
	}
	if !strings.Contains(line, flask) {
		t.Errorf("the venv path must be absolute: %s", line)
	}
	if !strings.Contains(line, "reset-password --stdin") {
		t.Errorf("missing the command: %s", line)
	}
	if plan.Cmd.Dir != st.Answers.Native.InstallDir {
		t.Errorf("cwd = %q, want the install dir %q", plan.Cmd.Dir, st.Answers.Native.InstallDir)
	}
	if plan.Owner == "" {
		t.Error("the owner of the settings file must be resolved")
	}
}

func TestNativeResetPlanRefusesWhenSettingsOrFlaskAreMissing(t *testing.T) {
	dir := t.TempDir()
	st := nativeState(t, dir)
	in := &Installer{UI: ui.NewPlain(os.Stderr)}
	_, err := in.planFor(context.Background(), st, resetArgs(PasswordResetOptions{}))
	if err == nil || !strings.Contains(err.Error(), "no settings file") {
		t.Fatalf("expected a settings-file error naming the path, got %v", err)
	}
	settings := filepath.Join(st.Answers.Native.DataDir, "manager.yml")
	if err := os.WriteFile(settings, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = in.planFor(context.Background(), st, resetArgs(PasswordResetOptions{}))
	if err == nil || !strings.Contains(err.Error(), "no flask binary") {
		t.Fatalf("expected a flask error pointing at tm update, got %v", err)
	}
}

func TestResetRefusesForAgents(t *testing.T) {
	a := answers.Defaults(answers.ModeAgentBinary)
	a.SetSecret(answers.SecretTMAAPIKey, "k")
	a.Finalize()
	st := state.New(a, "test", "")
	in := &Installer{UI: ui.NewPlain(os.Stderr)}
	err := in.ResetPassword(context.Background(), st, PasswordResetOptions{Password: "abcdefgh"})
	if err == nil || !strings.Contains(err.Error(), "agents have no password") {
		t.Fatalf("expected an agent refusal, got %v", err)
	}
}

func TestAdminPasswordInTheUnitBlocksTheReset(t *testing.T) {
	dir := t.TempDir()
	st := nativeState(t, dir)
	unit := filepath.Join(dir, "traefik-manager.service")
	if err := os.WriteFile(unit, []byte("[Service]\nEnvironment=\"ADMIN_PASSWORD=hunter2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := unitPathFor
	unitPathFor = func(string) string { return unit }
	defer func() { unitPathFor = restore }()
	in := &Installer{UI: ui.NewPlain(os.Stderr)}
	set, where := in.adminPasswordEnv(context.Background(), st)
	if !set || where == "" {
		t.Fatalf("ADMIN_PASSWORD in the unit must be detected: set=%v where=%q", set, where)
	}
	if err := os.WriteFile(unit, []byte("[Service]\nEnvironment=\"COOKIE_SECURE=false\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if set, _ := in.adminPasswordEnv(context.Background(), st); set {
		t.Error("no ADMIN_PASSWORD must read as not set")
	}
}
