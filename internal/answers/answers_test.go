package answers

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseKeepsDefaultsForAbsentFields(t *testing.T) {
	a, err := Parse([]byte("mode: full\ndomain: example.com\nmounts:\n  static_config: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Mounts.AccessLogs || !a.Mounts.Certs {
		t.Fatalf("absent mount bools lost their defaults: %+v", a.Mounts)
	}
	if !a.Mounts.StaticConfig {
		t.Fatal("present bool not applied")
	}
	if !a.Dashboard || a.TLS.Method != TLSHTTP || a.Network.Name != DefaultNetwork {
		t.Fatalf("defaults missing: %+v", a)
	}
}

func TestParseExplicitFalseSurvives(t *testing.T) {
	a, err := Parse([]byte("mode: tm-docker\naccess:\n  via_traefik: false\n  port: '5000'\nnetwork:\n  external: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Access.ViaTraefik || a.Network.External {
		t.Fatalf("explicit false overridden: %+v %+v", a.Access, a.Network)
	}
}

func TestDumpRoundTrip(t *testing.T) {
	a := Defaults(ModeFull)
	a.Domain = "example.com"
	a.Mounts.AccessLogs = false
	a.Access.ViaTraefik = false
	a.SetSecret(SecretTMAAPIKey, "x")
	a.Finalize()
	out, err := a.Dump()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "secrets") || strings.Contains(s, "native:") || strings.Contains(s, "access:") {
		t.Fatalf("dump leaked irrelevant sections or secrets:\n%s", s)
	}
	if !strings.HasPrefix(s, "mode: full\n") {
		t.Fatalf("mode must come first:\n%s", s)
	}
	b, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if b.Mounts.AccessLogs || b.Hosts.Manager != "manager.example.com" || b.Dir != a.Dir {
		t.Fatalf("round trip changed values: %+v", b)
	}
}

func TestValidate(t *testing.T) {
	a := Defaults(ModeFull)
	if err := a.Validate(); err == nil {
		t.Fatal("expected domain error")
	}
	a.Domain = "example.com"
	a.TLS.Email = "me@example.com"
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	b := Defaults(ModeAgentBinary)
	b.CrowdSec.Mode = CrowdSecInstall
	b.SetSecret(SecretTMAAPIKey, "k")
	b.SetSecret(SecretCrowdSecAPIKey, "k")
	b.Finalize()
	if err := b.Validate(); err != nil {
		t.Fatalf("the binary agent installs crowdsec natively now: %v", err)
	}
	b.Agent.TraefikURL = NativeLAPIURL
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "the address the CrowdSec LAPI listens on") {
		t.Fatalf("a traefik api on the lapi port must be rejected: %v", err)
	}
}

func TestSecrets(t *testing.T) {
	a := Defaults(ModeFull)
	a.TLS.Method = TLSDNS
	a.TLS.Provider = "route53"
	a.CrowdSec.Mode = CrowdSecInstall
	a.Finalize()
	keys := strings.Join(a.SecretKeys(), ",")
	if keys != "AWS_ACCESS_KEY_ID,AWS_SECRET_ACCESS_KEY,CROWDSEC_API_KEY,CROWDSEC_MACHINE_PASSWORD" {
		t.Fatalf("unexpected secret keys %s", keys)
	}
	if got := a.MissingSecrets(); len(got) != 2 {
		t.Fatalf("expected 2 missing, got %v", got)
	}
	if err := a.GenerateSecrets(); err != nil {
		t.Fatal(err)
	}
	if len(a.Secrets[SecretCrowdSecAPIKey]) != 64 || len(a.Secrets[SecretCrowdSecMachinePassword]) != 48 {
		t.Fatalf("generated lengths wrong: %v", a.Secrets)
	}
}

func TestNestedDecodeKeepsDefaults(t *testing.T) {
	var wrapper struct {
		Answers Answers `yaml:"answers"`
	}
	if err := yaml.Unmarshal([]byte("answers:\n  mode: tm-docker\n  hosts:\n    manager: m.example.com\n"), &wrapper); err != nil {
		t.Fatal(err)
	}
	a := wrapper.Answers
	if !a.Access.ViaTraefik || !a.Network.External || a.Network.Name != DefaultNetwork || a.Mounts.AccessLogPath != DefaultAccessLogPath {
		t.Fatalf("nested decode lost defaults: %+v", a)
	}
}

