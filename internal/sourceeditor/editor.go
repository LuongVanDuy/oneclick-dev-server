package sourceeditor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxEditableBytes = int64(2 * 1024 * 1024)

var (
	projectIDPattern = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
	shaPattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	editLock         sync.Mutex
	textExtensions   = map[string]bool{
		".php": true, ".css": true, ".scss": true, ".sass": true, ".less": true,
		".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
		".json": true, ".html": true, ".htm": true, ".xml": true, ".svg": true,
		".md": true, ".txt": true, ".yml": true, ".yaml": true, ".ini": true,
		".conf": true, ".config": true, ".toml": true, ".csv": true, ".po": true, ".pot": true,
	}
)

func List(config Config, directory string) (Listing, error) {
	if err := validateConfig(config); err != nil {
		return Listing{}, err
	}
	absolute, relative, err := resolve(config.ProjectPath, directory, true)
	if err != nil {
		return Listing{}, err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return Listing{}, fmt.Errorf("không đọc được thư mục source: %w", err)
	}
	if len(entries) > 5000 {
		return Listing{}, errors.New("thư mục có quá 5.000 mục; hãy mở bằng Explorer")
	}
	result := Listing{ProjectPath: config.ProjectPath, Directory: relative, Entries: make([]Entry, 0, len(entries))}
	if relative != "" {
		parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(relative)))
		if parent == "." {
			parent = ""
		}
		result.Parent = parent
	}
	for _, item := range entries {
		itemRelative := filepath.ToSlash(filepath.Join(filepath.FromSlash(relative), item.Name()))
		info, infoErr := os.Lstat(filepath.Join(absolute, item.Name()))
		entry := Entry{Name: item.Name(), Path: itemRelative}
		if infoErr != nil {
			entry.Kind = "blocked"
			entry.BlockedReason = "Không đọc được thông tin file"
			result.Entries = append(result.Entries, entry)
			continue
		}
		entry.ModifiedAt = info.ModTime().Format(time.RFC3339)
		if isLinkLike(info) {
			entry.Kind = "blocked"
			entry.BlockedReason = "Liên kết bị chặn"
		} else if info.IsDir() {
			entry.Kind = "directory"
			if reason := blockedPathReason(itemRelative, true); reason != "" {
				entry.BlockedReason = reason
			} else {
				entry.Editable = true
			}
		} else if info.Mode().IsRegular() {
			entry.Kind = "file"
			entry.Size = info.Size()
			entry.Editable, entry.BlockedReason = editableFile(itemRelative, info.Size())
		} else {
			entry.Kind = "blocked"
			entry.BlockedReason = "Loại file không được hỗ trợ"
		}
		result.Entries = append(result.Entries, entry)
	}
	sort.SliceStable(result.Entries, func(left, right int) bool {
		leftDirectory := result.Entries[left].Kind == "directory"
		rightDirectory := result.Entries[right].Kind == "directory"
		if leftDirectory != rightDirectory {
			return leftDirectory
		}
		return strings.ToLower(result.Entries[left].Name) < strings.ToLower(result.Entries[right].Name)
	})
	return result, nil
}

func Read(config Config, relativePath string) (File, error) {
	if err := validateConfig(config); err != nil {
		return File{}, err
	}
	absolute, relative, err := resolve(config.ProjectPath, relativePath, false)
	if err != nil {
		return File{}, err
	}
	before, err := os.Lstat(absolute)
	if err != nil {
		return File{}, err
	}
	editable, reason := editableFile(relative, before.Size())
	if !editable {
		return File{}, errors.New(reason)
	}
	handle, err := os.Open(absolute)
	if err != nil {
		return File{}, err
	}
	defer handle.Close()
	during, err := handle.Stat()
	if err != nil || !during.Mode().IsRegular() || !os.SameFile(before, during) {
		return File{}, errors.New("file đã thay đổi trong lúc mở")
	}
	data, err := io.ReadAll(io.LimitReader(handle, maxEditableBytes+1))
	if err != nil {
		return File{}, err
	}
	if int64(len(data)) > maxEditableBytes || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return File{}, errors.New("file không phải văn bản UTF-8 hoặc vượt quá 2 MB")
	}
	after, err := os.Lstat(absolute)
	if err != nil || isLinkLike(after) || !os.SameFile(during, after) || during.Size() != after.Size() || !during.ModTime().Equal(after.ModTime()) {
		return File{}, errors.New("file đã thay đổi trong lúc đọc; hãy mở lại")
	}
	digest := sha256.Sum256(data)
	return File{Path: relative, Name: filepath.Base(absolute), Content: string(data), SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data)), ModifiedAt: after.ModTime().Format(time.RFC3339)}, nil
}

