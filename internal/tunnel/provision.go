package tunnel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"oneclick-dev-server/internal/vm"
)

const guestTunnelManager = `#!/usr/bin/env bash
set -Eeuo pipefail
export LC_ALL=C

PHASE="$1"
PROJECT_ID="$2"
HOSTNAME="$3"
CLOUDFLARED_VERSION="$4"
CLOUDFLARED_URL="$5"
CLOUDFLARED_SHA256="$6"
TOKEN_SOURCE="$7"

case "$PROJECT_ID" in (*[!a-f0-9]*|'') echo 'invalid project id' >&2; exit 20;; esac
test "${#PROJECT_ID}" -ge 16
case "$HOSTNAME" in (*[!a-z0-9.-]*|''|.*|*..*|*.) echo 'invalid hostname' >&2; exit 20;; esac
test "$CLOUDFLARED_VERSION" = '2026.8.2'
test "$CLOUDFLARED_URL" = 'https://github.com/cloudflare/cloudflared/releases/download/2026.8.2/cloudflared-linux-amd64'
case "$CLOUDFLARED_SHA256" in (*[!a-f0-9]*|'') echo 'invalid cloudflared checksum' >&2; exit 20;; esac
test "${#CLOUDFLARED_SHA256}" -eq 64

TUNNEL_USER="oct${PROJECT_ID:0:12}"
TUNNEL_ROOT="/var/lib/oneclick/tunnels/$PROJECT_ID"
TUNNEL_CONFIG="/etc/oneclick/tunnels/$PROJECT_ID"
TOKEN_FILE="$TUNNEL_CONFIG/token"
SERVICE="oneclick-tunnel-$PROJECT_ID.service"
FIREWALL_CHAIN="OCF${PROJECT_ID:0:12}"
FIREWALL_SCRIPT="/usr/local/lib/oneclick/tunnel-firewall-$PROJECT_ID"
STATUS_FILE="$TUNNEL_ROOT/phase-status"
NGINX_CONFIG="/etc/nginx/sites-available/oneclick-$PROJECT_ID.conf"
SITE_ROOT="/var/lib/oneclick/sites/$PROJECT_ID/current"
PHP_UNIT="oneclick-php-$PROJECT_ID.service"
A=$((16#${PROJECT_ID:0:2} % 254 + 1))
B=$((16#${PROJECT_ID:2:2} % 254 + 1))
C=$((16#${PROJECT_ID:4:2} % 254 + 1))
ORIGIN_IP="127.$A.$B.$C"
INTERNAL_HOST="site-$PROJECT_ID.oneclick.local"

write_status() {
  install -d -m 0700 "$TUNNEL_ROOT"
  printf '%s\n' "$1" > "$STATUS_FILE.tmp"
  chmod 0400 "$STATUS_FILE.tmp"
  mv -f "$STATUS_FILE.tmp" "$STATUS_FILE"
}

verify_runtime() {
  test -s "$NGINX_CONFIG" && test -d "$SITE_ROOT"
  systemctl is-active --quiet nginx
  systemctl is-active --quiet mariadb
  systemctl is-active --quiet "$PHP_UNIT"
  nginx -t
  curl --fail --silent --show-error --max-time 20 -H "Host: $HOSTNAME" "http://$ORIGIN_IP:8080/" -o /dev/null
}

configure_origin() {
  python3 - "$NGINX_CONFIG" "$INTERNAL_HOST" "$HOSTNAME" <<'PY'
import pathlib,re,sys
path=pathlib.Path(sys.argv[1]); internal=sys.argv[2]; host=sys.argv[3]
text=path.read_text(encoding='utf-8')
updated,count=re.subn(r'(?m)^  server_name [^;]+;$', f'  server_name {internal} {host};', text, count=1)
if count != 1: raise SystemExit('nginx server_name marker missing')
temporary=path.with_suffix('.tmp')
temporary.write_text(updated, encoding='utf-8', newline='\n')
temporary.chmod(0o644)
temporary.replace(path)
PY
  nginx -t
  systemctl reload nginx
}

prepare_tunnel() {
  write_status 'prepare:runtime'
  configure_origin
  verify_runtime
  write_status 'prepare:credential'
  case "$TOKEN_SOURCE" in (/home/ubuntu/.oneclick/tunnel-token-"$PROJECT_ID".tmp) ;; (*) echo 'invalid token source' >&2; exit 20;; esac
  test -s "$TOKEN_SOURCE"
  getent passwd "$TUNNEL_USER" >/dev/null || useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin "$TUNNEL_USER"
  install -d -m 0700 -o root -g root "$TUNNEL_ROOT" "$TUNNEL_CONFIG" /usr/local/lib/oneclick
  install -m 0400 -o root -g root "$TOKEN_SOURCE" "$TOKEN_FILE"
  rm -f -- "$TOKEN_SOURCE"
  cat > "$FIREWALL_SCRIPT" <<FIREWALL
#!/usr/bin/env bash
set -Eeuo pipefail
CHAIN='$FIREWALL_CHAIN'
UID_VALUE='$(id -u "$TUNNEL_USER")'
ORIGIN='$ORIGIN_IP'
cleanup() {
  while iptables -w 10 -C OUTPUT -m owner --uid-owner "\$UID_VALUE" -j "\$CHAIN" 2>/dev/null; do iptables -w 10 -D OUTPUT -m owner --uid-owner "\$UID_VALUE" -j "\$CHAIN"; done
  iptables -w 10 -F "\$CHAIN" 2>/dev/null || true
  iptables -w 10 -X "\$CHAIN" 2>/dev/null || true
}
if [ "\${1:-}" = stop ]; then cleanup; exit 0; fi
cleanup
iptables -w 10 -N "\$CHAIN"
iptables -w 10 -A "\$CHAIN" -d "\$ORIGIN" -p tcp --dport 8080 -j ACCEPT
iptables -w 10 -A "\$CHAIN" -d 127.0.0.53/32 -p udp --dport 53 -j ACCEPT
iptables -w 10 -A "\$CHAIN" -d 127.0.0.53/32 -p tcp --dport 53 -j ACCEPT
for target in 0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.168.0.0/16 224.0.0.0/4 240.0.0.0/4; do iptables -w 10 -A "\$CHAIN" -d "\$target" -j REJECT; done
iptables -w 10 -A "\$CHAIN" -p tcp -m multiport --dports 443,7844 -j ACCEPT
iptables -w 10 -A "\$CHAIN" -j REJECT
iptables -w 10 -I OUTPUT 1 -m owner --uid-owner "\$UID_VALUE" -j "\$CHAIN"
FIREWALL
  chmod 0700 "$FIREWALL_SCRIPT"
  cat > "/etc/systemd/system/$SERVICE" <<UNIT
[Unit]
Description=OneClick isolated Cloudflare connector $PROJECT_ID
After=network-online.target nginx.service
Wants=network-online.target
Requires=nginx.service

[Service]
Type=simple
User=$TUNNEL_USER
Group=$TUNNEL_USER
LoadCredential=tunnel-token:$TOKEN_FILE
ExecStartPre=+$FIREWALL_SCRIPT start
ExecStart=/usr/local/bin/cloudflared-oneclick tunnel --no-autoupdate --protocol http2 run --token-file %d/tunnel-token
ExecStopPost=+$FIREWALL_SCRIPT stop
Restart=on-failure
RestartSec=5s
UMask=0077
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
ProcSubset=pid
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_INET AF_UNIX
DevicePolicy=closed
TasksMax=64
MemoryMax=160M
CPUQuota=30%

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  write_status 'prepare:complete'
}

install_binary() {
  write_status 'install:running'
  if [ -x /usr/local/bin/cloudflared-oneclick ] && [ "$(sha256sum /usr/local/bin/cloudflared-oneclick | awk '{print $1}')" = "$CLOUDFLARED_SHA256" ]; then
    write_status 'install:complete'; return 0
  fi
  temporary="$(mktemp /tmp/cloudflared-oneclick-XXXXXX)"
  trap 'rm -f "$temporary"' EXIT
  curl --proto '=https' --tlsv1.2 --fail --location --retry 4 --connect-timeout 20 --max-time 600 "$CLOUDFLARED_URL" -o "$temporary"
  test "$(sha256sum "$temporary" | awk '{print $1}')" = "$CLOUDFLARED_SHA256"
  install -m 0755 -o root -g root "$temporary" /usr/local/bin/cloudflared-oneclick
  /usr/local/bin/cloudflared-oneclick --version | grep -Fq "cloudflared version $CLOUDFLARED_VERSION"
  write_status 'install:complete'
}

start_tunnel() {
  write_status 'start:running'
  verify_runtime
  systemctl enable "$SERVICE" >/dev/null
  systemctl restart "$SERVICE"
  write_status 'start:complete'
}

verify_policy() {
  systemctl is-active --quiet "$SERVICE"
  test "$(systemctl show "$SERVICE" -p User --value)" = "$TUNNEL_USER"
  test "$(systemctl show "$SERVICE" -p NoNewPrivileges --value)" = yes
  test "$(systemctl show "$SERVICE" -p ProtectSystem --value)" = strict
  test "$(stat -c '%a %U %G' "$TOKEN_FILE")" = '400 root root'
  ! runuser -u "$TUNNEL_USER" -- test -r "$SITE_ROOT/wp-config.php"
  pid="$(systemctl show "$SERVICE" -p MainPID --value)"
  test "$pid" -gt 1
  ! tr '\0' '\n' < "/proc/$pid/cmdline" | grep -Fq "$(cat "$TOKEN_FILE")"
  iptables -w 10 -C OUTPUT -m owner --uid-owner "$(id -u "$TUNNEL_USER")" -j "$FIREWALL_CHAIN"
  iptables -w 10 -C "$FIREWALL_CHAIN" -d "$ORIGIN_IP" -p tcp --dport 8080 -j ACCEPT
  iptables -w 10 -C "$FIREWALL_CHAIN" -d 10.0.0.0/8 -j REJECT
  iptables -w 10 -C "$FIREWALL_CHAIN" -d 192.168.0.0/16 -j REJECT
}

verify_external() {
  write_status 'verify:running'
  verify_policy
  local deadline=$((SECONDS + 300))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if ! systemctl is-active --quiet "$SERVICE"; then journalctl -u "$SERVICE" --no-pager -n 40 >&2 || true; return 1; fi
    if python3 - "$HOSTNAME" <<'PY'
import ssl,sys,urllib.error,urllib.parse,urllib.request
host=sys.argv[1]
class SameHostRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        parsed=urllib.parse.urlparse(newurl)
        if parsed.scheme != 'https' or parsed.hostname != host: raise urllib.error.URLError('unsafe redirect')
        return super().redirect_request(req, fp, code, msg, headers, newurl)
opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl.create_default_context()), SameHostRedirect())
request=urllib.request.Request('https://'+host+'/', headers={'User-Agent':'OneClickHealth/2.0','Range':'bytes=0-0'})
with opener.open(request, timeout=15) as response:
    response.read(1)
    if not 200 <= response.status < 400: raise RuntimeError(response.status)
PY
    then
      write_status "verify:complete https://$HOSTNAME"
      printf 'TUNNEL_OK https://%s\n' "$HOSTNAME"
      return 0
    fi
    sleep 5
  done
  journalctl -u "$SERVICE" --no-pager -n 60 >&2 || true
  echo 'Domain chưa phản hồi HTTPS sau 5 phút.' >&2
  return 1
}

stop_tunnel() {
  systemctl disable --now "$SERVICE" >/dev/null 2>&1 || true
  "$FIREWALL_SCRIPT" stop >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/$SERVICE" "$TOKEN_FILE" "$FIREWALL_SCRIPT" "$STATUS_FILE" "$STATUS_FILE.tmp"
  rmdir "$TUNNEL_CONFIG" "$TUNNEL_ROOT" 2>/dev/null || true
  if [ -s "$NGINX_CONFIG" ]; then
    python3 - "$NGINX_CONFIG" "$INTERNAL_HOST" <<'PY'
import pathlib,re,sys
path=pathlib.Path(sys.argv[1]); internal=sys.argv[2]
text=path.read_text(encoding='utf-8')
text,count=re.subn(r'(?m)^  server_name [^;]+;$', f'  server_name {internal};', text, count=1)
if count == 1: path.write_text(text, encoding='utf-8', newline='\n')
PY
    nginx -t && systemctl reload nginx
  fi
  systemctl daemon-reload
}

case "$PHASE" in
  prepare) prepare_tunnel ;;
  install) install_binary ;;
  start) start_tunnel ;;
  verify) verify_external ;;
  stop|rollback) stop_tunnel ;;
  *) echo 'invalid tunnel phase' >&2; exit 20 ;;
esac
`