func TestOtherProvider(t *testing.T) {
	a := Defaults(ModeFull)
	a.Domain = "example.com"
	a.TLS = TLS{Method: TLSDNS, Provider: DNSProviderOther, Email: "me@example.com", SecretVars: []string{"HETZNER_API_KEY"}}
	a.Finalize()
	if err := a.Validate(); err == nil {
		t.Fatal("expected lego_provider error")
	}
	a.TLS.LegoProvider = "hetzner"
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if a.TLS.ProviderID() != "hetzner" {
		t.Fatalf("provider id %q", a.TLS.ProviderID())
	}
	b := Defaults(ModeFull)
	b.TLS = TLS{Method: TLSDNS, Provider: "cloudflare", LegoProvider: "stale"}
	b.Finalize()
	if b.TLS.LegoProvider != "" || b.TLS.ProviderID() != "cloudflare" {
		t.Fatalf("stale lego provider kept: %+v", b.TLS)
	}
}

func TestModesWithoutTLSValidate(t *testing.T) {
	for _, m := range []Mode{ModeTMNative, ModeAgentDocker, ModeAgentBinary} {
		a := Defaults(m)
		a.SetSecret(SecretTMAAPIKey, "k")
		a.Finalize()
		if err := a.Validate(); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		if a.TLS.Method != TLSNone {
			t.Fatalf("%s: tls method %q", m, a.TLS.Method)
		}
	}
}

func TestValidateRejectsInjection(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(a *Answers)
	}{
		{"host escaping the label rule", func(a *Answers) { a.Hosts.Manager = "m.example.com`) || Host(`evil.test" }},
		{"host with a quote", func(a *Answers) { a.Hosts.Dashboard = `d."example.com` }},
		{"newline in the network name", func(a *Answers) { a.Network.Name = "net\n  bad: true" }},
		{"newline in the email", func(a *Answers) { a.TLS.Email = "me@example.com\nstorage: /etc/passwd" }},
		{"control character in a secret", func(a *Answers) { a.SetSecret("CF_DNS_API_TOKEN", "tok\nTRAEFIK_ADMIN=pwned") }},
		{"relative mount path", func(a *Answers) {
			a.Mounts.StaticConfig = true
			a.Restart.Method = RestartSocket
			a.Mounts.StaticConfigPath = "traefik.yml"
		}},
		{"container name with a space", func(a *Answers) { a.Restart.Container = "my traefik" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Defaults(ModeTMDocker)
			a.Dir = "/srv/tm"
			a.Hosts.Manager = "tm.example.com"
			a.TLS = TLS{Method: TLSHTTP, Email: "me@example.com"}
			c.break_(a)
			a.Finalize()
			if err := a.Validate(); err == nil {
				t.Fatal("expected Validate to reject this")
			}
		})
	}
}

func TestValidateAcceptsRealisticValues(t *testing.T) {
	a := Defaults(ModeTMNative)
	a.Config.Layout = LayoutDirectory
	a.Config.Dir = "/etc/traefik/conf.d"
	a.Mounts.Certs = true
	a.Mounts.AcmePath = "/etc/traefik/acme.json"
	a.Mounts.StaticConfig = true
	a.Mounts.StaticConfigPath = "/etc/traefik/traefik.yml"
	a.Restart.Method = RestartPoisonPill
	a.Restart.TraefikSystemd = true
	a.Restart.TraefikService = "traefik@main"
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	a.Mounts.AcmePath = ""
	if err := a.Validate(); err == nil {
		t.Fatal("an enabled mount with no path must be rejected in every mode")
	}
}

