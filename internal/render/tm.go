package render

import (
	"errors"
	"path/filepath"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

type tmView struct {
	Image        string
	Network      string
	External     bool
	SocketProxy  bool
	NamedVolumes []string
	Port         string
	Volumes      []string
	Env          []string
	Labels       []string
	CrowdSec     *crowdsecView
}

func renderTMDocker(a *answers.Answers) (*Output, error) {
	install := a.CrowdSec.Mode == answers.CrowdSecInstall
	b := &builder{}
	b.dir("config")
	b.dir("backups")
	if install {
		b.dir("crowdsec")
	}
	b.tmpl("docker-compose.yml", 0o644, "compose-tm.tmpl", newTMView(a))
	b.env(a)
	if a.Config.Layout == answers.LayoutSingle {
		b.seedTmpl("config/dynamic.yml", 0o644, "dynamic.yml.tmpl", dashboardView{})
	}
	if install {
		b.acquisDocker()
	}
	return b.result()
}

func newTMView(a *answers.Answers) tmView {
	static := a.Mounts.StaticConfig
	proxy := static && a.Restart.Method == answers.RestartProxy
	pill := static && a.Restart.Method == answers.RestartPoisonPill
	socket := static && a.Restart.Method == answers.RestartSocket
	single := a.Config.Layout == answers.LayoutSingle
	install := a.CrowdSec.Mode == answers.CrowdSecInstall
	v := tmView{
		Image:       answers.ManagerImage + ":" + a.ImageTag(),
		Network:     a.Network.Name,
		External:    a.Network.External,
		SocketProxy: proxy,
	}
	if pill {
		v.NamedVolumes = append(v.NamedVolumes, "tm-signals")
	}
	if install {
		v.NamedVolumes = append(v.NamedVolumes, "crowdsec_data")
	}
	if !a.Access.ViaTraefik {
		v.Port = a.Access.Port
	}
	if socket {
		v.Volumes = append(v.Volumes, dockerSocketVolume)
	}
	v.Volumes = append(v.Volumes, "./config:/app/config", "./backups:/app/backups")
	if a.Mounts.AccessLogs {
		v.Volumes = append(v.Volumes, a.Mounts.AccessLogPath+":/app/logs/access.log:ro")
	}
	if a.Mounts.Certs {
		v.Volumes = append(v.Volumes, a.Mounts.AcmePath+":/app/acme.json:ro")
	}
	if static {
		v.Volumes = append(v.Volumes, a.Mounts.StaticConfigPath+":/app/traefik.yml")
		if pill {
			v.Volumes = append(v.Volumes, "tm-signals:/signals")
		}
	}
	if single {
		v.Volumes = append(v.Volumes, "./config/dynamic.yml:/app/config/dynamic.yml")
	} else {
		v.Volumes = append(v.Volumes, "./config:/app/config/dynamic")
	}
	v.Env = append(tmEnv(a, static, proxy, pill, single), crowdsecEnv(a)...)
	if a.Access.ViaTraefik {
		v.Labels = tmLabels(a)
	}
	if install {
		v.CrowdSec = &crowdsecView{
			Network:    a.Network.Name,
			BouncerVar: crowdsecBouncerFull,
			LogVolume:  a.Mounts.AccessLogPath + ":" + traefikAccessLog + ":ro",
		}
	}
	return v
}

type nativeView struct {
	User         string
	InstallDir   string
	ExecStartArg string
	HomeEnv      string
	BackupEnv    string
	SettingsEnv  string
	DataDir      string
	Port         string
	ConfigEnv    string
	EnvFile      string
	OptionalEnv  []string
}

func baseNativeView(a *answers.Answers) nativeView {
	return nativeView{
		User:         "traefik-manager",
		InstallDir:   a.Native.InstallDir,
		ExecStartArg: SystemdQuote(filepath.Join(a.Native.InstallDir, "venv", "bin", "gunicorn")),
		HomeEnv:      SystemdQuote("HOME=" + a.Native.InstallDir),
		BackupEnv:    SystemdQuote("BACKUP_DIR=" + filepath.Join(a.Native.DataDir, "backups")),
		SettingsEnv:  SystemdQuote("SETTINGS_PATH=" + filepath.Join(a.Native.DataDir, "manager.yml")),
		DataDir:      a.Native.DataDir,
		Port:         a.Native.Port,
		ConfigEnv:    nativeConfigEnv(a),
	}
}

func nativeConfigEnv(a *answers.Answers) string {
	if a.Config.Layout == answers.LayoutSingle {
		return SystemdQuote("CONFIG_PATH=" + a.Config.Path)
	}
	return SystemdQuote("CONFIG_DIR=" + a.Config.Dir)
}

type restartWatcherView struct {
	SignalFile    string
	SignalFileArg string
	Service       string
	ServiceArg    string
}

func renderTMNative(a *answers.Answers, user string) (*Output, error) {
	static := a.Mounts.StaticConfig
	pill := static && a.Restart.Method == answers.RestartPoisonPill
	socket := static && a.Restart.Method == answers.RestartSocket
	v := baseNativeView(a)
	if !a.Native.ServiceUser {
		if user == "" {
			return nil, errors.New("render: a user name is required when native.service_user is false")
		}
		v.User = user
	}
	if a.Mounts.Certs {
		v.OptionalEnv = append(v.OptionalEnv, SystemdQuote("ACME_JSON_PATH="+a.Mounts.AcmePath))
	}
	if a.Mounts.AccessLogs {
		v.OptionalEnv = append(v.OptionalEnv, SystemdQuote("ACCESS_LOG_PATH="+a.Mounts.AccessLogPath))
	}
	if static {
		v.OptionalEnv = append(v.OptionalEnv,
			SystemdQuote("STATIC_CONFIG_PATH="+a.Mounts.StaticConfigPath),
			SystemdQuote("RESTART_METHOD="+a.Restart.Method),
		)
		if socket {
			v.OptionalEnv = append(v.OptionalEnv, SystemdQuote("TRAEFIK_CONTAINER="+a.Restart.Container))
		}
		if pill {
			v.OptionalEnv = append(v.OptionalEnv, SystemdQuote("SIGNAL_FILE_PATH="+a.Restart.SignalFile))
		}
	}
	if a.CrowdSec.Mode != answers.CrowdSecNone {
		v.EnvFile = NativeEnvPath
		v.OptionalEnv = append(v.OptionalEnv, nativeCrowdSecEnv(a)...)
	}

	b := &builder{}
	b.dir(filepath.Join(a.Native.DataDir, "backups"))
	if pill {
		b.dir(filepath.Dir(a.Restart.SignalFile))
	}
	if a.CrowdSec.Mode == answers.CrowdSecInstall {
		b.dir(CrowdSecAcquisDir)
	}
	b.systemTmpl(NativeUnitPath, 0o644, "tm.service.tmpl", v)
	if static && a.Restart.TraefikSystemd {
		w := restartWatcherView{
			SignalFile:    a.Restart.SignalFile,
			SignalFileArg: SystemdQuote(a.Restart.SignalFile),
			Service:       a.Restart.TraefikService,
			ServiceArg:    SystemdQuote(a.Restart.TraefikService),
		}
		b.systemTmpl(RestartPathUnit, 0o644, "traefik-restart.path.tmpl", w)
		b.systemTmpl(RestartServiceUnit, 0o644, "traefik-restart.service.tmpl", w)
	}
	b.acquisNative(a)
	if len(a.SecretKeys()) > 0 {
		b.system(NativeEnvPath, 0o600, EnvFile(a))
	}
	return b.result()
}
