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

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/state"
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

func TestFullNativeURLsAndAcmePath(t *testing.T) {
	a := answers.Defaults(answers.ModeFullNative)
	a.Domain = "example.com"
	a.TLS.Email = "a@example.com"
	a.Finalize()
	st := state.New(a, "test", "")
	in := &Installer{}
	urls := in.URLs(st)
	if len(urls) != 2 || urls[0][0] != "Traefik dashboard" || urls[0][1] != "https://traefik.example.com" {
		t.Fatalf("urls = %v", urls)
	}
	if urls[1][0] != "Traefik Manager" || !strings.HasPrefix(urls[1][1], "http://") || !strings.HasSuffix(urls[1][1], ":5000") {
		t.Fatalf("tm url = %v", urls[1])
	}
	if got := acmePath(st); got != answers.NativeAcmePath {
		t.Fatalf("acme path = %q", got)
	}
	b := answers.Defaults(answers.ModeFullNative)
	b.TLS = answers.TLS{Method: answers.TLSNone}
	b.Finalize()
	st2 := state.New(b, "test", "")
	if urls := in.URLs(st2); len(urls) != 1 || urls[0][0] != "Traefik Manager" {
		t.Fatalf("urls without a dashboard host = %v", urls)
	}
	if got := acmePath(st2); got != "" {
		t.Fatalf("no acme path expected without tls, got %q", got)
	}
}

func TestStaticDeclaresPlugins(t *testing.T) {
	dir := t.TempDir()
	with := filepath.Join(dir, "with.yml")
	without := filepath.Join(dir, "without.yml")
	if err := os.WriteFile(with, []byte("experimental:\n  plugins:\n    crowdsec:\n      moduleName: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(without, []byte("api:\n  dashboard: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !staticDeclaresPlugins(with) {
		t.Fatal("declared plugins not detected")
	}
	if staticDeclaresPlugins(without) {
		t.Fatal("false positive without plugins")
	}
	if staticDeclaresPlugins(filepath.Join(dir, "missing.yml")) {
		t.Fatal("a missing file declares nothing")
	}
}

func TestCscliCommandsCarryTheSuppliedCredentials(t *testing.T) {
	bouncer := cscliBouncerAddArgs("traefik-manager", "bouncer-key")
	if strings.Join(bouncer, " ") != "cscli bouncers add traefik-manager --key bouncer-key" {
		t.Errorf("bouncer add: %v", bouncer)
	}
	del := cscliBouncerDeleteArgs("traefik-manager")
	if strings.Join(del, " ") != "cscli bouncers delete traefik-manager" {
		t.Errorf("bouncer delete: %v", del)
	}
	machine := cscliMachineAddArgs("traefik-manager", "machine-pw")
	if strings.Join(machine, " ") != "cscli machines add traefik-manager --password machine-pw --force" {
		t.Errorf("machine add: %v", machine)
	}
	if strings.Join(cscliCollectionArgs("crowdsecurity/traefik"), " ") != "cscli collections install crowdsecurity/traefik" {
		t.Errorf("collection: %v", cscliCollectionArgs("crowdsecurity/traefik"))
	}
}

func TestCrowdSecBouncerNamePerMode(t *testing.T) {
	cases := map[answers.Mode]string{
		answers.ModeFullNative:  "traefik-manager",
		answers.ModeTMNative:    "traefik-manager",
		answers.ModeAgentBinary: "tma",
	}
	for mode, want := range cases {
		if got := crowdsecBouncerName(mode); got != want {
			t.Errorf("%s bouncer name %q, want %q", mode, got, want)
		}
	}
}

func TestNativeCrowdSecNeeded(t *testing.T) {
	cases := []struct {
		mode answers.Mode
		csm  string
		want bool
	}{
		{answers.ModeTMNative, answers.CrowdSecInstall, true},
		{answers.ModeFullNative, answers.CrowdSecInstall, true},
		{answers.ModeAgentBinary, answers.CrowdSecInstall, true},
		{answers.ModeTMNative, answers.CrowdSecConnect, false},
		{answers.ModeTMNative, answers.CrowdSecNone, false},
		{answers.ModeTMDocker, answers.CrowdSecInstall, false},
		{answers.ModeFull, answers.CrowdSecInstall, false},
	}
	for _, c := range cases {
		a := answers.Defaults(c.mode)
		a.CrowdSec.Mode = c.csm
		if got := nativeCrowdSecNeeded(a); got != c.want {
			t.Errorf("%s/%s = %v, want %v", c.mode, c.csm, got, c.want)
		}
	}
}

func TestServiceNamesIncludeCrowdSec(t *testing.T) {
	a := answers.Defaults(answers.ModeTMDocker)
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.Finalize()
	if got := strings.Join(serviceNames(a), ","); got != "traefik-manager,crowdsec" {
		t.Errorf("tm-docker services: %s", got)
	}
}
