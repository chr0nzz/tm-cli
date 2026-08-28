package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/installer"
	"github.com/chr0nzz/tm-cli/internal/state"
	"github.com/chr0nzz/tm-cli/internal/ui"
	"github.com/chr0nzz/tm-cli/internal/wizard"
)

func init() { register(newInstallCmd) }

type installFlags struct {
	mode       string
	answers    string
	dump       string
	output     string
	apiKey     string
	traefikURL string
	dryRun     bool
	yes        bool
	channel    string
}

func newInstallCmd() *cobra.Command {
	var f installFlags
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the full stack, Traefik Manager, or an agent",
		Long: `Runs the interactive setup wizard and installs the result.

Modes: ` + strings.Join(modeNames(), ", ") + `, or agent (asks which agent method).
Non-interactive: --answers file.yml (see --dump-answers), secrets from env vars of the same name.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := cmd.Flags().GetString("dir")
			return handleAbort(runInstall(f, dir))
		},
	}
	cmd.Flags().StringVar(&f.mode, "mode", "", "install mode ("+strings.Join(modeNames(), "|")+"|agent)")
	cmd.Flags().StringVar(&f.answers, "answers", "", "answers file, skips the wizard")
	cmd.Flags().StringVar(&f.dump, "dump-answers", "", "write the final answers (no secrets) to this file")
	cmd.Flags().StringVar(&f.output, "output", "", "with --dry-run: directory to render into")
	cmd.Flags().StringVar(&f.apiKey, "api-key", "", "agent API key (TMA_API_KEY)")
	cmd.Flags().StringVar(&f.traefikURL, "traefik-url", "", "agent: Traefik API URL")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "render all files, start nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "assume yes for confirmations and skip the firewall pause")
	cmd.Flags().StringVar(&f.channel, "channel", "", "release channel: stable or beta (beta tracks the next release)")
	_ = cmd.Flags().MarkHidden("channel")
	return cmd
}

func modeNames() []string {
	names := make([]string, len(answers.Modes))
	for i, m := range answers.Modes {
		names[i] = string(m)
	}
	return names
}

func runInstall(f installFlags, dirFlag string) error {
	ctx, cancel := signalContext()
	defer cancel()
	u := ui.New()
	interactive := ui.Interactive()
	if !f.dryRun {
		u.Banner()
	}
	var tty *os.File
	if interactive {
		t, err := ui.OpenTTY()
		if err != nil {
			interactive = false
		} else {
			tty = t
		}
	}
	a, err := initialAnswers(ctx, f, u, tty, interactive)
	if err != nil {
		return err
	}
	if dirFlag != "" && a.Mode.IsDocker() {
		a.Dir = dirFlag
	}
	if f.apiKey != "" {
		a.SetSecret(answers.SecretTMAAPIKey, f.apiKey)
	}
	if f.traefikURL != "" {
		a.Agent.TraefikURL = f.traefikURL
	}
	if f.channel != "" {
		a.Channel = f.channel
	}
	a.LoadSecretsFromEnv()
	a.Finalize()
	inst := newInstaller(u, f.yes)
	if !f.dryRun {
		if err := preflightTools(ctx, u, a, f.yes, interactive); err != nil {
			return err
		}
	}
	if !f.dryRun && f.answers == "" {
		existing, err := existingInstall(a, u)
		if err != nil {
			return err
		}
		if existing != nil {
			proceed, err := handleExisting(ctx, u, inst, existing, a, tty, interactive, f.yes, a.Mode.IsDocker())
			if err != nil || !proceed {
				return err
			}
		}
	}
	if f.answers == "" {
		if !interactive {
			return errors.New("no terminal available: pass --answers <file> for a non-interactive install")
		}
		if err := wizard.Run(ctx, u, a, tty); err != nil {
			return err
		}
	} else if err := a.Validate(); err != nil {
		return err
	}
	if err := requireSecrets(ctx, u, a, tty, interactive); err != nil {
		return err
	}
	if f.dump != "" {
		if err := dumpAnswers(a, f.dump); err != nil {
			return err
		}
		u.OK("answers written to " + f.dump)
	}
	if f.dryRun {
		_, err := inst.Install(ctx, a, installer.Options{DryRun: true, OutputDir: f.output})
		return err
	}
	existing, err := existingInstall(a, u)
	if err != nil {
		return err
	}
	if existing != nil {
		_, err := handleExisting(ctx, u, inst, existing, a, tty, interactive, f.yes, false)
		return err
	}
	if a.Deployment == answers.DeploymentExternal && a.Mode.HasTraefik() {
		wizard.FirewallNotice(u, a, !f.yes && interactive, tty)
	}
	if err := sudoPreflight(ctx, u, a); err != nil {
		return err
	}
	st, err := inst.Install(ctx, a, installer.Options{})
	if err != nil {
		return err
	}
	inst.Summary(a)
	_ = st
	return nil
}

func initialAnswers(ctx context.Context, f installFlags, u *ui.UI, tty *os.File, interactive bool) (*answers.Answers, error) {
	if f.answers != "" {
		a, err := answers.Load(f.answers)
		if err != nil {
			return nil, err
		}
		if f.mode != "" && f.mode != string(a.Mode) && !(f.mode == "agent" && a.Mode.IsAgent()) {
			return nil, fmt.Errorf("--mode %s conflicts with mode %s in %s", f.mode, a.Mode, f.answers)
		}
		return a, nil
	}
	mode := f.mode
	if mode == "" && os.Getenv("TMA_INSTALL") == "1" {
		mode = "agent"
	}
	if mode == "" {
		if !interactive {
			return nil, errors.New("no terminal available: pass --mode and --answers for a non-interactive install")
		}
		m, err := wizard.PickMode(ctx, u, tty)
		if err != nil {
			return nil, err
		}
		return answers.Defaults(m), nil
	}
	if mode == "agent" {
		if !interactive {
			return nil, errors.New("--mode agent needs a terminal to pick the agent method; use --mode agent-docker, agent-docker-traefik, or agent-binary")
		}
		m, err := wizard.PickAgentMethod(ctx, u, tty)
		if err != nil {
			return nil, err
		}
		return answers.Defaults(m), nil
	}
	m := answers.Mode(mode)
	if !m.Valid() {
		return nil, fmt.Errorf("unknown mode %q (valid: %s, agent)", mode, strings.Join(modeNames(), ", "))
	}
	return answers.Defaults(m), nil
}

func requireSecrets(ctx context.Context, u *ui.UI, a *answers.Answers, tty *os.File, prompt bool) error {
	missing := a.MissingSecrets()
	if len(missing) == 0 {
		return nil
	}
	if prompt && tty != nil {
		return wizard.AskSecrets(ctx, u, a, missing, tty)
	}
	return fmt.Errorf("missing secrets: set env vars %s (or a secrets: map in the answers file)", strings.Join(missing, ", "))
}

func dumpAnswers(a *answers.Answers, path string) error {
	data, err := a.Dump()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func preflightTools(ctx context.Context, u *ui.UI, a *answers.Answers, yes, interactive bool) error {
	switch {
	case a.Mode.IsDocker():
		u.Step("Checking Docker")
		switch {
		case host.DockerInstalled() && host.DockerRunning():
			u.OK("Docker found and running")
		case host.DockerInstalled():
			u.Warn("Docker is installed but this session cannot reach the daemon.")
			if err := useDockerViaSudo(u); err != nil {
				return err
			}
		default:
			u.Warn("Docker is not installed or the daemon is not running.")
			ok := yes
			if !yes {
				if !interactive {
					return errors.New("docker is required: install it or run with --yes to let tm install it")
				}
				v, err := confirm("Install Docker now?", true)
				if err != nil {
					return err
				}
				ok = v
			}
			if !ok {
				return errors.New("docker is required")
			}
			u.Step("Installing Docker")
			if err := host.InstallDocker(ctx, u.Info); err != nil {
				return err
			}
			u.OK("Docker installed")
			if !host.DockerRunning() {
				if err := useDockerViaSudo(u); err != nil {
					return err
				}
			}
		}
		cmd, err := host.ComposeCommand()
		if err != nil {
			return err
		}
		u.OK(strings.Join(cmd, " ") + " found")
	case a.Mode == answers.ModeTMNative:
		u.Step("Checking dependencies")
		if !host.HasCommand("git") {
			return errors.New("git is required. Install it and re-run")
		}
		u.OK("git found")
		v, ok := host.PythonVersion()
		if !ok {
			if v == "" {
				return errors.New("python 3.11 or newer is required. Install it and re-run")
			}
			return fmt.Errorf("python 3.11 or newer is required. Found: %s", v)
		}
		u.OK("Python " + v + " found")
		if !host.HasCommand("systemctl") {
			return errors.New("systemd is required for the Linux service install")
		}
		u.OK("systemd found")
	case a.Mode == answers.ModeAgentBinary:
		u.Step("Checking dependencies")
		if !host.HasCommand("systemctl") {
			return errors.New("systemd is required for the binary agent install")
		}
		u.OK("systemd found")
		if _, err := host.Arch(); err != nil {
			return err
		}
	}
	return nil
}

func useDockerViaSudo(u *ui.UI) error {
	if !host.DockerRunsWithSudo() {
		if host.IsRoot() || host.InDockerGroup() {
			return errors.New("the docker daemon is not reachable: sudo systemctl start docker")
		}
		return errors.New("cannot reach the docker daemon: start it with sudo systemctl start docker, or add yourself to the docker group with sudo usermod -aG docker " + host.CurrentUser() + " and log back in")
	}
	host.UseDockerSudo(true)
	if host.DockerGroupPending() {
		u.Info("you are in the docker group but this shell started before that, so tm will use sudo for docker; log out and back in to stop needing it")
	} else {
		u.Info(host.CurrentUser() + " is not in the docker group, so tm will use sudo for docker")
	}
	return nil
}

func sudoPreflight(ctx context.Context, u *ui.UI, a *answers.Answers) error {
	var reasons []string
	switch a.Mode {
	case answers.ModeTMNative:
		reasons = []string{"systemd unit", "service user", a.Native.InstallDir, a.Native.DataDir}
	case answers.ModeAgentBinary:
		reasons = []string{answers.AgentBinaryPath, "systemd unit", "/etc/traefik-manager-agent"}
	default:
		if host.NeedsPrivilege(a.Dir) {
			reasons = []string{"install directory " + a.Dir}
		}
	}
	if len(reasons) == 0 {
		return nil
	}
	u.Blank()
	return host.SudoPreflight(ctx, reasons, u.Info)
}

func existingInstall(a *answers.Answers, u *ui.UI) (*state.State, error) {
	p := state.PathFor(a)
	if host.Exists(p) {
		return state.Load(p)
	}
	switch a.Mode {
	case answers.ModeTMNative:
		if host.Exists("/etc/systemd/system/traefik-manager.service") {
			st, _, err := state.AdoptSystemd()
			if err == nil && st.Mode == answers.ModeTMNative {
				return st, nil
			}
		}
	case answers.ModeAgentBinary:
		if host.Exists("/etc/systemd/system/tma.service") {
			st, _, err := state.AdoptSystemd()
			if err == nil && st.Mode == answers.ModeAgentBinary {
				return st, nil
			}
		}
	default:
		if host.Exists(filepath.Join(a.Dir, "docker-compose.yml")) {
			st, _, err := state.Adopt(a.Dir)
			if err == nil {
				return st, nil
			}
			if !errors.Is(err, state.ErrNotAdoptable) {
				return nil, err
			}
			u.Warn(filepath.Join(a.Dir, "docker-compose.yml") + " exists and is not a Traefik Manager stack")
		}
	}
	return nil, nil
}

func handleExisting(ctx context.Context, u *ui.UI, inst *installer.Installer, st *state.State, a *answers.Answers, tty *os.File, interactive, yes, offerAnother bool) (bool, error) {
	where := installLocation(st)
	u.Blank()
	u.Warn(fmt.Sprintf("an existing %s install was found at %s", st.Mode, where))
	if !interactive {
		return false, errors.New("refusing to overwrite it: use tm update, tm reconfigure, or a different --dir")
	}
	choice := "update"
	opts := []huh.Option[string]{
		huh.NewOption("Update it (pull latest and restart)", "update"),
		huh.NewOption("Reconfigure it (edit settings, regenerate files)", "reconfigure"),
	}
	if offerAnother {
		opts = append(opts, huh.NewOption("Install another copy in a different directory", "another"))
	}
	opts = append(opts, huh.NewOption("Cancel", "cancel"))
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("What would you like to do?").Options(opts...).Value(&choice),
	)).WithTheme(ui.Theme()).WithInput(tty).WithOutput(os.Stdout).WithShowHelp(false)
	if err := form.RunWithContext(ctx); err != nil {
		return false, err
	}
	switch choice {
	case "update":
		if err := inst.Update(ctx, st); err != nil {
			return false, err
		}
		return false, inst.Status(ctx, st)
	case "reconfigure":
		return false, runReconfigure(ctx, u, inst, st, "", tty)
	case "another":
		return true, nil
	default:
		return false, Exit(0)
	}
}
