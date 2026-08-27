//go:build !windows

package multipass

import (
	"errors"
	"os/exec"
)

func findVBoxManage() (string, error) {
	if path, err := exec.LookPath("VBoxManage"); err == nil {
		return path, nil
	}
	return "", errors.New("không tìm thấy VBoxManage")
}

func inspectVBoxDrivers(_ string) (vboxDriverInspection, error) {
	return vboxDriverInspection{Ready: true}, nil
}
