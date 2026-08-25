package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestVersionCommand(t *testing.T) {
	build = Build{Version: "1.2.3", Commit: "abc", Date: "2026-08-22"}
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	_, err := run(t, "version")
	w.Close()
	os.Stdout = old
	var out bytes.Buffer
	out.ReadFrom(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "tm 1.2.3 (abc, 2026-08-22)" {
		t.Fatalf("unexpected version output %q", out.String())
	}
}

func TestInstallRejectsUnknownMode(t *testing.T) {
	_, err := run(t, "install", "--mode", "bogus", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("expected unknown mode error, got %v", err)
	}
}

func TestInstallModeConflictsWithAnswers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.yml")
	if err := os.WriteFile(p, []byte("mode: agent-binary\nsecrets:\n  TMA_API_KEY: k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, "install", "--mode", "full", "--answers", p, "--dry-run", "--output", dir)
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestInstallDryRunRendersAnswers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.yml")
	if err := os.WriteFile(p, []byte("mode: agent-binary\nsecrets:\n  TMA_API_KEY: k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if _, err := run(t, "install", "--answers", p, "--dry-run", "--output", out, "--dump-answers", filepath.Join(dir, "dump.yml")); err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(filepath.Join(out, "etc/systemd/system/tma.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "EnvironmentFile=/etc/traefik-manager-agent/env") {
		t.Fatalf("unit missing env file:\n%s", unit)
	}
	env, err := os.ReadFile(filepath.Join(out, "etc/traefik-manager-agent/env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "TMA_API_KEY='k'") {
		t.Fatalf("env missing key:\n%s", env)
	}
	dump, err := os.ReadFile(filepath.Join(dir, "dump.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dump), "TMA_API_KEY") || !strings.HasPrefix(string(dump), "mode: agent-binary\n") {
		t.Fatalf("dump wrong:\n%s", dump)
	}
}

func TestPasswordValidation(t *testing.T) {
	cases := map[string]bool{
		"":                       false,
		"short7x":                false,
		"exactly8":               true,
		strings.Repeat("a", 72):  true,
		strings.Repeat("a", 73):  false,
		strings.Repeat("é", 36):  true,
		strings.Repeat("é", 37):  false,
		"a password with spaces": true,
	}
	for pw, ok := range cases {
		err := passwordError(pw)
		if ok && err != nil {
			t.Errorf("%q (%d bytes) should be accepted: %v", pw, len(pw), err)
		}
		if !ok && err == nil {
			t.Errorf("%q (%d bytes) should be rejected", pw, len(pw))
		}
	}
}

func TestReadPasswordStdin(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"correct-horse\n", "correct-horse"},
		{"correct-horse", "correct-horse"},
		{"correct-horse\r\n", "correct-horse"},
		{"correct-horse\nsecond line\n", "correct-horse"},
		{bom + "correct-horse\n", "correct-horse"},
		{"  padded spaces  \n", "  padded spaces  "},
	}
	for _, c := range cases {
		got, err := readPasswordStdin(strings.NewReader(c.in))
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q -> %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "\n", "short\n"} {
		if _, err := readPasswordStdin(strings.NewReader(bad)); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestPasswordResetFlagConflict(t *testing.T) {
	_, err := run(t, "password", "reset", "--random", "--stdin")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected a flag conflict error, got %v", err)
	}
}