func Start(parent context.Context, config Config, report Reporter) Result {
	if err := validateConfig(config, true); err != nil {
		return failed("Không chuẩn bị được domain", err)
	}
	lock, err := loadVersionLock()
	if err != nil {
		return failed("Không đọc được khóa phiên bản cloudflared", err)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 25*time.Minute)
	defer cancel()
	emit(report, "verify", "Đang kiểm tra runtime và domain…", 3)
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: config.ProjectPath, ProjectName: config.ProjectName})
	if err != nil {
		return failed("Máy ảo chưa sẵn sàng", err)
	}
	if guest.Name() != config.VMName {
		return failed("Máy ảo không khớp lịch sử website", errors.New("derived VM name differs from stored VM"))
	}

	scriptPath, scriptCleanup, err := writeTemporary("oneclick-tunnel-*.sh", guestTunnelManager)
	if err != nil {
		return failed("Không tạo được bộ cài tunnel tạm", err)
	}
	defer scriptCleanup()
	tokenPath, tokenCleanup, err := writeTemporary("oneclick-token-*.tmp", strings.TrimSpace(config.TunnelToken)+"\n")
	if err != nil {
		return failed("Không bảo vệ được connector token tạm", err)
	}
	defer tokenCleanup()
	guestDir := "/home/ubuntu/.oneclick"
	guestScript := guestDir + "/tunnel-manager-" + config.ProjectID + ".sh"
	guestToken := guestDir + "/tunnel-token-" + config.ProjectID + ".tmp"
	if _, err := guest.Exec(ctx, "mkdir", "-p", guestDir); err != nil {
		return failed("Không chuẩn bị được thư mục tunnel trong máy ảo", err)
	}
	if err := guest.Transfer(ctx, scriptPath, guestScript); err != nil {
		return failed("Không chuyển được bộ cài tunnel", err)
	}
	if err := guest.Transfer(ctx, tokenPath, guestToken); err != nil {
		return failed("Không chuyển được connector token", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = guest.Exec(cleanupCtx, "rm", "-f", guestScript, guestToken)
	}()

	base := []string{"sudo", "bash", guestScript, "", config.ProjectID, strings.ToLower(strings.TrimSuffix(config.Hostname, ".")), lock.Binary.Version, lock.Binary.URL, lock.Binary.SHA256, guestToken}
	stages := []struct {
		phase, message, failure string
		start, end              int
		timeout                 time.Duration
	}{
		{"prepare", "Đang tạo connector user và firewall riêng…", "Không tạo được vùng cách ly tunnel", 8, 28, 3 * time.Minute},
		{"install", "Đang kiểm tra cloudflared dùng chung…", "Không cài được cloudflared", 30, 52, 12 * time.Minute},
		{"start", "Đang kết nối domain với Cloudflare…", "Không khởi động được tunnel", 54, 70, 3 * time.Minute},
		{"verify", "Đang chờ HTTPS và kiểm tra từ Internet…", "Domain chưa vượt qua kiểm tra", 72, 99, 7 * time.Minute},
	}
	for _, stage := range stages {
		args := append([]string(nil), base...)
		args[3] = stage.phase
		stageCtx, stageCancel := context.WithTimeout(ctx, stage.timeout)
		output, stageErr := execWithProgress(stageCtx, guest, args, report, stage.phase, stage.message, stage.start, stage.end)
		stageCancel()
		if errors.Is(stageErr, context.DeadlineExceeded) {
			emit(report, stage.phase, "Đang xác nhận bước vừa hoàn tất trong máy ảo…", stage.end-1)
			if recoveredOutput, recovered := recoverCompletedStage(guest, config.ProjectID, stage.phase, config.Hostname); recovered {
				output, stageErr = recoveredOutput, nil
			}
		}
		if stageErr != nil {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 90*time.Second)
			rollbackArgs := append([]string(nil), base...)
			rollbackArgs[3] = "rollback"
			_, _ = guest.Exec(rollbackCtx, rollbackArgs...)
			rollbackCancel()
			return failed(stage.failure, stageErr)
		}
		if stage.phase == "verify" && !strings.Contains(output, "TUNNEL_OK https://"+strings.ToLower(strings.TrimSuffix(config.Hostname, "."))) {
			return failed("Domain chưa vượt qua kiểm tra", errors.New("missing verified tunnel result marker"))
		}
	}
	emit(report, "complete", "Domain đã sẵn sàng", 100)
	return Result{Success: true, Message: "Domain đã sẵn sàng", URL: "https://" + strings.ToLower(strings.TrimSuffix(config.Hostname, ".")), State: "running", Health: "healthy"}
}

