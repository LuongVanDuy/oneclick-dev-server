package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverIncludesCustomProjectRoot(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "demo-site")
	if err := os.Mkdir(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ONECLICK_PROJECT_ROOTS", root)

	projects := Discover()
	for _, candidate := range projects {
		if candidate.Name == "demo-site" && filepath.Clean(candidate.Path) == filepath.Clean(projectPath) {
			if candidate.SourceKind != "Custom" {
				t.Fatalf("expected Custom source kind, got %q", candidate.SourceKind)
			}
			return
		}
	}

	t.Fatalf("custom project was not discovered: %#v", projects)
}
