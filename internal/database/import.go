package database

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"oneclick-dev-server/internal/hostexec"
	"oneclick-dev-server/internal/vm"
)

const guestDatabaseScript = `#!/usr/bin/env bash
set -Eeuo pipefail
export LC_ALL=C

PHASE="$1"
PROJECT_ID="$2"
VALUE_A="${3:-}"
VALUE_B="${4:-}"
VALUE_C="${5:-}"

case "$PROJECT_ID" in (*[!a-f0-9]*|'') echo 'invalid project id' >&2; exit 20;; esac
test "${#PROJECT_ID}" -ge 16
CONFIG_ROOT="/etc/oneclick/sites/$PROJECT_ID"
SITE_ROOT="/var/lib/oneclick/sites/$PROJECT_ID/current"
DB_NAME="oc_${PROJECT_ID:0:16}"
DB_USER="$DB_NAME"
APP_CNF="$CONFIG_ROOT/database.cnf"
SITE_USER="$(cat "$CONFIG_ROOT/site-user")"

test -s "$APP_CNF"
test -d "$SITE_ROOT"
case "$SITE_USER" in (oc[0-9a-f]*) ;; (*) echo 'invalid site user' >&2; exit 20;; esac
systemctl is-active --quiet mariadb

import_database() {
  local dump_path="$VALUE_A" expected_hash="$VALUE_B" table_prefix="$VALUE_C" tables
  case "$dump_path" in
    (/home/ubuntu/.oneclick/database/import-"$PROJECT_ID".sql|/home/ubuntu/.oneclick/database/import-"$PROJECT_ID".sql.gz) ;;
    (*) echo 'invalid database import path' >&2; exit 20;;
  esac
  case "$expected_hash" in (*[!a-f0-9]*|'') echo 'invalid database checksum' >&2; exit 20;; esac
  test "${#expected_hash}" -eq 64
  case "$table_prefix" in (*[!A-Za-z0-9_]*|'') echo 'invalid WordPress table prefix' >&2; exit 20;; esac
  test "${#table_prefix}" -le 64
  test -s "$dump_path"
  test "$(sha256sum "$dump_path" | awk '{print $1}')" = "$expected_hash"
  if [[ "$dump_path" == *.gz ]]; then gzip -t "$dump_path"; fi
  trap 'rm -f "$VALUE_A"' EXIT
  password="$(awk -F= '$1=="password" {print substr($0,index($0,"=")+1); exit}' "$APP_CNF")"
  test -n "$password"
  mariadb --protocol=socket -e \
    "DROP DATABASE IF EXISTS $DB_NAME; CREATE DATABASE $DB_NAME CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; REVOKE ALL PRIVILEGES, GRANT OPTION FROM '$DB_USER'@'localhost'; GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, CREATE TEMPORARY TABLES, LOCK TABLES ON $DB_NAME.* TO '$DB_USER'@'localhost'; GRANT USAGE ON *.* TO '$DB_USER'@'localhost' WITH MAX_USER_CONNECTIONS 8 MAX_STATEMENT_TIME 30; FLUSH PRIVILEGES;"
  if [[ "$dump_path" == *.gz ]]; then
    gzip -dc "$dump_path" | mariadb --defaults-extra-file="$APP_CNF" "$DB_NAME"
  else
    mariadb --defaults-extra-file="$APP_CNF" "$DB_NAME" < "$dump_path"
  fi
  tables="$(mariadb --defaults-extra-file="$APP_CNF" -Nse "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$DB_NAME'" | tr -d '\r')"
  case "$tables" in (*[!0-9]*|'') echo 'database table count is invalid' >&2; exit 30;; esac
  test "$tables" -gt 0
  test "$(mariadb --defaults-extra-file="$APP_CNF" -Nse "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$DB_NAME' AND table_name='${table_prefix}options'" | tr -d '\r')" = 1
  python3 - "$SITE_ROOT/wp-config.php" "$table_prefix" "$SITE_USER" <<'PY'
import os, re, sys
import pwd
path, prefix, user = sys.argv[1], sys.argv[2], sys.argv[3]
if not re.fullmatch(r'[A-Za-z0-9_]{1,64}', prefix):
    raise SystemExit('invalid table prefix')
with open(path, 'r', encoding='utf-8') as handle:
    value = handle.read()
updated, count = re.subn(r"(?m)^\s*\$table_prefix\s*=\s*['\"][A-Za-z0-9_]+['\"]\s*;", "$table_prefix = '" + prefix + "';", value, count=1)
if count != 1:
    raise SystemExit('runtime wp-config table prefix is unavailable')
temporary = path + '.database.tmp'
with open(temporary, 'x', encoding='utf-8', newline='\n') as handle:
    handle.write(updated)
account=pwd.getpwnam(user)
os.chmod(temporary, 0o600)
os.chown(temporary, account.pw_uid, account.pw_gid)
os.replace(temporary, path)
PY
  setfacl -b "$SITE_ROOT/wp-config.php"
  echo "TABLES=$tables"
}

configure_url() {
  local table_prefix="$VALUE_A" hostname="$VALUE_B"
  case "$table_prefix" in (*[!A-Za-z0-9_]*|'') echo 'invalid WordPress table prefix' >&2; exit 20;; esac
  case "$hostname" in (*[!a-z0-9.-]*|'') echo 'invalid public hostname' >&2; exit 20;; esac
  test "${#table_prefix}" -le 64
  test "${#hostname}" -le 253
  test "$(mariadb --defaults-extra-file="$APP_CNF" -Nse "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$DB_NAME' AND table_name='${table_prefix}options'" | tr -d '\r')" = 1
  mariadb --defaults-extra-file="$APP_CNF" "$DB_NAME" -e \
    "UPDATE ${table_prefix}options SET option_value='https://${hostname}' WHERE option_name IN ('home','siteurl');"
}

case "$PHASE" in
  import) import_database ;;
  url) configure_url ;;
  *) echo 'unsupported database phase' >&2; exit 20;;
esac
`

