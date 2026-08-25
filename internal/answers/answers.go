package answers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Mode string

const (
	ModeFull               Mode = "full"
	ModeTMDocker           Mode = "tm-docker"
	ModeTMNative           Mode = "tm-native"
	ModeAgentDocker        Mode = "agent-docker"
	ModeAgentDockerTraefik Mode = "agent-docker-traefik"
	ModeAgentBinary        Mode = "agent-binary"
)

var Modes = []Mode{ModeFull, ModeTMDocker, ModeTMNative, ModeAgentDocker, ModeAgentDockerTraefik, ModeAgentBinary}

func (m Mode) Valid() bool {
	for _, k := range Modes {
		if k == m {
			return true
		}
	}
	return false
}

func (m Mode) IsDocker() bool {
	return m == ModeFull || m == ModeTMDocker || m == ModeAgentDocker || m == ModeAgentDockerTraefik
}

func (m Mode) IsAgent() bool {
	return m == ModeAgentDocker || m == ModeAgentDockerTraefik || m == ModeAgentBinary
}

func (m Mode) IsSystemd() bool {
	return m == ModeTMNative || m == ModeAgentBinary
}

func (m Mode) HasTraefik() bool {
	return m == ModeFull || m == ModeAgentDockerTraefik
}

func (m Mode) Label() string {
	switch m {
	case ModeFull:
		return "Traefik + Traefik Manager (full stack)"
	case ModeTMDocker:
		return "Traefik Manager only (Docker)"
	case ModeTMNative:
		return "Traefik Manager only (Linux service)"
	case ModeAgentDocker:
		return "Agent (Docker, alongside existing Traefik)"
	case ModeAgentDockerTraefik:
		return "Agent + Traefik (Docker)"
	case ModeAgentBinary:
		return "Agent binary (systemd service, no Docker)"
	}
	return string(m)
}

const (
	DeploymentExternal = "external"
	DeploymentInternal = "internal"

	TLSNone = "none"
	TLSHTTP = "http"
	TLSDNS  = "dns"

	LayoutSingle    = "single"
	LayoutDirectory = "directory"

	RestartNone       = "none"
	RestartProxy      = "proxy"
	RestartPoisonPill = "poison-pill"
	RestartSocket     = "socket"

	CrowdSecNone    = "none"
	CrowdSecInstall = "install"
	CrowdSecConnect = "connect"

	CertResolver     = "letsencrypt"
	DNSProviderOther = "other"

	ChannelStable = "stable"
	ChannelBeta   = "beta"

	ManagerImage = "ghcr.io/chr0nzz/traefik-manager"
	AgentImage   = "ghcr.io/chr0nzz/traefik-manager-agent"
)

type Hosts struct {
	Dashboard string `yaml:"dashboard,omitempty"`
	Manager   string `yaml:"manager,omitempty"`
	Agent     string `yaml:"agent,omitempty"`
}

type TLS struct {
	Method       string            `yaml:"method"`
	Provider     string            `yaml:"provider,omitempty"`
	LegoProvider string            `yaml:"lego_provider,omitempty"`
	Email        string            `yaml:"email,omitempty"`
	Vars         map[string]string `yaml:"vars,omitempty"`
	SecretVars   []string          `yaml:"secret_vars,omitempty"`
}

func (t TLS) ProviderID() string {
	if t.Provider == DNSProviderOther {
		return t.LegoProvider
	}
	return t.Provider
}

type Config struct {
	Layout string `yaml:"layout"`
	Path   string `yaml:"path,omitempty"`
	Dir    string `yaml:"dir,omitempty"`
}

type Mounts struct {
	AccessLogs       bool   `yaml:"access_logs"`
	AccessLogPath    string `yaml:"access_log_path,omitempty"`
	Certs            bool   `yaml:"certs"`
	AcmePath         string `yaml:"acme_path,omitempty"`
	StaticConfig     bool   `yaml:"static_config"`
	StaticConfigPath string `yaml:"static_config_path,omitempty"`
	Plugins          bool   `yaml:"plugins"`
	PluginsDir       string `yaml:"plugins_dir,omitempty"`
}

type Restart struct {
	Method         string `yaml:"method"`
	Container      string `yaml:"container,omitempty"`
	TraefikSystemd bool   `yaml:"traefik_systemd"`
	TraefikService string `yaml:"traefik_service,omitempty"`
	SignalFile     string `yaml:"signal_file,omitempty"`
	DockerHost     string `yaml:"docker_host,omitempty"`
}

type CrowdSec struct {
	Mode      string `yaml:"mode"`
	LAPIURL   string `yaml:"lapi_url,omitempty"`
	MachineID string `yaml:"machine_id,omitempty"`
}

