package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chr0nzz/traefik-stack/internal/answers"
)

func adopt(t *testing.T, fixture string) (*State, map[string]string, string) {
	t.Helper()
	isolate(t)
	dir := copyFixture(t, fixture)
	st, secrets, err := Adopt(dir)
	if err != nil {
		t.Fatalf("adopt %s: %v", fixture, err)
	}
	if st.Dir != dir || st.Answers.Dir != dir {
		t.Fatalf("dir not absolute install dir: %s / %s", st.Dir, st.Answers.Dir)
	}
	if !st.Adopted || st.TMVersion != AdoptedVersion || st.ComposeCmd != "" || st.InstalledAt.IsZero() {
		t.Fatalf("adopt metadata wrong: %+v", st)
	}
	want := filepath.Join(dir, ".tm", "state.yml")
	if st.Path != want {
		t.Fatalf("state path %s, want %s", st.Path, want)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatal("adoption must not write state: a read-only command would leave a file behind")
	}
	if registryHas(t, want) {
		t.Fatal("adoption must not touch the registry")
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("state not saved: %v", err)
	}
	for _, p := range placeholders {
		if strings.Contains(string(raw), p) {
			t.Fatalf("adopted state leaked a secret:\n%s", raw)
		}
	}
	compose, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if st.OwnedFiles["docker-compose.yml"] != Hash(compose) {
		t.Fatalf("compose hash not recorded: %v", st.OwnedFiles)
	}
	if _, err := os.Stat(filepath.Join(dir, "traefik", "traefik.yml")); err == nil {
		if _, ok := st.OwnedFiles["traefik/traefik.yml"]; !ok {
			t.Fatalf("traefik.yml present but not owned: %v", st.OwnedFiles)
		}
	} else if _, ok := st.OwnedFiles["traefik/traefik.yml"]; ok {
		t.Fatalf("traefik.yml owned but absent: %v", st.OwnedFiles)
	}
	loaded, err := Load(want)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Answers.Finalize()
	if !reflect.DeepEqual(loaded.Answers, st.Answers) {
		t.Fatalf("saved answers differ from adopted answers:\n%+v\n%+v", loaded.Answers, st.Answers)
	}
	again, err := st.LiteralSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, secrets) {
		t.Fatalf("LiteralSecrets %v differs from adopt result %v", again, secrets)
	}
	return st, secrets, dir
}

func wantSecrets(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("secrets: got %v, want %v", got, want)
	}
}

