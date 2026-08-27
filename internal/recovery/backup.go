package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	maxRemoteFiles       = 100000
	maxRemoteDirectories = 20000
	maxRemoteDepth       = 64
	maxBackupBytes       = int64(20 * 1024 * 1024 * 1024)
	maxRemoteFileBytes   = int64(2 * 1024 * 1024 * 1024)
	maxDirectoryList     = int64(32 * 1024 * 1024)
	maxScanBytes         = int64(4 * 1024 * 1024)
	maxFindings          = 500
	backupTimeout        = 2 * time.Hour
)

type remoteFile struct {
	Path string
	Size int64
}

type manifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Blob   string `json:"blob"`
}

type backupManifest struct {
	Schema      int            `json:"schema"`
	BackupID    string         `json:"backupId"`
	ProjectID   string         `json:"projectId"`
	Host        string         `json:"host"`
	RemotePath  string         `json:"remotePath"`
	CreatedAt   string         `json:"createdAt"`
	StorageMode string         `json:"storageMode"`
	Files       []manifestFile `json:"files"`
	TotalBytes  int64          `json:"totalBytes"`
}

type scanFinding struct {
	Path     string `json:"path"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
}

type scanReport struct {
	Schema     int           `json:"schema"`
	BackupID   string        `json:"backupId"`
	CreatedAt  string        `json:"createdAt"`
	Files      int           `json:"files"`
	Findings   int           `json:"findings"`
	Critical   int           `json:"critical"`
	High       int           `json:"high"`
	Disclaimer string        `json:"disclaimer"`
	Items      []scanFinding `json:"items"`
}

// Backup downloads remote files without changing the hosting. Untrusted
// bytes are stored under content-addressed .ocblob names, never their original
// executable extension, and are never executed or extracted by this package.
func Backup(parent context.Context, project Project, password, projectDirectory string, reporter func(BackupProgress)) BackupResult {
	createdAt := nowText()
	result := BackupResult{Message: "Không sao lưu được website", CreatedAt: createdAt}
	if password == "" {
		result.Detail = "Mật khẩu FTP/FTPS chưa có trong kho bí mật của hệ điều hành."
		return result
	}
	if normalizeID(project.ID) == "" {
		result.Detail = "Mã website khôi phục không hợp lệ."
		return result
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, backupTimeout)
	defer cancel()
	emit := func(progress BackupProgress) {
		if progress.Percent < 0 {
			progress.Percent = 0
		}
		if progress.Percent > 100 {
			progress.Percent = 100
		}
		if reporter != nil {
			reporter(progress)
		}
	}

	emit(BackupProgress{Stage: "connect", Message: "Đang xác nhận kết nối chỉ đọc…", Percent: 4})
	listingClient, err := openAuthenticatedFTPSession(ctx, project, password, nil)
	if err != nil {
		result.Detail = cleanConnectionError(err)
		return result
	}
	defer listingClient.close()

	emit(BackupProgress{Stage: "discover", Message: "Đang lập danh sách file trên hosting…", Percent: 9})
	files, totalBytes, err := discoverRemoteFiles(ctx, listingClient, emit)
	if err != nil {
		result.Detail = cleanBackupError(err)
		return result
	}
	if len(files) == 0 {
		result.Detail = "Thư mục website không có file để sao lưu."
		return result
	}

	backupID, err := newBackupID()
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	backupRoot := filepath.Join(projectDirectory, "backups")
	partialPath := filepath.Join(backupRoot, ".partial-"+backupID)
	finalPath := filepath.Join(backupRoot, backupID)
	if err := os.MkdirAll(filepath.Join(partialPath, "blobs"), 0o700); err != nil {
		result.Detail = fmt.Sprintf("không tạo được kho sao lưu: %v", err)
		return result
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(partialPath)
		}
	}()

	emit(BackupProgress{Stage: "download", Message: "Đang tải file vào kho blob an toàn…", Percent: 20, FilesTotal: len(files), BytesTotal: totalBytes})
	manifestFiles, err := downloadRemoteFiles(ctx, project, password, files, totalBytes, partialPath, emit)
	if err != nil {
		result.Detail = cleanBackupError(err)
		return result
	}

	emit(BackupProgress{Stage: "verify", Message: "Đang xác minh SHA-256 của bản sao…", Percent: 76, FilesDone: len(files), FilesTotal: len(files), BytesDone: totalBytes, BytesTotal: totalBytes})
	if err := verifyBlobs(partialPath, manifestFiles); err != nil {
		result.Detail = cleanBackupError(err)
		return result
	}

	emit(BackupProgress{Stage: "scan", Message: "Đang quét dấu hiệu mã độc, không chạy source…", Percent: 86, FilesDone: len(files), FilesTotal: len(files), BytesDone: totalBytes, BytesTotal: totalBytes})
	report := scanBackup(partialPath, backupID, manifestFiles)
	manifest := backupManifest{
		Schema: 1, BackupID: backupID, ProjectID: project.ID, Host: project.Host,
		RemotePath: project.RemotePath, CreatedAt: createdAt,
		StorageMode: "content-addressed-neutral-ocblob-sha256", Files: manifestFiles, TotalBytes: totalBytes,
	}
	if err := writeJSON(filepath.Join(partialPath, "manifest.json"), manifest); err != nil {
		result.Detail = fmt.Sprintf("không ghi được manifest: %v", err)
		return result
	}
	if err := writeJSON(filepath.Join(partialPath, "scan-report.json"), report); err != nil {
		result.Detail = fmt.Sprintf("không ghi được báo cáo quét: %v", err)
		return result
	}
	if err := os.Rename(partialPath, finalPath); err != nil {
		result.Detail = fmt.Sprintf("không hoàn tất được kho sao lưu: %v", err)
		return result
	}
	committed = true
	emit(BackupProgress{Stage: "complete", Message: "Đã sao lưu và quét xong", Percent: 100, FilesDone: len(files), FilesTotal: len(files), BytesDone: totalBytes, BytesTotal: totalBytes})
	result.Success = true
	result.Message = "Đã sao lưu và quét website"
	result.BackupID = backupID
	result.Path = finalPath
	result.ManifestPath = filepath.Join(finalPath, "manifest.json")
	result.ReportPath = filepath.Join(finalPath, "scan-report.json")
	result.Files = len(files)
	result.Bytes = totalBytes
	result.Findings = report.Findings
	result.Critical = report.Critical
	return result
}

func discoverRemoteFiles(ctx context.Context, client *authenticatedFTPSession, emit func(BackupProgress)) ([]remoteFile, int64, error) {
	type directory struct {
		remote, relative string
		depth            int
	}
	queue := []directory{{remote: client.project.RemotePath, relative: "", depth: 0}}
	files := make([]remoteFile, 0, 1024)
	var totalBytes int64
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		current := queue[0]
		queue = queue[1:]
		if current.depth > maxRemoteDepth || len(queue) > maxRemoteDirectories {
			return nil, 0, errors.New("cấu trúc thư mục website vượt giới hạn an toàn")
		}
		entries, err := client.listDirectory(ctx, current.remote)
		if err != nil {
			return nil, 0, fmt.Errorf("không đọc được thư mục %s: %w", current.remote, err)
		}
		for _, entry := range entries {
			relative := entry.name
			if current.relative != "" {
				relative = path.Join(current.relative, entry.name)
			}
			if len(relative) > 2048 {
				return nil, 0, errors.New("đường dẫn file trên hosting vượt giới hạn an toàn")
			}
			remote := path.Join(current.remote, entry.name)
			if entry.directory {
				queue = append(queue, directory{remote: remote, relative: relative, depth: current.depth + 1})
				if len(queue) > maxRemoteDirectories {
					return nil, 0, errors.New("website có quá nhiều thư mục")
				}
				continue
			}
			if entry.size < 0 || entry.size > maxRemoteFileBytes {
				return nil, 0, fmt.Errorf("file %s vượt giới hạn 2 GB", relative)
			}
			totalBytes += entry.size
			if totalBytes > maxBackupBytes {
				return nil, 0, errors.New("website vượt giới hạn sao lưu 20 GB")
			}
			files = append(files, remoteFile{Path: relative, Size: entry.size})
			if len(files) > maxRemoteFiles {
				return nil, 0, errors.New("website có quá 100.000 file")
			}
		}
		percent := 9 + len(files)/2500
		if percent > 18 {
			percent = 18
		}
		emit(BackupProgress{Stage: "discover", Message: fmt.Sprintf("Đã tìm thấy %d file…", len(files)), Percent: percent, FilesTotal: len(files), BytesTotal: totalBytes})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, totalBytes, nil
}

type mlsdEntry struct {
	name      string
	directory bool
	size      int64
}

func (client *authenticatedFTPSession) listDirectory(ctx context.Context, remotePath string) ([]mlsdEntry, error) {
	data, err := client.dataCommand(ctx, "MLSD "+remotePath, maxDirectoryList)
	if err != nil {
		return nil, fmt.Errorf("hosting không hỗ trợ MLSD an toàn: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	entries := make([]mlsdEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		space := strings.IndexByte(line, ' ')
		if space <= 0 || space == len(line)-1 {
			return nil, errors.New("phản hồi MLSD không hợp lệ")
		}
		factsText, name := line[:space], strings.TrimLeft(line[space+1:], " ")
		if !safeRemoteName(name) {
			return nil, errors.New("hosting trả về tên file không an toàn")
		}
		facts := map[string]string{}
		for _, fact := range strings.Split(factsText, ";") {
			if separator := strings.IndexByte(fact, '='); separator > 0 {
				facts[strings.ToLower(fact[:separator])] = fact[separator+1:]
			}
		}
		switch strings.ToLower(facts["type"]) {
		case "cdir", "pdir":
			continue
		case "dir":
			entries = append(entries, mlsdEntry{name: name, directory: true})
		case "file":
			size, err := strconv.ParseInt(facts["size"], 10, 64)
			if err != nil || size < 0 {
				return nil, fmt.Errorf("hosting không cung cấp kích thước an toàn cho file %s", name)
			}
			entries = append(entries, mlsdEntry{name: name, size: size})
		default:
			return nil, fmt.Errorf("hosting trả về loại file không hỗ trợ cho %s", name)
		}
	}
	return entries, nil
}

func safeRemoteName(name string) bool {
	if name == "" || name == "." || name == ".." || len([]rune(name)) > 255 || strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func (client *authenticatedFTPSession) dataCommand(ctx context.Context, command string, limit int64) ([]byte, error) {
	dataConnection, err := client.openPassiveConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer dataConnection.Close()
	if err := client.session.writeCommand(command); err != nil {
		return nil, err
	}
	code, _, err := client.session.readReply()
	if err != nil || (code != 125 && code != 150) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("máy chủ từ chối truyền dữ liệu (mã %d)", code)
	}
	dataConnection, err = client.secureDataConnection(ctx, dataConnection)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(dataConnection, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("dữ liệu trả về vượt giới hạn")
	}
	_ = dataConnection.Close()
	code, _, err = client.session.readReply()
	if err != nil || (code != 226 && code != 250) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("truyền dữ liệu không hoàn tất (mã %d)", code)
	}
	return data, nil
}

func (client *authenticatedFTPSession) retrieve(ctx context.Context, remotePath string, output io.Writer, expected int64) (int64, error) {
	if err := client.session.writeCommand("TYPE I"); err != nil {
		return 0, err
	}
	if code, _, err := client.session.readReply(); err != nil || code/100 != 2 {
		return 0, errors.New("máy chủ không bật được chế độ tải binary")
	}
	dataConnection, err := client.openPassiveConnection(ctx)
	if err != nil {
		return 0, err
	}
	defer dataConnection.Close()
	if err := client.session.writeCommand("RETR " + remotePath); err != nil {
		return 0, err
	}
	code, _, err := client.session.readReply()
	if err != nil || (code != 125 && code != 150) {
		if err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("máy chủ từ chối tải file (mã %d)", code)
	}
	dataConnection, err = client.secureDataConnection(ctx, dataConnection)
	if err != nil {
		return 0, err
	}
	written, err := io.Copy(output, io.LimitReader(dataConnection, maxRemoteFileBytes+1))
	if err != nil {
		return written, err
	}
	if written > maxRemoteFileBytes || written != expected {
		return written, fmt.Errorf("kích thước tải về không khớp (%d/%d byte)", written, expected)
	}
	_ = dataConnection.Close()
	code, _, err = client.session.readReply()
	if err != nil || (code != 226 && code != 250) {
		if err != nil {
			return written, err
		}
		return written, fmt.Errorf("file tải về chưa hoàn tất (mã %d)", code)
	}
	return written, nil
}

func (client *authenticatedFTPSession) openPassiveConnection(ctx context.Context) (net.Conn, error) {
	if err := client.session.writeCommand("EPSV"); err != nil {
		return nil, err
	}
	code, message, err := client.session.readReply()
	if err != nil {
		return nil, err
	}
	port := 0
	if code == 229 {
		port, err = parseEPSVPort(message)
	}
	if port == 0 {
		if err := client.session.writeCommand("PASV"); err != nil {
			return nil, err
		}
		code, message, err = client.session.readReply()
		if err != nil || code != 227 {
			return nil, errors.New("máy chủ không mở được kết nối passive")
		}
		port, err = parsePASVPort(message)
		if err != nil {
			return nil, err
		}
	}
	dialer := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 15 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(client.project.Host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("không mở được kênh dữ liệu passive: %w", err)
	}
	deadline := time.Now().Add(20 * time.Minute)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = connection.SetDeadline(deadline)
	return connection, nil
}

func (client *authenticatedFTPSession) secureDataConnection(ctx context.Context, connection net.Conn) (net.Conn, error) {
	if !client.secure {
		return connection, nil
	}
	if client.tlsConfig == nil {
		return nil, errors.New("thiếu cấu hình TLS cho kênh dữ liệu")
	}
	secure := tls.Client(connection, client.tlsConfig.Clone())
	if err := secure.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("kênh dữ liệu FTPS không an toàn: %w", err)
	}
	return secure, nil
}

func parseEPSVPort(message string) (int, error) {
	left, right := strings.IndexByte(message, '('), strings.LastIndexByte(message, ')')
	if left < 0 || right <= left+4 {
		return 0, errors.New("phản hồi EPSV không hợp lệ")
	}
	value := message[left+1 : right]
	delimiter := value[0]
	parts := strings.Split(value, string(delimiter))
	if len(parts) < 5 {
		return 0, errors.New("phản hồi EPSV không hợp lệ")
	}
	port, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("cổng EPSV không hợp lệ")
	}
	return port, nil
}

func parsePASVPort(message string) (int, error) {
	left, right := strings.IndexByte(message, '('), strings.IndexByte(message, ')')
	if left < 0 || right <= left {
		return 0, errors.New("phản hồi PASV không hợp lệ")
	}
	parts := strings.Split(message[left+1:right], ",")
	if len(parts) != 6 {
		return 0, errors.New("phản hồi PASV không hợp lệ")
	}
	high, errHigh := strconv.Atoi(strings.TrimSpace(parts[4]))
	low, errLow := strconv.Atoi(strings.TrimSpace(parts[5]))
	port := high*256 + low
	if errHigh != nil || errLow != nil || high < 0 || high > 255 || low < 0 || low > 255 || port < 1 {
		return 0, errors.New("cổng PASV không hợp lệ")
	}
	return port, nil
}

func downloadRemoteFiles(ctx context.Context, project Project, password string, files []remoteFile, totalBytes int64, partialPath string, emit func(BackupProgress)) ([]manifestFile, error) {
	manifest := make([]manifestFile, len(files))
	jobs := make(chan int)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var firstErr error
	var errorOnce sync.Once
	var filesDone atomic.Int64
	var bytesDone atomic.Int64
	var workers sync.WaitGroup
	workerCount := project.Workers
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > 8 {
		workerCount = 8
	}
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			client, err := openAuthenticatedFTPSession(ctx, project, password, nil)
			if err != nil {
				errorOnce.Do(func() { firstErr = err; cancel() })
				return
			}
			defer client.close()
			for index := range jobs {
				entry := files[index]
				item, err := downloadOne(ctx, client, project.RemotePath, entry, partialPath)
				if err != nil {
					errorOnce.Do(func() { firstErr = fmt.Errorf("không tải được %s: %w", entry.Path, err); cancel() })
					return
				}
				manifest[index] = item
				doneFiles := int(filesDone.Add(1))
				doneBytes := bytesDone.Add(entry.Size)
				percent := 20
				if totalBytes > 0 {
					percent += int(doneBytes * 52 / totalBytes)
				} else {
					percent += doneFiles * 52 / len(files)
				}
				emit(BackupProgress{Stage: "download", Message: fmt.Sprintf("Đã sao lưu %d/%d file", doneFiles, len(files)), Percent: percent, FilesDone: doneFiles, FilesTotal: len(files), BytesDone: doneBytes, BytesTotal: totalBytes})
			}
		}()
	}
	for index := range files {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func downloadOne(ctx context.Context, client *authenticatedFTPSession, root string, entry remoteFile, partialPath string) (manifestFile, error) {
	temporary, err := os.CreateTemp(partialPath, ".download-*.tmp")
	if err != nil {
		return manifestFile{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return manifestFile{}, err
	}
	hasher := sha256.New()
	written, err := client.retrieve(ctx, path.Join(root, entry.Path), io.MultiWriter(temporary, hasher), entry.Size)
	if err != nil {
		temporary.Close()
		return manifestFile{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return manifestFile{}, err
	}
	if err := temporary.Close(); err != nil {
		return manifestFile{}, err
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	relativeBlob := path.Join("blobs", hash[:2], hash+".ocblob")
	destination := filepath.Join(partialPath, filepath.FromSlash(relativeBlob))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return manifestFile{}, err
	}
	if _, err := os.Stat(destination); err == nil {
		return manifestFile{Path: entry.Path, Size: written, SHA256: hash, Blob: relativeBlob}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return manifestFile{}, err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		if _, statErr := os.Stat(destination); statErr != nil {
			return manifestFile{}, err
		}
	}
	return manifestFile{Path: entry.Path, Size: written, SHA256: hash, Blob: relativeBlob}, nil
}

func verifyBlobs(root string, files []manifestFile) error {
	seen := map[string]struct{}{}
	for _, entry := range files {
		if _, ok := seen[entry.SHA256]; ok {
			continue
		}
		seen[entry.SHA256] = struct{}{}
		blobPath := filepath.Join(root, filepath.FromSlash(entry.Blob))
		file, err := os.Open(blobPath)
		if err != nil {
			return err
		}
		hasher := sha256.New()
		written, copyErr := io.Copy(hasher, io.LimitReader(file, maxRemoteFileBytes+1))
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != entry.Size || hex.EncodeToString(hasher.Sum(nil)) != entry.SHA256 {
			return fmt.Errorf("checksum blob không khớp cho %s", entry.Path)
		}
	}
	return nil
}

func scanBackup(root, backupID string, files []manifestFile) scanReport {
	report := scanReport{
		Schema: 1, BackupID: backupID, CreatedAt: nowText(), Files: len(files),
		Disclaimer: "Quét tĩnh theo quy tắc; không thay thế điều tra mã độc chuyên sâu và không file nào được thực thi.",
		Items:      []scanFinding{},
	}
	for _, entry := range files {
		if len(report.Items) >= maxFindings || !scannablePath(entry.Path) {
			continue
		}
		blobPath := filepath.Join(root, filepath.FromSlash(entry.Blob))
		file, err := os.Open(blobPath)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(file, maxScanBytes+1))
		_ = file.Close()
		if int64(len(data)) > maxScanBytes {
			continue
		}
		items := scanContent(entry.Path, data)
		for _, item := range items {
			if len(report.Items) >= maxFindings {
				break
			}
			report.Items = append(report.Items, item)
			if item.Severity == "critical" {
				report.Critical++
			} else if item.Severity == "high" {
				report.High++
			}
		}
	}
	report.Findings = len(report.Items)
	return report
}

func scannablePath(value string) bool {
	lower := strings.ToLower(value)
	extension := strings.ToLower(path.Ext(lower))
	switch extension {
	case ".php", ".phtml", ".php3", ".php4", ".php5", ".php7", ".phar", ".inc", ".js", ".html", ".htm", ".svg", ".sql", ".txt":
		return true
	}
	return path.Base(lower) == ".htaccess" || strings.Contains("/"+strings.TrimPrefix(lower, "/"), "/wp-content/uploads/")
}

func scanContent(filePath string, data []byte) []scanFinding {
	lowerPath := strings.ToLower("/" + strings.TrimPrefix(filePath, "/"))
	extension := strings.ToLower(path.Ext(lowerPath))
	lower := bytes.ToLower(data)
	findings := []scanFinding{}
	add := func(severity, rule, message string) {
		findings = append(findings, scanFinding{Path: filePath, Severity: severity, Rule: rule, Message: message})
	}
	phpExtension := extension == ".php" || extension == ".phtml" || strings.HasPrefix(extension, ".php") || extension == ".phar" || extension == ".inc"
	if strings.Contains(lowerPath, "/wp-content/uploads/") && phpExtension {
		add("critical", "php_in_uploads", "File PHP nằm trong thư mục uploads của WordPress.")
	}
	if strings.Contains(lowerPath, "/wp-content/uploads/") && !phpExtension && bytes.Contains(lower, []byte("<?php")) {
		add("critical", "hidden_php_upload", "File uploads có nội dung PHP dù phần mở rộng không phải PHP.")
	}
	patterns := []struct{ needle, rule, message string }{
		{"eval(base64_decode", "encoded_eval", "Phát hiện eval(base64_decode(...)) thường gặp ở webshell."},
		{"gzinflate(base64_decode", "compressed_payload", "Phát hiện payload nén và mã hóa đáng ngờ."},
		{"base64_decode($_", "request_decode", "Dữ liệu request được giải mã trực tiếp."},
		{"assert($_", "request_assert", "Dữ liệu request được đưa vào assert."},
		{"shell_exec($_", "request_shell", "Dữ liệu request được đưa vào shell_exec."},
		{"passthru($_", "request_shell", "Dữ liệu request được đưa vào passthru."},
		{"system($_", "request_shell", "Dữ liệu request được đưa vào system."},
		{"exec($_", "request_shell", "Dữ liệu request được đưa vào exec."},
		{"str_rot13(base64_decode", "layered_obfuscation", "Phát hiện nhiều lớp làm rối mã nguồn."},
	}
	for _, pattern := range patterns {
		if bytes.Contains(lower, []byte(pattern.needle)) {
			severity := "high"
			if pattern.rule == "request_shell" || pattern.rule == "encoded_eval" {
				severity = "critical"
			}
			add(severity, pattern.rule, pattern.message)
		}
	}
	if hasLongBase64(data, 600) {
		add("high", "long_encoded_payload", "Có chuỗi mã hóa dài bất thường cần kiểm tra thủ công.")
	}
	return findings
}

func hasLongBase64(data []byte, minimum int) bool {
	run := 0
	for _, value := range data {
		if (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '+' || value == '/' || value == '=' {
			run++
			if run >= minimum {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

func writeJSON(destination string, value any) error {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".report-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func newBackupID() (string, error) {
	randomID, err := newID()
	if err != nil {
		return "", err
	}
	return "backup-" + time.Now().UTC().Format("20060102-150405") + "-" + randomID[:8], nil
}

func cleanBackupError(err error) string {
	if err == nil {
		return ""
	}
	value := cleanConnectionError(err)
	if errors.Is(err, context.Canceled) {
		return "Thao tác sao lưu đã dừng. Hosting không bị thay đổi."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Sao lưu vượt quá thời gian cho phép. Hosting không bị thay đổi."
	}
	return cleanText(value, 800)
}
