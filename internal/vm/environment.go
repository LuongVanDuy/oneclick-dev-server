package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"oneclick-dev-server/internal/hostexec"
	"oneclick-dev-server/internal/multipass"
	"oneclick-dev-server/internal/project"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	imageName     = "24.04"
	sharedVMName  = "oneclick-server"
	defaultCPUs   = 2
	defaultRAM    = "4G"
	defaultDisk   = "40G"
	createTimeout = 20 * time.Minute
	// A clean Ubuntu/VirtualBox boot on the Windows reference host completed
	// cloud-init at 2m25s. Allow five minutes for SSH before classifying a real
	// host networking/boot failure, while still staying far below the CLI's
	// 15-minute launch timeout.
	// Stop holding the employee UI at a fake percentage for the CLI's full
	// 15-minute timeout and return a clear recovery action instead.
	launchNetworkTimeout = 5 * time.Minute
)

var createLock sync.Mutex
var ansiSequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var spinnerNoise = regexp.MustCompile(`(?:[\\|/\-]){8,}`)
var errLaunchNetworkTimeout = errors.New("máy ảo đã bật nhưng chưa có địa chỉ IP hoặc kết nối SSH sau 5 phút")

const cloudInit = `#cloud-config
timezone: Etc/UTC
package_update: false
package_upgrade: false
write_files:
  - path: /etc/oneclick-environment
    owner: root:root
    content: |
      schema=2
      owner=oneclick
      image=24.04
      role=shared-server
`

type listedInstance struct {
	Name    string   `json:"name"`
	State   string   `json:"state"`
	IPv4    []string `json:"ipv4"`
	Release string   `json:"release"`
}

type listResponse struct {
	List []listedInstance `json:"list"`
}

type infoRecord struct {
	State    string          `json:"state"`
	Release  string          `json:"release"`
	IPv4     []string        `json:"ipv4"`
	CPUCount flexibleInt     `json:"cpu_count"`
	Mounts   json.RawMessage `json:"mounts"`
}

type flexibleInt int

func (value *flexibleInt) UnmarshalJSON(raw []byte) error {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		*value = 0
		return nil
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return err
	}
	*value = flexibleInt(parsed)
	return nil
}

