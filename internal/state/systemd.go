package state

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

var (
	nativeUnitPath     = "/etc/systemd/system/traefik-manager.service"
	agentUnitPath      = "/etc/systemd/system/tma.service"
	restartPathUnit    = "/etc/systemd/system/traefik-restart.path"
	restartServiceUnit = "/etc/systemd/system/traefik-restart.service"
)

var (
	bindRe             = regexp.MustCompile(`(?:--bind|-b)[\s=]+[^\s:]*:(\d+)`)
	systemctlRestartRe = regexp.MustCompile(`systemctl\s+restart\s+([^\s'"&;]+)`)
)

func AdoptSystemd() (*State, map[string]string, error) {
	if exists(nativeUnitPath) {
		return AdoptUnit(answers.ModeTMNative)
	}
	if exists(agentUnitPath) {
		return AdoptUnit(answers.ModeAgentBinary)
	}
	return nil, nil, fmt.Errorf("%w: neither %s nor %s exists", ErrNotFound, nativeUnitPath, agentUnitPath)
}

func AdoptUnit(mode answers.Mode) (*State, map[string]string, error) {
	st, secrets, err := inspectUnit(mode)
	if err != nil {
		return nil, nil, err
	}
	return st, secrets, nil
}

func inspectUnit(mode answers.Mode) (*State, map[string]string, error) {
	var unitPath string
	switch mode {
	case answers.ModeTMNative:
		unitPath = nativeUnitPath
	case answers.ModeAgentBinary:
		unitPath = agentUnitPath
	default:
		return nil, nil, fmt.Errorf("mode %q is not a systemd install", mode)
	}
	if !exists(unitPath) {
		return nil, nil, fmt.Errorf("%w: %s does not exist", ErrNotFound, unitPath)
	}
	data, err := readFile(unitPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", unitPath, err)
	}
	u := parseUnit(data)
	a := answers.Defaults(mode)
	secrets := map[string]string{}
	owned := map[string]string{unitPath: Hash(data)}
	switch mode {
	case answers.ModeTMNative:
		applyNativeUnit(a, u, owned)
	case answers.ModeAgentBinary:
		applyAgentEnv(a, u.env, secrets)
	}
	a.Finalize()
	st := &State{
		Version:     Version,
		Mode:        mode,
		TMVersion:   AdoptedVersion,
		InstalledAt: modTime(unitPath),
		Adopted:     true,
		Dir:         dirOf(a),
		OwnedFiles:  owned,
		Answers:     *a,
		Path:        PathFor(a),
	}
	return st, secrets, nil
}

func applyNativeUnit(a *answers.Answers, u *unit, owned map[string]string) {
	if v := u.first("WorkingDirectory"); v != "" {
		a.Native.InstallDir = v
	}
	a.Native.ServiceUser = u.first("User") == "traefik-manager"
	if m := bindRe.FindStringSubmatch(u.first("ExecStart")); m != nil {
		a.Native.Port = m[1]
	}
	env := u.env
	if v, ok := env.get("CONFIG_DIR"); ok && v != "" {
		a.Config.Layout = answers.LayoutDirectory
		a.Config.Dir = v
	} else if v, ok := env.get("CONFIG_PATH"); ok && v != "" {
		a.Config.Layout = answers.LayoutSingle
		a.Config.Path = v
	}
	if v, ok := env.get("BACKUP_DIR"); ok && v != "" {
		a.Native.DataDir = filepath.Dir(v)
	} else if v, ok := env.get("SETTINGS_PATH"); ok && v != "" {
		a.Native.DataDir = filepath.Dir(v)
	}
	a.Mounts.Certs, a.Mounts.AcmePath = envPath(env, "ACME_JSON_PATH", a.Mounts.AcmePath)
	a.Mounts.AccessLogs, a.Mounts.AccessLogPath = envPath(env, "ACCESS_LOG_PATH", a.Mounts.AccessLogPath)
	a.Mounts.StaticConfig, a.Mounts.StaticConfigPath = envPath(env, "STATIC_CONFIG_PATH", a.Mounts.StaticConfigPath)
	a.Mounts.Plugins = false
	applyRestartEnv(a, env)
	a.Restart.TraefikSystemd = exists(restartPathUnit)
	if !a.Restart.TraefikSystemd {
		return
	}
	if data, err := readFile(restartPathUnit); err == nil {
		owned[restartPathUnit] = Hash(data)
	}
	if !exists(restartServiceUnit) {
		return
	}
	data, err := readFile(restartServiceUnit)
	if err != nil {
		return
	}
	owned[restartServiceUnit] = Hash(data)
	if m := systemctlRestartRe.FindStringSubmatch(parseUnit(data).first("ExecStart")); m != nil {
		a.Restart.TraefikService = m[1]
	}
}

type unit struct {
	values map[string][]string
	env    kvList
}

func (u *unit) first(key string) string {
	if vs := u.values[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func parseUnit(data []byte) *unit {
	u := &unit{values: map[string][]string{}}
	var logical []string
	cur := ""
	cont := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if cont {
			cur += " " + strings.TrimSpace(line)
		} else {
			cur = line
		}
		trimmed := strings.TrimRight(cur, " \t")
		if strings.HasSuffix(trimmed, "\\") {
			cur = strings.TrimRight(strings.TrimSuffix(trimmed, "\\"), " \t")
			cont = true
			continue
		}
		cont = false
		logical = append(logical, cur)
	}
	if cont {
		logical = append(logical, cur)
	}
	for _, line := range logical {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "[") {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		u.values[k] = append(u.values[k], v)
		if k == "Environment" {
			for _, tok := range splitQuoted(v) {
				ek, ev, _ := strings.Cut(tok, "=")
				u.env = append(u.env, kv{Key: ek, Value: ev})
			}
		}
	}
	return u
}

func splitQuoted(s string) []string {
	var out []string
	var b strings.Builder
	inQuote := byte(0)
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			} else {
				b.WriteByte(c)
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == ' ' || c == '\t':
			flush()
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return out
}
