package main

import (
	"context"
	"errors"
	"fmt"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"oneclick-dev-server/internal/backup"
	"oneclick-dev-server/internal/cloudflare"
	"oneclick-dev-server/internal/database"
	"oneclick-dev-server/internal/datastore"
	"oneclick-dev-server/internal/hostopen"
	"oneclick-dev-server/internal/installer"
	"oneclick-dev-server/internal/project"
	"oneclick-dev-server/internal/readiness"
	"oneclick-dev-server/internal/recovery"
	"oneclick-dev-server/internal/recoverytool"
	runtimeenv "oneclick-dev-server/internal/runtime"
	"oneclick-dev-server/internal/secrets"
	"oneclick-dev-server/internal/sourceeditor"
	appstate "oneclick-dev-server/internal/state"
	"oneclick-dev-server/internal/tunnel"
	"oneclick-dev-server/internal/vm"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx              context.Context
	state            *appstate.Repository
	stateErr         error
	recovery         *recovery.Repository
	recoveryErr      error
	recoveryAction   sync.Mutex
	recoveryTool     *recoverytool.Manager
	recoveryToolErr  error
	sourceBackupRoot string
}

// NewApp creates a new App application struct
func NewApp() *App {
	repository, err := appstate.NewDefault()
	recoveryRepository, recoveryErr := recovery.NewDefault()
	recoveryToolManager, recoveryToolErr := recoverytool.NewDefault()
	return &App{
		state: repository, stateErr: err, recovery: recoveryRepository, recoveryErr: recoveryErr,
		recoveryTool: recoveryToolManager, recoveryToolErr: recoveryToolErr,
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	if a.recoveryTool != nil {
		a.recoveryTool.Close(ctx)
	}
}

// RunReadinessChecks inspects the host without changing system state.
func (a *App) RunReadinessChecks() readiness.Report {
	return readiness.Run(a.ctx)
}

// GetDependencyPlan returns the fixed operation shown to the employee before
// the app asks for permission to change the host.
func (a *App) GetDependencyPlan(action string) installer.Plan {
	return installer.GetPlan(action)
}

// RunDependencyAction executes an allowlisted installer and streams progress
// to the UI. It never accepts a shell command or download URL from JavaScript.
func (a *App) RunDependencyAction(action string) installer.Result {
	return installer.Run(a.ctx, action, func(progress installer.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "installer:progress", progress)
		}
	})
}

// GetDataStorageStatus reports the effective Multipass storage directory. It
// is read-only and locks configuration as soon as a project or VM exists.
func (a *App) GetDataStorageStatus() datastore.Status {
	projectCount, err := a.projectCount()
	status := datastore.Inspect(a.ctxOrBackground(), projectCount)
	if err != nil {
		status.CanConfigure = false
		status.Message = "Không đọc được lịch sử dự án"
		status.Detail = err.Error()
	}
	return status
}

// SelectDataStorageDirectory opens a native picker at the application folder.
// The selected directory is not changed until the user confirms separately.
func (a *App) SelectDataStorageDirectory() (string, error) {
	if a.ctx == nil {
		return "", errors.New("application is not ready")
	}
	root, err := datastore.ApplicationRoot()
	if err != nil {
		return "", err
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:                "Chọn thư mục trống dành cho dữ liệu máy ảo",
		DefaultDirectory:     root,
		CanCreateDirectories: true,
	})
}

// ConfigureDataStorage applies the official MULTIPASS_STORAGE mechanism using
// a fixed elevated helper. It is allowed only before the first project/VM.
func (a *App) ConfigureDataStorage(path string) datastore.Result {
	projectCount, err := a.projectCount()
	if err != nil {
		return datastore.Result{Message: "Không đọc được lịch sử dự án", Detail: err.Error()}
	}
	return datastore.Configure(a.ctx, path, projectCount, func(progress datastore.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "storage:progress", progress)
		}
	})
}

// GetRecoveryToolStatus reports the embedded wp-clean-rebuild bundle/runtime.
// It never uses code from a user-selected directory.
func (a *App) GetRecoveryToolStatus() recoverytool.Status {
	if a.recoveryToolErr != nil {
		return recoverytool.Status{State: "error", Message: "Khôi phục WP chưa sẵn sàng", Detail: a.recoveryToolErr.Error()}
	}
	if a.recoveryTool == nil {
		return recoverytool.Status{State: "error", Message: "Khôi phục WP chưa sẵn sàng", Detail: "embedded manager is unavailable"}
	}
	return a.recoveryTool.Inspect()
}

// PrepareRecoveryTool installs the pinned Python dependencies into OneClick's
// own data directory after the employee presses the explicit setup button.
func (a *App) PrepareRecoveryTool() recoverytool.Status {
	if a.recoveryToolErr != nil || a.recoveryTool == nil {
		return a.GetRecoveryToolStatus()
	}
	return a.recoveryTool.Prepare(a.ctxOrBackground(), func(progress recoverytool.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "recovery-tool:progress", progress)
		}
	})
}

// StartRecoveryTool launches only the source embedded in this OneClick build,
// hides the child console and returns a loopback URL suitable for the WebView.
func (a *App) StartRecoveryTool() recoverytool.Status {
	if a.recoveryToolErr != nil || a.recoveryTool == nil {
		return a.GetRecoveryToolStatus()
	}
	return a.recoveryTool.Start(a.ctxOrBackground())
}

// SelectLegacyRecoveryDirectory only chooses the old data root. The source is
// scanned and shown as a plan before any file is copied.
func (a *App) SelectLegacyRecoveryDirectory() (string, error) {
	if a.ctx == nil {
		return "", errors.New("application is not ready")
	}
	root, err := datastore.ApplicationRoot()
	if err != nil {
		return "", err
	}
	legacy := `D:\DuyAnhWeb\wp-clean-rebuild\wp-clean-rebuild`
	if info, statErr := os.Stat(legacy); statErr != nil || !info.IsDir() {
		legacy = root
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Chọn thư mục WP Clean Rebuild cũ", DefaultDirectory: legacy,
	})
}

func (a *App) GetLegacyRecoveryImportPlan(path string) recoverytool.ImportPlan {
	if a.recoveryToolErr != nil || a.recoveryTool == nil {
		return recoverytool.ImportPlan{SourcePath: path, Message: "Khôi phục WP chưa sẵn sàng", Detail: appErrorText(a.recoveryToolErr)}
	}
	return a.recoveryTool.LegacyImportPlan(path)
}

func (a *App) ImportLegacyRecoveryData(path string) recoverytool.ImportResult {
	if a.recoveryToolErr != nil || a.recoveryTool == nil {
		return recoverytool.ImportResult{Message: "Khôi phục WP chưa sẵn sàng", Detail: appErrorText(a.recoveryToolErr)}
	}
	return a.recoveryTool.ImportLegacy(a.ctxOrBackground(), path, func(progress recoverytool.ImportProgress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "recovery-tool:import-progress", progress)
		}
	})
}

// SelectProjectDirectory opens the native folder picker. It never mounts or
// copies the selected directory; deployment will create a separate snapshot.
func (a *App) SelectProjectDirectory() (string, error) {
	if a.ctx == nil {
		return "", errors.New("application is not ready")
	}

	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Chọn thư mục website",
	})
}

// DetectProjectDirectory reads only well-known project metadata. It never
// executes or modifies code from the selected website.
func (a *App) DetectProjectDirectory(path string) (project.Info, error) {
	return project.Detect(path)
}

// DiscoverProjectDirectories checks only immediate children of conventional
// local web roots. It does not recurse, execute code or persist a project.
func (a *App) DiscoverProjectDirectories() ([]project.Info, error) {
	return project.Discover()
}