var tableCountPattern = regexp.MustCompile(`(?m)^TABLES=([0-9]+)$`)

func Import(ctx context.Context, config Config, request Request, report Reporter) (result Result) {
	if report == nil {
		report = func(Progress) {}
	}
	plan, err := GetPlan(config, request)
	if err != nil {
		return Result{Message: "Không chuẩn bị được database", Detail: err.Error()}
	}
	if !plan.Ready {
		return Result{Message: "Chưa thể tự lấy database", Detail: plan.AutomaticMessage}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 35*time.Minute)
	defer cancel()
	report(Progress{Stage: "prepare", Message: "Đang chuẩn bị bản sao database…", Percent: 4})
	workRoot := strings.TrimSpace(config.TempRoot)
	if workRoot == "" {
		workRoot = os.TempDir()
	}
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return Result{Message: "Không tạo được vùng tạm an toàn", Detail: err.Error()}
	}
	workDir, err := os.MkdirTemp(workRoot, "database-")
	if err != nil {
		return Result{Message: "Không tạo được vùng tạm an toàn", Detail: err.Error()}
	}
	defer func() {
		if cleanupErr := os.RemoveAll(workDir); cleanupErr != nil {
			if result.Detail != "" {
				result.Detail = strings.TrimSpace(result.Detail + "\nKhông dọn được file tạm: " + cleanupErr.Error())
			} else {
				result.Detail = "Không dọn được file tạm: " + cleanupErr.Error()
			}
			result.Success = false
			if result.Message == "" || result.Message == "Đã nhập database" {
				result.Message = "Database đã xử lý nhưng chưa dọn sạch file tạm"
			}
		}
	}()

	dumpPath := filepath.Join(workDir, "database.sql")
	tablePrefix := plan.TablePrefix
	sourceLabel := plan.Source
	if plan.Mode == "automatic" {
		source, detectErr := detectWordPressSource(config.ProjectPath)
		if detectErr != nil {
			return Result{Message: "Không đọc được database WordPress", Detail: detectErr.Error()}
		}
		tablePrefix = source.TablePrefix
		report(Progress{Stage: "source_database", Message: "Đang kiểm tra database cục bộ…", Percent: 8})
		if err := ensureSourceDatabase(runCtx, source); err != nil {
			return Result{Message: "Database trên máy chưa sẵn sàng", Detail: err.Error()}
		}
		report(Progress{Stage: "export", Message: "Đang xuất database trên máy; dữ liệu gốc chỉ được đọc…", Percent: 14})
		if err := exportDatabase(runCtx, source, dumpPath); err != nil {
			return Result{Message: "Không xuất được database trên máy", Detail: err.Error()}
		}
	} else {
		if strings.HasSuffix(strings.ToLower(plan.ImportPath), ".gz") {
			dumpPath += ".gz"
		}
		report(Progress{Stage: "copy", Message: "Đang tạo bản sao tạm của file database…", Percent: 12})
		if err := copyRegularFile(plan.ImportPath, dumpPath, maxImportBytes); err != nil {
			return Result{Message: "Không đọc được file database", Detail: err.Error()}
		}
		if tablePrefix == "" {
			tablePrefix, err = detectDumpPrefix(dumpPath)
			if err != nil {
				return Result{Message: "Không nhận diện được database WordPress", Detail: err.Error()}
			}
		}
	}
	info, err := os.Stat(dumpPath)
	if err != nil || info.Size() <= 0 || info.Size() > maxImportBytes {
		return Result{Message: "Bản sao database không hợp lệ", Detail: "file database trống hoặc vượt quá giới hạn 4 GB"}
	}
	hash, err := hashFile(dumpPath)
	if err != nil {
		return Result{Message: "Không kiểm tra được database", Detail: err.Error()}
	}
	report(Progress{Stage: "guest", Message: "Đang kiểm tra máy ảo và runtime…", Percent: 38})
	guest, err := vm.OpenGuest(runCtx, vm.Request{ProjectPath: config.ProjectPath, ProjectName: config.ProjectName})
	if err != nil {
		return Result{Message: "Máy ảo chưa sẵn sàng để nhận database", Detail: err.Error()}
	}
	guestDir := "/home/ubuntu/.oneclick/database"
	guestSuffix := ".sql"
	if strings.HasSuffix(strings.ToLower(dumpPath), ".gz") {
		guestSuffix = ".sql.gz"
	}
	guestDump := guestDir + "/import-" + config.ProjectID + guestSuffix
	guestScript := guestDir + "/database-" + config.ProjectID + ".sh"
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cleanupCancel()
	defer guest.Exec(cleanupCtx, "rm", "-f", guestDump, guestScript)
	if _, err := guest.Exec(runCtx, "install", "-d", "-m", "0700", guestDir); err != nil {
		return Result{Message: "Không chuẩn bị được vùng nhận database", Detail: err.Error()}
	}
	scriptPath := filepath.Join(workDir, "database.sh")
	if err := os.WriteFile(scriptPath, []byte(guestDatabaseScript), 0o600); err != nil {
		return Result{Message: "Không chuẩn bị được bộ nhập database", Detail: err.Error()}
	}
	report(Progress{Stage: "transfer", Message: "Đang chuyển database một chiều vào máy ảo…", Percent: 48})
	if err := guest.Transfer(runCtx, scriptPath, guestScript); err != nil {
		return Result{Message: "Không chuyển được bộ nhập database", Detail: err.Error()}
	}
	if err := guest.Transfer(runCtx, dumpPath, guestDump); err != nil {
		return Result{Message: "Không chuyển được database vào máy ảo", Detail: err.Error()}
	}
	report(Progress{Stage: "import", Message: "Đang thay database trống bằng bản sao website…", Percent: 66})
	output, err := guest.Exec(runCtx, "sudo", "bash", guestScript, "import", config.ProjectID, guestDump, hash, tablePrefix)
	if err != nil {
		return Result{Message: "Không nhập được database", Detail: diagnosticDetail(output, err)}
	}
	match := tableCountPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return Result{Message: "Database đã chạy nhưng chưa xác minh được", Detail: "máy ảo không trả về số bảng đã nhập"}
	}
	tables, _ := strconv.Atoi(match[1])
	report(Progress{Stage: "done", Message: "Database WordPress đã sẵn sàng.", Percent: 100})
	return Result{
		Success: true, Message: "Đã nhập database", Source: sourceLabel,
		Tables: tables, Bytes: info.Size(), TablePrefix: tablePrefix,
	}
}

