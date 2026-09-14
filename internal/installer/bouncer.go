package installer

import (
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/render"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

func (in *Installer) readBouncerStatic(a *answers.Answers) render.Static {
	in.staticBackup = ""
	if !a.CrowdSec.BouncerPlugin || a.Mode.HasTraefik() {
		return render.Static{}
	}
	path := a.Mounts.StaticConfigPath
	data, err := host.ReadFile(path)
	if err != nil {
		return render.Static{Path: path, Note: "could not read " + path + ": " + err.Error()}
	}
	st := render.StaticFrom(path, data)
	if fi, err := os.Stat(path); err == nil {
		st.Mode = fi.Mode().Perm()
	}
	if st.Ready && st.Merged != "" {
		in.staticBackup = path
	}
	return st
}

func (in *Installer) backupStatic() {
	path := in.staticBackup
	in.staticBackup = ""
	if path == "" {
		return
	}
	data, err := host.ReadFile(path)
	if err != nil {
		in.UI.Warn("could not back up " + path + " before adding the plugin: " + err.Error())
		return
	}
	dest := path + render.BouncerBackupExt
	if err := host.WriteFile(dest, data, 0o600); err != nil {
		in.UI.Warn("could not back up " + path + " before adding the plugin: " + err.Error())
		return
	}
	in.UI.OK(path + " backed up to " + dest)
}

func (in *Installer) bouncerSummary(a *answers.Answers) {
	u := in.UI
	p := in.bouncer
	if !p.On {
		u.Warn("tm does not attach the CrowdSec bouncer to Traefik, so nothing is blocked yet - the CrowdSec tab shows what it detects.")
		if !a.BouncerPluginAvailable() {
			u.Info("The bouncer plugin is declared in traefik.yml, which this install does not manage. Set mounts.static_config and its path, then run tm reconfigure.")
		}
		return
	}
	u.Heading("CrowdSec bouncer plugin")
	if !p.Enabled {
		u.Warn("the bouncer plugin was not installed: " + p.Note)
		return
	}
	u.KVMuted("Plugin", render.BouncerModule+"  "+render.BouncerVersion)
	u.KVMuted("Declared in", in.bouncerPath(a, p.StaticPath))
	if p.MiddlewarePath == "" {
		u.Warn("the middleware was not written: " + p.Note)
		u.Info("add it to your dynamic config yourself:")
		u.Blank()
		for _, line := range strings.Split(strings.TrimRight(render.MiddlewareYAML(a, p), "\n"), "\n") {
			u.Code(line)
		}
		u.Blank()
	} else {
		u.KVMuted("Middleware", render.BouncerRef+"  "+in.bouncerPath(a, p.MiddlewarePath))
		u.Line("%s", ui.MutedStyle.Render("That file carries the bouncer key, so keep it with the rest of your Traefik config."))
		if len(p.TrustedIPs) > 0 {
			u.KVMuted("Trusted proxies", strings.Join(p.TrustedIPs, "  "))
		} else if !a.Mode.HasTraefik() {
			u.Info("the static config trusts no forwarded headers, so the bouncer bans the address Traefik connects from. Behind a reverse proxy or Cloudflare set entryPoints forwardedHeaders.trustedIPs, then run tm reconfigure.")
		}
	}
	if loopback(p.LapiHost) && traefikInDocker(a) {
		u.Warn("Traefik runs in Docker here, so " + p.LapiHost + " is the container's own loopback: give the plugin an address the Traefik container can reach.")
	}
	u.Warn("Nothing is blocked until you attach the middleware to your routers:")
	u.Blank()
	u.Code("  middlewares:")
	u.Code("    - " + render.BouncerRef)
	u.Blank()
}

func (in *Installer) bouncerPath(a *answers.Answers, path string) string {
	if filepath.IsAbs(path) || a.Dir == "" {
		return path
	}
	return filepath.Join(a.Dir, path)
}

func loopback(hostPort string) bool {
	h, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		h = hostPort
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func traefikInDocker(a *answers.Answers) bool {
	return a.Mode == answers.ModeTMNative && !a.Restart.TraefikSystemd
}
