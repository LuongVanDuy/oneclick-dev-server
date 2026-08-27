package datastore

import (
	"context"
	"errors"
	"sync"
	"time"
)

var configureLock sync.Mutex

// Inspect is read-only. projectCount comes from the application's durable
// state so the platform layer can lock storage before any deployment exists.
func Inspect(ctx context.Context, projectCount int) Status {
	if ctx == nil {
		ctx = context.Background()
	}
	return platformInspect(ctx, projectCount)
}

// Configure applies one fixed, reviewed platform operation. The frontend can
// choose only a directory; it cannot supply a command or script.
func Configure(ctx context.Context, target string, projectCount int, report Reporter) Result {
	if ctx == nil {
		return Result{Message: "Ứng dụng chưa sẵn sàng", Detail: "missing application context"}
	}
	if !configureLock.TryLock() {
		return Result{Message: "Đang thiết lập dữ liệu", Detail: "Đợi thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer configureLock.Unlock()
	if report == nil {
		report = func(Progress) {}
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return platformConfigure(runCtx, target, projectCount, report)
}

func ApplicationRoot() (string, error) {
	root, err := platformApplicationRoot()
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", errors.New("không xác định được thư mục ứng dụng")
	}
	return root, nil
}

// RunElevatedHelper lets the signed application binary perform the fixed
// Windows bootstrap without embedding a large privileged PowerShell script.
// Call this before starting the desktop runtime.
func RunElevatedHelper(args []string) (bool, int) {
	if len(args) != 3 || args[1] != "--oneclick-storage-helper" {
		return false, 0
	}
	return true, platformRunElevatedHelper(args[2])
}
