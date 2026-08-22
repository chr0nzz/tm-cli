package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fakeSudo(t *testing.T) func() []string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	dir := t.TempDir()
	logFile := filepath.Join(dir, "sudo.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logFile + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	orig := lookPath
	lookPath = exec.LookPath
	t.Cleanup(func() { lookPath = orig })
	return func() []string {
		data, _ := os.ReadFile(logFile)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

func TestSudoFallbacks(t *testing.T) {
	calls := fakeSudo(t)
	ctx := context.Background()

	if err := WriteFile("/proc/nope/tm/env", []byte("A=1\n"), 0o600); err != nil {
		t.Errorf("WriteFile: %v", err)
	}
	if err := MkdirAll("/proc/nope/data", 0o750); err != nil {
		t.Errorf("MkdirAll: %v", err)
	}
	if err := Chmod("/etc/hostname", 0o644); err != nil {
		t.Errorf("Chmod: %v", err)
	}
	if err := Chown("/opt/tm", "traefik-manager:", true); err != nil {
		t.Errorf("Chown: %v", err)
	}
	if err := Chown("/opt/tm/file", "root:root", false); err != nil {
		t.Errorf("Chown: %v", err)
	}
	if err := Systemctl(ctx, "daemon-reload"); err != nil {
		t.Errorf("Systemctl: %v", err)
	}
	if _, err := SystemctlOutput(ctx, "status", "tma"); err != nil {
		t.Errorf("SystemctlOutput: %v", err)
	}
	if _, err := Journalctl(ctx, "traefik-manager", 50); err != nil {
		t.Errorf("Journalctl: %v", err)
	}
	if err := AddSystemUser(ctx, "traefik-manager"); err != nil {
		t.Errorf("AddSystemUser: %v", err)
	}
	if err := AddUserToGroup(ctx, "traefik-manager", "docker"); err != nil {
		t.Errorf("AddUserToGroup: %v", err)
	}
	var logged string
	if err := SudoPreflight(ctx, []string{"systemd unit", "service user"}, func(s string) { logged = s }); err != nil {
		t.Errorf("SudoPreflight: %v", err)
	}
	if logged != "this install uses sudo for: systemd unit, service user" {
		t.Errorf("preflight log = %q", logged)
	}

	ro := filepath.Join(t.TempDir(), "ro")
	os.Mkdir(ro, 0o755)
	locked := filepath.Join(ro, "secret")
	os.WriteFile(locked, []byte("hidden"), 0o000)
	os.Chmod(ro, 0o500)
	t.Cleanup(func() { os.Chmod(ro, 0o700) })
	if _, err := ReadFile(locked); err != nil {
		t.Errorf("ReadFile via sudo: %v", err)
	}
	if err := Remove(locked, false); err != nil {
		t.Errorf("Remove via sudo: %v", err)
	}
	if err := Remove(ro, true); err != nil {
		t.Errorf("Remove -rf via sudo: %v", err)
	}

	got := calls()
	want := []string{
		"install -m 0600 -D <tmp> /proc/nope/tm/env",
		"mkdir -p -m 0750 /proc/nope/data",
		"chmod 0644 /etc/hostname",
		"chown -R traefik-manager: /opt/tm",
		"chown root:root /opt/tm/file",
		"systemctl daemon-reload",
		"systemctl status tma",
		"journalctl -u traefik-manager --no-pager -n 50",
		"useradd --system --no-create-home --shell " + nologinShell() + " traefik-manager",
		"usermod -aG docker traefik-manager",
		"-v",
		"cat " + locked,
		"rm -f " + locked,
		"rm -rf " + ro,
	}
	if len(got) != len(want) {
		t.Fatalf("sudo calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		g := got[i]
		if strings.HasPrefix(want[i], "install ") {
			fields := strings.Fields(g)
			if len(fields) == 6 && strings.HasPrefix(fields[4], os.TempDir()) {
				fields[4] = "<tmp>"
				g = strings.Join(fields, " ")
			}
		}
		if g != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "tm-*")); len(matches) > 0 {
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
				t.Errorf("temp file %s left behind", m)
			}
		}
	}
}

func TestDirectPathsSkipSudo(t *testing.T) {
	calls := fakeSudo(t)
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Chmod(filepath.Join(dir, "f"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(filepath.Join(dir, "f")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(filepath.Join(dir, "d"), true); err != nil {
		t.Fatal(err)
	}
	if got := calls(); len(got) != 1 || got[0] != "" {
		t.Errorf("unexpected sudo calls: %v", got)
	}
}
