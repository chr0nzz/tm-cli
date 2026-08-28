package wizard

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

func TestSectionsPerMode(t *testing.T) {
	want := map[answers.Mode][]Section{
		answers.ModeFull: {
			{"general", "General"}, {"deployment", "Deployment type"}, {"domain", "Domain"},
			{"tls", "TLS / Certificates"}, {"config", "Dynamic config"}, {"mounts", "Optional mounts"},
			{"crowdsec", "CrowdSec"}, {"network", "Docker network"},
		},
		answers.ModeTMDocker: {
			{"general", "General"}, {"network", "Network"}, {"access", "Access"},
			{"config", "Dynamic config"}, {"mounts", "Optional mounts"},
		},
		answers.ModeTMNative: {
			{"general", "General"}, {"user", "Service user"}, {"config", "Dynamic config"}, {"mounts", "Optional mounts"},
		},
		answers.ModeAgentDocker: {
			{"apikey", "API key"}, {"traefik", "Traefik connection"}, {"paths", "Optional paths"},
			{"restart", "Restart method"}, {"crowdsec", "CrowdSec"}, {"git", "Git backup"}, {"location", "Install location"},
		},
		answers.ModeAgentDockerTraefik: {
			{"apikey", "API key"}, {"traefik", "Traefik install"}, {"paths", "Optional paths"},
			{"restart", "Restart method"}, {"crowdsec", "CrowdSec"}, {"git", "Git backup"}, {"location", "Install location"},
		},
		answers.ModeAgentBinary: {
			{"apikey", "API key"}, {"traefik", "Traefik connection"}, {"paths", "Optional paths"},
			{"restart", "Restart method"}, {"crowdsec", "CrowdSec"}, {"git", "Git backup"},
		},
	}
	for _, mode := range answers.Modes {
		got := Sections(mode)
		if !reflect.DeepEqual(got, want[mode]) {
			t.Errorf("%s sections\n got %v\nwant %v", mode, got, want[mode])
		}
		for _, s := range sectionsFor(mode) {
			if s.run == nil {
				t.Errorf("%s section %s has no runner", mode, s.ID)
			}
		}
	}
	if Sections("bogus") != nil {
		t.Error("unknown mode should have no sections")
	}
}

func TestFindSectionAliases(t *testing.T) {
	cases := []struct {
		mode answers.Mode
		id   string
		want string
	}{
		{answers.ModeFull, "static", "mounts"},
		{answers.ModeFull, "restart", "mounts"},
		{answers.ModeFull, "crowdsec", "crowdsec"},
		{answers.ModeFull, " TLS ", "tls"},
		{answers.ModeFull, "dir", "general"},
		{answers.ModeTMDocker, "tls", "access"},
		{answers.ModeTMDocker, "static", "mounts"},
		{answers.ModeTMNative, "restart", "mounts"},
		{answers.ModeAgentDocker, "static", "traefik"},
		{answers.ModeAgentDocker, "dir", "location"},
		{answers.ModeAgentDockerTraefik, "tls", "traefik"},
		{answers.ModeAgentBinary, "crowdsec", "crowdsec"},
	}
	for _, c := range cases {
		s, err := findSection(c.mode, c.id)
		if err != nil {
			t.Errorf("%s %q: %v", c.mode, c.id, err)
			continue
		}
		if s.ID != c.want {
			t.Errorf("%s %q: got %s want %s", c.mode, c.id, s.ID, c.want)
		}
	}
	if _, err := findSection(answers.ModeAgentBinary, "location"); err == nil {
		t.Error("agent-binary has no location section")
	}
	if _, err := findSection(answers.ModeFull, "nope"); err == nil || !strings.Contains(err.Error(), "valid: general, deployment") {
		t.Errorf("unexpected error for unknown section: %v", err)
	}
}

