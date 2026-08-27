package state

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
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"oneclick-dev-server/internal/project"
)

const (
	maxStateBytes     = 5 * 1024 * 1024
	maxProjectHistory = 50
	maxDomains        = 24
)

type legacySnapshotV1 struct {
	Schema      int                  `json:"schema"`
	UpdatedAt   string               `json:"updatedAt"`
	StoragePath string               `json:"storagePath"`
	Cloudflare  CloudflareConnection `json:"cloudflare"`
	Projects    []ProjectRecord      `json:"projects"`
}

type Repository struct {
	mu   sync.Mutex
	path string
}

func NewDefault() (*Repository, error) {
	root := ""
	if runtime.GOOS == "windows" {
		root = strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	}
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("không xác định được thư mục dữ liệu người dùng: %w", err)
		}
	}
	return New(filepath.Join(root, "OneClickDevServer", "state.json"))
}

func New(path string) (*Repository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("đường dẫn state.json trống")
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
	changed := recoverInterrupted(&snapshot)
	if changed {
		if err := repository.saveLocked(&snapshot); err != nil {
			return Snapshot{}, err
		}
	}
	sort.SliceStable(snapshot.Projects, func(left, right int) bool {
		return snapshot.Projects[left].UpdatedAt > snapshot.Projects[right].UpdatedAt
	})
	snapshot.StoragePath = repository.path
	return snapshot, nil
}

func (repository *Repository) GetProject(path string) (ProjectRecord, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return ProjectRecord{}, false, err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return ProjectRecord{}, false, nil
	}
	return snapshot.Projects[index], true, nil
}

// RecordBackup appends a non-destructive data-management event without
// changing the deployment stage, runtime health, tunnel state or last error.
func (repository *Repository) RecordBackup(path string, success bool, message, detail string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	status := "error"
	if success {
		status = "success"
	}
	record.UpdatedAt = nowText()
	appendHistory(record, "backup", status, message, detail)
	return repository.saveLocked(&snapshot)
}

// RecordSourceEdit appends only non-secret audit metadata for an explicit
// source save. It never changes deployment/runtime/tunnel state or stores the
// edited content.
func (repository *Repository) RecordSourceEdit(path, relativePath string, success bool, message, detail string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	status := "error"
	if success {
		status = "success"
	}
	relativePath = filepath.ToSlash(cleanText(relativePath, 300))
	if relativePath != "" {
		message = fmt.Sprintf("%s · %s", cleanText(message, 220), relativePath)
	}
	record.UpdatedAt = nowText()
	appendHistory(record, "source_edit", status, message, detail)
	return repository.saveLocked(&snapshot)
}

// RemoveProjects forgets only the exact project paths supplied by the caller.
// It never removes source files, virtual machines or Cloudflare resources.
func (repository *Repository) RemoveProjects(paths []string) (int, error) {
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			wanted[canonicalPath(path)] = true
		}
	}
	if len(wanted) == 0 {
		return 0, errors.New("chưa chọn dự án cần xóa khỏi lịch sử")
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return 0, err
	}
	kept := make([]ProjectRecord, 0, len(snapshot.Projects))
	removed := 0
	for _, record := range snapshot.Projects {
		if wanted[canonicalPath(record.Path)] {
			removed++
			continue
		}
		kept = append(kept, record)
	}
	if removed == 0 {
		return 0, nil
	}
	snapshot.Projects = kept
	if err := repository.saveLocked(&snapshot); err != nil {
		return 0, err
	}
	return removed, nil
}

func (repository *Repository) SaveCloudflareConnection(connection CloudflareConnection) error {
	connection.Zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(connection.Zone), "."))
	connection.ZoneID = cleanText(connection.ZoneID, 64)
	connection.AccountID = cleanText(connection.AccountID, 64)
	connection.ZoneStatus = cleanText(connection.ZoneStatus, 40)
	if connection.Zone == "" || connection.ZoneID == "" || connection.AccountID == "" {
		return errors.New("thông tin kết nối Cloudflare chưa đầy đủ")
	}
	connection.NameServers = cleanStringList(connection.NameServers, 8, 255)
	connection.ConnectedAt = nowText()
	connection.CheckedAt = connection.ConnectedAt
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findDomain(snapshot.Domains, connection.Zone)
	if index < 0 {
		if len(snapshot.Domains) >= maxDomains {
			return fmt.Errorf("chỉ hỗ trợ tối đa %d domain", maxDomains)
		}
		snapshot.Domains = append(snapshot.Domains, connection)
	} else {
		if snapshot.Domains[index].ConnectedAt != "" {
			connection.ConnectedAt = snapshot.Domains[index].ConnectedAt
		}
		snapshot.Domains[index] = connection
	}
	sort.SliceStable(snapshot.Domains, func(left, right int) bool {
		return snapshot.Domains[left].Zone < snapshot.Domains[right].Zone
	})
	return repository.saveLocked(&snapshot)
}

