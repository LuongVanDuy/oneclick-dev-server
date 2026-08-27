package vm

import (
	"errors"
	"testing"
)

func TestGuestExportPathIsRestricted(t *testing.T) {
	accepted := "/home/ubuntu/.oneclick/exports/0123456789abcdef/20260824-091500/database.sql"
	if !safeGuestExport.MatchString(accepted) {
		t.Fatal("expected reviewed export path to be accepted")
	}
	for _, value := range []string{
		"/etc/shadow",
		"/home/ubuntu/.oneclick/exports/0123456789abcdef/../../etc/passwd",
		"/home/ubuntu/.oneclick/exports/not-an-id/20260824-091500/database.sql",
		"/home/ubuntu/.oneclick/exports/0123456789abcdef/20260824-091500/wp-config.php",
	} {
		if safeGuestExport.MatchString(value) {
			t.Fatalf("unexpected guest export path accepted: %s", value)
		}
	}
}

func TestDownloadIgnoresOnlyKnownNTFSPermissionWarning(t *testing.T) {
	if !ignorableDownloadPermissionError(errors.New("[sftp] cannot set permissions for local file D:\\backup.sql")) {
		t.Fatal("expected exact post-copy NTFS warning to be accepted")
	}
	for _, value := range []string{"ssh timeout", "permission denied", "cannot create local file"} {
		if ignorableDownloadPermissionError(errors.New(value)) {
			t.Fatalf("unexpected transfer error accepted: %s", value)
		}
	}
}
