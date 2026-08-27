package vm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceNameIsStableAndSafe(t *testing.T) {
	name := instanceName(`D:\laragon\www\Nội bộ`, "Nội bộ / Khách hàng 2026")
	if name != "oneclick-server" {
		t.Fatalf("unexpected name: %s", name)
	}
	if len(name) > 63 {
		t.Fatalf("name is too long: %d", len(name))
	}
	if name != instanceName(`D:\laragon\www\Nội bộ`, "Nội bộ / Khách hàng 2026") {
		t.Fatal("instance name is not stable")
	}
}

func TestInstanceNameFallsBackForNonASCIIName(t *testing.T) {
	name := instanceName(`D:\web\du-an`, "Dự án")
	if name != "oneclick-server" {
		t.Fatalf("unexpected name: %s", name)
	}
}

func TestHasMounts(t *testing.T) {
	for _, value := range []string{"", "null", "[]", "{}", "{\n  }", "[\n  ]"} {
		if hasMounts(json.RawMessage(value)) {
			t.Fatalf("expected %q to be empty", value)
		}
	}
	if !hasMounts(json.RawMessage(`{"host":"/home"}`)) {
		t.Fatal("expected a mount to be detected")
	}
	if !hasMounts(json.RawMessage(`{"broken"`)) {
		t.Fatal("malformed mount data must fail closed")
	}
}

func TestCleanProcessOutputRemovesTerminalControlCodes(t *testing.T) {
	input := "\x1b[2K\rConfiguring instance\n\x1b[31mfailed\x1b[0m\ufffd"
	got := cleanProcessOutput(input)
	if got != "Configuring instance\nfailed" {
		t.Fatalf("unexpected cleaned output: %q", got)
	}
}

func TestCloudInitAvoidsHostTimezoneBackgroundUpgradeAndAmbiguousMode(t *testing.T) {
	for _, expected := range []string{"timezone: Etc/UTC", "package_update: false", "package_upgrade: false", "owner=oneclick", "role=shared-server"} {
		if !strings.Contains(cloudInit, expected) {
			t.Fatalf("cloud-init is missing %q", expected)
		}
	}
	if strings.Contains(cloudInit, "permissions:") {
		t.Fatal("Multipass rewrites quoted 0644 as numeric 420; rely on cloud-init's safe 0644 default")
	}
}

func TestCleanProcessOutputRemovesMultipassSpinner(t *testing.T) {
	input := `Starting oneclick-test  /-\|/-\|/-\|/-\|/-\|/-\|/-\|`
	got := cleanProcessOutput(input)
	if got != "Starting oneclick-test" {
		t.Fatalf("unexpected cleaned spinner output: %q", got)
	}
}

func TestParseInstanceInfoSupportsMultipass116Shape(t *testing.T) {
	input := `{"errors":[],"info":{"oneclick-test":{"cpu_count":"2","ipv4":["10.0.2.15"],"mounts":{},"release":"Ubuntu 24.04.3 LTS","state":"Running"}}}`
	info, err := parseInstanceInfo(input, "oneclick-test")
	if err != nil {
		t.Fatal(err)
	}
	if info.State != "Running" || int(info.CPUCount) != 2 || firstIP(info.IPv4) != "10.0.2.15" {
		t.Fatalf("unexpected info: %#v", info)
	}
}

func TestNetworkPendingRequiresPoweredVMWithoutUsableIP(t *testing.T) {
	if !networkPending(listedInstance{State: "Starting"}, true) {
		t.Fatal("expected Starting VM without IP to be pending")
	}
	if !networkPending(listedInstance{State: "Running", IPv4: []string{"N/A"}}, true) {
		t.Fatal("expected Running VM with N/A IP to be pending")
	}
	if networkPending(listedInstance{State: "Running", IPv4: []string{"10.0.2.15"}}, true) {
		t.Fatal("did not expect VM with usable IP to be pending")
	}
	if networkPending(listedInstance{}, false) {
		t.Fatal("did not expect missing VM to be pending")
	}
}

func TestNetworkFailureNoLongerLoopsOnWindowsRestart(t *testing.T) {
	result := launchNetworkFailure("windows", "virtualbox", "oneclick-test")
	if result.RebootRequired || !result.CanRecreate {
		t.Fatalf("unexpected Windows recovery result: %#v", result)
	}
	if result.VMName != "oneclick-test" || result.State != "Starting" {
		t.Fatalf("missing VM identity: %#v", result)
	}
}

func TestOtherNetworkFailureAllowsConfirmedRecreate(t *testing.T) {
	result := launchNetworkFailure("linux", "qemu", "oneclick-test")
	if result.RebootRequired || !result.CanRecreate {
		t.Fatalf("unexpected non-Windows recovery result: %#v", result)
	}
}

func TestRunningInstanceCanBeRecreatedOnlyWhenIPAndSSHAreUnavailable(t *testing.T) {
	sshFailure := errors.New("ssh timeout")
	if !runningInstanceCanBeRecreated(listedInstance{State: "Running", IPv4: []string{"N/A"}}, sshFailure) {
		t.Fatal("expected unreachable Running VM to be eligible for confirmed recreation")
	}
	if runningInstanceCanBeRecreated(listedInstance{State: "Running", IPv4: []string{"10.0.2.15"}}, sshFailure) {
		t.Fatal("VM with usable IP must not be eligible for recreation")
	}
	if runningInstanceCanBeRecreated(listedInstance{State: "Running", IPv4: []string{"N/A"}}, nil) {
		t.Fatal("VM with working SSH must not be eligible for recreation")
	}
	if runningInstanceCanBeRecreated(listedInstance{State: "Suspended"}, sshFailure) {
		t.Fatal("helper only classifies Running instances")
	}
}

func TestLiveExistingEnvironment(t *testing.T) {
	projectPath := os.Getenv("ONECLICK_LIVE_PROJECT_PATH")
	if projectPath == "" {
		t.Skip("set ONECLICK_LIVE_PROJECT_PATH to run the local Multipass integration test")
	}
	result := Create(nil, Request{ProjectPath: projectPath, ProjectName: filepath.Base(projectPath)}, nil)
	expectFresh := os.Getenv("ONECLICK_LIVE_EXPECT_FRESH") == "1"
	if !result.Success || result.Reused == expectFresh {
		t.Fatalf("live environment did not pass: %#v", result)
	}
}