func Stop(parent context.Context, config Config) Result {
	if err := validateConfig(config, false); err != nil {
		return failed("Không xác định được tunnel cần dừng", err)
	}
	lock, err := loadVersionLock()
	if err != nil {
		return failed("Không đọc được khóa phiên bản cloudflared", err)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: config.ProjectPath, ProjectName: config.ProjectName})
	if err != nil {
		return failed("Không kết nối được máy ảo để dừng tunnel", err)
	}
	scriptPath, cleanup, err := writeTemporary("oneclick-tunnel-stop-*.sh", guestTunnelManager)
	if err != nil {
		return failed("Không tạo được thao tác dừng tunnel", err)
	}
	defer cleanup()
	guestScript := "/home/ubuntu/.oneclick/tunnel-stop-" + config.ProjectID + ".sh"
	if err := guest.Transfer(ctx, scriptPath, guestScript); err != nil {
		return failed("Không chuyển được thao tác dừng tunnel", err)
	}
	defer guest.Exec(context.Background(), "rm", "-f", guestScript)
	_, err = guest.Exec(ctx, "sudo", "bash", guestScript, "stop", config.ProjectID, strings.ToLower(strings.TrimSuffix(config.Hostname, ".")), lock.Binary.Version, lock.Binary.URL, lock.Binary.SHA256, "-")
	if err != nil {
		return failed("Không dừng được tunnel trong máy ảo", err)
	}
	return Result{Success: true, Message: "Đã dừng truy cập domain", State: "stopped"}
}

