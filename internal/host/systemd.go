package host

import (
	"context"
	"strconv"
)

func Systemctl(ctx context.Context, args ...string) error {
	return Run(Privileged(ctx, "systemctl", args...))
}

func SystemctlOutput(ctx context.Context, args ...string) (string, error) {
	return Output(Privileged(ctx, "systemctl", args...))
}

func Journalctl(ctx context.Context, unit string, lines int) (string, error) {
	return Output(Privileged(ctx, "journalctl", "-u", unit, "--no-pager", "-n", strconv.Itoa(lines)))
}

func ServiceActive(ctx context.Context, unit string) bool {
	out, _ := Output(Command(ctx, "systemctl", "is-active", unit))
	return out == "active"
}
