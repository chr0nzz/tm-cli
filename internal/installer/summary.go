package installer

import (
	"fmt"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

func (in *Installer) Summary(a *answers.Answers) {
	switch a.Mode {
	case answers.ModeFull:
		in.summaryFull(a)
	case answers.ModeTMDocker:
		in.summaryTMDocker(a)
	case answers.ModeTMNative:
		in.summaryNative(a)
	default:
		in.summaryAgent(a)
	}
}

func (in *Installer) composeString() string {
	if len(in.Compose) == 0 {
		return "docker compose"
	}
	s := in.Compose[0]
	for _, p := range in.Compose[1:] {
		s += " " + p
	}
	return s
}

func (in *Installer) printPassword(logsHint string) {
	u := in.UI
	if in.TempPassword != "" {
		u.Line("%s", ui.WarnStyle.Bold(true).Render(fmt.Sprintf("%-20s%s", "Temporary password", in.TempPassword)))
	} else {
		u.Line("%s", ui.WarnStyle.Render(fmt.Sprintf("%-20srun: %s", "Temporary password", logsHint)))
	}
}

func (in *Installer) staticConfigSummary(a *answers.Answers) {
	if !a.Mounts.StaticConfig {
		return
	}
	u := in.UI
	u.Heading("Static Config Editor")
	switch a.Restart.Method {
	case answers.RestartProxy:
		u.KVMuted("Restart method", "socket proxy (tecnativa/docker-socket-proxy)")
		u.Line("%s", ui.MutedStyle.Render("The socket-proxy service is running alongside TM with minimal permissions."))
	case answers.RestartPoisonPill:
		u.KVMuted("Restart method", "poison pill (signal file)")
		if a.Restart.TraefikSystemd {
			u.Line("%s", ui.MutedStyle.Render("Traefik running as systemd service: "+a.Restart.TraefikService))
			u.Line("%s", ui.MutedStyle.Render("traefik-restart.path watcher is active - restarts "+a.Restart.TraefikService+" when TM writes the signal file."))
		} else if a.Mode != answers.ModeFull {
			u.Warn(ui.MutedStyle.Render("Add this healthcheck to your Traefik service if not already set:"))
			u.Blank()
			u.Code("healthcheck:")
			u.Code(`  test: ["CMD-SHELL", "[ ! -f /signals/restart.sig ] || (rm /signals/restart.sig && kill -TERM 1)"]`)
			u.Code("  interval: 5s")
			u.Code("  timeout: 3s")
			u.Code("  retries: 1")
			u.Blank()
		}
	case answers.RestartSocket:
		u.KVMuted("Restart method", "direct Docker socket")
		u.Warn("Full Docker socket is mounted in TM. Keep TM behind authentication.")
	}
}

func (in *Installer) summaryFull(a *answers.Answers) {
	u := in.UI
	scheme := a.Scheme()
	u.Done("Setup complete!")
	u.KVAccent("Traefik dashboard", scheme+"://"+a.Hosts.Dashboard)
	u.KVAccent("Traefik Manager", scheme+"://"+a.Hosts.Manager)
	u.Blank()
	in.printPassword("docker logs traefik-manager")
	u.KV("Install dir", ui.MutedStyle.Render(a.Dir))
	u.Blank()
	if a.Config.Layout == answers.LayoutSingle {
		u.KVMuted("Dynamic config", a.Dir+"/traefik/config/dynamic.yml")
	} else {
		u.KVMuted("Dynamic config", a.Dir+"/traefik/config/*.yml")
	}
	in.staticConfigSummary(a)
	if a.CrowdSec.Mode != answers.CrowdSecNone {
		u.Heading("CrowdSec")
		if a.CrowdSec.Mode == answers.CrowdSecInstall {
			u.KVMuted("Mode", "installed as part of this stack")
			u.KVMuted("LAPI URL", answers.DefaultLAPIURL)
			u.KVMuted("Bouncer key", a.Secrets[answers.SecretCrowdSecAPIKey])
			u.KVMuted("Machine ID", a.CrowdSec.MachineID)
			u.KVMuted("Machine pass", a.Secrets[answers.SecretCrowdSecMachinePassword])
		} else {
			u.KVMuted("Mode", "connected to existing instance")
			u.KVMuted("LAPI URL", a.CrowdSec.LAPIURL)
			if a.CrowdSec.MachineID != "" {
				u.KVMuted("Machine ID", a.CrowdSec.MachineID)
			} else {
				u.Info("Alerts need machine credentials - add CROWDSEC_MACHINE_ID / CROWDSEC_MACHINE_PASSWORD later in Settings or env.")
			}
		}
		u.Info("Enable the CrowdSec tab in Traefik Manager under Settings to view decisions and alerts.")
	}
	u.Blank()
	u.Code("tm status")
	u.Code("tm logs traefik-manager")
	u.Blank()
	u.Heading("Updating")
	u.Code("tm update")
	u.Blank()
	if a.Deployment == answers.DeploymentExternal {
		u.Warn(fmt.Sprintf("DNS A records for %s and %s must point to this server's IP.", a.Hosts.Dashboard, a.Hosts.Manager))
	}
	if a.TLS.Method == answers.TLSNone {
		u.Warn("TLS is disabled. Consider enabling it before exposing this publicly.")
	}
	u.Blank()
}

func (in *Installer) summaryTMDocker(a *answers.Answers) {
	u := in.UI
	url := "http://" + host.PrimaryIP() + ":" + a.Access.Port
	if a.Access.ViaTraefik {
		url = a.Scheme() + "://" + a.Hosts.Manager
	}
	u.Done("Setup complete!")
	u.KVAccent("Traefik Manager", url)
	u.Blank()
	in.printPassword("docker logs traefik-manager")
	u.KV("Install dir", ui.MutedStyle.Render(a.Dir))
	in.staticConfigSummary(a)
	u.Blank()
	u.Code("tm status")
	u.Code("tm logs")
	u.Blank()
	u.Heading("Updating")
	u.Code("tm update")
	u.Blank()
}

func (in *Installer) summaryNative(a *answers.Answers) {
	u := in.UI
	u.Done("Setup complete!")
	u.KVAccent("Traefik Manager", "http://"+host.PrimaryIP()+":"+a.Native.Port)
	u.Blank()
	in.printPassword("sudo journalctl -u traefik-manager")
	u.KV("Install dir", ui.MutedStyle.Render(a.Native.InstallDir))
	u.KV("Data dir", ui.MutedStyle.Render(a.Native.DataDir))
	in.staticConfigSummary(a)
	u.Blank()
	u.Code("tm status")
	u.Code("tm logs")
	u.Blank()
	u.Heading("Updating")
	u.Code("tm update")
	u.Blank()
}

func (in *Installer) summaryAgent(a *answers.Answers) {
	u := in.UI
	ip := host.PrimaryIP()
	u.Done("Agent setup complete!")
	u.KVAccent("Agent URL", "http://"+ip+":"+a.Agent.Port)
	u.KVMuted("Health check", "curl http://"+ip+":"+a.Agent.Port+"/health")
	if a.Mode == answers.ModeAgentDockerTraefik {
		u.Blank()
		ports := "80"
		if a.TLS.Method != answers.TLSNone {
			ports = "80 + 443"
		}
		u.KVMuted("Traefik", "running on ports "+ports)
		if a.Hosts.Agent != "" {
			u.KVAccent("Agent (label)", a.Scheme()+"://"+a.Hosts.Agent)
		}
		if a.Hosts.Dashboard != "" {
			u.KVAccent("Dashboard", a.Scheme()+"://"+a.Hosts.Dashboard)
		}
		if a.Config.Layout == answers.LayoutSingle {
			u.KVMuted("Dynamic config", a.Dir+"/traefik/config/dynamic.yml")
		} else {
			u.KVMuted("Dynamic config", a.Dir+"/traefik/config/*.yml")
		}
		if a.TLS.Method == answers.TLSNone {
			u.Warn("TLS is disabled. Consider enabling it before exposing this publicly.")
		}
		if a.Deployment == answers.DeploymentExternal {
			if a.Hosts.Agent != "" {
				u.Warn("DNS A record for " + a.Hosts.Agent + " must point to this server's IP.")
			}
			if a.Hosts.Dashboard != "" {
				u.Warn("DNS A record for " + a.Hosts.Dashboard + " must point to this server's IP.")
			}
		}
	}
	u.Blank()
	u.Title("Next steps:")
	u.Line("%s", ui.MutedStyle.Render("1. In TM Settings -> Agents, click Add Agent"))
	u.Line("%s", ui.MutedStyle.Render("2. Enter the Agent URL above and the API key you configured"))
	u.Line("%s", ui.MutedStyle.Render("3. Use the server switcher in the TM nav bar to switch to this agent"))
	u.Blank()
	u.Heading("Updating")
	u.Code("tm update")
	u.Blank()
}
