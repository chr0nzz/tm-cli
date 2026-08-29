package installer

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/render"
	"github.com/chr0nzz/tm-cli/internal/state"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

func (in *Installer) prepare(st *state.State) error {
	if st.Mode.IsDocker() {
		if st.ComposeCmd != "" && len(in.Compose) == 0 {
			in.Compose = strings.Fields(st.ComposeCmd)
		}
		return in.ensureCompose()
	}
	return nil
}

type ServiceStatus struct {
	Name   string
	Status string
	Image  string
}

type Health struct {
	URL     string
	OK      bool
	Version string
	Err     string
}

func (in *Installer) Status(ctx context.Context, st *state.State) error {
	if err := in.prepare(st); err != nil {
		return err
	}
	a := &st.Answers
	u := in.UI
	u.Heading("Install")
	u.KV("Mode", string(st.Mode))
	if st.Mode.IsDocker() {
		u.KV("Directory", st.Dir)
	} else if st.Mode == answers.ModeTMNative || st.Mode == answers.ModeFullNative {
		u.KV("Directory", a.Native.InstallDir)
		u.KV("Data dir", a.Native.DataDir)
	} else {
		u.KV("Binary", answers.AgentBinaryPath)
	}
	if st.TraefikVersion != "" {
		u.KV("Traefik", st.TraefikVersion)
	}
	if st.Adopted {
		u.KV("Installed by", "adopted from an existing install")
	} else {
		u.KV("Installed by", "tm "+st.TMVersion)
	}
	if !st.InstalledAt.IsZero() {
		u.KV("Installed at", st.InstalledAt.Local().Format("2006-01-02 15:04"))
	}
	u.KV("Channel", channelLabel(&st.Answers))
	u.KV("State file", ui.MutedStyle.Render(st.Path))
	u.Heading("Services")
	running := true
	for _, s := range in.Services(ctx, st) {
		style := ui.OKStyle
		if s.Status != "running" && s.Status != "active" {
			style = ui.ErrStyle
			running = false
		}
		line := fmt.Sprintf("%-24s%s", s.Name, style.Render(s.Status))
		if s.Image != "" {
			line += "  " + ui.MutedStyle.Render(s.Image)
		}
		u.Line("%s", line)
	}
	u.Heading("Access")
	for _, url := range in.URLs(st) {
		u.KVAccent(url[0], url[1])
	}
	h := in.CheckHealth(ctx, st)
	if h.OK {
		v := "ok"
		if h.Version != "" {
			v += "  " + ui.MutedStyle.Render("version "+h.Version)
		}
		u.KV("Health", ui.OKStyle.Render(v))
	} else {
		u.KV("Health", ui.ErrStyle.Render("unreachable")+"  "+ui.MutedStyle.Render(h.Err))
	}
	if modified, _ := st.Modified(); len(modified) > 0 {
		u.Blank()
		u.Warn("files changed since tm wrote them: " + strings.Join(modified, ", "))
	}
	u.Blank()
	if !running {
		u.Info("some services are not running, try: tm logs")
	}
	return nil
}

func (in *Installer) Services(ctx context.Context, st *state.State) []ServiceStatus {
	a := &st.Answers
	var out []ServiceStatus
	switch st.Mode {
	case answers.ModeFullNative:
		out = append(out, ServiceStatus{Name: traefikUnit + ".service", Status: unitStatus(ctx, traefikUnit)})
		out = append(out, ServiceStatus{Name: nativeUnit + ".service", Status: unitStatus(ctx, nativeUnit)})
		if a.Mounts.StaticConfig {
			out = append(out, ServiceStatus{Name: restartPathUnit, Status: unitStatus(ctx, restartPathUnit)})
		}
	case answers.ModeTMNative:
		out = append(out, ServiceStatus{Name: nativeUnit + ".service", Status: unitStatus(ctx, nativeUnit)})
		if a.Mounts.StaticConfig && a.Restart.TraefikSystemd {
			out = append(out, ServiceStatus{Name: restartPathUnit, Status: unitStatus(ctx, restartPathUnit)})
		}
	case answers.ModeAgentBinary:
		out = append(out, ServiceStatus{Name: agentUnit + ".service", Status: unitStatus(ctx, agentUnit)})
	default:
		for _, name := range serviceNames(a) {
			status, image := containerStatus(ctx, name)
			out = append(out, ServiceStatus{Name: name, Status: status, Image: image})
		}
	}
	if st.Mode.IsSystemd() && a.CrowdSec.Mode == answers.CrowdSecInstall {
		out = append(out, ServiceStatus{Name: crowdsecUnit + ".service", Status: unitStatus(ctx, crowdsecUnit)})
	}
	return out
}

