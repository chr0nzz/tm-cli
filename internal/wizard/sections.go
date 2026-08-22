package wizard

import (
	"strings"

	"charm.land/huh/v2"

	"github.com/chr0nzz/traefik-stack/internal/answers"
)

const (
	layoutInfo = "Single file is simpler. Directory (one .yml per service) is easier at scale."
	mountsInfo = "Expose extra Traefik data to Traefik Manager for richer visibility."
	agentPaths = "Expose extra Traefik data to the agent for richer visibility."
)

var deploymentOptions = []huh.Option[string]{
	huh.NewOption("External (internet-facing)", answers.DeploymentExternal),
	huh.NewOption("Internal only (LAN / VPN / Tailscale)", answers.DeploymentInternal),
}

var layoutOptions = []huh.Option[string]{
	huh.NewOption("Single file (dynamic.yml)", answers.LayoutSingle),
	huh.NewOption("Directory - one .yml file per service", answers.LayoutDirectory),
}

var dockerRestartOptions = []huh.Option[string]{
	huh.NewOption("Docker socket proxy (recommended - one extra container, minimal socket exposure)", answers.RestartProxy),
	huh.NewOption("Poison pill (no extra container - adds a healthcheck to Traefik compose)", answers.RestartPoisonPill),
	huh.NewOption("Direct Docker socket (simplest - full Docker access, higher risk)", answers.RestartSocket),
}

var nativeRestartOptions = []huh.Option[string]{
	huh.NewOption("Poison pill (recommended - signal file, no Docker socket needed)", answers.RestartPoisonPill),
	huh.NewOption("Direct Docker socket (requires TM user to have Docker group access)", answers.RestartSocket),
}

var agentRestartOptions = []huh.Option[string]{
	huh.NewOption("None", answers.RestartNone),
	huh.NewOption("Socket proxy (recommended - minimal socket exposure)", answers.RestartProxy),
	huh.NewOption("Poison pill (signal file, no Docker socket)", answers.RestartPoisonPill),
	huh.NewOption("Direct Docker socket", answers.RestartSocket),
}

func layoutSelect(value *string) *huh.Select[string] {
	return selectOne("Dynamic config layout", value, layoutOptions...).Description(layoutInfo)
}

func (w *wizard) configLayout() error {
	return w.form(layoutSelect(&w.a.Config.Layout))
}

type mount struct {
	ask   string
	path  string
	on    *bool
	value *string
	def   string
}

func (w *wizard) mounts(info string, items []mount) error {
	fields := make([]huh.Field, 0, len(items))
	for _, m := range items {
		fields = append(fields, confirm(m.ask, m.on))
	}
	if err := w.groups(huh.NewGroup(fields...).Description(info)); err != nil {
		return err
	}
	var paths []huh.Field
	for _, m := range items {
		if !*m.on || m.path == "" {
			continue
		}
		*m.value = orDefault(*m.value, m.def)
		paths = append(paths, pathInput(m.path, m.value))
	}
	if len(paths) == 0 {
		return nil
	}
	return w.form(paths...)
}

func (w *wizard) restartMethodDocker(askContainer bool) error {
	a := w.a
	fields := []huh.Field{
		selectOne("How should TM restart Traefik?", &a.Restart.Method, dockerRestartOptions...).
			Description("TM can restart Traefik automatically when you save static config changes."),
	}
	if askContainer {
		a.Restart.Container = orDefault(a.Restart.Container, "traefik")
		fields = append(fields, containerInput(&a.Restart.Container))
	} else {
		a.Restart.Container = "traefik"
	}
	return w.groups(huh.NewGroup(fields...).Title("Static Config Editor"))
}

func (w *wizard) fullGeneral() error {
	return w.form(pathInput("Install directory", &w.a.Dir))
}

func (w *wizard) fullDeployment() error {
	return w.form(selectOne("Where will this be accessed from?", &w.a.Deployment, deploymentOptions...).
		Description("Internal = LAN / VPN / Tailscale only.  External = reachable from the internet."))
}