func Save(config Config, request SaveRequest) SaveResult {
	if err := validateConfig(config); err != nil {
		return SaveResult{Message: "Không thể lưu source", Detail: err.Error()}
	}
	if !shaPattern.MatchString(strings.ToLower(strings.TrimSpace(request.ExpectedSHA))) {
		return SaveResult{Message: "Không thể lưu source", Detail: "checksum phiên bản đang sửa không hợp lệ"}
	}
	data := []byte(request.Content)
	if int64(len(data)) > maxEditableBytes || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return SaveResult{Message: "Không thể lưu source", Detail: "nội dung phải là UTF-8 và không vượt quá 2 MB"}
	}
	editLock.Lock()
	defer editLock.Unlock()

	current, err := Read(config, request.Path)
	if err != nil {
		return SaveResult{Message: "Không đọc được file trước khi lưu", Detail: err.Error()}
	}
	expected := strings.ToLower(strings.TrimSpace(request.ExpectedSHA))
	if current.SHA256 != expected {
		return SaveResult{Message: "File đã được thay đổi ở nơi khác", Detail: "Mở lại file để tránh ghi đè thay đổi mới."}
	}
	absolute, relative, err := resolve(config.ProjectPath, request.Path, false)
	if err != nil {
		return SaveResult{Message: "Đường dẫn source không hợp lệ", Detail: err.Error()}
	}
	backupPath, err := createBackup(config, relative, []byte(current.Content), current.SHA256)
	if err != nil {
		return SaveResult{Message: "Không tạo được bản dự phòng", Detail: err.Error()}
	}

	directory := filepath.Dir(absolute)
	temporary, err := os.CreateTemp(directory, ".oneclick-edit-*.tmp")
	if err != nil {
		return SaveResult{Message: "Không tạo được file tạm", Detail: err.Error()}
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(absolute); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := temporary.Chmod(mode); err != nil {
		return SaveResult{Message: "Không giữ được quyền file", Detail: err.Error()}
	}
	if _, err := temporary.Write(data); err != nil {
		return SaveResult{Message: "Không ghi được file tạm", Detail: err.Error()}
	}
	if err := temporary.Sync(); err != nil {
		return SaveResult{Message: "Không đồng bộ được file tạm", Detail: err.Error()}
	}
	if err := temporary.Close(); err != nil {
		return SaveResult{Message: "Không đóng được file tạm", Detail: err.Error()}
	}
	latest, err := Read(config, relative)
	if err != nil || latest.SHA256 != expected {
		return SaveResult{Message: "File đã được thay đổi ở nơi khác", Detail: "Bản dự phòng đã giữ nguyên; mở lại file rồi thử lại."}
	}
	if err := os.Rename(temporaryPath, absolute); err != nil {
		return SaveResult{Message: "Không thay được file source", Detail: err.Error()}
	}
	committed = true
	verified, err := Read(config, relative)
	if err != nil {
		return SaveResult{Message: "File đã lưu nhưng chưa xác minh được", Detail: err.Error(), BackupPath: backupPath}
	}
	newDigest := sha256.Sum256(data)
	newHash := hex.EncodeToString(newDigest[:])
	if verified.SHA256 != newHash {
		return SaveResult{Message: "File đã lưu nhưng checksum không khớp", Detail: "Dùng bản dự phòng để phục hồi.", BackupPath: backupPath}
	}
	savedAt := time.Now().Format(time.RFC3339)
	return SaveResult{Success: true, Message: "Đã lưu source gốc", Path: relative, SHA256: newHash, Bytes: int64(len(data)), BackupPath: backupPath, SavedAt: savedAt}
}

