//go:build !windows

package recoverytool

import "os"

func isLinkLike(info os.FileInfo) bool {
	return info == nil || info.Mode()&os.ModeSymlink != 0
}