func TestReviewLinesFull(t *testing.T) {
	a := answers.Defaults(answers.ModeFull)
	a.Dir = "/home/me/traefik-stack"
	a.Domain = "example.com"
	a.TLS = answers.TLS{Method: answers.TLSDNS, Provider: "cloudflare", Email: "me@example.com"}
	a.Mounts.StaticConfig = true
	a.Restart.Method = answers.RestartProxy
	a.CrowdSec.Mode = answers.CrowdSecConnect
	a.CrowdSec.LAPIURL = "http://10.0.0.5:8080"
	a.Finalize()
	want := []string{
		"   1  General               /home/me/traefik-stack",
		"   2  Deployment type       external (internet-facing)",
		"   3  Domain                example.com  dash:traefik.example.com  tm:manager.example.com",
		"   4  TLS / Certificates    Let's Encrypt DNS (cloudflare)  me@example.com",
		"   5  Dynamic config        Single file",
		"   6  Optional mounts       logs certs static(restart:proxy)",
		"   7  CrowdSec              connect  http://10.0.0.5:8080",
		"   8  Docker network        traefik-net  api:8080",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("full review\n got %q\nwant %q", got, want)
	}

	a.Deployment = answers.DeploymentInternal
	a.Dashboard = false
	a.TLS = answers.TLS{Method: answers.TLSNone}
	a.Config.Layout = answers.LayoutDirectory
	a.Mounts = answers.Mounts{}
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.Finalize()
	want = []string{
		"   1  General               /home/me/traefik-stack",
		"   2  Deployment type       internal (LAN / VPN)",
		"   3  Domain                example.com  dash:traefik.example.com  tm:manager.example.com  dashboard:off",
		"   4  TLS / Certificates    none (HTTP only)",
		"   5  Dynamic config        Directory",
		"   6  Optional mounts       logs",
		"   7  CrowdSec              install alongside",
		"   8  Docker network        traefik-net  api:8080",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("full review (variant)\n got %q\nwant %q", got, want)
	}

	a.TLS = answers.TLS{Method: answers.TLSHTTP, Email: "me@example.com"}
	a.Mounts = answers.Mounts{}
	a.CrowdSec = answers.CrowdSec{Mode: answers.CrowdSecNone}
	a.Finalize()
	if got := ReviewLines(a); got[3] != "   4  TLS / Certificates    Let's Encrypt HTTP  me@example.com" || got[5] != "   6  Optional mounts       (none)" || got[6] != "   7  CrowdSec              disabled" {
		t.Errorf("full review (http/none)\n got %q", got)
	}
}

func TestReviewLinesTMDocker(t *testing.T) {
	a := answers.Defaults(answers.ModeTMDocker)
	a.Dir = "/home/me/traefik-manager"
	a.Hosts.Manager = "manager.example.com"
	a.TLS = answers.TLS{Method: answers.TLSDNS, Provider: "route53", Email: "me@example.com"}
	a.Mounts.StaticConfig = true
	a.Restart.Method = answers.RestartSocket
	a.Finalize()
	want := []string{
		"   1  General               /home/me/traefik-manager",
		"   2  Network               traefik-net (existing)",
		"   3  Access                via Traefik  manager.example.com  TLS:http",
		"   4  Dynamic config        Single file",
		"   5  Optional mounts       logs certs static(restart:socket)",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("tm-docker review\n got %q\nwant %q", got, want)
	}
	a.TLS.Method = answers.TLSHTTP
	a.Finalize()
	if got := ReviewLines(a)[2]; got != "   3  Access                via Traefik  manager.example.com  TLS:http" {
		t.Errorf("tm-docker http access: %q", got)
	}
	a.TLS.Method = answers.TLSNone
	a.Finalize()
	if got := ReviewLines(a)[2]; got != "   3  Access                via Traefik  manager.example.com  no TLS" {
		t.Errorf("tm-docker no tls access: %q", got)
	}
	a.Access = answers.Access{ViaTraefik: false, Port: "5000"}
	a.Network = answers.Network{Name: "traefik-manager-net", External: false}
	a.Finalize()
	if got := ReviewLines(a); got[1] != "   2  Network               traefik-manager-net (new)" || got[2] != "   3  Access                host port :5000" {
		t.Errorf("tm-docker host port review\n got %q", got)
	}
}