func unitStatus(ctx context.Context, unit string) string {
	if host.ServiceActive(ctx, unit) {
		return "active"
	}
	out, _ := host.SystemctlOutput(ctx, "is-active", unit)
	out = strings.TrimSpace(out)
	if out == "" {
		return "inactive"
	}
	return out
}

func (in *Installer) URLs(st *state.State) [][2]string {
	a := &st.Answers
	ip := host.PrimaryIP()
	switch st.Mode {
	case answers.ModeFull:
		return [][2]string{
			{"Traefik dashboard", a.Scheme() + "://" + a.Hosts.Dashboard},
			{"Traefik Manager", a.Scheme() + "://" + a.Hosts.Manager},
		}
	case answers.ModeFullNative:
		var urls [][2]string
		if a.Hosts.Dashboard != "" {
			urls = append(urls, [2]string{"Traefik dashboard", a.Scheme() + "://" + a.Hosts.Dashboard})
		}
		return append(urls, [2]string{"Traefik Manager", "http://" + ip + ":" + a.Native.Port})
	case answers.ModeTMDocker:
		if a.Access.ViaTraefik {
			return [][2]string{{"Traefik Manager", a.Scheme() + "://" + a.Hosts.Manager}}
		}
		return [][2]string{{"Traefik Manager", "http://" + ip + ":" + a.Access.Port}}
	case answers.ModeTMNative:
		return [][2]string{{"Traefik Manager", "http://" + ip + ":" + a.Native.Port}}
	case answers.ModeAgentDockerTraefik:
		urls := [][2]string{{"Agent", "http://" + ip + ":" + a.Agent.Port}}
		if a.Hosts.Agent != "" {
			urls = append(urls, [2]string{"Agent (label)", a.Scheme() + "://" + a.Hosts.Agent})
		}
		if a.Hosts.Dashboard != "" {
			urls = append(urls, [2]string{"Dashboard", a.Scheme() + "://" + a.Hosts.Dashboard})
		}
		return urls
	default:
		return [][2]string{{"Agent", "http://" + ip + ":" + a.Agent.Port}}
	}
}

func (in *Installer) CheckHealth(ctx context.Context, st *state.State) Health {
	a := &st.Answers
	var url, hostHeader string
	path := "/api/health"
	switch st.Mode {
	case answers.ModeFull:
		url = a.Scheme() + "://127.0.0.1"
		hostHeader = a.Hosts.Manager
	case answers.ModeTMDocker:
		if a.Access.ViaTraefik {
			url = a.Scheme() + "://127.0.0.1"
			hostHeader = a.Hosts.Manager
		} else {
			url = "http://127.0.0.1:" + a.Access.Port
		}
	case answers.ModeTMNative, answers.ModeFullNative:
		url = "http://127.0.0.1:" + a.Native.Port
	default:
		url = "http://127.0.0.1:" + a.Agent.Port
		path = "/health"
	}
	return probe(ctx, url+path, hostHeader)
}