func writeTemporary(pattern, content string) (string, func(), error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, err
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func execWithProgress(ctx context.Context, guest *vm.Guest, args []string, report Reporter, stage, message string, start, end int) (string, error) {
	type outcome struct {
		output string
		err    error
	}
	completed := make(chan outcome, 1)
	go func() { output, err := guest.Exec(ctx, args...); completed <- outcome{output, err} }()
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	percent := start
	started := time.Now()
	emit(report, stage, message, percent)
	for {
		select {
		case value := <-completed:
			return value.output, value.err
		case <-ticker.C:
			if percent < end-1 {
				percent += 2
			}
			progressMessage := message
			if elapsed := time.Since(started); elapsed >= 20*time.Second {
				progressMessage = fmt.Sprintf("%s · đã chờ %s", strings.TrimSuffix(message, "…"), formatElapsed(elapsed))
			}
			emit(report, stage, progressMessage, percent)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func recoverCompletedStage(guest *vm.Guest, projectID, stage, hostname string) (string, bool) {
	if guest == nil || !safeProjectID.MatchString(projectID) {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	statusPath := "/var/lib/oneclick/tunnels/" + projectID + "/phase-status"
	for {
		probeCtx, probeCancel := context.WithTimeout(ctx, 6*time.Second)
		status, err := guest.Exec(probeCtx, "sudo", "cat", statusPath)
		probeCancel()
		if err == nil {
			if output, complete := completedStageOutput(status, stage, hostname); complete {
				return output, true
			}
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-time.After(3 * time.Second):
		}
	}
}

func completedStageOutput(status, stage, hostname string) (string, bool) {
	status = strings.TrimSpace(status)
	expected := stage + ":complete"
	if stage == "verify" {
		hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
		expected += " https://" + hostname
	}
	if status != expected {
		return "", false
	}
	if stage == "verify" {
		return "TUNNEL_OK https://" + hostname, true
	}
	return status, true
}

func emit(report Reporter, stage, message string, percent int) {
	if report != nil {
		report(Progress{Stage: stage, Message: message, Percent: percent})
	}
}

func formatElapsed(value time.Duration) string {
	seconds := int(value.Round(time.Second).Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%d giây", seconds)
	}
	return fmt.Sprintf("%d phút %02d giây", seconds/60, seconds%60)
}

func failed(message string, err error) Result {
	detail := ""
	if err != nil {
		detail = diagnosticTail(err.Error(), 1200)
	}
	return Result{Message: message, Detail: detail, State: "failed"}
}

func diagnosticTail(value string, limit int) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(value), "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "Pulling fs layer") || strings.Contains(line, "Download complete") {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) > 24 {
		kept = kept[len(kept)-24:]
	}
	result := strings.Join(kept, "\n")
	if len(result) > limit {
		result = "…\n" + result[len(result)-limit:]
	}
	return strings.TrimSpace(result)
}
