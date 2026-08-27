package multipass

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"oneclick-dev-server/internal/hostexec"
)

const supportedVirtualBoxVersion = "7.1.18"

type BackendStatus struct {
	Driver         string
	Ready          bool
	RebootRequired bool
	Detail         string
	MissingAction  string
	VBoxManage     string
}

func Find() (string, error) {
	if path, err := exec.LookPath("multipass"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		path := filepath.Join(programFiles, "Multipass", "bin", "multipass.exe")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("không tìm thấy multipass")
}

// InspectBackend checks the selected Multipass driver and its required host
// executable without changing the driver or host configuration.
func InspectBackend(ctx context.Context, executable string) (BackendStatus, error) {
	output, err := hostexec.CommandContext(ctx, executable, "get", "local.driver").CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return BackendStatus{}, fmt.Errorf("không đọc được driver Multipass: %s", detail)
	}
	driver := strings.ToLower(strings.TrimSpace(string(output)))
	status := BackendStatus{Driver: driver}
	if runtime.GOOS != "windows" || driver == "hyperv" || driver == "qemu" {
		status.Ready = true
		status.Detail = "Driver: " + driver
		return status, nil
	}
	if driver != "virtualbox" {
		status.Detail = "Driver Multipass chưa được hỗ trợ: " + driver
		return status, nil
	}

	vboxManage, err := findVBoxManage()
	if err != nil {
		status.Detail = "Multipass đang dùng VirtualBox nhưng VirtualBox chưa được cài đặt."
		status.MissingAction = "install_virtualbox"
		return status, nil
	}
	versionOutput, err := hostexec.CommandContext(ctx, vboxManage, "--version").CombinedOutput()
	if err != nil {
		status.Detail = "VirtualBox đã có nhưng VBoxManage không phản hồi."
		status.MissingAction = "install_virtualbox"
		return status, nil
	}
	version := strings.TrimSpace(string(versionOutput))
	if !isSupportedVirtualBoxVersion(version) {
		status.Detail = "VirtualBox " + version + " không tương thích với bản OneClick này; cần VirtualBox " + supportedVirtualBoxVersion + "."
		status.MissingAction = "install_virtualbox"
		return status, nil
	}
	status.VBoxManage = vboxManage
	drivers, err := inspectVBoxDrivers(supportedVirtualBoxVersion)
	if err != nil {
		status.Detail = "Không xác minh được driver VirtualBox: " + err.Error()
		status.MissingAction = "install_virtualbox"
		return status, nil
	}
	if !drivers.Ready {
		status.RebootRequired = drivers.RebootRequired
		status.Detail = drivers.Detail
		if !drivers.RebootRequired {
			status.MissingAction = "install_virtualbox"
		}
		return status, nil
	}
	status.Ready = true
	status.Detail = "Driver: virtualbox · VirtualBox " + version + " · driver host đồng bộ"
	return status, nil
}

func isSupportedVirtualBoxVersion(version string) bool {
	return strings.HasPrefix(strings.TrimSpace(version), supportedVirtualBoxVersion+"r")
}

type vboxDriverInspection struct {
	Ready          bool
	RebootRequired bool
	Detail         string
}

func isSupportedVBoxDriverVersion(version, expected string) bool {
	version = strings.TrimSpace(version)
	return version == expected || strings.HasPrefix(version, expected+".")
}
