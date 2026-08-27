//go:build linux

package installer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func platformPlan(action string) Plan {
	if action == ActionInstallVirtualBox {
		return Plan{Action: action, Title: "Cài đặt VirtualBox", Description: "Ubuntu dùng QEMU/KVM; không cài VirtualBox qua OneClick.", Supported: false}
	}
	title := "Cài đặt Multipass"
	if action == ActionRepairMultipass {
		title = "Sửa cài đặt Multipass"
	}
	return Plan{
		Action: action, Title: title,
		Description: "Cài Multipass từ kênh stable chính thức của Canonical.",
		Publisher:   "Canonical Group Limited", Version: "1.16/stable",
		Source: "Snap Store", DownloadSize: "Phụ thuộc phiên bản",
		RequiresAdmin: true, RebootPossible: false, Supported: true,
	}
}

func platformRun(ctx context.Context, action string, report Reporter) Result {
	if action == ActionInstallVirtualBox {
		return Result{Message: "Không cần VirtualBox", Detail: "Ubuntu dùng QEMU/KVM."}
	}
	if _, err := exec.LookPath("snap"); err != nil {
		return Result{Message: "Thiếu Snap", Detail: "Cài snapd rồi thử lại."}
	}
	report(Progress{Stage: "permission", Message: "Đang yêu cầu quyền quản trị…", Percent: 15})
	program := "snap"
	args := []string{"install", "multipass", "--channel=1.16/stable"}
	if os.Geteuid() != 0 {
		if _, err := exec.LookPath("pkexec"); err != nil {
			return Result{Message: "Không thể yêu cầu quyền quản trị", Detail: "Không tìm thấy pkexec."}
		}
		program = "pkexec"
		args = append([]string{"snap"}, args...)
	}
	report(Progress{Stage: "install", Message: "Đang cài Multipass…", Percent: 45})
	output, err := exec.CommandContext(ctx, program, args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return Result{Message: "Cài đặt không thành công", Detail: detail}
	}
	if ctx.Err() != nil {
		return Result{Message: "Cài đặt bị dừng", Detail: errors.New("operation cancelled").Error()}
	}
	report(Progress{Stage: "complete", Message: "Đã cài đặt Multipass", Percent: 100})
	return Result{Success: true, Message: "Cài đặt thành công"}
}
