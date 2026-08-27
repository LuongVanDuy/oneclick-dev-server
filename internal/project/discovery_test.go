package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverRootsFindsOnlyImmediateWebsites(t *testing.T) {
	root := t.TempDir()
	wordpress := filepath.Join(root, "wordpress")
	static := filepath.Join(root, "landing")
	unknown := filepath.Join(root, "notes")
	for _, path := range []string{wordpress, static, unknown, filepath.Join(unknown, "nested")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(wordpress, "wp-config.php"), "")
	mustWrite(t, filepath.Join(static, "index.html"), "")
	mustWrite(t, filepath.Join(unknown, "nested", "index.php"), "")

	items, err := discoverRoots([]string{root, filepath.Join(root, "missing")})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 immediate websites, got %#v", items)
	}
	if items[0].Name != "landing" || items[1].Name != "wordpress" {
		t.Fatalf("unexpected discovery order: %#v", items)
	}
}
