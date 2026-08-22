package project

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Candidate struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	SourceKind string `json:"sourceKind"`
}

type root struct {
	path string
	kind string
}

func Discover() []Candidate {
	roots := defaultRoots()
	if extra := strings.TrimSpace(os.Getenv("ONECLICK_PROJECT_ROOTS")); extra != "" {
		for _, p := range filepath.SplitList(extra) {
			p = strings.TrimSpace(p)
			if p != "" {
				roots = append(roots, root{path: p, kind: "Custom"})
			}
		}
	}

	seen := map[string]bool{}
	projects := make([]Candidate, 0)
	for _, r := range roots {
		entries, err := os.ReadDir(r.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			full, err := filepath.Abs(filepath.Join(r.path, entry.Name()))
			if err != nil {
				continue
			}
			key := strings.ToLower(filepath.Clean(full))
			if seen[key] {
				continue
			}
			seen[key] = true
			projects = append(projects, Candidate{Name: entry.Name(), Path: full, SourceKind: r.kind})
		}
	}

	sort.Slice(projects, func(i, j int) bool {
		if projects[i].SourceKind == projects[j].SourceKind {
			return strings.ToLower(projects[i].Name) < strings.ToLower(projects[j].Name)
		}
		return projects[i].SourceKind < projects[j].SourceKind
	})
	return projects
}

func defaultRoots() []root {
	if runtime.GOOS == "windows" {
		return []root{{`C:\xampp\htdocs`, "XAMPP"}, {`C:\laragon\www`, "Laragon"}}
	}

	home, _ := os.UserHomeDir()
	roots := []root{{"/var/www", "System web root"}}
	if home != "" {
		roots = append(roots,
			root{filepath.Join(home, "www"), "User web root"},
			root{filepath.Join(home, "projects"), "Projects"},
		)
	}
	return roots
}
