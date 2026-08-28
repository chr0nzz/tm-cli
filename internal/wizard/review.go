package wizard

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

func ReviewLines(a *answers.Answers) []string {
	if a == nil {
		return nil
	}
	secs := sectionsFor(a.Mode)
	lines := make([]string, 0, len(secs))
	for i, s := range secs {
		lines = append(lines, fmt.Sprintf("  %2d  %-20s  %s", i+1, s.Label, reviewValue(a, s.ID)))
	}
	return lines
}

func reviewValue(a *answers.Answers, id string) string {
	switch a.Mode {
	case answers.ModeFull:
		return fullValue(a, id)
	case answers.ModeTMDocker:
		return tmdValue(a, id)
	case answers.ModeTMNative:
		return tmnValue(a, id)
	}
	return agentValue(a, id)
}

func fullValue(a *answers.Answers, id string) string {
	switch id {
	case "general":
		return a.Dir
	case "deployment":
		if a.Deployment == answers.DeploymentExternal {
			return "external (internet-facing)"
		}
		return "internal (LAN / VPN)"
	case "domain":
		v := fmt.Sprintf("%s  dash:%s  tm:%s", a.Domain, a.Hosts.Dashboard, a.Hosts.Manager)
		if !a.Dashboard {
			v += "  dashboard:off"
		}
		return v
	case "tls":
		return tlsValue(a)
	case "config":
		return layoutValue(a)
	case "mounts":
		return mountsValue(a, "logs", "certs")
	case "crowdsec":
		return crowdsecValue(a)
	case "network":
		return fmt.Sprintf("%s  api:%s", a.Network.Name, a.Network.TraefikAPIPort)
	}
	return ""
}

func tmdValue(a *answers.Answers, id string) string {
	switch id {
	case "general":
		return a.Dir
	case "network":
		if a.Network.External {
			return a.Network.Name + " (existing)"
		}
		return a.Network.Name + " (new)"
	case "access":
		if !a.Access.ViaTraefik {
			return "host port :" + a.Access.Port
		}
		v := "via Traefik  " + a.Hosts.Manager + "  "
		switch a.TLS.Method {
		case answers.TLSNone:
			return v + "no TLS"
		case answers.TLSDNS:
			return v + "TLS:dns(" + legoProvider(a.TLS) + ")"
		}
		return v + "TLS:http"
	case "config":
		return layoutValue(a)
	case "mounts":
		return mountsValue(a, "logs", "certs")
	}
	return ""
}

func tmnValue(a *answers.Answers, id string) string {
	switch id {
	case "general":
		return fmt.Sprintf("%s  data:%s  :%s", a.Native.InstallDir, a.Native.DataDir, a.Native.Port)
	case "user":
		if a.Native.ServiceUser {
			return "dedicated (traefik-manager)"
		}
		return "current user"
	case "config":
		if a.Config.Layout == answers.LayoutDirectory {
			return "Directory  " + a.Config.Dir
		}
		return "Single file  " + a.Config.Path
	case "mounts":
		return mountsValue(a, "certs", "logs")
	}
	return ""
}

func agentValue(a *answers.Answers, id string) string {
	switch id {
	case "apikey":
		return ui.Mask(a.Secrets[answers.SecretTMAAPIKey])
	case "traefik":
		if a.Mode == answers.ModeAgentDockerTraefik {
			return traefikInstallValue(a)
		}
		v := a.Agent.TraefikURL
		if a.Agent.InsecureTLS {
			v += "  insecure-tls"
		}
		if a.Mounts.StaticConfig && a.Mounts.StaticConfigPath != "" {
			v += "  static:" + filepath.Base(a.Mounts.StaticConfigPath)
		}
		if a.Mode == answers.ModeAgentBinary {
			v += "  :" + a.Agent.Port
		}
		return v
	case "paths":
		var p []string
		if a.Mounts.Certs {
			p = append(p, "acme")
		}
		if a.Mounts.AccessLogs {
			p = append(p, "logs")
		}
		if a.Mounts.Plugins {
			p = append(p, "plugins")
		}
		if len(p) == 0 {
			return "(none)"
		}
		return strings.Join(p, " ")
	case "restart":
		if a.Restart.Method == "" || a.Restart.Method == answers.RestartNone {
			return "(none)"
		}
		return a.Restart.Method
	case "crowdsec":
		return crowdsecValue(a)
	case "git":
		if !a.Agent.Git.Enabled {
			return "disabled"
		}
		return orDefault(a.Agent.Git.Repo, "(no repo set)")
	case "location":
		return fmt.Sprintf("%s  :%s", a.Dir, a.Agent.Port)
	}
	return ""
}

func traefikInstallValue(a *answers.Answers) string {
	v := orDefault(a.TLS.Method, answers.TLSNone)
	if a.Deployment == answers.DeploymentExternal {
		v += "  external"
	} else {
		v += "  internal"
	}
	if a.Hosts.Dashboard != "" {
		v += "  dash:" + a.Hosts.Dashboard
	}
	if a.Hosts.Agent != "" {
		v += "  tma:" + a.Hosts.Agent
	}
	v += "  net:" + a.Network.Name
	if a.Config.Layout == answers.LayoutDirectory {
		v += "  Directory"
	} else if a.Config.Layout != "" {
		v += "  Single"
	}
	return v
}

func tlsValue(a *answers.Answers) string {
	switch a.TLS.Method {
	case answers.TLSNone:
		return "none (HTTP only)"
	case answers.TLSDNS:
		return fmt.Sprintf("Let's Encrypt DNS (%s)  %s", legoProvider(a.TLS), a.TLS.Email)
	}
	return "Let's Encrypt HTTP  " + a.TLS.Email
}

func layoutValue(a *answers.Answers) string {
	if a.Config.Layout == answers.LayoutDirectory {
		return "Directory"
	}
	return "Single file"
}

func mountsValue(a *answers.Answers, order ...string) string {
	var m []string
	for _, k := range order {
		if k == "logs" && a.Mounts.AccessLogs {
			m = append(m, "logs")
		}
		if k == "certs" && a.Mounts.Certs {
			m = append(m, "certs")
		}
	}
	if a.Mounts.StaticConfig {
		m = append(m, "static(restart:"+a.Restart.Method+")")
	}
	if len(m) == 0 {
		return "(none)"
	}
	return strings.Join(m, " ")
}

func crowdsecValue(a *answers.Answers) string {
	switch a.CrowdSec.Mode {
	case answers.CrowdSecInstall:
		return "install alongside"
	case answers.CrowdSecConnect:
		return "connect  " + a.CrowdSec.LAPIURL
	}
	return "disabled"
}
