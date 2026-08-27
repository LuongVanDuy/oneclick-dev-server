//go:build windows

package multipass

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func findVBoxManage() (string, error) {
	if path, err := exec.LookPath("VBoxManage.exe"); err == nil {
		return path, nil
	}
	candidates := []string{}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432")} {
		if base != "" {
			candidates = append(candidates, filepath.Join(base, "Oracle", "VirtualBox", "VBoxManage.exe"))
		}
	}
	if key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Oracle\VirtualBox`, registry.QUERY_VALUE); err == nil {
		if installDir, _, valueErr := key.GetStringValue("InstallDir"); valueErr == nil {
			candidates = append(candidates, filepath.Join(installDir, "VBoxManage.exe"))
		}
		key.Close()
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("không tìm thấy VBoxManage.exe")
}

func inspectVBoxDrivers(expected string) (vboxDriverInspection, error) {
	registrations, err := registeredVirtualBoxVersions()
	if err != nil {
		return vboxDriverInspection{}, err
	}
	if len(registrations) != 1 || !isSupportedVBoxDriverVersion(registrations[0], expected) {
		detail := "Đăng ký cài đặt VirtualBox đang thiếu hoặc xung đột; cần cài sạch VirtualBox " + expected + "."
		if len(registrations) > 0 {
			detail += " Đang đăng ký: " + strings.Join(registrations, ", ") + "."
		}
		return vboxDriverInspection{Detail: detail}, nil
	}

	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	driverDir := filepath.Join(systemRoot, "System32", "drivers")
	drivers := []string{"VBoxSup.sys", "VBoxUSBMon.sys", "VBoxNetAdp6.sys", "VBoxNetLwf.sys"}
	versions := make([]string, 0, len(drivers))
	mismatches := make([]string, 0, len(drivers))

	for _, name := range drivers {
		path := filepath.Join(driverDir, name)
		_, statErr := os.Stat(path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) && name != "VBoxSup.sys" {
				continue
			}
			if errors.Is(statErr, os.ErrNotExist) {
				return vboxDriverInspection{Detail: "Thiếu driver bắt buộc VBoxSup.sys; cần cài lại VirtualBox."}, nil
			}
			return vboxDriverInspection{}, fmt.Errorf("không đọc được %s: %w", name, statErr)
		}
		version, versionErr := windowsFileVersion(path)
		if versionErr != nil {
			return vboxDriverInspection{}, fmt.Errorf("không đọc được phiên bản %s: %w", name, versionErr)
		}
		versions = append(versions, name+" "+version)
		if !isSupportedVBoxDriverVersion(version, expected) {
			mismatches = append(mismatches, name+" "+version)
		}
	}

	pending, pendingErr := hasPendingVBoxDriverReplacement()
	if pendingErr != nil {
		return vboxDriverInspection{}, pendingErr
	}
	if pending || len(mismatches) > 0 {
		detail := "VirtualBox đã được cập nhật nhưng driver Windows chưa đồng bộ; hãy khởi động lại Windows trước khi tạo máy ảo."
		if len(mismatches) > 0 {
			detail += " Driver còn cũ: " + strings.Join(mismatches, ", ") + "."
		}
		return vboxDriverInspection{RebootRequired: true, Detail: detail}, nil
	}

	return vboxDriverInspection{Ready: true, Detail: strings.Join(versions, ", ")}, nil
}

func registeredVirtualBoxVersions() ([]string, error) {
	roots := []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	versions := []string{}
	seen := map[string]struct{}{}
	for _, rootPath := range roots {
		root, err := registry.OpenKey(registry.LOCAL_MACHINE, rootPath, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("không đọc được Windows uninstall registry: %w", err)
		}
		names, err := root.ReadSubKeyNames(-1)
		root.Close()
		if err != nil {
			return nil, fmt.Errorf("không liệt kê được Windows uninstall registry: %w", err)
		}
		for _, name := range names {
			product, openErr := registry.OpenKey(registry.LOCAL_MACHINE, rootPath+`\`+name, registry.QUERY_VALUE)
			if openErr != nil {
				continue
			}
			displayName, _, nameErr := product.GetStringValue("DisplayName")
			publisher, _, publisherErr := product.GetStringValue("Publisher")
			version, _, versionErr := product.GetStringValue("DisplayVersion")
			product.Close()
			if nameErr != nil || publisherErr != nil || versionErr != nil {
				continue
			}
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(displayName)), "oracle virtualbox") ||
				!strings.Contains(strings.ToLower(publisher), "oracle") {
				continue
			}
			identity := strings.ToLower(rootPath + `\` + name)
			if _, exists := seen[identity]; exists {
				continue
			}
			seen[identity] = struct{}{}
			versions = append(versions, strings.TrimSpace(version))
		}
	}
	return versions, nil
}

func windowsFileVersion(path string) (string, error) {
	var zero windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &zero)
	if err != nil {
		return "", err
	}
	if size == 0 {
		return "", errors.New("file không có version resource")
	}
	buffer := make([]byte, size)
	if err = windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&buffer[0])); err != nil {
		return "", err
	}
	var fixed *windows.VS_FIXEDFILEINFO
	fixedSize := uint32(unsafe.Sizeof(*fixed))
	if err = windows.VerQueryValue(unsafe.Pointer(&buffer[0]), `\`, unsafe.Pointer(&fixed), &fixedSize); err != nil {
		return "", err
	}
	if fixed == nil {
		return "", errors.New("version resource rỗng")
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		(fixed.FileVersionMS>>16)&0xffff,
		fixed.FileVersionMS&0xffff,
		(fixed.FileVersionLS>>16)&0xffff,
		fixed.FileVersionLS&0xffff,
	), nil
}

func hasPendingVBoxDriverReplacement() (bool, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager`, registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("không đọc được trạng thái restart Windows: %w", err)
	}
	defer key.Close()
	values, _, err := key.GetStringsValue("PendingFileRenameOperations")
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("không đọc được danh sách driver chờ thay thế: %w", err)
	}
	for _, value := range values {
		lower := strings.ToLower(value)
		if strings.Contains(lower, `\vbox`) && strings.HasSuffix(lower, ".sys") {
			return true, nil
		}
	}
	return false, nil
}
