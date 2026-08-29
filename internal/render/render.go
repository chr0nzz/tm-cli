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
	CrowdSecAcquisDir   = "/etc/crowdsec/acquis.d"
	CrowdSecAcquisPath  = CrowdSecAcquisDir + "/traefik.yaml"
	AgentUnitPath       = "/etc/systemd/system/tma.service"
	NativeUnitPath      = "/etc/systemd/system/traefik-manager.service"
	NativeEnvPath       = "/etc/traefik-manager/env"
	TraefikUnitPath     = "/etc/systemd/system/traefik.service"
	LogrotatePath       = "/etc/logrotate.d/traefik"
	RestartPathUnit     = "/etc/systemd/system/traefik-restart.path"
	RestartServiceUnit  = "/etc/systemd/system/traefik-restart.service"
	socketProxyHost     = "tcp://socket-proxy:2375"
	composeSignalFile   = "/signals/restart.sig"
	dockerSocketVolume  = "/var/run/docker.sock:/var/run/docker.sock:ro"
	traefikAccessLog    = "/var/log/traefik/access.log"
	traefikAcmeJSON     = "/etc/traefik/acme.json"
	crowdsecBouncerFull = "BOUNCER_KEY_traefikmanager"
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
	case answers.ModeFullNative:
		return renderFullNative(a)
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

func (b *builder) systemSeed(p string, mode fs.FileMode, content string) {
	b.add(File{Path: p, Mode: mode, Content: content, CreateOnly: true, Privileged: true})
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

func (b *builder) systemSeedTmpl(p string, mode fs.FileMode, name string, data any) {
	b.render(File{Path: p, Mode: mode, CreateOnly: true, Privileged: true}, name, data)
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

func sortedDNSVars(a *answers.Answers) []answers.DNSVar {
	vars := a.DNSVars()
	if _, known := answers.FindDNSProvider(a.TLS.Provider); !known {
		sort.SliceStable(vars, func(i, j int) bool {
			if vars[i].Secret || vars[j].Secret {
				return vars[i].Secret && !vars[j].Secret
			}
			return vars[i].Name < vars[j].Name
		})
	}
	return vars
}

func dnsVarValue(a *answers.Answers, v answers.DNSVar) string {
	val := a.TLS.Vars[v.Name]
	if val == "" {
		val = v.Default
	}
	return val
}

func dnsEnv(a *answers.Answers) []string {
	var env []string
	for _, v := range sortedDNSVars(a) {
		if v.Secret {
			env = append(env, v.Name+"="+ref(v.Name))
			continue
		}
		env = append(env, v.Name+"="+dnsVarValue(a, v))
	}
	return env
}

func dnsUnitEnv(a *answers.Answers) []string {
	var env []string
	for _, v := range sortedDNSVars(a) {
		if v.Secret {
			continue
		}
		env = append(env, SystemdQuote(v.Name+"="+dnsVarValue(a, v)))
	}
	return env
}

func hasSecretDNSVars(a *answers.Answers) bool {
	for _, v := range a.DNSVars() {
		if v.Secret {
			return true
		}
	}
	return false
}

type crowdsecView struct {
	Network    string
	BouncerVar string
	LogVolume  string
}

type acquisView struct {
	Path string
}

func (b *builder) acquisDocker() {
	b.tmpl("crowdsec/acquis.yaml", 0o644, "acquis.yaml.tmpl", acquisView{Path: traefikAccessLog})
}

func (b *builder) acquisNative(a *answers.Answers) {
	if a.CrowdSec.Mode != answers.CrowdSecInstall {
		return
	}
	b.systemTmpl(CrowdSecAcquisPath, 0o644, "acquis.yaml.tmpl", acquisView{Path: a.Mounts.AccessLogPath})
}

func crowdsecEnv(a *answers.Answers) []string {
	if a.CrowdSec.Mode == answers.CrowdSecNone {
		return nil
	}
	env := []string{
		"CROWDSEC_LAPI_URL=" + a.CrowdSec.LAPIURL,
		"CROWDSEC_API_KEY=" + ref(answers.SecretCrowdSecAPIKey),
	}
	if a.CrowdSec.MachineID != "" {
		env = append(env,
			"CROWDSEC_MACHINE_ID="+a.CrowdSec.MachineID,
			"CROWDSEC_MACHINE_PASSWORD="+ref(answers.SecretCrowdSecMachinePassword),
		)
	}
	if a.CrowdSec.AlertLimit != "" {
		env = append(env, "CROWDSEC_ALERT_LIMIT="+a.CrowdSec.AlertLimit)
	}
	return env
}

type traefikView struct {
	Dashboard   bool
	TLS         bool
	DNS         bool
	Network     string
	SingleFile  bool
	Resolver    string
	Email       string
	Provider    string
	APIAddress  string
	Ping        bool
	DynamicFile string
	DynamicDir  string
	AcmeStorage string
	AccessLog   string
}

func newTraefikView(a *answers.Answers) traefikView {
	return traefikView{
		Dashboard:   a.Dashboard,
		TLS:         a.TLS.Method != answers.TLSNone,
		DNS:         a.TLS.Method == answers.TLSDNS,
		Network:     a.Network.Name,
		SingleFile:  a.Config.Layout == answers.LayoutSingle,
		Resolver:    answers.CertResolver,
		Email:       a.TLS.Email,
		Provider:    a.TLS.ProviderID(),
		DynamicFile: "/etc/traefik/config/dynamic.yml",
		DynamicDir:  "/etc/traefik/config",
		AcmeStorage: "/acme.json",
		AccessLog:   "/logs/access.log",
	}
}

func newNativeTraefikView(a *answers.Answers) traefikView {
	v := newTraefikView(a)
	v.Network = ""
	v.APIAddress = "127.0.0.1:" + a.Network.TraefikAPIPort
	v.Ping = true
	v.DynamicFile = a.Config.Path
	v.DynamicDir = a.Config.Dir
	v.AcmeStorage = answers.NativeAcmePath
	v.AccessLog = answers.DefaultAccessLogPath
	return v
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
