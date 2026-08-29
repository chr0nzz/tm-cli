package installer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/render"
	"github.com/chr0nzz/tm-cli/internal/state"
)

func (in *Installer) Reconfigure(ctx context.Context, st *state.State, edit func(a *answers.Answers) error) error {
	if err := in.prepare(st); err != nil {
		return err
	}
	a := st.Answers.Clone()
	a.Secrets = in.ExistingSecrets(st)
	if err := edit(a); err != nil {
		return err
	}
	a.Finalize()
	if err := a.Validate(); err != nil {
		return err
	}
	if err := a.GenerateSecrets(); err != nil {
		return err
	}
	if st.Mode.IsDocker() && a.Dir != st.Dir {
		return fmt.Errorf("the install directory cannot be changed with reconfigure (was %s, now %s)", st.Dir, a.Dir)
	}
	if (st.Mode == answers.ModeTMNative || st.Mode == answers.ModeFullNative) && (a.Native.InstallDir != st.Answers.Native.InstallDir || a.Native.DataDir != st.Answers.Native.DataDir) {
		return fmt.Errorf("native.install_dir and native.data_dir cannot be changed with reconfigure")
	}
	out, err := render.Render(render.Input{Answers: a, User: host.CurrentUser()})
	if err != nil {
		return err
	}
	modified, err := st.Modified()
	if err != nil {
		return fmt.Errorf("check which files changed since tm wrote them: %w", err)
	}
	changed := map[string]bool{}
	for _, m := range modified {
		changed[m] = true
	}
	if st.Adopted {
		for _, f := range out.Files {
			if !f.CreateOnly {
				changed[f.Path] = true
			}
		}
	}
	foreign, err := foreignServices(st, out)
	if err != nil {
		return err
	}
	if len(foreign) > 0 {
		in.UI.Blank()
		in.UI.Warn("this directory's docker-compose.yml also defines " + strings.Join(foreign, ", "))
		in.UI.Info("tm only manages Traefik, Traefik Manager and the services it added; regenerating the file drops the rest")
		ok, err := in.confirm("Regenerate docker-compose.yml without "+strings.Join(foreign, ", ")+"?", false)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAborted
		}
	}
	overwrite := map[string]bool{}
	for _, f := range out.Files {
		if f.CreateOnly || f.Path == ".env" {
			continue
		}
		if !changed[f.Path] {
			continue
		}
		full := resolvePath(st.Dir, f.Path)
		ok, err := in.confirm(fmt.Sprintf("%s was modified outside tm since it was written. Overwrite it? (a backup is kept)", f.Path), false)
		if err != nil {
			return err
		}
		overwrite[f.Path] = ok
		if ok {
			if err := backup(full); err != nil {
				return err
			}
		} else {
			in.UI.Warn("keeping " + f.Path + " as is; run tm reconfigure again to apply the change later")
		}
	}
	if err := in.installCrowdSecPackage(ctx, a); err != nil {
		return err
	}
	in.UI.Step("Writing configuration")
	for i := range out.Files {
		if out.Files[i].Path == ".env" || strings.HasSuffix(out.Files[i].Path, "/env") {
			existing, _ := host.ReadFile(resolvePath(st.Dir, out.Files[i].Path))
			out.Files[i].Content = mergeEnv(string(existing), out.Files[i].Content)
		}
	}
	prev := st.Answers
	prevOwned := st.OwnedFiles
	st.Answers = *a
	st.Answers.Secrets = nil
	st.OwnedFiles = map[string]string{}
	if err := in.writeOutput(a, out, st, overwrite); err != nil {
		return err
	}
	for path, keep := range overwrite {
		if keep {
			continue
		}
		if h, ok := prevOwned[path]; ok {
			st.OwnedFiles[path] = h
		}
	}
	st.UpdatedAt = time.Now().UTC()
	st.TMVersion = in.Version
	st.Adopted = false
	if err := st.Save(); err != nil {
		return err
	}
	switch st.Mode {
	case answers.ModeFullNative:
		if !host.UserExists(nativeUser) {
			if err := host.AddSystemUser(ctx, nativeUser); err != nil {
				return err
			}
		}
		for _, dir := range []string{answers.NativeTraefikConfigDir, answers.NativeTraefikStateDir, answers.NativeTraefikLogDir} {
			if err := host.Chown(dir, nativeUser+":", true); err != nil {
				return err
			}
		}
		if a.Mounts.StaticConfig {
			_ = host.MkdirAll(filepath.Dir(a.Restart.SignalFile), 0o755)
			_ = host.Chown(filepath.Dir(a.Restart.SignalFile), nativeUser+":", true)
		}
		if err := host.Systemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
		if a.Mounts.StaticConfig {
			if err := host.Systemctl(ctx, "enable", "--now", restartPathUnit); err != nil {
				return err
			}
		} else if prev.Mounts.StaticConfig {
			_ = host.Systemctl(ctx, "disable", "--now", restartPathUnit)
			_ = host.Remove("/etc/systemd/system/"+restartPathUnit, false)
			_ = host.Remove("/etc/systemd/system/traefik-restart.service", false)
		}
		in.registerCrowdSecNative(ctx, a)
		in.reloadCrowdSec(ctx, a)
		if err := host.Systemctl(ctx, "restart", traefikUnit); err != nil {
			return err
		}
		if err := host.Systemctl(ctx, "restart", nativeUnit); err != nil {
			return err
		}
		in.UI.OK("traefik and traefik-manager restarted")
	case answers.ModeTMNative:
		if a.Native.ServiceUser && !host.UserExists(nativeUser) {
			if err := host.AddSystemUser(ctx, nativeUser); err != nil {
				return err
			}
		}
		if a.Mounts.StaticConfig && a.Restart.Method == answers.RestartPoisonPill {
			_ = host.MkdirAll(filepath.Dir(a.Restart.SignalFile), 0o755)
			if a.Native.ServiceUser {
				_ = host.Chown(filepath.Dir(a.Restart.SignalFile), nativeUser+":", true)
			}
		}
		if err := host.Systemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
		if a.Mounts.StaticConfig && a.Restart.TraefikSystemd {
			if err := host.Systemctl(ctx, "enable", "--now", restartPathUnit); err != nil {
				return err
			}
		} else if prev.Mounts.StaticConfig && prev.Restart.TraefikSystemd {
			_ = host.Systemctl(ctx, "disable", "--now", restartPathUnit)
			_ = host.Remove("/etc/systemd/system/"+restartPathUnit, false)
			_ = host.Remove("/etc/systemd/system/traefik-restart.service", false)
		}
		in.registerCrowdSecNative(ctx, a)
		in.reloadCrowdSec(ctx, a)
		if err := host.Systemctl(ctx, "restart", nativeUnit); err != nil {
			return err
		}
		in.UI.OK("traefik-manager restarted")
	case answers.ModeAgentBinary:
		if err := host.Systemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
		in.registerCrowdSecNative(ctx, a)
		in.reloadCrowdSec(ctx, a)
		if err := host.Systemctl(ctx, "restart", agentUnit); err != nil {
			return err
		}
		in.UI.OK("tma restarted")
	default:
		in.UI.Step("Applying changes")
		args := []string{"up", "-d"}
		if len(foreign) == 0 {
			args = append(args, "--remove-orphans")
		}
		if err := in.compose(ctx, st.Dir, args...); err != nil {
			return err
		}
		in.UI.OK("services updated")
		if !a.Mode.IsAgent() && a.CrowdSec.Mode == answers.CrowdSecInstall && prev.CrowdSec.Mode != answers.CrowdSecInstall && a.CrowdSec.MachineID != "" {
			in.registerCrowdSecMachine(ctx, a.CrowdSec.MachineID, a.Secrets[answers.SecretCrowdSecMachinePassword])
		}
	}
	in.waitHealthy(ctx, st, 30*time.Second)
	return st.Save()
}

