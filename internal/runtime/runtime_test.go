package runtimeenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appstate "oneclick-dev-server/internal/state"
	"oneclick-dev-server/internal/vm"
)

func TestGuestRuntimeBashSyntax(t *testing.T) {
	bash := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
	if _, err := os.Stat(bash); err != nil {
		t.Skip("Git Bash is unavailable")
	}
	path := filepath.Join(t.TempDir(), "runtime.sh")
	if err := os.WriteFile(path, []byte(guestInstaller), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
		if strings.Contains(string(output), "couldn't create signal pipe") {
			t.Skip("Git Bash is blocked by the current Windows sandbox")
		}
		t.Fatalf("bash syntax: %v: %s", err, output)
	}
}

func TestVersionLockAllowsOnlyNativeUbuntuPackages(t *testing.T) {
	lock, err := loadVersionLock()
	if err != nil {
		t.Fatal(err)
	}
	if lock.RuntimeRevision != "native-v2" || len(lock.Packages) < 10 {
		t.Fatalf("unexpected native lock: %#v", lock)
	}
	joined := strings.Join(lock.Packages, " ")
	for _, forbidden := range []string{"docker.io", "docker-compose-v2", "containerd"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("native lock contains %s", forbidden)
		}
	}
}

func TestGuestRuntimePolicySeparatesUsersFilesDatabaseAndNetwork(t *testing.T) {
	for _, required := range []string{
		"User=$SITE_USER", "NoNewPrivileges=yes", "ProtectSystem=strict", "PrivateTmp=yes",
		"RestrictAddressFamilies=AF_UNIX", "IPAddressDeny=any", "CapabilityBoundingSet=",
		"MemoryMax=512M", "TasksMax=128", "open_basedir", "disable_functions",
		"setfacl -b \"$SITE_ROOT/wp-config.php\"", "runuser -u \"$other\" -- test -r",
		"CREATE USER IF NOT EXISTS '$DB_USER'@'localhost'", "REVOKE ALL PRIVILEGES, GRANT OPTION",
		"MAX_USER_CONNECTIONS 8 MAX_STATEMENT_TIME 30", "CREATE TEMPORARY TABLES, LOCK TABLES ON $DB_NAME.*",
		"bind-address=127.0.0.1", "local-infile=0", "secure-file-priv=",
		"listen $ORIGIN_IP:8080", "limit_req zone=oneclick_per_ip_rate", "limit_conn oneclick_per_ip_conn 20",
		"DISALLOW_FILE_EDIT", "DISALLOW_FILE_MODS", "error_log = $PRIVATE_ROOT/php-fpm.log",
		"runuser -u \"$SITE_USER\" -- /usr/sbin/php-fpm8.3 --test", "StartLimitBurst=3",
		"RuntimeDirectoryMode=0710",
		"ExecStartPre=+/usr/bin/setfacl -m m::x,u:www-data:--x $PHP_RUNTIME_DIR", "grep -q '^mask::--x'",
		"until [ -S $PHP_SOCKET ]", "systemctl reset-failed \"$PHP_UNIT\"",
	} {
		if !strings.Contains(guestInstaller, required) {
			t.Fatalf("guest policy is missing %q", required)
		}
	}
	for _, forbidden := range []string{"docker ", "docker.io", "docker-compose", "containerd", "0.0.0.0:8080", "'@'%'", "GRANT ALL PRIVILEGES", "error_log = /proc/self/fd/2"} {
		if strings.Contains(guestInstaller, forbidden) {
			t.Fatalf("guest policy contains forbidden setting %q", forbidden)
		}
	}
}

func TestDiagnosticTailDropsLayerNoiseAndKeepsCause(t *testing.T) {
	input := strings.Repeat("abc: Pulling fs layer\nabc: Download complete\n", 50) + "failed to copy: TLS handshake timeout"
	detail := diagnosticTail(input, 300)
	if strings.Contains(detail, "Pulling fs layer") || strings.Contains(detail, "Download complete") {
		t.Fatalf("layer noise was kept: %q", detail)
	}
	if !strings.Contains(detail, "TLS handshake timeout") {
		t.Fatalf("final cause was lost: %q", detail)
	}
}

