package project

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxSnapshotFiles     = 100_000
	maxSnapshotBytes     = int64(4 * 1024 * 1024 * 1024)
	maxSnapshotFileBytes = int64(512 * 1024 * 1024)
	manifestArchivePath  = ".oneclick-manifest.json"
)

type CopyRequest struct {
	ProjectPath string `json:"projectPath"`
}

type CopyPlan struct {
	ProjectPath    string `json:"projectPath"`
	FileCount      int    `json:"fileCount"`
	TotalBytes     int64  `json:"totalBytes"`
	ExcludedCount  int    `json:"excludedCount"`
	SecretExcluded int    `json:"secretExcluded"`
	Policy         string `json:"policy"`
}

type CopyProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

type CopyResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	SnapshotID string `json:"snapshotId,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	FileCount  int    `json:"fileCount,omitempty"`
	TotalBytes int64  `json:"totalBytes,omitempty"`
	GuestPath  string `json:"guestPath,omitempty"`
}

type CopyReporter func(CopyProgress)

type SnapshotArtifact struct {
	ArchivePath string
	Checksum    string
	SnapshotID  string
	FileCount   int
	TotalBytes  int64
	cleanup     func()
}

type snapshotEntry struct {
	absPath string
	relPath string
	info    fs.FileInfo
}

type manifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   int64  `json:"mode"`
	SHA256 string `json:"sha256"`
}

type snapshotManifest struct {
	Schema       int            `json:"schema"`
	SourcePolicy string         `json:"sourcePolicy"`
	FileCount    int            `json:"fileCount"`
	TotalBytes   int64          `json:"totalBytes"`
	Files        []manifestFile `json:"files"`
}

func (artifact SnapshotArtifact) Cleanup() {
	if artifact.cleanup != nil {
		artifact.cleanup()
	}
}

func PrepareCopyPlan(projectPath, documentRoot string) (CopyPlan, error) {
	root, entries, excluded, secrets, total, err := scanSnapshot(projectPath)
	if err != nil {
		return CopyPlan{}, err
	}
	if err := validateDocumentRootForCopy(root, documentRoot); err != nil {
		return CopyPlan{}, err
	}
	return CopyPlan{
		ProjectPath: root, FileCount: len(entries), TotalBytes: total,
		ExcludedCount: excluded, SecretExcluded: secrets,
		Policy: "Chỉ sao chép file thường; bỏ file bí mật, cache, log, backup và dependency có thể tạo lại.",
	}, nil
}

func BuildSnapshot(projectPath, documentRoot string, report CopyReporter) (SnapshotArtifact, error) {
	root, entries, _, _, total, err := scanSnapshot(projectPath)
	if err != nil {
		return SnapshotArtifact{}, err
	}
	if err := validateDocumentRootForCopy(root, documentRoot); err != nil {
		return SnapshotArtifact{}, err
	}

	temporary, err := os.CreateTemp("", "oneclick-snapshot-*.tar.gz")
	if err != nil {
		return SnapshotArtifact{}, fmt.Errorf("không tạo được file snapshot tạm: %w", err)
	}
	archivePath := temporary.Name()
	cleanup := func() { _ = os.Remove(archivePath) }
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		cleanup()
		return SnapshotArtifact{}, err
	}

	archiveHash := sha256.New()
	gzipWriter, err := gzip.NewWriterLevel(io.MultiWriter(temporary, archiveHash), gzip.BestSpeed)
	if err != nil {
		temporary.Close()
		cleanup()
		return SnapshotArtifact{}, err
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	manifest := snapshotManifest{
		Schema: 1, SourcePolicy: "one-way immutable snapshot; secrets excluded",
		FileCount: len(entries), TotalBytes: total, Files: make([]manifestFile, 0, len(entries)),
	}

	closeFailed := func(writeErr error) (SnapshotArtifact, error) {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = temporary.Close()
		cleanup()
		return SnapshotArtifact{}, writeErr
	}

	for index, entry := range entries {
		if report != nil && (index == 0 || index%100 == 0 || index == len(entries)-1) {
			percent := 12
			if len(entries) > 0 {
				percent = 12 + (index+1)*40/len(entries)
			}
			report(CopyProgress{Stage: "archive", Message: "Đang đóng gói bản sao an toàn…", Percent: percent})
		}
		file, openErr := os.Open(entry.absPath)
		if openErr != nil {
			return closeFailed(fmt.Errorf("không đọc được %s: %w", entry.relPath, openErr))
		}
		openedInfo, statErr := file.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(entry.info, openedInfo) {
			file.Close()
			return closeFailed(fmt.Errorf("file đã thay đổi trong lúc tạo snapshot: %s", entry.relPath))
		}
		mode := int64(0o644)
		if entry.info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		header := &tar.Header{
			Name: filepath.ToSlash(entry.relPath), Mode: mode, Size: entry.info.Size(),
			ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Time{}, ChangeTime: time.Time{},
			Typeflag: tar.TypeReg, Uid: 0, Gid: 0, Uname: "", Gname: "", Format: tar.FormatPAX,
		}
		if writeErr := tarWriter.WriteHeader(header); writeErr != nil {
			file.Close()
			return closeFailed(writeErr)
		}
		fileHash := sha256.New()
		written, copyErr := io.CopyN(io.MultiWriter(tarWriter, fileHash), file, entry.info.Size())
		finalInfo, finalErr := file.Stat()
		closeErr := file.Close()
		if copyErr != nil || written != entry.info.Size() || finalErr != nil || finalInfo.Size() != entry.info.Size() || !finalInfo.ModTime().Equal(entry.info.ModTime()) {
			return closeFailed(fmt.Errorf("file đã thay đổi trong lúc tạo snapshot: %s", entry.relPath))
		}
		if closeErr != nil {
			return closeFailed(closeErr)
		}
		manifest.Files = append(manifest.Files, manifestFile{
			Path: filepath.ToSlash(entry.relPath), Size: entry.info.Size(), Mode: mode,
			SHA256: hex.EncodeToString(fileHash.Sum(nil)),
		})
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return closeFailed(err)
	}
	manifestHeader := &tar.Header{
		Name: manifestArchivePath, Mode: 0o444, Size: int64(len(manifestBytes)),
		ModTime: time.Unix(0, 0).UTC(), Typeflag: tar.TypeReg, Format: tar.FormatPAX,
	}
	if err := tarWriter.WriteHeader(manifestHeader); err != nil {
		return closeFailed(err)
	}
	if _, err := tarWriter.Write(manifestBytes); err != nil {
		return closeFailed(err)
	}
	if err := tarWriter.Close(); err != nil {
		return closeFailed(err)
	}
	if err := gzipWriter.Close(); err != nil {
		temporary.Close()
		cleanup()
		return SnapshotArtifact{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		cleanup()
		return SnapshotArtifact{}, err
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return SnapshotArtifact{}, err
	}

	checksum := hex.EncodeToString(archiveHash.Sum(nil))
	return SnapshotArtifact{
		ArchivePath: archivePath, Checksum: checksum, SnapshotID: checksum[:16],
		FileCount: len(entries), TotalBytes: total, cleanup: cleanup,
	}, nil
}

func scanSnapshot(projectPath string) (string, []snapshotEntry, int, int, int64, error) {
	info, err := Detect(projectPath)
	if err != nil {
		return "", nil, 0, 0, 0, err
	}
	root, err := filepath.EvalSymlinks(info.Path)
	if err != nil {
		return "", nil, 0, 0, 0, fmt.Errorf("không xác minh được thư mục website: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", nil, 0, 0, 0, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return "", nil, 0, 0, 0, errors.New("thư mục website không còn tồn tại")
	}
	if unsafe, attrErr := unsafeLinkOrReparse(root, rootInfo); attrErr != nil || unsafe {
		if attrErr != nil {
			return "", nil, 0, 0, 0, attrErr
		}
		return "", nil, 0, 0, 0, errors.New("thư mục website là liên kết; hãy chọn thư mục thật")
	}

	entries := make([]snapshotEntry, 0, 1024)
	excludedCount := 0
	secretExcluded := 0
	var total int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return fmt.Errorf("đường dẫn thoát khỏi website: %s", path)
		}
		excluded, secret := exclusionReason(relative, entry.IsDir())
		if excluded {
			excludedCount++
			if secret {
				secretExcluded++
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		unsafe, attrErr := unsafeLinkOrReparse(path, entryInfo)
		if attrErr != nil {
			return attrErr
		}
		if unsafe {
			return fmt.Errorf("phát hiện liên kết/junction không an toàn: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("chỉ hỗ trợ file thường: %s", relative)
		}
		if entryInfo.Size() > maxSnapshotFileBytes {
			return fmt.Errorf("file vượt giới hạn 512 MB: %s", relative)
		}
		if len(entries)+1 > maxSnapshotFiles {
			return fmt.Errorf("website vượt giới hạn %d file", maxSnapshotFiles)
		}
		if total > maxSnapshotBytes-entryInfo.Size() {
			return errors.New("website vượt giới hạn snapshot 4 GB")
		}
		total += entryInfo.Size()
		entries = append(entries, snapshotEntry{absPath: path, relPath: relative, info: entryInfo})
		return nil
	})
	if err != nil {
		return "", nil, 0, 0, 0, fmt.Errorf("không thể tạo danh sách sao chép: %w", err)
	}
	if len(entries) == 0 {
		return "", nil, 0, 0, 0, errors.New("website không có file hợp lệ để sao chép")
	}
	sort.Slice(entries, func(i, j int) bool {
		return filepath.ToSlash(entries[i].relPath) < filepath.ToSlash(entries[j].relPath)
	})
	return root, entries, excludedCount, secretExcluded, total, nil
}

func validateDocumentRootForCopy(root, documentRoot string) error {
	documentRoot = strings.TrimSpace(documentRoot)
	if documentRoot == "" || filepath.IsAbs(documentRoot) {
		return errors.New("thư mục chạy phải nằm trong website")
	}
	cleaned := filepath.Clean(documentRoot)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return errors.New("thư mục chạy không được thoát khỏi website")
	}
	target := filepath.Join(root, cleaned)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return errors.New("không tìm thấy thư mục chạy trong snapshot")
	}
	if excluded, _ := exclusionReason(cleaned, true); excluded {
		return errors.New("thư mục chạy đang thuộc danh sách không sao chép")
	}
	return nil
}

func exclusionReason(relative string, directory bool) (bool, bool) {
	normalized := strings.ToLower(filepath.ToSlash(relative))
	parts := strings.Split(normalized, "/")
	base := parts[len(parts)-1]

	for _, part := range parts {
		switch part {
		case ".git", ".svn", ".hg", ".idea", ".vscode", "node_modules", ".oneclick":
			return true, false
		}
	}
	if directory {
		if normalized == "logs" || normalized == "log" || normalized == "tmp" || normalized == "temp" ||
			strings.HasPrefix(normalized, "storage/logs") || strings.HasPrefix(normalized, "storage/framework/cache") ||
			strings.HasPrefix(normalized, "storage/framework/sessions") || strings.HasPrefix(normalized, "storage/framework/views") ||
			strings.HasPrefix(normalized, "bootstrap/cache") || strings.HasPrefix(normalized, "wp-content/cache") {
			return true, false
		}
		return false, false
	}

	if base == manifestArchivePath || base == ".ds_store" || base == "thumbs.db" || base == "desktop.ini" {
		return true, false
	}
	if base == ".env" || (strings.HasPrefix(base, ".env.") && base != ".env.example" && base != ".env.sample" && base != ".env.dist") ||
		base == "wp-config.php" || base == "id_rsa" || base == "id_ed25519" || base == "authorized_keys" || base == "known_hosts" ||
		base == ".htpasswd" || base == ".npmrc" || base == ".netrc" || base == "auth.json" || base == "secrets.json" || base == "credentials.json" {
		return true, true
	}
	secretExtensions := []string{".pem", ".key", ".pfx", ".p12", ".crt", ".cer", ".jks", ".keystore"}
	for _, extension := range secretExtensions {
		if strings.HasSuffix(base, extension) {
			return true, true
		}
	}
	if strings.HasSuffix(base, ".log") || base == "error_log" || strings.HasSuffix(base, ".sql") ||
		strings.HasSuffix(base, ".sql.gz") || strings.HasSuffix(base, ".dump") || strings.HasSuffix(base, ".bak") ||
		strings.HasSuffix(base, ".backup") {
		return true, false
	}
	return false, false
}
