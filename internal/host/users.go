package host

import (
	"context"
	"os"
	osuser "os/user"
)

func UserExists(name string) bool {
	_, err := osuser.Lookup(name)
	return err == nil
}

func nologinShell() string {
	for _, p := range []string{"/usr/sbin/nologin", "/usr/bin/nologin", "/sbin/nologin"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "/usr/sbin/nologin"
}

func AddSystemUser(ctx context.Context, name string) error {
	return Run(Privileged(ctx, "useradd", "--system", "--no-create-home", "--shell", nologinShell(), name))
}

func AddUserToGroup(ctx context.Context, user, group string) error {
	return Run(Privileged(ctx, "usermod", "-aG", group, user))
}
