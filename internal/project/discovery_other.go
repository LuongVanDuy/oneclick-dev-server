//go:build !windows

package project

import (
	"os"
	"path/filepath"
)

func conventionalWebRoots() []string {
	roots := []string{"/var/www/html", "/srv/www"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "Sites"))
	}
	return roots
}
