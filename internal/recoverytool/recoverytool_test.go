package recoverytool

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureBundleContainsCompleteCodeAndSeparatesWorkspace(t *testing.T) {
	paths, err := ensureBundle(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"pyproject.toml", filepath.Join("src", "wpclean", "gui_server.py"),
		filepath.Join("src", "wpclean", "fresh_install.py"), filepath.Join("src", "wpclean", "fresh_ui.py"),
		filepath.Join("tests", "test_gui_server.py"), filepath.Join("scripts", "preserve_clean_authors.py"),
		filepath.Join("themes", "flatsome.zip"), filepath.Join("assets", "fresh-wordpress", "bricks.2.3.10.zip"),
		filepath.Join("assets", "fresh-wordpress", "bricks-child.zip"), filepath.Join("assets", "fresh-wordpress", "duyanhwebpro-1.2.3.zip"), "oneclick_entry.py",
	} {
		if !regularFile(filepath.Join(paths.Bundle, relative)) {
			t.Fatalf("missing embedded file %s", relative)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.Bundle, "WP-Clean-Rebuild.exe")); !os.IsNotExist(err) {
		t.Fatal("the nested launcher executable must not be bundled")
	}
	if _, err := os.Stat(filepath.Join(paths.Bundle, ".venv")); !os.IsNotExist(err) {
		t.Fatal("developer virtual environment must not be bundled")
	}
	for _, directory := range []string{"sites", "backups", "reports", "repairs", "logs", ".wpclean-cache", filepath.Join("fresh-installs", "sites")} {
		info, err := os.Stat(filepath.Join(paths.Workspace, directory))
		if err != nil || !info.IsDir() {
			t.Fatalf("workspace directory %s is missing: %v", directory, err)
		}
	}
	again, err := ensureBundle(filepath.Dir(filepath.Dir(paths.Bundle)))
	if err != nil {
		t.Fatal(err)
	}
	if again.Bundle != paths.Bundle || again.Version != paths.Version {
		t.Fatal("bundle extraction is not idempotent")
	}
}

func TestLocalProxyMovesFreshInstallPasswordsIntoNativeSecretStore(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Server", "WPCleanGUI/1.0")
		if request.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Header().Set("X-OneClick-Recovery", "1")
			_, _ = io.WriteString(writer, `<html><script>fetch('/api/projects')</script></html>`)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{"name":"demo.kidgrow.site"}`)
	}))
	defer upstream.Close()

	originalSave, originalGet := saveEmbeddedRecoveryPassword, getEmbeddedRecoveryPassword
	savedName, savedValue := "", ""
	saveEmbeddedRecoveryPassword = func(name, value string) error {
		savedName, savedValue = name, value
		return nil
	}
	getEmbeddedRecoveryPassword = func(_ string) (string, error) { return savedValue, nil }
	t.Cleanup(func() {
		saveEmbeddedRecoveryPassword = originalSave
		getEmbeddedRecoveryPassword = originalGet
	})

	server, proxyURL, err := startLocalProxy(upstream.URL, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	body := `{"domain":"demo.kidgrow.site","password":"ftp-secret","dbPassword":"db-secret","adminPassword":"admin-secret"}`
	request, err := http.NewRequest(http.MethodPost, proxyURL+"api/fresh-installs", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected fresh install status: %d", response.StatusCode)
	}
	if savedName != "fresh-demo.kidgrow.site" {
		t.Fatalf("unexpected credential key: %s", savedName)
	}
	var bundle map[string]string
	if err := json.Unmarshal([]byte(savedValue), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle["ftpPassword"] != "ftp-secret" || bundle["dbPassword"] != "ftp-secret" || bundle["adminPassword"] != "ftp-secret" {
		t.Fatalf("unexpected credential bundle keys: %#v", bundle)
	}
	updateRequest, err := http.NewRequest(http.MethodPost, proxyURL+"api/fresh-installs/demo.kidgrow.site", strings.NewReader(`{"password":"","dbPassword":"db-new","adminPassword":""}`))
	if err != nil {
		t.Fatal(err)
	}
	updateResponse, err := http.DefaultClient.Do(updateRequest)
	if err != nil {
		t.Fatal(err)
	}
	updateResponse.Body.Close()
	if updateResponse.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected fresh update status: %d", updateResponse.StatusCode)
	}
	if err := json.Unmarshal([]byte(savedValue), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle["ftpPassword"] != "ftp-secret" || bundle["dbPassword"] != "ftp-secret" || bundle["adminPassword"] != "ftp-secret" {
		t.Fatalf("blank update did not preserve native credentials: %#v", bundle)
	}
}

func TestFreshWorkerProbeDoesNotPersistProfileSecrets(t *testing.T) {
	if freshInstallProfileWrite(http.MethodPost, "/api/fresh-installs/probe-workers") {
		t.Fatal("FTP worker probe must be forwarded without writing the native credential bundle")
	}
	if !freshInstallProfileWrite(http.MethodPost, "/api/fresh-installs") {
		t.Fatal("fresh profile create must still persist its credential bundle")
	}
}

func TestFreshInstallDeleteRecognizesExactLocalRecordRoute(t *testing.T) {
	name, ok := freshInstallDeletionName(http.MethodPost, "/api/fresh-installs/demo.kidgrow.site/delete")
	if !ok || name != "demo.kidgrow.site" {
		t.Fatalf("unexpected fresh delete route: name=%q ok=%v", name, ok)
	}
	if _, ok := freshInstallDeletionName(http.MethodGet, "/api/fresh-installs/demo.kidgrow.site/delete"); ok {
		t.Fatal("GET must not delete a fresh install credential")
	}
}

func TestRuntimeLoadsFreshInstallCredentialBundle(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "tool"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := ensureBundle(manager.root)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(paths.Workspace, "fresh-installs", "sites", "demo.kidgrow.site.json"), `{"name":"demo.kidgrow.site"}`)
	originalGet := getEmbeddedRecoveryPassword
	getEmbeddedRecoveryPassword = func(name string) (string, error) {
		if name == "fresh-demo.kidgrow.site" {
			return `{"ftpPassword":"ftp","dbPassword":"db","adminPassword":"admin"}`, nil
		}
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { getEmbeddedRecoveryPassword = originalGet })

	if err := manager.writeRuntimeSecrets(paths); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(paths.SecretDir, "fresh-demo.kidgrow.site.secret"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dbPassword":"db"`) {
		t.Fatal("fresh credential bundle was not restored into the protected runtime")
	}
}