// RemoveCloudflareConnection forgets one exact zone. It refuses to remove a
// connection while a project may still own DNS or tunnel resources in it.
func (repository *Repository) RemoveCloudflareConnection(zone string) error {
	zone = normalizeDomain(zone)
	if zone == "" {
		return errors.New("domain không hợp lệ")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findDomain(snapshot.Domains, zone)
	if index < 0 {
		return errors.New("không tìm thấy domain trong danh sách")
	}
	for _, record := range snapshot.Projects {
		if projectUsesDomain(record, zone) {
			return fmt.Errorf("domain %s đang được website %s sử dụng; hãy dừng truy cập trước", zone, record.Name)
		}
	}
	snapshot.Domains = append(snapshot.Domains[:index], snapshot.Domains[index+1:]...)
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) SaveConfiguration(input ProjectInput) (ProjectRecord, error) {
	validated, err := validateProjectInput(input)
	if err != nil {
		return ProjectRecord{}, err
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return ProjectRecord{}, err
	}
	now := nowText()
	index := findProjectByPath(snapshot.Projects, validated.Path)
	if index < 0 {
		record := ProjectRecord{
			ID:        projectID(validated.Path),
			Path:      validated.Path,
			Stage:     "configured",
			Status:    "pending",
			CreatedAt: now,
			History:   []HistoryEntry{},
		}
		snapshot.Projects = append(snapshot.Projects, record)
		index = len(snapshot.Projects) - 1
	}

	record := &snapshot.Projects[index]
	runtimeConfigurationChanged := record.Name != "" && (record.Name != validated.Name || record.Kind != validated.Kind ||
		record.DocumentRoot != validated.DocumentRoot || record.PHPVersion != validated.PHPVersion)
	tunnelConfigurationChanged := record.Name != "" && record.TunnelMode != validated.TunnelMode
	if (runtimeConfigurationChanged || tunnelConfigurationChanged) && (record.Stage == "environment_creating" || record.Stage == "copying" || record.Stage == "runtime_installing" || record.Stage == "database_importing" || record.Stage == "tunnel_starting" || record.Stage == "tunnel_stopping") {
		return ProjectRecord{}, errors.New("website đang được xử lý; hãy chờ hoàn tất trước khi sửa cấu hình")
	}
	if (runtimeConfigurationChanged || tunnelConfigurationChanged) && (record.Stage == "public" || record.Stage == "tunnel_stop_failed") {
		return ProjectRecord{}, errors.New("hãy dừng truy cập domain trước khi sửa cấu hình")
	}
	record.Name = validated.Name
	record.Kind = validated.Kind
	record.KindLabel = validated.KindLabel
	record.DocumentRoot = validated.DocumentRoot
	record.PHPVersion = validated.PHPVersion
	record.TunnelMode = validated.TunnelMode
	if runtimeConfigurationChanged && (record.Stage == "environment_ready" || record.Stage == "copy_failed" || record.Stage == "source_ready" || record.Stage == "runtime_failed" || record.Stage == "runtime_ready" || record.Stage == "database_failed" || record.Stage == "database_ready" || record.Stage == "tunnel_failed") {
		record.Stage = "environment_ready"
		record.Status = "ready"
		record.SnapshotID = ""
		record.SnapshotHash = ""
		record.SnapshotPath = ""
		record.SnapshotFiles = 0
		record.SnapshotBytes = 0
		record.RuntimeAdapter = ""
		record.RuntimeState = ""
		record.RuntimeHealth = ""
		record.RuntimeContainers = 0
		record.RuntimeSnapshotID = ""
		clearDatabase(record)
		clearTunnel(record)
		record.LastError = ""
	}
	record.UpdatedAt = now
	appendHistory(record, "configuration", "success", "Đã lưu cấu hình website", "")
	if err := repository.saveLocked(&snapshot); err != nil {
		return ProjectRecord{}, err
	}
	return *record, nil
}

func (repository *Repository) MarkEnvironmentStarted(path, vmName string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("website chưa được lưu cấu hình")
	}
	record := &snapshot.Projects[index]
	record.Stage = "environment_creating"
	record.Status = "running"
	record.VMName = cleanText(vmName, 180)
	record.LastError = ""
	record.UpdatedAt = nowText()
	appendHistory(record, "environment", "running", "Bắt đầu tạo môi trường", "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkEnvironmentFinished(path string, outcome EnvironmentOutcome) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	if value := cleanText(outcome.VMName, 180); value != "" {
		record.VMName = value
	}
	if value := cleanText(outcome.VMState, 80); value != "" {
		record.VMState = value
	}
	if value := cleanText(outcome.IP, 100); value != "" {
		record.IP = value
	}
	record.UpdatedAt = nowText()
	if outcome.Success {
		record.Stage = "environment_ready"
		record.Status = "ready"
		record.LastError = ""
		appendHistory(record, "environment", "success", cleanText(outcome.Message, 300), "")
	} else {
		record.Stage = "environment_failed"
		record.Status = "error"
		record.LastError = cleanText(outcome.Detail, 1200)
		appendHistory(record, "environment", "error", cleanText(outcome.Message, 300), record.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkCopyStarted(path string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	if record.Stage != "environment_ready" && record.Stage != "copy_failed" && record.Stage != "source_ready" {
		return errors.New("môi trường an toàn chưa sẵn sàng")
	}
	record.Stage = "copying"
	record.Status = "running"
	record.LastError = ""
	record.UpdatedAt = nowText()
	appendHistory(record, "copy", "running", "Bắt đầu sao chép website", "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkCopyFinished(path string, outcome CopyOutcome) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	record.UpdatedAt = nowText()
	if outcome.Success {
		record.Stage = "source_ready"
		record.Status = "ready"
		record.SnapshotID = cleanText(outcome.SnapshotID, 64)
		record.SnapshotHash = cleanText(outcome.Checksum, 64)
		record.SnapshotPath = cleanText(outcome.GuestPath, 300)
		record.SnapshotFiles = outcome.FileCount
		record.SnapshotBytes = outcome.TotalBytes
		record.LastError = ""
		appendHistory(record, "copy", "success", cleanText(outcome.Message, 300), "")
	} else {
		record.Stage = "copy_failed"
		record.Status = "error"
		record.LastError = cleanText(outcome.Detail, 1200)
		appendHistory(record, "copy", "error", cleanText(outcome.Message, 300), record.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkRuntimeStarted(path string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	if record.Stage != "source_ready" && record.Stage != "runtime_failed" && record.Stage != "runtime_ready" && record.Stage != "database_failed" && record.Stage != "database_ready" {
		return errors.New("website chưa có snapshot sẵn sàng")
	}
	if record.SnapshotID == "" || record.SnapshotHash == "" || record.SnapshotPath == "" {
		return errors.New("lịch sử snapshot chưa đầy đủ")
	}
	record.Stage = "runtime_installing"
	record.Status = "running"
	record.RuntimeState = "installing"
	record.RuntimeHealth = ""
	clearDatabase(record)
	record.LastError = ""
	record.UpdatedAt = nowText()
	appendHistory(record, "runtime", "running", "Bắt đầu cài môi trường chạy", "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkRuntimeFinished(path string, outcome RuntimeOutcome) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	record.UpdatedAt = nowText()
	if outcome.Success {
		record.Stage = "runtime_ready"
		record.Status = "ready"
		record.RuntimeAdapter = cleanText(outcome.Adapter, 80)
		record.RuntimeState = cleanText(outcome.State, 80)
		record.RuntimeHealth = cleanText(outcome.Health, 80)
		record.RuntimeContainers = outcome.Containers
		record.RuntimeSnapshotID = cleanText(outcome.SnapshotID, 64)
		record.LastError = ""
		appendHistory(record, "runtime", "success", cleanText(outcome.Message, 300), "")
	} else {
		record.Stage = "runtime_failed"
		record.Status = "error"
		record.RuntimeState = "failed"
		record.RuntimeHealth = ""
		record.LastError = cleanText(outcome.Detail, 1200)
		appendHistory(record, "runtime", "error", cleanText(outcome.Message, 300), record.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkDatabaseStarted(path string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	if record.Stage != "runtime_ready" && record.Stage != "database_failed" && record.Stage != "database_ready" {
		return errors.New("runtime chưa sẵn sàng để nhập database")
	}
	if record.RuntimeState != "running" || record.RuntimeHealth != "healthy" {
		return errors.New("runtime chưa vượt qua kiểm tra healthy")
	}
	record.Stage = "database_importing"
	record.Status = "running"
	record.DatabaseState = "importing"
	record.LastError = ""
	record.UpdatedAt = nowText()
	appendHistory(record, "database", "running", "Bắt đầu sao chép database", "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkDatabaseFinished(path string, outcome DatabaseOutcome) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	record.UpdatedAt = nowText()
	if outcome.Success {
		prefix := cleanText(outcome.TablePrefix, 64)
		if prefix == "" {
			return errors.New("không lưu trạng thái database thiếu tiền tố bảng")
		}
		record.Stage = "database_ready"
		record.Status = "ready"
		record.DatabaseState = "ready"
		record.DatabaseSource = cleanText(outcome.Source, 120)
		record.DatabaseTables = outcome.Tables
		record.DatabaseBytes = outcome.Bytes
		record.DatabasePrefix = prefix
		record.DatabaseImportedAt = nowText()
		record.LastError = ""
		appendHistory(record, "database", "success", cleanText(outcome.Message, 300), "")
	} else {
		record.Stage = "database_failed"
		record.Status = "error"
		record.DatabaseState = "failed"
		record.LastError = cleanText(outcome.Detail, 1200)
		appendHistory(record, "database", "error", cleanText(outcome.Message, 300), record.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkTunnelStarted(path, hostname, domainZone string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	if record.Stage != "database_ready" && record.Stage != "tunnel_failed" {
		return errors.New("database chưa sẵn sàng để public domain")
	}
	if record.RuntimeState != "running" || record.RuntimeHealth != "healthy" {
		return errors.New("runtime chưa vượt qua kiểm tra healthy")
	}
	if record.DatabaseState != "ready" || record.DatabasePrefix == "" {
		return errors.New("database chưa vượt qua kiểm tra an toàn")
	}
	record.Stage = "tunnel_starting"
	record.Status = "running"
	record.TunnelMode = "named"
	record.DomainZone = normalizeDomain(domainZone)
	record.Hostname = cleanText(strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), ".")), 255)
	if record.DomainZone == "" || (record.Hostname != record.DomainZone && !strings.HasSuffix(record.Hostname, "."+record.DomainZone)) {
		return errors.New("domain đã chọn không khớp địa chỉ website")
	}
	record.TunnelState = "starting"
	record.TunnelHealth = ""
	record.TunnelURL = ""
	record.LastError = ""
	record.UpdatedAt = nowText()
	appendHistory(record, "tunnel", "running", "Bắt đầu kết nối domain "+record.Hostname, "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkTunnelAllocated(path, tunnelID, dnsRecordID string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 || snapshot.Projects[index].Stage != "tunnel_starting" {
		return errors.New("website không ở bước tạo tunnel")
	}
	record := &snapshot.Projects[index]
	record.TunnelID = cleanText(tunnelID, 64)
	record.DNSRecordID = cleanText(dnsRecordID, 64)
	record.UpdatedAt = nowText()
	appendHistory(record, "tunnel", "running", "Đã tạo tunnel và DNS; đang kiểm tra HTTPS", "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkTunnelFinished(path string, outcome TunnelOutcome) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	record.UpdatedAt = nowText()
	if outcome.Success {
		record.Stage = "public"
		record.Status = "ready"
		record.DomainZone = normalizeDomain(outcome.DomainZone)
		record.Hostname = cleanText(outcome.Hostname, 255)
		record.TunnelID = cleanText(outcome.TunnelID, 64)
		record.DNSRecordID = cleanText(outcome.DNSRecordID, 64)
		record.TunnelURL = cleanText(outcome.URL, 400)
		record.TunnelState = cleanText(outcome.State, 80)
		record.TunnelHealth = cleanText(outcome.Health, 80)
		record.TunnelStartedAt = nowText()
		record.LastError = ""
		appendHistory(record, "tunnel", "success", cleanText(outcome.Message, 300), "")
	} else {
		record.Stage = "tunnel_failed"
		record.Status = "error"
		record.TunnelID = cleanText(outcome.TunnelID, 64)
		record.DNSRecordID = cleanText(outcome.DNSRecordID, 64)
		record.TunnelState = "failed"
		record.TunnelHealth = ""
		record.TunnelURL = ""
		record.LastError = cleanText(outcome.Detail, 1200)
		appendHistory(record, "tunnel", "error", cleanText(outcome.Message, 300), record.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkTunnelStopping(path string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	if record.Stage != "public" && record.Stage != "tunnel_stop_failed" {
		return errors.New("website không có domain đang cần dừng")
	}
	record.Stage = "tunnel_stopping"
	record.Status = "running"
	record.TunnelState = "stopping"
	record.LastError = ""
	record.UpdatedAt = nowText()
	appendHistory(record, "tunnel", "running", "Đang dừng truy cập domain", "")
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) MarkTunnelStopped(path string, outcome TunnelOutcome) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot, err := repository.loadLocked()
	if err != nil {
		return err
	}
	index := findProjectByPath(snapshot.Projects, path)
	if index < 0 {
		return errors.New("không tìm thấy website trong lịch sử")
	}
	record := &snapshot.Projects[index]
	record.UpdatedAt = nowText()
	if outcome.Success {
		record.Stage = "database_ready"
		record.Status = "ready"
		clearTunnel(record)
		record.TunnelState = "stopped"
		record.LastError = ""
		appendHistory(record, "tunnel", "success", cleanText(outcome.Message, 300), "")
	} else {
		record.Stage = "tunnel_stop_failed"
		record.Status = "error"
		record.TunnelState = "unknown"
		record.TunnelHealth = ""
		record.LastError = cleanText(outcome.Detail, 1200)
		appendHistory(record, "tunnel", "error", cleanText(outcome.Message, 300), record.LastError)
	}
	return repository.saveLocked(&snapshot)
}

func (repository *Repository) loadLocked() (Snapshot, error) {
	file, err := os.Open(repository.path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{Schema: SchemaVersion, Projects: []ProjectRecord{}}, nil
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("không mở được lịch sử JSON: %w", err)
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil {
		return Snapshot{}, statErr
	} else if info.Size() > maxStateBytes {
		return Snapshot{}, errors.New("state.json vượt quá giới hạn 5 MB")
	}

	data, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil {
		return Snapshot{}, err
	}
	if len(data) > maxStateBytes {
		return Snapshot{}, errors.New("state.json vượt quá giới hạn 5 MB")
	}
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Snapshot{}, fmt.Errorf("state.json không hợp lệ; file được giữ nguyên: %w", err)
	}
	var snapshot Snapshot
	if header.Schema == 1 {
		var legacy legacySnapshotV1
		if err := decodeStrict(data, &legacy); err != nil {
			return Snapshot{}, fmt.Errorf("state.json không hợp lệ; file được giữ nguyên: %w", err)
		}
		snapshot = Snapshot{Schema: SchemaVersion, UpdatedAt: legacy.UpdatedAt, StoragePath: legacy.StoragePath, Projects: legacy.Projects, Domains: []CloudflareConnection{}}
		if legacy.Cloudflare.Zone != "" {
			snapshot.Domains = append(snapshot.Domains, legacy.Cloudflare)
		}
	} else if header.Schema == 2 {
		if err := decodeStrict(data, &snapshot); err != nil {
			return Snapshot{}, fmt.Errorf("state.json không hợp lệ; file được giữ nguyên: %w", err)
		}
		snapshot.Schema = SchemaVersion
	} else if header.Schema == SchemaVersion {
		if err := decodeStrict(data, &snapshot); err != nil {
			return Snapshot{}, fmt.Errorf("state.json không hợp lệ; file được giữ nguyên: %w", err)
		}
	} else {
		return Snapshot{}, fmt.Errorf("schema lịch sử %d chưa được hỗ trợ", header.Schema)
	}
	if snapshot.Projects == nil {
		snapshot.Projects = []ProjectRecord{}
	}
	if snapshot.Domains == nil {
		snapshot.Domains = []CloudflareConnection{}
	}
	for index := range snapshot.Projects {
		if snapshot.Projects[index].DomainZone == "" {
			snapshot.Projects[index].DomainZone = inferProjectDomain(snapshot.Projects[index].Hostname, snapshot.Domains)
		}
	}
	return snapshot, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("state.json chứa nhiều hơn một tài liệu JSON")
		}
		return err
	}
	return nil
}

func (repository *Repository) saveLocked(snapshot *Snapshot) error {
	snapshot.Schema = SchemaVersion
	snapshot.UpdatedAt = nowText()
	snapshot.StoragePath = ""
	if err := os.MkdirAll(filepath.Dir(repository.path), 0o700); err != nil {
		return fmt.Errorf("không tạo được thư mục lịch sử: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(repository.path), "state-*.json.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	defer cleanup()
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
		return fmt.Errorf("không ghi được lịch sử JSON: %w", err)
	}
	return nil
}

func validateProjectInput(input ProjectInput) (ProjectRecord, error) {
	detected, err := project.Detect(input.Path)
	if err != nil {
		return ProjectRecord{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 120 {
		return ProjectRecord{}, errors.New("tên website phải có từ 1 đến 120 ký tự")
	}
	kindLabels := map[string]string{
		"laravel": "Laravel", "wordpress": "WordPress", "php-framework": "PHP Framework",
		"php": "PHP", "static": "Website tĩnh",
	}
	kind := strings.TrimSpace(input.Kind)
	kindLabel, ok := kindLabels[kind]
	if !ok {
		return ProjectRecord{}, errors.New("loại website không được hỗ trợ")
	}
	root := strings.TrimSpace(input.DocumentRoot)
	if root == "" || filepath.IsAbs(root) {
		return ProjectRecord{}, errors.New("thư mục chạy phải nằm trong website")
	}
	root = filepath.Clean(root)
	if root == ".." || strings.HasPrefix(root, ".."+string(filepath.Separator)) {
		return ProjectRecord{}, errors.New("thư mục chạy không được thoát ra ngoài website")
	}
	rootInfo, err := os.Stat(filepath.Join(detected.Path, root))
	if err != nil || !rootInfo.IsDir() {
		return ProjectRecord{}, errors.New("không tìm thấy thư mục chạy trong website")
	}
	phpVersion := strings.TrimSpace(input.PHPVersion)
	if kind == "static" {
		phpVersion = "none"
	} else if phpVersion != "auto" && phpVersion != "8.1" && phpVersion != "8.2" && phpVersion != "8.3" {
		return ProjectRecord{}, errors.New("phiên bản PHP không được hỗ trợ")
	}
	if input.TunnelMode != "quick" && input.TunnelMode != "named" {
		return ProjectRecord{}, errors.New("kiểu truy cập chưa được hỗ trợ")
	}
	return ProjectRecord{
		Path: detected.Path, Name: name, Kind: kind, KindLabel: kindLabel,
		DocumentRoot: root, PHPVersion: phpVersion, TunnelMode: input.TunnelMode,
	}, nil
}

func recoverInterrupted(snapshot *Snapshot) bool {
	changed := false
	for index := range snapshot.Projects {
		record := &snapshot.Projects[index]
		if record.Stage != "environment_creating" && record.Stage != "copying" && record.Stage != "runtime_installing" && record.Stage != "database_importing" && record.Stage != "tunnel_starting" && record.Stage != "tunnel_stopping" {
			continue
		}
		operation := "environment"
		message := "Lần tạo môi trường trước bị gián đoạn"
		if record.Stage == "copying" {
			record.Stage = "copy_failed"
			operation = "copy"
			message = "Lần sao chép trước bị gián đoạn"
		} else if record.Stage == "runtime_installing" {
			record.Stage = "runtime_failed"
			operation = "runtime"
			message = "Lần cài môi trường chạy trước bị gián đoạn"
		} else if record.Stage == "database_importing" {
			record.Stage = "database_failed"
			record.DatabaseState = "failed"
			operation = "database"
			message = "Lần sao chép database trước bị gián đoạn"
		} else if record.Stage == "tunnel_starting" {
			record.Stage = "tunnel_failed"
			record.TunnelState = "failed"
			record.TunnelURL = ""
			operation = "tunnel"
			message = "Lần kết nối domain trước bị gián đoạn"
		} else if record.Stage == "tunnel_stopping" {
			record.Stage = "tunnel_stop_failed"
			record.TunnelState = "unknown"
			operation = "tunnel"
			message = "Lần dừng domain trước bị gián đoạn"
		} else {
			record.Stage = "environment_failed"
		}
		record.Status = "error"
		record.LastError = "Ứng dụng đã đóng khi đang xử lý. Có thể bấm Tiếp tục để thử lại."
		record.UpdatedAt = nowText()
		appendHistory(record, operation, "interrupted", message, record.LastError)
		changed = true
	}
	return changed
}

func findProjectByPath(projects []ProjectRecord, path string) int {
	wanted := canonicalPath(path)
	for index := range projects {
		if canonicalPath(projects[index].Path) == wanted {
			return index
		}
	}
	return -1
}

func projectID(path string) string {
	sum := sha256.Sum256([]byte(canonicalPath(path)))
	return hex.EncodeToString(sum[:8])
}

func canonicalPath(path string) string {
	cleaned, err := filepath.Abs(path)
	if err != nil {
		cleaned = filepath.Clean(path)
	}
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	return filepath.Clean(cleaned)
}

func appendHistory(record *ProjectRecord, action, status, message, detail string) {
	now := nowText()
	record.History = append(record.History, HistoryEntry{
		ID: fmt.Sprintf("%d", time.Now().UnixNano()), Action: action, Status: status,
		Message: cleanText(message, 300), Detail: cleanText(detail, 1200), CreatedAt: now,
	})
	if len(record.History) > maxProjectHistory {
		record.History = append([]HistoryEntry(nil), record.History[len(record.History)-maxProjectHistory:]...)
	}
}

func cleanText(value string, limit int) string {
	var builder strings.Builder
	for _, char := range strings.TrimSpace(value) {
		if char == unicode.ReplacementChar || (unicode.IsControl(char) && char != '\n' && char != '\t') {
			continue
		}
		builder.WriteRune(char)
		if builder.Len() >= limit {
			break
		}
	}
	return strings.TrimSpace(builder.String())
}

func cleanStringList(values []string, maxItems, maxLength int) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSuffix(cleanText(value, maxLength), "."))
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
			if len(result) >= maxItems {
				break
			}
		}
	}
	return result
}

func clearTunnel(record *ProjectRecord) {
	record.DomainZone = ""
	record.Hostname = ""
	record.TunnelID = ""
	record.DNSRecordID = ""
	record.TunnelState = ""
	record.TunnelHealth = ""
	record.TunnelURL = ""
	record.TunnelStartedAt = ""
}

func clearDatabase(record *ProjectRecord) {
	record.DatabaseState = ""
	record.DatabaseSource = ""
	record.DatabaseTables = 0
	record.DatabaseBytes = 0
	record.DatabasePrefix = ""
	record.DatabaseImportedAt = ""
}

func normalizeDomain(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

func findDomain(domains []CloudflareConnection, zone string) int {
	zone = normalizeDomain(zone)
	for index := range domains {
		if normalizeDomain(domains[index].Zone) == zone {
			return index
		}
	}
	return -1
}

func inferProjectDomain(hostname string, domains []CloudflareConnection) string {
	hostname = normalizeDomain(hostname)
	best := ""
	for _, connection := range domains {
		zone := normalizeDomain(connection.Zone)
		if zone != "" && (hostname == zone || strings.HasSuffix(hostname, "."+zone)) && len(zone) > len(best) {
			best = zone
		}
	}
	return best
}

func projectUsesDomain(record ProjectRecord, zone string) bool {
	zone = normalizeDomain(zone)
	if zone == "" {
		return false
	}
	belongs := normalizeDomain(record.DomainZone) == zone
	if !belongs {
		hostname := normalizeDomain(record.Hostname)
		belongs = hostname == zone || strings.HasSuffix(hostname, "."+zone)
	}
	return belongs && (record.TunnelID != "" || record.DNSRecordID != "" || record.Stage == "public" || record.Stage == "tunnel_starting" || record.Stage == "tunnel_stopping" || record.Stage == "tunnel_stop_failed")
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339)
}