func TestPlanRejectsSnapshotOutsideDeploymentStore(t *testing.T) {
	config := Config{
		ProjectID: "1234567890abcdef", ProjectPath: `D:\site`, ProjectName: "Site", ProjectKind: "wordpress",
		VMName: "oneclick-site", SnapshotID: "abcdef1234567890",
		SnapshotHash: strings.Repeat("a", 64), SnapshotPath: "/tmp/abcdef1234567890",
	}
	if _, err := GetPlan(config, false); err == nil {
		t.Fatal("unsafe snapshot path was accepted")
	}
}

func TestLiveGuestInstallerSyntax(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_RUNTIME") != "1" {
		t.Skip("set ONECLICK_LIVE_RUNTIME=1 for the dedicated VM syntax check")
	}
	file, err := os.CreateTemp("", "oneclick-runtime-syntax-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(guestInstaller); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: `D:\laragon\www\flatsome`, ProjectName: "flatsome"})
	if err != nil {
		t.Fatal(err)
	}
	guestPath := "/home/ubuntu/.oneclick/runtime-syntax.sh"
	if err := guest.Transfer(ctx, path, guestPath); err != nil {
		t.Fatal(err)
	}
	defer guest.Exec(context.Background(), "rm", "-f", guestPath)
	if _, err := guest.Exec(ctx, "bash", "-n", guestPath); err != nil {
		t.Fatal(err)
	}
}

func TestLiveInstalledNativePhaseReturns(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_RUNTIME_INSTALL") != "1" {
		t.Skip("set ONECLICK_LIVE_RUNTIME_INSTALL=1 for the idempotent native phase check")
	}
	lock, err := loadVersionLock()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp("", "oneclick-runtime-install-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(guestInstaller); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: `D:\laragon\www\flatsome`, ProjectName: "flatsome"})
	if err != nil {
		t.Fatal(err)
	}
	guestPath := "/home/ubuntu/.oneclick/runtime-install-check.sh"
	if err := guest.Transfer(ctx, path, guestPath); err != nil {
		t.Fatal(err)
	}
	defer guest.Exec(context.Background(), "rm", "-f", guestPath)
	_, err = guest.Exec(ctx,
		"sudo", "bash", guestPath, "install",
		"4c3c7c2dba7e8f1a", "/var/lib/oneclick/deployments/4c3c7c2dba7e8f1a/3c2684fd24d4a40a",
		"3c2684fd24d4a40a", "3c2684fd24d4a40a5bb7774e0c1a6ab810809f4d135516040aa796efc4b08345",
		".", lock.RuntimeRevision, strings.Join(lock.Packages, ","),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLiveFullRuntimeAndPersistState(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_FULL_RUNTIME") != "1" {
		t.Skip("set ONECLICK_LIVE_FULL_RUNTIME=1 to provision the real flatsome runtime")
	}
	repository, err := appstate.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.List(); err != nil {
		t.Fatal(err)
	}
	record, found, err := repository.GetProject(`D:\laragon\www\flatsome`)
	if err != nil || !found {
		t.Fatalf("flatsome state is unavailable: found=%v err=%v", found, err)
	}
	if err := repository.MarkRuntimeStarted(record.Path); err != nil {
		t.Fatal(err)
	}
	config := Config{
		ProjectID: record.ID, ProjectPath: record.Path, ProjectName: record.Name,
		ProjectKind: record.Kind, DocumentRoot: record.DocumentRoot, VMName: record.VMName,
		SnapshotID: record.SnapshotID, SnapshotHash: record.SnapshotHash, SnapshotPath: record.SnapshotPath,
	}
	lastStage := ""
	result := Provision(context.Background(), config, func(progress Progress) {
		if progress.Stage != lastStage || progress.Percent%10 == 0 {
			t.Logf("%s %d%% %s", progress.Stage, progress.Percent, progress.Message)
			lastStage = progress.Stage
		}
	})
	persistErr := repository.MarkRuntimeFinished(record.Path, appstate.RuntimeOutcome{
		Success: result.Success, Adapter: result.Adapter, State: result.RuntimeState,
		Health: result.Health, Containers: result.Services, SnapshotID: result.SnapshotID,
		Message: result.Message, Detail: result.Detail,
	})
	if persistErr != nil {
		t.Fatal(persistErr)
	}
	if !result.Success {
		t.Fatalf("%s: %s", result.Message, result.Detail)
	}
}