// GetProjects loads the durable, local JSON project history.
func (a *App) GetProjects() (appstate.Snapshot, error) {
	if a.stateErr != nil {
		return appstate.Snapshot{}, a.stateErr
	}
	if a.state == nil {
		return appstate.Snapshot{}, errors.New("project history is not ready")
	}
	return a.state.List()
}

// SaveProjectConfiguration validates and atomically persists one project.
func (a *App) SaveProjectConfiguration(input appstate.ProjectInput) (appstate.ProjectRecord, error) {
	if a.stateErr != nil {
		return appstate.ProjectRecord{}, a.stateErr
	}
	if a.state == nil {
		return appstate.ProjectRecord{}, errors.New("project history is not ready")
	}
	return a.state.SaveConfiguration(input)
}

// GetRecoveryProjects loads the separate WordPress recovery registry. Remote
// passwords are represented only by a boolean and never cross the Wails bridge.
func (a *App) GetRecoveryProjects() (recovery.Snapshot, error) {
	if a.recoveryErr != nil {
		return recovery.Snapshot{}, a.recoveryErr
	}
	if a.recovery == nil {
		return recovery.Snapshot{}, errors.New("WordPress recovery is not ready")
	}
	snapshot, err := a.recovery.List()
	if err != nil {
		return recovery.Snapshot{}, err
	}
	for index := range snapshot.Projects {
		snapshot.Projects[index].PasswordStored = secrets.HasRecoveryPassword(snapshot.Projects[index].ID)
	}
	return snapshot, nil
}

// SaveRecoveryProject validates a fixed FTPS/FTP profile and stores its secret
// in the native OS vault. recovery.json contains connection metadata only.
func (a *App) SaveRecoveryProject(input recovery.ProjectInput) (recovery.Project, error) {
	if a.recoveryErr != nil {
		return recovery.Project{}, a.recoveryErr
	}
	if a.recovery == nil {
		return recovery.Project{}, errors.New("WordPress recovery is not ready")
	}
	if !a.recoveryAction.TryLock() {
		return recovery.Project{}, errors.New("một thao tác khôi phục khác đang chạy")
	}
	defer a.recoveryAction.Unlock()

	password := input.Password
	input.Password = ""
	if strings.TrimSpace(input.ID) == "" {
		if password == "" {
			return recovery.Project{}, errors.New("nhập mật khẩu FTP/FTPS cho website mới")
		}
		projectRecord, err := a.recovery.Save(input)
		if err != nil {
			return recovery.Project{}, err
		}
		if err := secrets.SaveRecoveryPassword(projectRecord.ID, password); err != nil {
			_ = a.recovery.Delete(projectRecord.ID)
			return recovery.Project{}, fmt.Errorf("cấu hình đã lưu nhưng mật khẩu chưa được lưu an toàn: %w", err)
		}
		projectRecord.PasswordStored = true
		return projectRecord, nil
	}

	var previousPassword string
	var previousPasswordExists bool
	if password != "" {
		previousPassword, _ = secrets.GetRecoveryPassword(input.ID)
		previousPasswordExists = previousPassword != ""
		if err := secrets.SaveRecoveryPassword(input.ID, password); err != nil {
			return recovery.Project{}, err
		}
	}
	projectRecord, err := a.recovery.Save(input)
	if err != nil {
		if password != "" {
			if previousPasswordExists {
				_ = secrets.SaveRecoveryPassword(input.ID, previousPassword)
			} else {
				_ = secrets.DeleteRecoveryPassword(input.ID)
			}
		}
		return recovery.Project{}, err
	}
	projectRecord.PasswordStored = secrets.HasRecoveryPassword(projectRecord.ID)
	if !projectRecord.PasswordStored {
		return recovery.Project{}, errors.New("chưa có mật khẩu FTP/FTPS trong kho bí mật")
	}
	return projectRecord, nil
}

// TestRecoveryConnection performs a read-only FTPS/FTP control-channel check.
// It does not list, download, modify or execute anything on the remote site.
func (a *App) TestRecoveryConnection(projectID string) recovery.ConnectionResult {
	if a.recoveryErr != nil {
		return recovery.ConnectionResult{Message: "Chưa sẵn sàng", Detail: a.recoveryErr.Error()}
	}
	if a.recovery == nil {
		return recovery.ConnectionResult{Message: "Chưa sẵn sàng", Detail: "WordPress recovery is not ready"}
	}
	if !a.recoveryAction.TryLock() {
		return recovery.ConnectionResult{Message: "Đang có thao tác khác", Detail: "Đợi thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer a.recoveryAction.Unlock()
	projectRecord, ok, err := a.recovery.Get(projectID)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("không tìm thấy website khôi phục")
		}
		return recovery.ConnectionResult{Message: "Không đọc được website", Detail: err.Error()}
	}
	password, err := secrets.GetRecoveryPassword(projectRecord.ID)
	if err != nil {
		result := recovery.ConnectionResult{Message: "Chưa có mật khẩu kết nối", Detail: err.Error(), CheckedAt: time.Now().UTC().Format(time.RFC3339)}
		_ = a.recovery.RecordConnection(projectRecord.ID, result)
		return result
	}
	result := recovery.TestConnection(a.ctxOrBackground(), projectRecord, password)
	if recordErr := a.recovery.RecordConnection(projectRecord.ID, result); recordErr != nil {
		if result.Detail != "" {
			result.Detail += "; "
		}
		result.Detail += "không ghi được lịch sử: " + recordErr.Error()
		result.Success = false
		result.Message = "Kết nối đã kiểm tra nhưng không lưu được lịch sử"
	}
	return result
}

// GetRecoveryBackupPlan returns only reviewed, non-secret connection metadata
// and the backend-owned destination shown before the read-only transfer starts.
func (a *App) GetRecoveryBackupPlan(projectID string) (recovery.BackupPlan, error) {
	if a.recoveryErr != nil {
		return recovery.BackupPlan{}, a.recoveryErr
	}
	if a.recovery == nil {
		return recovery.BackupPlan{}, errors.New("WordPress recovery is not ready")
	}
	projectRecord, ok, err := a.recovery.Get(projectID)
	if err != nil {
		return recovery.BackupPlan{}, err
	}
	if !ok {
		return recovery.BackupPlan{}, errors.New("không tìm thấy website khôi phục")
	}
	if projectRecord.Status != "connection_ready" && projectRecord.Status != "backup_ready" && projectRecord.Status != "backup_failed" {
		return recovery.BackupPlan{}, errors.New("hãy kiểm tra kết nối thành công trước khi sao lưu")
	}
	if !secrets.HasRecoveryPassword(projectRecord.ID) {
		return recovery.BackupPlan{}, errors.New("chưa có mật khẩu FTP/FTPS trong kho bí mật")
	}
	return a.recovery.BackupPlan(projectRecord.ID)
}

