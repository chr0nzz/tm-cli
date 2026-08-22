package host

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeEnv(t *testing.T, euid int, commands map[string]string) {
	t.Helper()
	origLook, origEuid := lookPath, geteuid
	lookPath = func(name string) (string, error) {
		if p, ok := commands[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	geteuid = func() int { return euid }
	t.Cleanup(func() {
		lookPath, geteuid = origLook, origEuid
	})
}

func TestMapArch(t *testing.T) {
	cases := map[string]string{
		"x86_64":  "amd64",
		"aarch64": "arm64",
		"armv7l":  "armv7",
	}
	for in, want := range cases {
		got, err := mapArch(in)
		if err != nil || got != want {
			t.Errorf("mapArch(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"i686", "riscv64", "armv6l", ""} {
		if _, err := mapArch(in); err == nil || !strings.Contains(err.Error(), "unsupported architecture") {
			t.Errorf("mapArch(%q) expected unsupported error, got %v", in, err)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		in, id, like string
	}{
		{"ID=debian\nVERSION_ID=\"12\"\n", "debian", ""},
		{"NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n", "ubuntu", "debian"},
		{"ID='linuxmint'\nID_LIKE=\"ubuntu debian\"\n", "linuxmint", "ubuntu debian"},
		{"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", "rocky", "rhel centos fedora"},
		{"# comment\nNAME=Thing\n", "unknown", ""},
		{"", "unknown", ""},
		{"ID=\n", "unknown", ""},
	}
	for _, c := range cases {
		id, like := parseOSRelease(strings.NewReader(c.in))
		if id != c.id || like != c.like {
			t.Errorf("parseOSRelease(%q) = %q, %q; want %q, %q", c.in, id, like, c.id, c.like)
		}
	}
}

func TestPythonOK(t *testing.T) {
	cases := map[string]bool{
		"3.11.0":  true,
		"3.11.9":  true,
		"3.12.1":  true,
		"3.13":    true,
		"4.0.0":   true,
		"3.10.12": false,
		"3.9.2":   false,
		"2.7.18":  false,
		"":        false,
		"three":   false,
		"3":       false,
		"3.x.1":   false,
	}
	for in, want := range cases {
		if got := pythonOK(in); got != want {
			t.Errorf("pythonOK(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNeedsPrivilege(t *testing.T) {
	dir := t.TempDir()
	if NeedsPrivilege(dir) {
		t.Errorf("temp dir should be writable")
	}
	if NeedsPrivilege(filepath.Join(dir, "a", "b", "c.txt")) {
		t.Errorf("nested path under writable dir should not need privilege")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ro, 0o700) })
	if !NeedsPrivilege(filepath.Join(ro, "file")) {
		t.Errorf("path under read-only dir should need privilege")
	}
	if !NeedsPrivilege(filepath.Join(ro, "x", "y")) {
		t.Errorf("nested path under read-only dir should need privilege")
	}
	if !NeedsPrivilege("/proc/nope/x") {
		t.Errorf("root-owned path should need privilege")
	}
}

func TestWriteFileDirect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "file.env")
	if err := WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "A=1\n" {
		t.Fatalf("content = %q, %v", data, err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
	if err := WriteFile(path, []byte("B=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode after rewrite = %o, want 644", fi.Mode().Perm())
	}
	got, _ := ReadFile(path)
	if string(got) != "B=2\n" {
		t.Errorf("ReadFile = %q", got)
	}
}

func TestMkdirAllDirect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b")
	if err := MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("stat = %v, %v", fi, err)
	}
	if pfi, _ := os.Stat(filepath.Dir(path)); pfi.Mode().Perm() != 0o755 {
		t.Errorf("intermediate dir mode = %o, want 755", pfi.Mode().Perm())
	}
	if err := MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(path)
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("existing dir mode changed to %o", fi.Mode().Perm())
	}
	if !IsDir(path) || !Exists(path) || IsDir(filepath.Join(dir, "nope")) || Exists(filepath.Join(dir, "nope")) {
		t.Errorf("Exists/IsDir mismatch")
	}
}

func TestChmodDirect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", fi.Mode().Perm())
	}
}

func TestRemoveDirect(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, []byte("x"), 0o644)
	if err := Remove(file, false); err != nil {
		t.Fatal(err)
	}
	if Exists(file) {
		t.Errorf("file still exists")
	}
	if err := Remove(file, false); err != nil {
		t.Errorf("removing a missing path should be nil, got %v", err)
	}
	tree := filepath.Join(dir, "t", "u")
	os.MkdirAll(tree, 0o755)
	os.WriteFile(filepath.Join(tree, "f"), []byte("x"), 0o644)
	if err := Remove(filepath.Join(dir, "t"), true); err != nil {
		t.Fatal(err)
	}
	if Exists(filepath.Join(dir, "t")) {
		t.Errorf("tree still exists")
	}
}

