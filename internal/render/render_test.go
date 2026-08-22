package render

import (
	"strings"
	"testing"

	"github.com/chr0nzz/traefik-stack/internal/answers"
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
