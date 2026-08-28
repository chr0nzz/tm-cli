package wizard

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

type Section struct {
	ID    string
	Label string
}

type section struct {
	Section
	run func(w *wizard) error
}

func sec(id, label string, run func(*wizard) error) section {
	return section{Section: Section{ID: id, Label: label}, run: run}
}

func sectionsFor(mode answers.Mode) []section {
	switch mode {
	case answers.ModeFull:
		return []section{
			sec("general", "General", (*wizard).fullGeneral),
			sec("deployment", "Deployment type", (*wizard).fullDeployment),
			sec("domain", "Domain", (*wizard).fullDomain),
			sec("tls", "TLS / Certificates", (*wizard).fullTLS),
			sec("config", "Dynamic config", (*wizard).configLayout),
			sec("mounts", "Optional mounts", (*wizard).fullMounts),
			sec("crowdsec", "CrowdSec", (*wizard).fullCrowdSec),
			sec("network", "Docker network", (*wizard).fullNetwork),
		}
	case answers.ModeTMDocker:
		return []section{
			sec("general", "General", (*wizard).tmdGeneral),
			sec("network", "Network", (*wizard).tmdNetwork),
			sec("access", "Access", (*wizard).tmdAccess),
			sec("config", "Dynamic config", (*wizard).configLayout),
			sec("mounts", "Optional mounts", (*wizard).tmdMounts),
		}
	case answers.ModeTMNative:
		return []section{
			sec("general", "General", (*wizard).tmnGeneral),
			sec("user", "Service user", (*wizard).tmnUser),
			sec("config", "Dynamic config", (*wizard).tmnConfig),
			sec("mounts", "Optional mounts", (*wizard).tmnMounts),
		}
	case answers.ModeAgentDocker:
		return []section{
			sec("apikey", "API key", (*wizard).agentAPIKey),
			sec("traefik", "Traefik connection", (*wizard).agentTraefik),
			sec("paths", "Optional paths", (*wizard).agentPaths),
			sec("restart", "Restart method", (*wizard).agentRestart),
			sec("crowdsec", "CrowdSec", (*wizard).agentCrowdSec),
			sec("git", "Git backup", (*wizard).agentGit),
			sec("location", "Install location", (*wizard).agentLocation),
		}
	case answers.ModeAgentDockerTraefik:
		return []section{
			sec("apikey", "API key", (*wizard).agentAPIKey),
			sec("traefik", "Traefik install", (*wizard).agentTraefikInstall),
			sec("paths", "Optional paths", (*wizard).agentPaths),
			sec("restart", "Restart method", (*wizard).agentRestart),
			sec("crowdsec", "CrowdSec", (*wizard).agentCrowdSec),
			sec("git", "Git backup", (*wizard).agentGit),
			sec("location", "Install location", (*wizard).agentLocation),
		}
	case answers.ModeAgentBinary:
		return []section{
			sec("apikey", "API key", (*wizard).agentAPIKey),
			sec("traefik", "Traefik connection", (*wizard).agentTraefik),
			sec("paths", "Optional paths", (*wizard).agentPaths),
			sec("restart", "Restart method", (*wizard).agentRestart),
			sec("crowdsec", "CrowdSec", (*wizard).agentCrowdSec),
			sec("git", "Git backup", (*wizard).agentGit),
		}
	}
	return nil
}

func Sections(mode answers.Mode) []Section {
	secs := sectionsFor(mode)
	if len(secs) == 0 {
		return nil
	}
	out := make([]Section, len(secs))
	for i, s := range secs {
		out[i] = s.Section
	}
	return out
}

func aliasFor(mode answers.Mode, id string) string {
	switch id {
	case "static":
		if mode.IsAgent() {
			return "traefik"
		}
		return "mounts"
	case "restart":
		return "mounts"
	case "tls":
		switch mode {
		case answers.ModeTMDocker:
			return "access"
		case answers.ModeAgentDockerTraefik:
			return "traefik"
		}
	case "dir":
		if mode.IsAgent() {
			return "location"
		}
		return "general"
	}
	return id
}

