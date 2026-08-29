package render

import (
	"path/filepath"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

type traefikUnitView struct {
	User         string
	WorkingDir   string
	StateDir     string
	LogDir       string
	EnvFile      string
	Env          []string
	ExecStartArg string
	ConfigArg    string
}

type dashboardView struct {
	Host       string
	EntryPoint string
	TLS        bool
	Resolver   string
}

func newDashboardView(a *answers.Answers) dashboardView {
	return dashboardView{
		Host:       a.Hosts.Dashboard,
		EntryPoint: a.EntryPoint(),
		TLS:        a.TLS.Method != answers.TLSNone,
		Resolver:   answers.CertResolver,
	}
}

func newTraefikUnitView(a *answers.Answers) traefikUnitView {
	v := traefikUnitView{
		User:         "traefik-manager",
		WorkingDir:   answers.NativeTraefikStateDir,
		StateDir:     answers.NativeTraefikStateDir,
		LogDir:       answers.NativeTraefikLogDir,
		Env:          dnsUnitEnv(a),
		ExecStartArg: SystemdQuote(answers.TraefikBinaryPath),
		ConfigArg:    SystemdQuote("--configfile=" + answers.DefaultStaticConfigPath),
	}
	if a.TLS.Method == answers.TLSDNS && hasSecretDNSVars(a) {
		v.EnvFile = NativeEnvPath
	}
	return v
}

func newFullNativeTMView(a *answers.Answers) nativeView {
	v := baseNativeView(a)
	v.OptionalEnv = append(v.OptionalEnv, SystemdQuote("TRAEFIK_API_URL=http://127.0.0.1:"+a.Network.TraefikAPIPort))
	if a.Mounts.Certs {
		v.OptionalEnv = append(v.OptionalEnv, SystemdQuote("ACME_JSON_PATH="+a.Mounts.AcmePath))
	}
	v.OptionalEnv = append(v.OptionalEnv, SystemdQuote("ACCESS_LOG_PATH="+a.Mounts.AccessLogPath))
	if a.Mounts.StaticConfig {
		v.OptionalEnv = append(v.OptionalEnv,
			SystemdQuote("STATIC_CONFIG_PATH="+a.Mounts.StaticConfigPath),
			SystemdQuote("RESTART_METHOD="+a.Restart.Method),
			SystemdQuote("SIGNAL_FILE_PATH="+a.Restart.SignalFile),
		)
	}
	if a.CrowdSec.Mode != answers.CrowdSecNone {
		v.EnvFile = NativeEnvPath
		v.OptionalEnv = append(v.OptionalEnv, nativeCrowdSecEnv(a)...)
	}
	return v
}

func nativeCrowdSecEnv(a *answers.Answers) []string {
	if a.CrowdSec.Mode == answers.CrowdSecNone {
		return nil
	}
	env := []string{SystemdQuote("CROWDSEC_LAPI_URL=" + a.CrowdSec.LAPIURL)}
	if a.CrowdSec.MachineID != "" {
		env = append(env, SystemdQuote("CROWDSEC_MACHINE_ID="+a.CrowdSec.MachineID))
	}
	if a.CrowdSec.AlertLimit != "" {
		env = append(env, SystemdQuote("CROWDSEC_ALERT_LIMIT="+a.CrowdSec.AlertLimit))
	}
	return env
}

func renderFullNative(a *answers.Answers) (*Output, error) {
	tls := a.TLS.Method != answers.TLSNone
	single := a.Config.Layout == answers.LayoutSingle
	static := a.Mounts.StaticConfig

	b := &builder{}
	b.dir(answers.NativeTraefikConfigDir)
	b.dir(answers.NativeTraefikDynamicDir)
	b.dir(answers.NativeTraefikStateDir)
	b.dir(filepath.Join(answers.NativeTraefikStateDir, "plugins-storage"))
	b.dir(answers.NativeTraefikLogDir)
	b.dir(filepath.Join(a.Native.DataDir, "backups"))
	if static {
		b.dir(filepath.Dir(a.Restart.SignalFile))
	}
	if a.CrowdSec.Mode == answers.CrowdSecInstall {
		b.dir(CrowdSecAcquisDir)
	}
	b.systemTmpl(answers.DefaultStaticConfigPath, 0o644, "traefik.yml.tmpl", newNativeTraefikView(a))
	b.systemTmpl(TraefikUnitPath, 0o644, "traefik.service.tmpl", newTraefikUnitView(a))
	b.systemTmpl(NativeUnitPath, 0o644, "tm.service.tmpl", newFullNativeTMView(a))
	if static {
		w := restartWatcherView{
			SignalFile:    a.Restart.SignalFile,
			SignalFileArg: SystemdQuote(a.Restart.SignalFile),
			Service:       a.Restart.TraefikService,
			ServiceArg:    SystemdQuote(a.Restart.TraefikService),
		}
		b.systemTmpl(RestartPathUnit, 0o644, "traefik-restart.path.tmpl", w)
		b.systemTmpl(RestartServiceUnit, 0o644, "traefik-restart.service.tmpl", w)
	}
	b.systemTmpl(LogrotatePath, 0o644, "traefik-logrotate.tmpl", nil)
	dash := newDashboardView(a)
	if single {
		b.systemSeedTmpl(a.Config.Path, 0o644, "dynamic.yml.tmpl", dash)
	} else {
		if dash.Host != "" {
			b.systemSeedTmpl(filepath.Join(a.Config.Dir, "dashboard.yml"), 0o644, "dashboard.yml.tmpl", dash)
		}
		b.systemSeedTmpl(filepath.Join(a.Config.Dir, "example-app.yml.disabled"), 0o644, "example-app.yml.tmpl", nil)
	}
	if tls {
		b.systemSeed(answers.NativeAcmePath, 0o600, "")
	}
	b.systemSeed(answers.DefaultAccessLogPath, 0o644, "")
	b.acquisNative(a)
	if len(a.SecretKeys()) > 0 {
		b.system(NativeEnvPath, 0o600, EnvFile(a))
	}
	return b.result()
}