func (w *wizard) fullDomain() error {
	a := w.a
	old := a.Domain
	if err := w.form(requiredInput("Your domain (e.g. example.com)", &a.Domain, "a domain is required")); err != nil {
		return err
	}
	if a.Hosts.Dashboard == "" || (old != "" && a.Hosts.Dashboard == "traefik."+old) {
		a.Hosts.Dashboard = "traefik." + a.Domain
	}
	if a.Hosts.Manager == "" || (old != "" && a.Hosts.Manager == "manager."+old) {
		a.Hosts.Manager = "manager." + a.Domain
	}
	return w.form(
		requiredInput("Traefik dashboard subdomain", &a.Hosts.Dashboard, "a hostname is required"),
		requiredInput("Traefik Manager subdomain", &a.Hosts.Manager, "a hostname is required"),
		confirm("Enable Traefik API dashboard UI?", &a.Dashboard),
	)
}

func (w *wizard) fullTLS() error {
	return w.tls()
}

func (w *wizard) fullMounts() error {
	a := w.a
	err := w.mounts(mountsInfo, []mount{
		{ask: "Mount access logs?", on: &a.Mounts.AccessLogs},
		{ask: "Mount SSL certs (acme.json)?", on: &a.Mounts.Certs},
		{ask: "Mount Traefik static config (traefik.yml)?", on: &a.Mounts.StaticConfig},
	})
	if err != nil {
		return err
	}
	if !a.Mounts.StaticConfig {
		a.Restart.Method = answers.RestartNone
		return nil
	}
	return w.restartMethodDocker(false)
}

func (w *wizard) fullCrowdSec() error {
	a := w.a
	add := a.CrowdSec.Mode != answers.CrowdSecNone
	err := w.groups(huh.NewGroup(confirm("Add CrowdSec?", &add)).
		Description("CrowdSec detects intrusions and bans malicious IPs. Visible in the CrowdSec tab in Traefik Manager."))
	if err != nil {
		return err
	}
	if !add {
		a.CrowdSec.Mode = answers.CrowdSecNone
		return nil
	}
	err = w.form(selectOne("CrowdSec setup", &a.CrowdSec.Mode,
		huh.NewOption("Install as part of this stack", answers.CrowdSecInstall),
		huh.NewOption("Connect to existing instance", answers.CrowdSecConnect),
	))
	if err != nil {
		return err
	}
	if a.CrowdSec.Mode == answers.CrowdSecConnect {
		a.CrowdSec.LAPIURL = orDefault(a.CrowdSec.LAPIURL, answers.DefaultLAPIURL)
		err = w.form(
			requiredInput("CrowdSec LAPI URL", &a.CrowdSec.LAPIURL, "a lapi url is required"),
			w.secret(answers.SecretCrowdSecAPIKey, "CrowdSec API key (bouncer, for decisions)", "a crowdsec api key is required"),
			input("CrowdSec machine ID (optional, for alerts)", &a.CrowdSec.MachineID).
				Description("Machine credentials are needed to view alerts and unban (the bouncer key cannot). Create one with: cscli machines add traefik-manager --auto"),
		)
		if err != nil {
			return err
		}
		if a.CrowdSec.MachineID != "" {
			if err := w.form(w.secret(answers.SecretCrowdSecMachinePassword, "CrowdSec machine password", "a machine password is required when a machine id is set")); err != nil {
				return err
			}
		}
	}
	if a.CrowdSec.Mode == answers.CrowdSecInstall && !a.Mounts.AccessLogs {
		w.u.Warn("CrowdSec reads Traefik access logs - enabling access log mount.")
		a.Mounts.AccessLogs = true
	}
	return nil
}

func (w *wizard) fullNetwork() error {
	a := w.a
	return w.form(
		requiredInput("Docker network name", &a.Network.Name, "a network name is required"),
		portInput("Traefik internal API port", &a.Network.TraefikAPIPort),
	)
}

func (w *wizard) tmdGeneral() error {
	return w.form(pathInput("Install directory", &w.a.Dir))
}

func (w *wizard) tmdNetwork() error {
	a := w.a
	if err := w.form(confirm("Connect to an existing Traefik Docker network?", &a.Network.External)); err != nil {
		return err
	}
	if a.Network.External {
		if a.Network.Name == "" || a.Network.Name == answers.DefaultTMNetwork {
			a.Network.Name = answers.DefaultNetwork
		}
		return w.form(requiredInput("Traefik network name", &a.Network.Name, "a network name is required"))
	}
	if a.Network.Name == "" || a.Network.Name == answers.DefaultNetwork {
		a.Network.Name = answers.DefaultTMNetwork
	}
	return w.form(requiredInput("Docker network name", &a.Network.Name, "a network name is required"))
}

