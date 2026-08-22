package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type registryFile struct {
	Installs []string `yaml:"installs"`
}

func registryPath() string {
	if p := os.Getenv("TM_REGISTRY"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			home = "/root"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "tm", "installs.yml")
}

func readRegistry() ([]string, error) {
	p := registryPath()
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var f registryFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, nil
	}
	return dedup(f.Installs), nil
}

func writeRegistry(paths []string) error {
	p := registryPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}
	if paths == nil {
		paths = []string{}
	}
	data, err := yaml.Marshal(registryFile{Installs: paths})
	if err != nil {
		return fmt.Errorf("encode %s: %w", p, err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

func dedup(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func Registry() ([]string, error) {
	paths, err := readRegistry()
	if err != nil {
		return nil, err
	}
	var live []string
	for _, p := range paths {
		if exists(p) {
			live = append(live, p)
		}
	}
	if len(live) != len(paths) {
		if err := writeRegistry(live); err != nil {
			return nil, err
		}
	}
	return live, nil
}

func Register(path string) error {
	path = expandDir(path)
	paths, err := readRegistry()
	if err != nil {
		return err
	}
	for _, p := range paths {
		if p == path {
			return nil
		}
	}
	return writeRegistry(append(paths, path))
}

func Unregister(path string) error {
	path = expandDir(path)
	paths, err := readRegistry()
	if err != nil {
		return err
	}
	keep := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != path {
			keep = append(keep, p)
		}
	}
	if len(keep) == len(paths) {
		return nil
	}
	return writeRegistry(keep)
}
