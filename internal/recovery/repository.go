package recovery

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"oneclick-dev-server/internal/datastore"

	"golang.org/x/net/idna"
)

const (
	maxStateBytes = 2 * 1024 * 1024
	maxProjects   = 64
	maxHistory    = 50
)

type Repository struct {
	mu   sync.Mutex
	path string
}

func NewDefault() (*Repository, error) {
	root, err := datastore.ApplicationRoot()
	if err != nil {
		return nil, fmt.Errorf("không xác định được thư mục OneClick: %w", err)
	}
	return New(filepath.Join(root, "data", "recovery", "recovery.json"))
}

func New(path string) (*Repository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("đường dẫn recovery.json trống")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &Repository{path: filepath.Clean(absolute)}, nil
}

func (repository *Repository) List() (Snapshot, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return Snapshot{}, err
	}
	recovered := false
	for index := range snapshot.Projects {
		if snapshot.Projects[index].Status == "backup_running" {
			project := &snapshot.Projects[index]
			project.Status = "backup_failed"
			project.LastError = "Lần sao lưu trước bị gián đoạn; hosting không bị thay đổi."
			project.UpdatedAt = nowText()
			appendHistory(project, "backup", "error", "Sao lưu trước bị gián đoạn", project.LastError)
			recovered = true
		}
	}
	if recovered {
		if err := repository.saveLocked(&snapshot); err != nil {
			return Snapshot{}, err
		}
	}
	sort.SliceStable(snapshot.Projects, func(left, right int) bool {
		return snapshot.Projects[left].UpdatedAt > snapshot.Projects[right].UpdatedAt
	})
	snapshot.StoragePath = repository.path
	for index := range snapshot.Projects {
		snapshot.Projects[index].DataPath = repository.projectDataPath(snapshot.Projects[index].ID)
	}
	return snapshot, nil
}

func (repository *Repository) Get(id string) (Project, bool, error) {
	id = normalizeID(id)
	if id == "" {
		return Project{}, false, errors.New("mã website khôi phục không hợp lệ")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return Project{}, false, err
	}
	index := findProject(snapshot.Projects, id)
	if index < 0 {
		return Project{}, false, nil
	}
	project := snapshot.Projects[index]
	project.DataPath = repository.projectDataPath(project.ID)
	return project, true, nil
}

