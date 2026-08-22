package render

import (
	"github.com/chr0nzz/traefik-stack/internal/answers"
)

type envVar struct {
	Key    string
	Value  string
	Secret bool
}

type agentPaths struct {
	TraefikURL string
	ConfigPath string
	Static     string
	Acme       string
	Log        string
	Plugins    string
}

func agentDockerPaths(a *answers.Answers) agentPaths {
	p := agentPaths{TraefikURL: a.Agent.TraefikURL, ConfigPath: a.Agent.ConfigPath}
	if a.Mounts.StaticConfig {
		p.Static = a.Mounts.StaticConfigPath
	}
	if a.Mounts.Certs {
		p.Acme = a.Mounts.AcmePath
	}
	if a.Mounts.AccessLogs {
		p.Log = a.Mounts.AccessLogPath
	}
	if a.Mounts.Plugins {
		p.Plugins = a.Mounts.PluginsDir
	}
	return p
}

func agentTraefikPaths(a *answers.Answers) agentPaths {
	p := agentPaths{
		TraefikURL: "http://traefik:8080",
		ConfigPath: answers.AgentConfigPathTraefik,
		Log:        traefikAccessLog,
	}
	if a.TLS.Method != answers.TLSNone {
		p.Acme = traefikAcmeJSON
	}
	if a.Mounts.Plugins {
		p.Plugins = a.Mounts.PluginsDir
	}
	return p
}

func agentEnv(a *answers.Answers, p agentPaths, compose bool) []envVar {
	var env []envVar
	add := func(k, v string) { env = append(env, envVar{Key: k, Value: v}) }
	secret := func(k string) { env = append(env, envVar{Key: k, Value: ref(k), Secret: true}) }
	secret(answers.SecretTMAAPIKey)
	add("TRAEFIK_API_URL", p.TraefikURL)
	if a.Agent.BasicAuth {
		if a.Agent.BasicAuthUser != "" {
			add("TRAEFIK_API_USER", a.Agent.BasicAuthUser)
		}
		secret(answers.SecretTraefikAPIPassword)
	}
	if compose {
		add("TMA_RATE_LIMIT", "300")
	}
	add("CONFIG_PATH", p.ConfigPath)
	if a.Agent.InsecureTLS {
		add("TRAEFIK_INSECURE_SKIP_VERIFY", "true")
	}
	if p.Static != "" {
		add("STATIC_CONFIG_PATH", p.Static)
	}
	if a.Restart.Method != answers.RestartNone {
		add("RESTART_METHOD", a.Restart.Method)
		if a.Restart.Container != "" {
			add("TRAEFIK_CONTAINER", a.Restart.Container)
		}
		if a.Restart.Method == answers.RestartProxy && a.Restart.DockerHost != "" {
			add("DOCKER_HOST", a.Restart.DockerHost)
		}
		if a.Restart.Method == answers.RestartPoisonPill && a.Restart.SignalFile != "" {
			add("SIGNAL_FILE_PATH", a.Restart.SignalFile)
		}
	}
	if p.Acme != "" {
		add("ACME_JSON_PATH", p.Acme)
	}
	if p.Log != "" {
		add("ACCESS_LOG_PATH", p.Log)
	}
	if p.Plugins != "" {
		add("PLUGINS_DIR", p.Plugins)
	}
	switch a.CrowdSec.Mode {
	case answers.CrowdSecInstall:
		add("CROWDSEC_LAPI_URL", crowdsecLAPIURL)
		secret(answers.SecretCrowdSecAPIKey)
	case answers.CrowdSecConnect:
		if a.CrowdSec.LAPIURL != "" {
			add("CROWDSEC_LAPI_URL", a.CrowdSec.LAPIURL)
		}
		secret(answers.SecretCrowdSecAPIKey)
	}
	if a.Agent.Git.Enabled {
		add("GIT_BACKUP_ENABLED", "true")
		if a.Agent.Git.Repo != "" {
			add("GIT_BACKUP_REPO", a.Agent.Git.Repo)
		}
		add("GIT_BACKUP_BRANCH", a.Agent.Git.Branch)
		if a.Agent.Git.User != "" {
			add("GIT_BACKUP_USERNAME", a.Agent.Git.User)
		}
		secret(answers.SecretGitBackupToken)
		add("GIT_BACKUP_AUTO_PUSH", boolString(a.Agent.Git.AutoPush))
	}
	return env
}

