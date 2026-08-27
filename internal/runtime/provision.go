package runtimeenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"oneclick-dev-server/internal/vm"
)

const guestInstaller = `#!/usr/bin/env bash
set -Eeuo pipefail
export LC_ALL=C

PHASE="$1"
PROJECT_ID="$2"
SNAPSHOT_PATH="$3"
SNAPSHOT_ID="$4"
SNAPSHOT_HASH="$5"
DOCUMENT_ROOT="$6"
RUNTIME_REVISION="$7"
PACKAGE_LIST="$8"

case "$PROJECT_ID" in (*[!a-f0-9]*|'') echo 'invalid project id' >&2; exit 20;; esac
test "${#PROJECT_ID}" -ge 16
case "$SNAPSHOT_ID" in (*[!a-f0-9]*|'') echo 'invalid snapshot id' >&2; exit 20;; esac
case "$SNAPSHOT_HASH" in (*[!a-f0-9]*|'') echo 'invalid snapshot checksum' >&2; exit 20;; esac
test "${#SNAPSHOT_HASH}" -eq 64
case "$SNAPSHOT_PATH" in (/var/lib/oneclick/deployments/"$PROJECT_ID"/"$SNAPSHOT_ID") ;; (*) echo 'invalid snapshot path' >&2; exit 20;; esac
case "$RUNTIME_REVISION" in (native-v[0-9]*) ;; (*) echo 'invalid runtime revision' >&2; exit 20;; esac
case "$PACKAGE_LIST" in (*[!a-z0-9.,+-]*|'') echo 'invalid package allowlist' >&2; exit 20;; esac
python3 - "$DOCUMENT_ROOT" <<'PY'
import pathlib, sys
value=sys.argv[1]
path=pathlib.PurePosixPath(value)
if not value or path.is_absolute() or '..' in path.parts or any(part in ('',) for part in path.parts):
    raise SystemExit('invalid document root')
PY

SITE_USER="oc${PROJECT_ID:0:12}"
DB_NAME="oc_${PROJECT_ID:0:16}"
DB_USER="$DB_NAME"
CONFIG_ROOT="/etc/oneclick/sites/$PROJECT_ID"
DATA_ROOT="/var/lib/oneclick/sites/$PROJECT_ID"
SITE_ROOT="$DATA_ROOT/current"
PRIVATE_ROOT="$DATA_ROOT/private"
PHP_UNIT="oneclick-php-$PROJECT_ID.service"
PHP_RUNTIME_DIR="/run/oneclick-$PROJECT_ID"
PHP_SOCKET="$PHP_RUNTIME_DIR/php.sock"
NGINX_CONFIG="/etc/nginx/sites-available/oneclick-$PROJECT_ID.conf"
A=$((16#${PROJECT_ID:0:2} % 254 + 1))
B=$((16#${PROJECT_ID:2:2} % 254 + 1))
C=$((16#${PROJECT_ID:4:2} % 254 + 1))
ORIGIN_IP="127.$A.$B.$C"
INTERNAL_HOST="site-$PROJECT_ID.oneclick.local"

verify_snapshot() {
  test -d "$SNAPSHOT_PATH"
  test "$(cat "$SNAPSHOT_PATH/.oneclick-snapshot-sha256")" = "$SNAPSHOT_HASH"
  test "$(readlink -f "/var/lib/oneclick/projects/$PROJECT_ID/current")" = "$SNAPSHOT_PATH"
}

install_runtime() {
  local missing=0 package changed=0
  IFS=',' read -r -a PACKAGES <<< "$PACKAGE_LIST"
  install -d -m 0755 /var/lib/oneclick /etc/oneclick/sites /var/lib/oneclick/sites
  exec 9>/var/lib/oneclick/runtime-install.lock
  flock -w 600 9
  for package in "${PACKAGES[@]}"; do
    dpkg-query -W -f='${db:Status-Status}' "$package" 2>/dev/null | grep -qx installed || missing=1
  done
  if [ "$missing" -eq 1 ] || [ ! -f /etc/oneclick/native-runtime ] || [ "$(cat /etc/oneclick/native-runtime 2>/dev/null || true)" != "$RUNTIME_REVISION" ]; then
    export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a
    apt-get update -o Acquire::Retries=3
    apt-get install -y --no-install-recommends "${PACKAGES[@]}"
    changed=1
  fi
  for package in "${PACKAGES[@]}"; do
    dpkg-query -W -f='${db:Status-Status}' "$package" | grep -qx installed
  done
  install -d -m 000 /var/lib/oneclick/no-file-import
  cat > /etc/mysql/mariadb.conf.d/60-oneclick-security.cnf <<'CNF'
[mariadbd]
bind-address=127.0.0.1
skip-name-resolve
local-infile=0
skip-symbolic-links
secure-file-priv=/var/lib/oneclick/no-file-import
max-connections=100
CNF
  cat > /etc/nginx/sites-available/00-oneclick-default.conf <<'NGINX'
server {
  listen 127.0.0.1:8080 default_server;
  server_name _;
  access_log off;
  return 444;
}
NGINX
  cat > /etc/nginx/conf.d/00-oneclick-limits.conf <<'NGINX'
limit_req_zone $http_cf_connecting_ip zone=oneclick_per_ip_rate:10m rate=30r/s;
limit_conn_zone $http_cf_connecting_ip zone=oneclick_per_ip_conn:10m;
NGINX
  ln -sfn /etc/nginx/sites-available/00-oneclick-default.conf /etc/nginx/sites-enabled/00-oneclick-default.conf
  rm -f /etc/nginx/sites-enabled/default
  systemctl disable --now php8.3-fpm >/dev/null 2>&1 || true
  systemctl enable mariadb nginx apparmor >/dev/null
  if [ "$changed" -eq 1 ]; then systemctl restart mariadb nginx apparmor; else systemctl start mariadb nginx apparmor; fi
  mariadb-admin --protocol=socket ping >/dev/null
  nginx -t
  printf '%s\n' "$RUNTIME_REVISION" > /etc/oneclick/native-runtime
  chmod 0644 /etc/oneclick/native-runtime
}

ensure_secret() {
  local target="$1"
  if [ ! -s "$target" ]; then
    umask 077
    python3 -c 'import secrets; print(secrets.token_hex(32))' > "$target"
  fi
  chown root:root "$target"
  chmod 0600 "$target"
}

write_wordpress_config() {
  local password auth secure logged nonce auth_salt secure_salt logged_salt nonce_salt
  password="$(cat "$CONFIG_ROOT/db-password")"
  auth="$(cat "$CONFIG_ROOT/auth-key")"; secure="$(cat "$CONFIG_ROOT/secure-auth-key")"
  logged="$(cat "$CONFIG_ROOT/logged-in-key")"; nonce="$(cat "$CONFIG_ROOT/nonce-key")"
  auth_salt="$(cat "$CONFIG_ROOT/auth-salt")"; secure_salt="$(cat "$CONFIG_ROOT/secure-auth-salt")"
  logged_salt="$(cat "$CONFIG_ROOT/logged-in-salt")"; nonce_salt="$(cat "$CONFIG_ROOT/nonce-salt")"
  cat > "$SITE_ROOT/wp-config.php" <<PHP
<?php
define('DB_NAME', '$DB_NAME');
define('DB_USER', '$DB_USER');
define('DB_PASSWORD', '$password');
define('DB_HOST', 'localhost');
define('DB_CHARSET', 'utf8mb4');
define('DB_COLLATE', '');
define('AUTH_KEY', '$auth');
define('SECURE_AUTH_KEY', '$secure');
define('LOGGED_IN_KEY', '$logged');
define('NONCE_KEY', '$nonce');
define('AUTH_SALT', '$auth_salt');
define('SECURE_AUTH_SALT', '$secure_salt');
define('LOGGED_IN_SALT', '$logged_salt');
define('NONCE_SALT', '$nonce_salt');
\$table_prefix = 'wp_';
define('WP_DEBUG', false);
define('DISALLOW_FILE_EDIT', true);
define('DISALLOW_FILE_MODS', true);
if (isset(\$_SERVER['HTTP_X_FORWARDED_PROTO']) && strtolower((string) \$_SERVER['HTTP_X_FORWARDED_PROTO']) === 'https') {
    \$_SERVER['HTTPS'] = 'on';
    \$_SERVER['SERVER_PORT'] = '443';
}
if (!defined('ABSPATH')) define('ABSPATH', __DIR__ . '/');
require_once ABSPATH . 'wp-settings.php';
PHP
  chown "$SITE_USER:$SITE_USER" "$SITE_ROOT/wp-config.php"
  chmod 0600 "$SITE_ROOT/wp-config.php"
  setfacl -b "$SITE_ROOT/wp-config.php"
}

write_php_config() {
  touch "$PRIVATE_ROOT/php-fpm.log" "$PRIVATE_ROOT/php-error.log"
  chown "$SITE_USER:$SITE_USER" "$PRIVATE_ROOT/php-fpm.log" "$PRIVATE_ROOT/php-error.log"
  chmod 0600 "$PRIVATE_ROOT/php-fpm.log" "$PRIVATE_ROOT/php-error.log"
  cat > "$CONFIG_ROOT/php-fpm.conf" <<CONF
[global]
daemonize = no
error_log = $PRIVATE_ROOT/php-fpm.log
log_limit = 4096
[site]
user = $SITE_USER
group = $SITE_USER
listen = $PHP_SOCKET
listen.mode = 0600
pm = ondemand
pm.max_children = 8
pm.process_idle_timeout = 10s
pm.max_requests = 500
clear_env = yes
catch_workers_output = yes
security.limit_extensions = .php
php_admin_value[open_basedir] = $SITE_ROOT:$PRIVATE_ROOT:/tmp:/usr/share/php
php_admin_value[upload_tmp_dir] = $PRIVATE_ROOT/tmp
php_admin_value[session.save_path] = $PRIVATE_ROOT/sessions
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec,putenv,dl
php_admin_flag[allow_url_include] = off
php_admin_flag[expose_php] = off
php_admin_flag[display_errors] = off
php_admin_flag[log_errors] = on
php_admin_value[error_log] = $PRIVATE_ROOT/php-error.log
php_admin_value[cgi.fix_pathinfo] = 0
php_admin_value[memory_limit] = 256M
php_admin_value[upload_max_filesize] = 64M
php_admin_value[post_max_size] = 64M
php_admin_value[max_execution_time] = 120
php_admin_value[opcache.jit] = 0
php_admin_value[pcre.jit] = 0
CONF
  chown "root:$SITE_USER" "$CONFIG_ROOT/php-fpm.conf"
  chmod 0640 "$CONFIG_ROOT/php-fpm.conf"
  cat > "/etc/systemd/system/$PHP_UNIT" <<UNIT
[Unit]
Description=OneClick isolated PHP runtime $PROJECT_ID
After=mariadb.service
Requires=mariadb.service
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=simple
User=$SITE_USER
Group=$SITE_USER
RuntimeDirectory=oneclick-$PROJECT_ID
RuntimeDirectoryMode=0710
ExecStartPre=+/usr/bin/setfacl -m m::x,u:www-data:--x $PHP_RUNTIME_DIR
ExecStart=/usr/sbin/php-fpm8.3 --nodaemonize --fpm-config $CONFIG_ROOT/php-fpm.conf
ExecStartPost=+/usr/bin/timeout 5s /bin/sh -c 'until [ -S $PHP_SOCKET ]; do sleep 0.1; done; exec /usr/bin/setfacl -m u:www-data:rw $PHP_SOCKET'
Restart=on-failure
RestartSec=3s
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
RestrictAddressFamilies=AF_UNIX
IPAddressDeny=any
DevicePolicy=closed
ReadWritePaths=$SITE_ROOT $PRIVATE_ROOT
TasksMax=128
MemoryMax=512M
CPUQuota=75%

[Install]
WantedBy=multi-user.target
UNIT
}

write_nginx_config() {
  local web_root="$SITE_ROOT"
  if [ "$DOCUMENT_ROOT" != "." ]; then web_root="$SITE_ROOT/$DOCUMENT_ROOT"; fi
  test -d "$web_root"
  cat > "$NGINX_CONFIG" <<NGINX
server {
  listen $ORIGIN_IP:8080;
  server_name $INTERNAL_HOST;
  root $web_root;
  index index.php index.html;
  server_tokens off;
  client_max_body_size 64m;
  client_body_timeout 30s;
  client_header_timeout 15s;
  keepalive_timeout 20s;
  send_timeout 30s;
  limit_req zone=oneclick_per_ip_rate burst=60 nodelay;
  limit_conn oneclick_per_ip_conn 20;
  add_header X-Content-Type-Options nosniff always;
  add_header Referrer-Policy strict-origin-when-cross-origin always;
  add_header X-Frame-Options SAMEORIGIN always;
  location / { try_files \$uri \$uri/ /index.php?\$args; }
  location = /wp-config.php { deny all; }
  location ~ /\. { deny all; }
  location ~* ^/wp-content/uploads/.*\.php\$ { deny all; }
  location ~ \.php\$ {
    try_files \$uri =404;
    include fastcgi_params;
    fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
    fastcgi_param HTTP_X_FORWARDED_PROTO \$http_x_forwarded_proto;
    fastcgi_pass unix:$PHP_SOCKET;
  }
}
NGINX
  chmod 0644 "$NGINX_CONFIG"
  ln -sfn "$NGINX_CONFIG" "/etc/nginx/sites-enabled/oneclick-$PROJECT_ID.conf"
}

configure_runtime() {
  verify_snapshot
  getent passwd "$SITE_USER" >/dev/null || useradd --system --user-group --home-dir "$DATA_ROOT" --shell /usr/sbin/nologin "$SITE_USER"
  install -d -m 0710 -o root -g "$SITE_USER" "$CONFIG_ROOT"
  install -d -m 0711 -o root -g root /var/lib/oneclick/sites
  install -d -m 0700 -o "$SITE_USER" -g "$SITE_USER" "$DATA_ROOT" "$PRIVATE_ROOT" "$PRIVATE_ROOT/tmp" "$PRIVATE_ROOT/sessions"
  for secret in db-password auth-key secure-auth-key logged-in-key nonce-key auth-salt secure-auth-salt logged-in-salt nonce-salt; do ensure_secret "$CONFIG_ROOT/$secret"; done
  if [ ! -f "$CONFIG_ROOT/snapshot-id" ] || [ "$(cat "$CONFIG_ROOT/snapshot-id")" != "$SNAPSHOT_ID" ]; then
    systemctl stop "$PHP_UNIT" >/dev/null 2>&1 || true
    rm -rf -- "$DATA_ROOT/current.new" "$SITE_ROOT"
    install -d -m 0700 -o "$SITE_USER" -g "$SITE_USER" "$DATA_ROOT/current.new"
    cp -a "$SNAPSHOT_PATH/." "$DATA_ROOT/current.new/"
    rm -f -- "$DATA_ROOT/current.new/.oneclick-manifest.json" "$DATA_ROOT/current.new/.oneclick-snapshot-sha256"
    chown -R "$SITE_USER:$SITE_USER" "$DATA_ROOT/current.new"
    mv -- "$DATA_ROOT/current.new" "$SITE_ROOT"
    printf '%s\n' "$SNAPSHOT_ID" > "$CONFIG_ROOT/snapshot-id"
  fi
  chmod -R u=rwX,go= "$SITE_ROOT"
  setfacl -m u:www-data:--x "$DATA_ROOT"
  setfacl -Rm u:www-data:rX,m::rX "$SITE_ROOT"
  find "$SITE_ROOT" -type d -exec setfacl -m d:u:www-data:r-x,d:m::r-x {} +
  write_wordpress_config
  write_php_config
  write_nginx_config
  password="$(cat "$CONFIG_ROOT/db-password")"
  mariadb --protocol=socket <<SQL
CREATE DATABASE IF NOT EXISTS $DB_NAME CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '$DB_USER'@'localhost' IDENTIFIED BY '$password';
ALTER USER '$DB_USER'@'localhost' IDENTIFIED BY '$password';
REVOKE ALL PRIVILEGES, GRANT OPTION FROM '$DB_USER'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, CREATE TEMPORARY TABLES, LOCK TABLES ON $DB_NAME.* TO '$DB_USER'@'localhost';
GRANT USAGE ON *.* TO '$DB_USER'@'localhost' WITH MAX_USER_CONNECTIONS 8 MAX_STATEMENT_TIME 30;
FLUSH PRIVILEGES;
SQL
  cat > "$CONFIG_ROOT/database.cnf" <<CNF
[client]
user=$DB_USER
password=$password
database=$DB_NAME
protocol=socket
socket=/run/mysqld/mysqld.sock
default-character-set=utf8mb4
CNF
  chown root:root "$CONFIG_ROOT/database.cnf"
  chmod 0600 "$CONFIG_ROOT/database.cnf"
  printf '%s\n' "$SITE_USER" > "$CONFIG_ROOT/site-user"
  chmod 0600 "$CONFIG_ROOT/site-user"
  nginx -t
  runuser -u "$SITE_USER" -- /usr/sbin/php-fpm8.3 --test --fpm-config "$CONFIG_ROOT/php-fpm.conf"
}

start_runtime() {
  verify_snapshot
  systemctl daemon-reload
  systemctl enable "$PHP_UNIT" >/dev/null
  systemctl reset-failed "$PHP_UNIT" >/dev/null 2>&1 || true
  systemctl restart "$PHP_UNIT"
  systemctl reload nginx
}

verify_runtime() {
  systemctl is-active --quiet mariadb
  systemctl is-active --quiet nginx
  systemctl is-active --quiet "$PHP_UNIT"
  test "$(systemctl show "$PHP_UNIT" -p User --value)" = "$SITE_USER"
  test "$(systemctl show "$PHP_UNIT" -p NoNewPrivileges --value)" = yes
  test "$(systemctl show "$PHP_UNIT" -p PrivateTmp --value)" = yes
  test "$(systemctl show "$PHP_UNIT" -p ProtectSystem --value)" = strict
  systemctl show "$PHP_UNIT" -p RestrictAddressFamilies --value | grep -qx AF_UNIX
	test -S "$PHP_SOCKET"
	getfacl -cp "$PHP_RUNTIME_DIR" | grep -q '^user:www-data:--x'
	getfacl -cp "$PHP_RUNTIME_DIR" | grep -q '^mask::--x'
	getfacl -cp "$PHP_SOCKET" | grep -q '^user:www-data:rw-'
  test "$(stat -c '%a' "$SITE_ROOT/wp-config.php")" = 600
  ! getfacl -cp "$SITE_ROOT/wp-config.php" | grep -q '^user:www-data:'
  test "$(mariadb --protocol=socket -Nse "SELECT COUNT(*) FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE('$DB_USER'),'@',QUOTE('localhost')) AND TABLE_SCHEMA='$DB_NAME'")" -ge 10
  test "$(mariadb --protocol=socket -Nse "SELECT COUNT(*) FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE('$DB_USER'),'@',QUOTE('localhost')) AND TABLE_SCHEMA<>'$DB_NAME'")" = 0
  test "$(mariadb --protocol=socket -Nse "SELECT COUNT(*) FROM information_schema.USER_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE('$DB_USER'),'@',QUOTE('localhost')) AND PRIVILEGE_TYPE<>'USAGE'")" = 0
  limits="$(mariadb --protocol=socket -Nse "SELECT max_user_connections,CAST(max_statement_time AS UNSIGNED) FROM mysql.user WHERE User='$DB_USER' AND Host='localhost'")"
  test "$limits" = $'8\t30'
  while IFS=: read -r other _; do
    [ "$other" = "$SITE_USER" ] && continue
    if [[ "$other" == oc* ]]; then ! runuser -u "$other" -- test -r "$SITE_ROOT/wp-config.php"; fi
  done < /etc/passwd
  curl --fail --silent --show-error --max-time 20 -H "Host: $INTERNAL_HOST" "http://$ORIGIN_IP:8080/" -o /dev/null
  printf 'RUNTIME_OK\n'
}

case "$PHASE" in
  install) install_runtime ;;
  configure) configure_runtime ;;
  start) start_runtime ;;
  verify) verify_runtime ;;
  *) echo 'invalid runtime phase' >&2; exit 20 ;;
esac
`