// RunRecoveryBackup performs a read-only FTP/FTPS download into neutral
// content-addressed blobs, verifies SHA-256 and runs a bounded static scan. It
// never uploads, renames or deletes anything on the hosting.
func (a *App) RunRecoveryBackup(projectID string) recovery.BackupResult {
	if a.recoveryErr != nil {
		return recovery.BackupResult{Message: "Chưa sẵn sàng", Detail: a.recoveryErr.Error()}
	}
	if a.recovery == nil {
		return recovery.BackupResult{Message: "Chưa sẵn sàng", Detail: "WordPress recovery is not ready"}
	}
	if !a.recoveryAction.TryLock() {
		return recovery.BackupResult{Message: "Đang có thao tác khác", Detail: "Đợi thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer a.recoveryAction.Unlock()
	projectRecord, ok, err := a.recovery.Get(projectID)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("không tìm thấy website khôi phục")
		}
		return recovery.BackupResult{Message: "Không đọc được website", Detail: err.Error()}
	}
	if projectRecord.Status != "connection_ready" && projectRecord.Status != "backup_ready" && projectRecord.Status != "backup_failed" {
		return recovery.BackupResult{Message: "Chưa thể sao lưu", Detail: "Hãy kiểm tra kết nối thành công trước khi sao lưu."}
	}
	password, err := secrets.GetRecoveryPassword(projectRecord.ID)
	if err != nil {
		return recovery.BackupResult{Message: "Chưa có mật khẩu kết nối", Detail: err.Error()}
	}
	projectDirectory, err := a.recovery.EnsureProjectDirectory(projectRecord.ID)
	if err != nil {
		return recovery.BackupResult{Message: "Không chuẩn bị được kho sao lưu", Detail: err.Error()}
	}
	if err := a.recovery.RecordBackupStarted(projectRecord.ID); err != nil {
		return recovery.BackupResult{Message: "Không bắt đầu được sao lưu", Detail: err.Error()}
	}
	result := recovery.Backup(a.ctxOrBackground(), projectRecord, password, projectDirectory, func(progress recovery.BackupProgress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "recovery:backup-progress", progress)
		}
	})
	if recordErr := a.recovery.RecordBackup(projectRecord.ID, result); recordErr != nil {
		if result.Detail != "" {
			result.Detail += "; "
		}
		result.Detail += "không ghi được lịch sử: " + recordErr.Error()
		result.Success = false
		result.Message = "Sao lưu đã chạy nhưng không lưu được lịch sử"
	}
	return result
}

// RemoveRecoveryProject forgets one profile and its native-vault secret. It
// never deletes remote website data or the local recovery evidence directory.
func (a *App) RemoveRecoveryProject(projectID string) error {
	if a.recoveryErr != nil {
		return a.recoveryErr
	}
	if a.recovery == nil {
		return errors.New("WordPress recovery is not ready")
	}
	if !a.recoveryAction.TryLock() {
		return errors.New("một thao tác khôi phục khác đang chạy")
	}
	defer a.recoveryAction.Unlock()
	if _, ok, err := a.recovery.Get(projectID); err != nil {
		return err
	} else if !ok {
		return errors.New("không tìm thấy website khôi phục")
	}
	if err := secrets.DeleteRecoveryPassword(projectID); err != nil {
		return err
	}
	return a.recovery.Delete(projectID)
}

// OpenRecoveryFolder creates and opens only the owned data directory for a
// stored recovery profile. No path is accepted from the frontend.
func (a *App) OpenRecoveryFolder(projectID string) error {
	if a.recoveryErr != nil {
		return a.recoveryErr
	}
	if a.recovery == nil {
		return errors.New("WordPress recovery is not ready")
	}
	path, err := a.recovery.EnsureProjectDirectory(projectID)
	if err != nil {
		return err
	}
	return hostopen.Directory(path)
}

// GetProjectDataStatus returns non-secret source/database/backup metadata for
// one exact project stored in the local history.
func (a *App) GetProjectDataStatus(projectPath string) (backup.Status, error) {
	record, err := a.storedProject(projectPath)
	if err != nil {
		return backup.Status{}, err
	}
	return backup.Inspect(a.backupConfig(record))
}

// CreateProjectBackup exports only the selected website's working copy and
// schema. The reviewed reverse-transfer path excludes runtime wp-config.php
// and is verified before the backup is committed on the host.
func (a *App) CreateProjectBackup(projectPath string) backup.Result {
	record, err := a.storedProject(projectPath)
	if err != nil {
		return backup.Result{Message: "Không đọc được website", Detail: err.Error()}
	}
	result := backup.Create(a.ctxOrBackground(), a.backupConfig(record), func(progress backup.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "backup:progress", progress)
		}
	})
	if persistErr := a.state.RecordBackup(record.Path, result.Success, result.Message, result.Detail); persistErr != nil {
		if result.Success {
			result.Success = false
			result.Message = "Backup đã tạo nhưng chưa lưu được lịch sử"
			result.Detail = persistErr.Error()
		} else if result.Detail == "" {
			result.Detail = persistErr.Error()
		}
	}
	return result
}

// OpenProjectSource opens the exact original source directory persisted by the
// backend. It never opens the isolated working copy or accepts a command.
func (a *App) OpenProjectSource(projectPath string) error {
	record, err := a.storedProject(projectPath)
	if err != nil {
		return err
	}
	return hostopen.Directory(record.Path)
}

// ListProjectSource returns one immediate directory level from the exact
// original host source. Symlinks, dependency trees and secret files are
// marked as blocked before any path crosses the Go/JavaScript bridge.
func (a *App) ListProjectSource(projectPath, directory string) (sourceeditor.Listing, error) {
	record, err := a.storedProject(projectPath)
	if err != nil {
		return sourceeditor.Listing{}, err
	}
	return sourceeditor.List(a.sourceEditorConfig(record), directory)
}

// ReadProjectSourceFile reads only an allowlisted UTF-8 source file up to 2MB.
func (a *App) ReadProjectSourceFile(projectPath, relativePath string) (sourceeditor.File, error) {
	record, err := a.storedProject(projectPath)
	if err != nil {
		return sourceeditor.File{}, err
	}
	return sourceeditor.Read(a.sourceEditorConfig(record), relativePath)
}

// SaveProjectSourceFile writes an explicitly selected original source file.
// A private backup and an optimistic SHA-256 conflict check are mandatory.
func (a *App) SaveProjectSourceFile(request sourceeditor.SaveRequest) sourceeditor.SaveResult {
	record, err := a.storedProject(request.ProjectPath)
	if err != nil {
		return sourceeditor.SaveResult{Message: "Không đọc được website", Detail: err.Error()}
	}
	result := sourceeditor.Save(a.sourceEditorConfig(record), request)
	if persistErr := a.state.RecordSourceEdit(record.Path, request.Path, result.Success, result.Message, result.Detail); persistErr != nil {
		if result.Success {
			result.Message = "Đã lưu source, chưa ghi được lịch sử"
			result.Detail = persistErr.Error()
		} else if result.Detail == "" {
			result.Detail = persistErr.Error()
		}
	}
	return result
}

// OpenProjectBackupFolder opens only a completed per-project backup folder.
func (a *App) OpenProjectBackupFolder(projectPath string) error {
	record, err := a.storedProject(projectPath)
	if err != nil {
		return err
	}
	status, err := backup.Inspect(a.backupConfig(record))
	if err != nil {
		return err
	}
	if status.BackupCount == 0 {
		return errors.New("website chưa có bản sao hoàn chỉnh")
	}
	return hostopen.Directory(status.BackupRoot)
}

// GetEnvironmentPlan returns the fixed shared-server configuration shown for consent.
func (a *App) GetEnvironmentPlan(request vm.Request) (vm.Plan, error) {
	return vm.GetPlan(request)
}

// CreateEnvironment creates or reuses the single OneClick Ubuntu server and streams progress.
// It does not copy, mount or execute the selected website.
func (a *App) CreateEnvironment(request vm.Request) vm.Result {
	return a.executeEnvironment(request, false)
}

