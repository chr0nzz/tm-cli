package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

type Candidate struct {
	Path string
	Mode answers.Mode
	Dir  string
	Err  error
}

func (c Candidate) String() string {
	if c.Err != nil {
		return fmt.Sprintf("%s (unreadable: %v)", c.Path, c.Err)
	}
	if c.Dir != "" {
		return fmt.Sprintf("%-20s %s  (%s)", c.Mode, c.Dir, c.Path)
	}
	return fmt.Sprintf("%-20s %s", c.Mode, c.Path)
}

type AmbiguousError struct {
	Candidates []Candidate
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	b.WriteString(ErrAmbiguous.Error())
	b.WriteString(", pass --dir or set TM_DIR:")
	for _, c := range e.Candidates {
		b.WriteString("\n  ")
		b.WriteString(c.String())
	}
	return b.String()
}

func (e *AmbiguousError) Unwrap() error { return ErrAmbiguous }

var errNothingHere = errors.New("nothing here")

func Resolve(dirFlag string) (*State, error) {
	dir := dirFlag
	if dir == "" {
		dir = os.Getenv("TM_DIR")
	}
	if dir != "" {
		abs := expandDir(dir)
		if !exists(abs) {
			return nil, fmt.Errorf("%w: %s does not exist", ErrNotFound, abs)
		}
		st, err := resolveDir(abs)
		if errors.Is(err, errNothingHere) {
			return nil, fmt.Errorf("%w in %s: expected .tm/state.yml or a docker-compose.yml with traefik-manager or traefik-manager-agent", ErrNotFound, abs)
		}
		return st, err
	}
	if cwd, err := os.Getwd(); err == nil {
		st, err := resolveDir(cwd)
		if err == nil {
			return st, nil
		}
		if !errors.Is(err, errNothingHere) {
			return nil, err
		}
	}
	paths, err := Registry()
	if err != nil {
		return nil, err
	}
	switch len(paths) {
	case 0:
		if st, _, err := AdoptSystemd(); err == nil {
			return st, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: run tm install or pass --dir", ErrNotFound)
	case 1:
		return Load(paths[0])
	}
	ae := &AmbiguousError{}
	for _, p := range paths {
		c := Candidate{Path: p}
		if st, err := Load(p); err != nil {
			c.Err = err
		} else {
			c.Mode, c.Dir = st.Mode, st.Dir
		}
		ae.Candidates = append(ae.Candidates, c)
	}
	return nil, ae
}

func resolveDir(dir string) (*State, error) {
	p := filepath.Join(dir, ".tm", "state.yml")
	if exists(p) {
		return Load(p)
	}
	if exists(filepath.Join(dir, composeFileName)) {
		st, _, err := Adopt(dir)
		if err == nil {
			return st, nil
		}
		if !errors.Is(err, ErrNotAdoptable) {
			return nil, err
		}
	}
	return nil, errNothingHere
}
