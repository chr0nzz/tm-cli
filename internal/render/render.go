package render

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

type File struct {
	Path       string
	Mode       fs.FileMode
	Content    string
	CreateOnly bool
	Privileged bool
}

type Output struct {
	Dirs  []string
	Files []File
}

type Input struct {
	Answers *answers.Answers
	User    string
}

const (
	AgentEnvPath        = "/etc/traefik-manager-agent/env"
	AgentUnitPath       = "/etc/systemd/system/tma.service"
	NativeUnitPath      = "/etc/systemd/system/traefik-manager.service"
	RestartPathUnit     = "/etc/systemd/system/traefik-restart.path"
	RestartServiceUnit  = "/etc/systemd/system/traefik-restart.service"
	socketProxyHost     = "tcp://socket-proxy:2375"
	composeSignalFile   = "/signals/restart.sig"
	dockerSocketVolume  = "/var/run/docker.sock:/var/run/docker.sock:ro"
	traefikAccessLog    = "/var/log/traefik/access.log"
	traefikAcmeJSON     = "/etc/traefik/acme.json"
	crowdsecLAPIURL     = "http://crowdsec:8080"
	crowdsecBouncerFull = "BOUNCER_KEY_traefik-manager"
	crowdsecBouncerTMA  = "BOUNCER_KEY_tma"
)

func Render(in Input) (*Output, error) {
	a := in.Answers
	if a == nil {
		return nil, errors.New("render: answers are required")
	}
	switch a.Mode {
	case answers.ModeFull:
		return renderFull(a)
	case answers.ModeTMDocker:
		return renderTMDocker(a)
	case answers.ModeTMNative:
		return renderTMNative(a, in.User)
	case answers.ModeAgentDocker:
		return renderAgentDocker(a)
	case answers.ModeAgentDockerTraefik:
		return renderAgentDockerTraefik(a)
	case answers.ModeAgentBinary:
		return renderAgentBinary(a)
	}
	return nil, fmt.Errorf("render: unknown mode %q", a.Mode)
}

func EnvFile(a *answers.Answers) string {
	var b strings.Builder
	for _, k := range a.SecretKeys() {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(EnvValue(a.Secrets[k]))
		b.WriteString("\n")
	}
	return b.String()
}

func EnvValue(v string) string {
	return "'" + v + "'"
}

var systemdEscape = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%")

func SystemdQuote(v string) string {
	return `"` + systemdEscape.Replace(v) + `"`
}

type builder struct {
	out Output
	err error
}

func (b *builder) dir(p string) {
	b.out.Dirs = append(b.out.Dirs, p)
}

func (b *builder) add(f File) {
	b.out.Files = append(b.out.Files, f)
}

func (b *builder) file(p string, mode fs.FileMode, content string) {
	b.add(File{Path: p, Mode: mode, Content: content})
}

func (b *builder) seed(p string, mode fs.FileMode, content string) {
	b.add(File{Path: p, Mode: mode, Content: content, CreateOnly: true})
}

func (b *builder) system(p string, mode fs.FileMode, content string) {
	b.add(File{Path: p, Mode: mode, Content: content, Privileged: true})
}

func (b *builder) render(f File, name string, data any) {
	if b.err != nil {
		return
	}
	content, err := execute(name, data)
	if err != nil {
		b.err = err
		return
	}
	f.Content = content
	b.add(f)
}

func (b *builder) tmpl(p string, mode fs.FileMode, name string, data any) {
	b.render(File{Path: p, Mode: mode}, name, data)
}

func (b *builder) seedTmpl(p string, mode fs.FileMode, name string, data any) {
	b.render(File{Path: p, Mode: mode, CreateOnly: true}, name, data)
}

func (b *builder) systemTmpl(p string, mode fs.FileMode, name string, data any) {
	b.render(File{Path: p, Mode: mode, Privileged: true}, name, data)
}

func (b *builder) env(a *answers.Answers) {
	if len(a.SecretKeys()) == 0 {
		return
	}
	b.file(".env", 0o600, EnvFile(a))
}

func (b *builder) result() (*Output, error) {
	if b.err != nil {
		return nil, b.err
	}
	return &b.out, nil
}

func ref(key string) string {
	return "${" + key + "}"
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func dnsEnv(a *answers.Answers) []string {
	vars := a.DNSVars()
	if _, known := answers.FindDNSProvider(a.TLS.Provider); !known {
		sort.SliceStable(vars, func(i, j int) bool {
			if vars[i].Secret || vars[j].Secret {
				return vars[i].Secret && !vars[j].Secret
			}
			return vars[i].Name < vars[j].Name
		})
	}
	var env []string
	for _, v := range vars {
		if v.Secret {
			env = append(env, v.Name+"="+ref(v.Name))
			continue
		}
		val := a.TLS.Vars[v.Name]
		if val == "" {
			val = v.Default
		}
		env = append(env, v.Name+"="+val)
	}
	return env
}

type crowdsecView struct {
	Network    string
	BouncerVar string
	LogVolume  string
}

type traefikView struct {
	Dashboard  bool
	TLS        bool
	DNS        bool
	Network    string
	SingleFile bool
	Resolver   string
	Email      string
	Provider   string
}

func newTraefikView(a *answers.Answers) traefikView {
	return traefikView{
		Dashboard:  a.Dashboard,
		TLS:        a.TLS.Method != answers.TLSNone,
		DNS:        a.TLS.Method == answers.TLSDNS,
		Network:    a.Network.Name,
		SingleFile: a.Config.Layout == answers.LayoutSingle,
		Resolver:   answers.CertResolver,
		Email:      a.TLS.Email,
		Provider:   a.TLS.ProviderID(),
	}
}

func tlsLabel(router string, on bool) []string {
	if !on {
		return nil
	}
	return []string{"traefik.http.routers." + router + ".tls.certresolver=" + answers.CertResolver}
}

func routerLabels(router, host, entryPoint string) []string {
	return []string{
		"traefik.enable=true",
		"traefik.http.routers." + router + ".rule=Host(`" + host + "`)",
		"traefik.http.routers." + router + ".entrypoints=" + entryPoint,
	}
}