type Network struct {
	Name           string `yaml:"name,omitempty"`
	External       bool   `yaml:"external"`
	TraefikAPIPort string `yaml:"traefik_api_port,omitempty"`
}

type Access struct {
	ViaTraefik bool   `yaml:"via_traefik"`
	Port       string `yaml:"port,omitempty"`
}

type Native struct {
	InstallDir  string `yaml:"install_dir,omitempty"`
	DataDir     string `yaml:"data_dir,omitempty"`
	Port        string `yaml:"port,omitempty"`
	ServiceUser bool   `yaml:"service_user"`
}

type Git struct {
	Enabled  bool   `yaml:"enabled"`
	Repo     string `yaml:"repo,omitempty"`
	Branch   string `yaml:"branch,omitempty"`
	User     string `yaml:"user,omitempty"`
	AutoPush bool   `yaml:"auto_push"`
}

type Agent struct {
	TraefikURL    string `yaml:"traefik_url,omitempty"`
	InsecureTLS   bool   `yaml:"insecure_tls"`
	BasicAuth     bool   `yaml:"basic_auth"`
	BasicAuthUser string `yaml:"basic_auth_user,omitempty"`
	ConfigPath    string `yaml:"config_path,omitempty"`
	Port          string `yaml:"port,omitempty"`
	Git           Git    `yaml:"git"`
}

type Answers struct {
	Mode       Mode              `yaml:"mode"`
	Channel    string            `yaml:"channel,omitempty"`
	Dir        string            `yaml:"dir,omitempty"`
	Deployment string            `yaml:"deployment,omitempty"`
	Domain     string            `yaml:"domain,omitempty"`
	Hosts      Hosts             `yaml:"hosts"`
	Dashboard  bool              `yaml:"dashboard"`
	TLS        TLS               `yaml:"tls"`
	Config     Config            `yaml:"config"`
	Mounts     Mounts            `yaml:"mounts"`
	Restart    Restart           `yaml:"restart"`
	CrowdSec   CrowdSec          `yaml:"crowdsec"`
	Network    Network           `yaml:"network"`
	Access     Access            `yaml:"access"`
	Native     Native            `yaml:"native"`
	Agent      Agent             `yaml:"agent"`
	Secrets    map[string]string `yaml:"secrets,omitempty"`
}

const (
	DefaultAccessLogPath    = "/var/log/traefik/access.log"
	DefaultAcmePath         = "/etc/traefik/acme.json"
	DefaultStaticConfigPath = "/etc/traefik/traefik.yml"
	DefaultPluginsDir       = "/etc/traefik/plugins"
	DefaultNetwork          = "traefik-net"
	DefaultTMNetwork        = "traefik-manager-net"
	DefaultLAPIURL          = "http://crowdsec:8080"
	DefaultSignalFile       = "/signals/restart.sig"
	DefaultNativeSignalFile = "/var/lib/traefik-manager/signals/restart.sig"
	DefaultSocketProxyHost  = "tcp://socket-proxy:2375"
	AgentBinaryPath         = "/usr/local/bin/tma"
	AgentConfigPathTraefik  = "/etc/traefik/config"
)

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "/root"
	}
	return h
}

func Defaults(mode Mode) *Answers {
	a := &Answers{
		Mode:       mode,
		Channel:    ChannelStable,
		Deployment: DeploymentExternal,
		Dashboard:  true,
		TLS:        TLS{Method: TLSHTTP},
		Config:     Config{Layout: LayoutSingle, Path: "/etc/traefik/dynamic.yml", Dir: "/etc/traefik/conf.d"},
		Mounts: Mounts{
			AccessLogs:       true,
			AccessLogPath:    DefaultAccessLogPath,
			Certs:            true,
			AcmePath:         DefaultAcmePath,
			StaticConfigPath: DefaultStaticConfigPath,
			PluginsDir:       DefaultPluginsDir,
		},
		Restart:  Restart{Method: RestartNone, Container: "traefik", TraefikService: "traefik", SignalFile: DefaultSignalFile, DockerHost: DefaultSocketProxyHost},
		CrowdSec: CrowdSec{Mode: CrowdSecNone, LAPIURL: DefaultLAPIURL},
		Network:  Network{Name: DefaultNetwork, TraefikAPIPort: "8080"},
		Access:   Access{ViaTraefik: true, Port: "5000"},
		Native:   Native{InstallDir: "/opt/traefik-manager", DataDir: "/var/lib/traefik-manager", Port: "5000", ServiceUser: true},
		Agent: Agent{
			TraefikURL: "http://traefik:8080",
			ConfigPath: "/app/config",
			Port:       "8090",
			Git:        Git{Branch: "main", AutoPush: true},
		},
	}
	switch mode {
	case ModeFull:
		a.Dir = filepath.Join(homeDir(), "traefik-stack")
	case ModeTMDocker:
		a.Dir = filepath.Join(homeDir(), "traefik-manager")
		a.Network.External = true
	case ModeTMNative:
		a.Restart.SignalFile = DefaultNativeSignalFile
	case ModeAgentDocker:
		a.Dir = "/opt/traefik-manager-agent"
		a.Mounts.AccessLogs = false
		a.Mounts.Certs = false
	case ModeAgentDockerTraefik:
		a.Dir = "/opt/traefik-manager-agent"
		a.Agent.ConfigPath = AgentConfigPathTraefik
	case ModeAgentBinary:
		a.Mounts.AccessLogs = false
		a.Mounts.Certs = false
	}
	return a
}