func TestAdoptFull(t *testing.T) {
	st, secrets, _ := adopt(t, "full")
	a := st.Answers
	if st.Mode != answers.ModeFull || a.Mode != answers.ModeFull {
		t.Fatalf("mode %s", st.Mode)
	}
	if a.Domain != "example.com" || a.Hosts.Dashboard != "traefik.example.com" || a.Hosts.Manager != "manager.example.com" || !a.Dashboard {
		t.Fatalf("hosts: %+v domain %q dashboard %v", a.Hosts, a.Domain, a.Dashboard)
	}
	if a.TLS.Method != answers.TLSDNS || a.TLS.Provider != "cloudflare" || a.TLS.Email != "me@example.com" {
		t.Fatalf("tls: %+v", a.TLS)
	}
	if a.Config.Layout != answers.LayoutDirectory {
		t.Fatalf("layout %s", a.Config.Layout)
	}
	if !a.Mounts.AccessLogs || !a.Mounts.Certs || !a.Mounts.StaticConfig || a.Mounts.Plugins {
		t.Fatalf("mounts: %+v", a.Mounts)
	}
	if a.Restart.Method != answers.RestartPoisonPill || a.Restart.SignalFile != "/signals/restart.sig" || a.Restart.Container != "traefik" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.CrowdSec.Mode != answers.CrowdSecInstall || a.CrowdSec.LAPIURL != answers.DefaultLAPIURL || a.CrowdSec.MachineID != "traefik-manager" {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	if a.Network.Name != "traefik-net" || a.Network.External || a.Network.TraefikAPIPort != "8080" {
		t.Fatalf("network: %+v", a.Network)
	}
	if a.Deployment != answers.DeploymentInternal {
		t.Fatalf("deployment %s: a dns challenge is no evidence the host is internet-facing", a.Deployment)
	}
	wantSecrets(t, secrets, map[string]string{
		"CF_DNS_API_TOKEN":          "cf-dns-api-token-PLACEHOLDER",
		"CROWDSEC_API_KEY":          "crowdsec-api-key-PLACEHOLDER",
		"CROWDSEC_MACHINE_PASSWORD": "crowdsec-machine-password-PLACEHOLDER",
	})
	if err := a.Validate(); err != nil {
		t.Fatalf("adopted full answers should validate: %v", err)
	}
}

func TestAdoptFullPlain(t *testing.T) {
	st, secrets, _ := adopt(t, "full-plain")
	a := st.Answers
	if st.Mode != answers.ModeFull {
		t.Fatalf("mode %s", st.Mode)
	}
	if a.Domain != "lab.internal" || a.Hosts.Manager != "tm.lab.internal" || a.Hosts.Dashboard != "traefik.lab.internal" || a.Dashboard {
		t.Fatalf("hosts: %+v domain %q dashboard %v", a.Hosts, a.Domain, a.Dashboard)
	}
	if a.TLS.Method != answers.TLSNone || a.TLS.Email != "" || a.TLS.Provider != "" {
		t.Fatalf("tls: %+v", a.TLS)
	}
	if a.Config.Layout != answers.LayoutSingle {
		t.Fatalf("layout %s", a.Config.Layout)
	}
	if a.Mounts.AccessLogs || a.Mounts.Certs || a.Mounts.StaticConfig {
		t.Fatalf("mounts: %+v", a.Mounts)
	}
	if a.Restart.Method != answers.RestartNone || a.CrowdSec.Mode != answers.CrowdSecNone || a.CrowdSec.MachineID != "" {
		t.Fatalf("restart %+v crowdsec %+v", a.Restart, a.CrowdSec)
	}
	if a.Network.Name != "proxy" || a.Network.TraefikAPIPort != "18080" {
		t.Fatalf("network: %+v", a.Network)
	}
	wantSecrets(t, secrets, map[string]string{})
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptFullConnect(t *testing.T) {
	st, secrets, _ := adopt(t, "full-connect")
	a := st.Answers
	if a.TLS.Method != answers.TLSHTTP || a.TLS.Email != "ops@example.org" || a.TLS.Provider != "" {
		t.Fatalf("tls: %+v", a.TLS)
	}
	if a.Restart.Method != answers.RestartProxy || a.Restart.DockerHost != "tcp://socket-proxy:2375" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.CrowdSec.Mode != answers.CrowdSecConnect || a.CrowdSec.LAPIURL != "http://10.0.0.5:8080" || a.CrowdSec.MachineID != "tm-box" {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	if a.Network.Name != "traefik-net" || a.Network.External {
		t.Fatalf("network must skip socket-proxy-net: %+v", a.Network)
	}
	if !a.Mounts.AccessLogs || a.Mounts.Certs || !a.Mounts.StaticConfig {
		t.Fatalf("mounts: %+v", a.Mounts)
	}
	wantSecrets(t, secrets, map[string]string{
		"CROWDSEC_API_KEY":          "crowdsec-api-key-PLACEHOLDER",
		"CROWDSEC_MACHINE_PASSWORD": "crowdsec-machine-password-PLACEHOLDER",
	})
	keys := strings.Join(a.SecretKeys(), ",")
	if keys != "CROWDSEC_API_KEY,CROWDSEC_MACHINE_PASSWORD" {
		t.Fatalf("secret keys %s", keys)
	}
}

func TestAdoptTMDocker(t *testing.T) {
	st, secrets, _ := adopt(t, "tm-docker")
	a := st.Answers
	if st.Mode != answers.ModeTMDocker {
		t.Fatalf("mode %s", st.Mode)
	}
	if a.Hosts.Manager != "manager.example.com" || !a.Access.ViaTraefik || a.Access.Port != "" {
		t.Fatalf("access: %+v hosts %+v", a.Access, a.Hosts)
	}
	if a.TLS.Method != answers.TLSHTTP {
		t.Fatalf("tls: %+v", a.TLS)
	}
	if a.Network.Name != "traefik-net" || !a.Network.External {
		t.Fatalf("network: %+v", a.Network)
	}
	if a.Config.Layout != answers.LayoutSingle {
		t.Fatalf("layout %s", a.Config.Layout)
	}
	m := a.Mounts
	if !m.AccessLogs || m.AccessLogPath != "/srv/traefik/logs/access.log" || !m.Certs || m.AcmePath != "/srv/traefik/acme.json" || !m.StaticConfig || m.StaticConfigPath != "/srv/traefik/traefik.yml" {
		t.Fatalf("mounts: %+v", m)
	}
	if a.Restart.Method != answers.RestartSocket || a.Restart.Container != "traefik-prod" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.CrowdSec.Mode != answers.CrowdSecNone {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	wantSecrets(t, secrets, map[string]string{})
}

func TestAdoptTMDockerHostPort(t *testing.T) {
	st, _, _ := adopt(t, "tm-docker-port")
	a := st.Answers
	if a.Access.ViaTraefik || a.Access.Port != "5050" || a.TLS.Method != answers.TLSNone {
		t.Fatalf("access: %+v tls %+v", a.Access, a.TLS)
	}
	if a.Network.Name != "traefik-manager-net" || a.Network.External {
		t.Fatalf("network: %+v", a.Network)
	}
	if a.Config.Layout != answers.LayoutDirectory {
		t.Fatalf("layout %s", a.Config.Layout)
	}
	if a.Mounts.AccessLogs || a.Mounts.Certs || a.Mounts.StaticConfig || a.Restart.Method != answers.RestartNone {
		t.Fatalf("mounts %+v restart %+v", a.Mounts, a.Restart)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptTMDockerProxy(t *testing.T) {
	st, _, _ := adopt(t, "tm-docker-proxy")
	a := st.Answers
	if !a.Access.ViaTraefik || a.Hosts.Manager != "tm.lab.internal" || a.TLS.Method != answers.TLSNone {
		t.Fatalf("access: %+v hosts %+v tls %+v", a.Access, a.Hosts, a.TLS)
	}
	if !a.Mounts.StaticConfig || a.Mounts.StaticConfigPath != "/etc/traefik/traefik.yml" || a.Mounts.AccessLogs || a.Mounts.Certs {
		t.Fatalf("mounts: %+v", a.Mounts)
	}
	if a.Restart.Method != answers.RestartProxy || a.Restart.DockerHost != "tcp://socket-proxy:2375" || a.Restart.Container != "traefik" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.Network.Name != "traefik-net" || !a.Network.External {
		t.Fatalf("network: %+v", a.Network)
	}
}

func TestAdoptTMDockerLongSyntax(t *testing.T) {
	st, secrets, _ := adopt(t, "tm-docker-long")
	a := st.Answers
	if a.Access.ViaTraefik || a.Access.Port != "5100" {
		t.Fatalf("access: %+v", a.Access)
	}
	if a.Config.Layout != answers.LayoutDirectory {
		t.Fatalf("layout %s", a.Config.Layout)
	}
	if !a.Mounts.StaticConfig || a.Mounts.StaticConfigPath != "/srv/traefik/traefik.yml" {
		t.Fatalf("mounts: %+v", a.Mounts)
	}
	if a.Restart.Method != answers.RestartSocket {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.Network.Name != "proxy" || !a.Network.External {
		t.Fatalf("network: %+v", a.Network)
	}
	wantSecrets(t, secrets, map[string]string{})
}

func TestAdoptAgentDocker(t *testing.T) {
	st, secrets, _ := adopt(t, "agent-docker")
	a := st.Answers
	if st.Mode != answers.ModeAgentDocker {
		t.Fatalf("mode %s", st.Mode)
	}
	ag := a.Agent
	if ag.TraefikURL != "https://traefik.example.com:8443" || !ag.InsecureTLS || !ag.BasicAuth || ag.BasicAuthUser != "admin" || ag.ConfigPath != "/etc/traefik/dynamic" || ag.Port != "8091" {
		t.Fatalf("agent: %+v", ag)
	}
	m := a.Mounts
	if !m.Certs || m.AcmePath != "/etc/traefik/acme.json" || !m.AccessLogs || m.AccessLogPath != "/var/log/traefik/access.log" || !m.StaticConfig || m.StaticConfigPath != "/etc/traefik/traefik.yml" || !m.Plugins || m.PluginsDir != "/etc/traefik/plugins" {
		t.Fatalf("mounts: %+v", m)
	}
	if a.Restart.Method != answers.RestartProxy || a.Restart.Container != "traefik-main" || a.Restart.DockerHost != "tcp://socket-proxy:2375" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.CrowdSec.Mode != answers.CrowdSecConnect || a.CrowdSec.LAPIURL != "http://10.0.0.5:8080" || a.CrowdSec.MachineID != "" {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	g := ag.Git
	if !g.Enabled || g.Repo != "https://github.com/example/traefik-config.git" || g.Branch != "backup" || g.User != "deploy" || g.AutoPush {
		t.Fatalf("git: %+v", g)
	}
	wantSecrets(t, secrets, map[string]string{
		"TMA_API_KEY":          "tma-api-key-PLACEHOLDER",
		"TRAEFIK_API_PASSWORD": "traefik-api-password-PLACEHOLDER",
		"CROWDSEC_API_KEY":     "crowdsec-api-key-PLACEHOLDER",
		"GIT_BACKUP_TOKEN":     "git-backup-token-PLACEHOLDER",
	})
}

func TestAdoptAgentDockerInstall(t *testing.T) {
	st, secrets, _ := adopt(t, "agent-docker-install")
	a := st.Answers
	if a.CrowdSec.Mode != answers.CrowdSecInstall || a.CrowdSec.LAPIURL != answers.DefaultLAPIURL {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	if a.Restart.Method != answers.RestartPoisonPill || a.Restart.SignalFile != "/signals/restart.sig" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if !a.Mounts.AccessLogs || a.Mounts.AccessLogPath != "/var/log/traefik/access.log" || a.Mounts.Certs || a.Mounts.StaticConfig || a.Mounts.Plugins {
		t.Fatalf("mounts: %+v", a.Mounts)
	}
	if a.Agent.Port != "8090" || a.Agent.ConfigPath != "/app/config" || a.Agent.BasicAuth || a.Agent.Git.Enabled {
		t.Fatalf("agent: %+v", a.Agent)
	}
	wantSecrets(t, secrets, map[string]string{
		"TMA_API_KEY":      "tma-api-key-PLACEHOLDER",
		"CROWDSEC_API_KEY": "crowdsec-api-key-PLACEHOLDER",
	})
}

func TestAdoptAgentDockerReferencesAreNotSecrets(t *testing.T) {
	st, secrets, _ := adopt(t, "agent-docker-refs")
	a := st.Answers
	if !a.Agent.BasicAuth || a.Agent.BasicAuthUser != "admin" || a.CrowdSec.Mode != answers.CrowdSecConnect || !a.Agent.Git.Enabled {
		t.Fatalf("answers: %+v", a)
	}
	wantSecrets(t, secrets, map[string]string{})
	keys := strings.Join(a.SecretKeys(), ",")
	if keys != "CROWDSEC_API_KEY,TMA_API_KEY,TRAEFIK_API_PASSWORD,GIT_BACKUP_TOKEN" {
		t.Fatalf("secret keys %s", keys)
	}
}

func TestAdoptAgentDockerTraefik(t *testing.T) {
	st, secrets, _ := adopt(t, "agent-docker-traefik")
	a := st.Answers
	if st.Mode != answers.ModeAgentDockerTraefik {
		t.Fatalf("mode %s", st.Mode)
	}
	if !a.Dashboard || a.Hosts.Dashboard != "traefik.example.com" || a.Hosts.Agent != "agent.example.com" || !a.Access.ViaTraefik {
		t.Fatalf("hosts %+v dashboard %v access %+v", a.Hosts, a.Dashboard, a.Access)
	}
	if a.TLS.Method != answers.TLSHTTP || a.TLS.Email != "me@example.com" {
		t.Fatalf("tls: %+v", a.TLS)
	}
	if a.Config.Layout != answers.LayoutDirectory {
		t.Fatalf("layout %s", a.Config.Layout)
	}
	if a.Network.Name != "edge" || a.Network.External || a.Network.TraefikAPIPort != "8081" {
		t.Fatalf("network: %+v", a.Network)
	}
	ag := a.Agent
	if ag.TraefikURL != "http://traefik:8081" || ag.ConfigPath != answers.AgentConfigPathTraefik || ag.Port != "8090" || ag.BasicAuth || ag.InsecureTLS {
		t.Fatalf("agent: %+v", ag)
	}
	m := a.Mounts
	if !m.AccessLogs || !m.Certs || m.StaticConfig || !m.Plugins || m.PluginsDir != "/etc/traefik/plugins" {
		t.Fatalf("mounts: %+v", m)
	}
	if a.Restart.Method != answers.RestartPoisonPill || a.Restart.SignalFile != "/signals/restart.sig" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.CrowdSec.Mode != answers.CrowdSecInstall {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	wantSecrets(t, secrets, map[string]string{
		"TMA_API_KEY":      "tma-api-key-PLACEHOLDER",
		"CROWDSEC_API_KEY": "crowdsec-api-key-PLACEHOLDER",
	})
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptAgentDockerTraefikDNS(t *testing.T) {
	st, secrets, _ := adopt(t, "agent-docker-traefik-dns")
	a := st.Answers
	if a.Dashboard || a.Hosts.Dashboard != "" || a.Hosts.Agent != "" || a.Access.ViaTraefik {
		t.Fatalf("hosts %+v dashboard %v access %+v", a.Hosts, a.Dashboard, a.Access)
	}
	if a.TLS.Method != answers.TLSDNS || a.TLS.Provider != "route53" || a.TLS.Email != "dns@example.com" {
		t.Fatalf("tls: %+v", a.TLS)
	}
	if a.TLS.Vars["AWS_REGION"] != "eu-west-1" {
		t.Fatalf("non-secret dns var not kept: %+v", a.TLS.Vars)
	}
	if a.Config.Layout != answers.LayoutSingle {
		t.Fatalf("layout %s", a.Config.Layout)
	}
	if a.Restart.Method != answers.RestartSocket || a.Restart.Container != "traefik" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.CrowdSec.Mode != answers.CrowdSecNone || a.Mounts.Plugins {
		t.Fatalf("crowdsec %+v mounts %+v", a.CrowdSec, a.Mounts)
	}
	if a.Network.Name != "traefik-net" || a.Network.TraefikAPIPort != "8080" || a.Agent.TraefikURL != "http://traefik:8080" {
		t.Fatalf("network %+v agent %+v", a.Network, a.Agent)
	}
	wantSecrets(t, secrets, map[string]string{
		"TMA_API_KEY":           "tma-api-key-PLACEHOLDER",
		"AWS_ACCESS_KEY_ID":     "aws-access-key-id-PLACEHOLDER",
		"AWS_SECRET_ACCESS_KEY": "aws-secret-access-key-PLACEHOLDER",
	})
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptNotAdoptable(t *testing.T) {
	isolate(t)
	dir := copyFixture(t, "not-adoptable")
	_, _, err := Adopt(dir)
	if !errors.Is(err, ErrNotAdoptable) {
		t.Fatalf("expected ErrNotAdoptable, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tm", "state.yml")); err == nil {
		t.Fatal("state must not be written for a foreign compose")
	}
	paths, _ := Registry()
	if len(paths) != 0 {
		t.Fatalf("foreign compose registered: %v", paths)
	}
	_, _, err = Adopt(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound without a compose file, got %v", err)
	}
}

func TestImageName(t *testing.T) {
	cases := map[string]string{
		"traefik:latest":                         "traefik",
		"traefik":                                "traefik",
		"ghcr.io/chr0nzz/traefik-manager:latest": "traefik-manager",
		"ghcr.io/chr0nzz/traefik-manager-agent:1.5.0": "traefik-manager-agent",
		"localhost:5000/traefik-manager":              "traefik-manager",
		"localhost:5000/traefik-manager:v1":           "traefik-manager",
		"crowdsecurity/crowdsec@sha256:abc":           "crowdsec",
	}
	for in, want := range cases {
		if got := imageName(in); got != want {
			t.Fatalf("imageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"manager.example.com":   "example.com",
		"manager.example.co.uk": "example.co.uk",
		"example.com":           "example.com",
		"localhost":             "localhost",
	}
	for in, want := range cases {
		if got := domainOf(in); got != want {
			t.Fatalf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRouterHostAcceptsQuotes(t *testing.T) {
	s := &service{Labels: kvList{{Key: "traefik.http.routers.tma.rule", Value: `Host("agent.example.com") || Host("x")`}}}
	if h, ok := s.routerHost("tma"); !ok || h != "agent.example.com" {
		t.Fatalf("got %q %v", h, ok)
	}
}
