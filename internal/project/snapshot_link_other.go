//go:build !windows

package project

import "io/fs"

func unsafeLinkOrReparse(_ string, info fs.FileInfo) (bool, error) {
	return info.Mode()&fs.ModeSymlink != 0, nil
}