func TestFreshInstallKeepsEmbeddedEngineAliveWhenAppCloses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/projects" {
			_, _ = io.WriteString(writer, `{"projects":[],"activeProject":null}`)
			return
		}
		_, _ = io.WriteString(writer, `{"installs":[{"status":"running","job":{"status":"running"}}]}`)
	}))
	defer upstream.Close()
	if !targetHasRunningJob(upstream.URL) {
		t.Fatal("active fresh WordPress install did not keep the embedded engine alive")
	}
}

func TestLegacyImportCopiesDataAndRemovesProfilePassword(t *testing.T) {
	source := filepath.Join(t.TempDir(), "legacy")
	writeTestFile(t, filepath.Join(source, "pyproject.toml"), "[project]\nname='wp-clean-rebuild'\n")
	writeTestFile(t, filepath.Join(source, "src", "wpclean", "gui_server.py"), "# marker\n")
	profile := map[string]any{
		"host": "example.com", "username": "employee", "password": "secret-value",
		"protocol": "ftps", "port": 21, "remotePath": "/domains/example.com/public_html",
	}
	profileData, _ := json.Marshal(profile)
	writeTestFile(t, filepath.Join(source, "sites", "example.json"), string(profileData))
	writeTestFile(t, filepath.Join(source, "backups", "example.com", "wp-content", "uploads", "sample.php"), "<?php echo 'evidence';")
	writeTestFile(t, filepath.Join(source, "reports", "example.com", "report.json"), "{}\n")

	stored := map[string]string{}
	originalSave, originalGet := saveEmbeddedRecoveryPassword, getEmbeddedRecoveryPassword
	saveEmbeddedRecoveryPassword = func(name, password string) error { stored[name] = password; return nil }
	getEmbeddedRecoveryPassword = func(name string) (string, error) { return stored[name], nil }
	t.Cleanup(func() {
		saveEmbeddedRecoveryPassword = originalSave
		getEmbeddedRecoveryPassword = originalGet
	})

	manager, err := New(filepath.Join(t.TempDir(), "oneclick-tool"))
	if err != nil {
		t.Fatal(err)
	}
	plan := manager.LegacyImportPlan(source)
	if !plan.Ready || plan.Files != 3 || plan.Profiles != 1 || plan.Secrets != 1 || plan.Conflicts != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	result := manager.ImportLegacy(context.Background(), source, nil)
	if !result.Success || result.Files != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if stored["example"] != "secret-value" {
		t.Fatal("password was not moved to the secret store")
	}
	destinationProfile := filepath.Join(plan.DestinationPath, "sites", "example.json")
	data, err := os.ReadFile(destinationProfile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-value") || strings.Contains(string(data), `"password"`) {
		t.Fatal("destination profile still contains the password")
	}
	sourceData, err := os.ReadFile(filepath.Join(source, "sites", "example.json"))
	if err != nil || !strings.Contains(string(sourceData), "secret-value") {
		t.Fatal("copy-only import changed the legacy source")
	}
	if !regularFile(filepath.Join(plan.DestinationPath, "backups", "example.com", "wp-content", "uploads", "sample.php")) {
		t.Fatal("backup evidence was not copied")
	}
	retryPlan := manager.LegacyImportPlan(source)
	if !retryPlan.Ready || retryPlan.ExistingFiles != retryPlan.Files || retryPlan.Conflicts != 0 {
		t.Fatalf("retry plan is not idempotent: %+v", retryPlan)
	}
	retry := manager.ImportLegacy(context.Background(), source, nil)
	if !retry.Success || retry.Skipped != retryPlan.Files {
		t.Fatalf("retry did not skip verified existing files: %+v", retry)
	}
}

func TestLegacyImportRejectsNestedDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "legacy")
	writeTestFile(t, filepath.Join(source, "pyproject.toml"), "[project]\n")
	writeTestFile(t, filepath.Join(source, "src", "wpclean", "gui_server.py"), "# marker\n")
	writeTestFile(t, filepath.Join(source, "reports", "item.json"), "{}\n")
	_, _, err := scanLegacySource(source, filepath.Join(source, "data", "workspace"))
	if err == nil || !strings.Contains(err.Error(), "lồng") {
		t.Fatalf("expected nested path rejection, got %v", err)
	}
}

func TestLegacyImportDetectsSameSizeAndTimestampContentConflict(t *testing.T) {
	source := filepath.Join(t.TempDir(), "legacy")
	writeTestFile(t, filepath.Join(source, "pyproject.toml"), "[project]\n")
	writeTestFile(t, filepath.Join(source, "src", "wpclean", "gui_server.py"), "# marker\n")
	sourceFile := filepath.Join(source, "reports", "report.json")
	writeTestFile(t, sourceFile, "source-content")

	manager, err := New(filepath.Join(t.TempDir(), "oneclick-tool"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := ensureBundle(manager.root)
	if err != nil {
		t.Fatal(err)
	}
	destinationFile := filepath.Join(paths.Workspace, "reports", "report.json")
	writeTestFile(t, destinationFile, "target-content")
	sourceInfo, err := os.Stat(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(destinationFile, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
		t.Fatal(err)
	}

	plan := manager.LegacyImportPlan(source)
	if plan.Ready || plan.Conflicts != 1 {
		t.Fatalf("same metadata but different bytes must conflict: %+v", plan)
	}
}

func TestLocalProxyAllowsEmbeddingOnlyExpectedServer(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Server", "WPCleanGUI/1.0")
		if request.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Header().Set("X-OneClick-Recovery", "1")
			writer.Header().Set("X-Frame-Options", "DENY")
			writer.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
			_, _ = io.WriteString(writer, `<html><title>Khôi phục WordPress</title><script>fetch('/api/projects')</script></html>`)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"projects":[]}`)
	}))
	upstream.Listener = listener
	upstream.Start()
	defer upstream.Close()

	server, proxyURL, err := startLocalProxy(upstream.URL, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	response, err := http.Get(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("X-Frame-Options") != "" {
		t.Fatal("proxy did not remove frame denial")
	}
	policy := response.Header.Get("Content-Security-Policy")
	if strings.Contains(policy, "frame-ancestors 'none'") || !strings.Contains(policy, "object-src 'none'") {
		t.Fatalf("unexpected embedded CSP: %s", policy)
	}
}

func TestProbeRejectsRecoveryServerWithoutPrivateIdentityHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Server", "WPCleanGUI/1.0")
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, `<html><title>Khôi phục WordPress</title><script>fetch('/api/projects')</script></html>`)
	}))
	defer upstream.Close()

	if probeWPClean(upstream.URL) {
		t.Fatal("recovery probe accepted an upstream without the OneClick identity header")
	}
}

func TestLocalProxyDeletesEmbeddedCredentialWhenLocalProjectDeleteStarts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Server", "WPCleanGUI/1.0")
		if request.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Header().Set("X-OneClick-Recovery", "1")
			_, _ = io.WriteString(writer, `<html><title>Khôi phục WordPress</title><script>fetch('/api/projects')</script></html>`)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(writer, `{"ok":true,"status":"running"}`)
	}))
	defer upstream.Close()

	originalDelete := deleteEmbeddedRecoveryPassword
	deletedProject := ""
	deleteEmbeddedRecoveryPassword = func(projectName string) error {
		deletedProject = projectName
		return nil
	}
	t.Cleanup(func() { deleteEmbeddedRecoveryPassword = originalDelete })

	server, proxyURL, err := startLocalProxy(upstream.URL, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, proxyURL+"api/projects/old-import/delete", strings.NewReader(`{"confirmation":"old-import"}`))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected delete status: %d", response.StatusCode)
	}
	if deletedProject != "old-import" {
		t.Fatalf("credential deletion used %q", deletedProject)
	}
}

func TestLiveEmbeddedRuntime(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_RECOVERY_TOOL") != "1" {
		t.Skip("set ONECLICK_LIVE_RECOVERY_TOOL=1 to install and start the embedded runtime")
	}
	manager, err := New(filepath.Join(t.TempDir(), "wp-clean-runtime"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	defer manager.Close(context.Background())

	prepared := manager.Prepare(ctx, nil)
	if !prepared.Ready {
		t.Fatalf("embedded runtime was not prepared: %+v", prepared)
	}
	started := manager.Start(ctx)
	if !started.Running || started.URL == "" {
		t.Fatalf("embedded runtime did not start: %+v", started)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(started.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Khôi phục WordPress") {
		t.Fatalf("unexpected embedded page: status=%d bytes=%d", response.StatusCode, len(body))
	}
	if response.Header.Get("X-Frame-Options") != "" || strings.Contains(response.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("embedded page still denies OneClick framing")
	}
	paths, err := ensureBundle(manager.root)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	upstreamTarget := manager.targetURL
	manager.mu.Unlock()
	manager.Close(context.Background())
	deadline := time.Now().Add(3 * time.Second)
	for probeWPClean(upstreamTarget) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if probeWPClean(upstreamTarget) {
		t.Fatal("embedded runtime is still serving after an idle shutdown")
	}
	if _, err := os.Stat(paths.SecretDir); !os.IsNotExist(err) {
		t.Fatalf("runtime secret directory remains after shutdown: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Runtime, "server-state.json")); !os.IsNotExist(err) {
		t.Fatalf("server state remains after shutdown: %v", err)
	}
}

func TestRuntimeStatusUsesStartMessageWhenPythonAlreadyExists(t *testing.T) {
	root := t.TempDir()
	manager, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := bundlePaths{
		Version:   strings.Repeat("a", 64),
		Runtime:   filepath.Join(root, "runtime"),
		Workspace: filepath.Join(root, "workspace"),
	}
	python := runtimePython(paths)
	if err := os.MkdirAll(filepath.Dir(python), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("installed"), 0o600); err != nil {
		t.Fatal(err)
	}

	status := manager.runtimeStatus(paths)
	if status.State != "start_required" || !status.SetupRequired {
		t.Fatalf("expected installed runtime to require start, got %+v", status)
	}
	if status.Message != "Khởi động môi trường Khôi phục WP" || !strings.Contains(status.Detail, "đã có sẵn") {
		t.Fatalf("installed runtime used misleading setup text: %+v", status)
	}
}

func TestLiveLegacyDataScan(t *testing.T) {
	source := strings.TrimSpace(os.Getenv("ONECLICK_LEGACY_SCAN_SOURCE"))
	if source == "" {
		t.Skip("set ONECLICK_LEGACY_SCAN_SOURCE to scan a real legacy workspace without copying it")
	}
	manager, err := New(filepath.Join(t.TempDir(), "wp-clean-scan"))
	if err != nil {
		t.Fatal(err)
	}
	plan := manager.LegacyImportPlan(source)
	if !plan.Ready || plan.Files == 0 || plan.Bytes == 0 {
		t.Fatalf("legacy workspace was not accepted: %+v", plan)
	}
	t.Logf("scan ready: files=%d bytes=%d profiles=%d secrets=%d", plan.Files, plan.Bytes, plan.Profiles, plan.Secrets)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