// RecreateEnvironment is allowed only before any website data exists on the
// shared server. Once one snapshot/runtime exists, deleting the server would
// affect other websites and is therefore refused.
func (a *App) RecreateEnvironment(request vm.Request) vm.Result {
	if a.stateErr != nil || a.state == nil {
		return vm.Result{Message: "Không đọc được lịch sử môi trường", Detail: "project history is not ready"}
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil {
		return vm.Result{Message: "Không đọc được lịch sử môi trường", Detail: err.Error()}
	}
	plan, err := vm.GetPlan(request)
	if err != nil {
		return vm.Result{Message: "Không xác định được môi trường cần tạo lại", Detail: err.Error()}
	}
	if !found || record.VMName == "" || record.VMName != plan.VMName {
		return vm.Result{Message: "Không được phép xoá môi trường này", Detail: "Tên máy ảo không khớp lịch sử của website."}
	}
	snapshot, err := a.state.List()
	if err != nil {
		return vm.Result{Message: "Không kiểm tra được dữ liệu máy chủ", Detail: err.Error()}
	}
	if sharedServerContainsProjectData(snapshot) {
		return vm.Result{Message: "Không được phép xoá máy chủ dùng chung", Detail: "Máy chủ đã chứa dữ liệu website. OneClick từ chối tạo lại để không ảnh hưởng dự án khác."}
	}
	return a.executeEnvironment(request, true)
}

func sharedServerContainsProjectData(snapshot appstate.Snapshot) bool {
	for _, item := range snapshot.Projects {
		if item.SnapshotID != "" || item.RuntimeState != "" || item.DatabaseState != "" || item.TunnelID != "" || item.DNSRecordID != "" {
			return true
		}
	}
	return false
}

func (a *App) executeEnvironment(request vm.Request, recreate bool) vm.Result {
	if a.stateErr != nil || a.state == nil {
		detail := "project history is not ready"
		if a.stateErr != nil {
			detail = a.stateErr.Error()
		}
		return vm.Result{Message: "Không lưu được lịch sử triển khai", Detail: detail}
	}
	plan, err := vm.GetPlan(request)
	if err != nil {
		return vm.Result{Message: "Không chuẩn bị được môi trường", Detail: err.Error()}
	}
	if err := a.state.MarkEnvironmentStarted(request.ProjectPath, plan.VMName); err != nil {
		return vm.Result{Message: "Không lưu được bước đang thực hiện", Detail: err.Error()}
	}

	reporter := func(progress vm.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "environment:progress", progress)
		}
	}
	var result vm.Result
	if recreate {
		result = vm.Recreate(a.ctx, request, reporter)
	} else {
		result = vm.Create(a.ctx, request, reporter)
	}
	persistErr := a.state.MarkEnvironmentFinished(request.ProjectPath, appstate.EnvironmentOutcome{
		Success: result.Success, VMName: result.VMName, VMState: result.State,
		IP: result.IP, Message: result.Message, Detail: result.Detail,
	})
	if persistErr != nil {
		if result.Success {
			result.Detail = "Môi trường đã tạo nhưng chưa ghi được lịch sử: " + persistErr.Error()
		} else if result.Detail == "" {
			result.Detail = persistErr.Error()
		}
	}
	return result
}

// GetCopyPlan scans only file metadata and shows what will be copied. It does
// not create an archive, transfer data or execute project code.
func (a *App) GetCopyPlan(request project.CopyRequest) (project.CopyPlan, error) {
	if a.stateErr != nil || a.state == nil {
		return project.CopyPlan{}, errors.New("project history is not ready")
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil {
		return project.CopyPlan{}, err
	}
	if !found {
		return project.CopyPlan{}, errors.New("website chưa được lưu cấu hình")
	}
	if record.Stage != "environment_ready" && record.Stage != "copy_failed" && record.Stage != "source_ready" {
		return project.CopyPlan{}, errors.New("môi trường an toàn chưa sẵn sàng")
	}
	vmPlan, err := vm.GetPlan(vm.Request{ProjectPath: record.Path, ProjectName: record.Name})
	if err != nil {
		return project.CopyPlan{}, err
	}
	if record.VMName == "" || record.VMName != vmPlan.VMName {
		return project.CopyPlan{}, errors.New("máy ảo không khớp lịch sử website")
	}
	return project.PrepareCopyPlan(record.Path, record.DocumentRoot)
}

// CopyWebsite creates a one-way immutable snapshot, transfers it into the
// shared VM under a project-scoped path, verifies its checksum, and persists only metadata/history.
func (a *App) CopyWebsite(request project.CopyRequest) project.CopyResult {
	if a.stateErr != nil || a.state == nil {
		return project.CopyResult{Message: "Không đọc được lịch sử website", Detail: "project history is not ready"}
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil || !found {
		if err == nil {
			err = errors.New("website chưa được lưu cấu hình")
		}
		return project.CopyResult{Message: "Không chuẩn bị được bản sao", Detail: err.Error()}
	}
	vmPlan, err := vm.GetPlan(vm.Request{ProjectPath: record.Path, ProjectName: record.Name})
	if err != nil || record.VMName == "" || record.VMName != vmPlan.VMName {
		if err == nil {
			err = errors.New("máy ảo không khớp lịch sử website")
		}
		return project.CopyResult{Message: "Không xác định được máy ảo an toàn", Detail: err.Error()}
	}
	if err := a.state.MarkCopyStarted(record.Path); err != nil {
		return project.CopyResult{Message: "Không bắt đầu được bước sao chép", Detail: err.Error()}
	}

	report := func(progress project.CopyProgress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "copy:progress", progress)
		}
	}
	report(project.CopyProgress{Stage: "scan", Message: "Đang kiểm tra danh sách file…", Percent: 4})
	artifact, err := project.BuildSnapshot(record.Path, record.DocumentRoot, report)
	if err != nil {
		result := project.CopyResult{Message: "Không tạo được bản sao website", Detail: err.Error()}
		_ = a.state.MarkCopyFinished(record.Path, appstate.CopyOutcome{Message: result.Message, Detail: result.Detail})
		return result
	}
	defer artifact.Cleanup()

	transferResult := vm.TransferSnapshot(a.ctx, vm.SnapshotTransfer{
		ProjectPath: record.Path, ProjectName: record.Name, ProjectID: record.ID,
		ArchivePath: artifact.ArchivePath, Checksum: artifact.Checksum, SnapshotID: artifact.SnapshotID,
	}, func(progress vm.Progress) {
		report(project.CopyProgress{Stage: progress.Stage, Message: progress.Message, Percent: progress.Percent})
	})
	result := project.CopyResult{
		Success: transferResult.Success, Message: transferResult.Message, Detail: transferResult.Detail,
		SnapshotID: artifact.SnapshotID, Checksum: artifact.Checksum,
		FileCount: artifact.FileCount, TotalBytes: artifact.TotalBytes, GuestPath: transferResult.GuestPath,
	}
	persistErr := a.state.MarkCopyFinished(record.Path, appstate.CopyOutcome{
		Success: result.Success, SnapshotID: result.SnapshotID, Checksum: result.Checksum,
		GuestPath: result.GuestPath, FileCount: result.FileCount, TotalBytes: result.TotalBytes,
		Message: result.Message, Detail: result.Detail,
	})
	if persistErr != nil {
		if result.Success {
			result.Success = false
			result.Message = "Đã sao chép nhưng chưa lưu được lịch sử"
			result.Detail = persistErr.Error()
		} else if result.Detail == "" {
			result.Detail = persistErr.Error()
		}
	}
	return result
}

