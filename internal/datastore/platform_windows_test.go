//go:build windows

package datastore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"oneclick-dev-server/internal/hostexec"
)

func TestValidateTargetAcceptsEmptyFixedDriveDirectory(t *testing.T) {
	target := t.TempDir()
	got, err := validateTarget(target, filepath.Join(filepath.VolumeName(target)+`\`, "ProgramData", "Multipass"))
	if err != nil {
		t.Fatalf("validateTarget(%q): %v", target, err)
	}
	if !samePath(got, target) {
		t.Fatalf("got %q, want %q", got, target)
	}
}

func TestValidateTargetRejectsNonEmptyDirectory(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := validateTarget(target, filepath.Join(filepath.VolumeName(target)+`\`, "ProgramData", "Multipass"))
	if err == nil || !strings.Contains(err.Error(), "thư mục trống") {
		t.Fatalf("expected non-empty error, got %v", err)
	}
}

func TestValidateTargetRejectsDriveRoot(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + `\`
	if _, err := validateTarget(root, filepath.Join(root, "ProgramData", "Multipass")); err == nil {
		t.Fatal("expected drive root to be rejected")
	}
}

func TestValidateTargetRejectsDefaultMultipassTree(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + `\`
	defaultPath := filepath.Join(root, "ProgramData", "Multipass")
	if _, err := validateTarget(filepath.Join(defaultPath, "child"), defaultPath); err == nil {
		t.Fatal("expected default Multipass tree to be rejected")
	}
}

func TestElevationLauncherHasValidPowerShellSyntax(t *testing.T) {
	launcher := buildStorageLauncher(`D:\OneClick Dev Server\oneclick-dev-server.exe`, `C:\Users\Test User\AppData\Local\OneClickDevServer\operations\storage-test\request.json`)
	if strings.Contains(launcher, "ONECLICK_STORAGE_") {
		t.Fatal("launcher must not depend on environment after UAC")
	}
	parse := `$tokens=$null; $errors=$null; [System.Management.Automation.Language.Parser]::ParseInput($env:ONECLICK_TEST_SCRIPT,[ref]$tokens,[ref]$errors) | Out-Null; if ($errors.Count -ne 0) { throw ($errors | Out-String) }`
	command := hostexec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", parse)
	command.Env = append(os.Environ(), "ONECLICK_TEST_SCRIPT="+launcher)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell rejected elevation launcher: %v\n%s", err, output)
	}
}

func TestValidateHelperJournalPaths(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "OneClickDevServer", "operations", "storage-test")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"request.json", "result.json"} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(`{"success":false}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := validateHelperJournalPath(path, name); err != nil {
			t.Fatalf("validateHelperJournalPath(%s): %v", name, err)
		}
	}
}

func TestHelperRequestIsStrictAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	request := helperRequest{Schema: helperSchema, Operation: helperOperation, Target: `D:\oneclick-dev-server\data\multipass`, CreatedAt: "2026-08-23T03:00:00Z"}
	if err := writeJSONFile(path, request); err != nil {
		t.Fatal(err)
	}
	got, err := readHelperRequest(path)
	if err != nil {
		t.Fatalf("readHelperRequest: %v", err)
	}
	if got.Target != request.Target {
		t.Fatalf("got target %q, want %q", got.Target, request.Target)
	}
	if err := os.WriteFile(path, []byte(`{"schema":1,"operation":"configure_multipass_storage","target":"D:\\data","createdAt":"now","command":"whoami"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readHelperRequest(path); err == nil {
		t.Fatal("expected unknown command field to be rejected")
	}
}

func TestCopyBaselineCopiesRegularTree(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "data", "vault"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("baseline")
	if err := os.WriteFile(filepath.Join(source, "data", "vault", "record.json"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyBaseline(source, target); err != nil {
		t.Fatalf("copyBaseline: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "data", "vault", "record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
