package installer

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/host"
	"github.com/chr0nzz/traefik-stack/internal/state"
	"github.com/chr0nzz/traefik-stack/internal/ui"
)

type Check struct {
	Name   string
	Run    func(ctx context.Context) (ok bool, detail string)
	Advice string
	Soft   bool
}

func (in *Installer) Doctor(ctx context.Context, st *state.State) (failed int, err error) {
	if err := in.prepare(st); err != nil {
		return 0, err
	}
	checks := in.checks(st)
	for _, c := range checks {
		ok, detail := c.Run(ctx)
		label := fmt.Sprintf("%-42s", c.Name)
		switch {
		case ok:
			in.UI.OK(label + ui.MutedStyle.Render(detail))
		case c.Soft:
			in.UI.Warn(label + ui.WarnStyle.Render(detail))
			if c.Advice != "" {
				in.UI.Code(c.Advice)
			}
		default:
			failed++
			in.UI.Line("%s", "  "+ui.ErrStyle.Render("✖")+"  "+label+ui.ErrStyle.Render(detail))
			if c.Advice != "" {
				in.UI.Code(c.Advice)
			}
		}
	}
	return failed, nil
}

func (in *Installer) checks(st *state.State) []Check {
	a := &st.Answers
	var checks []Check
	if st.Mode.IsDocker() {
		checks = append(checks,
			Check{Name: "docker installed and running", Run: func(ctx context.Context) (bool, string) {
				if !host.DockerInstalled() {
					return false, "docker not found"
				}
				if !host.DockerRunning() {
					return false, "docker daemon not reachable"
				}
				return true, ""
			}, Advice: "sudo systemctl enable --now docker"},
			Check{Name: "docker compose available", Run: func(ctx context.Context) (bool, string) {
				cmd, err := host.ComposeCommand()
				if err != nil {
					return false, err.Error()
				}
				return true, strings.Join(cmd, " ")
			}},
			Check{Name: "current user can use docker", Soft: true, Run: func(ctx context.Context) (bool, string) {
				if host.IsRoot() || host.InDockerGroup() {
					return true, ""
				}
				if host.DockerSudo() {
					return true, "not in the docker group, tm is using sudo for docker"
				}
				if host.DockerRunning() {
					return true, "not in the docker group, but the daemon answers"
				}
				return false, host.CurrentUser() + " is not in the docker group"
			}, Advice: "sudo usermod -aG docker " + host.CurrentUser() + ", then log out and back in"},
			Check{Name: "docker-compose.yml is valid", Run: func(ctx context.Context) (bool, string) {
				if _, err := in.composeOutput(ctx, st.Dir, "config", "-q"); err != nil {
					return false, err.Error()
				}
				return true, filepath.Join(st.Dir, "docker-compose.yml")
			}},
		)
	}
	if st.Mode == answers.ModeTMNative {
		checks = append(checks,
			Check{Name: "python >= 3.11", Run: func(ctx context.Context) (bool, string) {
				v, ok := host.PythonVersion()
				return ok, v
			}},
			Check{Name: "git installed", Run: func(ctx context.Context) (bool, string) {
				return host.HasCommand("git"), ""
			}},
			Check{Name: "venv present", Run: func(ctx context.Context) (bool, string) {
				p := filepath.Join(a.Native.InstallDir, "venv", "bin", "gunicorn")
				return host.Exists(p), p
			}, Advice: "tm update"},
		)
	}
	if st.Mode == answers.ModeAgentBinary {
		checks = append(checks, Check{Name: "tma binary present", Run: func(ctx context.Context) (bool, string) {
			fi, err := os.Stat(answers.AgentBinaryPath)
			if err != nil {
				return false, err.Error()
			}
			if fi.Mode()&0o111 == 0 {
				return false, "not executable"
			}
			return true, answers.AgentBinaryPath
		}, Advice: "tm update"})
	}
	if st.Mode.IsSystemd() {
		unit := nativeUnit
		if st.Mode == answers.ModeAgentBinary {
			unit = agentUnit
		}
		checks = append(checks, Check{Name: unit + ".service active", Run: func(ctx context.Context) (bool, string) {
			return host.ServiceActive(ctx, unit), unitStatus(ctx, unit)
		}, Advice: "tm logs"})
	} else {
		for _, name := range serviceNames(a) {
			name := name
			checks = append(checks, Check{Name: "container " + name + " running", Run: func(ctx context.Context) (bool, string) {
				status, image := containerStatus(ctx, name)
				return status == "running", status + " " + image
			}, Advice: "tm logs " + name})
		}
	}
	if st.Mode.HasTraefik() {
		checks = append(checks, Check{Name: "port 80 listening", Run: func(ctx context.Context) (bool, string) {
			return portOpen("80")
		}})
		if a.TLS.Method != answers.TLSNone {
			checks = append(checks, Check{Name: "port 443 listening", Run: func(ctx context.Context) (bool, string) {
				return portOpen("443")
			}})
		}
		checks = append(checks, Check{Name: "traefik api answers", Soft: st.Adopted, Run: func(ctx context.Context) (bool, string) {
			h := probe(ctx, "http://127.0.0.1:"+a.Network.TraefikAPIPort+"/api/version", "")
			if !h.OK {
				return false, h.Err
			}
			return true, h.URL
		}, Advice: "tm logs traefik"})
	}
	if externalFacing(a) {
		for _, h := range []string{a.Hosts.Dashboard, a.Hosts.Manager, a.Hosts.Agent} {
			if h == "" {
				continue
			}
			h := h
			checks = append(checks, Check{Name: "dns " + h + " points here", Soft: true, Run: func(ctx context.Context) (bool, string) {
				return dnsPointsHere(h)
			}, Advice: "create an A record for " + h + " pointing at this server (proxied DNS like Cloudflare hides the IP, ignore if so)"})
		}
	}
	if acme := acmePath(st); acme != "" {
		checks = append(checks, Check{Name: "acme.json exists with mode 600", Run: func(ctx context.Context) (bool, string) {
			fi, err := os.Stat(acme)
			if err != nil {
				return false, err.Error()
			}
			if fi.Mode().Perm() != 0o600 {
				return false, fmt.Sprintf("mode %04o", fi.Mode().Perm())
			}
			return true, acme
		}, Advice: "chmod 600 " + acme})
	}
	if a.Mounts.StaticConfig && a.Restart.Method == answers.RestartPoisonPill {
		switch {
		case st.Mode == answers.ModeTMNative && a.Restart.TraefikSystemd:
			checks = append(checks, Check{Name: "traefik-restart.path active", Run: func(ctx context.Context) (bool, string) {
				return host.ServiceActive(ctx, restartPathUnit), unitStatus(ctx, restartPathUnit)
			}, Advice: "sudo systemctl enable --now traefik-restart.path"})
		case st.Mode == answers.ModeFull || st.Mode == answers.ModeAgentDockerTraefik:
			checks = append(checks, Check{Name: "poison pill healthcheck in compose", Run: func(ctx context.Context) (bool, string) {
				data, err := host.ReadFile(filepath.Join(st.Dir, "docker-compose.yml"))
				if err != nil {
					return false, err.Error()
				}
				s := string(data)
				return strings.Contains(s, "restart.sig") && strings.Contains(s, ":/signals"), ""
			}, Advice: "tm reconfigure --section mounts"})
		default:
			checks = append(checks, Check{Name: "traefik healthcheck watches the signal file", Soft: true, Run: func(ctx context.Context) (bool, string) {
				return false, "cannot verify an external traefik compose"
			}, Advice: `healthcheck test: ["CMD-SHELL", "[ ! -f /signals/restart.sig ] || (rm /signals/restart.sig && kill -TERM 1)"]`})
		}
	}
	if a.CrowdSec.Mode != answers.CrowdSecNone {
		checks = append(checks, Check{Name: "crowdsec lapi reachable", Soft: a.CrowdSec.Mode == answers.CrowdSecConnect, Run: func(ctx context.Context) (bool, string) {
			if a.CrowdSec.Mode == answers.CrowdSecInstall {
				if _, err := host.Output(host.DockerCommand(ctx, "exec", "crowdsec", "cscli", "lapi", "status")); err != nil {
					return false, "cscli lapi status failed"
				}
				return true, "cscli lapi status ok"
			}
			key := in.ExistingSecrets(st)[answers.SecretCrowdSecAPIKey]
			return lapiReachable(ctx, a.CrowdSec.LAPIURL, key)
		}, Advice: "tm logs crowdsec"})
	}
	checks = append(checks, Check{Name: "health endpoint answers", Run: func(ctx context.Context) (bool, string) {
		h := in.CheckHealth(ctx, st)
		if !h.OK {
			return false, h.Err
		}
		return true, h.URL
	}, Advice: "tm logs"})
	return checks
}