func (repository *Repository) Save(input ProjectInput) (Project, error) {
	normalized, err := normalizeInput(input)
	if err != nil {
		return Project{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return Project{}, err
	}
	index := -1
	if normalized.ID != "" {
		index = findProject(snapshot.Projects, normalized.ID)
		if index < 0 {
			return Project{}, errors.New("không tìm thấy website khôi phục cần cập nhật")
		}
	}
	now := nowText()
	if index < 0 {
		if len(snapshot.Projects) >= maxProjects {
			return Project{}, fmt.Errorf("chỉ quản lý tối đa %d website khôi phục", maxProjects)
		}
		normalized.ID, err = newID()
		if err != nil {
			return Project{}, err
		}
		normalized.CreatedAt = now
		normalized.Status = "configured"
		normalized.History = []HistoryEntry{}
		appendHistory(&normalized, "configure", "success", "Đã lưu cấu hình kết nối", "")
		snapshot.Projects = append(snapshot.Projects, normalized)
		index = len(snapshot.Projects) - 1
	} else {
		current := snapshot.Projects[index]
		normalized.CreatedAt = current.CreatedAt
		normalized.Status = "configured"
		normalized.History = current.History
		normalized.LastBackupID = current.LastBackupID
		normalized.BackupFiles = current.BackupFiles
		normalized.BackupBytes = current.BackupBytes
		normalized.FindingCount = current.FindingCount
		normalized.CriticalCount = current.CriticalCount
		normalized.BackupCreatedAt = current.BackupCreatedAt
		appendHistory(&normalized, "configure", "success", "Đã cập nhật cấu hình kết nối", "")
		snapshot.Projects[index] = normalized
	}
	snapshot.Projects[index].UpdatedAt = now
	if err := repository.saveLocked(&snapshot); err != nil {
		return Project{}, err
	}
	result := snapshot.Projects[index]
	result.DataPath = repository.projectDataPath(result.ID)
	return result, nil
}

func (repository *Repository) RecordConnection(id string, result ConnectionResult) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("mã website khôi phục không hợp lệ")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProject(snapshot.Projects, id)
	if index < 0 {
		return errors.New("không tìm thấy website khôi phục")
	}
	project := &snapshot.Projects[index]
	project.UpdatedAt = nowText()
	project.LastCheckedAt = result.CheckedAt
	if result.Success {
		project.Status = "connection_ready"
		project.LastError = ""
		appendHistory(project, "connection_test", "success", cleanText(result.Message, 240), "")
	} else {
		project.Status = "connection_failed"
		project.LastError = cleanText(result.Detail, 800)
		appendHistory(project, "connection_test", "error", cleanText(result.Message, 240), project.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) RecordBackupStarted(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("mã website khôi phục không hợp lệ")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProject(snapshot.Projects, id)
	if index < 0 {
		return errors.New("không tìm thấy website khôi phục")
	}
	project := &snapshot.Projects[index]
	project.Status = "backup_running"
	project.LastError = ""
	project.UpdatedAt = nowText()
	appendHistory(project, "backup", "running", "Bắt đầu sao lưu và quét", "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) RecordBackup(id string, result BackupResult) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("mã website khôi phục không hợp lệ")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProject(snapshot.Projects, id)
	if index < 0 {
		return errors.New("không tìm thấy website khôi phục")
	}
	project := &snapshot.Projects[index]
	project.UpdatedAt = nowText()
	if result.Success {
		project.Status = "backup_ready"
		project.LastError = ""
		project.LastBackupID = result.BackupID
		project.BackupFiles = result.Files
		project.BackupBytes = result.Bytes
		project.FindingCount = result.Findings
		project.CriticalCount = result.Critical
		project.BackupCreatedAt = result.CreatedAt
		appendHistory(project, "backup", "success", cleanText(result.Message, 240), "")
	} else {
		project.Status = "backup_failed"
		project.LastError = cleanText(result.Detail, 800)
		appendHistory(project, "backup", "error", cleanText(result.Message, 240), project.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) Delete(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("mã website khôi phục không hợp lệ")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProject(snapshot.Projects, id)
	if index < 0 {
		return errors.New("không tìm thấy website khôi phục")
	}
	snapshot.Projects = append(snapshot.Projects[:index], snapshot.Projects[index+1:]...)
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) EnsureProjectDirectory(id string) (string, error) {
	project, ok, err := repository.Get(id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("không tìm thấy website khôi phục")
	}
	path := repository.projectDataPath(project.ID)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("không tạo được thư mục dữ liệu khôi phục: %w", err)
	}
	return path, nil
}

func (repository *Repository) BackupPlan(id string) (BackupPlan, error) {
	project, ok, err := repository.Get(id)
	if err != nil {
		return BackupPlan{}, err
	}
	if !ok {
		return BackupPlan{}, errors.New("không tìm thấy website khôi phục")
	}
	return BackupPlan{
		ProjectID: project.ID, Name: project.Name, Host: project.Host,
		Protocol: project.Protocol, RemotePath: project.RemotePath,
		Destination: filepath.Join(repository.projectDataPath(project.ID), "backups"),
		Workers:     project.Workers, ReadOnly: true,
		StorageMode: "Blob trung tính + SHA-256; không giữ đuôi file có thể chạy",
	}, nil
}

func (repository *Repository) projectDataPath(id string) string {
	return filepath.Join(filepath.Dir(repository.path), "projects", normalizeID(id))
}

func (repository *Repository) loadLocked() (Snapshot, error) {
	file, err := os.Open(repository.path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{Schema: SchemaVersion, Projects: []Project{}}, nil
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("không mở được recovery.json: %w", err)
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil {
		return Snapshot{}, statErr
	} else if info.Size() > maxStateBytes {
		return Snapshot{}, errors.New("recovery.json vượt quá giới hạn 2 MB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("recovery.json không hợp lệ; file được giữ nguyên: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Snapshot{}, errors.New("recovery.json chứa nhiều hơn một tài liệu JSON")
		}
		return Snapshot{}, err
	}
	if snapshot.Schema != SchemaVersion {
		return Snapshot{}, fmt.Errorf("schema khôi phục %d chưa được hỗ trợ", snapshot.Schema)
	}
	if snapshot.Projects == nil {
		snapshot.Projects = []Project{}
	}
	if len(snapshot.Projects) > maxProjects {
		return Snapshot{}, errors.New("recovery.json có quá nhiều website")
	}
	for index := range snapshot.Projects {
		snapshot.Projects[index].PasswordStored = false
		snapshot.Projects[index].DataPath = ""
	}
	return snapshot, nil
}

func (repository *Repository) saveLocked(snapshot *Snapshot) error {
	snapshot.Schema = SchemaVersion
	snapshot.UpdatedAt = nowText()
	snapshot.StoragePath = ""
	for index := range snapshot.Projects {
		snapshot.Projects[index].PasswordStored = false
		snapshot.Projects[index].DataPath = ""
	}
	if err := os.MkdirAll(filepath.Dir(repository.path), 0o700); err != nil {
		return fmt.Errorf("không tạo được thư mục dữ liệu khôi phục: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(repository.path), "recovery-*.json.tmp")
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
	if err := encoder.Encode(snapshot); err != nil {
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
	if err := replaceFile(temporaryPath, repository.path); err != nil {
		return fmt.Errorf("không ghi được recovery.json: %w", err)
	}
	return nil
}

func normalizeInput(input ProjectInput) (Project, error) {
	normalizedID := normalizeID(input.ID)
	if strings.TrimSpace(input.ID) != "" && normalizedID == "" {
		return Project{}, errors.New("mã website khôi phục không hợp lệ")
	}
	name, err := normalizedText(input.Name, 100, "tên website")
	if err != nil {
		return Project{}, err
	}
	host, err := normalizeHost(input.Host)
	if err != nil {
		return Project{}, err
	}
	username, err := normalizedText(input.Username, 256, "tài khoản FTP")
	if err != nil {
		return Project{}, err
	}
	protocol := strings.ToLower(strings.TrimSpace(input.Protocol))
	if protocol == "" {
		protocol = "ftps"
	}
	if protocol != "ftps" && protocol != "ftp" {
		return Project{}, errors.New("chỉ hỗ trợ FTPS hoặc FTP")
	}
	if protocol == "ftp" && !input.AllowInsecure {
		return Project{}, errors.New("FTP không mã hóa; cần xác nhận rủi ro trước khi lưu")
	}
	port := input.Port
	if port == 0 {
		port = 21
	}
	if port < 1 || port > 65535 {
		return Project{}, errors.New("cổng FTP phải nằm trong khoảng 1–65535")
	}
	remotePathInput := strings.TrimSpace(input.RemotePath)
	if remotePathInput == "" {
		remotePathInput = DefaultRemotePath(host)
	}
	remotePath, err := normalizeRemotePath(remotePathInput)
	if err != nil {
		return Project{}, err
	}
	siteURL, err := normalizeSiteURL(input.SiteURL)
	if err != nil {
		return Project{}, err
	}
	workers := input.Workers
	if workers == 0 {
		workers = 4
	}
	if workers < 1 || workers > 8 {
		return Project{}, errors.New("số luồng tải phải nằm trong khoảng 1–8")
	}
	return Project{
		ID: normalizedID, Name: name, Host: host, Username: username,
		Protocol: protocol, Port: port, RemotePath: remotePath, SiteURL: siteURL,
		Workers: workers, Passive: true,
	}, nil
}

// DefaultRemotePath matches the common DirectAdmin layout used by the
// configured hostname. The backend still validates the final absolute path.
func DefaultRemotePath(host string) string {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(host, ".")))
	if strings.HasPrefix(host, "ftp.") {
		host = strings.TrimPrefix(host, "ftp.")
	}
	if normalized, err := normalizeHost(host); err == nil && net.ParseIP(normalized) == nil {
		return "/domains/" + normalized + "/public_html"
	}
	return "/public_html"
}

func normalizeHost(value string) (string, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "."))
	if value == "" || strings.ContainsAny(value, "/\\@") {
		return "", errors.New("máy chủ FTP không hợp lệ")
	}
	if parsed := net.ParseIP(strings.Trim(value, "[]")); parsed != nil {
		return parsed.String(), nil
	}
	if strings.Contains(value, ":") {
		return "", errors.New("không nhập cổng trong máy chủ FTP; dùng ô Cổng")
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || len(ascii) > 253 {
		return "", errors.New("máy chủ FTP không hợp lệ")
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("máy chủ FTP không hợp lệ")
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '-' {
				return "", errors.New("máy chủ FTP không hợp lệ")
			}
		}
	}
	return strings.ToLower(ascii), nil
}

func normalizeRemotePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "/"
	}
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') {
		return "", errors.New("thư mục website trên máy chủ phải là đường dẫn tuyệt đối, ví dụ /public_html")
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return "", errors.New("thư mục website chứa ký tự không hợp lệ")
		}
	}
	parts := strings.Split(value, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", errors.New("thư mục website không được chứa ..")
		}
		if len([]rune(part)) > 255 {
			return "", errors.New("một phần đường dẫn website quá dài")
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "/", nil
	}
	result := "/" + strings.Join(clean, "/")
	if len(result) > 1024 {
		return "", errors.New("đường dẫn website quá dài")
	}
	return result, nil
}

func normalizeSiteURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := urlpkg.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("địa chỉ website phải là HTTPS, ví dụ https://example.com")
	}
	host, err := normalizeHost(parsed.Hostname())
	if err != nil {
		return "", errors.New("địa chỉ website không hợp lệ")
	}
	if parsed.Port() != "" {
		if _, err := strconv.ParseUint(parsed.Port(), 10, 16); err != nil {
			return "", errors.New("cổng website không hợp lệ")
		}
		host = net.JoinHostPort(host, parsed.Port())
	}
	parsed.Host = host
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String(), nil
}

func normalizedText(value string, max int, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > max {
		return "", fmt.Errorf("%s phải có từ 1 đến %d ký tự", label, max)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return "", fmt.Errorf("%s chứa ký tự không hợp lệ", label)
		}
	}
	return value, nil
}

func normalizeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 24 {
		return ""
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'f') && !(char >= '0' && char <= '9') {
			return ""
		}
	}
	return value
}

func newID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("không tạo được mã website: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func findProject(projects []Project, id string) int {
	for index := range projects {
		if projects[index].ID == id {
			return index
		}
	}
	return -1
}

func appendHistory(project *Project, action, status, message, detail string) {
	entryID, err := newID()
	if err != nil {
		entryID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	project.History = append([]HistoryEntry{{
		ID: entryID, Action: action, Status: status, Message: cleanText(message, 240),
		Detail: cleanText(detail, 800), CreatedAt: nowText(),
	}}, project.History...)
	if len(project.History) > maxHistory {
		project.History = project.History[:maxHistory]
	}
}

func cleanText(value string, max int) string {
	value = strings.TrimSpace(strings.Map(func(char rune) rune {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return -1
		}
		return char
	}, value))
	characters := []rune(value)
	if len(characters) > max {
		return string(characters[:max]) + "…"
	}
	return value
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339)
}
