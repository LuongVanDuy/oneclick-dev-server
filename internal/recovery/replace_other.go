//go:build !windows

package recovery

import "os"

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return os.Chmod(target, 0o600)
}
