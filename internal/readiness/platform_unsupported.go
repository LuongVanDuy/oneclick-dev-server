//go:build !windows && !linux

package readiness

import (
	"context"
	"errors"
)

func checkVirtualization(_ context.Context) Check {
	return Check{
		ID:       "virtualization",
		Title:    "Ảo hoá phần cứng",
		Status:   StatusUnsupported,
		Summary:  "Nền tảng chưa được hỗ trợ",
		Required: true,
	}
}

func platformResources() (uint64, uint64, error) {
	return 0, 0, errors.New("resource detection is not implemented on this platform")
}
