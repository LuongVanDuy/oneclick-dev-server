package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryPersistsMultipleProjectsAndEnvironmentHistory(t *testing.T) {
	root := t.TempDir()
	first := createStaticProject(t, filepath.Join(root, "first"))
	second := createStaticProject(t, filepath.Join(root, "second"))
	repository, err := New(filepath.Join(root, "data", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if _, err := repository.SaveConfiguration(ProjectInput{Path: path, Name: filepath.Base(path), Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "quick"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.MarkEnvironmentStarted(first, "oneclick-first-test"); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentFinished(first, EnvironmentOutcome{Success: true, VMName: "oneclick-first-test", VMState: "Running", IP: "10.0.0.2", Message: "Sẵn sàng"}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Projects) != 2 {
		t.Fatalf("got %d projects", len(snapshot.Projects))
	}
	var ready *ProjectRecord
	for index := range snapshot.Projects {
		if snapshot.Projects[index].Path == first {
			ready = &snapshot.Projects[index]
		}
	}
	if ready == nil || ready.Stage != "environment_ready" || len(ready.History) < 3 {
		t.Fatalf("unexpected ready record: %#v", ready)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "state.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRecordBackupPreservesDeploymentState(t *testing.T) {
	root := t.TempDir()
	projectPath := createStaticProject(t, filepath.Join(root, "site"))
	repository, _ := New(filepath.Join(root, "state.json"))
	if _, err := repository.SaveConfiguration(ProjectInput{Path: projectPath, Name: "Site", Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "named"}); err != nil {
		t.Fatal(err)
	}
	before, found, err := repository.GetProject(projectPath)
	if err != nil || !found {
		t.Fatalf("project unavailable: %v", err)
	}
	if err := repository.RecordBackup(projectPath, true, "Đã tạo backup", ""); err != nil {
		t.Fatal(err)
	}
	after, found, err := repository.GetProject(projectPath)
	if err != nil || !found {
		t.Fatalf("project unavailable after backup: %v", err)
	}
	if after.Stage != before.Stage || after.Status != before.Status || after.LastError != before.LastError {
		t.Fatalf("backup changed deployment state: before=%#v after=%#v", before, after)
	}
	last := after.History[len(after.History)-1]
	if last.Action != "backup" || last.Status != "success" {
		t.Fatalf("unexpected backup history: %#v", last)
	}
}

func TestRecordSourceEditPreservesDeploymentStateAndDoesNotStoreContent(t *testing.T) {
	root := t.TempDir()
	projectPath := createStaticProject(t, filepath.Join(root, "site"))
	repository, _ := New(filepath.Join(root, "state.json"))
	if _, err := repository.SaveConfiguration(ProjectInput{Path: projectPath, Name: "Site", Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "named"}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := repository.GetProject(projectPath)
	if err := repository.RecordSourceEdit(projectPath, "public/style.css", true, "Đã lưu source gốc", ""); err != nil {
		t.Fatal(err)
	}
	after, found, err := repository.GetProject(projectPath)
	if err != nil || !found {
		t.Fatalf("project unavailable after source edit: %v", err)
	}
	if after.Stage != before.Stage || after.Status != before.Status || after.LastError != before.LastError {
		t.Fatalf("source edit changed deployment state: before=%#v after=%#v", before, after)
	}
	last := after.History[len(after.History)-1]
	if last.Action != "source_edit" || last.Status != "success" || !strings.Contains(last.Message, "public/style.css") {
		t.Fatalf("unexpected source edit history: %#v", last)
	}
	encoded, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "body { secret") {
		t.Fatal("source content leaked into state")
	}
}

func TestRepositoryRecoversInterruptedOperation(t *testing.T) {
	root := t.TempDir()
	projectPath := createStaticProject(t, filepath.Join(root, "site"))
	repository, _ := New(filepath.Join(root, "state.json"))
	if _, err := repository.SaveConfiguration(ProjectInput{Path: projectPath, Name: "Site", Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "quick"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentStarted(projectPath, "oneclick-site-test"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Projects[0].Stage != "environment_failed" || snapshot.Projects[0].Status != "error" {
		t.Fatalf("operation was not recovered: %#v", snapshot.Projects[0])
	}
}

func TestRepositoryPersistsCopySnapshotAndRecoversInterruptedCopy(t *testing.T) {
	root := t.TempDir()
	projectPath := createStaticProject(t, filepath.Join(root, "site"))
	repository, _ := New(filepath.Join(root, "state.json"))
	if _, err := repository.SaveConfiguration(ProjectInput{Path: projectPath, Name: "Site", Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "quick"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentStarted(projectPath, "oneclick-site-test"); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentFinished(projectPath, EnvironmentOutcome{Success: true, VMName: "oneclick-site-test", VMState: "Running", Message: "Sẵn sàng"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkCopyStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkCopyFinished(projectPath, CopyOutcome{Success: true, SnapshotID: "1234567890abcdef", Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GuestPath: "/var/lib/oneclick/deployments/test", FileCount: 2, TotalBytes: 10, Message: "Đã sao chép"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	record := snapshot.Projects[0]
	if record.Stage != "source_ready" || record.SnapshotFiles != 2 || record.SnapshotBytes != 10 || record.SnapshotID != "1234567890abcdef" {
		t.Fatalf("unexpected copy record: %#v", record)
	}
	if err := os.MkdirAll(filepath.Join(projectPath, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveConfiguration(ProjectInput{Path: projectPath, Name: "Site", Kind: "static", DocumentRoot: "public", PHPVersion: "none", TunnelMode: "quick"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Projects[0].Stage != "environment_ready" || snapshot.Projects[0].SnapshotID != "" {
		t.Fatalf("configuration change did not invalidate the copied source: %#v", snapshot.Projects[0])
	}
	if err := repository.MarkCopyStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Projects[0].Stage != "copy_failed" || snapshot.Projects[0].Status != "error" {
		t.Fatalf("copy interruption was not recovered: %#v", snapshot.Projects[0])
	}
}

func TestRepositoryPersistsAndRecoversRuntimeState(t *testing.T) {
	root := t.TempDir()
	projectPath := createStaticProject(t, filepath.Join(root, "site"))
	repository, _ := New(filepath.Join(root, "state.json"))
	if _, err := repository.SaveConfiguration(ProjectInput{Path: projectPath, Name: "Site", Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "quick"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentStarted(projectPath, "oneclick-site-test"); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentFinished(projectPath, EnvironmentOutcome{Success: true, VMName: "oneclick-site-test", VMState: "Running", Message: "Sẵn sàng"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkCopyStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkCopyFinished(projectPath, CopyOutcome{
		Success: true, SnapshotID: "1234567890abcdef", Checksum: strings.Repeat("a", 64),
		GuestPath: "/var/lib/oneclick/deployments/1234567890abcdef/1234567890abcdef", Message: "Đã sao chép",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRuntimeStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Projects[0].Stage != "runtime_failed" {
		t.Fatalf("runtime interruption was not recovered: %#v", snapshot.Projects[0])
	}
	if err := repository.MarkRuntimeStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRuntimeFinished(projectPath, RuntimeOutcome{
		Success: true, Adapter: "WordPress", State: "running", Health: "healthy",
		Containers: 3, SnapshotID: "1234567890abcdef", Message: "Runtime sẵn sàng",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil {
		t.Fatal(err)
	}
	record := snapshot.Projects[0]
	if record.Stage != "runtime_ready" || record.RuntimeHealth != "healthy" || record.RuntimeContainers != 3 {
		t.Fatalf("unexpected runtime record: %#v", record)
	}
	if err := repository.MarkDatabaseStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Projects[0].Stage != "database_failed" || snapshot.Projects[0].DatabaseState != "failed" {
		t.Fatalf("database interruption was not recovered: %#v", snapshot.Projects[0])
	}
	if err := repository.MarkDatabaseStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkDatabaseFinished(projectPath, DatabaseOutcome{
		Success: true, Source: "File database", Tables: 15, Bytes: 4096,
		TablePrefix: "wp7_", Message: "Đã nhập database",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil {
		t.Fatal(err)
	}
	record = snapshot.Projects[0]
	if record.Stage != "database_ready" || record.DatabaseTables != 15 || record.DatabasePrefix != "wp7_" || record.DatabaseImportedAt == "" {
		t.Fatalf("unexpected database record: %#v", record)
	}
}

func TestRepositoryPersistsCloudflareAndNamedTunnelLifecycle(t *testing.T) {
	root := t.TempDir()
	projectPath := createStaticProject(t, filepath.Join(root, "site"))
	repository, _ := New(filepath.Join(root, "state.json"))
	if _, err := repository.SaveConfiguration(ProjectInput{Path: projectPath, Name: "Site", Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "named"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveCloudflareConnection(CloudflareConnection{
		Zone: "kidgrow.site", ZoneID: "zone", AccountID: "account", ZoneStatus: "active",
		NameServers: []string{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentStarted(projectPath, "oneclick-site-test"); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkEnvironmentFinished(projectPath, EnvironmentOutcome{Success: true, VMName: "oneclick-site-test", VMState: "Running", Message: "Sẵn sàng"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkCopyStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkCopyFinished(projectPath, CopyOutcome{
		Success: true, SnapshotID: "1234567890abcdef", Checksum: strings.Repeat("a", 64),
		GuestPath: "/var/lib/oneclick/deployments/1234567890abcdef/1234567890abcdef", Message: "Đã sao chép",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRuntimeStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRuntimeFinished(projectPath, RuntimeOutcome{
		Success: true, Adapter: "Static", State: "running", Health: "healthy", Containers: 3,
		SnapshotID: "1234567890abcdef", Message: "Runtime sẵn sàng",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkDatabaseStarted(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkDatabaseFinished(projectPath, DatabaseOutcome{
		Success: true, Source: "WordPress trên máy", Tables: 12, Bytes: 2048,
		TablePrefix: "wp_", Message: "Đã nhập database",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkTunnelStarted(projectPath, "demo.kidgrow.site", "kidgrow.site"); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkTunnelAllocated(projectPath, "11111111-1111-1111-1111-111111111111", strings.Repeat("d", 32)); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkTunnelFinished(projectPath, TunnelOutcome{
		Success: true, DomainZone: "kidgrow.site", Hostname: "demo.kidgrow.site", TunnelID: "11111111-1111-1111-1111-111111111111",
		DNSRecordID: strings.Repeat("d", 32), URL: "https://demo.kidgrow.site", State: "running", Health: "healthy", Message: "Domain sẵn sàng",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	record := snapshot.Projects[0]
	if len(snapshot.Domains) != 1 || snapshot.Domains[0].Zone != "kidgrow.site" || record.DomainZone != "kidgrow.site" || record.Stage != "public" || record.TunnelURL != "https://demo.kidgrow.site" || record.TunnelID == "" {
		t.Fatalf("unexpected named tunnel state: %#v / %#v", snapshot.Domains, record)
	}
	if err := repository.MarkTunnelStopping(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkTunnelStopped(projectPath, TunnelOutcome{Success: true, Message: "Đã dừng"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil {
		t.Fatal(err)
	}
	record = snapshot.Projects[0]
	if record.Stage != "database_ready" || record.Hostname != "" || record.TunnelID != "" || record.DNSRecordID != "" || record.TunnelURL != "" {
		t.Fatalf("tunnel metadata was not cleared after stop: %#v", record)
	}
	if err := repository.MarkTunnelStarted(projectPath, "retry.kidgrow.site", "kidgrow.site"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Projects[0].Stage != "tunnel_failed" || snapshot.Projects[0].Status != "error" {
		t.Fatalf("interrupted tunnel was not recovered: %#v", snapshot.Projects[0])
	}
}

func TestRemoveProjectsForgetsOnlyExactPaths(t *testing.T) {
	root := t.TempDir()
	first := createStaticProject(t, filepath.Join(root, "first"))
	second := createStaticProject(t, filepath.Join(root, "second"))
	repository, _ := New(filepath.Join(root, "state.json"))
	for _, path := range []string{first, second} {
		if _, err := repository.SaveConfiguration(ProjectInput{Path: path, Name: filepath.Base(path), Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "named"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.SaveCloudflareConnection(CloudflareConnection{Zone: "kidgrow.site", ZoneID: "zone", AccountID: "account"}); err != nil {
		t.Fatal(err)
	}

	removed, err := repository.RemoveProjects([]string{first})
	if err != nil || removed != 1 {
		t.Fatalf("unexpected removal result: %d, %v", removed, err)
	}
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Projects) != 1 || canonicalPath(snapshot.Projects[0].Path) != canonicalPath(second) {
		t.Fatalf("removed the wrong project: %#v", snapshot.Projects)
	}
	if len(snapshot.Domains) != 1 || snapshot.Domains[0].Zone != "kidgrow.site" {
		t.Fatalf("shared Cloudflare connection was removed: %#v", snapshot.Domains)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("source was modified: %v", err)
	}
}

func TestRepositoryMigratesLegacyCloudflareConnectionToDomainList(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	legacy := `{
  "schema": 1,
  "updatedAt": "2026-08-23T00:00:00Z",
  "storagePath": "",
  "cloudflare": {"zone":"kidgrow.site","zoneId":"zone-1","accountId":"account-1","zoneStatus":"active"},
  "projects": []
}`
	if err := os.WriteFile(statePath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	repository, _ := New(statePath)
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != SchemaVersion || len(snapshot.Domains) != 1 || snapshot.Domains[0].Zone != "kidgrow.site" {
		t.Fatalf("legacy domain was not migrated in memory: %#v", snapshot)
	}
	if err := repository.SaveCloudflareConnection(CloudflareConnection{Zone: "example.com", ZoneID: "zone-2", AccountID: "account-2"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"schema": 3`) || !strings.Contains(string(data), `"domains"`) || strings.Contains(string(data), `"cloudflare"`) {
		t.Fatalf("state was not persisted as schema 3: %s", data)
	}
}

func TestRepositoryLoadsSchemaTwoBeforePersistingSchemaThree(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	previous := `{
  "schema": 2,
  "updatedAt": "2026-08-23T00:00:00Z",
  "storagePath": "",
  "domains": [],
  "projects": []
}`
	if err := os.WriteFile(statePath, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	repository, _ := New(statePath)
	snapshot, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != SchemaVersion {
		t.Fatalf("schema two was not migrated in memory: %#v", snapshot)
	}
	if err := repository.SaveCloudflareConnection(CloudflareConnection{Zone: "example.com", ZoneID: "zone", AccountID: "account"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"schema": 3`) {
		t.Fatalf("schema three was not persisted: %s", data)
	}
}

func TestRepositoryManagesMultipleDomainsAndGuardsActiveUsage(t *testing.T) {
	root := t.TempDir()
	repository, _ := New(filepath.Join(root, "state.json"))
	for _, connection := range []CloudflareConnection{
		{Zone: "kidgrow.site", ZoneID: "zone-1", AccountID: "account-1"},
		{Zone: "example.com", ZoneID: "zone-2", AccountID: "account-2"},
	} {
		if err := repository.SaveCloudflareConnection(connection); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := repository.List()
	if err != nil || len(snapshot.Domains) != 2 {
		t.Fatalf("unexpected domain list: %#v / %v", snapshot.Domains, err)
	}
	snapshot.Projects = append(snapshot.Projects, ProjectRecord{
		ID: "project", Path: filepath.Join(root, "site"), Name: "Site", DomainZone: "kidgrow.site",
		Hostname: "demo.kidgrow.site", TunnelID: "tunnel", Stage: "public", Status: "ready", History: []HistoryEntry{},
	})
	if err := repository.saveLocked(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err := repository.RemoveCloudflareConnection("kidgrow.site"); err == nil {
		t.Fatal("active domain removal should be rejected")
	}
	if err := repository.RemoveCloudflareConnection("example.com"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.List()
	if err != nil || len(snapshot.Domains) != 1 || snapshot.Domains[0].Zone != "kidgrow.site" {
		t.Fatalf("wrong domain removed: %#v / %v", snapshot.Domains, err)
	}
}

func createStaticProject(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
