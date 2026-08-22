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
	if err := b.Validate(); err == nil {
		t.Fatal("the binary agent cannot install crowdsec, Validate must say so")
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
