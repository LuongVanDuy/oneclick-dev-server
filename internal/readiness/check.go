package readiness

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"oneclick-dev-server/internal/hostexec"
	"oneclick-dev-server/internal/multipass"
)

const (
	minimumMemoryBytes = 8 * 1024 * 1024 * 1024
	minimumDiskBytes   = 15 * 1024 * 1024 * 1024
)

func Run(ctx context.Context) Report {
	if ctx == nil {
		ctx = context.Background()
	}

	checks := []Check{
		checkPlatform(),
		checkVirtualization(ctx),
		checkResources(),
		checkMultipass(ctx),
		checkNetwork(ctx),
		{
			ID:       "desktop-runtime",
			Title:    "Giao diện ứng dụng",
			Status:   StatusReady,
			Summary:  "Desktop runtime đang hoạt động",
			Detail:   "Ứng dụng đã hiển thị thành công bằng WebView của hệ điều hành.",
			Required: true,
		},
	}

	ready := true
	for _, check := range checks {
		if check.Required && check.Status != StatusReady {
			ready = false
			break
		}
	}

	return Report{
		Platform:  runtime.GOOS,
		Arch:      runtime.GOARCH,
		Ready:     ready,
		CheckedAt: time.Now(),
		Checks:    checks,
	}
}

func checkPlatform() Check {
	check := Check{
		ID:       "platform",
		Title:    "Hệ điều hành",
		Required: true,
	}

	if runtime.GOARCH != "amd64" {
		check.Status = StatusUnsupported
		check.Summary = "Kiến trúc máy chưa được hỗ trợ"
		check.Detail = fmt.Sprintf("Bản thử nghiệm hiện chỉ hỗ trợ x64; máy này là %s.", runtime.GOARCH)
		return check
	}

	switch runtime.GOOS {
	case "windows":
		check.Status = StatusReady
		check.Summary = "Windows x64 được hỗ trợ"
		check.Detail = "Bản MVP hỗ trợ Windows 10/11 x64. Edition và backend sẽ được xác minh ở bước hypervisor."
	case "linux":
		check.Status = StatusReady
		check.Summary = "Linux x64 được nhận diện"
		check.Detail = "Bản MVP chính thức nhắm tới Ubuntu 22.04/24.04 x64."
	default:
		check.Status = StatusUnsupported
		check.Summary = "Hệ điều hành chưa được hỗ trợ"
		check.Detail = fmt.Sprintf("Không có backend cho %s trong bản MVP.", runtime.GOOS)
	}

	return check
}

func checkResources() Check {
	memory, disk, err := platformResources()
	check := Check{
		ID:       "resources",
		Title:    "Tài nguyên máy",
		Required: true,
	}
	if err != nil {
		check.Status = StatusBroken
		check.Summary = "Không đọc được RAM hoặc ổ đĩa"
		check.Detail = err.Error()
		check.ActionLabel = "Kiểm tra lại"
		check.ActionKind = "retry"
		return check
	}

	check.Detail = fmt.Sprintf("RAM: %.1f GB · Dung lượng trống: %.1f GB", toGB(memory), toGB(disk))
	if memory < minimumMemoryBytes {
		check.Status = StatusUnsupported
		check.Summary = "Cần tối thiểu 8 GB RAM"
		return check
	}
	if disk < minimumDiskBytes {
		check.Status = StatusUserAction
		check.Summary = "Cần thêm dung lượng trống"
		check.ActionLabel = "Xem hướng dẫn"
		check.ActionKind = "free_disk"
		return check
	}

	check.Status = StatusReady
	check.Summary = "Đủ RAM và dung lượng cho deployment đầu tiên"
	return check
}

func checkMultipass(ctx context.Context) Check {
	check := Check{
		ID:       "multipass",
		Title:    "Máy ảo an toàn",
		Required: true,
	}

	path, err := multipass.Find()
	if err != nil {
		check.Status = StatusInstallable
		check.Summary = "Chưa cài Multipass"
		check.Detail = "OneClick dùng Multipass để tạo máy ảo cách ly website khỏi dữ liệu trên máy chính."
		check.ActionLabel = "Cài đặt"
		check.ActionKind = "install_multipass"
		return check
	}

	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := hostexec.CommandContext(commandCtx, path, "version").CombinedOutput()
	if err != nil {
		check.Status = StatusBroken
		check.Summary = "Multipass không phản hồi"
		check.Detail = strings.TrimSpace(string(output))
		if check.Detail == "" {
			check.Detail = err.Error()
		}
		check.ActionLabel = "Sửa cài đặt"
		check.ActionKind = "repair_multipass"
		return check
	}
	backend, err := multipass.InspectBackend(commandCtx, path)
	if err != nil {
		check.Status = StatusBroken
		check.Summary = "Không kiểm tra được driver máy ảo"
		check.Detail = err.Error()
		check.ActionLabel = "Kiểm tra lại"
		check.ActionKind = "retry"
		return check
	}
	if !backend.Ready {
		if backend.RebootRequired {
			check.Status = StatusUserAction
			check.Summary = "Cần khởi động lại Windows"
			check.Detail = backend.Detail
			check.ActionLabel = "Kiểm tra lại"
			check.ActionKind = "retry"
			return check
		}
		check.Status = StatusInstallable
		check.Summary = "Cần VirtualBox tương thích"
		check.Detail = backend.Detail
		check.ActionLabel = "Cài đặt"
		check.ActionKind = backend.MissingAction
		return check
	}

	check.Status = StatusReady
	check.Summary = "Multipass đã sẵn sàng"
	check.Detail = firstLine(string(output)) + " · " + backend.Detail
	return check
}

func checkNetwork(ctx context.Context) Check {
	check := Check{
		ID:       "network",
		Title:    "Kết nối Internet",
		Required: true,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://www.cloudflare.com/cdn-cgi/trace", nil)
	if err == nil {
		client := &http.Client{Timeout: 6 * time.Second}
		var response *http.Response
		response, err = client.Do(req)
		if response != nil {
			response.Body.Close()
		}
		if err == nil && response.StatusCode < http.StatusInternalServerError {
			check.Status = StatusReady
			check.Summary = "Có thể kết nối tới Cloudflare"
			check.Detail = fmt.Sprintf("HTTP %d", response.StatusCode)
			return check
		}
	}

	check.Status = StatusUserAction
	check.Summary = "Chưa kết nối được tới Cloudflare"
	check.Detail = "Kiểm tra Internet, proxy hoặc firewall của công ty."
	if err != nil {
		check.Detail += " Chi tiết: " + err.Error()
	}
	check.ActionLabel = "Kiểm tra lại"
	check.ActionKind = "retry"
	return check
}

func firstLine(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) == 0 {
		return "Đã cài đặt"
	}
	return strings.TrimSpace(lines[0])
}

func toGB(value uint64) float64 {
	return float64(value) / 1024 / 1024 / 1024
}