func findSection(mode answers.Mode, id string) (section, error) {
	secs := sectionsFor(mode)
	id = strings.ToLower(strings.TrimSpace(id))
	for _, s := range secs {
		if s.ID == id {
			return s, nil
		}
	}
	alias := aliasFor(mode, id)
	for _, s := range secs {
		if s.ID == alias {
			return s, nil
		}
	}
	ids := make([]string, len(secs))
	for i, s := range secs {
		ids[i] = s.ID
	}
	return section{}, fmt.Errorf("unknown section %q for mode %s (valid: %s)", id, mode, strings.Join(ids, ", "))
}

type wizard struct {
	ctx     context.Context
	u       *ui.UI
	a       *answers.Answers
	in      *os.File
	pending []*secretValue
}

type secretValue struct {
	key   string
	value string
}

func newWizard(ctx context.Context, u *ui.UI, a *answers.Answers, in *os.File) *wizard {
	if ctx == nil {
		ctx = context.Background()
	}
	if u == nil {
		u = ui.NewPlain(os.Stdout)
	}
	if in == nil {
		in = os.Stdin
	}
	return &wizard{ctx: ctx, u: u, a: a, in: in}
}

func (w *wizard) form(fields ...huh.Field) error {
	return w.groups(huh.NewGroup(fields...))
}

func (w *wizard) groups(groups ...*huh.Group) error {
	pending := w.pending
	w.pending = nil
	f := huh.NewForm(groups...).
		WithTheme(ui.Theme()).
		WithInput(w.in).
		WithOutput(os.Stdout).
		WithShowHelp(false)
	if err := f.RunWithContext(w.ctx); err != nil {
		return err
	}
	for _, p := range pending {
		if p.value != "" {
			w.a.SetSecret(p.key, p.value)
		}
	}
	return nil
}

func (w *wizard) secret(key, title, requiredMsg string) *huh.Input {
	p := &secretValue{key: key}
	w.pending = append(w.pending, p)
	in := huh.NewInput().Title(title).EchoMode(huh.EchoModePassword).Value(&p.value)
	if w.a.Secrets[key] != "" {
		return in.Description("(keep current) leave empty to keep the existing value")
	}
	if requiredMsg != "" {
		in = in.Validate(nonEmpty(requiredMsg))
	}
	return in
}

func sectionHeader(mode answers.Mode, s Section) string {
	if mode.IsAgent() {
		switch s.ID {
		case "restart":
			return "Traefik restart"
		case "crowdsec":
			return "CrowdSec (optional)"
		case "git":
			return "Git backup (optional)"
		}
		return s.Label
	}
	switch s.ID {
	case "config":
		return "Dynamic Config"
	case "mounts":
		return "Optional Mounts"
	case "network":
		if mode == answers.ModeFull {
			return "Docker Network"
		}
	case "crowdsec":
		return "CrowdSec IDS"
	case "user":
		return "Service User"
	}
	return s.Label
}

func (w *wizard) runSection(s section) error {
	w.u.Section(sectionHeader(w.a.Mode, s.Section))
	if err := s.run(w); err != nil {
		return err
	}
	w.a.Finalize()
	w.u.OK(s.Label + "  " + ui.MutedStyle.Render(reviewValue(w.a, s.ID)))
	return nil
}

func stepTitle(mode answers.Mode) string {
	switch mode {
	case answers.ModeFull:
		return "Traefik + Traefik Manager Setup"
	case answers.ModeTMDocker:
		return "Traefik Manager - Docker Setup"
	case answers.ModeTMNative:
		return "Traefik Manager - Linux Service Setup"
	}
	return "Traefik Manager Agent Setup"
}

func Run(ctx context.Context, u *ui.UI, a *answers.Answers, in *os.File) error {
	if a == nil {
		return errors.New("no answers to fill")
	}
	if !a.Mode.Valid() {
		return fmt.Errorf("unknown mode %q", a.Mode)
	}
	w := newWizard(ctx, u, a, in)
	w.u.Step(stepTitle(a.Mode))
	w.u.Line("%s", ui.MutedStyle.Render("Defaults are pre-filled. Press Enter to accept them, ctrl+c to abort."))
	for _, s := range sectionsFor(a.Mode) {
		if err := w.runSection(s); err != nil {
			return err
		}
	}
	return w.review()
}

