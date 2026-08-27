package installer

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	ActionInstallMultipass  = "install_multipass"
	ActionRepairMultipass   = "repair_multipass"
	ActionInstallVirtualBox = "install_virtualbox"
)

var installLock sync.Mutex

// GetPlan returns metadata for an allowlisted dependency action.
func GetPlan(action string) Plan {
	if !isAllowedAction(action) {
		return Plan{Action: action, Title: "Thao tác không hợp lệ"}
	}
	return platformPlan(action)
}

// Run executes only a built-in action. Commands, paths and URLs are never
// accepted from the frontend.
func Run(ctx context.Context, action string, report Reporter) Result {
	if !isAllowedAction(action) {
		return Result{Message: "Không thể thực hiện", Detail: "Thao tác không nằm trong danh sách cho phép."}
	}
	plan := platformPlan(action)
	if !plan.Supported {
		return Result{Message: "Chưa hỗ trợ", Detail: plan.Description}
	}
	if ctx == nil {
		return Result{Message: "Ứng dụng chưa sẵn sàng", Detail: errors.New("missing application context").Error()}
	}
	if !installLock.TryLock() {
		return Result{Message: "Đang có cài đặt khác", Detail: "Đợi thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer installLock.Unlock()
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if report == nil {
		report = func(Progress) {}
	}
	return platformRun(runCtx, action, report)
}

func isAllowedAction(action string) bool {
	return action == ActionInstallMultipass || action == ActionRepairMultipass || action == ActionInstallVirtualBox
}
