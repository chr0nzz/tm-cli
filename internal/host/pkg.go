package host

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

func RunRemoteScript(ctx context.Context, url string) error {
	script, err := fetch(ctx, url)
	if err != nil {
		return err
	}
	sh := Privileged(ctx, "sh")
	sh.Stdin = bytes.NewReader(script)
	return Run(sh)
}

var packageManagers = []string{"apt-get", "dnf", "yum", "zypper", "pacman"}

func PackageManager() (string, error) {
	for _, pm := range packageManagers {
		if HasCommand(pm) {
			return pm, nil
		}
	}
	return "", fmt.Errorf("no supported package manager found (looked for %s)", strings.Join(packageManagers, ", "))
}

func InstallPackageArgs(pm, pkg string) []string {
	switch pm {
	case "apt-get":
		return []string{"apt-get", "install", "-y", pkg}
	case "dnf", "yum":
		return []string{pm, "install", "-y", pkg}
	case "zypper":
		return []string{"zypper", "--non-interactive", "install", pkg}
	case "pacman":
		return []string{"pacman", "-S", "--needed", "--noconfirm", pkg}
	}
	return nil
}

func InstallPackage(ctx context.Context, pkg string) error {
	pm, err := PackageManager()
	if err != nil {
		return err
	}
	args := InstallPackageArgs(pm, pkg)
	if len(args) == 0 {
		return fmt.Errorf("no install command for package manager %s", pm)
	}
	if pm == "apt-get" {
		if err := Run(Privileged(ctx, "apt-get", "update", "-qq")); err != nil {
			return err
		}
	}
	return Run(Privileged(ctx, args[0], args[1:]...))
}
