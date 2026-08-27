//go:build windows

package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"oneclick-dev-server/internal/hostexec"
)

func checkVirtualization(ctx context.Context) Check {
	check := Check{
		ID:       "virtualization",
		Title:    "Ảo hoá phần cứng",
		Required: true,
	}

	command := "$cs=Get-CimInstance Win32_ComputerSystem; $cpu=Get-CimInstance Win32_Processor | Select-Object -First 1; [pscustomobject]@{HypervisorPresent=[bool]$cs.HypervisorPresent; FirmwareEnabled=[bool]$cpu.VirtualizationFirmwareEnabled} | ConvertTo-Json -Compress"
	output, err := hostexec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput()
	if err != nil {
		check.Status = StatusBroken
		check.Summary = "Không kiểm tra được trạng thái ảo hoá"
		check.Detail = strings.TrimSpace(string(output))
		if check.Detail == "" {
			check.Detail = err.Error()
		}
		check.ActionLabel = "Kiểm tra lại"
		check.ActionKind = "retry"
		return check
	}

	var state struct {
		HypervisorPresent bool `json:"HypervisorPresent"`
		FirmwareEnabled   bool `json:"FirmwareEnabled"`
	}
	if err := json.Unmarshal(output, &state); err != nil {
		check.Status = StatusBroken
		check.Summary = "Kết quả kiểm tra ảo hoá không hợp lệ"
		check.Detail = err.Error()
		return check
	}

	if state.HypervisorPresent || state.FirmwareEnabled {
		check.Status = StatusReady
		check.Summary = "Ảo hoá phần cứng đã bật"
		check.Detail = "Máy có thể chạy sandbox VM. Backend Hyper-V sẽ được kiểm tra cùng Multipass."
		return check
	}

	check.Status = StatusUserAction
	check.Summary = "Ảo hoá đang tắt trong BIOS/UEFI"
	check.Detail = "Cần bật Intel VT-x hoặc AMD-V rồi khởi động lại máy."
	check.ActionLabel = "Xem hướng dẫn"
	check.ActionKind = "enable_virtualization"
	return check
}

func platformResources() (uint64, uint64, error) {
	memoryOutput, err := hostexec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory").Output()
	if err != nil {
		return 0, 0, fmt.Errorf("read total memory: %w", err)
	}
	memory, err := strconv.ParseUint(strings.TrimSpace(string(memoryOutput)), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse total memory: %w", err)
	}

	executable, err := os.Executable()
	if err != nil {
		return 0, 0, fmt.Errorf("locate executable: %w", err)
	}
	volume := strings.TrimSuffix(filepath.VolumeName(executable), ":")
	if len(volume) != 1 {
		return 0, 0, errors.New("cannot determine application drive")
	}
	diskCommand := fmt.Sprintf("(Get-PSDrive -Name '%s').Free", volume)
	diskOutput, err := hostexec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", diskCommand).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("read free disk: %w", err)
	}
	disk, err := strconv.ParseUint(strings.TrimSpace(string(diskOutput)), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse free disk: %w", err)
	}

	return memory, disk, nil
}