func (a *Answers) Clone() *Answers {
	c := *a
	if a.TLS.Vars != nil {
		c.TLS.Vars = make(map[string]string, len(a.TLS.Vars))
		for k, v := range a.TLS.Vars {
			c.TLS.Vars[k] = v
		}
	}
	c.TLS.SecretVars = append([]string(nil), a.TLS.SecretVars...)
	if a.Secrets != nil {
		c.Secrets = make(map[string]string, len(a.Secrets))
		for k, v := range a.Secrets {
			c.Secrets[k] = v
		}
	}
	return &c
}

func (a *Answers) ImageTag() string {
	if a.Channel == ChannelBeta {
		return ChannelBeta
	}
	return "latest"
}

func (a *Answers) GitBranch() string {
	if a.Channel == ChannelBeta {
		return "dev"
	}
	return "main"
}

func (a *Answers) Finalize() {
	if a.Channel == "" {
		a.Channel = ChannelStable
	}
	switch a.Mode {
	case ModeTMNative, ModeAgentDocker, ModeAgentBinary:
		a.TLS = TLS{Method: TLSNone}
	}
	if a.Domain != "" {
		if a.Hosts.Dashboard == "" {
			a.Hosts.Dashboard = "traefik." + a.Domain
		}
		if a.Hosts.Manager == "" {
			a.Hosts.Manager = "manager." + a.Domain
		}
	}
	if a.TLS.Method != TLSDNS {
		a.TLS.Provider = ""
		a.TLS.LegoProvider = ""
		a.TLS.Vars = nil
		a.TLS.SecretVars = nil
	}
	if a.TLS.Provider != DNSProviderOther {
		a.TLS.LegoProvider = ""
	}
	if a.TLS.Method == TLSNone {
		a.TLS.Email = ""
	}
	switch a.Mode {
	case ModeFull:
		if !a.Mounts.StaticConfig {
			a.Restart.Method = RestartNone
		}
		a.Restart.Container = "traefik"
		if a.CrowdSec.Mode == CrowdSecInstall {
			a.CrowdSec.LAPIURL = DefaultLAPIURL
			a.CrowdSec.MachineID = "traefik-manager"
			a.Mounts.AccessLogs = true
		}
		if a.CrowdSec.Mode == CrowdSecNone {
			a.CrowdSec.MachineID = ""
		}
	case ModeTMDocker:
		if a.TLS.Method == TLSDNS {
			a.TLS = TLS{Method: TLSHTTP, Email: a.TLS.Email}
		}
		if a.Network.Name == "" {
			if a.Network.External {
				a.Network.Name = DefaultNetwork
			} else {
				a.Network.Name = DefaultTMNetwork
			}
		}
		if a.Access.ViaTraefik {
			a.Access.Port = ""
		} else {
			a.TLS = TLS{Method: TLSNone}
		}
		if !a.Mounts.StaticConfig {
			a.Restart.Method = RestartNone
		}
		a.CrowdSec = CrowdSec{Mode: CrowdSecNone}
	case ModeTMNative:
		if !a.Mounts.StaticConfig {
			a.Restart.Method = RestartNone
			a.Restart.TraefikSystemd = false
		}
		if a.Restart.TraefikSystemd {
			a.Restart.Method = RestartPoisonPill
		}
		a.CrowdSec = CrowdSec{Mode: CrowdSecNone}
	case ModeAgentDockerTraefik:
		a.Agent.TraefikURL = "http://traefik:" + a.Network.TraefikAPIPort
		a.Agent.ConfigPath = AgentConfigPathTraefik
		a.Agent.InsecureTLS = false
		a.Agent.BasicAuth = false
		a.Agent.BasicAuthUser = ""
		a.Mounts.AccessLogs = true
		a.Mounts.AccessLogPath = DefaultAccessLogPath
		a.Mounts.Certs = a.TLS.Method != TLSNone
		a.Mounts.AcmePath = DefaultAcmePath
		a.Mounts.StaticConfig = false
		if !a.Dashboard {
			a.Hosts.Dashboard = ""
		}
		if !a.Access.ViaTraefik {
			a.Hosts.Agent = ""
		}
		if a.Restart.Method == RestartPoisonPill {
			a.Restart.SignalFile = DefaultSignalFile
		}
		if a.Restart.Method == RestartProxy {
			a.Restart.DockerHost = DefaultSocketProxyHost
		}
	}
	if a.Mode.IsAgent() {
		if a.CrowdSec.Mode == CrowdSecInstall {
			a.CrowdSec.LAPIURL = DefaultLAPIURL
			if !a.Mounts.AccessLogs {
				a.Mounts.AccessLogs = true
				if a.Mounts.AccessLogPath == "" {
					a.Mounts.AccessLogPath = DefaultAccessLogPath
				}
			}
		}
		a.CrowdSec.MachineID = ""
		if !a.Agent.Git.Enabled {
			a.Agent.Git.Repo = ""
			a.Agent.Git.User = ""
		}
		if !a.Agent.BasicAuth {
			a.Agent.BasicAuthUser = ""
		}
		if !strings.HasPrefix(a.Agent.TraefikURL, "https://") {
			a.Agent.InsecureTLS = false
		}
	}
	if a.Mode.IsDocker() && a.Dir != "" {
		a.Dir = expandHome(a.Dir)
	}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), strings.TrimPrefix(p, "~"))
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func (a *Answers) EntryPoint() string {
	if a.TLS.Method == TLSNone {
		return "web"
	}
	return "websecure"
}

