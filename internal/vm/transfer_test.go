package vm

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"oneclick-dev-server/internal/project"
)

func TestGuestSnapshotPathIsRestricted(t *testing.T) {
	if !safeGuestSnapshotPath("/var/lib/oneclick/staging/0123456789abcdef/0123456789abcdef") {
		t.Fatal("expected the fixed staging root to be accepted")
	}
	if safeGuestSnapshotPath("/home/ubuntu/source") || safeGuestSnapshotPath("/var/lib/other") || safeGuestSnapshotPath("/var/lib/oneclick/staging/0123456789abcdef/../../etc") {
		t.Fatal("unexpected guest path accepted")
	}
}

func TestLiveSnapshotTransfer(t *testing.T) {
	projectPath := os.Getenv("ONECLICK_LIVE_PROJECT_PATH")
	if projectPath == "" {
		t.Skip("set ONECLICK_LIVE_PROJECT_PATH to run the local snapshot transfer integration test")
	}
	documentRoot := os.Getenv("ONECLICK_LIVE_DOCUMENT_ROOT")
	if documentRoot == "" {
		documentRoot = "."
	}
	artifact, err := project.BuildSnapshot(projectPath, documentRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Cleanup()
	absolute, err := filepath.Abs(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	sum := sha256.Sum256([]byte(canonical))
	projectID := hex.EncodeToString(sum[:8])
	result := TransferSnapshot(nil, SnapshotTransfer{
		ProjectPath: projectPath, ProjectName: filepath.Base(projectPath), ProjectID: projectID,
		ArchivePath: artifact.ArchivePath, Checksum: artifact.Checksum, SnapshotID: artifact.SnapshotID,
	}, nil)
	if !result.Success {
		t.Fatalf("live transfer failed: %#v", result)
	}
	t.Logf("snapshot=%s files=%d bytes=%d guest=%s", artifact.SnapshotID, artifact.FileCount, artifact.TotalBytes, result.GuestPath)
}