func (w *wizard) tmdAccess() error {
	a := w.a
	if err := w.form(confirm("Expose via Traefik labels (requires Traefik on same network)?", &a.Access.ViaTraefik)); err != nil {
		return err
	}
	if !a.Access.ViaTraefik {
		a.Access.Port = orDefault(a.Access.Port, "5000")
		a.TLS = answers.TLS{Method: answers.TLSNone}
		return w.form(portInput("Port to expose on host", &a.Access.Port))
	}
	err := w.form(requiredInput("Traefik Manager domain (e.g. manager.example.com)", &a.Hosts.Manager, "a domain is required for Traefik labels"))
	if err != nil {
		return err
	}
	return w.tlsExisting()
}

func (w *wizard) tmdMounts() error {
	a := w.a
	err := w.mounts(mountsInfo, []mount{
		{ask: "Mount access logs?", path: "Path to Traefik access log", on: &a.Mounts.AccessLogs, value: &a.Mounts.AccessLogPath, def: answers.DefaultAccessLogPath},
		{ask: "Mount SSL certs (acme.json)?", path: "Path to acme.json", on: &a.Mounts.Certs, value: &a.Mounts.AcmePath, def: answers.DefaultAcmePath},
		{ask: "Mount Traefik static config (traefik.yml)?", path: "Path to traefik.yml", on: &a.Mounts.StaticConfig, value: &a.Mounts.StaticConfigPath, def: answers.DefaultStaticConfigPath},
	})
	if err != nil {
		return err
	}
	if !a.Mounts.StaticConfig {
		a.Restart.Method = answers.RestartNone
		return nil
	}
	return w.restartMethodDocker(true)
}

func (w *wizard) tmnGeneral() error {
	a := w.a
	return w.form(
		pathInput("Install directory", &a.Native.InstallDir),
		pathInput("Data directory", &a.Native.DataDir),
		portInput("Port", &a.Native.Port),
	)
}

func (w *wizard) tmnUser() error {
	return w.form(confirm("Create a dedicated system user (traefik-manager)?", &w.a.Native.ServiceUser))
}

func (w *wizard) tmnConfig() error {
	a := w.a
	if err := w.configLayout(); err != nil {
		return err
	}
	if a.Config.Layout == answers.LayoutSingle {
		a.Config.Path = orDefault(a.Config.Path, "/etc/traefik/dynamic.yml")
		return w.form(pathInput("Path to Traefik dynamic config file", &a.Config.Path))
	}
	a.Config.Dir = orDefault(a.Config.Dir, "/etc/traefik/conf.d")
	return w.form(pathInput("Path to Traefik dynamic config directory", &a.Config.Dir))
}

func (w *wizard) tmnMounts() error {
	a := w.a
	err := w.mounts(mountsInfo, []mount{
		{ask: "Mount SSL certs (acme.json)?", path: "Path to acme.json", on: &a.Mounts.Certs, value: &a.Mounts.AcmePath, def: answers.DefaultAcmePath},
		{ask: "Mount access logs?", path: "Path to Traefik access log", on: &a.Mounts.AccessLogs, value: &a.Mounts.AccessLogPath, def: answers.DefaultAccessLogPath},
		{ask: "Mount Traefik static config (traefik.yml)?", path: "Path to traefik.yml", on: &a.Mounts.StaticConfig, value: &a.Mounts.StaticConfigPath, def: answers.DefaultStaticConfigPath},
	})
	if err != nil {
		return err
	}
	if !a.Mounts.StaticConfig {
		a.Restart.Method = answers.RestartNone
		a.Restart.TraefikSystemd = false
		return nil
	}
	return w.restartMethodNative()
}

func (w *wizard) restartMethodNative() error {
	a := w.a
	err := w.groups(huh.NewGroup(selectOne("How is Traefik running on this server?", &a.Restart.TraefikSystemd,
		huh.NewOption("Docker", false),
		huh.NewOption("Linux service (systemd)", true),
	)).Title("Static Config Editor"))
	if err != nil {
		return err
	}
	if a.Restart.TraefikSystemd {
		a.Restart.Method = answers.RestartPoisonPill
		a.Restart.TraefikService = orDefault(a.Restart.TraefikService, "traefik")
		a.Restart.SignalFile = orDefault(a.Restart.SignalFile, answers.DefaultNativeSignalFile)
		return w.form(
			requiredInput("Traefik service name", &a.Restart.TraefikService, "a service name is required"),
			pathInput("Signal file path", &a.Restart.SignalFile),
		)
	}
	err = w.form(selectOne("Restart method", &a.Restart.Method, nativeRestartOptions...).
		Description("Choose how TM should restart Traefik after saving static config changes."))
	if err != nil {
		return err
	}
	if a.Restart.Method == answers.RestartSocket {
		a.Restart.Container = orDefault(a.Restart.Container, "traefik")
		return w.form(containerInput(&a.Restart.Container))
	}
	a.Restart.SignalFile = orDefault(a.Restart.SignalFile, answers.DefaultNativeSignalFile)
	return w.form(pathInput("Signal file path", &a.Restart.SignalFile))
}

