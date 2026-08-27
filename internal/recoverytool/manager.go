package recoverytool

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"oneclick-dev-server/internal/datastore"
	"oneclick-dev-server/internal/hostexec"
	"oneclick-dev-server/internal/secrets"
)

type Manager struct {
	root   string
	action sync.Mutex
	mu     sync.Mutex

	command    *exec.Cmd
	commandLog *os.File
	targetURL  string
	proxy      *http.Server
	proxyURL   string
	controlKey string
	lastError  string
}

var (
	saveEmbeddedRecoveryPassword   = secrets.SaveEmbeddedRecoveryPassword
	getEmbeddedRecoveryPassword    = secrets.GetEmbeddedRecoveryPassword
	deleteEmbeddedRecoveryPassword = secrets.DeleteEmbeddedRecoveryPassword
)

func NewDefault() (*Manager, error) {
	root, err := datastore.ApplicationRoot()
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(root, "data", "tools", "wp-clean-rebuild"))
}

func New(root string) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("thư mục Khôi phục WP trống")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Manager{root: filepath.Clean(absolute)}, nil
}

func (manager *Manager) Inspect() Status {
	paths, err := ensureBundle(manager.root)
	if err != nil {
		return Status{State: "error", Message: "Không chuẩn bị được Khôi phục WP", Detail: err.Error()}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.proxyURL != "" && manager.targetURL != "" {
		return Status{
			State: "running", Message: "Khôi phục WP đang hoạt động", URL: manager.proxyURL,
			Version: paths.Version[:16], DataPath: paths.Workspace, Ready: true, Running: true,
		}
	}
	status := manager.runtimeStatus(paths)
	if status.Detail == "" && manager.lastError != "" {
		status.Detail = manager.lastError
	}
	return status
}

func (manager *Manager) Prepare(ctx context.Context, report Reporter) Status {
	if !manager.action.TryLock() {
		return Status{State: "busy", Message: "Khôi phục WP đang có thao tác khác", Detail: "Đợi thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer manager.action.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if report == nil {
		report = func(Progress) {}
	}
	report(Progress{Stage: "bundle", Message: "Đang chuẩn bị mã nguồn trong OneClick…", Percent: 8})
	paths, err := ensureBundle(manager.root)
	if err != nil {
		return Status{State: "error", Message: "Không chuẩn bị được mã nguồn", Detail: err.Error()}
	}
	uv, err := findUV()
	if err != nil {
		return Status{
			State: "setup_required", Message: "Thiếu môi trường chạy Khôi phục WP",
			Detail:  "Chưa tìm thấy uv. Cài uv từ nguồn chính thức rồi bấm lại; OneClick không dùng thư mục mã nguồn bên ngoài.",
			Version: paths.Version[:16], DataPath: paths.Workspace, SetupRequired: true,
		}
	}
	runtimeInstalled := regularFile(runtimePython(paths))
	runtimeMessage := "Đang cài Python và thư viện đã khóa phiên bản…"
	if runtimeInstalled {
		runtimeMessage = "Đang khởi động môi trường Khôi phục WP…"
	}
	report(Progress{Stage: "runtime", Message: runtimeMessage, Percent: 25})
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	command := hostexec.CommandContext(runCtx, uv, "sync", "--project", paths.Bundle, "--no-dev", "--locked")
	command.Dir = paths.Bundle
	command.Env = toolEnvironment(os.Environ(), paths)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return Status{
			State: "setup_required", Message: "Không cài được môi trường Khôi phục WP",
			Detail: cleanTechnical(output.String() + "\n" + err.Error()), Version: paths.Version[:16],
			DataPath: paths.Workspace, SetupRequired: true,
		}
	}
	report(Progress{Stage: "verify", Message: "Đang kiểm tra engine và thư viện…", Percent: 82})
	python := runtimePython(paths)
	if !regularFile(python) {
		return Status{State: "setup_required", Message: "Python đã cài nhưng chưa sẵn sàng", Detail: python, Version: paths.Version[:16], DataPath: paths.Workspace, SetupRequired: true}
	}
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 45*time.Second)
	defer verifyCancel()
	verify := hostexec.CommandContext(verifyCtx, python, "-c", "import bcrypt,paramiko,rich,typer,yaml,wpclean;print('oneclick-wpclean-ready')")
	verify.Dir = paths.Workspace
	verify.Env = toolEnvironment(os.Environ(), paths)
	verification, err := verify.CombinedOutput()
	if err != nil || !strings.Contains(string(verification), "oneclick-wpclean-ready") {
		return Status{State: "setup_required", Message: "Môi trường Khôi phục WP chưa vượt qua kiểm tra", Detail: cleanTechnical(string(verification) + "\n" + errorText(err)), Version: paths.Version[:16], DataPath: paths.Workspace, SetupRequired: true}
	}
	if err := writeAtomic(filepath.Join(paths.Runtime, "synced-version"), []byte(paths.Version+"\n"), 0o600); err != nil {
		return Status{State: "error", Message: "Đã cài nhưng chưa lưu được trạng thái", Detail: err.Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
	}
	report(Progress{Stage: "complete", Message: "Môi trường Khôi phục WP đã sẵn sàng", Percent: 100})
	return manager.runtimeStatus(paths)
}

func (manager *Manager) Start(ctx context.Context) Status {
	if !manager.action.TryLock() {
		return Status{State: "busy", Message: "Khôi phục WP đang khởi động", Detail: "Đợi vài giây rồi thử lại."}
	}
	defer manager.action.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	paths, err := ensureBundle(manager.root)
	if err != nil {
		return Status{State: "error", Message: "Không chuẩn bị được Khôi phục WP", Detail: err.Error()}
	}
	manager.mu.Lock()
	if manager.proxyURL != "" && manager.targetURL != "" {
		status := Status{State: "running", Message: "Khôi phục WP đang hoạt động", URL: manager.proxyURL, Version: paths.Version[:16], DataPath: paths.Workspace, Ready: true, Running: true}
		manager.mu.Unlock()
		return status
	}
	manager.mu.Unlock()
	status := manager.runtimeStatus(paths)
	if !status.Ready {
		return status
	}
	if err := manager.writeRuntimeSecrets(paths); err != nil {
		return Status{State: "error", Message: "Không nạp được mật khẩu đã lưu an toàn", Detail: err.Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
	}
	if target := manager.recoverExistingTarget(paths); target != "" {
		return manager.attachProxy(paths, target)
	}
	port, err := availableLoopbackPort()
	if err != nil {
		return Status{State: "error", Message: "Không tìm được cổng nội bộ", Detail: err.Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
	}
	python := runtimePython(paths)
	controlKey, err := randomControlKey()
	if err != nil {
		return Status{State: "error", Message: "Không tạo được phiên điều khiển nội bộ", Detail: err.Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
	}
	logPath := filepath.Join(paths.Workspace, "logs", "oneclick-embedded.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Status{State: "error", Message: "Không mở được log Khôi phục WP", Detail: err.Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
	}
	command := hostexec.Command(python, filepath.Join(paths.Bundle, "oneclick_entry.py"))
	command.Dir = paths.Workspace
	command.Env = append(toolEnvironment(os.Environ(), paths),
		"WPCLEAN_EMBEDDED=1",
		"WPCLEAN_PORT="+strconv.Itoa(port),
		"WPCLEAN_SECRET_DIR="+paths.SecretDir,
		"WPCLEAN_DATA_ROOT="+paths.Workspace,
		"WPCLEAN_CONTROL_TOKEN="+controlKey,
	)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		return Status{State: "error", Message: "Không khởi động được Khôi phục WP", Detail: err.Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
	}
	target := fmt.Sprintf("http://127.0.0.1:%d", port)
	manager.mu.Lock()
	manager.command = command
	manager.commandLog = logFile
	manager.targetURL = target
	manager.controlKey = controlKey
	manager.lastError = ""
	manager.mu.Unlock()
	go manager.waitForCommand(command, logFile)

	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if probeWPClean(target) {
			_ = writeServerState(paths, port, controlKey)
			return manager.attachProxy(paths, target)
		}
		manager.mu.Lock()
		running := manager.command == command
		lastError := manager.lastError
		manager.mu.Unlock()
		if !running {
			return Status{State: "error", Message: "Khôi phục WP đã dừng khi khởi động", Detail: lastError, Version: paths.Version[:16], DataPath: paths.Workspace}
		}
		select {
		case <-ctx.Done():
			_ = command.Process.Kill()
			return Status{State: "error", Message: "Đã dừng khởi động Khôi phục WP", Detail: ctx.Err().Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
		case <-time.After(250 * time.Millisecond):
		}
	}
	_ = command.Process.Kill()
	return Status{State: "error", Message: "Giao diện Khôi phục WP chưa phản hồi", Detail: "Đã chờ 35 giây. Xem data/tools/wp-clean-rebuild/workspace/logs/oneclick-embedded.log", Version: paths.Version[:16], DataPath: paths.Workspace}
}

func (manager *Manager) Close(ctx context.Context) {
	manager.mu.Lock()
	proxy := manager.proxy
	command := manager.command
	logFile := manager.commandLog
	target := manager.targetURL
	controlKey := manager.controlKey
	manager.proxy = nil
	manager.proxyURL = ""
	manager.mu.Unlock()
	if proxy != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proxy.Shutdown(shutdownCtx)
		cancel()
	}
	if !targetHasRunningJob(target) {
		if command != nil && command.Process != nil {
			_ = command.Process.Kill()
		} else {
			_ = shutdownTarget(target, controlKey)
		}
		manager.cleanupRuntimeSecrets()
	}
	if logFile != nil && command == nil {
		_ = logFile.Close()
	}
}

func (manager *Manager) runtimeStatus(paths bundlePaths) Status {
	marker, _ := os.ReadFile(filepath.Join(paths.Runtime, "synced-version"))
	pythonReady := regularFile(runtimePython(paths))
	ready := strings.TrimSpace(string(marker)) == paths.Version && pythonReady
	if ready {
		return Status{State: "ready", Message: "Khôi phục WP sẵn sàng", Version: paths.Version[:16], DataPath: paths.Workspace, Ready: true}
	}
	if pythonReady {
		return Status{
			State: "start_required", Message: "Khởi động môi trường Khôi phục WP",
			Detail:  "Python và thư viện đã có sẵn. Bấm Khởi động môi trường để nạp phiên bản Khôi phục WP mới.",
			Version: paths.Version[:16], DataPath: paths.Workspace, SetupRequired: true,
		}
	}
	detail := "Cần cài Python và thư viện một lần trong thư mục dữ liệu OneClick."
	if _, err := findUV(); err != nil {
		detail = "Chưa tìm thấy uv để cài Python và thư viện đã khóa phiên bản."
	}
	return Status{State: "setup_required", Message: "Cần chuẩn bị Khôi phục WP", Detail: detail, Version: paths.Version[:16], DataPath: paths.Workspace, SetupRequired: true}
}

func (manager *Manager) attachProxy(paths bundlePaths, target string) Status {
	proxy, proxyURL, err := startLocalProxy(target, paths.Workspace)
	if err != nil {
		return Status{State: "error", Message: "Không nhúng được giao diện Khôi phục WP", Detail: err.Error(), Version: paths.Version[:16], DataPath: paths.Workspace}
	}
	manager.mu.Lock()
	if manager.proxy != nil {
		_ = proxy.Close()
		proxyURL = manager.proxyURL
	} else {
		manager.proxy = proxy
		manager.proxyURL = proxyURL
		manager.targetURL = target
	}
	manager.mu.Unlock()
	return Status{State: "running", Message: "Khôi phục WP đang hoạt động", URL: proxyURL, Version: paths.Version[:16], DataPath: paths.Workspace, Ready: true, Running: true}
}

func (manager *Manager) waitForCommand(command *exec.Cmd, logFile *os.File) {
	err := command.Wait()
	_ = logFile.Close()
	manager.mu.Lock()
	if manager.command != command {
		manager.mu.Unlock()
		return
	}
	manager.command = nil
	manager.commandLog = nil
	manager.lastError = cleanTechnical(errorText(err))
	if manager.proxy != nil {
		_ = manager.proxy.Close()
		manager.proxy = nil
		manager.proxyURL = ""
	}
	manager.targetURL = ""
	manager.controlKey = ""
	manager.mu.Unlock()
	manager.cleanupRuntimeSecrets()
}

func (manager *Manager) recoverExistingTarget(paths bundlePaths) string {
	data, err := os.ReadFile(filepath.Join(paths.Runtime, "server-state.json"))
	if err != nil {
		return ""
	}
	var state struct {
		Port       int    `json:"port"`
		Version    string `json:"version"`
		ControlKey string `json:"controlToken"`
	}
	if json.Unmarshal(data, &state) != nil || state.Port < 1024 || state.Port > 65535 || state.Version != paths.Version || len(state.ControlKey) < 32 {
		return ""
	}
	target := fmt.Sprintf("http://127.0.0.1:%d", state.Port)
	if probeWPClean(target) {
		manager.mu.Lock()
		manager.controlKey = state.ControlKey
		manager.mu.Unlock()
		return target
	}
	return ""
}

func writeServerState(paths bundlePaths, port int, controlKey string) error {
	data, err := json.Marshal(struct {
		Port       int    `json:"port"`
		Version    string `json:"version"`
		ControlKey string `json:"controlToken"`
	}{Port: port, Version: paths.Version, ControlKey: controlKey})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(paths.Runtime, "server-state.json"), append(data, '\n'), 0o600)
}

func randomControlKey() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func shutdownTarget(target, controlKey string) bool {
	if target == "" || len(controlKey) < 32 {
		return false
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(target, "/")+"/api/oneclick/shutdown", nil)
	if err != nil {
		return false
	}
	request.Header.Set("X-OneClick-Control", controlKey)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (manager *Manager) cleanupRuntimeSecrets() {
	runtimeRoot := filepath.Join(manager.root, "runtime")
	secretDir := filepath.Join(runtimeRoot, "secrets")
	if pathWithin(manager.root, runtimeRoot) && pathWithin(runtimeRoot, secretDir) {
		_ = os.RemoveAll(secretDir)
		_ = os.Remove(filepath.Join(runtimeRoot, "server-state.json"))
	}
}

func (manager *Manager) writeRuntimeSecrets(paths bundlePaths) error {
	if !pathWithin(paths.Runtime, paths.SecretDir) {
		return errors.New("thư mục secret runtime không hợp lệ")
	}
	if err := os.RemoveAll(paths.SecretDir); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.SecretDir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(paths.Workspace, "sites"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		profilePath := filepath.Join(paths.Workspace, "sites", entry.Name())
		password, _, err := migrateProfileSecret(profilePath, name)
		if err != nil {
			return err
		}
		if password == "" {
			password, err = getEmbeddedRecoveryPassword(name)
			if err != nil {
				continue
			}
		}
		if err := writeAtomic(filepath.Join(paths.SecretDir, strings.ToLower(name)+".secret"), []byte(password), 0o600); err != nil {
			return err
		}
	}
	freshEntries, err := os.ReadDir(filepath.Join(paths.Workspace, "fresh-installs", "sites"))
	if err != nil {
		return err
	}
	for _, entry := range freshEntries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		credentialKey := "fresh-" + name
		bundle, readErr := getEmbeddedRecoveryPassword(credentialKey)
		if readErr != nil || bundle == "" {
			continue
		}
		if err := writeAtomic(filepath.Join(paths.SecretDir, strings.ToLower(credentialKey)+".secret"), []byte(bundle), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func migrateProfileSecret(path, name string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	if len(data) > 1024*1024 {
		return "", false, errors.New("file cấu hình WP Clean Rebuild vượt quá 1 MB")
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false, fmt.Errorf("cấu hình %s không hợp lệ: %w", name, err)
	}
	password, _ := raw["password"].(string)
	if password == "" {
		return "", false, nil
	}
	if err := saveEmbeddedRecoveryPassword(name, password); err != nil {
		return "", false, err
	}
	delete(raw, "password")
	sanitized, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", false, err
	}
	if err := writeAtomic(path, append(sanitized, '\n'), 0o600); err != nil {
		return "", false, err
	}
	return password, true, nil
}

func runtimePython(paths bundlePaths) string {
	if runtime.GOOS == "windows" {
		pythonw := filepath.Join(paths.Runtime, ".venv", "Scripts", "pythonw.exe")
		if regularFile(pythonw) {
			return pythonw
		}
		return filepath.Join(paths.Runtime, ".venv", "Scripts", "python.exe")
	}
	return filepath.Join(paths.Runtime, ".venv", "bin", "python")
}

func findUV() (string, error) {
	if path, err := exec.LookPath("uv"); err == nil && regularFile(path) {
		return path, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{filepath.Join(home, ".local", "bin", executableName("uv"))}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates, filepath.Join(local, "Programs", "uv", "uv.exe"))
	}
	for _, candidate := range candidates {
		if regularFile(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("uv chưa được cài")
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func toolEnvironment(base []string, paths bundlePaths) []string {
	values := map[string]string{
		"UV_PROJECT_ENVIRONMENT": filepath.Join(paths.Runtime, ".venv"),
		"UV_CACHE_DIR":           paths.RuntimeCache,
		"PYTHONPATH":             filepath.Join(paths.Bundle, "src"),
		"PYTHONUTF8":             "1",
		"PYTHONIOENCODING":       "utf-8:replace",
	}
	result := make([]string, 0, len(base)+len(values))
	for _, item := range base {
		key := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			key = item[:index]
		}
		if _, replaced := values[strings.ToUpper(key)]; replaced {
			continue
		}
		result = append(result, item)
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func availableLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && !isLinkLike(info)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".oneclick-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
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
	return replaceFile(temporaryPath, path)
}

func cleanTechnical(value string) string {
	value = strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return -1
		}
		return character
	}, value))
	characters := []rune(value)
	if len(characters) > 2400 {
		return string(characters[len(characters)-2400:])
	}
	return value
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func targetHasRunningJob(target string) bool {
	if target == "" {
		return false
	}
	client := &http.Client{Timeout: 750 * time.Millisecond}
	response, err := client.Get(strings.TrimSuffix(target, "/") + "/api/projects")
	if err == nil {
		var payload struct {
			ActiveProject string `json:"activeProject"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if decodeErr == nil && payload.ActiveProject != "" {
			return true
		}
	}
	response, err = client.Get(strings.TrimSuffix(target, "/") + "/api/fresh-installs")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var payload struct {
		Installs []struct {
			Status string `json:"status"`
			Job    *struct {
				Status string `json:"status"`
			} `json:"job"`
		} `json:"installs"`
	}
	if json.NewDecoder(response.Body).Decode(&payload) != nil {
		return false
	}
	for _, install := range payload.Installs {
		if install.Status == "running" || install.Job != nil && install.Job.Status == "running" {
			return true
		}
	}
	return false
}

func parseTarget(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.User != nil {
		return nil, errors.New("địa chỉ engine Khôi phục WP không hợp lệ")
	}
	return parsed, nil
}
