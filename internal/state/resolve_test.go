package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chr0nzz/traefik-stack/internal/answers"
)

func savedInstall(t *testing.T, mode answers.Mode, dir string) *State {
	t.Helper()
	a := answers.Defaults(mode)
	a.Dir = dir
	a.Domain = "example.com"
	a.TLS.Email = "me@example.com"
	a.Finalize()
	st := New(a, "1.12.0", "docker compose")
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestResolveFlagWithState(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	savedInstall(t, answers.ModeFull, dir)
	t.Chdir(t.TempDir())
	st, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dir != dir || st.Mode != answers.ModeFull {
		t.Fatalf("resolved wrong install: %+v", st)
	}
}

func TestResolveFlagAdoptsCompose(t *testing.T) {
	isolate(t)
	dir := copyFixture(t, "tm-docker")
	t.Chdir(t.TempDir())
	st, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Adopted || st.Mode != answers.ModeTMDocker || st.Dir != dir {
		t.Fatalf("expected adoption of %s, got %+v", dir, st)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tm", "state.yml")); err == nil {
		t.Fatal("resolving must not write state into the user's directory")
	}
	again, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Path != st.Path {
		t.Fatalf("second resolve did not load the saved state: %s", again.Path)
	}
}

func TestResolveFlagErrors(t *testing.T) {
	isolate(t)
	empty := t.TempDir()
	_, err := Resolve(empty)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), empty) || !strings.Contains(err.Error(), "docker-compose.yml") {
		t.Fatalf("expected descriptive not found error, got %v", err)
	}
	foreign := copyFixture(t, "not-adoptable")
	_, err = Resolve(foreign)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign compose must be reported as not found, got %v", err)
	}
	missing := filepath.Join(empty, "nope")
	_, err = Resolve(missing)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing dir error, got %v", err)
	}
}

func TestResolveTMDirEnv(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	savedInstall(t, answers.ModeFull, dir)
	t.Chdir(t.TempDir())
	t.Setenv("TM_DIR", dir)
	st, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if st.Dir != dir {
		t.Fatalf("TM_DIR ignored: %s", st.Dir)
	}
	other := t.TempDir()
	savedInstall(t, answers.ModeTMDocker, other)
	st, err = Resolve(other)
	if err != nil {
		t.Fatal(err)
	}
	if st.Dir != other {
		t.Fatalf("--dir must win over TM_DIR: %s", st.Dir)
	}
}

func TestResolveCwdState(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	savedInstall(t, answers.ModeFull, dir)
	other := t.TempDir()
	savedInstall(t, answers.ModeTMDocker, other)
	t.Chdir(dir)
	st, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if st.Dir != dir {
		t.Fatalf("cwd state must win over the registry: %s", st.Dir)
	}
}

func TestResolveCwdAdopt(t *testing.T) {
	isolate(t)
	savedInstall(t, answers.ModeFull, t.TempDir())
	dir := copyFixture(t, "agent-docker")
	t.Chdir(dir)
	st, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Adopted || st.Mode != answers.ModeAgentDocker || st.Dir != dir {
		t.Fatalf("cwd compose must be adopted before consulting the registry: %+v", st)
	}
}

func TestResolveRegistrySingle(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	savedInstall(t, answers.ModeFull, dir)
	t.Chdir(t.TempDir())
	st, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if st.Dir != dir {
		t.Fatalf("single registry entry not used: %s", st.Dir)
	}
}

func TestResolveRegistryAmbiguous(t *testing.T) {
	isolate(t)
	a := t.TempDir()
	b := t.TempDir()
	savedInstall(t, answers.ModeFull, a)
	savedInstall(t, answers.ModeTMDocker, b)
	t.Chdir(t.TempDir())
	_, err := Resolve("")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected ErrAmbiguous, got %v", err)
	}
	var amb *AmbiguousError
	if !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("expected two candidates, got %v", err)
	}
	if amb.Candidates[0].Mode != answers.ModeFull || amb.Candidates[0].Dir != a || amb.Candidates[1].Mode != answers.ModeTMDocker || amb.Candidates[1].Dir != b {
		t.Fatalf("candidates wrong: %+v", amb.Candidates)
	}
	msg := err.Error()
	for _, want := range []string{"--dir", filepath.Join(a, ".tm", "state.yml"), filepath.Join(b, ".tm", "state.yml"), "full", "tm-docker"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error text missing %q:\n%s", want, msg)
		}
	}
}

func TestResolveRegistryPrunesBeforeDeciding(t *testing.T) {
	isolate(t)
	a := t.TempDir()
	b := t.TempDir()
	savedInstall(t, answers.ModeFull, a)
	gone := savedInstall(t, answers.ModeTMDocker, b)
	if err := os.RemoveAll(filepath.Join(b, ".tm")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	st, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if st.Dir != a {
		t.Fatalf("stale entry %s should have been pruned, got %s", gone.Path, st.Dir)
	}
}

func TestResolveNothing(t *testing.T) {
	isolate(t)
	t.Chdir(t.TempDir())
	_, err := Resolve("")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "tm install") || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("expected guidance, got %v", err)
	}
}