func Review(ctx context.Context, u *ui.UI, a *answers.Answers, in *os.File) error {
	if a == nil {
		return errors.New("no answers to review")
	}
	if !a.Mode.Valid() {
		return fmt.Errorf("unknown mode %q", a.Mode)
	}
	return newWizard(ctx, u, a, in).review()
}

func AskSecrets(ctx context.Context, u *ui.UI, a *answers.Answers, keys []string, in *os.File) error {
	if a == nil {
		return errors.New("no answers to fill")
	}
	if len(keys) == 0 {
		return nil
	}
	prompts := map[string]string{}
	for _, spec := range a.SecretSpecs() {
		prompts[spec.Key] = spec.Prompt
	}
	w := newWizard(ctx, u, a, in)
	fields := make([]huh.Field, 0, len(keys))
	for _, key := range keys {
		title := orDefault(prompts[key], key)
		fields = append(fields, w.secret(key, title, strings.ToLower(title)+" is required"))
	}
	return w.groups(huh.NewGroup(fields...).Title("Secrets").Description("Values are stored in the env file, never in docker-compose.yml."))
}

func RunSection(ctx context.Context, u *ui.UI, a *answers.Answers, id string, in *os.File) error {
	if a == nil {
		return errors.New("no answers to fill")
	}
	if !a.Mode.Valid() {
		return fmt.Errorf("unknown mode %q", a.Mode)
	}
	s, err := findSection(a.Mode, id)
	if err != nil {
		return err
	}
	return newWizard(ctx, u, a, in).runSection(s)
}

const continueChoice = "continue"

func (w *wizard) review() error {
	secs := sectionsFor(w.a.Mode)
	for {
		w.printReview()
		opts := []huh.Option[string]{huh.NewOption("Continue", continueChoice)}
		for _, s := range secs {
			opts = append(opts, huh.NewOption("Edit "+s.Label, s.ID))
		}
		choice := continueChoice
		err := w.form(huh.NewSelect[string]().
			Title("Edit a section, or Continue").
			Options(opts...).
			Value(&choice))
		if err != nil {
			return err
		}
		if choice == continueChoice {
			return nil
		}
		for _, s := range secs {
			if s.ID == choice {
				if err := w.runSection(s); err != nil {
					return err
				}
				break
			}
		}
	}
}

const reviewRule = "────────────────────────────────────────────────────────"

func (w *wizard) printReview() {
	w.u.Blank()
	w.u.Line("%s", ui.BoldStyle.Render("Review configuration"))
	w.u.Line("%s", ui.MutedStyle.Render(reviewRule))
	for i, s := range sectionsFor(w.a.Mode) {
		w.u.Line("%s  %s  %s",
			ui.AccentStyle.Bold(true).Render(fmt.Sprintf("%2d", i+1)),
			ui.MutedStyle.Render(fmt.Sprintf("%-20s", s.Label)),
			reviewValue(w.a, s.ID))
	}
	w.u.Line("%s", ui.MutedStyle.Render(reviewRule))
}

func PickMode(ctx context.Context, u *ui.UI, in *os.File) (answers.Mode, error) {
	w := newWizard(ctx, u, nil, in)
	w.u.Step("What would you like to install?")
	top := "full"
	err := w.form(huh.NewSelect[string]().
		Title("Choose an option").
		Options(
			huh.NewOption("Traefik + Traefik Manager (full stack)", "full"),
			huh.NewOption("Traefik Manager only", "tm"),
			huh.NewOption("Traefik Manager Agent", "agent"),
		).
		Value(&top))
	if err != nil {
		return "", err
	}
	switch top {
	case "tm":
		mode := answers.ModeTMDocker
		err := w.form(huh.NewSelect[answers.Mode]().
			Title("Deployment method").
			Options(
				huh.NewOption("Docker", answers.ModeTMDocker),
				huh.NewOption("Linux service (systemd)", answers.ModeTMNative),
			).
			Value(&mode))
		if err != nil {
			return "", err
		}
		w.u.OK(mode.Label())
		return mode, nil
	case "agent":
		return w.pickAgentMethod()
	}
	w.u.OK(answers.ModeFull.Label())
	return answers.ModeFull, nil
}

func PickAgentMethod(ctx context.Context, u *ui.UI, in *os.File) (answers.Mode, error) {
	return newWizard(ctx, u, nil, in).pickAgentMethod()
}

