package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

func TestRenderRequiresAnswers(t *testing.T) {
	if _, err := Render(Input{}); err == nil {
		t.Fatal("expected an error for nil answers")
	}
}

func TestNativeNeedsUserWithoutServiceUser(t *testing.T) {
	a := answers.Defaults(answers.ModeTMNative)
	a.TLS.Method = answers.TLSNone
	a.Native.ServiceUser = false
	a.Finalize()
	if _, err := Render(Input{Answers: a}); err == nil {
		t.Fatal("expected an error when no user is given")
	}
	out, err := Render(Input{Answers: a, User: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Files[0].Content, "User=bob\n") {
		t.Fatalf("unit does not run as the invoking user:\n%s", out.Files[0].Content)
	}
}

func TestEnvFileFollowsSecretKeyOrder(t *testing.T) {
	a := answers.Defaults(answers.ModeAgentDocker)
	a.TLS.Method = answers.TLSNone
	a.Agent.Git.Enabled = true
	a.Agent.Git.Repo = "https://example.com/r.git"
	a.CrowdSec.Mode = answers.CrowdSecConnect
	a.Finalize()
	a.SetSecret(answers.SecretGitBackupToken, "t")
	a.SetSecret(answers.SecretTMAAPIKey, "k")
	a.SetSecret(answers.SecretCrowdSecAPIKey, "c")
	want := "CROWDSEC_API_KEY='c'\nTMA_API_KEY='k'\nGIT_BACKUP_TOKEN='t'\n"
	if got := EnvFile(a); got != want {
		t.Fatalf("env file mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestDockerModesNeverInlineSecrets(t *testing.T) {
	a := answers.Defaults(answers.ModeFull)
	a.Domain = "example.com"
	a.TLS = answers.TLS{Method: answers.TLSDNS, Provider: "cloudflare", Email: "a@example.com"}
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.Finalize()
	a.SetSecret("CF_DNS_API_TOKEN", "cf-secret")
	a.SetSecret(answers.SecretCrowdSecAPIKey, "cs-secret")
	a.SetSecret(answers.SecretCrowdSecMachinePassword, "pw-secret")
	out, err := Render(Input{Answers: a})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out.Files {
		if f.Path == ".env" {
			continue
		}
		if strings.Contains(f.Content, "-secret") {
			t.Fatalf("%s inlines a secret:\n%s", f.Path, f.Content)
		}
	}
}

func TestEnvValueKeepsAwkwardSecretsIntact(t *testing.T) {
	for _, v := range []string{"p4ss$word", "tok en # x", "$HOME", `a"b`, `a\b`, ""} {
		got := EnvValue(v)
		if got != "'"+v+"'" {
			t.Fatalf("EnvValue(%q) = %s", v, got)
		}
	}
}

func TestSystemdQuote(t *testing.T) {
	cases := map[string]string{
		"CONFIG_PATH=/etc/traefik/dynamic.yml":    `"CONFIG_PATH=/etc/traefik/dynamic.yml"`,
		"HOME=/opt/traefik manager":               `"HOME=/opt/traefik manager"`,
		"GIT_BACKUP_REPO=https://u%40e.com/x.git": `"GIT_BACKUP_REPO=https://u%%40e.com/x.git"`,
		`K=a"b`: `"K=a\"b"`,
		`K=a\b`: `"K=a\\b"`,
	}
	for in, want := range cases {
		if got := SystemdQuote(in); got != want {
			t.Fatalf("SystemdQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDNSUnitEnvSkipsSecretsAndSortsUnknownProviders(t *testing.T) {
	a := answers.Defaults(answers.ModeFullNative)
	a.Domain = "example.com"
	a.TLS = answers.TLS{
		Method:       answers.TLSDNS,
		Provider:     answers.DNSProviderOther,
		LegoProvider: "hetzner",
		Email:        "a@example.com",
		Vars:         map[string]string{"ZZZ_REGION": "eu", "AAA_ZONE": "z1"},
		SecretVars:   []string{"HETZNER_API_KEY"},
	}
	a.Finalize()
	got := strings.Join(dnsUnitEnv(a), " ")
	want := `"AAA_ZONE=z1" "ZZZ_REGION=eu"`
	if got != want {
		t.Fatalf("unit env = %s, want %s", got, want)
	}
	if !hasSecretDNSVars(a) {
		t.Fatal("secret vars not detected")
	}
	b := answers.Defaults(answers.ModeFullNative)
	b.Domain = "example.com"
	b.TLS = answers.TLS{Method: answers.TLSDNS, Provider: "route53", Email: "a@example.com"}
	b.Finalize()
	if got := strings.Join(dnsUnitEnv(b), " "); got != `"AWS_REGION=us-east-1"` {
		t.Fatalf("route53 default not applied: %s", got)
	}
}

func TestFullNativeUnitsShareTheEnvFileOnlyWhenNeeded(t *testing.T) {
	a := answers.Defaults(answers.ModeFullNative)
	a.Domain = "example.com"
	a.TLS = answers.TLS{Method: answers.TLSDNS, Provider: "cloudflare", Email: "a@example.com"}
	a.Finalize()
	a.SetSecret("CF_DNS_API_TOKEN", "cf-secret")
	out, err := Render(Input{Answers: a})
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]string{}
	for _, f := range out.Files {
		units[f.Path] = f.Content
		if f.Path != NativeEnvPath && strings.Contains(f.Content, "cf-secret") {
			t.Fatalf("%s inlines a secret", f.Path)
		}
	}
	if !strings.Contains(units[TraefikUnitPath], "EnvironmentFile="+NativeEnvPath) {
		t.Fatal("traefik unit must read the env file for the dns secret")
	}
	if strings.Contains(units[NativeUnitPath], "EnvironmentFile=") {
		t.Fatal("the tm unit has no secrets here and must not read the env file")
	}
	if !strings.Contains(units[NativeEnvPath], "CF_DNS_API_TOKEN='cf-secret'") {
		t.Fatal("env file misses the secret")
	}
}

func crowdSecAnswers(t *testing.T, mode answers.Mode) *answers.Answers {
	t.Helper()
	a := answers.Defaults(mode)
	a.Domain = "example.com"
	a.Hosts.Manager = "manager.example.com"
	a.TLS.Email = "me@example.com"
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.CrowdSec.AlertLimit = "900"
	if mode == answers.ModeFullNative {
		a.Network.TraefikAPIPort = "8081"
	}
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatalf("%s: %v", mode, err)
	}
	if err := a.GenerateSecrets(); err != nil {
		t.Fatal(err)
	}
	a.SetSecret(answers.SecretTMAAPIKey, "tma-key")
	return a
}

func findFile(out *Output, path string) (File, bool) {
	for _, f := range out.Files {
		if f.Path == path {
			return f, true
		}
	}
	return File{}, false
}

func TestCrowdSecInstallRendersInEveryMode(t *testing.T) {
	for _, mode := range answers.Modes {
		a := crowdSecAnswers(t, mode)
		out, err := Render(Input{Answers: a, User: "alice"})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if mode.IsSystemd() {
			f, ok := findFile(out, CrowdSecAcquisPath)
			if !ok {
				t.Errorf("%s: no acquisition file at %s", mode, CrowdSecAcquisPath)
				continue
			}
			if !strings.Contains(f.Content, a.Mounts.AccessLogPath) || !strings.Contains(f.Content, "type: traefik") {
				t.Errorf("%s acquis:\n%s", mode, f.Content)
			}
			if !f.Privileged {
				t.Errorf("%s: the acquisition file must be privileged", mode)
			}
			continue
		}
		f, ok := findFile(out, "crowdsec/acquis.yaml")
		if !ok {
			t.Errorf("%s: no crowdsec/acquis.yaml", mode)
			continue
		}
		if !strings.Contains(f.Content, traefikAccessLog) {
			t.Errorf("%s acquis:\n%s", mode, f.Content)
		}
		compose, ok := findFile(out, "docker-compose.yml")
		if !ok {
			t.Fatalf("%s: no compose file", mode)
		}
		if !strings.Contains(compose.Content, "\n  crowdsec:\n") {
			t.Errorf("%s compose has no crowdsec service:\n%s", mode, compose.Content)
		}
		if !strings.Contains(compose.Content, "crowdsec_data:") {
			t.Errorf("%s compose has no crowdsec_data volume:\n%s", mode, compose.Content)
		}
	}
}

func TestRenderIsIdempotent(t *testing.T) {
	for _, mode := range answers.Modes {
		a := crowdSecAnswers(t, mode)
		first, err := Render(Input{Answers: a, User: "alice"})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		second, err := Render(Input{Answers: a, User: "alice"})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Errorf("%s: a second render differs from the first", mode)
		}
	}
}

func TestTMDockerCrowdSecMountsTheUsersOwnLog(t *testing.T) {
	a := answers.Defaults(answers.ModeTMDocker)
	a.Hosts.Manager = "manager.example.com"
	a.TLS.Email = "me@example.com"
	a.Mounts.AccessLogPath = "/srv/logs/traefik/access.log"
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.Finalize()
	if err := a.GenerateSecrets(); err != nil {
		t.Fatal(err)
	}
	out, err := Render(Input{Answers: a, User: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	compose, _ := findFile(out, "docker-compose.yml")
	want := "/srv/logs/traefik/access.log:" + traefikAccessLog + ":ro"
	if !strings.Contains(compose.Content, want) {
		t.Errorf("compose does not bind the user's log as %s:\n%s", want, compose.Content)
	}
	if !strings.Contains(compose.Content, "CROWDSEC_LAPI_URL=http://crowdsec:8080") {
		t.Errorf("tm is not pointed at the container lapi:\n%s", compose.Content)
	}
	if !strings.Contains(compose.Content, "BOUNCER_KEY_traefikmanager=${CROWDSEC_API_KEY}") {
		t.Errorf("the generated bouncer key is not handed to crowdsec:\n%s", compose.Content)
	}
}

func TestNativeCrowdSecSplitsSecretsFromTheUnit(t *testing.T) {
	for _, mode := range []answers.Mode{answers.ModeTMNative, answers.ModeFullNative, answers.ModeAgentBinary} {
		a := crowdSecAnswers(t, mode)
		out, err := Render(Input{Answers: a, User: "alice"})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		unitPath := NativeUnitPath
		envPath := NativeEnvPath
		if mode == answers.ModeAgentBinary {
			unitPath = AgentUnitPath
			envPath = AgentEnvPath
		}
		unit, ok := findFile(out, unitPath)
		if !ok {
			t.Fatalf("%s: no unit at %s", mode, unitPath)
		}
		if !strings.Contains(unit.Content, "EnvironmentFile="+envPath) {
			t.Errorf("%s unit does not read %s:\n%s", mode, envPath, unit.Content)
		}
		if !strings.Contains(unit.Content, `Environment="CROWDSEC_LAPI_URL=`+answers.NativeLAPIURL+`"`) {
			t.Errorf("%s unit lacks the native lapi url:\n%s", mode, unit.Content)
		}
		if !strings.Contains(unit.Content, `Environment="CROWDSEC_ALERT_LIMIT=900"`) {
			t.Errorf("%s unit lacks the alert limit:\n%s", mode, unit.Content)
		}
		if strings.Contains(unit.Content, "CROWDSEC_API_KEY") {
			t.Errorf("%s unit inlines the bouncer key:\n%s", mode, unit.Content)
		}
		env, ok := findFile(out, envPath)
		if !ok {
			t.Fatalf("%s: no env file at %s", mode, envPath)
		}
		if !strings.Contains(env.Content, "CROWDSEC_API_KEY=") {
			t.Errorf("%s env file lacks the bouncer key:\n%s", mode, env.Content)
		}
		wantsMachine := !mode.IsAgent()
		if got := strings.Contains(env.Content, "CROWDSEC_MACHINE_PASSWORD="); got != wantsMachine {
			t.Errorf("%s machine password in env = %v, want %v", mode, got, wantsMachine)
		}
	}
}

func TestBouncerVarNamesAreShellSafe(t *testing.T) {
	for _, v := range []string{crowdsecBouncerFull, crowdsecBouncerTMA} {
		name, ok := strings.CutPrefix(v, "BOUNCER_KEY_")
		if !ok {
			t.Fatalf("%q must start with BOUNCER_KEY_", v)
		}
		if name == "" {
			t.Fatalf("%q has no bouncer name", v)
		}
		for _, r := range v {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				t.Fatalf("%q is not a valid shell variable name, so the crowdsec entrypoint cannot see it and the bouncer is never registered", v)
			}
		}
	}
}