// Provision installs the native Ubuntu stack once and starts one sandboxed
// WordPress PHP service. It never publishes a host port or returns credentials.
func Provision(parent context.Context, config Config, report Reporter) Result {
	if _, err := GetPlan(config, false); err != nil {
		return failed("Không chuẩn bị được môi trường chạy", err)
	}
	lock, err := loadVersionLock()
	if err != nil {
		return failed("Không đọc được khóa phiên bản runtime", err)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Minute)
	defer cancel()

	emit(report, "verify", "Đang kiểm tra snapshot và máy ảo…", 3)
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: config.ProjectPath, ProjectName: config.ProjectName})
	if err != nil {
		return failed("Máy ảo chưa sẵn sàng", err)
	}
	if guest.Name() != config.VMName {
		return failed("Máy ảo không khớp lịch sử website", errors.New("derived VM name differs from the stored project VM"))
	}
	marker, err := guest.Exec(ctx, "sudo", "cat", config.SnapshotPath+"/.oneclick-snapshot-sha256")
	if err != nil || strings.TrimSpace(marker) != config.SnapshotHash {
		if err == nil {
			err = errors.New("immutable snapshot checksum marker does not match state")
		}
		return failed("Snapshot chưa vượt qua kiểm tra", err)
	}

	temporary, err := os.CreateTemp("", "oneclick-runtime-*.sh")
	if err != nil {
		return failed("Không tạo được bộ cài runtime tạm", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return failed("Không bảo vệ được bộ cài runtime tạm", err)
	}
	if _, err := temporary.WriteString(guestInstaller); err != nil {
		temporary.Close()
		return failed("Không ghi được bộ cài runtime tạm", err)
	}
	if err := temporary.Close(); err != nil {
		return failed("Không hoàn tất được bộ cài runtime tạm", err)
	}

	guestDir := "/home/ubuntu/.oneclick/runtime"
	guestScript := guestDir + "/installer-" + config.ProjectID + ".sh"
	if _, err := guest.Exec(ctx, "mkdir", "-p", guestDir); err != nil {
		return failed("Không chuẩn bị được thư mục cài đặt trong máy ảo", err)
	}
	if err := guest.Transfer(ctx, temporaryPath, guestScript); err != nil {
		return failed("Không chuyển được bộ cài vào máy ảo", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = guest.Exec(cleanupCtx, "rm", "-f", guestScript)
	}()

	baseArgs := []string{
		"sudo", "bash", guestScript, "",
		config.ProjectID, config.SnapshotPath, config.SnapshotID, config.SnapshotHash,
		config.DocumentRoot, lock.RuntimeRevision, strings.Join(lock.Packages, ","),
	}
	stages := []struct {
		phase, message, failure string
		start, end              int
		timeout                 time.Duration
	}{
		{"install", "Đang chuẩn bị Nginx, PHP và MariaDB dùng chung…", "Không cài được runtime Ubuntu", 8, 38, 12 * time.Minute},
		{"configure", "Đang tạo user, database và sandbox riêng…", "Không tạo được vùng cách ly website", 40, 70, 6 * time.Minute},
		{"start", "Đang khởi động PHP riêng của website…", "Không khởi động được runtime", 72, 84, 3 * time.Minute},
		{"verify", "Đang kiểm tra quyền chéo, network và database…", "Runtime chưa vượt qua kiểm tra an toàn", 86, 99, 5 * time.Minute},
	}
	for _, stage := range stages {
		args := append([]string(nil), baseArgs...)
		args[3] = stage.phase
		stageCtx, stageCancel := context.WithTimeout(ctx, stage.timeout)
		_, stageErr := execWithProgress(stageCtx, guest, args, report, stage.phase, stage.message, stage.start, stage.end)
		stageCancel()
		if stageErr != nil {
			return failed(stage.failure, stageErr)
		}
	}
	emit(report, "complete", "Môi trường chạy đã sẵn sàng", 100)
	return Result{
		Success: true, Message: "Môi trường chạy đã sẵn sàng", Adapter: "WordPress",
		Health: "healthy", Services: 2, SnapshotID: config.SnapshotID, RuntimeState: "running",
	}
}

func execWithProgress(ctx context.Context, guest *vm.Guest, args []string, report Reporter, stage, message string, start, end int) (string, error) {
	type outcome struct {
		output string
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		output, err := guest.Exec(ctx, args...)
		completed <- outcome{output: output, err: err}
	}()
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	percent := start
	startedAt := time.Now()
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
			if elapsed := time.Since(startedAt); elapsed >= 20*time.Second {
				progressMessage = fmt.Sprintf("%s · đã chờ %s", strings.TrimSuffix(message, "…"), formatElapsed(elapsed))
			}
			emit(report, stage, progressMessage, percent)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func formatElapsed(value time.Duration) string {
	seconds := int(value.Round(time.Second).Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%d giây", seconds)
	}
	return fmt.Sprintf("%d phút %02d giây", seconds/60, seconds%60)
}

func emit(report Reporter, stage, message string, percent int) {
	if report != nil {
		report(Progress{Stage: stage, Message: message, Percent: percent})
	}
}

func failed(message string, err error) Result {
	detail := ""
	if err != nil {
		detail = diagnosticTail(err.Error(), 1100)
	}
	return Result{Message: message, Detail: detail, RuntimeState: "failed"}
}

func diagnosticTail(value string, limit int) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(value), "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "Pulling fs layer") || strings.Contains(line, "Download complete") || strings.Contains(line, "Pull complete") {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) > 20 {
		kept = kept[len(kept)-20:]
	}
	result := strings.Join(kept, "\n")
	if len(result) > limit {
		result = "…\n" + result[len(result)-limit:]
	}
	return strings.TrimSpace(result)
}
