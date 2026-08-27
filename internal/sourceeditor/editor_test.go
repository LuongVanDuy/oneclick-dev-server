package sourceeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListReadAndSaveSourceWithBackup(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	if err := os.MkdirAll(filepath.Join(site, "wp-content", "themes", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(site, "wp-content", "themes", "demo", "style.css")
	if err := os.WriteFile(filePath, []byte("body { color: red; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := Config{ProjectID: "0123456789abcdef", ProjectPath: site, ProjectName: "Demo", BackupRoot: filepath.Join(root, "backups")}
	listing, err := List(config, "wp-content/themes/demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 || !listing.Entries[0].Editable {
		t.Fatalf("unexpected listing: %#v", listing)
	}
	file, err := Read(config, listing.Entries[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	result := Save(config, SaveRequest{Path: file.Path, Content: "body { color: blue; }\n", ExpectedSHA: file.SHA256})
	if !result.Success {
		t.Fatalf("save failed: %#v", result)
	}
	updated, err := os.ReadFile(filePath)
	if err != nil || string(updated) != "body { color: blue; }\n" {
		t.Fatalf("source not updated: %q %v", updated, err)
	}
	backup, err := os.ReadFile(filepath.Join(result.BackupPath, "original.css"))
	if err != nil || string(backup) != file.Content {
		t.Fatalf("backup mismatch: %q %v", backup, err)
	}
}

func TestSaveRejectsStaleHash(t *testing.T) {
	config, path := fixture(t, "index.php", "<?php echo 'first';\n")
	file, err := Read(config, "index.php")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<?php echo 'other';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Save(config, SaveRequest{Path: "index.php", Content: "<?php echo 'mine';\n", ExpectedSHA: file.SHA256})
	if result.Success || !strings.Contains(result.Message, "thay đổi") {
		t.Fatalf("expected stale save rejection: %#v", result)
	}
}

func TestEditorRejectsTraversalSecretsBinaryAndSymlink(t *testing.T) {
	config, _ := fixture(t, "wp-config.php", "<?php define('DB_PASSWORD', 'secret');")
	if _, err := Read(config, "../outside.php"); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := Read(config, "wp-config.php"); err == nil {
		t.Fatal("expected wp-config rejection")
	}
	binary := filepath.Join(config.ProjectPath, "image.png")
	if err := os.WriteFile(binary, []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(config, "image.png"); err == nil {
		t.Fatal("expected binary rejection")
	}
	link := filepath.Join(config.ProjectPath, "linked.php")
	if err := os.Symlink(filepath.Join(config.ProjectPath, "wp-config.php"), link); err == nil {
		defer os.Remove(link)
		if _, err := Read(config, "linked.php"); err == nil {
			t.Fatal("expected symlink rejection")
		}
	}
}

func TestListBlocksDependencyDirectories(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	if err := os.MkdirAll(filepath.Join(site, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := Config{ProjectID: "0123456789abcdef", ProjectPath: site, ProjectName: "Demo", BackupRoot: filepath.Join(root, "backups")}
	listing, err := List(config, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Editable || listing.Entries[0].BlockedReason == "" {
		t.Fatalf("dependency directory not blocked: %#v", listing)
	}
}

func fixture(t *testing.T, name, content string) (Config, string) {
	t.Helper()
	root := t.TempDir()
	site := filepath.Join(root, "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(site, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return Config{ProjectID: "0123456789abcdef", ProjectPath: site, ProjectName: "Demo", BackupRoot: filepath.Join(root, "backups")}, path
}
