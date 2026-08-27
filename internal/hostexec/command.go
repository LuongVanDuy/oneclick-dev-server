package hostexec

import (
	"context"
	"os/exec"
)

// Command creates a host process using platform-safe desktop defaults.
func Command(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	configure(command)
	return command
}

// CommandContext creates a cancellable host process using platform-safe
// desktop defaults.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	configure(command)
	return command
}