func TestPrivilegedAsRoot(t *testing.T) {
	fakeEnv(t, 0, nil)
	cmd := Privileged(context.Background(), "systemctl", "daemon-reload")
	if !reflect.DeepEqual(cmd.Args, []string{"systemctl", "daemon-reload"}) {
		t.Errorf("args = %v", cmd.Args)
	}
	if !IsRoot() {
		t.Errorf("IsRoot should be true")
	}
}

func TestPrivilegedWithSudo(t *testing.T) {
	fakeEnv(t, 1000, map[string]string{"sudo": "/usr/bin/sudo"})
	cmd := Privileged(context.Background(), "apt-get", "update", "-qq")
	if !reflect.DeepEqual(cmd.Args, []string{"sudo", "apt-get", "update", "-qq"}) {
		t.Errorf("args = %v", cmd.Args)
	}
	if cmd.Err != nil {
		t.Errorf("unexpected cmd.Err %v", cmd.Err)
	}
}

func TestPrivilegedWithoutSudo(t *testing.T) {
	fakeEnv(t, 1000, nil)
	cmd := Privileged(context.Background(), "/usr/sbin/useradd", "--system", "x")
	err := Run(cmd)
	if err == nil || err.Error() != "sudo is required for useradd" {
		t.Errorf("Run error = %v", err)
	}
	_, err = Output(Privileged(context.Background(), "apt-get", "update"))
	if err == nil || err.Error() != "sudo is required for apt-get" {
		t.Errorf("Output error = %v", err)
	}
}

func TestSudoPreflight(t *testing.T) {
	fakeEnv(t, 0, nil)
	logged := false
	if err := SudoPreflight(context.Background(), []string{"x"}, func(string) { logged = true }); err != nil || logged {
		t.Errorf("root preflight: err=%v logged=%v", err, logged)
	}
	fakeEnv(t, 1000, nil)
	err := SudoPreflight(context.Background(), []string{"systemd unit", "service user"}, nil)
	if err == nil || err.Error() != "sudo is required for: systemd unit, service user" {
		t.Errorf("no sudo preflight error = %v", err)
	}
}

func TestRunAndOutputErrors(t *testing.T) {
	ctx := context.Background()
	err := Run(Command(ctx, "sh", "-c", "exit 2"))
	if err == nil || err.Error() != "sh: exit status 2" {
		t.Errorf("Run error = %v", err)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Errorf("Run error should wrap ExitError")
	}
	out, err := Output(Command(ctx, "sh", "-c", "echo hi; echo bad >&2; exit 3"))
	if out != "hi\nbad" {
		t.Errorf("Output = %q", out)
	}
	if err == nil || err.Error() != "sh: exit status 3: hi\nbad" {
		t.Errorf("Output error = %v", err)
	}
	out, err = Output(Command(ctx, "sh", "-c", "printf '  ok \\n'"))
	if err != nil || out != "ok" {
		t.Errorf("Output = %q, %v", out, err)
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"/usr/bin/apt-get", "update"}, "apt-get"},
		{[]string{"sudo", "apt-get", "update"}, "apt-get"},
		{[]string{"sudo", "-n", "/usr/sbin/useradd", "x"}, "useradd"},
		{[]string{"sudo", "-v"}, "sudo"},
	}
	for _, c := range cases {
		cmd := &exec.Cmd{Path: c.args[0], Args: c.args}
		if got := displayName(cmd); got != c.want {
			t.Errorf("displayName(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestFilterPublic(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("10.1.2.3"),
		net.ParseIP("172.17.0.1"),
		net.ParseIP("192.168.1.10"),
		net.ParseIP("100.64.5.5"),
		net.ParseIP("169.254.1.1"),
		net.ParseIP("203.0.113.7"),
		net.ParseIP("::1"),
		net.ParseIP("fe80::1"),
		net.ParseIP("fd00::1"),
		net.ParseIP("2001:db8::1"),
		net.ParseIP("0.0.0.0"),
	}
	got := filterPublic(ips)
	want := []string{"203.0.113.7", "2001:db8::1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterPublic = %v, want %v", got, want)
	}
	if filterPublic(nil) != nil {
		t.Errorf("nil input should give nil")
	}
}

