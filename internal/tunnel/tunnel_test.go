package tunnel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"oneclick-dev-server/internal/vm"
)

func TestGuestTunnelBashSyntax(t *testing.T) {
	bash := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
	if _, err := os.Stat(bash); err != nil {
		t.Skip("Git Bash is unavailable")
	}
	path := filepath.Join(t.TempDir(), "tunnel.sh")
	if err := os.WriteFile(path, []byte(guestTunnelManager), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
		if strings.Contains(string(output), "couldn't create signal pipe") {
			t.Skip("Git Bash is blocked by the current Windows sandbox")
		}
		t.Fatalf("bash syntax: %v: %s", err, output)
	}
}

func TestCloudflaredBinaryIsPinnedByOfficialURLAndChecksum(t *testing.T) {
	lock, err := loadVersionLock()
	if err != nil {
		t.Fatal(err)
	}
	if lock.Binary.Version != "2026.8.2" || !checksum.MatchString(lock.Binary.SHA256) || !strings.HasPrefix(lock.Binary.URL, "https://github.com/cloudflare/cloudflared/releases/download/2026.8.2/") {
		t.Fatalf("cloudflared binary lock is incomplete: %#v", lock.Binary)
	}
}

func TestGuestTunnelPolicyKeepsCredentialsAndNetworksIsolated(t *testing.T) {
	for _, required := range []string{
		"LoadCredential=tunnel-token:$TOKEN_FILE", "--token-file %d/tunnel-token", "User=$TUNNEL_USER",
		"NoNewPrivileges=yes", "ProtectSystem=strict", "CapabilityBoundingSet=", "MemoryMax=160M",
		"iptables -w 10 -I OUTPUT 1 -m owner", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"install -m 0400 -o root -g root", "runuser -u \"$TUNNEL_USER\" -- test -r",
		"HTTPSHandler(context=ssl.create_default_context())",
		"write_status 'prepare:complete'", "write_status 'install:complete'", "write_status 'start:complete'",
		"write_status \"verify:complete https://$HOSTNAME\"", "systemctl reload nginx",
		"sha256sum \"$temporary\"", "curl --proto '=https' --tlsv1.2",
	} {
		if !strings.Contains(guestTunnelManager, required) {
			t.Fatalf("guest tunnel policy is missing %q", required)
		}
	}
	for _, forbidden := range []string{"docker ", "docker.sock", "privileged: true", "network_mode: host", "TUNNEL_TOKEN:"} {
		if strings.Contains(guestTunnelManager, forbidden) {
			t.Fatalf("guest tunnel policy contains forbidden setting %q", forbidden)
		}
	}
	if strings.Contains(guestTunnelManager, "Environment=TUNNEL_TOKEN") {
		t.Fatal("connector token was introduced through the environment")
	}
	if strings.Contains(guestTunnelManager, "opener.open(request, timeout=15, context=") {
		t.Fatal("Python opener still receives an unsupported context argument")
	}
	prepareStart := strings.Index(guestTunnelManager, "ExecStartPre=+$FIREWALL_SCRIPT start")
	connectorStart := strings.Index(guestTunnelManager, "ExecStart=/usr/local/bin/cloudflared-oneclick")
	if prepareStart < 0 || connectorStart < 0 || prepareStart > connectorStart {
		t.Fatal("egress firewall must be installed before the connector starts")
	}
}

func TestCompletedStageOutputOnlyAcceptsExactJournalMarker(t *testing.T) {
	for _, stage := range []string{"prepare", "install", "start"} {
		output, complete := completedStageOutput(stage+":complete\n", stage, "demo.kidgrow.site")
		if !complete || output != stage+":complete" {
			t.Fatalf("stage %s was not recovered: %q %v", stage, output, complete)
		}
	}
	output, complete := completedStageOutput("verify:complete https://demo.kidgrow.site", "verify", "DEMO.KIDGROW.SITE.")
	if !complete || output != "TUNNEL_OK https://demo.kidgrow.site" {
		t.Fatalf("verify marker was not recovered: %q %v", output, complete)
	}
	for _, status := range []string{"prepare:firewall", "verify:complete https://other.kidgrow.site", ""} {
		if _, complete := completedStageOutput(status, "verify", "demo.kidgrow.site"); complete {
			t.Fatalf("incomplete or foreign marker was accepted: %q", status)
		}
	}
}

