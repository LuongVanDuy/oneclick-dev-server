package project

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const maxDiscoveredProjects = 128

// Discover inspects immediate child directories of conventional local web
// roots. It deliberately does not recurse, execute project code or write data.
func Discover() ([]Info, error) {
	return discoverRoots(conventionalWebRoots())
}

func discoverRoots(roots []string) ([]Info, error) {
	result := make([]Info, 0)
	seen := make(map[string]bool)
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if len(result) >= maxDiscoveredProjects {
				break
			}
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			info, err := Detect(filepath.Join(root, entry.Name()))
			if err != nil || !info.Ready {
				continue
			}
			key := filepath.Clean(info.Path)
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, info)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.ToLower(result[left].Path) < strings.ToLower(result[right].Path)
	})
	return result, nil
}