func TestReviewLinesTMNative(t *testing.T) {
	a := answers.Defaults(answers.ModeTMNative)
	a.Config.Layout = answers.LayoutDirectory
	a.Mounts.StaticConfig = true
	a.Restart.TraefikSystemd = true
	a.Finalize()
	want := []string{
		"   1  General               /opt/traefik-manager  data:/var/lib/traefik-manager  :5000",
		"   2  Service user          dedicated (traefik-manager)",
		"   3  Dynamic config        Directory  /etc/traefik/conf.d",
		"   4  Optional mounts       certs logs static(restart:poison-pill)",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("tm-native review\n got %q\nwant %q", got, want)
	}
	a.Native.ServiceUser = false
	a.Config.Layout = answers.LayoutSingle
	a.Mounts.Certs = false
	a.Mounts.StaticConfig = false
	a.Finalize()
	want = []string{
		"   1  General               /opt/traefik-manager  data:/var/lib/traefik-manager  :5000",
		"   2  Service user          current user",
		"   3  Dynamic config        Single file  /etc/traefik/dynamic.yml",
		"   4  Optional mounts       logs",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("tm-native review (variant)\n got %q\nwant %q", got, want)
	}
}

func TestReviewLinesAgentDocker(t *testing.T) {
	a := answers.Defaults(answers.ModeAgentDocker)
	a.SetSecret(answers.SecretTMAAPIKey, "tma-api-key-PLACEHOLDER")
	a.Agent.TraefikURL = "https://traefik.example.com"
	a.Agent.InsecureTLS = true
	a.Mounts.StaticConfig = true
	a.Mounts.StaticConfigPath = "/etc/traefik/traefik.yml"
	a.Mounts.Certs = true
	a.Mounts.Plugins = true
	a.Restart.Method = answers.RestartProxy
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.Agent.Git.Enabled = true
	a.Agent.Git.Repo = "git@github.com:me/traefik-config.git"
	a.Finalize()
	want := []string{
		"   1  API key               tma-••••••••",
		"   2  Traefik connection    https://traefik.example.com  insecure-tls  static:traefik.yml",
		"   3  Optional paths        acme logs plugins",
		"   4  Restart method        proxy",
		"   5  CrowdSec              install alongside",
		"   6  Git backup            git@github.com:me/traefik-config.git",
		"   7  Install location      /opt/traefik-manager-agent  :8090",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("agent-docker review\n got %q\nwant %q", got, want)
	}

	b := answers.Defaults(answers.ModeAgentDocker)
	b.Finalize()
	want = []string{
		"   1  API key               (not set)",
		"   2  Traefik connection    http://traefik:8080",
		"   3  Optional paths        (none)",
		"   4  Restart method        (none)",
		"   5  CrowdSec              disabled",
		"   6  Git backup            disabled",
		"   7  Install location      /opt/traefik-manager-agent  :8090",
	}
	if got := ReviewLines(b); !reflect.DeepEqual(got, want) {
		t.Errorf("agent-docker defaults review\n got %q\nwant %q", got, want)
	}
	b.Agent.Git.Enabled = true
	b.CrowdSec.Mode = answers.CrowdSecConnect
	b.CrowdSec.LAPIURL = "http://crowdsec:8080"
	b.Finalize()
	if got := ReviewLines(b); got[4] != "   5  CrowdSec              connect  http://crowdsec:8080" || got[5] != "   6  Git backup            (no repo set)" {
		t.Errorf("agent-docker connect/git review\n got %q", got)
	}
}

func TestReviewLinesAgentBinary(t *testing.T) {
	a := answers.Defaults(answers.ModeAgentBinary)
	a.SetSecret(answers.SecretTMAAPIKey, "abc")
	a.Agent.Port = "8091"
	a.Finalize()
	want := []string{
		"   1  API key               ••••••••",
		"   2  Traefik connection    http://traefik:8080  :8091",
		"   3  Optional paths        (none)",
		"   4  Restart method        (none)",
		"   5  CrowdSec              disabled",
		"   6  Git backup            disabled",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("agent-binary review\n got %q\nwant %q", got, want)
	}
}

