package wizard

import (
	"errors"
	"regexp"
	"strings"

	"charm.land/huh/v2"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

const dnsPrefix = "dns:"

func tlsOptions() []huh.Option[string] {
	opts := []huh.Option[string]{
		huh.NewOption("Let's Encrypt - HTTP challenge (port 80 must be open)", answers.TLSHTTP),
	}
	for _, p := range answers.DNSProviders {
		opts = append(opts, huh.NewOption("Let's Encrypt - DNS challenge: "+p.Label, dnsPrefix+p.ID))
	}
	return append(opts,
		huh.NewOption("Let's Encrypt - DNS challenge: other provider (lego name)", dnsPrefix+answers.DNSProviderOther),
		huh.NewOption("No TLS (HTTP only)", answers.TLSNone),
	)
}

func tlsChoice(t answers.TLS) string {
	if t.Method == answers.TLSDNS && t.Provider != "" {
		return dnsPrefix + t.Provider
	}
	return t.Method
}

func applyTLSChoice(t *answers.TLS, choice string) {
	switch {
	case strings.HasPrefix(choice, dnsPrefix):
		t.Method = answers.TLSDNS
		t.Provider = strings.TrimPrefix(choice, dnsPrefix)
	case choice == answers.TLSNone:
		t.Method = answers.TLSNone
		t.Provider = ""
	default:
		t.Method = answers.TLSHTTP
		t.Provider = ""
	}
}

func legoProvider(t answers.TLS) string {
	return t.ProviderID()
}

func (w *wizard) tls() error {
	a := w.a
	choice := tlsChoice(a.TLS)
	if err := w.form(selectOne("Certificate method", &choice, tlsOptions()...)); err != nil {
		return err
	}
	applyTLSChoice(&a.TLS, choice)
	if a.TLS.Method == answers.TLSNone {
		a.TLS.Vars = nil
		a.TLS.SecretVars = nil
		w.u.Warn("Running without TLS. HTTP only.")
		return nil
	}
	email := requiredInput("Email for Let's Encrypt", &a.TLS.Email, "an email is required for Let's Encrypt")
	if a.TLS.Method != answers.TLSDNS {
		a.TLS.Vars = nil
		a.TLS.SecretVars = nil
		return w.form(email)
	}
	if a.TLS.Provider == answers.DNSProviderOther {
		return w.tlsOther(email)
	}
	p, _ := answers.FindDNSProvider(a.TLS.Provider)
	fields := []huh.Field{email}
	values := map[string]*string{}
	for _, v := range p.Vars {
		if v.Secret {
			fields = append(fields, w.secret(v.Name, v.Prompt, strings.ToLower(v.Prompt)+" is required"))
			continue
		}
		val := new(string)
		*val = orDefault(a.TLS.Vars[v.Name], v.Default)
		values[v.Name] = val
		fields = append(fields, input(v.Prompt, val))
	}
	if err := w.form(fields...); err != nil {
		return err
	}
	a.TLS.SecretVars = nil
	a.TLS.Vars = nil
	for name, val := range values {
		if a.TLS.Vars == nil {
			a.TLS.Vars = map[string]string{}
		}
		a.TLS.Vars[name] = *val
	}
	return nil
}

func (w *wizard) tlsOther(email *huh.Input) error {
	a := w.a
	lego := a.TLS.LegoProvider
	names := strings.Join(a.TLS.SecretVars, ",")
	err := w.form(
		email,
		requiredInput("Lego DNS provider id (e.g. hetzner, ovh, gandiv5)", &lego, "a lego provider id is required").
			Description("Provider ids and their credential variables: https://go-acme.github.io/lego/dns/"),
		checkedInput("Credential environment variables (comma separated)", &names, validVarNames).
			Description("Every variable is stored as a secret and passed to Traefik."),
	)
	if err != nil {
		return err
	}
	vars := splitVarNames(names)
	a.TLS.LegoProvider = strings.ToLower(strings.TrimSpace(lego))
	a.TLS.Vars = nil
	a.TLS.SecretVars = vars
	fields := make([]huh.Field, 0, len(vars))
	for _, n := range vars {
		fields = append(fields, w.secret(n, n, n+" is required"))
	}
	return w.form(fields...)
}

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func splitVarNames(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '\t' }) {
		n := strings.ToUpper(strings.TrimSpace(f))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func validVarNames(s string) error {
	names := splitVarNames(s)
	if len(names) == 0 {
		return errors.New("at least one variable name is required")
	}
	for _, n := range names {
		if !varNameRe.MatchString(n) {
			return errors.New("invalid variable name " + n)
		}
	}
	return nil
}

func (w *wizard) tlsExisting() error {
	a := w.a
	choice := answers.TLSHTTP
	if a.TLS.Method == answers.TLSNone {
		choice = answers.TLSNone
	}
	err := w.form(selectOne("TLS via the existing Traefik", &choice,
		huh.NewOption("Let's Encrypt - certificate resolver \""+answers.CertResolver+"\" must exist in the Traefik static config", answers.TLSHTTP),
		huh.NewOption("No TLS (HTTP only)", answers.TLSNone),
	))
	if err != nil {
		return err
	}
	a.TLS = answers.TLS{Method: choice}
	if choice == answers.TLSNone {
		w.u.Warn("Running without TLS. HTTP only.")
	}
	return nil
}
