package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

var placeholders = []string{
	"tma-api-key-PLACEHOLDER",
	"traefik-api-password-PLACEHOLDER",
	"crowdsec-api-key-PLACEHOLDER",
	"crowdsec-machine-password-PLACEHOLDER",
	"git-backup-token-PLACEHOLDER",
	"cf-dns-api-token-PLACEHOLDER",
	"aws-access-key-id-PLACEHOLDER",
	"aws-secret-access-key-PLACEHOLDER",
}

var writtenModes = map[string]os.FileMode{}

func TestMain(m *testing.M) {
	writeFile = func(path string, data []byte, mode os.FileMode) error {
		writtenModes[path] = mode
		return os.WriteFile(path, data, mode)
	}
	readFile = os.ReadFile
	mkdirAll = os.MkdirAll
	exists = func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}
	dir, err := os.MkdirTemp("", "tm-state-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("TM_REGISTRY", filepath.Join(dir, "installs.yml"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("TM_REGISTRY", filepath.Join(t.TempDir(), "installs.yml"))
	t.Setenv("TM_DIR", "")
}

func copyFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", name)
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func registryHas(t *testing.T, path string) bool {
	t.Helper()
	paths, err := Registry()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

func fullAnswers(dir string) *answers.Answers {
	a := answers.Defaults(answers.ModeFull)
	a.Dir = dir
	a.Domain = "example.com"
	a.TLS.Method = answers.TLSDNS
	a.TLS.Provider = "cloudflare"
	a.TLS.Email = "me@example.com"
	a.Mounts.Certs = false
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.SetSecret("CF_DNS_API_TOKEN", "cf-dns-api-token-PLACEHOLDER")
	a.SetSecret(answers.SecretCrowdSecAPIKey, "crowdsec-api-key-PLACEHOLDER")
	a.Finalize()
	return a
}

func TestNewSaveLoadRoundTrip(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	a := fullAnswers(dir)
	st := New(a, "1.12.0", "docker compose")
	st.Own("docker-compose.yml", []byte("services: {}\n"))
	if st.Path != filepath.Join(dir, ".tm", "state.yml") {
		t.Fatalf("unexpected path %s", st.Path)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, p := range placeholders {
		if strings.Contains(text, p) {
			t.Fatalf("state file leaked a secret:\n%s", text)
		}
	}
	if strings.Contains(text, "secrets") {
		t.Fatalf("state file contains a secrets key:\n%s", text)
	}
	for _, key := range []string{"version: 1", "mode: full", "tm_version: 1.12.0", "installed_at:", "updated_at:", "adopted: false", "compose_cmd: docker compose", "dir: " + dir, "owned_files:", "answers:"} {
		if !strings.Contains(text, key) {
			t.Fatalf("state file missing %q:\n%s", key, text)
		}
	}
	if writtenModes[st.Path] != 0o644 {
		t.Fatalf("expected 0644 outside /etc, got %o", writtenModes[st.Path])
	}
	if !registryHas(t, st.Path) {
		t.Fatal("save did not register the state path")
	}
	got, err := Load(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != answers.ModeFull || got.TMVersion != "1.12.0" || got.ComposeCmd != "docker compose" || got.Dir != dir || got.Path != st.Path {
		t.Fatalf("round trip changed fields: %+v", got)
	}
	if got.InstalledAt.IsZero() || !got.InstalledAt.Equal(st.InstalledAt) {
		t.Fatalf("installed_at changed: %v vs %v", got.InstalledAt, st.InstalledAt)
	}
	if got.OwnedFiles["docker-compose.yml"] != Hash([]byte("services: {}\n")) {
		t.Fatalf("owned files lost: %v", got.OwnedFiles)
	}
	b := got.Answers
	if b.Domain != "example.com" || b.Hosts.Manager != "manager.example.com" || b.TLS.Provider != "cloudflare" || b.Mounts.Certs || !b.Mounts.AccessLogs || b.CrowdSec.MachineID != "traefik-manager" {
		t.Fatalf("answers changed in round trip: %+v", b)
	}
	if len(b.Secrets) != 0 {
		t.Fatalf("secrets must not survive a round trip: %v", b.Secrets)
	}
}

func TestLoadKeepsAnswerDefaultsForAbsentFields(t *testing.T) {
	isolate(t)
	p := writeTemp(t, t.TempDir(), "state.yml", "version: 1\nmode: tm-docker\ntm_version: 1.12.0\ndir: /srv/tm\nanswers:\n  mode: tm-docker\n  hosts:\n    manager: m.example.com\n")
	st, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	a := st.Answers
	if !a.Access.ViaTraefik || !a.Network.External || a.Network.Name != answers.DefaultNetwork || a.Mounts.AccessLogPath != answers.DefaultAccessLogPath || a.Hosts.Manager != "m.example.com" {
		t.Fatalf("defaults not applied: %+v", a)
	}
	if st.OwnedFiles == nil {
		t.Fatal("owned files must never be nil")
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.yml")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	cases := map[string]string{
		"version":  "version: 2\nmode: full\nanswers:\n  mode: full\n",
		"mode":     "version: 1\nmode: nope\nanswers:\n  mode: full\n",
		"answers":  "version: 1\nmode: full\n",
		"mismatch": "version: 1\nmode: full\nanswers:\n  mode: tm-docker\n",
		"yaml":     "version: [\n",
	}
	for name, content := range cases {
		p := writeTemp(t, dir, name+".yml", content)
		if _, err := Load(p); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestSaveUsesRestrictiveModeUnderEtc(t *testing.T) {
	if fileMode("/etc/traefik-manager/tm-state.yml") != 0o644 || fileMode("/home/x/stack/.tm/state.yml") != 0o644 {
		t.Fatal("state holds no secrets, so it must stay world readable: a root-owned 0600 file forces sudo on every read")
	}
	isolate(t)
	defer func(p string) { nativeStatePath = p }(nativeStatePath)
	nativeStatePath = filepath.Join(t.TempDir(), "etc", "traefik-manager", "tm-state.yml")
	a := answers.Defaults(answers.ModeTMNative)
	a.Finalize()
	st := New(a, "1.12.0", "")
	if st.Path != nativeStatePath || st.Dir != a.Native.InstallDir {
		t.Fatalf("native state path or dir wrong: %s %s", st.Path, st.Dir)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(nativeStatePath); err != nil {
		t.Fatal("state not written to the native path")
	}
}

func TestPathFor(t *testing.T) {
	if got := PathFor(&answers.Answers{Mode: answers.ModeFull, Dir: "/srv/stack"}); got != "/srv/stack/.tm/state.yml" {
		t.Fatalf("docker path wrong: %s", got)
	}
	if got := PathFor(&answers.Answers{Mode: answers.ModeTMNative}); got != "/etc/traefik-manager/tm-state.yml" {
		t.Fatalf("native path wrong: %s", got)
	}
	if got := PathFor(&answers.Answers{Mode: answers.ModeAgentBinary}); got != "/etc/traefik-manager-agent/tm-state.yml" {
		t.Fatalf("agent binary path wrong: %s", got)
	}
}

func TestModified(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	compose := writeTemp(t, dir, "docker-compose.yml", "services: {}\n")
	static := writeTemp(t, dir, "traefik/traefik.yml", "api: {}\n")
	abs := writeTemp(t, dir, "unit.service", "[Service]\n")
	st := New(fullAnswers(dir), "1.12.0", "docker compose")
	st.Own("docker-compose.yml", []byte("services: {}\n"))
	st.Own("traefik/traefik.yml", []byte("api: {}\n"))
	st.Own(abs, []byte("[Service]\n"))
	changed, err := st.Modified()
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("expected nothing modified, got %v", changed)
	}
	if err := os.WriteFile(compose, []byte("services:\n  x: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(static); err != nil {
		t.Fatal(err)
	}
	changed, err = st.Modified()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "docker-compose.yml,traefik/traefik.yml" {
		t.Fatalf("unexpected modified list %v", changed)
	}
}

func TestHash(t *testing.T) {
	if Hash([]byte("")) != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("sha256 hex mismatch")
	}
}