// GetPlan returns the fixed, reviewable VM operation. It performs only a
// read-only Multipass list when Multipass is available.
func GetPlan(request Request) (Plan, error) {
	plan, err := makePlan(request)
	if err != nil {
		return Plan{}, err
	}

	executable, err := multipass.Find()
	if err != nil {
		return Plan{}, errors.New("Multipass chưa được cài đặt hoặc không tìm thấy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	backend, err := multipass.InspectBackend(ctx, executable)
	if err != nil {
		return Plan{}, err
	}
	if !backend.Ready {
		if backend.RebootRequired {
			return Plan{}, errors.New(backend.Detail)
		}
		return Plan{}, errors.New("cần cài VirtualBox trong màn Kiểm tra máy trước khi tạo môi trường: " + backend.Detail)
	}
	instances, err := waitForService(ctx, executable, 30*time.Second)
	if err != nil {
		return Plan{}, fmt.Errorf("Multipass chưa sẵn sàng: %w", err)
	}
	_, plan.Existing = findInstance(instances, plan.VMName)
	return plan, nil
}

// Create creates or reuses the single deterministic OneClick server. Project files
// are not mounted, copied or executed in this operation.
func Create(parent context.Context, request Request, report Reporter) Result {
	if !createLock.TryLock() {
		return Result{Message: "Đang có một môi trường khác được tạo", Detail: "Chờ thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer createLock.Unlock()
	return create(parent, request, report)
}

func create(parent context.Context, request Request, report Reporter) Result {
	plan, err := makePlan(request)
	if err != nil {
		return failed("Không thể tạo môi trường", err)
	}
	executable, err := multipass.Find()
	if err != nil {
		return failed("Không tìm thấy Multipass", err)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, createTimeout)
	defer cancel()
	backend, err := multipass.InspectBackend(ctx, executable)
	if err != nil {
		return failed("Không kiểm tra được driver máy ảo", err)
	}
	if !backend.Ready {
		if backend.RebootRequired {
			return Result{RebootRequired: true, Message: "Cần khởi động lại Windows", Detail: backend.Detail}
		}
		return failed("Thiếu VirtualBox", errors.New("quay lại Kiểm tra máy và bấm Cài đặt"))
	}
	emit(report, "service", "Đang kiểm tra dịch vụ máy ảo…", 5)
	instances, err := waitForService(ctx, executable, 60*time.Second)
	if err != nil {
		return failed("Multipass chưa sẵn sàng", err)
	}

	emit(report, "inspect", "Đang kiểm tra môi trường hiện có…", 12)
	instance, exists := findInstance(instances, plan.VMName)
	reused := exists

	if !exists {
		cloudInitPath, cleanup, err := writeCloudInit()
		if err != nil {
			return failed("Không chuẩn bị được cấu hình máy ảo", err)
		}
		defer cleanup()

		emit(report, "launch", "Đang tạo Ubuntu 24.04 — lần đầu có thể mất vài phút…", 20)
		_, err = runLaunchWithProgress(ctx, executable, []string{
			"launch", imageName,
			"--name", plan.VMName,
			"--cpus", fmt.Sprint(defaultCPUs),
			"--memory", defaultRAM,
			"--disk", defaultDisk,
			"--timeout", "900",
			"--cloud-init", cloudInitPath,
		}, plan.VMName, report)
		if err != nil {
			if errors.Is(err, errLaunchNetworkTimeout) {
				return launchNetworkFailure(runtime.GOOS, backend.Driver, plan.VMName)
			}
			return failedRecoverable("Tạo máy ảo không thành công", err, plan.VMName, "Starting")
		}
	} else if !strings.EqualFold(instance.State, "Running") {
		emit(report, "start", "Đang khởi động môi trường đã có…", 35)
		if strings.EqualFold(instance.State, "Starting") {
			if err = waitForRunning(ctx, executable, plan.VMName, 30*time.Second, report); err != nil {
				return failedRecoverable("Máy ảo bị kẹt khi khởi động", err, plan.VMName, instance.State)
			}
		} else {
			_, err = runWithProgress(ctx, executable, []string{"start", "--timeout", "180", plan.VMName}, report, "start", 35, 68, "Đang khởi động môi trường đã có…")
			if err != nil {
				return failedRecoverable("Không khởi động được máy ảo", err, plan.VMName, instance.State)
			}
		}
	}

	emit(report, "health", "Đang xác minh môi trường cách ly…", 74)
	marker := ""
	if exists && strings.EqualFold(instance.State, "Running") && !hasUsableIP(instance.IPv4) {
		emit(report, "health", "Máy ảo không có IP, đang kiểm tra kết nối lần cuối…", 76)
		probeCtx, probeCancel := context.WithTimeout(ctx, 12*time.Second)
		marker, err = execInVM(probeCtx, executable, plan.VMName, "cat", "/etc/oneclick-environment")
		probeCancel()
		if err != nil {
			return failedRecoverable(
				"Máy ảo đang chạy nhưng mất kết nối",
				errors.New("máy ảo không có địa chỉ IP và SSH không phản hồi; hãy tạo lại môi trường lỗi"),
				plan.VMName,
				instance.State,
			)
		}
	}
	if marker == "" {
		marker, err = waitForExec(ctx, executable, plan.VMName, "/etc/oneclick-environment", 90*time.Second)
	}
	if err != nil {
		probeCtx, probeCancel := context.WithTimeout(ctx, 15*time.Second)
		currentInstances, probeErr := listInstances(probeCtx, executable)
		probeCancel()
		if probeErr == nil {
			current, found := findInstance(currentInstances, plan.VMName)
			if found && !hasUsableIP(current.IPv4) {
				return failedRecoverable(
					"Máy ảo đang chạy nhưng mất kết nối",
					errors.New("máy ảo không có địa chỉ IP và SSH không phản hồi; hãy tạo lại môi trường lỗi"),
					plan.VMName,
					current.State,
				)
			}
		}
		if err != nil {
			return failed("Máy ảo chưa vượt qua kiểm tra an toàn", err)
		}
	}
	if !strings.Contains(marker, "owner=oneclick") || !strings.Contains(marker, "image=24.04") || !strings.Contains(marker, "role=shared-server") {
		return failed("Máy ảo trùng tên nhưng không thuộc OneClick", errors.New("OneClick sẽ không sử dụng hoặc xoá máy ảo này"))
	}
	if !reused {
		emit(report, "health", "Đang chờ Ubuntu hoàn tất khởi tạo…", 82)
		if _, err = waitForExec(ctx, executable, plan.VMName, "/var/lib/cloud/instance/boot-finished", 8*time.Minute); err != nil {
			return failed("Ubuntu chưa hoàn tất khởi tạo", err)
		}
	}

	osRelease, err := execInVM(ctx, executable, plan.VMName, "cat", "/etc/os-release")
	if err != nil || !strings.Contains(osRelease, "ID=ubuntu") || !strings.Contains(osRelease, `VERSION_ID="24.04"`) {
		if err == nil {
			err = errors.New("yêu cầu Ubuntu 24.04")
		}
		return failed("Hệ điều hành máy ảo không hợp lệ", err)
	}
	architecture, err := execInVM(ctx, executable, plan.VMName, "uname", "-m")
	if err != nil || (strings.TrimSpace(architecture) != "x86_64" && strings.TrimSpace(architecture) != "amd64") {
		if err == nil {
			err = fmt.Errorf("kiến trúc không hỗ trợ: %s", strings.TrimSpace(architecture))
		}
		return failed("Kiến trúc máy ảo không hợp lệ", err)
	}

	emit(report, "verify", "Đang kiểm tra cấu hình cuối…", 88)
	info, err := inspectInstance(ctx, executable, plan.VMName)
	if err != nil {
		return failed("Không đọc được cấu hình máy ảo", err)
	}
	if !strings.EqualFold(info.State, "Running") {
		return failed("Máy ảo chưa chạy", fmt.Errorf("trạng thái hiện tại: %s", info.State))
	}
	if int(info.CPUCount) > 0 && int(info.CPUCount) < defaultCPUs {
		return failed("Máy ảo không đủ tài nguyên", fmt.Errorf("cần tối thiểu %d CPU", defaultCPUs))
	}
	if hasMounts(info.Mounts) {
		return failed("Máy ảo có thư mục máy chính đang được gắn", errors.New("hãy tháo mount trước khi tiếp tục để giữ cách ly"))
	}

	emit(report, "complete", "Môi trường an toàn đã sẵn sàng", 100)
	return Result{
		Success: true,
		Reused:  reused,
		Message: "Môi trường an toàn đã sẵn sàng",
		VMName:  plan.VMName,
		State:   info.State,
		IP:      firstIP(info.IPv4),
		Release: info.Release,
	}
}

// Recreate permanently removes only the deterministic OneClick instance and
// then creates it again. A Running instance is eligible only when it has no
// usable IP and an independent SSH probe also fails. The desktop UI requires
// a separate, explicit destructive confirmation before calling this method.
func Recreate(parent context.Context, request Request, report Reporter) Result {
	if !createLock.TryLock() {
		return Result{Message: "Đang có một môi trường khác được xử lý", Detail: "Chờ thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer createLock.Unlock()

	plan, err := makePlan(request)
	if err != nil {
		return failed("Không thể xác định môi trường cần tạo lại", err)
	}
	executable, err := multipass.Find()
	if err != nil {
		return failed("Không tìm thấy Multipass", err)
	}
	if parent == nil {
		parent = context.Background()
	}
	resetCtx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	instances, err := waitForService(resetCtx, executable, 30*time.Second)
	if err != nil {
		return failed("Multipass chưa sẵn sàng", err)
	}
	instance, exists := findInstance(instances, plan.VMName)
	if exists {
		if strings.EqualFold(instance.State, "Running") {
			probeCtx, probeCancel := context.WithTimeout(resetCtx, 12*time.Second)
			_, probeErr := execInVM(probeCtx, executable, plan.VMName, "true")
			probeCancel()
			if !runningInstanceCanBeRecreated(instance, probeErr) {
				return failed("Không xoá môi trường đang hoạt động", errors.New("máy ảo vẫn có IP hoặc SSH còn phản hồi; OneClick từ chối xoá để bảo vệ dữ liệu"))
			}
		}
		emit(report, "reset", "Đang dọn môi trường lỗi…", 8)
		if !strings.EqualFold(instance.State, "Stopped") && !strings.EqualFold(instance.State, "Deleted") {
			stopCtx, stopCancel := context.WithTimeout(resetCtx, 60*time.Second)
			_, err = runWithProgress(stopCtx, executable, []string{"stop", "--force", plan.VMName}, report, "reset", 8, 18, "Đang dừng môi trường lỗi…")
			stopCancel()
			if err != nil {
				return failed("Không dừng được môi trường lỗi", errors.New("khởi động lại Windows rồi thử Tạo lại từ đầu"))
			}
		}
		deleteCtx, deleteCancel := context.WithTimeout(resetCtx, 90*time.Second)
		_, err = runWithProgress(deleteCtx, executable, []string{"delete", "--purge", plan.VMName}, report, "reset", 18, 28, "Đang xoá môi trường lỗi…")
		deleteCancel()
		if err != nil {
			return failed("Không xoá được môi trường lỗi", err)
		}
	}
	return create(parent, request, report)
}

func makePlan(request Request) (Plan, error) {
	info, err := project.Detect(request.ProjectPath)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		VMName:    instanceName(info.Path, info.Name),
		Image:     "Ubuntu 24.04 LTS",
		CPUs:      defaultCPUs,
		Memory:    "4 GB",
		Disk:      "40 GB (cấp phát động)",
		Network:   "NAT mặc định (không bridge LAN)",
		Isolation: "Một Ubuntu dùng chung; mỗi website có user, PHP sandbox và database riêng",
	}, nil
}

func instanceName(projectPath, projectName string) string {
	return sharedVMName
}

func listInstances(ctx context.Context, executable string) ([]listedInstance, error) {
	output, err := run(ctx, executable, "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var response listResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return nil, fmt.Errorf("kết quả Multipass không hợp lệ: %w", err)
	}
	if response.List == nil {
		response.List = []listedInstance{}
	}
	return response.List, nil
}

// waitForService uses `list`, which is available in the pinned Multipass
// 1.16.3 release and also proves that the daemon can answer requests. Newer
// Multipass versions may offer `wait-ready`, but 1.16.3 does not.
func waitForService(ctx context.Context, executable string, timeout time.Duration) ([]listedInstance, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		instances, err := listInstances(ctx, executable)
		if err == nil {
			return instances, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}

		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func findInstance(instances []listedInstance, name string) (listedInstance, bool) {
	for _, instance := range instances {
		if instance.Name == name {
			return instance, true
		}
	}
	return listedInstance{}, false
}

func inspectInstance(ctx context.Context, executable, name string) (infoRecord, error) {
	output, err := run(ctx, executable, "info", name, "--format", "json")
	if err != nil {
		return infoRecord{}, err
	}
	return parseInstanceInfo(output, name)
}

func parseInstanceInfo(output, name string) (infoRecord, error) {
	var envelope struct {
		Info map[string]json.RawMessage `json:"info"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		return infoRecord{}, fmt.Errorf("kết quả info không hợp lệ: %w", err)
	}
	raw, ok := envelope.Info[name]
	if !ok {
		return infoRecord{}, errors.New("không tìm thấy thông tin máy ảo vừa tạo")
	}
	var record infoRecord
	if err := json.Unmarshal(raw, &record); err == nil {
		return record, nil
	}
	var records []infoRecord
	if err := json.Unmarshal(raw, &records); err != nil || len(records) == 0 {
		return infoRecord{}, errors.New("cấu trúc thông tin máy ảo không hợp lệ")
	}
	return records[0], nil
}

func execInVM(ctx context.Context, executable, name string, command ...string) (string, error) {
	args := []string{"exec", "--no-map-working-directory", name, "--"}
	args = append(args, command...)
	return run(ctx, executable, args...)
}

func waitForExec(ctx context.Context, executable, name, path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		attemptTimeout := 8 * time.Second
		if remaining := time.Until(deadline); remaining < attemptTimeout {
			attemptTimeout = remaining
		}
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		output, err := execInVM(attemptCtx, executable, name, "cat", path)
		cancel()
		if err == nil {
			return output, nil
		}
		lastErr = err
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", fmt.Errorf("máy ảo chưa phản hồi: %w", lastErr)
}

func waitForRunning(ctx context.Context, executable, name string, timeout time.Duration, report Reporter) error {
	deadline := time.Now().Add(timeout)
	percent := 35
	for time.Now().Before(deadline) {
		instances, err := listInstances(ctx, executable)
		if err == nil {
			instance, exists := findInstance(instances, name)
			if !exists {
				return errors.New("không còn tìm thấy máy ảo")
			}
			if strings.EqualFold(instance.State, "Running") {
				return nil
			}
			if !strings.EqualFold(instance.State, "Starting") {
				return fmt.Errorf("trạng thái hiện tại: %s", instance.State)
			}
		}
		if percent < 64 {
			percent += 3
		}
		emit(report, "start", "Máy ảo đang khởi động — đang chờ địa chỉ mạng…", percent)
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("máy ảo giữ trạng thái Starting quá 30 giây và chưa có địa chỉ IP; có thể tạo lại môi trường lỗi")
}

func writeCloudInit() (string, func(), error) {
	file, err := os.CreateTemp("", "oneclick-cloud-init-*.yaml")
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
	if _, err := file.WriteString(cloudInit); err != nil {
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

func run(ctx context.Context, executable string, args ...string) (string, error) {
	output, err := hostexec.CommandContext(ctx, executable, args...).CombinedOutput()
	text := cleanProcessOutput(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", errors.New(text)
	}
	return text, nil
}

func cleanProcessOutput(value string) string {
	value = ansiSequence.ReplaceAllString(value, "")
	value = spinnerNoise.ReplaceAllString(value, "")
	var builder strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || (!unicode.IsControl(character) && character != unicode.ReplacementChar) {
			builder.WriteRune(character)
		}
	}
	return strings.TrimSpace(builder.String())
}

func runWithProgress(ctx context.Context, executable string, args []string, report Reporter, stage string, start, end int, message string) (string, error) {
	type outcome struct {
		output string
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		output, err := run(ctx, executable, args...)
		completed <- outcome{output: output, err: err}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	percent := start
	for {
		select {
		case result := <-completed:
			return result.output, result.err
		case <-ticker.C:
			if percent < end-1 {
				percent += 2
				emit(report, stage, message, percent)
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// runLaunchWithProgress distinguishes a slow first image download from a VM
// that has already powered on but cannot obtain networking/SSH. Multipass's
// launch command remains the source of truth; list is only a read-only health
// signal used to avoid leaving the desktop at 68% for up to 15 minutes.
func runLaunchWithProgress(ctx context.Context, executable string, args []string, name string, report Reporter) (string, error) {
	type outcome struct {
		output string
		err    error
	}
	launchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	completed := make(chan outcome, 1)
	go func() {
		output, err := run(launchCtx, executable, args...)
		completed <- outcome{output: output, err: err}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	percent := 20
	var networkPendingSince time.Time
	for {
		select {
		case result := <-completed:
			return result.output, result.err
		case <-ticker.C:
			message := "Đang tải và chuẩn bị Ubuntu 24.04…"
			probeCtx, probeCancel := context.WithTimeout(ctx, 4*time.Second)
			instances, probeErr := listInstances(probeCtx, executable)
			probeCancel()
			if probeErr == nil {
				instance, exists := findInstance(instances, name)
				if networkPending(instance, exists) {
					sshCtx, sshCancel := context.WithTimeout(ctx, 4*time.Second)
					_, sshErr := execInVM(sshCtx, executable, name, "true")
					sshCancel()
					if sshErr == nil {
						networkPendingSince = time.Time{}
						message = "SSH đã sẵn sàng, đang hoàn tất Ubuntu…"
						if percent < 88 {
							percent += 3
						}
						emit(report, "launch", message, percent)
						continue
					}
					if networkPendingSince.IsZero() {
						networkPendingSince = time.Now()
					}
					message = "Máy ảo đã bật, đang chờ kết nối mạng…"
					if time.Since(networkPendingSince) >= launchNetworkTimeout {
						cancel()
						return "", errLaunchNetworkTimeout
					}
					if percent < 82 {
						percent += 3
					}
				} else if exists && hasUsableIP(instance.IPv4) {
					networkPendingSince = time.Time{}
					message = "Đã có kết nối mạng, đang hoàn tất Ubuntu…"
					if percent < 88 {
						percent += 3
					}
				} else {
					networkPendingSince = time.Time{}
					if percent < 50 {
						percent += 2
					}
				}
			}
			emit(report, "launch", message, percent)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func networkPending(instance listedInstance, exists bool) bool {
	if !exists || hasUsableIP(instance.IPv4) {
		return false
	}
	return strings.EqualFold(instance.State, "Starting") || strings.EqualFold(instance.State, "Running")
}

func runningInstanceCanBeRecreated(instance listedInstance, sshProbeErr error) bool {
	return strings.EqualFold(instance.State, "Running") &&
		!hasUsableIP(instance.IPv4) &&
		sshProbeErr != nil
}

func hasUsableIP(values []string) bool {
	return firstIP(values) != ""
}

func launchNetworkFailure(goos, driver, vmName string) Result {
	_ = goos
	_ = driver
	return failedRecoverable("Máy ảo chưa nhận được kết nối mạng", errLaunchNetworkTimeout, vmName, "Starting")
}

func hasMounts(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil && object != nil {
		return len(object) > 0
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil && list != nil {
		return len(list) > 0
	}
	// Unknown or malformed mount data is treated as mounted (fail closed).
	return true
}

func firstIP(values []string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" && !strings.EqualFold(trimmed, "N/A") {
			return trimmed
		}
	}
	return ""
}

func emit(report Reporter, stage, message string, percent int) {
	if report != nil {
		report(Progress{Stage: stage, Message: message, Percent: percent})
	}
}

func failed(message string, err error) Result {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return Result{Message: message, Detail: detail}
}

func failedRecoverable(message string, err error, vmName, state string) Result {
	result := failed(message, err)
	result.CanRecreate = true
	result.VMName = vmName
	result.State = state
	return result
}