func TestOctal(t *testing.T) {
	cases := map[os.FileMode]string{
		0o600:                 "0600",
		0o755:                 "0755",
		0o644 | os.ModeDir:    "0644",
		0o700 | os.ModeSticky: "0700",
	}
	for in, want := range cases {
		if got := octal(in); got != want {
			t.Errorf("octal(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestMisc(t *testing.T) {
	if CurrentUser() == "" {
		t.Errorf("CurrentUser empty")
	}
	if Executable() == "" {
		t.Errorf("Executable empty")
	}
	if !HasCommand("sh") || HasCommand("definitely-not-a-command-xyz") {
		t.Errorf("HasCommand mismatch")
	}
	if utsString([]int8{'x', '8', '6', 0, 'z'}) != "x86" || utsString([]uint8{'a', 'r', 'm', 0}) != "arm" {
		t.Errorf("utsString")
	}
	if _, err := Arch(); err != nil && !strings.Contains(err.Error(), "unsupported architecture") {
		t.Errorf("Arch error = %v", err)
	}
	if id, _ := OSInfo(); id == "" {
		t.Errorf("OSInfo id empty")
	}
	if !UserExists(CurrentUser()) || UserExists("no-such-user-xyz") {
		t.Errorf("UserExists mismatch")
	}
	if nologinShell() == "" {
		t.Errorf("nologinShell empty")
	}
}

func TestWriteFileReplacesARunningBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tm")
	if err := WriteFile(bin, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot execute in the test environment: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	if err := WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("overwriting a running binary must work, self-update depends on it: %v", err)
	}
	data, err := os.ReadFile(bin)
	if err != nil || string(data) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("content = %q, err %v", data, err)
	}
	fi, err := os.Stat(bin)
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, err %v", fi.Mode().Perm(), err)
	}
}

func TestRunAs(t *testing.T) {
	restore := lookPath
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	defer func() { lookPath = restore }()
	me := CurrentUser()
	own := RunAs(context.Background(), me, "git", "-C", "/opt/x", "pull")
	if own.Args[0] == "sudo" {
		t.Fatalf("running as the current user must not shell out to sudo: %v", own.Args)
	}
	other := RunAs(context.Background(), "traefik-manager", "git", "-C", "/opt/x", "pull")
	want := []string{"sudo", "-u", "traefik-manager", "git", "-C", "/opt/x", "pull"}
	if strings.Join(other.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", other.Args, want)
	}
}

func TestDockerInstallRouting(t *testing.T) {
	cases := []struct {
		id, like string
		pacman   bool
	}{
		{"arch", "", true},
		{"cachyos", "arch", true},
		{"endeavouros", "arch", true},
		{"manjaro", "arch", true},
		{"ubuntu", "debian", false},
		{"linuxmint", "ubuntu", false},
		{"pop", "ubuntu debian", false},
		{"raspbian", "debian", false},
		{"debian", "", false},
		{"fedora", "", false},
		{"rocky", "rhel centos fedora", false},
		{"almalinux", "rhel centos fedora", false},
		{"opensuse-tumbleweed", "opensuse suse", false},
	}
	for _, c := range cases {
		if got := usesPacman(c.id, c.like); got != c.pacman {
			t.Errorf("%s: pacman = %v, want %v", c.id, got, c.pacman)
		}
	}
}

func TestDnfRepoURL(t *testing.T) {
	cases := map[[2]string]string{
		{"fedora", ""}:                      "https://download.docker.com/linux/fedora/docker-ce.repo",
		{"almalinux", "rhel centos fedora"}: "https://download.docker.com/linux/centos/docker-ce.repo",
		{"rocky", "rhel centos fedora"}:     "https://download.docker.com/linux/centos/docker-ce.repo",
		{"ol", "fedora"}:                    "https://download.docker.com/linux/fedora/docker-ce.repo",
		{"rhel", ""}:                        "https://download.docker.com/linux/centos/docker-ce.repo",
	}
	for in, want := range cases {
		if got := dnfRepoURL(in[0], in[1]); got != want {
			t.Errorf("%s/%s: %s, want %s", in[0], in[1], got, want)
		}
	}
}

func TestDockerCommandUsesSudoOnlyWhenAsked(t *testing.T) {
	restore := lookPath
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	geteuidRestore := geteuid
	geteuid = func() int { return 1000 }
	defer func() {
		lookPath = restore
		geteuid = geteuidRestore
		UseDockerSudo(false)
	}()
	UseDockerSudo(false)
	if got := DockerCommand(context.Background(), "info").Args[0]; got == "sudo" {
		t.Fatal("docker must run directly when the user is in the docker group")
	}
	UseDockerSudo(true)
	args := DockerCommand(context.Background(), "logs", "traefik-manager").Args
	want := "sudo docker logs traefik-manager"
	if strings.Join(args, " ") != want {
		t.Fatalf("args = %v, want %s", args, want)
	}
}

func TestDockerGroupMembershipIsAboutThisProcess(t *testing.T) {
	if _, err := user.LookupGroup("docker"); err != nil {
		t.Skip("no docker group on this machine")
	}
	effective := InDockerGroup()
	pending := DockerGroupPending()
	if effective && pending {
		t.Fatal("a process that already has the group cannot also be pending")
	}
}
