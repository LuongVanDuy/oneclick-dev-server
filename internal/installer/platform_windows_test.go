//go:build windows

package installer

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestTrustedDownloadURL(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"https://github.com/canonical/multipass/releases/download/file.msi", true},
		{"https://release-assets.githubusercontent.com/file.msi", true},
		{"http://github.com/file.msi", false},
		{"https://github.com.example.org/file.msi", false},
		{"https://example.org/file.msi", false},
	}
	for _, test := range tests {
		parsed, err := url.Parse(test.value)
		if err != nil {
			t.Fatal(err)
		}
		if got := isTrustedDownloadURL(parsed); got != test.want {
			t.Errorf("isTrustedDownloadURL(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestTrustedVirtualBoxDownloadURL(t *testing.T) {
	for value, want := range map[string]bool{
		"https://download.virtualbox.org/virtualbox/7.1.18/file.exe": true,
		"http://download.virtualbox.org/virtualbox/file.exe":         false,
		"https://download.virtualbox.org.example.com/file.exe":       false,
	} {
		parsed, err := url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		if got := isTrustedVirtualBoxDownloadURL(parsed); got != want {
			t.Errorf("trusted VirtualBox URL %q = %v, want %v", value, got, want)
		}
	}
}

func TestVirtualBoxElevationScriptDoesNotDependOnElevatedEnvironment(t *testing.T) {
	script := buildVirtualBoxElevationScript(`C:\Temp\virtual'box.exe`, `C:\Temp\result.txt`)
	for _, unexpected := range []string{"ONECLICK_VBOX_INSTALLER", "ONECLICK_VBOX_RESULT", "ONECLICK_VBOX_SHA256"} {
		if strings.Contains(script, unexpected) {
			t.Fatalf("script still depends on %s", unexpected)
		}
	}
	if !strings.Contains(script, `$installer='C:\Temp\virtual''box.exe'`) {
		t.Fatalf("installer path was not embedded safely: %s", script)
	}
	if !strings.Contains(script, `$resultPath='C:\Temp\result.txt'`) {
		t.Fatalf("result path was not embedded safely: %s", script)
	}
	if !strings.Contains(script, strings.ToUpper(virtualBoxSHA256)) {
		t.Fatal("expected checksum was not embedded")
	}
	for _, expected := range []string{"Oracle VirtualBox*", "msiexec.exe", "'/x'", "Stop-Service -Name Multipass", "VirtualBox-stale-"} {
		if !strings.Contains(script, expected) {
			t.Fatalf("clean replacement script is missing %q", expected)
		}
	}
}

func TestVirtualBoxElevatedPowerShellIsHidden(t *testing.T) {
	if !strings.Contains(virtualBoxLauncher, "-WindowStyle Hidden") {
		t.Fatalf("elevated PowerShell must be hidden: %s", virtualBoxLauncher)
	}
	if !strings.Contains(virtualBoxLauncher, "-Verb RunAs") {
		t.Fatal("launcher must keep the explicit UAC prompt")
	}
}

func TestVirtualBoxElevationScriptParsesInWindowsPowerShell(t *testing.T) {
	script := buildVirtualBoxElevationScript(`C:\Temp\virtualbox.exe`, `C:\Temp\result.txt`)
	parseOnly := `$tokens=$null; $errors=$null; [System.Management.Automation.Language.Parser]::ParseInput([Console]::In.ReadToEnd(),[ref]$tokens,[ref]$errors) | Out-Null; if($errors.Count -gt 0){ $errors | ForEach-Object { $_.Message }; exit 1 }`
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", parseOnly)
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("elevated script is not valid Windows PowerShell: %s", strings.TrimSpace(string(output)))
	}
}

func TestLiveInstallPinnedVirtualBox(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_INSTALL_VIRTUALBOX") != "1" {
		t.Skip("set ONECLICK_LIVE_INSTALL_VIRTUALBOX=1 to run the verified installer integration test")
	}
	result := Run(context.Background(), ActionInstallVirtualBox, nil)
	if !result.Success {
		t.Fatalf("VirtualBox install failed: %#v", result)
	}
}