// GetRuntimePlan returns the native Ubuntu runtime plan shown before any
// shared package or isolated site service is created. JavaScript supplies only project path.
func (a *App) GetRuntimePlan(request runtimeenv.Request) (runtimeenv.Plan, error) {
	if a.stateErr != nil || a.state == nil {
		return runtimeenv.Plan{}, errors.New("project history is not ready")
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil {
		return runtimeenv.Plan{}, err
	}
	if !found {
		return runtimeenv.Plan{}, errors.New("website chưa được lưu cấu hình")
	}
	if record.Stage != "source_ready" && record.Stage != "runtime_failed" && record.Stage != "runtime_ready" {
		return runtimeenv.Plan{}, errors.New("website chưa được sao chép đầy đủ")
	}
	config := runtimeConfig(record)
	return runtimeenv.GetPlan(config, record.RuntimeState != "")
}

// InstallRuntime installs the shared native stack once, then provisions a
// locked-down Linux user, PHP-FPM service and MariaDB database for this site.
// It does not publish a port or return any secret.
func (a *App) InstallRuntime(request runtimeenv.Request) runtimeenv.Result {
	if a.stateErr != nil || a.state == nil {
		return runtimeenv.Result{Message: "Không đọc được lịch sử website", Detail: "project history is not ready"}
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil || !found {
		if err == nil {
			err = errors.New("website chưa được lưu cấu hình")
		}
		return runtimeenv.Result{Message: "Không chuẩn bị được môi trường chạy", Detail: err.Error()}
	}
	config := runtimeConfig(record)
	if _, err := runtimeenv.GetPlan(config, record.RuntimeState != ""); err != nil {
		return runtimeenv.Result{Message: "Không chuẩn bị được môi trường chạy", Detail: err.Error()}
	}
	if err := a.state.MarkRuntimeStarted(record.Path); err != nil {
		return runtimeenv.Result{Message: "Không bắt đầu được bước cài môi trường chạy", Detail: err.Error()}
	}
	result := runtimeenv.Provision(a.ctx, config, func(progress runtimeenv.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "runtime:progress", progress)
		}
	})
	persistErr := a.state.MarkRuntimeFinished(record.Path, appstate.RuntimeOutcome{
		Success: result.Success, Adapter: result.Adapter, State: result.RuntimeState,
		Health: result.Health, Containers: result.Services, SnapshotID: result.SnapshotID,
		Message: result.Message, Detail: result.Detail,
	})
	if persistErr != nil {
		if result.Success {
			result.Success = false
			result.Message = "Runtime đã chạy nhưng chưa lưu được lịch sử"
			result.Detail = persistErr.Error()
		} else if result.Detail == "" {
			result.Detail = persistErr.Error()
		}
	}
	return result
}

func runtimeConfig(record appstate.ProjectRecord) runtimeenv.Config {
	return runtimeenv.Config{
		ProjectID: record.ID, ProjectPath: record.Path, ProjectName: record.Name,
		ProjectKind: record.Kind, DocumentRoot: record.DocumentRoot, PHPVersion: record.PHPVersion, VMName: record.VMName,
		SnapshotID: record.SnapshotID, SnapshotHash: record.SnapshotHash, SnapshotPath: record.SnapshotPath,
	}
}

// GetDatabasePlan detects a local WordPress database without executing any
// project PHP. The frontend receives metadata only; credentials stay in Go.
func (a *App) GetDatabasePlan(request database.Request) (database.Plan, error) {
	if a.stateErr != nil || a.state == nil {
		return database.Plan{}, errors.New("project history is not ready")
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil {
		return database.Plan{}, err
	}
	if !found {
		return database.Plan{}, errors.New("website chưa được lưu cấu hình")
	}
	if record.Stage != "runtime_ready" && record.Stage != "database_failed" && record.Stage != "database_ready" {
		return database.Plan{}, errors.New("runtime chưa sẵn sàng để nhập database")
	}
	if err := a.requireReplacementBackup(record); err != nil {
		return database.Plan{}, err
	}
	request.ProjectPath = record.Path
	return database.GetPlan(a.databaseConfig(record), request)
}

// SelectDatabaseFile opens a native picker. Selection alone does not read,
// transfer or import the file.
func (a *App) SelectDatabaseFile(projectPath string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("application is not ready")
	}
	if a.stateErr != nil || a.state == nil {
		return "", errors.New("project history is not ready")
	}
	record, found, err := a.state.GetProject(projectPath)
	if err != nil || !found {
		if err == nil {
			err = errors.New("website chưa được lưu cấu hình")
		}
		return "", err
	}
	if record.Stage != "runtime_ready" && record.Stage != "database_failed" && record.Stage != "database_ready" {
		return "", errors.New("runtime chưa sẵn sàng để chọn database")
	}
	if err := a.requireReplacementBackup(record); err != nil {
		return "", err
	}
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Chọn bản sao database WordPress",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Database WordPress (*.sql;*.sql.gz)", Pattern: "*.sql;*.sql.gz"},
		},
	})
}

// ImportDatabase exports a local WordPress database or copies an explicit SQL
// dump one way into the VM, then replaces only that project's blank database.
func (a *App) ImportDatabase(request database.Request) database.Result {
	if a.stateErr != nil || a.state == nil {
		return database.Result{Message: "Không đọc được lịch sử website", Detail: "project history is not ready"}
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil || !found {
		if err == nil {
			err = errors.New("website chưa được lưu cấu hình")
		}
		return database.Result{Message: "Không chuẩn bị được database", Detail: err.Error()}
	}
	request.ProjectPath = record.Path
	if err := a.requireReplacementBackup(record); err != nil {
		return database.Result{Message: "Chưa thể thay database", Detail: err.Error()}
	}
	if plan, err := database.GetPlan(a.databaseConfig(record), request); err != nil || !plan.Ready {
		if err == nil {
			err = errors.New(plan.AutomaticMessage)
		}
		return database.Result{Message: "Không chuẩn bị được database", Detail: err.Error()}
	}
	if err := a.state.MarkDatabaseStarted(record.Path); err != nil {
		return database.Result{Message: "Không bắt đầu được bước database", Detail: err.Error()}
	}
	result := database.Import(a.ctx, a.databaseConfig(record), request, func(progress database.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "database:progress", progress)
		}
	})
	persistErr := a.state.MarkDatabaseFinished(record.Path, appstate.DatabaseOutcome{
		Success: result.Success, Source: result.Source, Tables: result.Tables, Bytes: result.Bytes,
		TablePrefix: result.TablePrefix, Message: result.Message, Detail: result.Detail,
	})
	if persistErr != nil {
		if result.Success {
			result.Success = false
			result.Message = "Database đã nhập nhưng chưa lưu được lịch sử"
			result.Detail = persistErr.Error()
		} else if result.Detail == "" {
			result.Detail = persistErr.Error()
		}
	}
	return result
}

func (a *App) databaseConfig(record appstate.ProjectRecord) database.Config {
	tempRoot := ""
	if root, err := datastore.ApplicationRoot(); err == nil {
		tempRoot = filepath.Join(root, "data", "transfers")
	}
	return database.Config{
		ProjectID: record.ID, ProjectPath: record.Path, ProjectName: record.Name,
		ProjectKind: record.Kind, DocumentRoot: record.DocumentRoot, VMName: record.VMName,
		RuntimeState: record.RuntimeState, RuntimeHealth: record.RuntimeHealth, TempRoot: tempRoot,
	}
}

func (a *App) backupConfig(record appstate.ProjectRecord) backup.Config {
	backupRoot := ""
	if root, err := datastore.ApplicationRoot(); err == nil {
		backupRoot = filepath.Join(root, "data", "backups")
	}
	return backup.Config{
		ProjectID: record.ID, ProjectPath: record.Path, ProjectName: record.Name, ProjectKind: record.Kind,
		VMName: record.VMName, Stage: record.Stage, RuntimeState: record.RuntimeState, RuntimeHealth: record.RuntimeHealth,
		DatabaseState: record.DatabaseState, DatabaseTables: record.DatabaseTables,
		DatabaseImportedAt: record.DatabaseImportedAt, BackupRoot: backupRoot,
	}
}