func TestReviewLinesAgentDockerTraefik(t *testing.T) {
	a := answers.Defaults(answers.ModeAgentDockerTraefik)
	a.SetSecret(answers.SecretTMAAPIKey, "tma-api-key-PLACEHOLDER")
	a.TLS = answers.TLS{Method: answers.TLSHTTP, Email: "me@example.com"}
	a.Hosts.Dashboard = "traefik.example.com"
	a.Hosts.Agent = "agent.example.com"
	a.Config.Layout = answers.LayoutDirectory
	a.Mounts.Plugins = true
	a.Restart.Method = answers.RestartPoisonPill
	a.Finalize()
	want := []string{
		"   1  API key               tma-••••••••",
		"   2  Traefik install       http  external  dash:traefik.example.com  tma:agent.example.com  net:traefik-net  Directory",
		"   3  Optional paths        acme logs plugins",
		"   4  Restart method        poison-pill",
		"   5  CrowdSec              disabled",
		"   6  Git backup            disabled",
		"   7  Install location      /opt/traefik-manager-agent  :8090",
	}
	if got := ReviewLines(a); !reflect.DeepEqual(got, want) {
		t.Errorf("agent-docker-traefik review\n got %q\nwant %q", got, want)
	}
	a.Deployment = answers.DeploymentInternal
	a.Dashboard = false
	a.Access.ViaTraefik = false
	a.TLS = answers.TLS{Method: answers.TLSNone}
	a.Config.Layout = answers.LayoutSingle
	a.Mounts.Plugins = false
	a.Finalize()
	if got := ReviewLines(a); got[1] != "   2  Traefik install       none  internal  net:traefik-net  Single" || got[2] != "   3  Optional paths        logs" {
		t.Errorf("agent-docker-traefik review (variant)\n got %q", got)
	}
}

func TestTLSChoiceRoundTrip(t *testing.T) {
	opts := tlsOptions()
	if len(opts) != 9 {
		t.Fatalf("expected 9 certificate options, got %d", len(opts))
	}
	if opts[0].Key != "Let's Encrypt - HTTP challenge (port 80 must be open)" || opts[0].Value != answers.TLSHTTP {
		t.Errorf("first option: %+v", opts[0])
	}
	if opts[1].Key != "Let's Encrypt - DNS challenge: Cloudflare" || opts[1].Value != "dns:cloudflare" {
		t.Errorf("cloudflare option: %+v", opts[1])
	}
	if opts[7].Value != "dns:other" || opts[8].Key != "No TLS (HTTP only)" || opts[8].Value != answers.TLSNone {
		t.Errorf("tail options: %+v %+v", opts[7], opts[8])
	}
	for _, want := range []answers.TLS{
		{Method: answers.TLSHTTP},
		{Method: answers.TLSNone},
		{Method: answers.TLSDNS, Provider: "cloudflare"},
		{Method: answers.TLSDNS, Provider: "desec"},
		{Method: answers.TLSDNS, Provider: answers.DNSProviderOther},
	} {
		var got answers.TLS
		applyTLSChoice(&got, tlsChoice(want))
		if got.Method != want.Method || got.Provider != want.Provider {
			t.Errorf("round trip %+v -> %q -> %+v", want, tlsChoice(want), got)
		}
	}
	var dflt answers.TLS
	applyTLSChoice(&dflt, "")
	if dflt.Method != answers.TLSHTTP {
		t.Errorf("empty choice should fall back to http, got %+v", dflt)
	}
	other := answers.TLS{Method: answers.TLSDNS, Provider: answers.DNSProviderOther, LegoProvider: "hetzner"}
	if legoProvider(other) != "hetzner" {
		t.Errorf("lego provider lookup: %q", legoProvider(other))
	}
	if legoProvider(answers.TLS{Method: answers.TLSDNS, Provider: "duckdns"}) != "duckdns" {
		t.Error("known provider should be returned as is")
	}
}