func foreignServices(st *state.State, out *render.Output) ([]string, error) {
	if !st.Mode.IsDocker() {
		return nil, nil
	}
	existing, err := host.ReadFile(filepath.Join(st.Dir, "docker-compose.yml"))
	if err != nil {
		return nil, nil
	}
	old, err := state.ServiceNames(existing)
	if err != nil {
		return nil, nil
	}
	var rendered []byte
	for _, f := range out.Files {
		if f.Path == "docker-compose.yml" {
			rendered = []byte(f.Content)
		}
	}
	if rendered == nil {
		return nil, nil
	}
	next, err := state.ServiceNames(rendered)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	for _, n := range next {
		keep[n] = true
	}
	var foreign []string
	for _, n := range old {
		if !keep[n] {
			foreign = append(foreign, n)
		}
	}
	return foreign, nil
}

func backup(path string) error {
	data, err := host.ReadFile(path)
	if err != nil {
		return err
	}
	dest := path + ".bak-" + time.Now().Format("20060102-150405")
	if err := host.WriteFile(dest, data, 0o600); err != nil {
		return err
	}
	return nil
}

func mergeEnv(existing, rendered string) string {
	values := map[string]string{}
	var order []string
	add := func(line string) {
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" {
			return
		}
		if _, seen := values[k]; !seen {
			order = append(order, k)
		}
		values[k] = v
	}
	for _, line := range strings.Split(existing, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		add(line)
	}
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		add(line)
	}
	var b strings.Builder
	for _, k := range order {
		b.WriteString(k + "=" + values[k] + "\n")
	}
	return b.String()
}

func (in *Installer) ExistingSecrets(st *state.State) map[string]string {
	secrets := map[string]string{}
	keys := map[string]bool{}
	for _, k := range allSecretKeys(&st.Answers) {
		keys[k] = true
	}
	switch st.Mode {
	case answers.ModeAgentBinary:
		mergeEnvFile(secrets, "/etc/traefik-manager-agent/env", keys)
	case answers.ModeFullNative, answers.ModeTMNative:
		mergeEnvFile(secrets, render.NativeEnvPath, keys)
	default:
		mergeEnvFile(secrets, filepath.Join(st.Dir, ".env"), nil)
	}
	if literal, err := st.LiteralSecrets(); err == nil {
		for k, v := range literal {
			if _, ok := secrets[k]; !ok {
				secrets[k] = v
			}
		}
	}
	return secrets
}

func allSecretKeys(a *answers.Answers) []string {
	seen := map[string]bool{}
	var keys []string
	push := func(k string) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, k := range []string{answers.SecretTMAAPIKey, answers.SecretTraefikAPIPassword, answers.SecretCrowdSecAPIKey, answers.SecretCrowdSecMachinePassword, answers.SecretGitBackupToken} {
		push(k)
	}
	for _, p := range answers.DNSProviders {
		for _, v := range p.Vars {
			if v.Secret {
				push(v.Name)
			}
		}
	}
	for _, k := range a.SecretKeys() {
		push(k)
	}
	return keys
}

func mergeEnvFile(into map[string]string, path string, only map[string]bool) {
	f, err := os.Open(path)
	if err != nil {
		data, rerr := host.ReadFile(path)
		if rerr != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			putEnvLine(into, line, only)
		}
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		putEnvLine(into, sc.Text(), only)
	}
}

func putEnvLine(into map[string]string, line string, only map[string]bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}
	k, v, ok := strings.Cut(line, "=")
	if !ok || k == "" {
		return
	}
	if only != nil && !only[k] {
		return
	}
	into[k] = strings.Trim(v, `"'`)
}
