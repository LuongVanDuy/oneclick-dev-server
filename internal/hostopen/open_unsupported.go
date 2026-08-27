//go:build !windows && !linux

package hostopen

import (
	"errors"
	"os"
)

func isLinkLike(info os.FileInfo) bool {
	return info == nil || info.Mode()&os.ModeSymlink != 0
}

func openDirectory(string) error {
	return errors.New("nền tảng chưa hỗ trợ mở thư mục")
}
