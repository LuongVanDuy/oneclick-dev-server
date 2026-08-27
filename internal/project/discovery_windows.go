//go:build windows

package project

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func conventionalWebRoots() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	roots := make([]string, 0, 12)
	for index := 0; index < 26; index++ {
		if mask&(1<<index) == 0 {
			continue
		}
		drive := fmt.Sprintf("%c:\\", 'A'+index)
		encoded, err := windows.UTF16PtrFromString(drive)
		if err != nil || windows.GetDriveType(encoded) != windows.DRIVE_FIXED {
			continue
		}
		roots = append(roots,
			drive+`laragon\www`,
			drive+`xampp\htdocs`,
			drive+`wamp64\www`,
		)
	}
	return roots
}
