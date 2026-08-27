//go:build windows

package datastore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"oneclick-dev-server/internal/hostexec"
	"oneclick-dev-server/internal/multipass"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	machineEnvironmentKey = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	storageValueName      = "MULTIPASS_STORAGE"
	minimumFreeBytes      = 10 * 1024 * 1024 * 1024
	helperSchema          = 1
	helperOperation       = "configure_multipass_storage"
)

type multipassList struct {
	List []json.RawMessage `json:"list"`
}

func platformInspect(ctx context.Context, projectCount int) Status {
	root, rootErr := platformApplicationRoot()
	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	defaultPath := filepath.Join(programData, "Multipass")
	recommended := ""
	if rootErr == nil {
		recommended = filepath.Join(root, "data", "multipass")
	}
	customPath, registryErr := readMachineStorage()
	currentPath := defaultPath
	configured := customPath != ""
	if configured {
		currentPath = customPath
	}
	status := Status{
		Platform: "windows", Supported: true, Configured: configured,
		CurrentPath: currentPath, DefaultPath: defaultPath, RecommendedPath: recommended,
		ProjectCount: projectCount, RequiresAdmin: true,
	}
	if registryErr != nil {
		status.Message = "Không đọc được cấu hình lưu trữ của Windows"
		status.Detail = registryErr.Error()
		return status
	}
	spacePath := currentPath
	if !configured && recommended != "" {
		spacePath = recommended
	}
	if free, err := freeBytes(spacePath); err == nil {
		status.FreeBytes = free
	} else if recommended != "" {
		status.FreeBytes, _ = freeBytes(recommended)
	}

	executable, err := multipass.Find()
	if err != nil {
		status.Message = "Cài Multipass trước khi chọn nơi lưu dữ liệu"
		status.Detail = err.Error()
		return status
	}
	instances, err := listInstanceCount(ctx, executable)
	if err != nil {
		status.Message = "Chưa xác minh được dữ liệu Multipass"
		status.Detail = err.Error()
		return status
	}
	status.InstanceCount = instances
	status.Locked = instances > 0 || projectCount > 0
	status.CanConfigure = !configured && !status.Locked
	switch {
	case configured:
		status.Message = "Dữ liệu máy ảo mới sẽ được lưu tại thư mục đã chọn."
		if status.Locked {
			status.Detail = "Vị trí đã khóa sau lần deploy đầu tiên để tránh làm hỏng máy ảo."
		}
	case status.Locked:
		status.Message = "Không thể đổi vị trí sau khi đã có dự án hoặc máy ảo."
		status.Detail = "OneClick không tự chuyển nóng dữ liệu đang hoạt động."
	default:
		status.Message = "Chưa chọn nơi lưu dữ liệu nặng. Hãy thiết lập trước lần deploy đầu tiên."
	}
	return status
}