func TestChannel(t *testing.T) {
	a := Defaults(ModeFull)
	if a.Channel != ChannelStable || a.ImageTag() != "latest" || a.GitBranch() != "main" {
		t.Fatalf("stable defaults wrong: %+v", a.Channel)
	}
	a.Channel = ChannelBeta
	if a.ImageTag() != "beta" || a.GitBranch() != "dev" {
		t.Fatalf("beta mapping wrong: %s %s", a.ImageTag(), a.GitBranch())
	}
	a.Domain = "example.com"
	a.TLS.Email = "me@example.com"
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	b := Defaults(ModeAgentBinary)
	b.Channel = ChannelBeta
	b.SetSecret(SecretTMAAPIKey, "k")
	b.Finalize()
	if err := b.Validate(); err == nil {
		t.Fatal("the agent binary has no beta build, this must be refused")
	}
	c := Defaults(ModeFull)
	c.Channel = "nightly"
	c.Domain = "example.com"
	c.TLS.Email = "me@example.com"
	c.Finalize()
	if err := c.Validate(); err == nil {
		t.Fatal("an unknown channel must be refused")
	}
	d := Defaults(ModeFull)
	d.Channel = ""
	d.Finalize()
	if d.Channel != ChannelStable {
		t.Fatalf("an empty channel must normalise to stable, got %q", d.Channel)
	}
}

func TestCrowdSecAlertLimit(t *testing.T) {
	a := Defaults(ModeFull)
	a.Domain = "example.com"
	a.TLS.Email = "me@example.com"
	a.CrowdSec.Mode = CrowdSecInstall
	a.CrowdSec.AlertLimit = "2500"
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"abc", "-1", "100001", "2 500"} {
		a.CrowdSec.AlertLimit = bad
		if err := a.Validate(); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	a.CrowdSec.AlertLimit = "0"
	if err := a.Validate(); err != nil {
		t.Fatalf("0 means no cap and must be accepted: %v", err)
	}
	b := Defaults(ModeAgentDocker)
	b.SetSecret(SecretTMAAPIKey, "k")
	b.CrowdSec.Mode = CrowdSecNone
	b.CrowdSec.AlertLimit = "2500"
	b.Finalize()
	if b.CrowdSec.AlertLimit != "" {
		t.Fatal("the limit must clear when crowdsec is off")
	}
}

func TestFullNativeFinalizeForcesTheFixedLayout(t *testing.T) {
	a := Defaults(ModeFullNative)
	a.Domain = "example.com"
	a.TLS.Email = "me@example.com"
	a.Mounts.StaticConfig = true
	a.Finalize()
	if a.Hosts.Dashboard != "traefik.example.com" {
		t.Fatalf("dashboard host not derived: %q", a.Hosts.Dashboard)
	}
	if a.Hosts.Manager != "" {
		t.Fatalf("full-native has no manager host, got %q", a.Hosts.Manager)
	}
	if !a.Native.ServiceUser || a.Network.Name != "" {
		t.Fatalf("service user or network not forced: %+v %+v", a.Native, a.Network)
	}
	if a.Config.Path != NativeDynamicPath || a.Config.Dir != NativeTraefikDynamicDir {
		t.Fatalf("dynamic config paths not forced: %+v", a.Config)
	}
	if !a.Mounts.AccessLogs || a.Mounts.AccessLogPath != DefaultAccessLogPath {
		t.Fatalf("access log not forced on: %+v", a.Mounts)
	}
	if !a.Mounts.Certs || a.Mounts.AcmePath != NativeAcmePath {
		t.Fatalf("acme path not forced: %+v", a.Mounts)
	}
	if a.Restart.Method != RestartPoisonPill || !a.Restart.TraefikSystemd || a.Restart.TraefikService != "traefik" {
		t.Fatalf("static editor restart not wired: %+v", a.Restart)
	}
	if a.Restart.SignalFile != DefaultNativeSignalFile {
		t.Fatalf("signal file default missing: %q", a.Restart.SignalFile)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	a.Mounts.StaticConfig = false
	a.Finalize()
	if a.Restart.Method != RestartNone || a.Restart.TraefikSystemd {
		t.Fatalf("restart must clear when the editor is off: %+v", a.Restart)
	}
	b := Defaults(ModeFullNative)
	b.TLS = TLS{Method: TLSNone}
	b.Finalize()
	if b.Mounts.Certs {
		t.Fatal("certs must be off without TLS")
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("no TLS and no domain must be valid: %v", err)
	}
}

func TestFullNativeValidate(t *testing.T) {
	a := Defaults(ModeFullNative)
	a.TLS.Email = "me@example.com"
	a.Finalize()
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "domain is required") {
		t.Fatalf("tls without a domain must be rejected: %v", err)
	}
	a.Domain = "example.com"
	a.Hosts.Dashboard = ""
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	a.CrowdSec.Mode = CrowdSecInstall
	a.Finalize()
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "the port the CrowdSec LAPI listens on") {
		t.Fatalf("crowdsec install on the traefik api port must be rejected: %v", err)
	}
	a.Network.TraefikAPIPort = "8081"
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatalf("crowdsec install must be allowed off the lapi port: %v", err)
	}
	a.Network.TraefikAPIPort = "8080"
	a.CrowdSec.Mode = CrowdSecConnect
	a.CrowdSec.LAPIURL = "http://127.0.0.1:" + a.Network.TraefikAPIPort
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("lapi on the traefik api port must be rejected: %v", err)
	}
	a.CrowdSec.LAPIURL = "http://127.0.0.1:8081"
	a.SetSecret(SecretCrowdSecAPIKey, "k")
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	a.Channel = ChannelBeta
	if err := a.Validate(); err != nil {
		t.Fatalf("beta must be allowed for full-native: %v", err)
	}
	if a.GitBranch() != "dev" {
		t.Fatalf("beta must track the dev branch, got %q", a.GitBranch())
	}
	a.Native.Port = "nope"
	if err := a.Validate(); err == nil {
		t.Fatal("a bad port must be rejected")
	}
}

