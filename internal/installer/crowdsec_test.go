package installer

import (
	"io"
	"net"
	"strings"
	"testing"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"github.com/chr0nzz/tm-cli/internal/ui"
)

func TestCheckLAPIPortFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := strings.Split(ln.Addr().String(), ":")
	port := busy[len(busy)-1]

	a := answers.Defaults(answers.ModeAgentBinary)
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.CrowdSec.LAPIPort = port
	a.SetSecret(answers.SecretTMAAPIKey, "k")
	a.Finalize()
	in := &Installer{UI: ui.NewPlain(io.Discard)}
	err = in.checkLAPIPortFree(a)
	if err == nil || !strings.Contains(err.Error(), "already listening") {
		t.Fatalf("an occupied LAPI port must be refused before the package is installed, got %v", err)
	}
	if !strings.Contains(err.Error(), "crowdsec.lapi_port") {
		t.Errorf("the error must name the way out: %v", err)
	}

	ln.Close()
	if err := in.checkLAPIPortFree(a); err != nil {
		t.Fatalf("a free port must pass: %v", err)
	}

	b := answers.Defaults(answers.ModeFull)
	b.CrowdSec.Mode = answers.CrowdSecInstall
	b.Domain = "example.com"
	b.TLS.Email = "me@example.com"
	b.Finalize()
	if err := in.checkLAPIPortFree(b); err != nil {
		t.Fatalf("docker installs reach crowdsec on their own network and must not be port checked: %v", err)
	}
}

func TestNativeLAPIPortThreadsThrough(t *testing.T) {
	a := answers.Defaults(answers.ModeTMNative)
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.CrowdSec.LAPIPort = "8099"
	a.Finalize()
	if got := a.NativeLAPIURL(); got != "http://127.0.0.1:8099" {
		t.Fatalf("lapi url = %q", got)
	}
	if a.CrowdSec.LAPIURL != "http://127.0.0.1:8099" {
		t.Fatalf("finalize must apply the port to the lapi url, got %q", a.CrowdSec.LAPIURL)
	}
	a.Native.Port = "8099"
	if err := a.Validate(); err == nil {
		t.Fatal("a collision with the chosen lapi port must still be caught")
	}
}