func envLines(env []envVar, includeSecrets bool) []string {
	var lines []string
	for _, e := range env {
		if e.Secret && !includeSecrets {
			continue
		}
		lines = append(lines, e.Key+"="+e.Value)
	}
	return lines
}

func agentNamedVolumes(a *answers.Answers) []string {
	var vols []string
	if a.Restart.Method == answers.RestartPoisonPill {
		vols = append(vols, "traefik-signals")
	}
	if a.CrowdSec.Mode == answers.CrowdSecInstall {
		vols = append(vols, "crowdsec_data")
	}
	return vols
}

type agentView struct {
	Port         string
	Env          []string
	Volumes      []string
	CrowdSec     *crowdsecView
	NamedVolumes []string
}

func renderAgentDocker(a *answers.Answers) (*Output, error) {
	install := a.CrowdSec.Mode == answers.CrowdSecInstall
	p := agentDockerPaths(a)
	v := agentView{
		Port:         a.Agent.Port,
		Env:          envLines(agentEnv(a, p, true), true),
		NamedVolumes: agentNamedVolumes(a),
	}
	v.Volumes = []string{p.ConfigPath + ":" + p.ConfigPath, "./backups:/app/backups"}
	if p.Static != "" {
		v.Volumes = append(v.Volumes, p.Static+":"+p.Static)
	}
	if p.Acme != "" {
		v.Volumes = append(v.Volumes, p.Acme+":"+p.Acme+":ro")
	}
	if p.Log != "" {
		v.Volumes = append(v.Volumes, p.Log+":"+p.Log+":ro")
	}
	if p.Plugins != "" {
		v.Volumes = append(v.Volumes, p.Plugins+":"+p.Plugins+":ro")
	}
	if a.Restart.Method == answers.RestartSocket {
		v.Volumes = append(v.Volumes, dockerSocketVolume)
	}
	if a.Restart.Method == answers.RestartPoisonPill {
		v.Volumes = append(v.Volumes, "traefik-signals:/signals")
	}
	if install {
		v.CrowdSec = &crowdsecView{BouncerVar: crowdsecBouncerTMA}
		if p.Log != "" {
			v.CrowdSec.LogVolume = p.Log + ":" + traefikAccessLog + ":ro"
		}
	}

	b := &builder{}
	b.dir("backups")
	if install {
		b.dir("crowdsec")
	}
	b.tmpl("docker-compose.yml", 0o644, "compose-agent.tmpl", v)
	b.env(a)
	if install {
		b.tmpl("crowdsec/acquis.yaml", 0o644, "acquis.yaml.tmpl", nil)
	}
	return b.result()
}

type agentTraefikView struct {
	Network         string
	NetworkExternal bool
	APIPort         string
	SocketProxy     bool
	PoisonPill      bool
	TLS             bool
	TraefikVolumes  []string
	DNSEnv          []string
	TraefikLabels   []string
	Port            string
	Env             []string
	Volumes         []string
	AgentLabels     []string
	CrowdSec        *crowdsecView
	NamedVolumes    []string
}