func TestFullNativeMachinePasswordSpec(t *testing.T) {
	a := Defaults(ModeFullNative)
	a.TLS = TLS{Method: TLSNone}
	a.CrowdSec.Mode = CrowdSecConnect
	a.CrowdSec.MachineID = "traefik-manager"
	a.Finalize()
	keys := strings.Join(a.SecretKeys(), ",")
	if keys != SecretCrowdSecAPIKey+","+SecretCrowdSecMachinePassword {
		t.Fatalf("machine password spec missing: %s", keys)
	}
}

func TestFullNativeDumpRoundTrip(t *testing.T) {
	a := Defaults(ModeFullNative)
	a.Domain = "example.com"
	a.TLS.Email = "me@example.com"
	a.Mounts.StaticConfig = true
	a.Network.TraefikAPIPort = "9080"
	a.Finalize()
	out, err := a.Dump()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	b.Finalize()
	if b.Network.TraefikAPIPort != "9080" || !b.Mounts.StaticConfig || b.Hosts.Dashboard != "traefik.example.com" {
		t.Fatalf("round trip changed values: %+v", b)
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCrowdSecIsAvailableInEveryMode(t *testing.T) {
	for _, mode := range Modes {
		a := Defaults(mode)
		if !hasSection(a.Sections(), "crowdsec") {
			t.Errorf("%s has no crowdsec section", mode)
		}
		a.CrowdSec.Mode = CrowdSecInstall
		a.Finalize()
		if a.CrowdSec.Mode != CrowdSecInstall {
			t.Errorf("%s: Finalize cleared the crowdsec install choice", mode)
		}
		if !a.Mounts.AccessLogs || a.Mounts.AccessLogPath == "" {
			t.Errorf("%s: install must force the access log on, got %+v", mode, a.Mounts)
		}
		want := DefaultLAPIURL
		if mode.IsSystemd() {
			want = NativeLAPIURL
		}
		if a.CrowdSec.LAPIURL != want {
			t.Errorf("%s: lapi url %q, want %q", mode, a.CrowdSec.LAPIURL, want)
		}
		wantMachine := CrowdSecMachineID
		if mode.IsAgent() {
			wantMachine = ""
		}
		if a.CrowdSec.MachineID != wantMachine {
			t.Errorf("%s: machine id %q, want %q", mode, a.CrowdSec.MachineID, wantMachine)
		}
	}
}

func hasSection(sections []string, name string) bool {
	for _, s := range sections {
		if s == name {
			return true
		}
	}
	return false
}

func TestCrowdSecInstallSecretsPerMode(t *testing.T) {
	cases := []struct {
		mode Mode
		keys string
	}{
		{ModeFull, "CROWDSEC_API_KEY,CROWDSEC_MACHINE_PASSWORD"},
		{ModeFullNative, "CROWDSEC_API_KEY,CROWDSEC_MACHINE_PASSWORD"},
		{ModeTMDocker, "CROWDSEC_API_KEY,CROWDSEC_MACHINE_PASSWORD"},
		{ModeTMNative, "CROWDSEC_API_KEY,CROWDSEC_MACHINE_PASSWORD"},
		{ModeAgentDocker, "CROWDSEC_API_KEY,TMA_API_KEY"},
		{ModeAgentBinary, "CROWDSEC_API_KEY,TMA_API_KEY"},
	}
	for _, c := range cases {
		a := Defaults(c.mode)
		a.CrowdSec.Mode = CrowdSecInstall
		a.Finalize()
		if got := strings.Join(a.SecretKeys(), ","); got != c.keys {
			t.Errorf("%s secret keys %s, want %s", c.mode, got, c.keys)
		}
		if err := a.GenerateSecrets(); err != nil {
			t.Fatal(err)
		}
		if len(a.Secrets[SecretCrowdSecAPIKey]) != 64 {
			t.Errorf("%s did not generate a bouncer key: %q", c.mode, a.Secrets[SecretCrowdSecAPIKey])
		}
		pw := a.Secrets[SecretCrowdSecMachinePassword]
		if c.mode.IsAgent() && pw != "" {
			t.Errorf("%s has no alerts view, it must not get machine credentials", c.mode)
		}
		if !c.mode.IsAgent() && len(pw) != 48 {
			t.Errorf("%s did not generate a machine password: %q", c.mode, pw)
		}
	}
}

func TestCrowdSecNativePortConflicts(t *testing.T) {
	full := Defaults(ModeFullNative)
	full.TLS = TLS{Method: TLSNone}
	full.CrowdSec.Mode = CrowdSecInstall
	full.Finalize()
	if err := full.crowdSecPortConflict(); err == nil {
		t.Error("full-native traefik api on 8080 must conflict")
	}
	full.Network.TraefikAPIPort = "8081"
	if err := full.crowdSecPortConflict(); err != nil {
		t.Errorf("8081 is free: %v", err)
	}

	tmn := Defaults(ModeTMNative)
	tmn.CrowdSec.Mode = CrowdSecInstall
	tmn.Native.Port = NativeLAPIPort
	tmn.Finalize()
	if err := tmn.crowdSecPortConflict(); err == nil {
		t.Error("tm-native on 8080 must conflict")
	}

	agb := Defaults(ModeAgentBinary)
	agb.CrowdSec.Mode = CrowdSecInstall
	agb.Agent.TraefikURL = "http://localhost:8080"
	agb.Finalize()
	if err := agb.crowdSecPortConflict(); err == nil {
		t.Error("agent-binary pointing at localhost:8080 must conflict")
	}
	agb.Agent.TraefikURL = "http://traefik.internal:8080"
	if err := agb.crowdSecPortConflict(); err != nil {
		t.Errorf("a remote traefik api is fine: %v", err)
	}
	agb.CrowdSec.Mode = CrowdSecConnect
	agb.Agent.TraefikURL = NativeLAPIURL
	if err := agb.crowdSecPortConflict(); err != nil {
		t.Errorf("connect never installs a local lapi: %v", err)
	}
}

func TestCrowdSecDumpRoundTripForTheNewModes(t *testing.T) {
	for _, mode := range []Mode{ModeTMDocker, ModeTMNative} {
		a := Defaults(mode)
		a.Hosts.Manager = "manager.example.com"
		a.CrowdSec.Mode = CrowdSecConnect
		a.CrowdSec.LAPIURL = "http://10.0.0.5:8080"
		a.CrowdSec.MachineID = "tm-box"
		a.CrowdSec.AlertLimit = "1200"
		a.Finalize()
		out, err := a.Dump()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "crowdsec:") {
			t.Fatalf("%s dump has no crowdsec section:\n%s", mode, out)
		}
		b, err := Parse(out)
		if err != nil {
			t.Fatal(err)
		}
		b.Finalize()
		if b.CrowdSec != a.CrowdSec {
			t.Fatalf("%s round trip changed crowdsec: %+v want %+v", mode, b.CrowdSec, a.CrowdSec)
		}
	}
}

func TestCrowdSecInstallNeedsAnAccessLogPath(t *testing.T) {
	a := Defaults(ModeTMDocker)
	a.Hosts.Manager = "manager.example.com"
	a.TLS.Email = "me@example.com"
	a.CrowdSec.Mode = CrowdSecInstall
	a.Finalize()
	a.Mounts.AccessLogPath = ""
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "access_log_path is required") {
		t.Fatalf("install without a log path must be rejected: %v", err)
	}
}
