package project

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotExcludesSecretsCachesAndDependencies(t *testing.T) {
	root := t.TempDir()
	writeSnapshotFixture(t, root, "index.php", "<?php echo 'ok';")
	writeSnapshotFixture(t, root, ".env", "DB_PASSWORD=secret")
	writeSnapshotFixture(t, root, ".env.example", "DB_PASSWORD=")
	writeSnapshotFixture(t, root, "wp-config.php", "password")
	writeSnapshotFixture(t, root, "private.key", "secret")
	writeSnapshotFixture(t, root, "database.sql", "secret")
	writeSnapshotFixture(t, root, "storage/logs/app.log", "secret")
	writeSnapshotFixture(t, root, ".git/config", "secret")
	writeSnapshotFixture(t, root, "node_modules/pkg/index.js", "generated")
	writeSnapshotFixture(t, root, "assets/app.css", "body{}")

	plan, err := PrepareCopyPlan(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	if plan.FileCount != 3 || plan.SecretExcluded < 3 || plan.ExcludedCount < 7 {
		t.Fatalf("unexpected plan: %#v", plan)
	}

	artifact, err := BuildSnapshot(root, ".", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Cleanup()
	if len(artifact.Checksum) != 64 || len(artifact.SnapshotID) != 16 {
		t.Fatalf("unexpected artifact identity: %#v", artifact)
	}
	names, manifest := readSnapshotArchive(t, artifact.ArchivePath)
	for _, expected := range []string{"index.php", ".env.example", "assets/app.css", manifestArchivePath} {
		if !names[expected] {
			t.Fatalf("archive is missing %s: %#v", expected, names)
		}
	}
	for _, forbidden := range []string{".env", "wp-config.php", "private.key", "database.sql", "storage/logs/app.log", ".git/config", "node_modules/pkg/index.js"} {
		if names[forbidden] {
			t.Fatalf("archive contains forbidden path %s", forbidden)
		}
	}
	if manifest.FileCount != 3 || len(manifest.Files) != 3 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestSnapshotRejectsIncludedSymlink(t *testing.T) {
	root := t.TempDir()
	writeSnapshotFixture(t, root, "index.html", "ok")
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "outside-link.txt")); err != nil {
		t.Skipf("symlink is unavailable on this host: %v", err)
	}
	if _, err := PrepareCopyPlan(root, "."); err == nil {
		t.Fatal("expected an unsafe link to be rejected")
	}
}

func writeSnapshotFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readSnapshotArchive(t *testing.T, path string) (map[string]bool, snapshotManifest) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	names := map[string]bool{}
	var manifest snapshotManifest
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[header.Name] = true
		if header.Name == manifestArchivePath {
			if err := json.NewDecoder(tarReader).Decode(&manifest); err != nil {
				t.Fatal(err)
			}
		}
	}
	return names, manifest
}