func (a *App) sourceEditorConfig(record appstate.ProjectRecord) sourceeditor.Config {
	backupRoot := a.sourceBackupRoot
	if backupRoot == "" {
		if root, err := datastore.ApplicationRoot(); err == nil {
			backupRoot = filepath.Join(root, "data", "source-edits")
		}
	}
	return sourceeditor.Config{
		ProjectID: record.ID, ProjectPath: record.Path, ProjectName: record.Name, BackupRoot: backupRoot,
	}
}

func (a *App) requireReplacementBackup(record appstate.ProjectRecord) error {
	if record.Stage != "database_ready" {
		return nil
	}
	status, err := backup.Inspect(a.backupConfig(record))
	if err != nil {
		return fmt.Errorf("không kiểm tra được backup trước khi thay database: %w", err)
	}
	if status.BackupCount == 0 {
		return errors.New("hãy tạo backup source và database hiện tại trước khi thay database")
	}
	return nil
}

// GetCloudflareConnections returns every configured zone with a live provider
// and DNS check. API tokens never cross the Go/JavaScript bridge.
func (a *App) GetCloudflareConnections() ([]cloudflare.ConnectionStatus, error) {
	if a.stateErr != nil || a.state == nil {
		return nil, errors.New("không đọc được cấu hình Cloudflare")
	}
	snapshot, err := a.state.List()
	if err != nil {
		return nil, err
	}
	statuses := make([]cloudflare.ConnectionStatus, len(snapshot.Domains))
	var wait sync.WaitGroup
	for index := range snapshot.Domains {
		wait.Add(1)
		go func(item int) {
			defer wait.Done()
			statuses[item] = a.cloudflareConnectionStatus(snapshot.Domains[item])
		}(index)
	}
	wait.Wait()
	sort.SliceStable(statuses, func(left, right int) bool { return statuses[left].Zone < statuses[right].Zone })
	return statuses, nil
}

// GetCloudflareConnection is retained for older frontend bindings. New UI uses
// GetCloudflareConnections and lets each website select an explicit zone.
func (a *App) GetCloudflareConnection() cloudflare.ConnectionStatus {
	statuses, err := a.GetCloudflareConnections()
	if err != nil || len(statuses) == 0 {
		message := "Chưa kết nối Cloudflare"
		if err != nil {
			message = err.Error()
		}
		return cloudflare.ConnectionStatus{Message: message}
	}
	for _, status := range statuses {
		if status.DNSReady {
			return status
		}
	}
	return statuses[0]
}

func (a *App) cloudflareConnectionStatus(connection appstate.CloudflareConnection) cloudflare.ConnectionStatus {
	status := cloudflare.ConnectionStatus{Connected: true, Zone: connection.Zone, ZoneID: connection.ZoneID, AccountID: connection.AccountID, ZoneStatus: connection.ZoneStatus, AssignedNameServers: connection.NameServers}
	status.Connected = true
	token, err := secrets.GetCloudflareToken(connection.Zone)
	if err != nil {
		status.Message = "Cần kết nối lại API token"
		return status
	}
	status.TokenStored = true
	verifyCtx, verifyCancel := context.WithTimeout(a.ctxOrBackground(), 15*time.Second)
	if client, clientErr := cloudflare.NewClient(token); clientErr == nil {
		if fresh, verifyErr := client.VerifyZone(verifyCtx, connection.Zone); verifyErr == nil {
			connection.ZoneID = fresh.ZoneID
			connection.AccountID = fresh.AccountID
			connection.ZoneStatus = fresh.Status
			connection.NameServers = fresh.NameServers
			_ = a.state.SaveCloudflareConnection(connection)
			status.ZoneID = fresh.ZoneID
			status.AccountID = fresh.AccountID
			status.ZoneStatus = fresh.Status
			status.AssignedNameServers = fresh.NameServers
		}
	}
	verifyCancel()
	dnsCtx, dnsCancel := context.WithTimeout(a.ctxOrBackground(), 10*time.Second)
	active, err := cloudflare.LookupNameServers(dnsCtx, connection.Zone)
	dnsCancel()
	if err != nil {
		status.Message = "Chưa kiểm tra được nameserver hiện tại"
		return status
	}
	status.ActiveNameServers = active
	status.DNSReady = connection.ZoneStatus == "active" && cloudflare.NameServersMatch(active, connection.NameServers)
	if status.DNSReady {
		status.Message = "Domain đã sẵn sàng"
	} else {
		status.Message = "Chờ chuyển nameserver sang Cloudflare"
	}
	return status
}

// SaveCloudflareConnection verifies a scoped API token and stores it only in
// the native OS credential vault. state.json receives non-secret identifiers.
func (a *App) SaveCloudflareConnection(input cloudflare.ConnectionInput) (cloudflare.ConnectionStatus, error) {
	if a.stateErr != nil || a.state == nil {
		return cloudflare.ConnectionStatus{}, errors.New("project history is not ready")
	}
	client, err := cloudflare.NewClient(input.APIToken)
	if err != nil {
		return cloudflare.ConnectionStatus{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 45*time.Second)
	defer cancel()
	zone, err := client.VerifyZone(ctx, input.Zone)
	if err != nil {
		return cloudflare.ConnectionStatus{}, err
	}
	if err := secrets.SaveCloudflareToken(zone.Name, input.APIToken); err != nil {
		return cloudflare.ConnectionStatus{}, err
	}
	if err := a.state.SaveCloudflareConnection(appstate.CloudflareConnection{
		Zone: zone.Name, ZoneID: zone.ZoneID, AccountID: zone.AccountID,
		ZoneStatus: zone.Status, NameServers: zone.NameServers,
	}); err != nil {
		return cloudflare.ConnectionStatus{}, err
	}
	return a.cloudflareConnectionStatus(appstate.CloudflareConnection{
		Zone: zone.Name, ZoneID: zone.ZoneID, AccountID: zone.AccountID,
		ZoneStatus: zone.Status, NameServers: zone.NameServers,
	}), nil
}

// RemoveCloudflareConnection removes only one exact, unused zone. The state
// repository blocks deletion while a project may still own DNS/tunnel data.
func (a *App) RemoveCloudflareConnection(zone string) error {
	if a.stateErr != nil || a.state == nil {
		return errors.New("project history is not ready")
	}
	normalized, err := cloudflare.NormalizeZone(zone)
	if err != nil {
		return err
	}
	if err := a.state.RemoveCloudflareConnection(normalized); err != nil {
		return err
	}
	if err := secrets.DeleteCloudflareToken(normalized); err != nil {
		return fmt.Errorf("đã xóa domain khỏi danh sách nhưng chưa dọn được token trong kho bí mật: %w", err)
	}
	return nil
}

// GetTunnelPlan verifies that the requested hostname belongs to the connected
// zone and is not already occupied by an unrelated DNS record.
func (a *App) GetTunnelPlan(request tunnel.Request) (tunnel.Plan, error) {
	record, connection, client, err := a.tunnelContext(request.ProjectPath, request.Hostname, true)
	if err != nil {
		return tunnel.Plan{}, err
	}
	if record.Stage != "database_ready" && record.Stage != "tunnel_failed" {
		return tunnel.Plan{}, errors.New("database chưa sẵn sàng để kết nối domain")
	}
	hostname, err := cloudflare.NormalizeHostname(request.Hostname, connection.Name)
	if err != nil {
		return tunnel.Plan{}, err
	}
	spec := cloudflare.DeploymentSpec{ProjectID: record.ID, TunnelID: record.TunnelID, DNSRecordID: record.DNSRecordID, Hostname: hostname, Zone: connection}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 30*time.Second)
	defer cancel()
	if err := client.CheckHostname(ctx, spec); err != nil {
		return tunnel.Plan{}, err
	}
	config := tunnelConfig(record, hostname, "")
	return tunnel.GetPlan(config, record.TunnelID != "")
}