func platformConfigure(ctx context.Context, target string, projectCount int, report Reporter) Result {
	report(Progress{Stage: "check", Message: "Đang kiểm tra thư mục và Multipass…", Percent: 8})
	status := platformInspect(ctx, projectCount)
	if !status.Supported {
		return Result{Message: "Chưa hỗ trợ trên máy này", Detail: status.Detail, Status: status}
	}
	if status.Configured {
		return Result{Message: "Vị trí dữ liệu đã được thiết lập", Detail: "OneClick không chuyển nóng kho dữ liệu đã cấu hình.", Status: status}
	}
	if status.Locked || !status.CanConfigure {
		return Result{Message: "Không thể đổi vị trí dữ liệu", Detail: status.Message + " " + status.Detail, Status: status}
	}

	cleanTarget, err := validateTarget(target, status.DefaultPath)
	if err != nil {
		return Result{Message: "Thư mục chưa phù hợp", Detail: err.Error(), Status: status}
	}
	executable, err := multipass.Find()
	if err != nil {
		return Result{Message: "Không tìm thấy Multipass", Detail: err.Error(), Status: status}
	}
	// Recheck immediately before elevation. This closes the normal UI race
	// where an instance could have appeared after the status card loaded.
	instances, err := listInstanceCount(ctx, executable)
	if err != nil || instances != 0 {
		detail := "Multipass không phản hồi."
		if err != nil {
			detail = err.Error()
		} else {
			detail = fmt.Sprintf("Đã có %d máy ảo; vị trí dữ liệu đã bị khóa.", instances)
		}
		return Result{Message: "Không thể thiết lập vị trí dữ liệu", Detail: detail, Status: platformInspect(ctx, projectCount)}
	}

	report(Progress{Stage: "permission", Message: "Hãy xác nhận cửa sổ của Windows…", Percent: 24})
	if err := runElevatedBootstrap(ctx, cleanTarget, report); err != nil {
		return Result{Message: "Không thiết lập được nơi lưu dữ liệu", Detail: err.Error(), Status: platformInspect(ctx, projectCount)}
	}
	report(Progress{Stage: "verify", Message: "Đang kiểm tra lại dịch vụ Multipass…", Percent: 94})
	verified := platformInspect(ctx, projectCount)
	if !verified.Configured || !samePath(verified.CurrentPath, cleanTarget) {
		return Result{Message: "Windows chưa nhận vị trí dữ liệu mới", Detail: "Cấu hình không vượt qua kiểm tra sau khi khởi động lại dịch vụ.", Status: verified}
	}
	if _, err := listInstanceCount(ctx, executable); err != nil {
		return Result{Message: "Multipass chưa phản hồi sau khi thiết lập", Detail: err.Error(), Status: verified}
	}
	report(Progress{Stage: "complete", Message: "Đã thiết lập nơi lưu dữ liệu", Percent: 100})
	return Result{Success: true, Message: "Đã thiết lập nơi lưu dữ liệu", Status: verified}
}

