//go:build !windows

package sourceeditor

import "os"

func isLinkLike(info os.FileInfo) bool {
	return info == nil || info.Mode()&os.ModeSymlink != 0
}
