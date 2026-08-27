package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLaravel(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "artisan"), "")
	mustWrite(t, filepath.Join(root, "composer.json"), `{"require":{"php":"^8.2"}}`)
	if err := os.Mkdir(filepath.Join(root, "public"), 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "laravel" || info.DocumentRoot != "public" || info.PHPRequirement != "^8.2" || !info.Ready {
		t.Fatalf("unexpected detection: %#v", info)
	}
}

func TestDetectUnknownDoesNotRecurse(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "nested", "index.php"), "")

	info, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "unknown" || info.Ready || len(info.Warnings) == 0 {
		t.Fatalf("unexpected detection: %#v", info)
	}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
