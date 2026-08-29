package render

import (
	"github.com/chr0nzz/tm-cli/internal/answers"
)

type fullView struct {
	Image           string
	Network         string
	NetworkExternal bool
	SocketProxy     bool
	PoisonPill      bool
	NamedVolumes    []string
	TLS             bool
	APIPort         string
	TraefikVolumes  []string
	DNSEnv          []string
	TraefikLabels   []string
	TMNetworks      []string
	TMVolumes       []string
	TMEnv           []string
	TMLabels        []string
	CrowdSec        *crowdsecView
}

func renderFull(a *answers.Answers) (*Output, error) {
	b := &builder{}
	install := a.CrowdSec.Mode == answers.CrowdSecInstall
	single := a.Config.Layout == answers.LayoutSingle
	b.dir("traefik/config")
	b.dir("traefik/logs")
	b.dir("traefik-manager/config")
	b.dir("traefik-manager/backups")
	if install {
		b.dir("crowdsec")
	}
	b.tmpl("docker-compose.yml", 0o644, "compose-full.tmpl", newFullView(a))
	b.env(a)
	b.tmpl("traefik/traefik.yml", 0o644, "traefik.yml.tmpl", newTraefikView(a))
	if single {
		b.seedTmpl("traefik/config/dynamic.yml", 0o644, "dynamic.yml.tmpl", dashboardView{})
	} else {
		b.seedTmpl("traefik/config/example-app.yml.disabled", 0o644, "example-app.yml.tmpl", nil)
	}
	b.seed("traefik/acme.json", 0o600, "")
	b.seed("traefik/logs/access.log", 0o644, "")
	if install {
		b.acquisDocker()
	}
	return b.result()
}

func newFullView(a *answers.Answers) fullView {
	static := a.Mounts.StaticConfig
	proxy := static && a.Restart.Method == answers.RestartProxy
	pill := static && a.Restart.Method == answers.RestartPoisonPill
	socket := static && a.Restart.Method == answers.RestartSocket
	tls := a.TLS.Method != answers.TLSNone
	single := a.Config.Layout == answers.LayoutSingle
	install := a.CrowdSec.Mode == answers.CrowdSecInstall
	v := fullView{
		Image:           answers.ManagerImage + ":" + a.ImageTag(),
		Network:         a.Network.Name,
		NetworkExternal: a.Network.External,
		SocketProxy:     proxy,
		PoisonPill:      pill,
		TLS:             tls,
		APIPort:         a.Network.TraefikAPIPort,
		DNSEnv:          dnsEnv(a),
	}
	if pill {
		v.NamedVolumes = append(v.NamedVolumes, "tm-signals")
	}
	if install {
		v.NamedVolumes = append(v.NamedVolumes, "crowdsec_data")
	}

	v.TraefikVolumes = []string{
		dockerSocketVolume,
		"./traefik/traefik.yml:/traefik.yml:ro",
		"./traefik/acme.json:/acme.json",
		"./traefik/logs:/logs",
	}
	if single {
		v.TraefikVolumes = append(v.TraefikVolumes, "./traefik/config/dynamic.yml:/etc/traefik/config/dynamic.yml:ro")
	} else {
		v.TraefikVolumes = append(v.TraefikVolumes, "./traefik/config:/etc/traefik/config:ro")
	}
	if pill {
		v.TraefikVolumes = append(v.TraefikVolumes, "tm-signals:/signals")
	}

	v.TraefikLabels = append(routerLabels("dashboard", a.Hosts.Dashboard, a.EntryPoint()), "traefik.http.routers.dashboard.service=api@internal")
	v.TraefikLabels = append(v.TraefikLabels, tlsLabel("dashboard", tls)...)
	if static {
		v.TraefikLabels = append(v.TraefikLabels,
			"traefik-manager.role=traefik",
			"traefik-manager.static-config=/app/traefik.yml",
			"traefik-manager.restart-method="+a.Restart.Method,
		)
	}

	v.TMNetworks = []string{a.Network.Name}
	if proxy {
		v.TMNetworks = append(v.TMNetworks, "socket-proxy-net")
	}

	if socket {
		v.TMVolumes = append(v.TMVolumes, dockerSocketVolume)
	}
	v.TMVolumes = append(v.TMVolumes, "./traefik-manager/config:/app/config", "./traefik-manager/backups:/app/backups")
	if a.Mounts.AccessLogs {
		v.TMVolumes = append(v.TMVolumes, "./traefik/logs:/app/logs:ro")
	}
	if a.Mounts.Certs {
		v.TMVolumes = append(v.TMVolumes, "./traefik/acme.json:/app/acme.json:ro")
	}
	if static {
		v.TMVolumes = append(v.TMVolumes, "./traefik/traefik.yml:/app/traefik.yml")
		if pill {
			v.TMVolumes = append(v.TMVolumes, "tm-signals:/signals")
		}
	}
	if single {
		v.TMVolumes = append(v.TMVolumes, "./traefik/config/dynamic.yml:/app/config/dynamic.yml")
	} else {
		v.TMVolumes = append(v.TMVolumes, "./traefik/config:/app/config/dynamic")
	}

	v.TMEnv = append(tmEnv(a, static, proxy, pill, single), crowdsecEnv(a)...)

	v.TMLabels = tmLabels(a)

	if install {
		v.CrowdSec = &crowdsecView{
			Network:    a.Network.Name,
			BouncerVar: crowdsecBouncerFull,
			LogVolume:  "./traefik/logs/access.log:" + traefikAccessLog + ":ro",
		}
	}
	return v
}

func tmEnv(a *answers.Answers, static, proxy, pill, single bool) []string {
	env := []string{"COOKIE_SECURE=" + boolString(a.TLS.Method != answers.TLSNone)}
	if !single {
		env = append(env, "CONFIG_DIR=/app/config/dynamic")
	}
	if static {
		env = append(env,
			"STATIC_CONFIG_PATH=/app/traefik.yml",
			"RESTART_METHOD="+a.Restart.Method,
			"TRAEFIK_CONTAINER="+a.Restart.Container,
		)
		if proxy {
			env = append(env, "DOCKER_HOST="+socketProxyHost)
		} else if pill {
			env = append(env, "SIGNAL_FILE_PATH="+composeSignalFile)
		}
	}
	return env
}

func tmLabels(a *answers.Answers) []string {
	labels := append(routerLabels("traefik-manager", a.Hosts.Manager, a.EntryPoint()), "traefik.http.services.traefik-manager.loadbalancer.server.port=5000")
	return append(labels, tlsLabel("traefik-manager", a.TLS.Method != answers.TLSNone)...)
}
