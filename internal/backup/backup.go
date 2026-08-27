package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"oneclick-dev-server/internal/vm"
)

const guestExportScript = `#!/usr/bin/env bash
set -Eeuo pipefail
export LC_ALL=C

PROJECT_ID="$1"
STAMP="$2"
case "$PROJECT_ID" in (*[!a-f0-9]*|'') echo 'invalid project id' >&2; exit 20;; esac
test "${#PROJECT_ID}" -ge 16
case "$STAMP" in ([0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]) ;; (*) echo 'invalid export stamp' >&2; exit 20;; esac

SITE_ROOT="/var/lib/oneclick/sites/$PROJECT_ID/current"
CONFIG_ROOT="/etc/oneclick/sites/$PROJECT_ID"
APP_CNF="$CONFIG_ROOT/database.cnf"
DB_NAME="oc_${PROJECT_ID:0:16}"
EXPORT_ROOT="/home/ubuntu/.oneclick/exports/$PROJECT_ID/$STAMP"
SOURCE_ARCHIVE="$EXPORT_ROOT/source.tar.gz"
DATABASE_DUMP="$EXPORT_ROOT/database.sql"

test -d "$SITE_ROOT"
test -s "$APP_CNF"
systemctl is-active --quiet mariadb
if find "$SITE_ROOT" -xdev \( -type l -o -type b -o -type c -o -type p -o -type s \) -print -quit | grep -q .; then
  echo 'source contains a link or special file' >&2
  exit 30
fi
install -d -m 0700 -o ubuntu -g ubuntu "/home/ubuntu/.oneclick/exports/$PROJECT_ID" "$EXPORT_ROOT"
trap 'rm -rf -- "$EXPORT_ROOT"' ERR

tar --create --gzip --file "$SOURCE_ARCHIVE" \
  --exclude='./wp-config.php' --exclude='./.env' --exclude='./.env.*' \
  --exclude='*/.env' --exclude='*/.env.*' --directory "$SITE_ROOT" .
mariadb-dump --defaults-extra-file="$APP_CNF" --single-transaction --quick \
  --skip-lock-tables --hex-blob --add-drop-table --skip-comments "$DB_NAME" > "$DATABASE_DUMP"
test -s "$SOURCE_ARCHIVE"
test -s "$DATABASE_DUMP"
chown ubuntu:ubuntu "$SOURCE_ARCHIVE" "$DATABASE_DUMP"
chmod 0600 "$SOURCE_ARCHIVE" "$DATABASE_DUMP"

printf 'SOURCE_SHA=%s\n' "$(sha256sum "$SOURCE_ARCHIVE" | awk '{print $1}')"
printf 'SOURCE_BYTES=%s\n' "$(stat -c '%s' "$SOURCE_ARCHIVE")"
printf 'DATABASE_SHA=%s\n' "$(sha256sum "$DATABASE_DUMP" | awk '{print $1}')"
printf 'DATABASE_BYTES=%s\n' "$(stat -c '%s' "$DATABASE_DUMP")"
`

