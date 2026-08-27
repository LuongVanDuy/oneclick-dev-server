package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuestBackupBashSyntax(t *testing.T) {
	bash := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
	if _, err := os.Stat(bash); err != nil {
		t.Skip("Git Bash is unavailable")
	}
	path := filepath.Join(t.TempDir(), "backup.sh")
	if err := os.WriteFile(path, []byte(guestExportScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
		if strings.Contains(string(output), "couldn't create signal pipe") {
			t.Skip("Git Bash is blocked by the current Windows sandbox")
		}
		t.Fatalf("bash syntax: %v: %s", err, output)
	}
}

func TestGuestBackupPolicyExcludesRuntimeSecretsAndUsesScopedDatabase(t *testing.T) {
	for _, required := range []string{"--exclude='./wp-config.php'", "--exclude='./.env'", "mariadb-dump --defaults-extra-file=", `DB_NAME="oc_${PROJECT_ID:0:16}"`, "find \"$SITE_ROOT\" -xdev", "sha256sum \"$SOURCE_ARCHIVE\""} {
		if !strings.Contains(guestExportScript, required) {
			t.Fatalf("guest backup policy is missing %q", required)
		}
	}
	for _, forbidden := range []string{"/etc/shadow", "multipass mount", "password=$(", "cat \"$APP_CNF\""} {
		if strings.Contains(guestExportScript, forbidden) {
			t.Fatalf("guest backup policy contains forbidden text %q", forbidden)
		}
	}
}

func TestInspectFindsOnlyCompletedBackups(t *testing.T) {
	root := t.TempDir()
	config := testConfig(root)
	completed := filepath.Join(root, config.ProjectID, "20260824-090000")
	if err := os.MkdirAll(completed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(completed, "backup.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, config.ProjectID, "bad-name"), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(config)
	if err != nil {
		t.Fatal(err)
	}
	if status.BackupCount != 1 || status.LastBackupPath != completed || !status.CanBackup || !status.CanReplaceDatabase {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestInspectRequiresCompletedBackupBeforeDatabaseReplacement(t *testing.T) {
	config := testConfig(t.TempDir())
	status, err := Inspect(config)
	if err != nil {
		t.Fatal(err)
	}
	if status.CanReplaceDatabase {
		t.Fatal("database replacement must require a completed backup")
	}
}

func TestParseExportOutputRejectsMissingOrOversizedValues(t *testing.T) {
	validHash := strings.Repeat("a", 64)
	metadata, err := parseExportOutput("SOURCE_SHA=" + validHash + "\nSOURCE_BYTES=12\nDATABASE_SHA=" + validHash + "\nDATABASE_BYTES=34\n")
	if err != nil || metadata.databaseBytes != 34 {
		t.Fatalf("unexpected parse: %#v %v", metadata, err)
	}
	if _, err := parseExportOutput("SOURCE_SHA=x\nSOURCE_BYTES=0\n"); err == nil {
		t.Fatal("expected invalid output to fail")
	}
}

func TestVerifySourceArchiveRejectsSecretAndSymlink(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good.tar.gz")
	writeArchive(t, good, []archiveEntry{{name: "index.php", body: "ok", typeflag: tar.TypeReg}})
	if err := verifySourceArchive(good); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.tar.gz")
	writeArchive(t, secret, []archiveEntry{{name: "wp-config.php", body: "secret", typeflag: tar.TypeReg}})
	if err := verifySourceArchive(secret); err == nil {
		t.Fatal("expected secret file to be rejected")
	}
	link := filepath.Join(root, "link.tar.gz")
	writeArchive(t, link, []archiveEntry{{name: "escape", typeflag: tar.TypeSymlink}})
	if err := verifySourceArchive(link); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
}

func TestPreparedDirectoryRemovalCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := removePreparedDirectory(root, outside); err == nil {
		t.Fatal("expected outside removal to fail")
	}
}

type archiveEntry struct {
	name, body string
	typeflag   byte
}

func writeArchive(t *testing.T, path string, entries []archiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o600, Size: int64(len(entry.body)), Typeflag: entry.typeflag}
		if entry.typeflag == tar.TypeSymlink {
			header.Size = 0
			header.Linkname = "../outside"
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" && entry.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func testConfig(root string) Config {
	return Config{ProjectID: "0123456789abcdef", ProjectPath: filepath.Join(root, "site"), ProjectName: "Site", ProjectKind: "wordpress",
		VMName: "oneclick-server", Stage: "database_ready", RuntimeState: "running", RuntimeHealth: "healthy", DatabaseState: "ready", BackupRoot: root}
}

func TestLiveProjectBackup(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_BACKUP") != "1" {
		t.Skip("set ONECLICK_LIVE_BACKUP=1 to run the live read-only backup")
	}
	config := Config{
		ProjectID: os.Getenv("ONECLICK_LIVE_PROJECT_ID"), ProjectPath: os.Getenv("ONECLICK_LIVE_PROJECT_PATH"),
		ProjectName: os.Getenv("ONECLICK_LIVE_PROJECT_NAME"), ProjectKind: "wordpress", VMName: "oneclick-server",
		Stage: "public", RuntimeState: "running", RuntimeHealth: "healthy", DatabaseState: "ready",
		BackupRoot: os.Getenv("ONECLICK_LIVE_BACKUP_ROOT"),
	}
	result := Create(context.Background(), config, func(progress Progress) { t.Logf("%d%% %s", progress.Percent, progress.Message) })
	if !result.Success {
		t.Fatalf("live backup failed: %#v", result)
	}
	for _, name := range []string{"source.tar.gz", "database.sql", "backup.json"} {
		info, err := os.Stat(filepath.Join(result.Path, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("missing live backup %s: %v", name, err)
		}
	}
}
