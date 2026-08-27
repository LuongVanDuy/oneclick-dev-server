//go:build !windows

package datastore

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
)

func platformInspect(_ context.Context, projectCount int) Status {
	root, _ := platformApplicationRoot()
	recommended := filepath.Join(root, "data", "multipass")
	return Status{
		Platform: runtime.GOOS, Supported: false, ProjectCount: projectCount,
		RecommendedPath: recommended,
		Message:         "Thiết lập vị trí dữ liệu tự động hiện hỗ trợ Windows.",
		Detail:          "Ubuntu sẽ được nối bằng systemd MULTIPASS_STORAGE trong pha đóng gói nền tảng.",
	}
}

func platformConfigure(_ context.Context, _ string, projectCount int, _ Reporter) Result {
	status := platformInspect(context.Background(), projectCount)
	return Result{Message: "Chưa hỗ trợ trên hệ điều hành này", Detail: status.Detail, Status: status}
}

func platformApplicationRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(executable), nil
}

func platformRunElevatedHelper(_ string) int {
	return 1
}
