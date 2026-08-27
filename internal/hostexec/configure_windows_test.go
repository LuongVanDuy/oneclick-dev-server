//go:build windows

package hostexec

import (
	"context"
	"testing"
)

func TestCommandContextHidesConsoleWindow(t *testing.T) {
	command := CommandContext(context.Background(), "powershell.exe", "-NoProfile")
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("Windows desktop child process must hide its console window")
	}
	if command.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("Windows desktop child process must use CREATE_NO_WINDOW")
	}
}