func ConfigureWordPressURL(ctx context.Context, config Config, tablePrefix, hostname string) error {
	if err := validateConfig(config); err != nil {
		return err
	}
	if !safeTablePrefix.MatchString(tablePrefix) {
		return errors.New("tiền tố bảng WordPress không hợp lệ")
	}
	if !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`).MatchString(hostname) {
		return errors.New("hostname không hợp lệ")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	guest, err := vm.OpenGuest(runCtx, vm.Request{ProjectPath: config.ProjectPath, ProjectName: config.ProjectName})
	if err != nil {
		return err
	}
	root := strings.TrimSpace(config.TempRoot)
	if root == "" {
		root = os.TempDir()
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	directory, err := os.MkdirTemp(root, "database-url-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "database.sh")
	if err := os.WriteFile(path, []byte(guestDatabaseScript), 0o600); err != nil {
		return err
	}
	guestDir := "/home/ubuntu/.oneclick/database"
	guestScript := guestDir + "/database-url-" + config.ProjectID + ".sh"
	if _, err := guest.Exec(runCtx, "install", "-d", "-m", "0700", guestDir); err != nil {
		return err
	}
	if err := guest.Transfer(runCtx, path, guestScript); err != nil {
		return err
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	defer guest.Exec(cleanupCtx, "rm", "-f", guestScript)
	output, err := guest.Exec(runCtx, "sudo", "bash", guestScript, "url", config.ProjectID, tablePrefix, hostname)
	if err != nil {
		return errors.New(diagnosticDetail(output, err))
	}
	return nil
}

func exportDatabase(ctx context.Context, source sourceConfig, destination string) error {
	credentials := destination + ".cnf"
	content := fmt.Sprintf("[client]\nuser=\"%s\"\npassword=\"%s\"\nhost=\"%s\"\nport=%s\nprotocol=tcp\ndefault-character-set=utf8mb4\n",
		optionValue(source.User), optionValue(source.Password), optionValue(source.Host), source.Port)
	if err := os.WriteFile(credentials, []byte(content), 0o600); err != nil {
		return err
	}
	defer os.Remove(credentials)
	arguments := []string{
		"--defaults-extra-file=" + credentials,
		"--single-transaction", "--quick", "--skip-lock-tables", "--hex-blob",
		"--default-character-set=utf8mb4", "--add-drop-table", "--skip-comments",
	}
	versionCommand := hostexec.CommandContext(ctx, source.DumpTool, "--version")
	versionOutput, _ := versionCommand.CombinedOutput()
	if regexp.MustCompile(`(?i)mysqldump\s+ver\s+8\.`).Match(versionOutput) {
		arguments = append(arguments, "--column-statistics=0", "--set-gtid-purged=OFF", "--no-tablespaces")
	}
	arguments = append(arguments, source.Database)
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	command := hostexec.CommandContext(ctx, source.DumpTool, arguments...)
	command.Stdout = file
	var stderr cappedBuffer
	command.Stderr = &stderr
	runErr := command.Run()
	closeErr := file.Close()
	if runErr != nil {
		return fmt.Errorf("%s", diagnosticDetail(stderr.String(), runErr))
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func optionValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func copyRegularFile(source, destination string, limit int64) error {
	pathInfo, err := os.Lstat(source)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("file database không còn là file thông thường")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	openedInfo, err := input.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return errors.New("file database đã thay đổi trong lúc mở")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > limit {
		return errors.New("file database vượt quá giới hạn 4 GB")
	}
	finalInfo, err := input.Stat()
	if err != nil || finalInfo.Size() != openedInfo.Size() || !finalInfo.ModTime().Equal(openedInfo.ModTime()) {
		return errors.New("file database đã thay đổi trong lúc sao chép")
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func detectDumpPrefix(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return "", errors.New("file .sql.gz bị lỗi hoặc không đúng định dạng")
		}
		defer compressed.Close()
		reader = compressed
	}
	data, err := io.ReadAll(io.LimitReader(reader, 2*1024*1024))
	if err != nil {
		return "", err
	}
	match := optionsPattern.FindSubmatch(data)
	if len(match) != 2 || !safeTablePrefix.Match(match[1]) {
		return "", errors.New("không tìm thấy bảng options WordPress trong 2 MB đầu của file")
	}
	return string(match[1]), nil
}

type cappedBuffer struct{ data []byte }

func (buffer *cappedBuffer) Write(value []byte) (int, error) {
	const limit = 32 * 1024
	buffer.data = append(buffer.data, value...)
	if len(buffer.data) > limit {
		buffer.data = append([]byte(nil), buffer.data[len(buffer.data)-limit:]...)
	}
	return len(value), nil
}

func (buffer *cappedBuffer) String() string { return string(buffer.data) }

func diagnosticDetail(output string, err error) string {
	output = strings.TrimSpace(strings.ToValidUTF8(output, ""))
	if len(output) > 4000 {
		output = output[len(output)-4000:]
	}
	if output != "" {
		return output
	}
	if err != nil {
		return err.Error()
	}
	return "database operation failed"
}

var _ io.Writer = (*cappedBuffer)(nil)
