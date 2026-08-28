package state

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

var secretKeys = []string{
	answers.SecretTMAAPIKey,
	answers.SecretTraefikAPIPassword,
	answers.SecretCrowdSecAPIKey,
	answers.SecretCrowdSecMachinePassword,
	answers.SecretGitBackupToken,
}

type adopter struct {
	c       *compose
	a       *answers.Answers
	secrets map[string]string
	static  staticInfo
}

func Adopt(dir string) (*State, map[string]string, error) {
	st, secrets, err := inspectCompose(dir)
	if err != nil {
		return nil, nil, err
	}
	return st, secrets, nil
}

func (s *State) LiteralSecrets() (map[string]string, error) {
	var secrets map[string]string
	var err error
	switch {
	case s.Mode.IsDocker():
		_, secrets, err = inspectCompose(s.Dir)
	case s.Mode == answers.ModeAgentBinary:
		_, secrets, err = inspectUnit(s.Mode)
	default:
		return map[string]string{}, nil
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	return secrets, nil
}

func inspectCompose(dir string) (*State, map[string]string, error) {
	abs := expandDir(dir)
	composePath := filepath.Join(abs, composeFileName)
	if !exists(composePath) {
		return nil, nil, fmt.Errorf("%w: no %s in %s", ErrNotFound, composeFileName, abs)
	}
	data, err := readFile(composePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", composePath, err)
	}
	c, err := parseCompose(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", composePath, err)
	}
	tm, agent, traefik := c.find(imageTM), c.find(imageAgent), c.find(imageTraefik)
	channel := answers.ChannelStable
	for _, svc := range []*service{tm, agent} {
		if svc != nil && imageTag(svc.Image) == answers.ChannelBeta {
			channel = answers.ChannelBeta
		}
	}
	var mode answers.Mode
	switch {
	case tm != nil && traefik != nil:
		mode = answers.ModeFull
	case tm != nil:
		mode = answers.ModeTMDocker
	case agent != nil && traefik != nil:
		mode = answers.ModeAgentDockerTraefik
	case agent != nil:
		mode = answers.ModeAgentDocker
	default:
		return nil, nil, fmt.Errorf("%w: %s", ErrNotAdoptable, composePath)
	}
	a := answers.Defaults(mode)
	a.Channel = channel
	a.Dir = abs
	ad := &adopter{c: c, a: a, secrets: map[string]string{}}
	owned := map[string]string{composeFileName: Hash(data)}
	staticRel := filepath.Join("traefik", "traefik.yml")
	if staticPath := filepath.Join(abs, staticRel); exists(staticPath) {
		if sd, err := readFile(staticPath); err == nil {
			owned[staticRel] = Hash(sd)
			ad.static = parseStatic(sd)
		}
	}
	switch mode {
	case answers.ModeFull:
		ad.full(tm, traefik)
	case answers.ModeTMDocker:
		ad.tmDocker(tm)
	case answers.ModeAgentDockerTraefik:
		ad.agentTraefik(agent, traefik)
	case answers.ModeAgentDocker:
		ad.agentDocker(agent)
	}
	if mode.HasTraefik() {
		if a.TLS.Method == answers.TLSHTTP {
			a.Deployment = answers.DeploymentExternal
		} else {
			a.Deployment = answers.DeploymentInternal
		}
	}
	a.Finalize()
	st := &State{
		Version:     Version,
		Mode:        mode,
		TMVersion:   AdoptedVersion,
		InstalledAt: modTime(composePath),
		Adopted:     true,
		Dir:         abs,
		OwnedFiles:  owned,
		Answers:     *a,
		Path:        PathFor(a),
	}
	return st, ad.secrets, nil
}

func (ad *adopter) full(tm, traefik *service) {
	a := ad.a
	if h, ok := traefik.routerHost("dashboard"); ok {
		a.Hosts.Dashboard = h
	}
	if h, ok := tm.routerHost("traefik-manager"); ok {
		a.Hosts.Manager = h
		a.Domain = domainOf(h)
	}
	a.Dashboard = traefik.hasLabelPrefix("traefik.http.routers.dashboard.")
	if ad.static.dashboard != nil {
		a.Dashboard = *ad.static.dashboard
	}
	ad.tls(traefik, tm, traefik)
	a.Config.Layout = layoutOf(tm, "/app/config/dynamic.yml", "/app/config/dynamic")
	a.Mounts.AccessLogs = mounted(tm, "ACCESS_LOG_PATH", "/app/logs", "/app/logs/access.log")
	a.Mounts.Certs = mounted(tm, "ACME_JSON_PATH", "/app/acme.json")
	a.Mounts.StaticConfig = mounted(tm, "STATIC_CONFIG_PATH", "/app/traefik.yml")
	a.Mounts.Plugins = tm.Environment.has("PLUGINS_DIR")
	ad.restart(tm, traefik)
	ad.collect(tm)
	ad.crowdsec(tm)
	ad.network(tm, traefik)
}

func (ad *adopter) tmDocker(tm *service) {
	a := ad.a
	if h, ok := tm.routerHost("traefik-manager"); ok {
		a.Hosts.Manager = h
	}
	a.Access.ViaTraefik = tm.hasLabelPrefix("traefik.")
	if p := tm.hostPort("5000"); p != "" {
		a.Access.Port = p
	}
	ad.tls(nil, tm)
	a.Config.Layout = layoutOf(tm, "/app/config/dynamic.yml", "/app/config/dynamic")
	a.Mounts.AccessLogs, a.Mounts.AccessLogPath = mountedPath(tm, "ACCESS_LOG_PATH", a.Mounts.AccessLogPath, "/app/logs/access.log", "/app/logs")
	a.Mounts.Certs, a.Mounts.AcmePath = mountedPath(tm, "ACME_JSON_PATH", a.Mounts.AcmePath, "/app/acme.json")
	a.Mounts.StaticConfig, a.Mounts.StaticConfigPath = mountedPath(tm, "STATIC_CONFIG_PATH", a.Mounts.StaticConfigPath, "/app/traefik.yml")
	a.Mounts.Plugins, a.Mounts.PluginsDir = envPath(tm.Environment, "PLUGINS_DIR", a.Mounts.PluginsDir)
	ad.restart(tm, nil)
	ad.collect(tm)
	ad.network(tm, nil)
}

func (ad *adopter) agentDocker(agent *service) {
	a := ad.a
	applyAgentEnv(a, agent.Environment, ad.secrets)
	if p := agent.hostPort("8090"); p != "" {
		a.Agent.Port = p
	}
	a.Agent.ConfigPath = hostSide(agent, a.Agent.ConfigPath)
	if a.Mounts.Certs {
		a.Mounts.AcmePath = hostSide(agent, a.Mounts.AcmePath)
	}
	if a.Mounts.AccessLogs {
		a.Mounts.AccessLogPath = hostSide(agent, a.Mounts.AccessLogPath)
	}
	if a.Mounts.StaticConfig {
		a.Mounts.StaticConfigPath = hostSide(agent, a.Mounts.StaticConfigPath)
	}
	if a.Mounts.Plugins {
		a.Mounts.PluginsDir = hostSide(agent, a.Mounts.PluginsDir)
	}
	ad.crowdsec(agent)
}

func (ad *adopter) agentTraefik(agent, traefik *service) {
	a := ad.a
	if h, ok := traefik.routerHost("dashboard"); ok {
		a.Hosts.Dashboard = h
	}
	a.Dashboard = traefik.hasLabelPrefix("traefik.http.routers.dashboard.")
	if ad.static.dashboard != nil && *ad.static.dashboard {
		a.Dashboard = true
	}
	if h, ok := agent.routerHost("tma"); ok {
		a.Hosts.Agent = h
	}
	a.Access.ViaTraefik = agent.hasLabelPrefix("traefik.")
	ad.tls(traefik, agent, traefik)
	a.Config.Layout = layoutOf(agent, "/etc/traefik/config/dynamic.yml", "/etc/traefik/config")
	applyAgentEnv(a, agent.Environment, ad.secrets)
	if p := agent.hostPort("8090"); p != "" {
		a.Agent.Port = p
	}
	if a.Mounts.Plugins {
		a.Mounts.PluginsDir = hostSide(agent, a.Mounts.PluginsDir)
	}
	ad.network(agent, traefik)
	if u, err := url.Parse(a.Agent.TraefikURL); err == nil && u.Port() != "" {
		a.Network.TraefikAPIPort = u.Port()
	}
	ad.crowdsec(agent)
}

func (ad *adopter) tls(traefik *service, labelled ...*service) {
	a := ad.a
	ad.collectDNS(traefik)
	on := ad.static.hasResolver
	for _, s := range labelled {
		if s.hasLabelSuffix(".tls.certresolver") {
			on = true
		}
	}
	if traefik.hostPort("443") != "" {
		on = true
	}
	if !on {
		a.TLS.Method = answers.TLSNone
		return
	}
	a.TLS.Method = answers.TLSHTTP
	a.TLS.Email = ad.static.email
	provider := ""
	if traefik != nil {
		for _, p := range answers.DNSProviders {
			for _, v := range p.Vars {
				if traefik.Environment.has(v.Name) {
					provider = p.ID
					break
				}
			}
			if provider != "" {
				break
			}
		}
	}
	if ad.static.dnsProvider != "" {
		if _, ok := answers.FindDNSProvider(ad.static.dnsProvider); ok {
			provider = ad.static.dnsProvider
		} else {
			provider = answers.DNSProviderOther
		}
	}
	if provider == "" {
		return
	}
	a.TLS.Method = answers.TLSDNS
	a.TLS.Provider = provider
	if traefik == nil {
		return
	}
	if provider == answers.DNSProviderOther {
		for _, e := range traefik.Environment {
			a.TLS.SecretVars = append(a.TLS.SecretVars, e.Key)
			if e.Value != "" && !isReference(e.Value) {
				ad.secrets[e.Key] = e.Value
			}
		}
		return
	}
	p, _ := answers.FindDNSProvider(provider)
	for _, v := range p.Vars {
		val, ok := traefik.Environment.get(v.Name)
		if !ok || v.Secret || isReference(val) {
			continue
		}
		if a.TLS.Vars == nil {
			a.TLS.Vars = map[string]string{}
		}
		a.TLS.Vars[v.Name] = val
	}
}

func (ad *adopter) collectDNS(traefik *service) {
	if traefik == nil {
		return
	}
	for _, p := range answers.DNSProviders {
		for _, v := range p.Vars {
			if v.Secret {
				collectEnv(traefik.Environment, ad.secrets, v.Name)
			}
		}
	}
}

func (ad *adopter) collect(svc *service) {
	collectEnv(svc.Environment, ad.secrets, secretKeys...)
}

func collectEnv(env kvList, secrets map[string]string, keys ...string) {
	for _, k := range keys {
		if v, ok := env.get(k); ok && v != "" && !isReference(v) {
			secrets[k] = v
		}
	}
}

func (ad *adopter) restart(svc, traefik *service) {
	applyRestartEnv(ad.a, svc.Environment)
	if !svc.Environment.has("RESTART_METHOD") {
		if v, ok := traefik.label("traefik-manager.restart-method"); ok {
			ad.a.Restart.Method = restartMethod(v)
		}
	}
}

func (ad *adopter) crowdsec(svc *service) {
	a := ad.a
	cs := ad.c.find(imageCrowdSec)
	switch {
	case cs != nil:
		a.CrowdSec.Mode = answers.CrowdSecInstall
		for _, e := range cs.Environment {
			if !strings.HasPrefix(e.Key, "BOUNCER_KEY_") || e.Value == "" || isReference(e.Value) {
				continue
			}
			if _, ok := ad.secrets[answers.SecretCrowdSecAPIKey]; !ok {
				ad.secrets[answers.SecretCrowdSecAPIKey] = e.Value
			}
		}
	case svc.Environment.has("CROWDSEC_LAPI_URL"):
		a.CrowdSec.Mode = answers.CrowdSecConnect
		a.CrowdSec.LAPIURL, _ = svc.env("CROWDSEC_LAPI_URL")
		if id, ok := svc.env("CROWDSEC_MACHINE_ID"); ok {
			a.CrowdSec.MachineID = id
		}
	default:
		a.CrowdSec.Mode = answers.CrowdSecNone
	}
}

func (ad *adopter) network(svc, traefik *service) {
	a := ad.a
	for _, n := range svc.Networks {
		def := ad.c.network(n)
		if n == "socket-proxy-net" || bool(def.Internal) {
			continue
		}
		a.Network.Name = n
		if def.Name != "" {
			a.Network.Name = def.Name
		}
		a.Network.External = bool(def.External)
		break
	}
	if p := traefik.hostPort("8080"); p != "" {
		a.Network.TraefikAPIPort = p
	}
}

func applyAgentEnv(a *answers.Answers, env kvList, secrets map[string]string) {
	if v, ok := env.get("TRAEFIK_API_URL"); ok {
		a.Agent.TraefikURL = v
	}
	a.Agent.InsecureTLS = env.truthy("TRAEFIK_INSECURE_SKIP_VERIFY")
	if v, ok := env.get("TRAEFIK_API_USER"); ok && v != "" {
		a.Agent.BasicAuth = true
		a.Agent.BasicAuthUser = v
	} else {
		a.Agent.BasicAuth = false
		a.Agent.BasicAuthUser = ""
	}
	if v, ok := env.get("CONFIG_PATH"); ok && v != "" {
		a.Agent.ConfigPath = v
	}
	if v, ok := env.get("TMA_PORT"); ok && v != "" {
		a.Agent.Port = v
	}
	a.Mounts.Certs, a.Mounts.AcmePath = envPath(env, "ACME_JSON_PATH", a.Mounts.AcmePath)
	a.Mounts.AccessLogs, a.Mounts.AccessLogPath = envPath(env, "ACCESS_LOG_PATH", a.Mounts.AccessLogPath)
	a.Mounts.StaticConfig, a.Mounts.StaticConfigPath = envPath(env, "STATIC_CONFIG_PATH", a.Mounts.StaticConfigPath)
	a.Mounts.Plugins, a.Mounts.PluginsDir = envPath(env, "PLUGINS_DIR", a.Mounts.PluginsDir)
	applyRestartEnv(a, env)
	if v, ok := env.get("CROWDSEC_LAPI_URL"); ok && v != "" {
		a.CrowdSec.Mode = answers.CrowdSecConnect
		a.CrowdSec.LAPIURL = v
	} else {
		a.CrowdSec.Mode = answers.CrowdSecNone
	}
	a.Agent.Git.Enabled = env.truthy("GIT_BACKUP_ENABLED")
	if v, ok := env.get("GIT_BACKUP_REPO"); ok {
		a.Agent.Git.Repo = v
	}
	if v, ok := env.get("GIT_BACKUP_BRANCH"); ok && v != "" {
		a.Agent.Git.Branch = v
	}
	if v, ok := env.get("GIT_BACKUP_USERNAME"); ok {
		a.Agent.Git.User = v
	}
	if env.has("GIT_BACKUP_AUTO_PUSH") {
		a.Agent.Git.AutoPush = env.truthy("GIT_BACKUP_AUTO_PUSH")
	}
	collectEnv(env, secrets, secretKeys...)
}

func applyRestartEnv(a *answers.Answers, env kvList) {
	method, _ := env.get("RESTART_METHOD")
	a.Restart.Method = restartMethod(method)
	if v, ok := env.get("TRAEFIK_CONTAINER"); ok && v != "" {
		a.Restart.Container = v
	}
	if v, ok := env.get("DOCKER_HOST"); ok && v != "" {
		a.Restart.DockerHost = v
	}
	if v, ok := env.get("SIGNAL_FILE_PATH"); ok && v != "" {
		a.Restart.SignalFile = v
	}
}

func restartMethod(v string) string {
	switch strings.TrimSpace(v) {
	case answers.RestartProxy, answers.RestartPoisonPill, answers.RestartSocket:
		return strings.TrimSpace(v)
	}
	return answers.RestartNone
}

func layoutOf(svc *service, single, dir string) string {
	if _, ok := svc.mountSource(single); ok {
		return answers.LayoutSingle
	}
	if _, ok := svc.mountSource(dir); ok {
		return answers.LayoutDirectory
	}
	if svc.Environment.has("CONFIG_DIR") {
		return answers.LayoutDirectory
	}
	return answers.LayoutSingle
}

func mounted(svc *service, envKey string, targets ...string) bool {
	for _, t := range targets {
		if _, ok := svc.mountSource(t); ok {
			return true
		}
	}
	return svc.Environment.has(envKey)
}

func mountedPath(svc *service, envKey, def string, targets ...string) (bool, string) {
	for _, t := range targets {
		if src, ok := svc.mountSource(t); ok {
			if isHostPath(src) {
				return true, src
			}
			return true, def
		}
	}
	return svc.Environment.has(envKey), def
}

func envPath(env kvList, key, def string) (bool, string) {
	if v, ok := env.get(key); ok && v != "" {
		return true, v
	}
	return false, def
}

func hostSide(svc *service, containerPath string) string {
	if src, ok := svc.mountSource(containerPath); ok && isHostPath(src) {
		return src
	}
	return containerPath
}

func isHostPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

func domainOf(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) > 2 {
		return strings.Join(parts[1:], ".")
	}
	return host
}

type staticInfo struct {
	email       string
	dnsProvider string
	hasResolver bool
	dashboard   *bool
}

func parseStatic(data []byte) staticInfo {
	var doc struct {
		API struct {
			Dashboard *bool `yaml:"dashboard"`
		} `yaml:"api"`
		Resolvers map[string]struct {
			ACME struct {
				Email string `yaml:"email"`
				DNS   *struct {
					Provider string `yaml:"provider"`
				} `yaml:"dnsChallenge"`
			} `yaml:"acme"`
		} `yaml:"certificatesResolvers"`
	}
	var info staticInfo
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return info
	}
	info.dashboard = doc.API.Dashboard
	names := make([]string, 0, len(doc.Resolvers))
	for n := range doc.Resolvers {
		names = append(names, n)
	}
	sort.Strings(names)
	if _, ok := doc.Resolvers[answers.CertResolver]; ok {
		names = append([]string{answers.CertResolver}, names...)
	}
	for _, n := range names {
		r := doc.Resolvers[n]
		info.hasResolver = true
		info.email = r.ACME.Email
		if r.ACME.DNS != nil {
			info.dnsProvider = r.ACME.DNS.Provider
		}
		break
	}
	return info
}