func (a *Answers) Scheme() string {
	if a.TLS.Method == TLSNone {
		return "http"
	}
	return "https"
}

func (a *Answers) Validate() error {
	if !a.Mode.Valid() {
		return fmt.Errorf("unknown mode %q (valid: %s)", a.Mode, joinModes())
	}
	if err := oneOf("channel", a.Channel, ChannelStable, ChannelBeta); err != nil {
		return err
	}
	if a.Channel == ChannelBeta && a.Mode == ModeAgentBinary {
		return fmt.Errorf("there is no beta build of the agent binary, it ships only in releases: use the stable channel, or run the agent with docker")
	}
	if a.Mode.IsDocker() && a.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := oneOf("tls.method", a.TLS.Method, TLSNone, TLSHTTP, TLSDNS); err != nil {
		return err
	}
	if err := oneOf("config.layout", a.Config.Layout, LayoutSingle, LayoutDirectory); err != nil {
		return err
	}
	if err := oneOf("restart.method", a.Restart.Method, RestartNone, RestartProxy, RestartPoisonPill, RestartSocket); err != nil {
		return err
	}
	if err := oneOf("crowdsec.mode", a.CrowdSec.Mode, CrowdSecNone, CrowdSecInstall, CrowdSecConnect); err != nil {
		return err
	}
	if a.TLS.Method != TLSNone && a.TLS.Email == "" && a.Mode != ModeTMDocker {
		return fmt.Errorf("tls.email is required for Let's Encrypt")
	}
	if a.Mounts.StaticConfig && a.Restart.Method == RestartNone && !a.Mode.IsAgent() {
		return fmt.Errorf("restart.method is required when mounts.static_config is true (proxy, poison-pill, or socket)")
	}
	if err := a.validateNames(); err != nil {
		return err
	}
	if err := a.validatePaths(); err != nil {
		return err
	}
	if a.TLS.Method == TLSDNS {
		if a.TLS.Provider == "" {
			return fmt.Errorf("tls.provider is required for the DNS challenge")
		}
		if _, ok := FindDNSProvider(a.TLS.Provider); !ok && a.TLS.Provider != DNSProviderOther {
			return fmt.Errorf("unknown tls.provider %q (known: %s, or %q)", a.TLS.Provider, joinProviders(), DNSProviderOther)
		}
		if a.TLS.Provider == DNSProviderOther {
			if a.TLS.LegoProvider == "" || a.TLS.LegoProvider == DNSProviderOther {
				return fmt.Errorf("tls.lego_provider (the lego provider id) is required when tls.provider is %q", DNSProviderOther)
			}
			if len(a.TLS.SecretVars) == 0 && len(a.TLS.Vars) == 0 {
				return fmt.Errorf("tls.secret_vars or tls.vars is required when tls.provider is %q", DNSProviderOther)
			}
		}
	}
	switch a.Mode {
	case ModeFull:
		if err := oneOf("deployment", a.Deployment, DeploymentExternal, DeploymentInternal); err != nil {
			return err
		}
		if a.Domain == "" {
			return fmt.Errorf("domain is required")
		}
		if a.Hosts.Dashboard == "" || a.Hosts.Manager == "" {
			return fmt.Errorf("hosts.dashboard and hosts.manager are required")
		}
		if err := port("network.traefik_api_port", a.Network.TraefikAPIPort); err != nil {
			return err
		}
		if a.Network.Name == "" {
			return fmt.Errorf("network.name is required")
		}
		if a.CrowdSec.Mode == CrowdSecConnect && a.CrowdSec.LAPIURL == "" {
			return fmt.Errorf("crowdsec.lapi_url is required")
		}
	case ModeTMDocker:
		if a.TLS.Method == TLSDNS {
			a.TLS = TLS{Method: TLSHTTP, Email: a.TLS.Email}
		}
		if a.Network.Name == "" {
			return fmt.Errorf("network.name is required")
		}
		if a.Access.ViaTraefik {
			if a.Hosts.Manager == "" {
				return fmt.Errorf("hosts.manager is required when access.via_traefik is true")
			}
		} else if err := port("access.port", a.Access.Port); err != nil {
			return err
		}
	case ModeTMNative:
		if a.Native.InstallDir == "" || a.Native.DataDir == "" {
			return fmt.Errorf("native.install_dir and native.data_dir are required")
		}
		if err := port("native.port", a.Native.Port); err != nil {
			return err
		}
		if a.Config.Layout == LayoutSingle && a.Config.Path == "" {
			return fmt.Errorf("config.path is required")
		}
		if a.Config.Layout == LayoutDirectory && a.Config.Dir == "" {
			return fmt.Errorf("config.dir is required")
		}
		if a.Mounts.StaticConfig {
			if a.Mounts.StaticConfigPath == "" {
				return fmt.Errorf("mounts.static_config_path is required")
			}
			if a.Restart.Method == RestartProxy {
				return fmt.Errorf("restart.method proxy is not available for the Linux service install")
			}
			if a.Restart.Method == RestartPoisonPill && a.Restart.SignalFile == "" {
				return fmt.Errorf("restart.signal_file is required")
			}
		}
	case ModeAgentDocker, ModeAgentBinary:
		if a.Agent.TraefikURL == "" {
			return fmt.Errorf("agent.traefik_url is required")
		}
		if a.Agent.ConfigPath == "" {
			return fmt.Errorf("agent.config_path is required")
		}
		if err := port("agent.port", a.Agent.Port); err != nil {
			return err
		}
		if a.Mode == ModeAgentDocker && a.Dir == "" {
			return fmt.Errorf("dir is required")
		}
		if a.Mode == ModeAgentBinary && a.CrowdSec.Mode == CrowdSecInstall {
			return fmt.Errorf("crowdsec.mode install is not available for the binary agent")
		}
	case ModeAgentDockerTraefik:
		if err := oneOf("deployment", a.Deployment, DeploymentExternal, DeploymentInternal); err != nil {
			return err
		}
		if err := port("network.traefik_api_port", a.Network.TraefikAPIPort); err != nil {
			return err
		}
		if err := port("agent.port", a.Agent.Port); err != nil {
			return err
		}
		if a.Network.Name == "" {
			return fmt.Errorf("network.name is required")
		}
	}
	if a.Mode.IsAgent() {
		if a.CrowdSec.Mode == CrowdSecConnect && a.CrowdSec.LAPIURL == "" {
			return fmt.Errorf("crowdsec.lapi_url is required")
		}
		if a.Agent.Git.Enabled && a.Agent.Git.Repo == "" {
			return fmt.Errorf("agent.git.repo is required when git backup is enabled")
		}
	}
	return nil
}