var (
	projectIDPattern = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
	stampPattern     = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}$`)
	hashPattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
	createLock       sync.Mutex
)

const (
	maxBackupFileBytes = int64(4 * 1024 * 1024 * 1024)
	maxArchiveEntries  = 100000
)

func Inspect(config Config) (Status, error) {
	if err := validateConfig(config, false); err != nil {
		return Status{}, err
	}
	projectRoot := filepath.Join(config.BackupRoot, config.ProjectID)
	databaseName := ""
	if config.ProjectKind == "wordpress" {
		databaseName = "oc_" + config.ProjectID[:16]
	}
	status := Status{
		ProjectPath: config.ProjectPath, SourcePath: config.ProjectPath,
		DeployedPath: "/var/lib/oneclick/sites/" + config.ProjectID + "/current",
		Database:     databaseName, DatabaseTables: config.DatabaseTables,
		DatabaseImportedAt: config.DatabaseImportedAt, BackupRoot: projectRoot,
		CanBackup: canBackup(config),
	}
	if status.CanBackup {
		status.Message = "Có thể tạo bản sao source và database đang chạy."
	} else if config.DatabaseState != "ready" {
		status.Message = "Hoàn tất bước database trước khi tạo bản sao."
	} else {
		status.Message = "Runtime chưa sẵn sàng để tạo bản sao."
	}
	entries, err := os.ReadDir(projectRoot)
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("không đọc được danh sách backup: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !stampPattern.MatchString(entry.Name()) {
			continue
		}
		manifestPath := filepath.Join(projectRoot, entry.Name(), "backup.json")
		if info, statErr := os.Lstat(manifestPath); statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		status.BackupCount++
		if entry.Name() > filepath.Base(status.LastBackupPath) {
			status.LastBackupPath = filepath.Join(projectRoot, entry.Name())
			if created, parseErr := time.ParseInLocation("20060102-150405", entry.Name(), time.Local); parseErr == nil {
				status.LastBackupAt = created.Format(time.RFC3339)
			}
		}
	}
	status.CanReplaceDatabase = config.Stage == "database_ready" && status.BackupCount > 0
	return status, nil
}

func Create(parent context.Context, config Config, report Reporter) (result Result) {
	if report == nil {
		report = func(Progress) {}
	}
	if err := validateConfig(config, true); err != nil {
		return Result{Message: "Chưa thể tạo bản sao", Detail: err.Error()}
	}
	if !createLock.TryLock() {
		return Result{Message: "Đang tạo một bản sao khác", Detail: "Đợi thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer createLock.Unlock()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()

	report(Progress{Stage: "prepare", Message: "Đang chuẩn bị thư mục backup an toàn…", Percent: 5})
	stamp := time.Now().Format("20060102-150405")
	backupDir, err := prepareBackupDirectory(config.BackupRoot, config.ProjectID, stamp)
	if err != nil {
		return Result{Message: "Không chuẩn bị được thư mục backup", Detail: err.Error()}
	}
	committed := false
	defer func() {
		if !committed {
			_ = removePreparedDirectory(config.BackupRoot, backupDir)
		}
	}()

	report(Progress{Stage: "guest", Message: "Đang kiểm tra máy chủ Ubuntu…", Percent: 12})
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: config.ProjectPath, ProjectName: config.ProjectName})
	if err != nil {
		return Result{Message: "Máy chủ chưa sẵn sàng để backup", Detail: err.Error()}
	}
	if guest.Name() != config.VMName {
		return Result{Message: "Máy chủ không khớp lịch sử", Detail: "verified VM name does not match project state"}
	}
	script, cleanupScript, err := writeGuestScript(config.BackupRoot)
	if err != nil {
		return Result{Message: "Không chuẩn bị được tác vụ backup", Detail: err.Error()}
	}
	defer cleanupScript()
	guestScript := "/home/ubuntu/.oneclick/backup-" + config.ProjectID + ".sh"
	if _, err := guest.Exec(ctx, "mkdir", "-p", "/home/ubuntu/.oneclick"); err != nil {
		return Result{Message: "Không chuẩn bị được vùng xuất dữ liệu", Detail: err.Error()}
	}
	if err := guest.Transfer(ctx, script, guestScript); err != nil {
		return Result{Message: "Không chuyển được tác vụ backup", Detail: err.Error()}
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = guest.Exec(cleanupCtx, "rm", "-f", guestScript)
		_, _ = guest.Exec(cleanupCtx, "rm", "-rf", "/home/ubuntu/.oneclick/exports/"+config.ProjectID+"/"+stamp)
	}()

	report(Progress{Stage: "export", Message: "Đang xuất source và database trong máy ảo…", Percent: 28})
	output, err := guest.Exec(ctx, "sudo", "/usr/bin/bash", guestScript, config.ProjectID, stamp)
	if err != nil {
		return Result{Message: "Không xuất được dữ liệu website", Detail: err.Error()}
	}
	metadata, err := parseExportOutput(output)
	if err != nil {
		return Result{Message: "Kết quả backup không hợp lệ", Detail: err.Error()}
	}

	guestRoot := "/home/ubuntu/.oneclick/exports/" + config.ProjectID + "/" + stamp
	sourcePath := filepath.Join(backupDir, "source.tar.gz")
	databasePath := filepath.Join(backupDir, "database.sql")
	report(Progress{Stage: "download", Message: "Đang lưu bản sao vào ổ đĩa ứng dụng…", Percent: 58})
	if err := guest.Download(ctx, guestRoot+"/source.tar.gz", sourcePath); err != nil {
		return Result{Message: "Không tải được bản sao source", Detail: err.Error()}
	}
	if err := guest.Download(ctx, guestRoot+"/database.sql", databasePath); err != nil {
		return Result{Message: "Không tải được bản sao database", Detail: err.Error()}
	}

	report(Progress{Stage: "verify", Message: "Đang kiểm tra checksum và nội dung backup…", Percent: 82})
	if err := verifyFile(sourcePath, metadata.sourceHash, metadata.sourceBytes); err != nil {
		return Result{Message: "Bản sao source không qua kiểm tra", Detail: err.Error()}
	}
	if err := verifySourceArchive(sourcePath); err != nil {
		return Result{Message: "Bản sao source không an toàn", Detail: err.Error()}
	}
	if err := verifyFile(databasePath, metadata.databaseHash, metadata.databaseBytes); err != nil {
		return Result{Message: "Bản sao database không qua kiểm tra", Detail: err.Error()}
	}
	createdAt := time.Now().Format(time.RFC3339)
	manifest := Manifest{Schema: 1, ProjectID: config.ProjectID, ProjectName: cleanName(config.ProjectName), CreatedAt: createdAt,
		Source:   ManifestFile{Name: "source.tar.gz", Bytes: metadata.sourceBytes, SHA256: metadata.sourceHash},
		Database: ManifestFile{Name: "database.sql", Bytes: metadata.databaseBytes, SHA256: metadata.databaseHash},
	}
	if err := writeManifest(filepath.Join(backupDir, "backup.json"), manifest); err != nil {
		return Result{Message: "Không hoàn tất được manifest backup", Detail: err.Error()}
	}
	committed = true
	report(Progress{Stage: "complete", Message: "Đã tạo bản sao source và database", Percent: 100})
	return Result{Success: true, Message: "Đã tạo bản sao source và database", Path: backupDir,
		SourceBytes: metadata.sourceBytes, DatabaseBytes: metadata.databaseBytes, CreatedAt: createdAt}
}

type exportMetadata struct {
	sourceHash, databaseHash   string
	sourceBytes, databaseBytes int64
}

func parseExportOutput(output string) (exportMetadata, error) {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	result := exportMetadata{sourceHash: values["SOURCE_SHA"], databaseHash: values["DATABASE_SHA"]}
	var err error
	result.sourceBytes, err = parseSize(values["SOURCE_BYTES"])
	if err != nil {
		return exportMetadata{}, err
	}
	result.databaseBytes, err = parseSize(values["DATABASE_BYTES"])
	if err != nil {
		return exportMetadata{}, err
	}
	if !hashPattern.MatchString(result.sourceHash) || !hashPattern.MatchString(result.databaseHash) {
		return exportMetadata{}, errors.New("export checksum is missing or invalid")
	}
	return result, nil
}

func parseSize(value string) (int64, error) {
	size, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || size <= 0 || size > maxBackupFileBytes {
		return 0, errors.New("export size is missing or outside the 4 GB limit")
	}
	return size, nil
}

func validateConfig(config Config, requireReady bool) error {
	if !projectIDPattern.MatchString(config.ProjectID) {
		return errors.New("project id không hợp lệ")
	}
	if strings.TrimSpace(config.ProjectPath) == "" || strings.TrimSpace(config.ProjectName) == "" || strings.TrimSpace(config.BackupRoot) == "" {
		return errors.New("thông tin website hoặc nơi lưu backup chưa đầy đủ")
	}
	if requireReady && config.ProjectKind != "wordpress" {
		return errors.New("phiên bản hiện tại chỉ backup website WordPress")
	}
	if requireReady && !canBackup(config) {
		return errors.New("runtime và database của website chưa sẵn sàng")
	}
	return nil
}

func canBackup(config Config) bool {
	stageReady := config.Stage == "database_ready" || config.Stage == "tunnel_failed" || config.Stage == "public" || config.Stage == "tunnel_stop_failed"
	return config.ProjectKind == "wordpress" && stageReady && config.RuntimeState == "running" && config.RuntimeHealth == "healthy" && config.DatabaseState == "ready"
}

func prepareBackupDirectory(root, projectID, stamp string) (string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absoluteRoot, 0o700); err != nil {
		return "", err
	}
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("backup root is not a trusted directory")
	}
	projectRoot := filepath.Join(absoluteRoot, projectID)
	if err := os.Mkdir(projectRoot, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	projectInfo, err := os.Lstat(projectRoot)
	if err != nil || !projectInfo.IsDir() || projectInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("project backup root is not trusted")
	}
	if !stampPattern.MatchString(stamp) {
		return "", errors.New("backup timestamp is invalid")
	}
	backupDir := filepath.Join(projectRoot, stamp)
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", errors.New("một backup vừa được tạo trong cùng giây; hãy thử lại")
		}
		return "", err
	}
	return backupDir, nil
}

func removePreparedDirectory(root, target string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absoluteRoot, absoluteTarget)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("refusing to remove path outside backup root")
	}
	return os.RemoveAll(absoluteTarget)
}

func writeGuestScript(root string) (string, func(), error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", func() {}, err
	}
	file, err := os.CreateTemp(root, ".backup-script-*.sh")
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
	if _, err := file.WriteString(guestExportScript); err != nil {
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

func verifyFile(filePath, expectedHash string, expectedBytes int64) error {
	info, err := os.Lstat(filePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("artifact is not a regular file")
	}
	if info.Size() != expectedBytes || info.Size() <= 0 || info.Size() > maxBackupFileBytes {
		return errors.New("artifact size does not match export metadata")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	if hex.EncodeToString(digest.Sum(nil)) != expectedHash {
		return errors.New("artifact SHA-256 does not match export metadata")
	}
	return nil
}

func verifySourceArchive(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	entries := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxArchiveEntries {
			return errors.New("source archive exceeds the 100000-entry limit")
		}
		name := strings.TrimPrefix(header.Name, "./")
		clean := pathpkg.Clean(name)
		if clean == "." && header.Typeflag == tar.TypeDir {
			continue
		}
		if name == "" || strings.Contains(name, "\\") || pathpkg.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("source archive contains an unsafe path")
		}
		base := strings.ToLower(pathpkg.Base(clean))
		if base == "wp-config.php" || base == ".env" || strings.HasPrefix(base, ".env.") {
			return errors.New("source archive contains a blocked secret file")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return errors.New("source archive contains a link or special file")
		}
		if header.Size < 0 || header.Size > 512*1024*1024 {
			return errors.New("source archive contains an oversized file")
		}
	}
	if entries == 0 {
		return errors.New("source archive is empty")
	}
	return nil
}

func writeManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func cleanName(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}
