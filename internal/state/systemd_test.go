package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

func unitSandbox(t *testing.T) string {
	t.Helper()
	isolate(t)
	dir := t.TempDir()
	saved := []string{nativeUnitPath, agentUnitPath, restartPathUnit, restartServiceUnit, nativeStatePath, agentBinaryStatePath}
	t.Cleanup(func() {
		nativeUnitPath, agentUnitPath, restartPathUnit, restartServiceUnit, nativeStatePath, agentBinaryStatePath = saved[0], saved[1], saved[2], saved[3], saved[4], saved[5]
	})
	nativeUnitPath = filepath.Join(dir, "systemd", "traefik-manager.service")
	agentUnitPath = filepath.Join(dir, "systemd", "tma.service")
	restartPathUnit = filepath.Join(dir, "systemd", "traefik-restart.path")
	restartServiceUnit = filepath.Join(dir, "systemd", "traefik-restart.service")
	nativeStatePath = filepath.Join(dir, "etc", "traefik-manager", "tm-state.yml")
	agentBinaryStatePath = filepath.Join(dir, "etc", "traefik-manager-agent", "tm-state.yml")
	return dir
}

func installUnit(t *testing.T, fixture, dest string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "units", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptSystemdNative(t *testing.T) {
	unitSandbox(t)
	installUnit(t, "traefik-manager.service", nativeUnitPath)
	installUnit(t, "traefik-restart.path", restartPathUnit)
	installUnit(t, "traefik-restart.service", restartServiceUnit)
	st, secrets, err := AdoptSystemd()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != answers.ModeTMNative || !st.Adopted || st.TMVersion != AdoptedVersion || st.Path != nativeStatePath || st.Dir != "/opt/traefik-manager" {
		t.Fatalf("state: %+v", st)
	}
	a := st.Answers
	if a.Native.InstallDir != "/opt/traefik-manager" || a.Native.DataDir != "/var/lib/traefik-manager" || a.Native.Port != "5001" || !a.Native.ServiceUser {
		t.Fatalf("native: %+v", a.Native)
	}
	if a.Config.Layout != answers.LayoutDirectory || a.Config.Dir != "/etc/traefik/conf.d" {
		t.Fatalf("config: %+v", a.Config)
	}
	m := a.Mounts
	if !m.Certs || m.AcmePath != "/etc/traefik/acme.json" || !m.AccessLogs || m.AccessLogPath != "/var/log/traefik/access.log" || !m.StaticConfig || m.StaticConfigPath != "/etc/traefik/traefik.yml" || m.Plugins {
		t.Fatalf("mounts: %+v", m)
	}
	r := a.Restart
	if r.Method != answers.RestartPoisonPill || r.SignalFile != "/var/lib/traefik-manager/signals/restart.sig" || !r.TraefikSystemd || r.TraefikService != "traefik-custom" {
		t.Fatalf("restart: %+v", r)
	}
	if a.CrowdSec.Mode != answers.CrowdSecNone {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	if len(secrets) != 0 {
		t.Fatalf("native install has no secrets, got %v", secrets)
	}
	for _, p := range []string{nativeUnitPath, restartPathUnit, restartServiceUnit} {
		data, _ := os.ReadFile(p)
		if st.OwnedFiles[p] != Hash(data) {
			t.Fatalf("%s not owned: %v", p, st.OwnedFiles)
		}
	}
	if len(st.OwnedFiles) != 3 {
		t.Fatalf("owned files: %v", st.OwnedFiles)
	}
	if _, err := os.Stat(nativeStatePath); err == nil {
		t.Fatal("adoption must not write state: a read-only command would leave a file behind")
	}
	if registryHas(t, nativeStatePath) {
		t.Fatal("adoption must not touch the registry")
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(nativeStatePath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Answers.Native != a.Native || loaded.Answers.Restart != a.Restart || loaded.Answers.Config != a.Config || loaded.Answers.Mounts != a.Mounts {
		t.Fatalf("round trip changed native answers: %+v", loaded.Answers)
	}
	changed, err := st.Modified()
	if err != nil || len(changed) != 0 {
		t.Fatalf("fresh adoption must not be modified: %v %v", changed, err)
	}
}

func TestAdoptSystemdNativePlain(t *testing.T) {
	unitSandbox(t)
	installUnit(t, "traefik-manager-plain.service", nativeUnitPath)
	st, _, err := AdoptSystemd()
	if err != nil {
		t.Fatal(err)
	}
	a := st.Answers
	if a.Native.InstallDir != "/home/alice/traefik-manager" || a.Native.DataDir != "/home/alice/tm-data" || a.Native.Port != "5000" || a.Native.ServiceUser {
		t.Fatalf("native: %+v", a.Native)
	}
	if a.Config.Layout != answers.LayoutSingle || a.Config.Path != "/etc/traefik/dynamic.yml" {
		t.Fatalf("config: %+v", a.Config)
	}
	if a.Mounts.Certs || a.Mounts.AccessLogs || a.Mounts.StaticConfig {
		t.Fatalf("mounts: %+v", a.Mounts)
	}
	if a.Restart.Method != answers.RestartNone || a.Restart.TraefikSystemd {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if len(st.OwnedFiles) != 1 {
		t.Fatalf("owned files: %v", st.OwnedFiles)
	}
}

func TestAdoptSystemdAgentBinary(t *testing.T) {
	unitSandbox(t)
	installUnit(t, "tma.service", agentUnitPath)
	st, secrets, err := AdoptSystemd()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != answers.ModeAgentBinary || st.Path != agentBinaryStatePath || st.Dir != "" {
		t.Fatalf("state: %+v", st)
	}
	a := st.Answers
	ag := a.Agent
	if ag.TraefikURL != "https://traefik.example.com:8443" || !ag.InsecureTLS || !ag.BasicAuth || ag.BasicAuthUser != "admin" || ag.ConfigPath != "/etc/traefik/dynamic" || ag.Port != "8095" {
		t.Fatalf("agent: %+v", ag)
	}
	m := a.Mounts
	if !m.Certs || !m.AccessLogs || !m.StaticConfig || m.StaticConfigPath != "/etc/traefik/traefik.yml" || !m.Plugins || m.PluginsDir != "/etc/traefik/plugins" {
		t.Fatalf("mounts: %+v", m)
	}
	if a.Restart.Method != answers.RestartSocket || a.Restart.Container != "traefik" {
		t.Fatalf("restart: %+v", a.Restart)
	}
	if a.CrowdSec.Mode != answers.CrowdSecConnect || a.CrowdSec.LAPIURL != "http://10.0.0.5:8080" {
		t.Fatalf("crowdsec: %+v", a.CrowdSec)
	}
	g := ag.Git
	if !g.Enabled || g.Repo != "https://github.com/example/traefik-config.git" || g.Branch != "main" || g.User != "deploy" || !g.AutoPush {
		t.Fatalf("git: %+v", g)
	}
	want := map[string]string{
		"TMA_API_KEY":          "tma-api-key-PLACEHOLDER",
		"TRAEFIK_API_PASSWORD": "traefik-api-password-PLACEHOLDER",
		"CROWDSEC_API_KEY":     "crowdsec-api-key-PLACEHOLDER",
		"GIT_BACKUP_TOKEN":     "git-backup-token-PLACEHOLDER",
	}
	wantSecrets(t, secrets, want)
	again, err := st.LiteralSecrets()
	if err != nil {
		t.Fatal(err)
	}
	wantSecrets(t, again, want)
	raw, _ := os.ReadFile(agentBinaryStatePath)
	for _, v := range want {
		if strings.Contains(string(raw), v) {
			t.Fatalf("state leaked %s", v)
		}
	}
	if len(st.OwnedFiles) != 1 || st.OwnedFiles[agentUnitPath] == "" {
		t.Fatalf("owned files: %v", st.OwnedFiles)
	}
}

func TestAdoptSystemdPrefersTMAndReportsMissing(t *testing.T) {
	unitSandbox(t)
	_, _, err := AdoptSystemd()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	_, _, err = AdoptUnit(answers.ModeAgentBinary)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the agent unit, got %v", err)
	}
	if _, _, err := AdoptUnit(answers.ModeFull); err == nil {
		t.Fatal("docker modes are not systemd installs")
	}
	installUnit(t, "tma.service", agentUnitPath)
	installUnit(t, "traefik-manager-plain.service", nativeUnitPath)
	st, _, err := AdoptSystemd()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != answers.ModeTMNative {
		t.Fatalf("expected tm-native to win, got %s", st.Mode)
	}
	st, _, err = AdoptUnit(answers.ModeAgentBinary)
	if err != nil || st.Mode != answers.ModeAgentBinary {
		t.Fatalf("explicit agent adoption failed: %v", err)
	}
}

func TestParseUnitQuotedEnvironment(t *testing.T) {
	u := parseUnit([]byte("[Service]\nEnvironment=\"A=b c\" D=e\nEnvironment='F=g'\nExecStart=/bin/x \\\n  --bind 127.0.0.1:9000 \\\n  app\n# comment\nUser=tm\n"))
	if v, _ := u.env.get("A"); v != "b c" {
		t.Fatalf("quoted value wrong: %q", v)
	}
	if v, _ := u.env.get("D"); v != "e" {
		t.Fatalf("second token wrong: %q", v)
	}
	if v, _ := u.env.get("F"); v != "g" {
		t.Fatalf("single quoted wrong: %q", v)
	}
	if u.first("ExecStart") != "/bin/x --bind 127.0.0.1:9000 app" {
		t.Fatalf("continuation wrong: %q", u.first("ExecStart"))
	}
	if m := bindRe.FindStringSubmatch(u.first("ExecStart")); m == nil || m[1] != "9000" {
		t.Fatalf("bind port not found: %v", m)
	}
	if u.first("User") != "tm" {
		t.Fatal("user not parsed")
	}
}

func TestAdoptSystemdTellsFullNativeFromTMNative(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	defer func(n, tr, e string) { nativeUnitPath, traefikUnitPath, nativeStatePath = n, tr, e }(nativeUnitPath, traefikUnitPath, nativeStatePath)
	nativeUnitPath = filepath.Join(dir, "traefik-manager.service")
	traefikUnitPath = filepath.Join(dir, "traefik.service")
	nativeStatePath = filepath.Join(dir, "tm-state.yml")

	tmUnit := "[Service]\nUser=traefik-manager\nWorkingDirectory=/opt/traefik-manager\nExecStart=/opt/traefik-manager/venv/bin/gunicorn --bind 0.0.0.0:5000 app:app\nEnvironment=\"SETTINGS_PATH=/var/lib/traefik-manager/manager.yml\"\n"
	if err := os.WriteFile(nativeUnitPath, []byte(tmUnit), 0o644); err != nil {
		t.Fatal(err)
	}

	st, _, err := AdoptSystemd()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != answers.ModeTMNative {
		t.Fatalf("with no traefik unit the mode must be tm-native, got %s", st.Mode)
	}

	foreign := "[Service]\nExecStart=/usr/bin/traefik --configfile=/srv/other/traefik.yml\n"
	if err := os.WriteFile(traefikUnitPath, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _, err = AdoptSystemd()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != answers.ModeTMNative {
		t.Fatalf("a traefik unit tm did not write must stay tm-native, got %s", st.Mode)
	}

	ours := "[Service]\nExecStart=\"/usr/local/bin/traefik\" \"--configfile=/etc/traefik/traefik.yml\"\n"
	if err := os.WriteFile(traefikUnitPath, []byte(ours), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _, err = AdoptSystemd()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != answers.ModeFullNative {
		t.Fatalf("a tm-written traefik unit means full-native, got %s", st.Mode)
	}
	if _, ok := st.OwnedFiles[traefikUnitPath]; !ok {
		t.Fatal("the traefik unit must be recorded as owned so reconfigure and uninstall see it")
	}
}
