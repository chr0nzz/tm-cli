package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/chr0nzz/traefik-stack/internal/answers"
	"github.com/chr0nzz/traefik-stack/internal/host"
)

const Version = 1

const AdoptedVersion = "adopted"

var (
	ErrNotFound     = errors.New("no install found")
	ErrAmbiguous    = errors.New("several installs found")
	ErrNotAdoptable = errors.New("docker-compose.yml has no traefik-manager or traefik-manager-agent service")
)

var (
	writeFile = host.WriteFile
	readFile  = host.ReadFile
	mkdirAll  = host.MkdirAll
	exists    = host.Exists
)

var (
	nativeStatePath      = "/etc/traefik-manager/tm-state.yml"
	agentBinaryStatePath = "/etc/traefik-manager-agent/tm-state.yml"
)

type State struct {
	Version     int               `yaml:"version"`
	Mode        answers.Mode      `yaml:"mode"`
	TMVersion   string            `yaml:"tm_version"`
	InstalledAt time.Time         `yaml:"installed_at"`
	UpdatedAt   time.Time         `yaml:"updated_at"`
	Adopted     bool              `yaml:"adopted"`
	ComposeCmd  string            `yaml:"compose_cmd"`
	Dir         string            `yaml:"dir"`
	OwnedFiles  map[string]string `yaml:"owned_files"`
	Answers     answers.Answers   `yaml:"answers"`
	Path        string            `yaml:"-"`
}

func PathFor(a *answers.Answers) string {
	switch a.Mode {
	case answers.ModeTMNative:
		return nativeStatePath
	case answers.ModeAgentBinary:
		return agentBinaryStatePath
	}
	return filepath.Join(a.Dir, ".tm", "state.yml")
}

func dirOf(a *answers.Answers) string {
	if a.Dir == "" && a.Mode == answers.ModeTMNative {
		return a.Native.InstallDir
	}
	return a.Dir
}

func New(a *answers.Answers, tmVersion, composeCmd string) *State {
	now := time.Now().UTC().Truncate(time.Second)
	return &State{
		Version:     Version,
		Mode:        a.Mode,
		TMVersion:   tmVersion,
		InstalledAt: now,
		UpdatedAt:   now,
		ComposeCmd:  composeCmd,
		Dir:         dirOf(a),
		OwnedFiles:  map[string]string{},
		Answers:     *a.Clone(),
		Path:        PathFor(a),
	}
}

func Load(path string) (*State, error) {
	if !exists(path) {
		return nil, fmt.Errorf("%w: %s does not exist", ErrNotFound, path)
	}
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.Version != Version {
		return nil, fmt.Errorf("%s is state version %d, this tm supports version %d: update tm", path, s.Version, Version)
	}
	if !s.Mode.Valid() {
		return nil, fmt.Errorf("%s has unknown mode %q", path, s.Mode)
	}
	if s.Answers.Mode == "" {
		return nil, fmt.Errorf("%s has no answers block", path)
	}
	if s.Answers.Mode != s.Mode {
		return nil, fmt.Errorf("%s: answers mode %q does not match state mode %q", path, s.Answers.Mode, s.Mode)
	}
	if s.OwnedFiles == nil {
		s.OwnedFiles = map[string]string{}
	}
	s.Path = path
	return &s, nil
}

func (s *State) Save() error {
	if s.Path == "" {
		a := s.Answers
		if a.Dir == "" {
			a.Dir = s.Dir
		}
		s.Path = PathFor(&a)
	}
	s.Version = Version
	s.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if s.InstalledAt.IsZero() {
		s.InstalledAt = s.UpdatedAt
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	dir := filepath.Dir(s.Path)
	if err := mkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := writeFile(s.Path, data, fileMode(s.Path)); err != nil {
		return fmt.Errorf("write %s: %w", s.Path, err)
	}
	return Register(s.Path)
}

func fileMode(path string) os.FileMode {
	return 0o644
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *State) AbsPath(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(s.Dir, name)
}

func (s *State) Own(name string, data []byte) {
	if s.OwnedFiles == nil {
		s.OwnedFiles = map[string]string{}
	}
	s.OwnedFiles[name] = Hash(data)
}

func (s *State) Modified() ([]string, error) {
	names := make([]string, 0, len(s.OwnedFiles))
	for n := range s.OwnedFiles {
		names = append(names, n)
	}
	sort.Strings(names)
	var changed []string
	for _, n := range names {
		p := s.AbsPath(n)
		if !exists(p) {
			changed = append(changed, n)
			continue
		}
		data, err := readFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		if Hash(data) != s.OwnedFiles[n] {
			changed = append(changed, n)
		}
	}
	return changed, nil
}

func expandDir(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

func modTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}
