//go:build !windows

package recoverytool

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
