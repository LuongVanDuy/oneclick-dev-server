//go:build windows

package installer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"oneclick-dev-server/internal/hostexec"
	"oneclick-dev-server/internal/multipass"
)

const (
	multipassVersion      = "1.16.3"
	multipassDownloadURL  = "https://github.com/canonical/multipass/releases/download/v1.16.3/multipass-1.16.3+win-win64.msi"
	virtualBoxVersion     = "7.1.18"
	virtualBoxDownloadURL = "https://download.virtualbox.org/virtualbox/7.1.18/VirtualBox-7.1.18-173720-Win.exe"
	virtualBoxSHA256      = "de14e4d6572e5a602e5053f3fd8c641356fd377ef9baa813136ae32711d19488"
	virtualBoxLauncher    = `$arguments='-NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand ' + $env:ONECLICK_VBOX_COMMAND; $process=Start-Process -FilePath "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -ArgumentList $arguments -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ErrorAction Stop; $process.ExitCode`
)

func platformPlan(action string) Plan {
	if action == ActionInstallVirtualBox {
		return Plan{
			Action: action, Title: "Cài đặt VirtualBox",
			Description: "Gỡ các bản VirtualBox xung đột rồi cài sạch bản tương thích; không xóa máy ảo, có thể gián đoạn mạng trong chốc lát.",
			Publisher:   "Oracle Corporation", Version: virtualBoxVersion,
			Source: "Máy chủ chính thức download.virtualbox.org", DownloadSize: "Khoảng 120 MB",
			RequiresAdmin: true, RebootPossible: true, Supported: true,
		}
	}
	title := "Cài đặt Multipass"
	if action == ActionRepairMultipass {
		title = "Sửa cài đặt Multipass"
	}
	return Plan{
		Action:         action,
		Title:          title,
		Description:    "Tạo máy ảo cách ly website khỏi máy chính.",
		Publisher:      "Canonical Group Limited",
		Version:        multipassVersion,
		Source:         "GitHub chính thức của Canonical",
		DownloadSize:   "Khoảng 65 MB",
		RequiresAdmin:  true,
		RebootPossible: true,
		Supported:      true,
	}
}

func platformRun(ctx context.Context, action string, report Reporter) Result {
	if action == ActionInstallVirtualBox {
		return installVirtualBox(ctx, report)
	}
	return installMultipass(ctx, report)
}

func installMultipass(ctx context.Context, report Reporter) Result {
	installCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "oneclick-multipass-")
	if err != nil {
		return failed("Không tạo được thư mục tạm", err)
	}
	defer os.RemoveAll(tempDir)

	msiPath := filepath.Join(tempDir, "multipass.msi")
	report(Progress{Stage: "download", Message: "Đang tải Multipass…", Percent: 3})
	if err := downloadArtifact(installCtx, msiPath, multipassDownloadURL, "Multipass", report, isTrustedMultipassDownloadURL); err != nil {
		return failed("Tải Multipass không thành công", err)
	}

	report(Progress{Stage: "verify", Message: "Đang kiểm tra chữ ký Canonical…", Percent: 74})
	if err := verifyAuthenticode(installCtx, msiPath, "canonical"); err != nil {
		return failed("Gói cài đặt không hợp lệ", err)
	}

	report(Progress{Stage: "permission", Message: "Hãy xác nhận cửa sổ của Windows…", Percent: 82})
	exitCode, err := runElevatedMSI(installCtx, msiPath)
	if err != nil {
		return failed("Không thể chạy trình cài đặt", err)
	}

	switch exitCode {
	case 0:
		report(Progress{Stage: "complete", Message: "Đã cài đặt Multipass", Percent: 100})
		return Result{Success: true, Message: "Cài đặt thành công"}
	case 1641, 3010:
		report(Progress{Stage: "complete", Message: "Đã cài đặt, cần khởi động lại", Percent: 100})
		return Result{Success: true, RebootRequired: true, Message: "Cài đặt thành công", Detail: "Khởi động lại Windows để hoàn tất."}
	default:
		return Result{Message: "Cài đặt không thành công", Detail: fmt.Sprintf("Windows Installer trả về mã %d.", exitCode)}
	}
}

