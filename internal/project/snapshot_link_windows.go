//go:build windows

package project

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

func unsafeLinkOrReparse(path string, info fs.FileInfo) (bool, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := windows.GetFileAttributes(pointer)
	if err != nil {
		return false, err
	}
	return info.Mode()&fs.ModeSymlink != 0 || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}