func validateConfig(config Config) error {
	if !projectIDPattern.MatchString(config.ProjectID) {
		return errors.New("project id không hợp lệ")
	}
	if strings.TrimSpace(config.ProjectPath) == "" || strings.TrimSpace(config.ProjectName) == "" || strings.TrimSpace(config.BackupRoot) == "" {
		return errors.New("thông tin source editor chưa đầy đủ")
	}
	root, err := filepath.Abs(config.ProjectPath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || isLinkLike(info) {
		return errors.New("thư mục source gốc không còn an toàn hoặc không tồn tại")
	}
	return nil
}

func resolve(root, requested string, directory bool) (string, string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	normalized := strings.TrimSpace(strings.ReplaceAll(requested, "\\", "/"))
	if normalized == "." {
		normalized = ""
	}
	if !directory && normalized == "" {
		return "", "", errors.New("chưa chọn file")
	}
	if strings.HasPrefix(normalized, "/") || filepath.IsAbs(filepath.FromSlash(normalized)) || filepath.VolumeName(filepath.FromSlash(normalized)) != "" {
		return "", "", errors.New("đường dẫn source phải nằm trong website")
	}
	clean := filepath.Clean(filepath.FromSlash(normalized))
	if clean == "." {
		clean = ""
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", errors.New("đường dẫn source thoát khỏi website")
	}
	target := filepath.Join(absoluteRoot, clean)
	relative, err := filepath.Rel(absoluteRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", errors.New("đường dẫn source thoát khỏi website")
	}
	current := absoluteRoot
	if clean != "" {
		for _, part := range strings.Split(clean, string(filepath.Separator)) {
			if part == "" || part == "." {
				continue
			}
			current = filepath.Join(current, part)
			info, statErr := os.Lstat(current)
			if statErr != nil {
				return "", "", statErr
			}
			if isLinkLike(info) {
				return "", "", errors.New("không mở symlink hoặc junction trong source")
			}
		}
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", "", err
	}
	if directory && !info.IsDir() {
		return "", "", errors.New("đường dẫn không phải thư mục")
	}
	if !directory && !info.Mode().IsRegular() {
		return "", "", errors.New("đường dẫn không phải regular file")
	}
	relativeText := filepath.ToSlash(relative)
	if relativeText == "." {
		relativeText = ""
	}
	return target, relativeText, nil
}

func editableFile(relative string, size int64) (bool, string) {
	if reason := blockedPathReason(relative, false); reason != "" {
		return false, reason
	}
	if size < 0 || size > maxEditableBytes {
		return false, "File vượt quá giới hạn 2 MB"
	}
	base := strings.ToLower(filepath.Base(relative))
	extension := strings.ToLower(filepath.Ext(base))
	if textExtensions[extension] || base == "license" || base == "readme" || base == ".htaccess" || base == ".gitignore" || base == "composer.lock" {
		return true, ""
	}
	return false, "Chỉ mở file văn bản/source phổ biến"
}

func blockedPathReason(relative string, _ bool) string {
	parts := strings.Split(strings.ToLower(filepath.ToSlash(relative)), "/")
	for _, part := range parts {
		if part == ".git" {
			return "Dữ liệu Git được bảo vệ"
		}
		if part == "node_modules" || part == "vendor" {
			return "Thư mục dependency không mở trong ứng dụng"
		}
	}
	base := parts[len(parts)-1]
	if base == "wp-config.php" || base == ".env" || strings.HasPrefix(base, ".env.") || base == ".htpasswd" || base == "auth.json" || base == ".npmrc" || base == "id_rsa" || base == "id_ed25519" {
		return "File chứa thông tin mật được bảo vệ"
	}
	for _, extension := range []string{".key", ".pem", ".p12", ".pfx"} {
		if strings.HasSuffix(base, extension) {
			return "Private key được bảo vệ"
		}
	}
	return ""
}

type editManifest struct {
	Schema      int    `json:"schema"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	SourcePath  string `json:"sourcePath"`
	OriginalSHA string `json:"originalSha256"`
	CreatedAt   string `json:"createdAt"`
}

func createBackup(config Config, relative string, data []byte, originalSHA string) (string, error) {
	root, err := filepath.Abs(config.BackupRoot)
	if err != nil {
		return "", err
	}
	projectRoot := filepath.Join(root, config.ProjectID)
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(projectRoot)
	if err != nil || !info.IsDir() || isLinkLike(info) {
		return "", errors.New("thư mục dự phòng source không an toàn")
	}
	stamp := time.Now().Format("20060102-150405.000000000")
	backupDir := filepath.Join(projectRoot, stamp)
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(backupDir)
		}
	}()
	backupFile := filepath.Join(backupDir, "original"+filepath.Ext(relative))
	if err := os.WriteFile(backupFile, data, 0o600); err != nil {
		return "", err
	}
	manifest := editManifest{Schema: 1, ProjectID: config.ProjectID, ProjectName: config.ProjectName, SourcePath: filepath.ToSlash(relative), OriginalSHA: originalSHA, CreatedAt: time.Now().Format(time.RFC3339)}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(backupDir, "edit.json"), append(encoded, '\n'), 0o600); err != nil {
		return "", err
	}
	complete = true
	return backupDir, nil
}
