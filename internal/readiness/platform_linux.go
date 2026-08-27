//go:build linux

package readiness

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func checkVirtualization(_ context.Context) Check {
	check := Check{
		ID:       "virtualization",
		Title:    "Ảo hoá phần cứng",
		Required: true,
	}

	file, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err == nil {
		file.Close()
		check.Status = StatusReady
		check.Summary = "KVM đã sẵn sàng"
		check.Detail = "Ứng dụng có quyền sử dụng /dev/kvm."
		return check
	}
	if os.IsPermission(err) {
		check.Status = StatusInstallable
		check.Summary = "Tài khoản chưa có quyền dùng KVM"
		check.Detail = "Cần thêm tài khoản hiện tại vào nhóm kvm rồi đăng nhập lại."
		check.ActionLabel = "Cấp quyền"
		check.ActionKind = "grant_kvm"
		return check
	}

	check.Status = StatusUserAction
	check.Summary = "KVM chưa khả dụng"
	check.Detail = "Kiểm tra VT-x/AMD-V trong BIOS/UEFI và module KVM của Ubuntu."
	check.ActionLabel = "Xem hướng dẫn"
	check.ActionKind = "enable_virtualization"
	return check
}

func platformResources() (uint64, uint64, error) {
	memory, err := linuxMemoryBytes()
	if err != nil {
		return 0, 0, err
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs("/", &stats); err != nil {
		return 0, 0, fmt.Errorf("read free disk: %w", err)
	}
	disk := stats.Bavail * uint64(stats.Bsize)
	return memory, disk, nil
}

func linuxMemoryBytes() (uint64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read memory info: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("parse memory info: %w", parseErr)
		}
		return kb * 1024, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("MemTotal was not found")
}
