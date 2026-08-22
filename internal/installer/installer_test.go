package installer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/state"
)

func TestMergeEnvKeepsExistingAndUpdates(t *testing.T) {
	existing := "CF_DNS_API_TOKEN=old\nCUSTOM=keep\n\nGIT_BACKUP_TOKEN=\n"
	rendered := "CF_DNS_API_TOKEN=new\nCROWDSEC_API_KEY=abc\n"
	got := mergeEnv(existing, rendered)
	want := "CF_DNS_API_TOKEN=new\nCUSTOM=keep\nGIT_BACKUP_TOKEN=\nCROWDSEC_API_KEY=abc\n"
	if got != want {
		t.Fatalf("merge mismatch:\n%s\nwant:\n%s", got, want)
	}
}

func TestPasswordRegex(t *testing.T) {
	logs := "something\n=== AUTO-GENERATED PASSWORD ===\nUsername: admin\nPassword: s3cr3t-pw\n===\n"
	m := passwordRe.FindStringSubmatch(logs)
	if m == nil || m[1] != "s3cr3t-pw" {
		t.Fatalf("password not parsed: %v", m)
	}
	if passwordRe.FindStringSubmatch("no password here") != nil {
		t.Fatal("false positive")
	}
}

func TestServiceNames(t *testing.T) {
	a := answers.Defaults(answers.ModeFull)
	a.Mounts.StaticConfig = true
	a.Restart.Method = answers.RestartProxy
	a.CrowdSec.Mode = answers.CrowdSecInstall
	got := strings.Join(serviceNames(a), ",")
	if got != "traefik,traefik-manager,socket-proxy,crowdsec" {
		t.Fatalf("unexpected services %s", got)
	}
	b := answers.Defaults(answers.ModeAgentDocker)
	if got := strings.Join(serviceNames(b), ","); got != "traefik-manager-agent" {
		t.Fatalf("unexpected agent services %s", got)
	}
}

func TestExistingSecretsFromEnvAndCompose(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_REGISTRY", filepath.Join(dir, "registry.yml"))
	compose := `services:
  traefik:
    image: traefik:latest
    environment:
      - CF_DNS_API_TOKEN=literal-cf
  traefik-manager:
    image: ghcr.io/chr0nzz/traefik-manager:latest
    environment:
      - CROWDSEC_API_KEY=${CROWDSEC_API_KEY}
      - CROWDSEC_MACHINE_PASSWORD=literal-machine
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("CROWDSEC_API_KEY=from-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := answers.Defaults(answers.ModeFull)
	a.Dir = dir
	a.Domain = "example.com"
	a.TLS = answers.TLS{Method: answers.TLSDNS, Provider: "cloudflare", Email: "a@example.com"}
	a.CrowdSec.Mode = answers.CrowdSecConnect
	a.CrowdSec.MachineID = "traefik-manager"
	a.Finalize()
	st := state.New(a, "test", "docker compose")
	in := &Installer{}
	got := in.ExistingSecrets(st)
	if got["CROWDSEC_API_KEY"] != "from-env" {
		t.Fatalf(".env value must win: %v", got)
	}
	if got["CF_DNS_API_TOKEN"] != "literal-cf" || got["CROWDSEC_MACHINE_PASSWORD"] != "literal-machine" {
		t.Fatalf("literal compose secrets not recovered: %v", got)
	}
}

func TestResolvePath(t *testing.T) {
	if resolvePath("/base", "a/b") != "/base/a/b" || resolvePath("/base", "/etc/x") != "/etc/x" {
		t.Fatal("resolvePath")
	}
}

func TestKeptFileStaysOwnedSoItIsAskedAboutAgain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_REGISTRY", filepath.Join(dir, "registry.yml"))
	a := answers.Defaults(answers.ModeTMDocker)
	a.Dir = dir
	a.Access.ViaTraefik = false
	a.Access.Port = "5000"
	a.Finalize()
	st := state.New(a, "test", "docker compose")
	original := "services: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	st.OwnedFiles = map[string]string{"docker-compose.yml": state.Hash([]byte(original))}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(original+"# hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modified, err := st.Modified()
	if err != nil || len(modified) != 1 {
		t.Fatalf("expected the edit to be detected: %v %v", modified, err)
	}
	prevOwned := st.OwnedFiles
	st.OwnedFiles = map[string]string{}
	overwrite := map[string]bool{"docker-compose.yml": false}
	for path, keep := range overwrite {
		if keep {
			continue
		}
		if h, ok := prevOwned[path]; ok {
			st.OwnedFiles[path] = h
		}
	}
	if st.OwnedFiles["docker-compose.yml"] != prevOwned["docker-compose.yml"] {
		t.Fatal("a kept file must retain its recorded hash so the next reconfigure asks again")
	}
	again, err := st.Modified()
	if err != nil || len(again) != 1 {
		t.Fatalf("kept file must still read as modified: %v %v", again, err)
	}
}

func TestProbeAcceptsBodiesWithoutAnOkField(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
		ok   bool
		ver  string
	}{
		{"traefik version", `{"Version":"3.6.1","Codename":"chevrotin"}`, 200, true, "3.6.1"},
		{"tm health", `{"ok":true}`, 200, true, ""},
		{"agent health", `{"ok":true,"version":"1.11.0"}`, 200, true, "1.11.0"},
		{"explicit not ok", `{"ok":false}`, 200, false, ""},
		{"not json", `hello`, 200, true, ""},
		{"server error", `{"ok":true}`, 500, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.code)
				fmt.Fprint(w, c.body)
			}))
			defer srv.Close()
			h := probe(context.Background(), srv.URL, "")
			if h.OK != c.ok {
				t.Fatalf("ok = %v, want %v (err %q)", h.OK, c.ok, h.Err)
			}
			if c.ok && h.Version != c.ver {
				t.Fatalf("version = %q, want %q", h.Version, c.ver)
			}
		})
	}
}

func TestExternalFacing(t *testing.T) {
	full := answers.Defaults(answers.ModeFull)
	full.Deployment = answers.DeploymentInternal
	if externalFacing(full) {
		t.Fatal("full stack must follow the deployment answer")
	}
	full.Deployment = answers.DeploymentExternal
	if !externalFacing(full) {
		t.Fatal("full stack marked external must be external")
	}
	tmd := answers.Defaults(answers.ModeTMDocker)
	tmd.TLS = answers.TLS{Method: answers.TLSNone}
	if externalFacing(tmd) {
		t.Fatal("tm-docker without TLS has no evidence of being internet-facing")
	}
	tmd.TLS = answers.TLS{Method: answers.TLSHTTP}
	if !externalFacing(tmd) {
		t.Fatal("an http challenge implies the host is reachable from the internet")
	}
}