// StartNamedTunnel creates one remotely-managed Cloudflare tunnel and one DNS
// record for the project, then starts a token-file connector inside the VM.
func (a *App) StartNamedTunnel(request tunnel.Request) tunnel.Result {
	record, connection, client, err := a.tunnelContext(request.ProjectPath, request.Hostname, true)
	if err != nil {
		return tunnel.Result{Message: "Không chuẩn bị được domain", Detail: err.Error(), State: "failed"}
	}
	hostname, err := cloudflare.NormalizeHostname(request.Hostname, connection.Name)
	if err != nil {
		return tunnel.Result{Message: "Subdomain không hợp lệ", Detail: err.Error(), State: "failed"}
	}
	if err := a.state.MarkTunnelStarted(record.Path, hostname, connection.Name); err != nil {
		return tunnel.Result{Message: "Không bắt đầu được bước kết nối domain", Detail: err.Error(), State: "failed"}
	}
	emitTunnel := func(progress tunnel.Progress) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "tunnel:progress", progress)
		}
	}
	emitTunnel(tunnel.Progress{Stage: "cloudflare", Message: "Đang tạo tunnel và DNS trên Cloudflare…", Percent: 4})
	spec := cloudflare.DeploymentSpec{ProjectID: record.ID, TunnelID: record.TunnelID, DNSRecordID: record.DNSRecordID, Hostname: hostname, Zone: connection}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 30*time.Minute)
	defer cancel()
	allocation, err := client.EnsureDeployment(ctx, spec)
	if err != nil {
		result := tunnel.Result{Message: "Không tạo được tunnel và DNS", Detail: err.Error(), State: "failed"}
		_ = a.state.MarkTunnelFinished(record.Path, appstate.TunnelOutcome{Message: result.Message, Detail: result.Detail})
		return result
	}
	if err := a.state.MarkTunnelAllocated(record.Path, allocation.TunnelID, allocation.DNSRecordID); err != nil {
		_ = client.DeleteDeployment(context.Background(), cloudflare.DeploymentSpec{ProjectID: record.ID, TunnelID: allocation.TunnelID, DNSRecordID: allocation.DNSRecordID, Hostname: hostname, Zone: connection})
		return tunnel.Result{Message: "Đã tạo tunnel nhưng chưa lưu được lịch sử", Detail: err.Error(), State: "failed"}
	}
	emitTunnel(tunnel.Progress{Stage: "database", Message: "Đang cập nhật địa chỉ website theo domain…", Percent: 38})
	if err := database.ConfigureWordPressURL(ctx, a.databaseConfig(record), record.DatabasePrefix, hostname); err != nil {
		result := tunnel.Result{Message: "Không cập nhật được địa chỉ WordPress", Detail: err.Error(), State: "failed"}
		cleanupErr := client.DeleteDeployment(context.Background(), cloudflare.DeploymentSpec{ProjectID: record.ID, TunnelID: allocation.TunnelID, DNSRecordID: allocation.DNSRecordID, Hostname: hostname, Zone: connection})
		if cleanupErr != nil {
			result.Detail = strings.TrimSpace(result.Detail + "\nKhông dọn được Cloudflare: " + cleanupErr.Error())
			_ = a.state.MarkTunnelFinished(record.Path, appstate.TunnelOutcome{TunnelID: allocation.TunnelID, DNSRecordID: allocation.DNSRecordID, Message: result.Message, Detail: result.Detail})
		} else {
			_ = a.state.MarkTunnelFinished(record.Path, appstate.TunnelOutcome{Message: result.Message, Detail: result.Detail})
		}
		return result
	}
	config := tunnelConfig(record, hostname, allocation.TunnelToken)
	result := tunnel.Start(ctx, config, emitTunnel)
	if !result.Success {
		cleanupErr := client.DeleteDeployment(context.Background(), cloudflare.DeploymentSpec{ProjectID: record.ID, TunnelID: allocation.TunnelID, DNSRecordID: allocation.DNSRecordID, Hostname: hostname, Zone: connection})
		if cleanupErr != nil {
			result.Detail = strings.TrimSpace(result.Detail + "\nKhông dọn được Cloudflare: " + cleanupErr.Error())
			_ = a.state.MarkTunnelFinished(record.Path, appstate.TunnelOutcome{TunnelID: allocation.TunnelID, DNSRecordID: allocation.DNSRecordID, Message: result.Message, Detail: result.Detail})
		} else {
			_ = a.state.MarkTunnelFinished(record.Path, appstate.TunnelOutcome{Message: result.Message, Detail: result.Detail})
		}
		return result
	}
	persistErr := a.state.MarkTunnelFinished(record.Path, appstate.TunnelOutcome{
		Success: true, DomainZone: connection.Name, Hostname: hostname, TunnelID: allocation.TunnelID, DNSRecordID: allocation.DNSRecordID,
		URL: result.URL, State: result.State, Health: result.Health, Message: result.Message,
	})
	if persistErr != nil {
		_ = tunnel.Stop(context.Background(), config)
		_ = client.DeleteDeployment(context.Background(), cloudflare.DeploymentSpec{ProjectID: record.ID, TunnelID: allocation.TunnelID, DNSRecordID: allocation.DNSRecordID, Hostname: hostname, Zone: connection})
		_ = a.state.MarkTunnelFinished(record.Path, appstate.TunnelOutcome{Message: "Đã rollback domain vì chưa lưu được lịch sử", Detail: persistErr.Error()})
		return tunnel.Result{Message: "Không lưu được trạng thái domain", Detail: persistErr.Error(), State: "failed"}
	}
	return result
}

