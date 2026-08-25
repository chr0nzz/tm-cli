package host

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const writeOK = 0x2

func NeedsPrivilege(path string) bool {
	if IsRoot() {
		return false
	}
	p := filepath.Clean(path)
	for {
		if _, err := os.Lstat(p); err == nil {
			return syscall.Access(p, writeOK) != nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return true
		}
		p = parent
	}
}

func owned(path string) bool {
	if IsRoot() {
		return true
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(st.Uid) == geteuid()
}

func octal(mode os.FileMode) string {
	return fmt.Sprintf("%04o", mode.Perm())
}

func WriteFile(path string, data []byte, mode os.FileMode) error {
	if !NeedsPrivilege(path) {
		if err := writeAtomic(path, data, mode); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrPermission) {
			return err
		}
	}
	tmp, err := os.CreateTemp("", "tm-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return Run(Privileged(context.Background(), "install", "-m", octal(mode), "-D", tmp.Name(), path))
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tm-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func MkdirAll(path string, mode os.FileMode) error {
	if !NeedsPrivilege(path) {
		if fi, err := os.Stat(path); err == nil && fi.IsDir() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.Mkdir(path, mode); err != nil {
			return err
		}
		return os.Chmod(path, mode)
	}
	return Run(Privileged(context.Background(), "mkdir", "-p", "-m", octal(mode), path))
}

func Chmod(path string, mode os.FileMode) error {
	if owned(path) {
		return os.Chmod(path, mode)
	}
	return Run(Privileged(context.Background(), "chmod", octal(mode), path))
}

func Chown(path, owner string, recursive bool) error {
	args := []string{}
	if recursive {
		args = append(args, "-R")
	}
	args = append(args, owner, path)
	return Run(Privileged(context.Background(), "chown", args...))
}

func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil || !errors.Is(err, fs.ErrPermission) || IsRoot() {
		return data, err
	}
	cmd := Privileged(context.Background(), "cat", path)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("read %s: %s", path, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func Remove(path string, recursive bool) error {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if !errors.Is(err, fs.ErrPermission) {
			return err
		}
	} else {
		var err error
		if recursive {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err == nil || !errors.Is(err, fs.ErrPermission) || IsRoot() {
			return err
		}
	}
	args := []string{"-f"}
	if recursive {
		args = []string{"-rf"}
	}
	args = append(args, path)
	return Run(Privileged(context.Background(), "rm", args...))
}

func FileOwner(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("cannot read the owner of %s", path)
	}
	uid := strconv.FormatUint(uint64(st.Uid), 10)
	if u, err := user.LookupId(uid); err == nil {
		return u.Username, nil
	}
	return uid, nil
}