func externalFacing(a *answers.Answers) bool {
	if a.Mode.HasTraefik() {
		return a.Deployment == answers.DeploymentExternal
	}
	return a.TLS.Method == answers.TLSHTTP
}

func portOpen(port string) (bool, string) {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
	if err != nil {
		return false, "nothing listening on " + port
	}
	c.Close()
	return true, ""
}

func dnsPointsHere(hostname string) (bool, string) {
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return false, "does not resolve"
	}
	mine := map[string]bool{}
	for _, ip := range host.PublicIPs() {
		mine[ip] = true
	}
	for _, ip := range ips {
		if mine[ip] {
			return true, ip
		}
	}
	return false, "resolves to " + strings.Join(ips, ", ")
}

func acmePath(st *state.State) string {
	a := &st.Answers
	switch st.Mode {
	case answers.ModeFull:
		if a.TLS.Method != answers.TLSNone {
			return filepath.Join(st.Dir, "traefik", "acme.json")
		}
	case answers.ModeAgentDockerTraefik:
		if a.TLS.Method != answers.TLSNone {
			return filepath.Join(st.Dir, "traefik", "acme.json")
		}
	case answers.ModeTMDocker, answers.ModeTMNative, answers.ModeAgentDocker, answers.ModeAgentBinary:
		if a.Mounts.Certs {
			return a.Mounts.AcmePath
		}
	}
	return ""
}

func lapiReachable(ctx context.Context, lapi, key string) (bool, string) {
	if lapi == "" {
		return false, "no lapi url"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(lapi, "/")+"/v1/decisions?limit=1", nil)
	if err != nil {
		return false, err.Error()
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return false, "lapi rejected the bouncer key"
	}
	if resp.StatusCode >= 500 {
		return false, fmt.Sprintf("http %d", resp.StatusCode)
	}
	return true, lapi
}