var (
	hostnameRe   = regexp.MustCompile(`^\*?[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	dockerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	unitNameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@:_.-]*$`)
	legoRe       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	emailRe      = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	envNameRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func hasControl(v string) bool {
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func matches(field, value string, re *regexp.Regexp) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !re.MatchString(value) {
		return fmt.Errorf("%s contains characters that are not allowed: %q", field, value)
	}
	return nil
}

func absPath(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if hasControl(value) {
		return fmt.Errorf("%s contains a control character", field)
	}
	if !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "~") {
		return fmt.Errorf("%s must be an absolute path, got %q", field, value)
	}
	if strings.Contains(value, ": ") || strings.Contains(value, "'") {
		return fmt.Errorf("%s contains characters that are not allowed in a path here: %q", field, value)
	}
	return nil
}

func singleLine(field, value string) error {
	if hasControl(value) {
		return fmt.Errorf("%s contains a control character", field)
	}
	if strings.Contains(value, ": ") {
		return fmt.Errorf("%s cannot contain a colon followed by a space", field)
	}
	if strings.Contains(value, "'") {
		return fmt.Errorf("%s cannot contain a single quote", field)
	}
	return nil
}

func (a *Answers) validateNames() error {
	hosts := [][2]string{
		{"hosts.dashboard", a.Hosts.Dashboard},
		{"hosts.manager", a.Hosts.Manager},
		{"hosts.agent", a.Hosts.Agent},
		{"domain", a.Domain},
	}
	for _, h := range hosts {
		if h[1] == "" {
			continue
		}
		if err := matches(h[0], h[1], hostnameRe); err != nil {
			return err
		}
	}
	if a.Network.Name != "" {
		if err := matches("network.name", a.Network.Name, dockerNameRe); err != nil {
			return err
		}
	}
	if a.Restart.Container != "" {
		if err := matches("restart.container", a.Restart.Container, dockerNameRe); err != nil {
			return err
		}
	}
	if a.Restart.TraefikSystemd {
		if err := matches("restart.traefik_service", a.Restart.TraefikService, unitNameRe); err != nil {
			return err
		}
	}
	if a.TLS.Method != TLSNone && a.TLS.Email != "" {
		if err := matches("tls.email", a.TLS.Email, emailRe); err != nil {
			return err
		}
	}
	if a.TLS.Provider == DNSProviderOther && a.TLS.LegoProvider != "" {
		if err := matches("tls.lego_provider", a.TLS.LegoProvider, legoRe); err != nil {
			return err
		}
	}
	for name, v := range a.TLS.Vars {
		if err := matches("tls.vars key", name, envNameRe); err != nil {
			return err
		}
		if err := singleLine("tls.vars."+name, v); err != nil {
			return err
		}
	}
	for _, name := range a.TLS.SecretVars {
		if err := matches("tls.secret_vars entry", name, envNameRe); err != nil {
			return err
		}
	}
	for key, value := range a.Secrets {
		if err := matches("secret name", key, envNameRe); err != nil {
			return err
		}
		if hasControl(value) {
			return fmt.Errorf("the value of %s contains a control character; a secret must be a single line", key)
		}
		if strings.Contains(value, "'") {
			return fmt.Errorf("the value of %s contains a single quote, which docker compose and systemd cannot read back from an env file; change the secret", key)
		}
	}
	if a.Mode.IsAgent() {
		if err := singleLine("agent.traefik_url", a.Agent.TraefikURL); err != nil {
			return err
		}
		if err := singleLine("agent.basic_auth_user", a.Agent.BasicAuthUser); err != nil {
			return err
		}
		if a.Agent.Git.Enabled {
			for _, f := range [][2]string{
				{"agent.git.repo", a.Agent.Git.Repo},
				{"agent.git.branch", a.Agent.Git.Branch},
				{"agent.git.user", a.Agent.Git.User},
			} {
				if err := singleLine(f[0], f[1]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (a *Answers) validatePaths() error {
	checks := []struct {
		field string
		value string
		on    bool
	}{
		{"mounts.access_log_path", a.Mounts.AccessLogPath, a.Mounts.AccessLogs && !a.Mode.HasTraefik()},
		{"mounts.acme_path", a.Mounts.AcmePath, a.Mounts.Certs && !a.Mode.HasTraefik()},
		{"mounts.static_config_path", a.Mounts.StaticConfigPath, a.Mounts.StaticConfig && !a.Mode.HasTraefik()},
		{"mounts.plugins_dir", a.Mounts.PluginsDir, a.Mounts.Plugins},
		{"restart.signal_file", a.Restart.SignalFile, a.Mounts.StaticConfig && a.Restart.Method == RestartPoisonPill && a.Mode != ModeFull},
		{"config.path", a.Config.Path, a.Mode == ModeTMNative && a.Config.Layout == LayoutSingle},
		{"config.dir", a.Config.Dir, a.Mode == ModeTMNative && a.Config.Layout == LayoutDirectory},
		{"native.install_dir", a.Native.InstallDir, a.Mode == ModeTMNative},
		{"native.data_dir", a.Native.DataDir, a.Mode == ModeTMNative},
		{"agent.config_path", a.Agent.ConfigPath, a.Mode == ModeAgentDocker || a.Mode == ModeAgentBinary},
		{"dir", a.Dir, a.Mode.IsDocker()},
	}
	for _, c := range checks {
		if !c.on {
			continue
		}
		if err := absPath(c.field, c.value); err != nil {
			return err
		}
	}
	return nil
}

func oneOf(field, value string, allowed ...string) error {
	for _, v := range allowed {
		if v == value {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s, got %q", field, strings.Join(allowed, ", "), value)
}

func port(field, value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%s must be a port number, got %q", field, value)
	}
	return nil
}

func joinModes() string {
	s := make([]string, len(Modes))
	for i, m := range Modes {
		s[i] = string(m)
	}
	return strings.Join(s, ", ")
}

type DNSVar struct {
	Name    string
	Prompt  string
	Default string
	Secret  bool
}

type DNSProvider struct {
	ID    string
	Label string
	Vars  []DNSVar
}

var DNSProviders = []DNSProvider{
	{ID: "cloudflare", Label: "Cloudflare", Vars: []DNSVar{
		{Name: "CF_DNS_API_TOKEN", Prompt: "Cloudflare API Token (DNS-scoped token)", Secret: true},
	}},
	{ID: "route53", Label: "Route 53 (AWS)", Vars: []DNSVar{
		{Name: "AWS_ACCESS_KEY_ID", Prompt: "AWS Access Key ID", Secret: true},
		{Name: "AWS_SECRET_ACCESS_KEY", Prompt: "AWS Secret Access Key", Secret: true},
		{Name: "AWS_REGION", Prompt: "AWS Region", Default: "us-east-1"},
	}},
	{ID: "digitalocean", Label: "DigitalOcean", Vars: []DNSVar{
		{Name: "DO_AUTH_TOKEN", Prompt: "DigitalOcean API Token", Secret: true},
	}},
	{ID: "namecheap", Label: "Namecheap", Vars: []DNSVar{
		{Name: "NAMECHEAP_API_USER", Prompt: "Namecheap API User"},
		{Name: "NAMECHEAP_API_KEY", Prompt: "Namecheap API Key", Secret: true},
	}},
	{ID: "duckdns", Label: "DuckDNS", Vars: []DNSVar{
		{Name: "DUCKDNS_TOKEN", Prompt: "DuckDNS Token", Secret: true},
	}},
	{ID: "desec", Label: "deSEC", Vars: []DNSVar{
		{Name: "DESEC_TOKEN", Prompt: "deSEC Token", Secret: true},
	}},
}

func FindDNSProvider(id string) (DNSProvider, bool) {
	for _, p := range DNSProviders {
		if p.ID == id {
			return p, true
		}
	}
	return DNSProvider{}, false
}

func joinProviders() string {
	s := make([]string, len(DNSProviders))
	for i, p := range DNSProviders {
		s[i] = p.ID
	}
	return strings.Join(s, ", ")
}

func (a *Answers) DNSVars() []DNSVar {
	if a.TLS.Method != TLSDNS {
		return nil
	}
	if p, ok := FindDNSProvider(a.TLS.Provider); ok {
		return p.Vars
	}
	var vars []DNSVar
	for _, n := range a.TLS.SecretVars {
		vars = append(vars, DNSVar{Name: n, Prompt: n, Secret: true})
	}
	for n := range a.TLS.Vars {
		vars = append(vars, DNSVar{Name: n, Prompt: n})
	}
	return vars
}

const (
	SecretTMAAPIKey               = "TMA_API_KEY"
	SecretTraefikAPIPassword      = "TRAEFIK_API_PASSWORD"
	SecretCrowdSecAPIKey          = "CROWDSEC_API_KEY"
	SecretCrowdSecMachinePassword = "CROWDSEC_MACHINE_PASSWORD"
	SecretGitBackupToken          = "GIT_BACKUP_TOKEN"
)

type SecretSpec struct {
	Key       string
	Prompt    string
	Required  bool
	Generated bool
	Bytes     int
}

func (a *Answers) SecretSpecs() []SecretSpec {
	var specs []SecretSpec
	for _, v := range a.DNSVars() {
		if v.Secret {
			specs = append(specs, SecretSpec{Key: v.Name, Prompt: v.Prompt, Required: true})
		}
	}
	switch a.CrowdSec.Mode {
	case CrowdSecInstall:
		specs = append(specs, SecretSpec{Key: SecretCrowdSecAPIKey, Prompt: "CrowdSec bouncer key", Generated: true, Bytes: 32})
		if a.Mode == ModeFull {
			specs = append(specs, SecretSpec{Key: SecretCrowdSecMachinePassword, Prompt: "CrowdSec machine password", Generated: true, Bytes: 24})
		}
	case CrowdSecConnect:
		specs = append(specs, SecretSpec{Key: SecretCrowdSecAPIKey, Prompt: "CrowdSec API key (bouncer, for decisions)", Required: true})
		if a.Mode == ModeFull && a.CrowdSec.MachineID != "" {
			specs = append(specs, SecretSpec{Key: SecretCrowdSecMachinePassword, Prompt: "CrowdSec machine password", Required: true})
		}
	}
	if a.Mode.IsAgent() {
		specs = append(specs, SecretSpec{Key: SecretTMAAPIKey, Prompt: "API key (TMA_API_KEY)", Required: true})
		if a.Agent.BasicAuth {
			specs = append(specs, SecretSpec{Key: SecretTraefikAPIPassword, Prompt: "Traefik API password"})
		}
		if a.Agent.Git.Enabled {
			specs = append(specs, SecretSpec{Key: SecretGitBackupToken, Prompt: "Git access token"})
		}
	}
	return specs
}

func (a *Answers) MissingSecrets() []string {
	var missing []string
	for _, s := range a.SecretSpecs() {
		if s.Required && a.Secrets[s.Key] == "" {
			missing = append(missing, s.Key)
		}
	}
	return missing
}

func (a *Answers) LoadSecretsFromEnv() {
	for _, s := range a.SecretSpecs() {
		if a.Secrets[s.Key] != "" {
			continue
		}
		if v := os.Getenv(s.Key); v != "" {
			a.SetSecret(s.Key, v)
		}
	}
}

func (a *Answers) GenerateSecrets() error {
	for _, s := range a.SecretSpecs() {
		if !s.Generated || a.Secrets[s.Key] != "" {
			continue
		}
		b := make([]byte, s.Bytes)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		a.SetSecret(s.Key, hex.EncodeToString(b))
	}
	return nil
}

func (a *Answers) SetSecret(key, value string) {
	if a.Secrets == nil {
		a.Secrets = map[string]string{}
	}
	a.Secrets[key] = value
}

func (a *Answers) SecretKeys() []string {
	var keys []string
	for _, s := range a.SecretSpecs() {
		keys = append(keys, s.Key)
	}
	return keys
}

func (a *Answers) Sections() []string {
	switch a.Mode {
	case ModeFull:
		return []string{"dir", "deployment", "domain", "hosts", "dashboard", "tls", "config", "mounts", "restart", "crowdsec", "network"}
	case ModeTMDocker:
		return []string{"dir", "hosts", "tls", "config", "mounts", "restart", "network", "access"}
	case ModeTMNative:
		return []string{"native", "config", "mounts", "restart"}
	case ModeAgentDocker:
		return []string{"dir", "agent", "mounts", "restart", "crowdsec"}
	case ModeAgentDockerTraefik:
		return []string{"dir", "deployment", "dashboard", "hosts", "tls", "config", "network", "access", "agent", "mounts", "restart", "crowdsec"}
	case ModeAgentBinary:
		return []string{"agent", "mounts", "restart", "crowdsec"}
	}
	return nil
}

func (a Answers) MarshalYAML() (any, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, v any) error {
		var n yaml.Node
		if err := n.Encode(v); err != nil {
			return err
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &n)
		return nil
	}
	if err := add("mode", string(a.Mode)); err != nil {
		return nil, err
	}
	for _, s := range a.Sections() {
		var v any
		switch s {
		case "dir":
			v = a.Dir
		case "deployment":
			v = a.Deployment
		case "domain":
			v = a.Domain
		case "hosts":
			v = a.Hosts
		case "dashboard":
			v = a.Dashboard
		case "tls":
			v = a.TLS
		case "config":
			v = a.Config
		case "mounts":
			v = a.Mounts
		case "restart":
			v = a.Restart
		case "crowdsec":
			v = a.CrowdSec
		case "network":
			v = a.Network
		case "access":
			v = a.Access
		case "native":
			v = a.Native
		case "agent":
			v = a.Agent
		}
		if err := add(s, v); err != nil {
			return nil, err
		}
	}
	return root, nil
}

type plain Answers

func (a *Answers) UnmarshalYAML(n *yaml.Node) error {
	var head struct {
		Mode Mode `yaml:"mode"`
	}
	if err := n.Decode(&head); err != nil {
		return err
	}
	if head.Mode == "" {
		return fmt.Errorf("answers have no mode (valid: %s)", joinModes())
	}
	if !head.Mode.Valid() {
		return fmt.Errorf("unknown mode %q (valid: %s)", head.Mode, joinModes())
	}
	*a = *Defaults(head.Mode)
	return n.Decode((*plain)(a))
}

func Parse(data []byte) (*Answers, error) {
	var a Answers
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse answers: %w", err)
	}
	if a.Mode == "" {
		return nil, fmt.Errorf("answers file has no mode (valid: %s)", joinModes())
	}
	return &a, nil
}

func Load(path string) (*Answers, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func (a *Answers) Dump() ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(a); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