func installVirtualBox(ctx context.Context, report Reporter) Result {
	installCtx, cancel := context.WithTimeout(ctx, 25*time.Minute)
	defer cancel()
	tempDir, err := os.MkdirTemp("", "oneclick-virtualbox-")
	if err != nil {
		return failed("Không tạo được thư mục tạm", err)
	}
	defer os.RemoveAll(tempDir)
	installerPath := filepath.Join(tempDir, "virtualbox.exe")
	report(Progress{Stage: "download", Message: "Đang tải VirtualBox…", Percent: 3})
	if err := downloadArtifact(installCtx, installerPath, virtualBoxDownloadURL, "VirtualBox", report, isTrustedVirtualBoxDownloadURL); err != nil {
		return failed("Tải VirtualBox không thành công", err)
	}
	report(Progress{Stage: "verify", Message: "Đang kiểm tra checksum và chữ ký Oracle…", Percent: 74})
	if err := verifySHA256(installerPath, virtualBoxSHA256); err != nil {
		return failed("Checksum VirtualBox không hợp lệ", err)
	}
	if err := verifyAuthenticode(installCtx, installerPath, "oracle"); err != nil {
		return failed("Chữ ký VirtualBox không hợp lệ", err)
	}
	report(Progress{Stage: "permission", Message: "Hãy xác nhận cửa sổ của Windows…", Percent: 82})
	exitCode, err := runElevatedVirtualBoxWithProgress(installCtx, installerPath, filepath.Join(tempDir, "result.txt"), report)
	if err != nil {
		return failed("Không thể cài VirtualBox", err)
	}
	if exitCode != 0 && exitCode != 1641 && exitCode != 3010 {
		return Result{Message: "Cài đặt không thành công", Detail: fmt.Sprintf("VirtualBox Installer trả về mã %d.", exitCode)}
	}

	multipassPath, findErr := multipass.Find()
	if findErr == nil {
		verifyCtx, verifyCancel := context.WithTimeout(installCtx, 15*time.Second)
		backend, backendErr := multipass.InspectBackend(verifyCtx, multipassPath)
		verifyCancel()
		if backendErr != nil || !backend.Ready {
			return Result{Success: true, RebootRequired: true, Message: "Đã cài VirtualBox", Detail: "Khởi động lại Windows rồi bấm Kiểm tra lại để Multipass nhận driver."}
		}
	}
	report(Progress{Stage: "complete", Message: "Đã cài đặt VirtualBox — cần khởi động lại", Percent: 100})
	return Result{
		Success:        true,
		RebootRequired: true,
		Message:        "Cài đặt VirtualBox thành công",
		Detail:         "Khởi động lại Windows trước khi tạo môi trường để hoàn tất driver máy ảo và mạng.",
	}
}

