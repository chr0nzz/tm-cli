package installer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/host"
	"github.com/chr0nzz/tm-cli/internal/render"
)

const (
	crowdsecUnit       = "crowdsec"
	crowdsecPackage    = "crowdsec"
	crowdsecRepoScript = "https://install.crowdsec.net"
	crowdsecCollection = "crowdsecurity/traefik"
)

func crowdsecBouncerName(mode answers.Mode) string {
	if mode.IsAgent() {
		return "tma"
	}
	return answers.CrowdSecMachineID
}

func cscliBouncerAddArgs(name, key string) []string {
	return []string{"cscli", "bouncers", "add", name, "--key", key}
}

func cscliBouncerDeleteArgs(name string) []string {
	return []string{"cscli", "bouncers", "delete", name}
}

func cscliMachineAddArgs(id, password string) []string {
	return []string{"cscli", "machines", "add", id, "--password", password, "--force"}
}

func cscliCollectionArgs(name string) []string {
	return []string{"cscli", "collections", "install", name}
}

func nativeCrowdSecNeeded(a *answers.Answers) bool {
	return a.Mode.IsSystemd() && a.CrowdSec.Mode == answers.CrowdSecInstall
}

func (in *Installer) installCrowdSecPackage(ctx context.Context, a *answers.Answers) error {
	if !nativeCrowdSecNeeded(a) {
		return nil
	}
	if host.HasCommand("cscli") {
		in.UI.OK("CrowdSec is already installed on this server")
	} else {
		in.UI.Step("Installing CrowdSec")
		if host.HasCommand("pacman") {
			in.UI.Info("the CrowdSec repository script does not cover Arch, installing the distribution package instead")
		} else if err := host.RunRemoteScript(ctx, crowdsecRepoScript); err != nil {
			return fmt.Errorf("add the CrowdSec package repository: %w", err)
		}
		if err := host.InstallPackage(ctx, crowdsecPackage); err != nil {
			return fmt.Errorf("install the crowdsec package: %w", err)
		}
		in.UI.OK("CrowdSec package installed")
	}
	if err := host.Systemctl(ctx, "enable", "--now", crowdsecUnit); err != nil {
		return err
	}
	if !in.waitCrowdSecReady(ctx, 60*time.Second) {
		return fmt.Errorf("crowdsec started but its local API did not answer cscli lapi status within 60s")
	}
	in.UI.OK("CrowdSec service enabled and running")
	return nil
}

func (in *Installer) waitCrowdSecReady(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := host.Output(host.Privileged(ctx, "cscli", "lapi", "status")); err == nil {
			return true
		}
		if time.Now().After(deadline) || !sleep(ctx, 2*time.Second) {
			return false
		}
	}
}

func (in *Installer) registerCrowdSecNative(ctx context.Context, a *answers.Answers) {
	if !nativeCrowdSecNeeded(a) {
		return
	}
	in.UI.Step("Registering Traefik Manager with CrowdSec")
	collection := cscliCollectionArgs(crowdsecCollection)
	if _, err := host.Output(host.Privileged(ctx, collection[0], collection[1:]...)); err != nil {
		in.UI.Warn("could not install the " + crowdsecCollection + " collection, CrowdSec will not parse Traefik logs until it is")
		in.UI.Code("sudo " + strings.Join(collection, " "))
	} else {
		in.UI.OK("collection " + crowdsecCollection + " installed")
	}
	name := crowdsecBouncerName(a.Mode)
	key := a.Secrets[answers.SecretCrowdSecAPIKey]
	del := cscliBouncerDeleteArgs(name)
	_, _ = host.Output(host.Privileged(ctx, del[0], del[1:]...))
	add := cscliBouncerAddArgs(name, key)
	if _, err := host.Output(host.Privileged(ctx, add[0], add[1:]...)); err != nil {
		in.UI.Warn("could not register the CrowdSec bouncer. Run it manually:")
		in.UI.Code("sudo " + strings.Join(add, " "))
	} else {
		in.UI.OK("CrowdSec bouncer '" + name + "' registered (enables the Decisions view)")
	}
	if a.CrowdSec.MachineID == "" {
		return
	}
	machine := cscliMachineAddArgs(a.CrowdSec.MachineID, a.Secrets[answers.SecretCrowdSecMachinePassword])
	if _, err := host.Output(host.Privileged(ctx, machine[0], machine[1:]...)); err != nil {
		in.UI.Warn("could not register the CrowdSec machine. Run it manually:")
		in.UI.Code("sudo " + strings.Join(machine, " "))
		return
	}
	in.UI.OK("CrowdSec machine '" + a.CrowdSec.MachineID + "' registered (enables the Alerts view)")
}

func (in *Installer) reloadCrowdSec(ctx context.Context, a *answers.Answers) {
	if !nativeCrowdSecNeeded(a) {
		return
	}
	if err := host.Systemctl(ctx, "reload", crowdsecUnit); err == nil {
		in.UI.OK("CrowdSec reloaded, reading " + a.Mounts.AccessLogPath)
		return
	}
	if err := host.Systemctl(ctx, "restart", crowdsecUnit); err != nil {
		in.UI.Warn("could not reload crowdsec after writing " + render.CrowdSecAcquisPath + ": " + err.Error())
		return
	}
	in.UI.OK("CrowdSec restarted, reading " + a.Mounts.AccessLogPath)
}
