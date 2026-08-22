package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"
	"time"
)

const dockerPackages = "docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"

func DockerInstalled() bool { return HasCommand("docker") }

var dockerSudo bool

func UseDockerSudo(v bool) { dockerSudo = v }

func DockerSudo() bool { return dockerSudo }

func DockerCommand(ctx context.Context, args ...string) *exec.Cmd {
	if dockerSudo {
		return Privileged(ctx, "docker", args...)
	}
	return Command(ctx, "docker", args...)
}

func DockerRunsWithSudo() bool {
	if !DockerInstalled() || IsRoot() || !HasCommand("sudo") {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := Output(Privileged(ctx, "docker", "info"))
	return err == nil
}

func DockerRunning() bool {
	if !DockerInstalled() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := Output(DockerCommand(ctx, "info"))
	return err == nil
}

func InDockerGroup() bool {
	g, err := user.LookupGroup("docker")
	if err != nil {
		return false
	}
	gids, err := syscall.Getgroups()
	if err != nil {
		return false
	}
	for _, gid := range gids {
		if fmt.Sprint(gid) == g.Gid {
			return true
		}
	}
	return false
}

func DockerGroupPending() bool {
	if InDockerGroup() {
		return false
	}
	g, err := user.LookupGroup("docker")
	if err != nil {
		return false
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, id := range ids {
		if id == g.Gid {
			return true
		}
	}
	return false
}

func InstallDocker(ctx context.Context, log func(string)) error {
	if log == nil {
		log = func(string) {}
	}
	id, like := OSInfo()
	if usesPacman(id, like) {
		log("detected Arch, installing with pacman: the Docker install script does not support it")
		if err := installDockerPacman(ctx); err != nil {
			return err
		}
	} else {
		log("installing with the official Docker script from get.docker.com")
		err := installDockerScript(ctx)
		if errors.Is(err, errUnsupportedDistro) {
			switch {
			case HasCommand("dnf"):
				log("the Docker script does not know " + id + ", using the Docker rpm repository instead")
				err = installDockerDnf(ctx, id, like)
			case HasCommand("pacman"):
				log("the Docker script does not know " + id + ", using pacman instead")
				err = installDockerPacman(ctx)
			default:
				return fmt.Errorf("%w: install docker yourself, then re-run tm", err)
			}
		}
		if err != nil {
			return err
		}
	}
	if err := Run(Privileged(ctx, "systemctl", "enable", "--now", "docker")); err != nil {
		return err
	}
	_ = AddUserToGroup(ctx, CurrentUser(), "docker")
	return nil
}

func usesPacman(id, like string) bool {
	return id == "arch" || strings.Contains(like, "arch")
}

func dnfRepoURL(id, like string) string {
	if id == "fedora" || (strings.Contains(like, "fedora") && !strings.Contains(like, "rhel")) {
		return "https://download.docker.com/linux/fedora/docker-ce.repo"
	}
	return "https://download.docker.com/linux/centos/docker-ce.repo"
}

func installDockerDnf(ctx context.Context, id, like string) error {
	repo := dnfRepoURL(id, like)
	if HasCommand("dnf5") {
		if err := Run(Privileged(ctx, "dnf5", "config-manager", "addrepo", "--overwrite", "--save-filename=docker-ce.repo", "--from-repofile="+repo)); err != nil {
			return err
		}
	} else {
		if err := Run(Privileged(ctx, "dnf", "-y", "install", "dnf-plugins-core")); err != nil {
			return err
		}
		if err := Run(Privileged(ctx, "dnf", "config-manager", "--add-repo", repo)); err != nil {
			return err
		}
	}
	return Run(Privileged(ctx, "dnf", append([]string{"install", "-y"}, strings.Fields(dockerPackages)...)...))
}

func installDockerPacman(ctx context.Context) error {
	if err := Run(Privileged(ctx, "pacman", "-S", "--needed", "--noconfirm", "docker", "docker-compose")); err != nil {
		return fmt.Errorf("%w: if pacman could not find the packages the database is stale, run sudo pacman -Syu and try again", err)
	}
	return nil
}

var errUnsupportedDistro = errors.New("the Docker install script does not support this distribution")

func installDockerScript(ctx context.Context) error {
	script, err := fetch(ctx, "https://get.docker.com")
	if err != nil {
		return err
	}
	var out bytes.Buffer
	sh := Privileged(ctx, "sh")
	sh.Stdin = bytes.NewReader(script)
	sh.Stdout = io.MultiWriter(os.Stdout, &out)
	sh.Stderr = io.MultiWriter(os.Stderr, &out)
	if err := Run(sh); err != nil {
		if strings.Contains(out.String(), "Unsupported distribution") {
			return errUnsupportedDistro
		}
		return err
	}
	return nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	return data, nil
}

func ComposeCommand() ([]string, error) {
	if !DockerInstalled() {
		return nil, errors.New("docker compose is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prefix := []string{}
	if dockerSudo {
		prefix = []string{"sudo"}
	}
	if _, err := Output(DockerCommand(ctx, "compose", "version")); err == nil {
		return append(prefix, "docker", "compose"), nil
	}
	if HasCommand("docker-compose") {
		return append(prefix, "docker-compose"), nil
	}
	return nil, errors.New("docker compose is required")
}