func probe(ctx context.Context, url, hostHeader string) Health {
	h := Health{URL: url}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: hostHeader},
			DialContext:     (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		h.Err = err.Error()
		return h
	}
	if hostHeader != "" {
		req.Host = hostHeader
	}
	resp, err := client.Do(req)
	if err != nil {
		h.Err = err.Error()
		return h
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		h.Err = fmt.Sprintf("http %d", resp.StatusCode)
		return h
	}
	var body struct {
		OK      *bool  `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		h.Version = body.Version
		if body.OK != nil && !*body.OK {
			h.Err = "endpoint returned ok=false"
			return h
		}
	}
	h.OK = true
	return h
}

func (in *Installer) waitHealthy(ctx context.Context, st *state.State, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	announced := false
	for {
		if in.CheckHealth(ctx, st).OK {
			return
		}
		if time.Now().After(deadline) || !sleep(ctx, time.Second) {
			return
		}
		if !announced {
			announced = true
			in.UI.Info("waiting for it to answer")
		}
	}
}

func (in *Installer) SwitchChannel(ctx context.Context, st *state.State, channel string) error {
	if st.Answers.Channel == channel {
		return nil
	}
	a := st.Answers.Clone()
	a.Channel = channel
	a.Finalize()
	if err := a.Validate(); err != nil {
		return err
	}
	st.Answers.Channel = channel
	if st.Mode.IsDocker() {
		out, err := render.Render(render.Input{Answers: a, User: host.CurrentUser()})
		if err != nil {
			return err
		}
		for _, f := range out.Files {
			if f.Path != "docker-compose.yml" {
				continue
			}
			if err := host.WriteFile(filepath.Join(st.Dir, f.Path), []byte(f.Content), f.Mode); err != nil {
				return err
			}
			if st.OwnedFiles == nil {
				st.OwnedFiles = map[string]string{}
			}
			st.OwnedFiles[f.Path] = state.Hash([]byte(f.Content))
		}
	}
	in.UI.OK("switched to the " + channel + " channel")
	return nil
}

func (in *Installer) Update(ctx context.Context, st *state.State) error {
	if err := in.prepare(st); err != nil {
		return err
	}
	switch st.Mode {
	case answers.ModeFullNative:
		if err := in.updateFullNative(ctx, st); err != nil {
			return err
		}
	case answers.ModeTMNative:
		if err := in.updateNative(ctx, &st.Answers); err != nil {
			return err
		}
	case answers.ModeAgentBinary:
		if err := in.updateAgentBinary(ctx); err != nil {
			return err
		}
	default:
		in.UI.Step("Pulling images")
		if err := in.compose(ctx, st.Dir, "pull"); err != nil {
			return err
		}
		in.UI.Step("Recreating services")
		if err := in.compose(ctx, st.Dir, "up", "-d"); err != nil {
			return err
		}
		in.UI.OK("Services updated")
	}
	in.waitHealthy(ctx, st, 30*time.Second)
	return nil
}

func (in *Installer) Logs(ctx context.Context, st *state.State, service string, follow bool, lines int) error {
	if err := in.prepare(st); err != nil {
		return err
	}
	switch st.Mode {
	case answers.ModeTMNative, answers.ModeAgentBinary, answers.ModeFullNative:
		unit := nativeUnit
		if st.Mode == answers.ModeAgentBinary {
			unit = agentUnit
		}
		if service != "" {
			unit = service
		}
		args := []string{"-u", unit, "--no-pager", "-n", fmt.Sprint(lines)}
		if follow {
			args = append(args, "-f")
		}
		return host.Run(host.Privileged(ctx, "journalctl", args...))
	default:
		args := []string{"logs", "--tail", fmt.Sprint(lines)}
		if follow {
			args = append(args, "-f")
		}
		if service != "" {
			args = append(args, service)
		}
		return in.compose(ctx, st.Dir, args...)
	}
}

func (in *Installer) Control(ctx context.Context, st *state.State, action, service string) error {
	if err := in.prepare(st); err != nil {
		return err
	}
	switch st.Mode {
	case answers.ModeTMNative, answers.ModeAgentBinary, answers.ModeFullNative:
		units := []string{nativeUnit}
		if st.Mode == answers.ModeAgentBinary {
			units = []string{agentUnit}
		}
		if st.Mode == answers.ModeFullNative {
			units = []string{traefikUnit, nativeUnit}
		}
		if service != "" {
			units = []string{service}
		}
		for _, unit := range units {
			if err := host.Systemctl(ctx, action, unit); err != nil {
				return err
			}
			in.UI.OK(unit + " " + pastTense(action))
		}
		return nil
	default:
		args := []string{action}
		if service != "" {
			args = append(args, service)
		}
		if action == "start" {
			args = []string{"up", "-d"}
			if service != "" {
				args = append(args, service)
			}
		}
		if err := in.compose(ctx, st.Dir, args...); err != nil {
			return err
		}
		target := "services"
		if service != "" {
			target = service
		}
		in.UI.OK(target + " " + pastTense(action))
		return nil
	}
}

func pastTense(action string) string {
	switch action {
	case "stop":
		return "stopped"
	case "start":
		return "started"
	default:
		return "restarted"
	}
}

func (in *Installer) Password(ctx context.Context, st *state.State) (string, error) {
	if err := in.prepare(st); err != nil {
		return "", err
	}
	var logs string
	switch st.Mode {
	case answers.ModeTMNative, answers.ModeFullNative:
		out, err := host.Journalctl(ctx, nativeUnit, 2000)
		if err != nil {
			return "", err
		}
		logs = out
	case answers.ModeFull, answers.ModeTMDocker:
		out, err := host.Output(host.DockerCommand(ctx, "logs", "traefik-manager"))
		if err != nil {
			return "", err
		}
		logs = out
	default:
		return "", fmt.Errorf("agents have no password, the API key lives in %s", secretsLocation(st))
	}
	m := passwordRe.FindAllStringSubmatch(logs, -1)
	if len(m) == 0 {
		return "", fmt.Errorf("no auto-generated password in the logs (it is only printed on first start, before a password is set)")
	}
	return m[len(m)-1][1], nil
}

func secretsLocation(st *state.State) string {
	if st.Mode == answers.ModeAgentBinary {
		return "/etc/traefik-manager-agent/env"
	}
	return filepath.Join(st.Dir, ".env")
}

type UninstallOptions struct {
	Purge bool
}

func (in *Installer) Uninstall(ctx context.Context, st *state.State, opts UninstallOptions) error {
	if err := in.prepare(st); err != nil {
		return err
	}
	a := &st.Answers
	switch st.Mode {
	case answers.ModeFullNative:
		in.UI.Step("Stopping services")
		_ = host.Systemctl(ctx, "disable", "--now", nativeUnit)
		if host.Exists("/etc/systemd/system/" + restartPathUnit) {
			_ = host.Systemctl(ctx, "disable", "--now", restartPathUnit)
			_ = host.Remove("/etc/systemd/system/"+restartPathUnit, false)
			_ = host.Remove("/etc/systemd/system/traefik-restart.service", false)
		}
		_ = host.Systemctl(ctx, "disable", "--now", traefikUnit)
		if err := host.Remove(render.NativeUnitPath, false); err != nil {
			return err
		}
		if err := host.Remove(render.TraefikUnitPath, false); err != nil {
			return err
		}
		_ = host.Systemctl(ctx, "daemon-reload")
		in.UI.OK("systemd units removed")
		_ = host.Remove(answers.TraefikBinaryPath, false)
		_ = host.Remove(answers.TraefikBinaryPath+".prev", false)
		in.UI.OK(answers.TraefikBinaryPath + " removed")
		_ = host.Remove(render.LogrotatePath, false)
		if err := host.Remove(a.Native.InstallDir, true); err != nil {
			return err
		}
		in.UI.OK(a.Native.InstallDir + " removed")
		if opts.Purge {
			for _, p := range []string{a.Native.DataDir, answers.NativeTraefikConfigDir, answers.NativeTraefikStateDir, answers.NativeTraefikLogDir} {
				if err := host.Remove(p, true); err != nil {
					return err
				}
				in.UI.OK(p + " removed")
			}
			if a.Native.ServiceUser && host.UserExists(nativeUser) {
				if err := host.Run(host.Privileged(ctx, "userdel", nativeUser)); err != nil {
					in.UI.Warn("could not remove user " + nativeUser + ": " + err.Error())
				} else {
					in.UI.OK("user " + nativeUser + " removed")
				}
			}
		} else {
			in.UI.Info("kept " + answers.NativeTraefikConfigDir + " (your routing config), " + answers.NativeTraefikStateDir + " (certificates) and " + answers.NativeTraefikLogDir + " (logs)")
			in.UI.Info("data kept in " + a.Native.DataDir + " (use --purge to remove all of it)")
		}
	case answers.ModeTMNative:
		in.UI.Step("Stopping services")
		_ = host.Systemctl(ctx, "disable", "--now", nativeUnit)
		if host.Exists("/etc/systemd/system/" + restartPathUnit) {
			_ = host.Systemctl(ctx, "disable", "--now", restartPathUnit)
			_ = host.Remove("/etc/systemd/system/"+restartPathUnit, false)
			_ = host.Remove("/etc/systemd/system/traefik-restart.service", false)
		}
		if err := host.Remove("/etc/systemd/system/"+nativeUnit+".service", false); err != nil {
			return err
		}
		_ = host.Systemctl(ctx, "daemon-reload")
		in.UI.OK("systemd units removed")
		if err := host.Remove(a.Native.InstallDir, true); err != nil {
			return err
		}
		in.UI.OK(a.Native.InstallDir + " removed")
		if opts.Purge {
			if err := host.Remove(a.Native.DataDir, true); err != nil {
				return err
			}
			in.UI.OK(a.Native.DataDir + " removed")
			if a.Native.ServiceUser && host.UserExists(nativeUser) {
				if err := host.Run(host.Privileged(ctx, "userdel", nativeUser)); err != nil {
					in.UI.Warn("could not remove user " + nativeUser + ": " + err.Error())
				} else {
					in.UI.OK("user " + nativeUser + " removed")
				}
			}
		} else {
			in.UI.Info("data kept in " + a.Native.DataDir + " (use --purge to remove it)")
		}
	case answers.ModeAgentBinary:
		in.UI.Step("Stopping service")
		_ = host.Systemctl(ctx, "disable", "--now", agentUnit)
		if err := host.Remove("/etc/systemd/system/"+agentUnit+".service", false); err != nil {
			return err
		}
		_ = host.Systemctl(ctx, "daemon-reload")
		_ = host.Remove(answers.AgentBinaryPath, false)
		_ = host.Remove("/etc/traefik-manager-agent", true)
		in.UI.OK("agent service, binary, and env removed")
	default:
		in.UI.Step("Stopping services")
		args := []string{"down", "--remove-orphans"}
		if opts.Purge {
			args = append(args, "-v")
		}
		if err := in.compose(ctx, st.Dir, args...); err != nil {
			return err
		}
		in.UI.OK("containers removed")
		if opts.Purge {
			if err := host.Remove(st.Dir, true); err != nil {
				return err
			}
			in.UI.OK(st.Dir + " removed")
		} else {
			in.removeOwned(st)
			in.UI.Info("configs, backups, certificates and logs kept in " + st.Dir + " (use --purge to remove everything)")
		}
	}
	if st.Mode.IsSystemd() && a.CrowdSec.Mode == answers.CrowdSecInstall {
		_ = host.Remove(render.CrowdSecAcquisPath, false)
		in.UI.Info("the crowdsec package is left installed, its Traefik acquisition file is removed")
	}
	_ = host.Remove(st.Path, false)
	if st.Mode.IsDocker() || st.Mode == answers.ModeTMNative || st.Mode == answers.ModeFullNative {
		_ = host.Remove(filepath.Dir(st.Path), true)
	}
	_ = state.Unregister(st.Path)
	return nil
}

func (in *Installer) removeOwned(st *state.State) {
	if st.Adopted {
		in.UI.Info("this install was adopted, so its files were not written by tm and are left in place")
		return
	}
	var kept []string
	for p, want := range st.OwnedFiles {
		if p == traefikStaticRel {
			continue
		}
		full := filepath.Join(st.Dir, p)
		data, err := host.ReadFile(full)
		if err != nil {
			continue
		}
		if state.Hash(data) != want {
			kept = append(kept, p)
			continue
		}
		_ = host.Remove(full, false)
	}
	in.UI.OK("files tm wrote removed")
	if len(kept) > 0 {
		sort.Strings(kept)
		in.UI.Info("kept because they changed since tm wrote them: " + strings.Join(kept, ", "))
	}
}

func (in *Installer) composeFile(st *state.State) string {
	return filepath.Join(st.Dir, "docker-compose.yml")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func channelLabel(a *answers.Answers) string {
	if a.Channel == answers.ChannelBeta {
		return ui.WarnStyle.Render("beta") + "  " + ui.MutedStyle.Render("tracks the next release")
	}
	return "stable"
}