func TestPlanRequiresHealthyRuntimeAndReturnsStableHTTPS(t *testing.T) {
	config := Config{
		ProjectID: "1234567890abcdef", ProjectPath: `D:\site`, ProjectName: "Site", VMName: "oneclick-site",
		RuntimeState: "running", RuntimeHealth: "healthy", Hostname: "demo.kidgrow.site",
	}
	plan, err := GetPlan(config, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Address != "https://demo.kidgrow.site" || plan.Mode != "Named Tunnel" || plan.Existing {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	config.RuntimeHealth = ""
	if _, err := GetPlan(config, false); err == nil {
		t.Fatal("unhealthy runtime was accepted")
	}
}

func TestLiveGuestTunnelSyntax(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_TUNNEL") != "1" {
		t.Skip("set ONECLICK_LIVE_TUNNEL=1 for the dedicated VM syntax check")
	}
	file, err := os.CreateTemp("", "oneclick-tunnel-syntax-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(guestTunnelManager); err != nil {
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
	guestPath := "/home/ubuntu/.oneclick/tunnel-syntax.sh"
	if err := guest.Transfer(ctx, path, guestPath); err != nil {
		t.Fatal(err)
	}
	defer guest.Exec(context.Background(), "rm", "-f", guestPath)
	if _, err := guest.Exec(ctx, "bash", "-n", guestPath); err != nil {
		t.Fatal(err)
	}
}

func TestLiveGuestTunnelPrepareAndRollback(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_TUNNEL_PREPARE") != "1" {
		t.Skip("set ONECLICK_LIVE_TUNNEL_PREPARE=1 for the isolated Compose/network check")
	}
	lock, err := loadVersionLock()
	if err != nil {
		t.Fatal(err)
	}
	scriptPath, removeScript, err := writeTemporary("oneclick-tunnel-live-*.sh", guestTunnelManager)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScript()
	tokenPath, removeToken, err := writeTemporary("oneclick-tunnel-token-live-*.tmp", strings.Repeat("a", 64)+"\n")
	if err != nil {
		t.Fatal(err)
	}
	defer removeToken()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: `D:\laragon\www\flatsome`, ProjectName: "flatsome"})
	if err != nil {
		t.Fatal(err)
	}
	const projectID = "4c3c7c2dba7e8f1a"
	const hostname = "prepare-check.kidgrow.site"
	const guestScript = "/home/ubuntu/.oneclick/tunnel-live-check.sh"
	const guestToken = "/home/ubuntu/.oneclick/tunnel-token-4c3c7c2dba7e8f1a.tmp"
	if _, err := guest.Exec(ctx, "mkdir", "-p", "/home/ubuntu/.oneclick"); err != nil {
		t.Fatal(err)
	}
	if err := guest.Transfer(ctx, scriptPath, guestScript); err != nil {
		t.Fatal(err)
	}
	if err := guest.Transfer(ctx, tokenPath, guestToken); err != nil {
		t.Fatal(err)
	}
	defer guest.Exec(context.Background(), "rm", "-f", guestScript, guestToken)
	defer guest.Exec(context.Background(), "sudo", "bash", guestScript, "rollback", projectID, hostname, lock.Binary.Version, lock.Binary.URL, lock.Binary.SHA256, "-")
	if _, err := guest.Exec(ctx, "sudo", "bash", guestScript, "prepare", projectID, hostname, lock.Binary.Version, lock.Binary.URL, lock.Binary.SHA256, guestToken); err != nil {
		t.Fatal(err)
	}
	output, err := guest.Exec(ctx, "sudo", "stat", "-c", "%U:%G:%a", "/etc/oneclick/tunnels/4c3c7c2dba7e8f1a/token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != "root:root:400" {
		t.Fatalf("unexpected connector secret ownership: %q", output)
	}
}
