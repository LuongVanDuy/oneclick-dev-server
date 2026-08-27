//go:build !windows

package hostexec

import "os/exec"

func configure(_ *exec.Cmd) {}
