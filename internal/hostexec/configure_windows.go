//go:build windows

package hostexec

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configure(command *exec.Cmd) {
	// OneClick is a GUI application. PowerShell, Multipass and other console
	// helpers must stay invisible; expected UAC consent is created separately.
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