func renderAgentDockerTraefik(a *answers.Answers) (*Output, error) {
	install := a.CrowdSec.Mode == answers.CrowdSecInstall
	tls := a.TLS.Method != answers.TLSNone
	single := a.Config.Layout == answers.LayoutSingle
	p := agentTraefikPaths(a)
	v := agentTraefikView{
		Network:         a.Network.Name,
		NetworkExternal: a.Network.External,
		APIPort:         a.Network.TraefikAPIPort,
		TLS:             tls,
		DNSEnv:          dnsEnv(a),
		Port:            a.Agent.Port,
		SocketProxy:     a.Restart.Method == answers.RestartProxy,
		PoisonPill:      a.Restart.Method == answers.RestartPoisonPill,
		Env:             envLines(agentEnv(a, p, true), true),
		NamedVolumes:    agentNamedVolumes(a),
	}

	v.TraefikVolumes = []string{
		dockerSocketVolume,
		"./traefik/traefik.yml:/traefik.yml:ro",
		"./traefik/logs:/logs",
	}
	if tls {
		v.TraefikVolumes = append(v.TraefikVolumes, "./traefik/acme.json:/acme.json")
	}
	if single {
		v.TraefikVolumes = append(v.TraefikVolumes, "./traefik/config/dynamic.yml:/etc/traefik/config/dynamic.yml:ro")
	} else {
		v.TraefikVolumes = append(v.TraefikVolumes, "./traefik/config:/etc/traefik/config:ro")
	}
	if a.Dashboard && a.Hosts.Dashboard != "" {
		v.TraefikLabels = append(routerLabels("dashboard", a.Hosts.Dashboard, a.EntryPoint()), "traefik.http.routers.dashboard.service=api@internal")
		v.TraefikLabels = append(v.TraefikLabels, tlsLabel("dashboard", tls)...)
	}

	if single {
		v.Volumes = append(v.Volumes, "./traefik/config/dynamic.yml:/etc/traefik/config/dynamic.yml")
	} else {
		v.Volumes = append(v.Volumes, "./traefik/config:/etc/traefik/config")
	}
	v.Volumes = append(v.Volumes, "./backups:/app/backups", "./traefik/logs/access.log:"+traefikAccessLog+":ro")
	if tls {
		v.Volumes = append(v.Volumes, "./traefik/acme.json:"+traefikAcmeJSON+":ro")
	}
	if p.Plugins != "" {
		v.Volumes = append(v.Volumes, p.Plugins+":"+p.Plugins+":ro")
	}
	if a.Restart.Method == answers.RestartSocket {
		v.Volumes = append(v.Volumes, dockerSocketVolume)
	}
	if a.Restart.Method == answers.RestartPoisonPill {
		v.Volumes = append(v.Volumes, "traefik-signals:/signals")
		v.TraefikVolumes = append(v.TraefikVolumes, "traefik-signals:/signals")
	}
	if a.Hosts.Agent != "" {
		v.AgentLabels = append(routerLabels("tma", a.Hosts.Agent, a.EntryPoint()), "traefik.http.services.tma.loadbalancer.server.port=8090")
		v.AgentLabels = append(v.AgentLabels, tlsLabel("tma", tls)...)
	}
	if install {
		v.CrowdSec = &crowdsecView{
			Network:    a.Network.Name,
			BouncerVar: crowdsecBouncerTMA,
			LogVolume:  "./traefik/logs/access.log:" + traefikAccessLog + ":ro",
		}
	}

	b := &builder{}
	b.dir("traefik/config")
	b.dir("traefik/logs")
	b.dir("backups")
	if install {
		b.dir("crowdsec")
	}
	b.tmpl("docker-compose.yml", 0o644, "compose-agent-traefik.tmpl", v)
	b.env(a)
	b.tmpl("traefik/traefik.yml", 0o644, "traefik.yml.tmpl", newTraefikView(a))
	if single {
		b.seedTmpl("traefik/config/dynamic.yml", 0o644, "dynamic.yml.tmpl", nil)
	}
	b.seed("traefik/logs/access.log", 0o644, "")
	if tls {
		b.seed("traefik/acme.json", 0o600, "")
	}
	if install {
		b.tmpl("crowdsec/acquis.yaml", 0o644, "acquis.yaml.tmpl", nil)
	}
	return b.result()
}

type agentUnitView struct {
	EnvFile string
	Env     []string
}

func renderAgentBinary(a *answers.Answers) (*Output, error) {
	env := envLines(agentEnv(a, agentDockerPaths(a), false), false)
	env = append(env, "TMA_PORT="+a.Agent.Port)
	for i, line := range env {
		env[i] = SystemdQuote(line)
	}
	b := &builder{}
	b.dir("/etc/traefik-manager-agent")
	b.systemTmpl(AgentUnitPath, 0o644, "tma.service.tmpl", agentUnitView{EnvFile: AgentEnvPath, Env: env})
	b.system(AgentEnvPath, 0o600, EnvFile(a))
	return b.result()
}