func runElevatedVirtualBoxWithProgress(ctx context.Context, installerPath, resultPath string, report Reporter) (int, error) {
	type outcome struct {
		exitCode int
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		exitCode, err := runElevatedVirtualBox(ctx, installerPath, resultPath)
		completed <- outcome{exitCode: exitCode, err: err}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	percent := 84
	for {
		select {
		case result := <-completed:
			return result.exitCode, result.err
		case <-ticker.C:
			if percent < 96 {
				percent += 2
			}
			report(Progress{Stage: "install", Message: "Windows đang cài VirtualBox…", Percent: percent})
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

func downloadArtifact(ctx context.Context, destination, sourceURL, label string, report Reporter, trusted func(*url.URL) bool) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: 15 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("quá nhiều lần chuyển hướng")
			}
			if !trusted(req.URL) {
				return fmt.Errorf("nguồn tải xuống không được phép: %s", req.URL.Hostname())
			}
			return nil
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("máy chủ trả về HTTP %d", response.StatusCode)
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	buffer := make([]byte, 128*1024)
	var written int64
	lastPercent := 3
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if _, err := file.Write(buffer[:count]); err != nil {
				return err
			}
			written += int64(count)
			if response.ContentLength > 0 {
				percent := 3 + int(float64(written)/float64(response.ContentLength)*67)
				if percent > 70 {
					percent = 70
				}
				if percent > lastPercent {
					lastPercent = percent
					report(Progress{Stage: "download", Message: "Đang tải " + label + "…", Percent: percent})
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if written == 0 {
		return errors.New("tệp tải xuống bị trống")
	}
	return file.Sync()
}

func isTrustedDownloadURL(value *url.URL) bool {
	return isTrustedMultipassDownloadURL(value)
}

func isTrustedMultipassDownloadURL(value *url.URL) bool {
	if value == nil || !strings.EqualFold(value.Scheme, "https") {
		return false
	}
	host := strings.ToLower(value.Hostname())
	return host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func isTrustedVirtualBoxDownloadURL(value *url.URL) bool {
	return value != nil && strings.EqualFold(value.Scheme, "https") && strings.EqualFold(value.Hostname(), "download.virtualbox.org")
}

func verifyAuthenticode(ctx context.Context, path, expectedPublisher string) error {
	script := `$env:PSModulePath = "$env:ProgramFiles\WindowsPowerShell\Modules;$env:SystemRoot\system32\WindowsPowerShell\v1.0\Modules"; Import-Module Microsoft.PowerShell.Security -ErrorAction Stop; $signature = Get-AuthenticodeSignature -LiteralPath $env:ONECLICK_INSTALLER_FILE; $subject = ''; if ($null -ne $signature.SignerCertificate) { $subject = $signature.SignerCertificate.Subject }; [pscustomobject]@{Status=[string]$signature.Status; Subject=$subject} | ConvertTo-Json -Compress`
	command := hostexec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	command.Env = append(os.Environ(), "ONECLICK_INSTALLER_FILE="+path)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("không đọc được chữ ký: %s", cleanCommandError(output, err))
	}
	var signature struct {
		Status  string `json:"Status"`
		Subject string `json:"Subject"`
	}
	if err := json.Unmarshal(output, &signature); err != nil {
		return fmt.Errorf("kết quả chữ ký không hợp lệ: %w (%s)", err, cleanCommandError(output, err))
	}
	if !strings.EqualFold(signature.Status, "Valid") {
		return fmt.Errorf("trạng thái chữ ký: %s", signature.Status)
	}
	if !strings.Contains(strings.ToLower(signature.Subject), strings.ToLower(expectedPublisher)) {
		return fmt.Errorf("nhà phát hành không khớp %s: %s", expectedPublisher, signature.Subject)
	}
	return nil
}

func verifySHA256(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("SHA-256 nhận được %s", actual)
	}
	return nil
}

func runElevatedMSI(ctx context.Context, path string) (int, error) {
	// Start-Process is intentionally fixed to msiexec; only the verified temp
	// file path is passed as data. -Verb RunAs provides the standard UAC prompt.
	script := `$line = '/i "' + $env:ONECLICK_INSTALLER_MSI + '" /quiet /norestart'; $process = Start-Process -FilePath "$env:SystemRoot\System32\msiexec.exe" -ArgumentList $line -Verb RunAs -Wait -PassThru -ErrorAction Stop; $process.ExitCode`
	command := hostexec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	command.Env = append(os.Environ(), "ONECLICK_INSTALLER_MSI="+path)
	output, err := command.CombinedOutput()
	if err != nil {
		message := cleanCommandError(output, err)
		if strings.Contains(strings.ToLower(message), "canceled") || strings.Contains(strings.ToLower(message), "cancelled") {
			return 0, errors.New("bạn đã huỷ yêu cầu quyền quản trị")
		}
		return 0, errors.New(message)
	}
	exitCode, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, fmt.Errorf("không đọc được kết quả Windows Installer: %w", err)
	}
	return exitCode, nil
}

func runElevatedVirtualBox(ctx context.Context, installerPath, resultPath string) (int, error) {
	if err := os.WriteFile(resultPath, []byte("PENDING"), 0o600); err != nil {
		return 0, fmt.Errorf("không chuẩn bị được file kết quả VirtualBox: %w", err)
	}
	elevatedScript := buildVirtualBoxElevationScript(installerPath, resultPath)
	encoded := encodePowerShell(elevatedScript)
	command := hostexec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", virtualBoxLauncher)
	command.Env = append(os.Environ(), "ONECLICK_VBOX_COMMAND="+encoded)
	output, err := command.CombinedOutput()
	if err != nil {
		message := cleanCommandError(output, err)
		if strings.Contains(strings.ToLower(message), "canceled") || strings.Contains(strings.ToLower(message), "cancelled") {
			return 0, errors.New("bạn đã huỷ yêu cầu quyền quản trị")
		}
		return 0, errors.New(message)
	}
	launcherExitCode, parseErr := strconv.Atoi(strings.TrimSpace(string(output)))
	if parseErr != nil {
		return 0, fmt.Errorf("không đọc được mã kết thúc helper VirtualBox: %s", cleanCommandError(output, parseErr))
	}
	result, err := os.ReadFile(resultPath)
	if err != nil {
		return 0, fmt.Errorf("helper VirtualBox kết thúc mã %d nhưng không đọc được file kết quả: %w", launcherExitCode, err)
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(result), "\ufeff"))
	if text == "PENDING" || text == "" {
		return 0, fmt.Errorf("helper VirtualBox kết thúc mã %d nhưng chưa cập nhật kết quả", launcherExitCode)
	}
	if strings.HasPrefix(text, "ERROR:") {
		return 0, errors.New(strings.TrimSpace(strings.TrimPrefix(text, "ERROR:")))
	}
	exitCode, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("kết quả VirtualBox không hợp lệ: %s", text)
	}
	return exitCode, nil
}

