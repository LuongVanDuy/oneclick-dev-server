//go:build linux

package hostopen

import (
	"os"

	"oneclick-dev-server/internal/hostexec"
)

func isLinkLike(info os.FileInfo) bool {
	return info == nil || info.Mode()&os.ModeSymlink != 0
}

func openDirectory(path string) error {
	command := hostexec.Command("xdg-open", path)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