func validateTarget(value, defaultPath string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("chưa chọn thư mục")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("đường dẫn không hợp lệ: %w", err)
	}
	target := filepath.Clean(absolute)
	volume := filepath.VolumeName(target)
	if volume == "" || strings.HasPrefix(volume, `\\`) {
		return "", errors.New("chỉ dùng ổ đĩa cục bộ, không dùng thư mục mạng")
	}
	root := volume + `\`
	if samePath(target, root) {
		return "", errors.New("không dùng trực tiếp thư mục gốc của ổ đĩa")
	}
	if isInside(target, defaultPath) || isInside(defaultPath, target) {
		return "", errors.New("thư mục mới phải tách khỏi C:\\ProgramData\\Multipass")
	}
	for _, protected := range []string{os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramData")} {
		if strings.TrimSpace(protected) != "" && isInside(target, protected) {
			return "", errors.New("không lưu dữ liệu máy ảo trong thư mục hệ thống của Windows")
		}
	}
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil || windows.GetDriveType(rootPointer) != windows.DRIVE_FIXED {
		return "", errors.New("chỉ dùng ổ cứng cục bộ cố định")
	}
	if err := rejectReparseAncestors(target, root); err != nil {
		return "", err
	}
	if info, err := os.Stat(target); err == nil {
		if !info.IsDir() {
			return "", errors.New("đường dẫn đã tồn tại nhưng không phải thư mục")
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return "", fmt.Errorf("không đọc được thư mục đã chọn: %w", readErr)
		}
		if len(entries) != 0 {
			return "", errors.New("hãy chọn một thư mục trống dành riêng cho dữ liệu máy ảo")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("không kiểm tra được thư mục: %w", err)
	}
	free, err := freeBytes(target)
	if err != nil {
		return "", fmt.Errorf("không đọc được dung lượng ổ đĩa: %w", err)
	}
	if free < minimumFreeBytes {
		return "", errors.New("ổ đĩa cần còn trống ít nhất 10 GB")
	}
	return target, nil
}

func platformApplicationRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(executable)
	if strings.EqualFold(filepath.Base(directory), "bin") {
		buildDirectory := filepath.Dir(directory)
		if strings.EqualFold(filepath.Base(buildDirectory), "build") {
			return filepath.Dir(buildDirectory), nil
		}
	}
	return directory, nil
}

func readMachineStorage() (string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, machineEnvironmentKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(storageValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("MULTIPASS_STORAGE không hợp lệ: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func listInstanceCount(ctx context.Context, executable string) (int, error) {
	listCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := hostexec.CommandContext(listCtx, executable, "list", "--format", "json").CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return 0, fmt.Errorf("không đọc được danh sách máy ảo: %s", detail)
	}
	var list multipassList
	if err := json.Unmarshal(output, &list); err != nil {
		return 0, fmt.Errorf("kết quả Multipass không hợp lệ: %w", err)
	}
	return len(list.List), nil
}

func freeBytes(path string) (uint64, error) {
	volume := filepath.VolumeName(filepath.Clean(path))
	if volume == "" {
		return 0, errors.New("không xác định được ổ đĩa")
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return 0, err
	}
	var available, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(root, &available, &total, &totalFree); err != nil {
		return 0, err
	}
	return available, nil
}

func rejectReparseAncestors(target, root string) error {
	current := target
	for {
		pointer, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attributes, attributeErr := windows.GetFileAttributes(pointer)
		if attributeErr == nil && attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("không dùng junction hoặc symbolic link làm nơi lưu máy ảo")
		}
		if attributeErr != nil && !errors.Is(attributeErr, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(attributeErr, windows.ERROR_PATH_NOT_FOUND) {
			return fmt.Errorf("không kiểm tra được thuộc tính thư mục: %w", attributeErr)
		}
		if samePath(current, root) {
			break
		}
		parent := filepath.Dir(current)
		if samePath(parent, current) {
			break
		}
		current = parent
	}
	return nil
}

type helperRequest struct {
	Schema    int    `json:"schema"`
	Operation string `json:"operation"`
	Target    string `json:"target"`
	CreatedAt string `json:"createdAt"`
}

type helperResult struct {
	Schema     int    `json:"schema"`
	Success    bool   `json:"success"`
	Stage      string `json:"stage"`
	Detail     string `json:"detail"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

func runElevatedBootstrap(ctx context.Context, target string, report Reporter) error {
	localData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localData == "" {
		return errors.New("không xác định được LOCALAPPDATA")
	}
	operations := filepath.Join(localData, "OneClickDevServer", "operations")
	if err := os.MkdirAll(operations, 0o700); err != nil {
		return fmt.Errorf("không chuẩn bị được thư mục kết quả: %w", err)
	}
	temporary, err := os.MkdirTemp(operations, "storage-")
	if err != nil {
		return err
	}
	requestPath := filepath.Join(temporary, "request.json")
	resultPath := filepath.Join(temporary, "result.json")
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	request := helperRequest{Schema: helperSchema, Operation: helperOperation, Target: target, CreatedAt: createdAt}
	if err := writeJSONFile(requestPath, request); err != nil {
		return fmt.Errorf("không ghi được yêu cầu helper: %w", err)
	}
	if err := writeHelperResult(resultPath, helperResult{Schema: helperSchema, Stage: "pending", Detail: "Đang chờ UAC", StartedAt: createdAt}); err != nil {
		return err
	}
	helpExecutable, err := os.Executable()
	if err != nil {
		return err
	}
	launcher := buildStorageLauncher(helpExecutable, requestPath)
	command := hostexec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", launcher)
	type elevatedOutcome struct {
		output []byte
		err    error
	}
	completed := make(chan elevatedOutcome, 1)
	go func() {
		output, commandErr := command.CombinedOutput()
		completed <- elevatedOutcome{output: output, err: commandErr}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	percent := 34
	var output []byte
	for {
		select {
		case outcome := <-completed:
			output, err = outcome.output, outcome.err
			goto finished
		case <-ticker.C:
			progress := readHelperProgress(resultPath)
			if progress.Message != "" {
				report(progress)
			} else {
				if percent < 78 {
					percent += 4
				}
				report(Progress{Stage: "bootstrap", Message: "Windows đang khởi tạo kho dữ liệu…", Percent: percent})
			}
		case <-ctx.Done():
			return fmt.Errorf("%w\nLog: %s", ctx.Err(), resultPath)
		}
	}

finished:
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		lower := strings.ToLower(message)
		if strings.Contains(lower, "canceled") || strings.Contains(lower, "cancelled") {
			_ = writeHelperResult(resultPath, helperResult{Schema: helperSchema, Stage: "uac_cancelled", Detail: "Người dùng đã huỷ UAC", StartedAt: createdAt, FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)})
			return fmt.Errorf("bạn đã huỷ yêu cầu quyền quản trị\nLog: %s", resultPath)
		}
		return fmt.Errorf("%s\nLog: %s", message, resultPath)
	}
	encoded, err := os.ReadFile(resultPath)
	if err != nil {
		return fmt.Errorf("không đọc được kết quả helper Windows: %w", err)
	}
	var result helperResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return fmt.Errorf("kết quả helper Windows không hợp lệ: %w", err)
	}
	if !result.Success {
		detail := strings.TrimSpace(result.Detail)
		if detail == "" || result.Stage == "pending" {
			detail = "helper kết thúc nhưng không trả về kết quả; mã: " + strings.TrimSpace(string(output))
		}
		return fmt.Errorf("%s\nLog: %s", detail, resultPath)
	}
	return nil
}