func (w *wizard) pickAgentMethod() (answers.Mode, error) {
	w.u.Section("Install method")
	mode := answers.ModeAgentDocker
	err := w.form(huh.NewSelect[answers.Mode]().
		Title("Install method").
		Description("TMA runs alongside Traefik and lets a central TM manage this server remotely.").
		Options(
			huh.NewOption("Docker - Agent only (alongside existing Traefik)", answers.ModeAgentDocker),
			huh.NewOption("Docker - Agent + Traefik (deploy both together)", answers.ModeAgentDockerTraefik),
			huh.NewOption("Binary - Agent only (systemd service, no Docker)", answers.ModeAgentBinary),
		).
		Value(&mode))
	if err != nil {
		return "", err
	}
	w.u.OK(mode.Label())
	return mode, nil
}

func FirewallNotice(u *ui.UI, a *answers.Answers, wait bool, in *os.File) {
	if a == nil || !a.Mode.HasTraefik() || a.Deployment != answers.DeploymentExternal {
		return
	}
	if u == nil {
		u = ui.NewPlain(os.Stdout)
	}
	tls := a.TLS.Method != answers.TLSNone
	u.Sep()
	u.Blank()
	u.Line("%s", ui.WarnStyle.Bold(true).Render("Firewall / Port Requirements"))
	u.Line("%s", ui.MutedStyle.Render("The following ports must be open on this server's firewall:"))
	u.Blank()
	if tls {
		u.Line("  %s   HTTP (redirects to HTTPS + ACME HTTP-01 challenge)", ui.AccentStyle.Render("80/tcp"))
		u.Line("  %s  HTTPS", ui.AccentStyle.Render("443/tcp"))
	} else {
		u.Line("  %s   HTTP", ui.AccentStyle.Render("80/tcp"))
	}
	u.Blank()
	u.Code("sudo ufw allow 80/tcp")
	if tls {
		u.Code("sudo ufw allow 443/tcp")
	}
	u.Code("sudo ufw reload")
	u.Blank()
	if !wait {
		return
	}
	if in == nil {
		in = os.Stdin
	}
	fmt.Fprint(u.Writer(), "  "+ui.BoldStyle.Render("Press Enter when ports are open to continue..."))
	_, _ = bufio.NewReader(in).ReadString('\n')
	u.Blank()
}

func nonEmpty(msg string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(msg)
		}
		return nil
	}
}

func validPort(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return errors.New("must be a port number between 1 and 65535")
	}
	return nil
}

type trimmed struct {
	p *string
}

func (t trimmed) Get() string { return *t.p }

func (t trimmed) Set(v string) { *t.p = strings.TrimSpace(v) }

func input(title string, value *string) *huh.Input {
	return huh.NewInput().Title(title).Accessor(trimmed{p: value})
}

func withDefault(value *string, check func(string) error) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" && *value != "" {
			return check(*value)
		}
		return check(s)
	}
}

func checkedInput(title string, value *string, check func(string) error) *huh.Input {
	return input(title, value).Validate(withDefault(value, check))
}

func requiredInput(title string, value *string, msg string) *huh.Input {
	return checkedInput(title, value, nonEmpty(msg))
}

func containerInput(value *string) *huh.Input {
	return requiredInput("Traefik container name", value, "a container name is required")
}

func validURL(s string) error {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, "http://")
	if !ok {
		rest, ok = strings.CutPrefix(s, "https://")
	}
	if !ok {
		return errors.New("must start with http:// or https://")
	}
	if rest == "" {
		return errors.New("a url is required")
	}
	return nil
}

func pathInput(title string, value *string) *huh.Input {
	return requiredInput(title, value, "a path is required")
}

func portInput(title string, value *string) *huh.Input {
	return checkedInput(title, value, validPort)
}

func confirm(title string, value *bool) *huh.Confirm {
	return huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(value)
}

func selectOne[T comparable](title string, value *T, opts ...huh.Option[T]) *huh.Select[T] {
	return huh.NewSelect[T]().Title(title).Options(opts...).Value(value)
}

func orDefault(value, def string) string {
	if value == "" {
		return def
	}
	return value
}
