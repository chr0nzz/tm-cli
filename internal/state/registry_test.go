package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryLifecycle(t *testing.T) {
	isolate(t)
	paths, err := Registry()
	if err != nil || len(paths) != 0 {
		t.Fatalf("missing registry should be empty, got %v %v", paths, err)
	}
	dir := t.TempDir()
	a := writeTemp(t, dir, "a/.tm/state.yml", "x")
	b := writeTemp(t, dir, "b/.tm/state.yml", "x")
	for _, p := range []string{a, a, b, a} {
		if err := Register(p); err != nil {
			t.Fatal(err)
		}
	}
	paths, err = Registry()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != a+","+b {
		t.Fatalf("expected deduped [a b], got %v", paths)
	}
	raw, err := os.ReadFile(os.Getenv("TM_REGISTRY"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "installs:\n") || strings.Count(string(raw), a) != 1 {
		t.Fatalf("unexpected registry file:\n%s", raw)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	paths, err = Registry()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != a {
		t.Fatalf("expected prune to drop b, got %v", paths)
	}
	raw, _ = os.ReadFile(os.Getenv("TM_REGISTRY"))
	if strings.Contains(string(raw), b) {
		t.Fatalf("prune did not rewrite the file:\n%s", raw)
	}
	if err := Unregister(a); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(a); err != nil {
		t.Fatal(err)
	}
	paths, err = Registry()
	if err != nil || len(paths) != 0 {
		t.Fatalf("expected empty after unregister, got %v %v", paths, err)
	}
}

func TestRegistryRelativePathsAreAbsolute(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Chdir(dir)
	writeTemp(t, dir, "state.yml", "x")
	if err := Register("state.yml"); err != nil {
		t.Fatal(err)
	}
	paths, err := Registry()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !filepath.IsAbs(paths[0]) {
		t.Fatalf("expected one absolute path, got %v", paths)
	}
}

func TestRegistryCorruptFileIsIgnored(t *testing.T) {
	isolate(t)
	if err := os.WriteFile(os.Getenv("TM_REGISTRY"), []byte("installs: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := Registry()
	if err != nil || len(paths) != 0 {
		t.Fatalf("corrupt registry must read as empty, got %v %v", paths, err)
	}
	p := writeTemp(t, t.TempDir(), "state.yml", "x")
	if err := Register(p); err != nil {
		t.Fatal(err)
	}
	paths, _ = Registry()
	if len(paths) != 1 {
		t.Fatalf("register after corrupt file failed: %v", paths)
	}
}

func TestRegistryPathDefault(t *testing.T) {
	t.Setenv("TM_REGISTRY", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := registryPath(); got != "/tmp/xdg/tm/installs.yml" {
		t.Fatalf("unexpected default registry path %s", got)
	}
}