// StopNamedTunnel removes the public DNS/tunnel and stops the guest connector.
// Native runtime services and copied website data remain untouched.
func (a *App) StopNamedTunnel(request tunnel.Request) tunnel.Result {
	if a.stateErr != nil || a.state == nil {
		return tunnel.Result{Message: "Không chuẩn bị được thao tác dừng", Detail: "project history is not ready", State: "failed"}
	}
	record, found, err := a.state.GetProject(request.ProjectPath)
	if err != nil || !found {
		if err == nil {
			err = errors.New("website chưa được lưu cấu hình")
		}
		return tunnel.Result{Message: "Không chuẩn bị được thao tác dừng", Detail: err.Error(), State: "failed"}
	}
	if record.Hostname == "" || record.TunnelID == "" {
		return tunnel.Result{Message: "Không tìm thấy domain đang public", State: "failed"}
	}
	snapshot, err := a.state.List()
	if err != nil {
		return tunnel.Result{Message: "Không đọc được cấu hình Cloudflare", Detail: err.Error(), State: "failed"}
	}
	connection, err := selectCloudflareConnection(snapshot.Domains, record.Hostname, record.DomainZone)
	if err != nil {
		return tunnel.Result{Message: "Không tìm thấy domain đã dùng", Detail: err.Error(), State: "failed"}
	}
	if err := a.state.MarkTunnelStopping(record.Path); err != nil {
		return tunnel.Result{Message: "Không bắt đầu được thao tác dừng", Detail: err.Error(), State: "failed"}
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 6*time.Minute)
	defer cancel()
	type providerOutcome struct{ err error }
	providerDone := make(chan providerOutcome, 1)
	go func() {
		if connection.Zone == "" || connection.ZoneID == "" || connection.AccountID == "" {
			providerDone <- providerOutcome{errors.New("thiếu cấu hình Cloudflare; connector đã được ưu tiên dừng trong máy ảo")}
			return
		}
		token, tokenErr := secrets.GetCloudflareToken(connection.Zone)
		if tokenErr != nil {
			providerDone <- providerOutcome{fmt.Errorf("cần kết nối lại API token để xóa DNS: %w", tokenErr)}
			return
		}
		client, clientErr := cloudflare.NewClient(token)
		if clientErr != nil {
			providerDone <- providerOutcome{clientErr}
			return
		}
		zone := cloudflare.Zone{Name: connection.Zone, ZoneID: connection.ZoneID, AccountID: connection.AccountID, Status: connection.ZoneStatus, NameServers: connection.NameServers}
		spec := cloudflare.DeploymentSpec{ProjectID: record.ID, TunnelID: record.TunnelID, DNSRecordID: record.DNSRecordID, Hostname: record.Hostname, Zone: zone}
		providerDone <- providerOutcome{client.DeleteDeployment(ctx, spec)}
	}()
	guestDone := make(chan tunnel.Result, 1)
	go func() { guestDone <- tunnel.Stop(ctx, tunnelConfig(record, record.Hostname, "")) }()
	providerErr := (<-providerDone).err
	guestResult := <-guestDone
	if providerErr != nil || !guestResult.Success {
		detail := ""
		if providerErr != nil {
			detail = "Cloudflare: " + providerErr.Error()
		}
		if !guestResult.Success {
			detail = strings.TrimSpace(detail + "\nMáy ảo: " + guestResult.Detail)
		}
		result := tunnel.Result{Message: "Chưa dừng sạch được domain", Detail: detail, State: "unknown"}
		_ = a.state.MarkTunnelStopped(record.Path, appstate.TunnelOutcome{Message: result.Message, Detail: result.Detail})
		return result
	}
	result := tunnel.Result{Success: true, Message: "Đã dừng truy cập domain", State: "stopped"}
	if err := a.state.MarkTunnelStopped(record.Path, appstate.TunnelOutcome{Success: true, Message: result.Message}); err != nil {
		result.Success = false
		result.Message = "Đã dừng domain nhưng chưa lưu được lịch sử"
		result.Detail = err.Error()
	}
	return result
}

func (a *App) tunnelContext(projectPath, hostname string, requireDNS bool) (appstate.ProjectRecord, cloudflare.Zone, *cloudflare.Client, error) {
	if a.stateErr != nil || a.state == nil {
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, errors.New("project history is not ready")
	}
	snapshot, err := a.state.List()
	if err != nil {
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, err
	}
	record, found, err := a.state.GetProject(projectPath)
	if err != nil || !found {
		if err == nil {
			err = errors.New("website chưa được lưu cấu hình")
		}
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, err
	}
	connection, err := selectCloudflareConnection(snapshot.Domains, hostname, record.DomainZone)
	if err != nil {
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, err
	}
	if connection.Zone == "" || connection.ZoneID == "" || connection.AccountID == "" {
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, errors.New("chưa kết nối Cloudflare trong Cài đặt")
	}
	token, err := secrets.GetCloudflareToken(connection.Zone)
	if err != nil {
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, err
	}
	client, err := cloudflare.NewClient(token)
	if err != nil {
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, err
	}
	verifyCtx, verifyCancel := context.WithTimeout(a.ctxOrBackground(), 20*time.Second)
	fresh, verifyErr := client.VerifyZone(verifyCtx, connection.Zone)
	verifyCancel()
	if verifyErr == nil {
		connection.ZoneID = fresh.ZoneID
		connection.AccountID = fresh.AccountID
		connection.ZoneStatus = fresh.Status
		connection.NameServers = fresh.NameServers
		_ = a.state.SaveCloudflareConnection(connection)
	} else if requireDNS {
		return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, fmt.Errorf("không xác minh được domain Cloudflare: %w", verifyErr)
	}
	if requireDNS {
		ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 12*time.Second)
		defer cancel()
		active, lookupErr := cloudflare.LookupNameServers(ctx, connection.Zone)
		if lookupErr != nil || connection.ZoneStatus != "active" || !cloudflare.NameServersMatch(active, connection.NameServers) {
			return appstate.ProjectRecord{}, cloudflare.Zone{}, nil, fmt.Errorf("domain %s chưa chuyển nameserver sang Cloudflare", connection.Zone)
		}
	}
	zone := cloudflare.Zone{Name: connection.Zone, ZoneID: connection.ZoneID, AccountID: connection.AccountID, Status: connection.ZoneStatus, NameServers: connection.NameServers}
	return record, zone, client, nil
}

func selectCloudflareConnection(connections []appstate.CloudflareConnection, hostname, preferredZone string) (appstate.CloudflareConnection, error) {
	preferredZone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(preferredZone), "."))
	if preferredZone != "" {
		for _, connection := range connections {
			if strings.EqualFold(connection.Zone, preferredZone) {
				return connection, nil
			}
		}
	}
	hostname, err := cloudflare.NormalizeZone(hostname)
	if err != nil {
		return appstate.CloudflareConnection{}, err
	}
	best := appstate.CloudflareConnection{}
	for _, connection := range connections {
		zone := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(connection.Zone), "."))
		if zone != "" && (hostname == zone || strings.HasSuffix(hostname, "."+zone)) && len(zone) > len(best.Zone) {
			best = connection
		}
	}
	if best.Zone == "" {
		return appstate.CloudflareConnection{}, errors.New("địa chỉ website không thuộc domain nào đã kết nối")
	}
	return best, nil
}

func tunnelConfig(record appstate.ProjectRecord, hostname, token string) tunnel.Config {
	return tunnel.Config{
		ProjectID: record.ID, ProjectPath: record.Path, ProjectName: record.Name, VMName: record.VMName,
		RuntimeState: record.RuntimeState, RuntimeHealth: record.RuntimeHealth,
		Hostname: hostname, TunnelToken: token,
	}
}

func (a *App) ctxOrBackground() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func appErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (a *App) storedProject(projectPath string) (appstate.ProjectRecord, error) {
	if a.stateErr != nil {
		return appstate.ProjectRecord{}, a.stateErr
	}
	if a.state == nil {
		return appstate.ProjectRecord{}, errors.New("project history is not ready")
	}
	record, found, err := a.state.GetProject(projectPath)
	if err != nil {
		return appstate.ProjectRecord{}, err
	}
	if !found {
		return appstate.ProjectRecord{}, errors.New("website chưa được lưu cấu hình")
	}
	return record, nil
}

func (a *App) projectCount() (int, error) {
	if a.stateErr != nil {
		return 0, a.stateErr
	}
	if a.state == nil {
		return 0, errors.New("project history is not ready")
	}
	snapshot, err := a.state.List()
	if err != nil {
		return 0, err
	}
	return len(snapshot.Projects), nil
}

// OpenExternalURL delegates URL opening to the operating system.
func (a *App) OpenExternalURL(url string) error {
	if a.ctx == nil {
		return errors.New("application is not ready")
	}
	if url == "" {
		return errors.New("URL is empty")
	}
	parsed, err := urlpkg.Parse(url)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("only valid HTTPS URLs without credentials are allowed")
	}

	wailsruntime.BrowserOpenURL(a.ctx, url)
	return nil
}