func (w *wizard) agentAPIKey() error {
	return w.groups(huh.NewGroup(w.secret(answers.SecretTMAAPIKey, "API key (TMA_API_KEY)", "api key is required")).
		Description("Generate this in TM Settings -> Agents before running the installer."))
}

func (w *wizard) agentTraefik() error {
	a := w.a
	fields := []huh.Field{
		checkedInput("Traefik API URL", &a.Agent.TraefikURL, validURL),
		pathInput("Dynamic config path", &a.Agent.ConfigPath),
	}
	if a.Mode == answers.ModeAgentBinary {
		fields = append(fields, portInput("Agent port", &a.Agent.Port))
	}
	if err := w.form(fields...); err != nil {
		return err
	}
	fields = nil
	if strings.HasPrefix(a.Agent.TraefikURL, "https://") {
		fields = append(fields, confirm("Skip TLS verification? (needed for self-signed / Cloudflare Origin certs)", &a.Agent.InsecureTLS))
	} else {
		a.Agent.InsecureTLS = false
	}
	fields = append(fields,
		confirm("Is the Traefik API behind basic auth (username / password)?", &a.Agent.BasicAuth),
		confirm("Mount static config (traefik.yml)?", &a.Mounts.StaticConfig),
	)
	if err := w.form(fields...); err != nil {
		return err
	}
	fields = nil
	if a.Agent.BasicAuth {
		fields = append(fields,
			requiredInput("Traefik API username", &a.Agent.BasicAuthUser, "a username is required"),
			w.secret(answers.SecretTraefikAPIPassword, "Traefik API password", ""),
		)
	} else {
		a.Agent.BasicAuthUser = ""
	}
	if a.Mounts.StaticConfig {
		a.Mounts.StaticConfigPath = orDefault(a.Mounts.StaticConfigPath, answers.DefaultStaticConfigPath)
		fields = append(fields, pathInput("Static config path", &a.Mounts.StaticConfigPath))
	}
	if len(fields) == 0 {
		return nil
	}
	return w.form(fields...)
}

func (w *wizard) agentTraefikInstall() error {
	a := w.a
	err := w.groups(huh.NewGroup(
		selectOne("Where will this be accessed from?", &a.Deployment, deploymentOptions...),
		confirm("Enable Traefik dashboard?", &a.Dashboard),
	).Description("Traefik will be deployed on the same server alongside the agent."))
	if err != nil {
		return err
	}
	if a.Dashboard {
		if err := w.form(input("Dashboard hostname (e.g. traefik.example.com)", &a.Hosts.Dashboard)); err != nil {
			return err
		}
	} else {
		a.Hosts.Dashboard = ""
	}
	w.u.Section("TLS / Certificates")
	if err := w.tls(); err != nil {
		return err
	}
	w.u.Section("Dynamic Config")
	err = w.form(
		layoutSelect(&a.Config.Layout),
		requiredInput("Docker network name", &a.Network.Name, "a network name is required"),
		portInput("Traefik internal API port", &a.Network.TraefikAPIPort),
	)
	if err != nil {
		return err
	}
	w.u.Section("Agent access")
	err = w.groups(huh.NewGroup(confirm("Expose agent via Traefik label (recommended for HTTPS)?", &a.Access.ViaTraefik)).
		Description("The agent port is always bound. A Traefik label adds a hostname + TLS route on top."))
	if err != nil {
		return err
	}
	if !a.Access.ViaTraefik {
		a.Hosts.Agent = ""
		return nil
	}
	return w.form(input("Agent hostname (e.g. agent.example.com)", &a.Hosts.Agent))
}