func TestSplitVarNames(t *testing.T) {
	got := splitVarNames(" hetzner_api_key, HETZNER_API_KEY ;OVH_ENDPOINT  ovh_application_key,")
	want := []string{"HETZNER_API_KEY", "OVH_ENDPOINT", "OVH_APPLICATION_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitVarNames: got %v want %v", got, want)
	}
	if splitVarNames("") != nil {
		t.Error("empty input should yield nil")
	}
	if err := validVarNames("HETZNER_API_KEY,OVH_ENDPOINT"); err != nil {
		t.Errorf("valid names rejected: %v", err)
	}
	if err := validVarNames(""); err == nil {
		t.Error("empty list accepted")
	}
	if err := validVarNames("1BAD,GOOD"); err == nil || !strings.Contains(err.Error(), "1BAD") {
		t.Errorf("bad name accepted: %v", err)
	}
}

func TestValidators(t *testing.T) {
	if err := validPort("8080"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "abc", "0", "65536", "80a"} {
		if err := validPort(bad); err == nil {
			t.Errorf("port %q accepted", bad)
		}
	}
	for _, good := range []string{"http://traefik:8080", "https://traefik.example.com", " http://x "} {
		if err := validURL(good); err != nil {
			t.Errorf("url %q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"", "traefik:8080", "https://", "ftp://x"} {
		if err := validURL(bad); err == nil {
			t.Errorf("url %q accepted", bad)
		}
	}
	if err := nonEmpty("a path is required")("  "); err == nil || err.Error() != "a path is required" {
		t.Errorf("nonEmpty: %v", err)
	}
	var v string
	acc := trimmed{p: &v}
	acc.Set("  /opt/x  ")
	if acc.Get() != "/opt/x" || v != "/opt/x" {
		t.Errorf("trimmed accessor: %q", v)
	}
	if orDefault("", "x") != "x" || orDefault("y", "x") != "y" {
		t.Error("orDefault")
	}
	prefilled := "8080"
	check := withDefault(&prefilled, validPort)
	if err := check(""); err != nil {
		t.Errorf("empty input must fall back to the pre-filled value: %v", err)
	}
	if err := check("9"); err != nil {
		t.Errorf("typed value rejected: %v", err)
	}
	if err := check("nope"); err == nil {
		t.Error("invalid typed value accepted")
	}
	prefilled = ""
	if err := check(""); err == nil {
		t.Error("empty input with no pre-filled value accepted")
	}
}

func TestAskSecretsWithoutKeys(t *testing.T) {
	if err := AskSecrets(nil, nil, answers.Defaults(answers.ModeAgentDocker), nil, nil); err != nil {
		t.Errorf("no keys should be a no-op: %v", err)
	}
	if err := AskSecrets(nil, nil, nil, []string{"X"}, nil); err == nil {
		t.Error("nil answers must fail")
	}
	if err := Review(nil, nil, nil, nil); err == nil {
		t.Error("nil answers must fail")
	}
	if err := RunSection(nil, nil, &answers.Answers{Mode: "bogus"}, "general", nil); err == nil {
		t.Error("invalid mode must fail")
	}
}

func TestSectionHeaders(t *testing.T) {
	cases := []struct {
		mode answers.Mode
		id   string
		want string
	}{
		{answers.ModeFull, "crowdsec", "CrowdSec IDS"},
		{answers.ModeFull, "network", "Docker Network"},
		{answers.ModeFull, "mounts", "Optional Mounts"},
		{answers.ModeTMDocker, "network", "Network"},
		{answers.ModeTMNative, "user", "Service User"},
		{answers.ModeAgentDocker, "restart", "Traefik restart"},
		{answers.ModeAgentDocker, "crowdsec", "CrowdSec (optional)"},
		{answers.ModeAgentBinary, "git", "Git backup (optional)"},
		{answers.ModeAgentDockerTraefik, "traefik", "Traefik install"},
	}
	for _, c := range cases {
		s, err := findSection(c.mode, c.id)
		if err != nil {
			t.Fatal(err)
		}
		if got := sectionHeader(c.mode, s.Section); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.mode, c.id, got, c.want)
		}
	}
}
