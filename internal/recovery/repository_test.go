package recovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryPersistsProfileWithoutPasswordOrDerivedPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.json")
	repository, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.Save(ProjectInput{
		Name: "Công ty", Host: "FTP.Example.com.", Username: "operator",
		Protocol: "ftps", Port: 21, RemotePath: "/public_html/", SiteURL: "https://example.com/",
		Workers: 4, Passive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.ID) != 24 || project.Host != "ftp.example.com" || project.RemotePath != "/public_html" {
		t.Fatalf("unexpected normalized project: %+v", project)
	}
	project.PasswordStored = true
	project.DataPath = `D:\private`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"password", "passwordStored", `D:\\private`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("recovery.json leaked %q: %s", forbidden, text)
		}
	}
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Projects) != 1 || snapshot.StoragePath != path || snapshot.Projects[0].DataPath == "" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestRepositoryRejectsInsecureFTPWithoutExplicitConsent(t *testing.T) {
	repository, err := New(filepath.Join(t.TempDir(), "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Save(ProjectInput{
		Name: "Legacy", Host: "ftp.example.com", Username: "operator",
		Protocol: "ftp", Port: 21, RemotePath: "/public_html", Workers: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "xác nhận rủi ro") {
		t.Fatalf("expected insecure FTP consent error, got %v", err)
	}
}

func TestRepositoryRecordsConnectionAndDeletesOnlyProfile(t *testing.T) {
	root := t.TempDir()
	repository, err := New(filepath.Join(root, "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.Save(ProjectInput{
		Name: "Store", Host: "ftp.example.com", Username: "operator",
		Protocol: "ftps", Port: 21, RemotePath: "/public_html", Workers: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	dataPath, err := repository.EnsureProjectDirectory(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dataPath, "evidence.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := ConnectionResult{Success: true, Message: "Kết nối website thành công", CheckedAt: nowText()}
	if err := repository.RecordConnection(project.ID, result); err != nil {
		t.Fatal(err)
	}
	updated, ok, err := repository.Get(project.ID)
	if err != nil || !ok || updated.Status != "connection_ready" || len(updated.History) < 2 {
		t.Fatalf("unexpected recorded project: ok=%v err=%v project=%+v", ok, err, updated)
	}
	if err := repository.Delete(project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("owned evidence must remain after forgetting profile: %v", err)
	}
}

func TestRepositoryRecoversInterruptedBackupAndKeepsEvidenceMetadata(t *testing.T) {
	repository, err := New(filepath.Join(t.TempDir(), "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.Save(ProjectInput{Name: "Store", Host: "store.example.com", Username: "operator", Protocol: "ftps", Port: 21, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordBackupStarted(project.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Projects[0].Status != "backup_failed" || !strings.Contains(snapshot.Projects[0].LastError, "gián đoạn") {
		t.Fatalf("interrupted backup was not recovered: %+v", snapshot.Projects[0])
	}
	result := BackupResult{Success: true, Message: "Đã sao lưu", BackupID: "backup-test", Files: 12, Bytes: 3456, Findings: 2, Critical: 1, CreatedAt: nowText()}
	if err := repository.RecordBackup(project.ID, result); err != nil {
		t.Fatal(err)
	}
	updated, err := repository.Save(ProjectInput{ID: project.ID, Name: "Store mới", Host: "store.example.com", Username: "operator", Protocol: "ftps", Port: 21, RemotePath: "/public_html", Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastBackupID != "backup-test" || updated.BackupFiles != 12 || updated.CriticalCount != 1 {
		t.Fatalf("profile update lost backup metadata: %+v", updated)
	}
}