func buildVirtualBoxElevationScript(installerPath, resultPath string) string {
	installer := powerShellLiteral(installerPath)
	result := powerShellLiteral(resultPath)
	expectedHash := powerShellLiteral(strings.ToUpper(virtualBoxSHA256))
	return fmt.Sprintf(`$ErrorActionPreference='Stop';
$installer=%s;
$resultPath=%s;
$recoveryPath='';
function Invoke-VBoxMsiUninstall([string]$productCode) {
  $process=Start-Process -FilePath "$env:SystemRoot\System32\msiexec.exe" -ArgumentList @('/x',$productCode,'/qn','/norestart') -Wait -PassThru -ErrorAction Stop;
  if ($process.ExitCode -notin @(0,1605,1614,1641,3010)) { throw "Windows Installer could not remove $productCode (exit $($process.ExitCode))" }
}
try {
  $hash=(Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash;
  if ($hash -ne %s) { throw 'Checksum changed before elevation' };
  $uninstallRoots=@('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall');
  $products=@();
  foreach ($root in $uninstallRoots) {
    if (Test-Path -LiteralPath $root) {
      foreach ($key in Get-ChildItem -LiteralPath $root) {
        $item=Get-ItemProperty -LiteralPath $key.PSPath -ErrorAction SilentlyContinue;
        if ($null -ne $item -and $item.DisplayName -like 'Oracle VirtualBox*' -and $item.Publisher -like '*Oracle*' -and $item.WindowsInstaller -eq 1 -and $key.PSChildName -match '^\{[0-9A-Fa-f-]{36}\}$') { $products += $key.PSChildName }
      }
    }
  }
  $products=@($products | Sort-Object -Unique);
  Stop-Service -Name Multipass -Force -ErrorAction SilentlyContinue;
  Get-Process -Name VBoxSDS,VBoxSVC,VirtualBox,VirtualBoxVM,VBoxHeadless -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue;
  foreach ($product in $products) { Invoke-VBoxMsiUninstall $product };
  $installDir=Join-Path $env:ProgramFiles 'Oracle\VirtualBox';
  if (Test-Path -LiteralPath $installDir) {
    $recoveryRoot=Join-Path $env:ProgramData 'OneClick Dev Server\recovery';
    New-Item -ItemType Directory -Path $recoveryRoot -Force | Out-Null;
    $recoveryPath=Join-Path $recoveryRoot ('VirtualBox-stale-' + [guid]::NewGuid().ToString('N'));
    Move-Item -LiteralPath $installDir -Destination $recoveryPath -ErrorAction Stop;
  }
  $args='--silent --ignore-reboot --msiparams "VBOX_INSTALLDESKTOPSHORTCUT=0 VBOX_START=0"';
  $process=Start-Process -FilePath $installer -ArgumentList $args -Wait -PassThru -ErrorAction Stop;
  if ($process.ExitCode -notin @(0,1641,3010)) { throw "VirtualBox Installer returned $($process.ExitCode)" };
  if ($recoveryPath -ne '' -and (Test-Path -LiteralPath $recoveryPath)) { Remove-Item -LiteralPath $recoveryPath -Recurse -Force };
  $service=Get-Service -Name Multipass -ErrorAction SilentlyContinue;
  if ($null -ne $service) { try { Start-Service -Name Multipass -ErrorAction Stop } catch {} };
  [System.IO.File]::WriteAllText($resultPath,[string]$process.ExitCode,[System.Text.UTF8Encoding]::new($false));
} catch {
  $recovery=if ($recoveryPath -ne '') { " Recovery files: $recoveryPath" } else { '' };
  try { [System.IO.File]::WriteAllText($resultPath,('ERROR:' + $_.Exception.Message + $recovery),[System.Text.UTF8Encoding]::new($false)) } catch {};
  exit 1;
}`, installer, result, expectedHash)
}

func powerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func encodePowerShell(script string) string {
	encodedRunes := utf16.Encode([]rune(script))
	bytes := make([]byte, len(encodedRunes)*2)
	for index, value := range encodedRunes {
		binary.LittleEndian.PutUint16(bytes[index*2:], value)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func cleanCommandError(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message != "" {
		return message
	}
	return err.Error()
}

func failed(message string, err error) Result {
	return Result{Message: message, Detail: err.Error()}
}
