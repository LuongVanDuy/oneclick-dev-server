package hostopen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDirectoryAcceptsDirectoryAndRejectsFile(t *testing.T) {
	root := t.TempDir()
	absolute, err := ValidateDirectory(root)
	if err != nil || absolute != filepath.Clean(root) {
		t.Fatalf("unexpected directory result: %q %v", absolute, err)
	}
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDirectory(file); err == nil {
		t.Fatal("expected file to be rejected")
	}
}

func TestValidateDirectoryRejectsSymlinkWhenSupported(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	defer os.Remove(link)
	if _, err := ValidateDirectory(link); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
}