func platformRunElevatedHelper(requestPath string) int {
	requestPath, err := validateHelperJournalPath(requestPath, "request.json")
	if err != nil {
		return 1
	}
	resultPath := filepath.Join(filepath.Dir(requestPath), "result.json")
	if _, err := validateHelperJournalPath(resultPath, "result.json"); err != nil {
		return 1
	}
	request, err := readHelperRequest(requestPath)
	if err != nil {
		_ = writeHelperResult(resultPath, helperResult{Schema: helperSchema, Stage: "request_failed", Detail: cleanHelperDetail(err.Error()), FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return 1
	}
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	journal := func(stage string) error {
		return writeHelperResult(resultPath, helperResult{Schema: helperSchema, Stage: stage, Detail: stage, StartedAt: startedAt})
	}
	err = elevatedBootstrap(request.Target, journal)
	result := helperResult{Schema: helperSchema, Success: err == nil, Stage: "complete", Detail: "Đã thiết lập MULTIPASS_STORAGE", StartedAt: startedAt, FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err != nil {
		result.Stage = "failed"
		result.Detail = cleanHelperDetail(err.Error())
	}
	if writeErr := writeHelperResult(resultPath, result); writeErr != nil {
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}

func elevatedBootstrap(target string, journal func(string) error) error {
	if journal == nil {
		journal = func(string) error { return nil }
	}
	if err := journal("validating"); err != nil {
		return fmt.Errorf("không ghi được nhật ký helper: %w", err)
	}
	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	defaultPath := filepath.Join(programData, "Multipass")
	cleanTarget, err := validateTarget(target, defaultPath)
	if err != nil {
		return err
	}
	executable, err := multipass.Find()
	if err != nil {
		return err
	}
	instances, err := listInstanceCount(context.Background(), executable)
	if err != nil {
		return err
	}
	if instances != 0 {
		return fmt.Errorf("đã có %d máy ảo; vị trí dữ liệu đã bị khóa", instances)
	}
	if err := journal("preflight_ready"); err != nil {
		return err
	}
	if _, err := os.Stat(defaultPath); err != nil {
		return fmt.Errorf("không đọc được kho Multipass mặc định: %w", err)
	}
	if err := os.MkdirAll(cleanTarget, 0o700); err != nil {
		return fmt.Errorf("không tạo được thư mục đích: %w", err)
	}
	if err := rejectReparseAncestors(cleanTarget, filepath.VolumeName(cleanTarget)+`\`); err != nil {
		return err
	}
	if entries, err := os.ReadDir(cleanTarget); err != nil || len(entries) != 0 {
		if err != nil {
			return fmt.Errorf("không đọc được thư mục đích: %w", err)
		}
		return errors.New("thư mục đích không còn trống")
	}

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("không kết nối được Windows Service Manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService("Multipass")
	if err != nil {
		return fmt.Errorf("không mở được dịch vụ Multipass: %w", err)
	}
	defer service.Close()
	if err := journal("stopping_service"); err != nil {
		return err
	}
	if err := stopService(service, 60*time.Second); err != nil {
		return err
	}

	registryWritten := false
	rollback := func(cause error) error {
		_ = journal("rolling_back")
		_ = stopService(service, 30*time.Second)
		if registryWritten {
			_ = deleteMachineStorage()
		}
		_ = startService(service, 60*time.Second)
		return cause
	}
	if err := journal("copying_baseline"); err != nil {
		return rollback(err)
	}
	if err := copyBaseline(defaultPath, cleanTarget); err != nil {
		return rollback(fmt.Errorf("không sao chép được baseline Multipass: %w", err))
	}
	if err := journal("writing_registry"); err != nil {
		return rollback(err)
	}
	if err := setMachineStorage(cleanTarget); err != nil {
		return rollback(fmt.Errorf("không ghi được MULTIPASS_STORAGE: %w", err))
	}
	registryWritten = true
	if err := journal("starting_service"); err != nil {
		return rollback(err)
	}
	if err := startService(service, 60*time.Second); err != nil {
		return rollback(err)
	}
	postflightCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := journal("postflight"); err != nil {
		return rollback(err)
	}
	instances, err = listInstanceCount(postflightCtx, executable)
	if err != nil {
		return rollback(fmt.Errorf("Multipass không phản hồi tại vị trí mới: %w", err))
	}
	if instances != 0 {
		return rollback(errors.New("kho dữ liệu mới không ở trạng thái sạch"))
	}
	return nil
}

func stopService(service *mgr.Service, timeout time.Duration) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("không đọc được trạng thái dịch vụ Multipass: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if status.State != svc.StopPending {
		if _, err := service.Control(svc.Stop); err != nil {
			return fmt.Errorf("không dừng được dịch vụ Multipass: %w", err)
		}
	}
	return waitService(service, svc.Stopped, timeout)
}

func startService(service *mgr.Service, timeout time.Duration) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("không đọc được trạng thái dịch vụ Multipass: %w", err)
	}
	if status.State == svc.Running {
		return nil
	}
	if status.State != svc.StartPending {
		if err := service.Start(); err != nil {
			return fmt.Errorf("không khởi động được dịch vụ Multipass: %w", err)
		}
	}
	return waitService(service, svc.Running, timeout)
}

func waitService(service *mgr.Service, wanted svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == wanted {
			return nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("dịch vụ Multipass không chuyển sang trạng thái %d trong thời gian cho phép", wanted)
}

func copyBaseline(source, target string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyBaselineEntry(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyBaselineEntry(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	pointer, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(pointer)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("baseline chứa reparse point không được phép: %s", filepath.Base(source))
	}
	if info.IsDir() {
		if err := os.Mkdir(target, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyBaselineEntry(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("baseline chứa file không hợp lệ: %s", filepath.Base(source))
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func setMachineStorage(target string) error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, machineEnvironmentKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(storageValueName, target)
}

func deleteMachineStorage() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, machineEnvironmentKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(storageValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func buildStorageLauncher(executable, requestPath string) string {
	return fmt.Sprintf(`$request=%s; $arguments='--oneclick-storage-helper "' + $request + '"'; $process=Start-Process -FilePath %s -ArgumentList $arguments -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ErrorAction Stop; $process.ExitCode`, powerShellLiteral(requestPath), powerShellLiteral(executable))
}

func powerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func readHelperProgress(path string) Progress {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Progress{}
	}
	var result helperResult
	if json.Unmarshal(encoded, &result) != nil {
		return Progress{}
	}
	stages := map[string]Progress{
		"pending":          {Stage: "permission", Message: "Đang chờ xác nhận của Windows…", Percent: 24},
		"validating":       {Stage: "bootstrap", Message: "Đang kiểm tra lại thư mục…", Percent: 32},
		"preflight_ready":  {Stage: "bootstrap", Message: "Đã xác minh kho dữ liệu sạch…", Percent: 40},
		"stopping_service": {Stage: "bootstrap", Message: "Đang dừng dịch vụ Multipass…", Percent: 48},
		"copying_baseline": {Stage: "bootstrap", Message: "Đang khởi tạo dữ liệu trên ổ mới…", Percent: 60},
		"writing_registry": {Stage: "bootstrap", Message: "Đang lưu vị trí dữ liệu…", Percent: 72},
		"starting_service": {Stage: "bootstrap", Message: "Đang khởi động lại Multipass…", Percent: 80},
		"postflight":       {Stage: "bootstrap", Message: "Đang xác minh Multipass tại ổ mới…", Percent: 88},
		"rolling_back":     {Stage: "rollback", Message: "Đang khôi phục cấu hình an toàn…", Percent: 76},
		"complete":         {Stage: "bootstrap", Message: "Kho dữ liệu mới đã sẵn sàng…", Percent: 92},
		"failed":           {Stage: "bootstrap", Message: "Helper đã dừng và lưu log lỗi…", Percent: 90},
		"request_failed":   {Stage: "bootstrap", Message: "Yêu cầu helper không hợp lệ…", Percent: 28},
		"uac_cancelled":    {Stage: "permission", Message: "Đã huỷ xác nhận Windows", Percent: 24},
	}
	return stages[result.Stage]
}

func readHelperRequest(path string) (helperRequest, error) {
	file, err := os.Open(path)
	if err != nil {
		return helperRequest{}, err
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
	if err != nil {
		return helperRequest{}, err
	}
	if len(encoded) > 16*1024 {
		return helperRequest{}, errors.New("request.json vượt quá giới hạn")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var request helperRequest
	if err := decoder.Decode(&request); err != nil {
		return helperRequest{}, fmt.Errorf("request.json không hợp lệ: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return helperRequest{}, errors.New("request.json có dữ liệu thừa")
	}
	if request.Schema != helperSchema || request.Operation != helperOperation || strings.TrimSpace(request.Target) == "" || strings.TrimSpace(request.CreatedAt) == "" {
		return helperRequest{}, errors.New("nội dung request.json không được hỗ trợ")
	}
	return request, nil
}

func validateHelperJournalPath(value, expectedName string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil || !strings.EqualFold(filepath.Base(absolute), expectedName) {
		return "", errors.New("đường dẫn nhật ký helper không hợp lệ")
	}
	operation := filepath.Dir(absolute)
	if !strings.HasPrefix(strings.ToLower(filepath.Base(operation)), "storage-") || !strings.EqualFold(filepath.Base(filepath.Dir(operation)), "operations") || !strings.EqualFold(filepath.Base(filepath.Dir(filepath.Dir(operation))), "OneClickDevServer") {
		return "", errors.New("thư mục nhật ký helper không hợp lệ")
	}
	volume := filepath.VolumeName(absolute)
	if volume == "" || strings.HasPrefix(volume, `\\`) {
		return "", errors.New("nhật ký helper phải nằm trên ổ cục bộ")
	}
	if err := rejectReparseAncestors(operation, volume+`\`); err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return "", errors.New("file nhật ký helper không hợp lệ")
	}
	return absolute, nil
}

func writeHelperResult(path string, result helperResult) error {
	return writeJSONFile(path, result)
}

func writeJSONFile(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func cleanHelperDetail(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1600 {
		value = value[:1600]
	}
	return value
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func isInside(path, parent string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