func (w *wizard) agentPaths() error {
	a := w.a
	info := agentPaths
	var items []mount
	if a.Mode == answers.ModeAgentDockerTraefik {
		info += " Access logs and acme.json are always mounted for this install."
	} else {
		items = append(items,
			mount{ask: "Mount ACME / certs (acme.json)?", path: "ACME path", on: &a.Mounts.Certs, value: &a.Mounts.AcmePath, def: answers.DefaultAcmePath},
			mount{ask: "Mount access logs?", path: "Access log path", on: &a.Mounts.AccessLogs, value: &a.Mounts.AccessLogPath, def: answers.DefaultAccessLogPath},
		)
	}
	items = append(items, mount{ask: "Mount plugins directory?", path: "Plugins dir", on: &a.Mounts.Plugins, value: &a.Mounts.PluginsDir, def: answers.DefaultPluginsDir})
	return w.mounts(info, items)
}

func (w *wizard) agentRestart() error {
	a := w.a
	err := w.groups(huh.NewGroup(selectOne("Restart method", &a.Restart.Method, agentRestartOptions...)).
		Description("Allows the agent to restart Traefik after static config changes."))
	if err != nil {
		return err
	}
	a.Restart.Container = orDefault(a.Restart.Container, "traefik")
	switch a.Restart.Method {
	case answers.RestartProxy:
		a.Restart.DockerHost = orDefault(a.Restart.DockerHost, answers.DefaultSocketProxyHost)
		return w.form(
			requiredInput("Docker host", &a.Restart.DockerHost, "a docker host is required"),
			containerInput(&a.Restart.Container),
		)
	case answers.RestartPoisonPill:
		a.Restart.SignalFile = orDefault(a.Restart.SignalFile, answers.DefaultSignalFile)
		return w.form(pathInput("Signal file path", &a.Restart.SignalFile))
	case answers.RestartSocket:
		return w.form(containerInput(&a.Restart.Container))
	}
	return nil
}

func (w *wizard) agentCrowdSec() error {
	a := w.a
	opts := []huh.Option[string]{huh.NewOption("None", answers.CrowdSecNone)}
	if a.Mode != answers.ModeAgentBinary {
		opts = append(opts, huh.NewOption("Install alongside agent", answers.CrowdSecInstall))
	}
	opts = append(opts, huh.NewOption("Connect to existing instance", answers.CrowdSecConnect))
	if err := w.form(selectOne("CrowdSec integration", &a.CrowdSec.Mode, opts...)); err != nil {
		return err
	}
	switch a.CrowdSec.Mode {
	case answers.CrowdSecInstall:
		a.CrowdSec.LAPIURL = answers.DefaultLAPIURL
		if !a.Mounts.AccessLogs {
			w.u.Warn("CrowdSec reads Traefik access logs - enabling access log mount.")
			a.Mounts.AccessLogs = true
			a.Mounts.AccessLogPath = orDefault(a.Mounts.AccessLogPath, answers.DefaultAccessLogPath)
			if err := w.form(pathInput("Access log path", &a.Mounts.AccessLogPath)); err != nil {
				return err
			}
		}
		w.u.OK("CrowdSec will be installed alongside the agent")
	case answers.CrowdSecConnect:
		a.CrowdSec.LAPIURL = orDefault(a.CrowdSec.LAPIURL, answers.DefaultLAPIURL)
		return w.form(
			requiredInput("LAPI URL", &a.CrowdSec.LAPIURL, "a lapi url is required"),
			w.secret(answers.SecretCrowdSecAPIKey, "API key", "a crowdsec api key is required"),
		)
	}
	return nil
}

func (w *wizard) agentGit() error {
	a := w.a
	if err := w.form(confirm("Enable git backup?", &a.Agent.Git.Enabled)); err != nil {
		return err
	}
	if !a.Agent.Git.Enabled {
		return nil
	}
	a.Agent.Git.Branch = orDefault(a.Agent.Git.Branch, "main")
	return w.form(
		requiredInput("Repository URL", &a.Agent.Git.Repo, "a repository url is required"),
		requiredInput("Branch", &a.Agent.Git.Branch, "a branch is required"),
		input("Username", &a.Agent.Git.User),
		w.secret(answers.SecretGitBackupToken, "Access token", ""),
		confirm("Auto-push on config change?", &a.Agent.Git.AutoPush),
	)
}

func (w *wizard) agentLocation() error {
	a := w.a
	return w.form(
		pathInput("Install directory", &a.Dir),
		portInput("Agent port", &a.Agent.Port),
	)
}
