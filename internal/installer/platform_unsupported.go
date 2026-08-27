//go:build !windows && !linux

package installer

import "context"

func platformPlan(action string) Plan {
	title := "Cài đặt Multipass"
	if action == ActionInstallVirtualBox {
		title = "Cài đặt VirtualBox"
	}
	return Plan{Action: action, Title: title, Description: "Hệ điều hành này chưa được hỗ trợ."}
}

func platformRun(_ context.Context, _ string, _ Reporter) Result {
	return Result{Message: "Chưa hỗ trợ", Detail: "Chỉ hỗ trợ Windows và Ubuntu x64."}
}
