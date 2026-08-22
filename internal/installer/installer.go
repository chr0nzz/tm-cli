package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/host"
	"github.com/chr0nzz/traefik-stack/internal/render"
	"github.com/chr0nzz/traefik-stack/internal/state"
	"github.com/chr0nzz/traefik-stack/internal/ui"
)

type Installer struct {
	UI              *ui.UI
	Version         string
	Compose         []string
	Yes             bool
	AllowUnverified bool
	Confirm         func(prompt string, def bool) (bool, error)
	TempPassword    string
}

func New(u *ui.UI, version string) *Installer {
	return &Installer{UI: u, Version: version}
}

type Options struct {
	DryRun    bool
	OutputDir string
}

var ErrAborted = errors.New("aborted")

const traefikStaticRel = "traefik/traefik.yml"

func (in *Installer) Install(ctx context.Context, a *answers.Answers, opts Options) (*state.State, error) {
	if err := a.GenerateSecrets(); err != nil {
		return nil, err
	}
	out, err := render.Render(render.Input{Answers: a, User: host.CurrentUser()})
	if err != nil {
		return nil, err
	}
	if opts.DryRun {
		return nil, in.dryRun(a, out, opts.OutputDir)
	}
	composeCmd := ""
	if a.Mode.IsDocker() {
		if len(in.Compose) == 0 {
			cmd, err := host.ComposeCommand()
			if err != nil {
				return nil, err
			}
			in.Compose = cmd
		}
		composeCmd = strings.Join(in.Compose, " ")
	}
	st := state.New(a, in.Version, composeCmd)
	switch a.Mode {
	case answers.ModeTMNative:
		if err := in.installNative(ctx, a, out, st); err != nil {
			return nil, err
		}
	case answers.ModeAgentBinary:
		if err := in.installAgentBinary(ctx, a, out, st); err != nil {
			return nil, err
		}
	default:
		if err := in.installDocker(ctx, a, out, st); err != nil {
			return nil, err
		}
	}
	st.InstalledAt = time.Now().UTC()
	if err := st.Save(); err != nil {
		in.UI.Warn("could not save tm state: " + err.Error())
	}
	return st, nil
}

func (in *Installer) writeOutput(a *answers.Answers, out *render.Output, st *state.State, overwrite map[string]bool) error {
	base := a.Dir
	for _, d := range out.Dirs {
		p := resolvePath(base, d)
		if err := host.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}
	for _, f := range out.Files {
		p := resolvePath(base, f.Path)
		if f.CreateOnly && host.Exists(p) {
			continue
		}
		if overwrite != nil && !f.CreateOnly {
			if keep, ok := overwrite[f.Path]; ok && !keep {
				continue
			}
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := host.WriteFile(p, []byte(f.Content), mode); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		if !f.CreateOnly {
			if st.OwnedFiles == nil {
				st.OwnedFiles = map[string]string{}
			}
			st.OwnedFiles[f.Path] = state.Hash([]byte(f.Content))
		}
		in.UI.OK(displayPath(base, f.Path) + " written")
	}
	return nil
}

func resolvePath(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func displayPath(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return p
}

func (in *Installer) dryRun(a *answers.Answers, out *render.Output, outputDir string) error {
	if outputDir == "" {
		if a.Mode.IsDocker() {
			outputDir = a.Dir
		} else {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			outputDir = wd
		}
	}
	in.UI.Step("Dry run - rendering to " + outputDir)
	for _, d := range out.Dirs {
		p := filepath.Join(outputDir, strings.TrimPrefix(d, "/"))
		if filepath.IsAbs(d) || a.Mode.IsDocker() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		}
	}
	for _, f := range out.Files {
		p := filepath.Join(outputDir, strings.TrimPrefix(f.Path, "/"))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(p, []byte(f.Content), mode); err != nil {
			return err
		}
		if err := os.Chmod(p, mode); err != nil {
			return err
		}
		in.UI.OK(strings.TrimPrefix(f.Path, "/"))
	}
	in.UI.Blank()
	in.UI.Info("nothing was started, no services were touched")
	return nil
}

func (in *Installer) confirm(prompt string, def bool) (bool, error) {
	if in.Yes {
		return true, nil
	}
	if in.Confirm == nil {
		return def, nil
	}
	return in.Confirm(prompt, def)
}
