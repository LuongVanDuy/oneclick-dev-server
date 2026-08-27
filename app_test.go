package main

import (
	"os"
	"path/filepath"
	"testing"

	"oneclick-dev-server/internal/sourceeditor"
	appstate "oneclick-dev-server/internal/state"
)

func TestSelectCloudflareConnectionUsesChosenOrLongestMatchingZone(t *testing.T) {
	connections := []appstate.CloudflareConnection{
		{Zone: "example.com", ZoneID: "root"},
		{Zone: "team.example.com", ZoneID: "nested"},
	}
	selected, err := selectCloudflareConnection(connections, "demo.team.example.com", "")
	if err != nil || selected.ZoneID != "nested" {
		t.Fatalf("expected longest matching zone, got %#v / %v", selected, err)
	}
	selected, err = selectCloudflareConnection(connections, "demo.team.example.com", "example.com")
	if err != nil || selected.ZoneID != "root" {
		t.Fatalf("expected explicit project zone, got %#v / %v", selected, err)
	}
	if _, err := selectCloudflareConnection(connections, "demo.other.net", ""); err == nil {
		t.Fatal("unmanaged hostname should be rejected")
	}
}

func TestSharedServerDataGuardChecksEveryProject(t *testing.T) {
	if sharedServerContainsProjectData(appstate.Snapshot{Projects: []appstate.ProjectRecord{{VMName: "oneclick-server"}}}) {
		t.Fatal("an empty shared server was incorrectly marked as containing website data")
	}
	fields := []appstate.ProjectRecord{
		{SnapshotID: "snapshot"},
		{RuntimeState: "running"},
		{DatabaseState: "ready"},
		{TunnelID: "tunnel"},
		{DNSRecordID: "dns"},
	}
	for _, record := range fields {
		snapshot := appstate.Snapshot{Projects: []appstate.ProjectRecord{{}, record}}
		if !sharedServerContainsProjectData(snapshot) {
			t.Fatalf("shared server data was not detected: %#v", record)
		}
	}
}

func TestSourceEditorBridgeUsesStoredProjectAndRecordsSave(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(site, "index.html")
	if err := os.WriteFile(indexPath, []byte("<h1>Ban dau</h1>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repository, err := appstate.New(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveConfiguration(appstate.ProjectInput{Path: site, Name: "Site", Kind: "static", DocumentRoot: ".", PHPVersion: "none", TunnelMode: "named"}); err != nil {
		t.Fatal(err)
	}
	app := &App{state: repository, sourceBackupRoot: filepath.Join(root, "source-edits")}
	listing, err := app.ListProjectSource(site, "")
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Path != "index.html" {
		t.Fatalf("unexpected source listing: %#v / %v", listing, err)
	}
	opened, err := app.ReadProjectSourceFile(site, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	saved := app.SaveProjectSourceFile(sourceeditor.SaveRequest{ProjectPath: site, Path: opened.Path, Content: "<h1>Da sua</h1>\n", ExpectedSHA: opened.SHA256})
	if !saved.Success || saved.BackupPath == "" {
		t.Fatalf("source bridge save failed: %#v", saved)
	}
	record, found, err := repository.GetProject(site)
	if err != nil || !found {
		t.Fatalf("project missing after save: %v", err)
	}
	last := record.History[len(record.History)-1]
	if last.Action != "source_edit" || last.Status != "success" {
		t.Fatalf("source edit was not audited: %#v", last)
	}
}
