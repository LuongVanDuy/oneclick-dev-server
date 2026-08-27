package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"oneclick-dev-server/internal/multipass"
)

var safeSnapshotToken = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
var safeGuestSnapshot = regexp.MustCompile(`^/var/lib/oneclick/(?:staging|deployments)/[a-f0-9]{16,64}/[a-f0-9]{16,64}$`)

// TransferSnapshot copies one verified archive into the dedicated OneClick VM.
// It never mounts the host project and never executes project code.
func TransferSnapshot(parent context.Context, request SnapshotTransfer, report Reporter) TransferResult {
	if !safeSnapshotToken.MatchString(request.ProjectID) || !safeSnapshotToken.MatchString(request.SnapshotID) ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(request.Checksum) {
		return transferFailed("Thông tin snapshot không hợp lệ", errors.New("snapshot identity failed validation"))
	}
	archivePath, err := filepath.Abs(request.ArchivePath)
	if err != nil {
		return transferFailed("Không tìm thấy bản sao tạm", err)
	}
	archiveInfo, err := os.Stat(archivePath)
	if err != nil || !archiveInfo.Mode().IsRegular() {
		if err == nil {
			err = errors.New("snapshot archive is not a regular file")
		}
		return transferFailed("Không tìm thấy bản sao tạm", err)
	}

	plan, err := makePlan(Request{ProjectPath: request.ProjectPath, ProjectName: request.ProjectName})
	if err != nil {
		return transferFailed("Không xác định được máy ảo của website", err)
	}
	executable, err := multipass.Find()
	if err != nil {
		return transferFailed("Không tìm thấy Multipass", err)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()

	emit(report, "verify", "Đang kiểm tra lại máy ảo…", 5)
	if err := verifySnapshotTarget(ctx, executable, plan.VMName); err != nil {
		return transferFailed("Máy ảo chưa sẵn sàng để nhận website", err)
	}

	incomingDir := "/home/ubuntu/.oneclick/incoming"
	guestArchive := incomingDir + "/" + request.Checksum + ".tar.gz"
	staging := "/var/lib/oneclick/staging/" + request.ProjectID + "/" + request.SnapshotID
	final := "/var/lib/oneclick/deployments/" + request.ProjectID + "/" + request.SnapshotID
	currentDir := "/var/lib/oneclick/projects/" + request.ProjectID
	current := currentDir + "/current"
	if !safeGuestSnapshotPath(staging) || !safeGuestSnapshotPath(final) {
		return transferFailed("Đường dẫn snapshot không hợp lệ", errors.New("guest path failed validation"))
	}
	if existingChecksum, existingErr := execInVM(ctx, executable, plan.VMName, "sudo", "cat", final+"/.oneclick-snapshot-sha256"); existingErr == nil {
		if strings.TrimSpace(existingChecksum) != request.Checksum {
			return transferFailed("Snapshot trùng tên nhưng không hợp lệ", errors.New("existing snapshot checksum mismatch"))
		}
		emit(report, "commit", "Bản sao đã có, đang xác minh lại…", 96)
		if err := activateSnapshot(ctx, executable, plan.VMName, final, currentDir, current, request.Checksum); err != nil {
			return transferFailed("Không xác minh được snapshot hiện hành", err)
		}
		emit(report, "complete", "Website đã được sao chép an toàn", 100)
		return TransferResult{Success: true, Message: "Website đã được sao chép an toàn", GuestPath: final}
	}

	if _, err := execInVM(ctx, executable, plan.VMName, "mkdir", "-p", incomingDir); err != nil {
		return transferFailed("Không chuẩn bị được nơi nhận file", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = execInVM(cleanupCtx, executable, plan.VMName, "rm", "-f", guestArchive)
	}()

	emit(report, "transfer", "Đang chuyển bản sao vào máy ảo…", 60)
	if _, err := runWithProgress(ctx, executable, []string{"transfer", archivePath, plan.VMName + ":" + guestArchive}, report, "transfer", 60, 84, "Đang chuyển bản sao vào máy ảo…"); err != nil {
		return transferFailed("Không sao chép được website vào máy ảo", err)
	}

	emit(report, "checksum", "Đang kiểm tra bản sao đã nhận…", 86)
	checksumOutput, err := execInVM(ctx, executable, plan.VMName, "sha256sum", guestArchive)
	checksumFields := strings.Fields(checksumOutput)
	if err != nil || len(checksumFields) == 0 || !strings.EqualFold(checksumFields[0], request.Checksum) {
		if err == nil {
			err = errors.New("checksum trong máy ảo không khớp")
		}
		return transferFailed("Bản sao bị thay đổi trong lúc chuyển", err)
	}

	if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "mkdir", "-p", staging); err != nil {
		return transferFailed("Không tạo được thư mục snapshot", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = execInVM(cleanupCtx, executable, plan.VMName, "sudo", "rm", "-rf", "--", staging)
	}()

	emit(report, "extract", "Đang mở bản sao trong vùng cách ly…", 90)
	if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "tar", "--extract", "--gzip", "--file", guestArchive, "--directory", staging, "--no-same-owner", "--no-same-permissions"); err != nil {
		return transferFailed("Không mở được bản sao trong máy ảo", err)
	}
	if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "test", "-f", staging+"/.oneclick-manifest.json"); err != nil {
		return transferFailed("Snapshot thiếu manifest xác minh", err)
	}
	markerCommand := fmt.Sprintf("umask 022; printf '%%s\\n' %s > %s/.oneclick-snapshot-sha256", request.Checksum, staging)
	if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "sh", "-c", markerCommand); err != nil {
		return transferFailed("Không ghi được dấu xác minh snapshot", err)
	}
	if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "chmod", "-R", "a-w", staging); err != nil {
		return transferFailed("Không khoá được snapshot", err)
	}

	emit(report, "commit", "Đang hoàn tất bản sao bất biến…", 96)
	if existingChecksum, existingErr := execInVM(ctx, executable, plan.VMName, "sudo", "cat", final+"/.oneclick-snapshot-sha256"); existingErr == nil {
		if strings.TrimSpace(existingChecksum) != request.Checksum {
			return transferFailed("Snapshot trùng tên nhưng không hợp lệ", errors.New("existing snapshot checksum mismatch"))
		}
		if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "rm", "-rf", "--", staging); err != nil {
			return transferFailed("Không dọn được bản sao tạm", err)
		}
	} else {
		if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "mkdir", "-p", pathpkg.Dir(final)); err != nil {
			return transferFailed("Không tạo được kho snapshot", err)
		}
		if _, err := execInVM(ctx, executable, plan.VMName, "sudo", "mv", "--", staging, final); err != nil {
			return transferFailed("Không chốt được snapshot", err)
		}
	}
	committed = true
	if err := activateSnapshot(ctx, executable, plan.VMName, final, currentDir, current, request.Checksum); err != nil {
		return transferFailed("Không xác minh được snapshot hiện hành", err)
	}

	emit(report, "complete", "Website đã được sao chép an toàn", 100)
	return TransferResult{Success: true, Message: "Website đã được sao chép an toàn", GuestPath: final}
}

func activateSnapshot(ctx context.Context, executable, vmName, final, currentDir, current, checksum string) error {
	if _, err := execInVM(ctx, executable, vmName, "sudo", "mkdir", "-p", currentDir); err != nil {
		return err
	}
	if _, err := execInVM(ctx, executable, vmName, "sudo", "ln", "--symbolic", "--force", "--no-dereference", final, current); err != nil {
		return err
	}
	marker, err := execInVM(ctx, executable, vmName, "sudo", "cat", current+"/.oneclick-snapshot-sha256")
	if err != nil {
		return err
	}
	if strings.TrimSpace(marker) != checksum {
		return errors.New("snapshot hiện hành không khớp checksum")
	}
	return nil
}

func verifySnapshotTarget(ctx context.Context, executable, name string) error {
	marker, err := waitForExec(ctx, executable, name, "/etc/oneclick-environment", 55*time.Second)
	if err != nil || !strings.Contains(marker, "owner=oneclick") || !strings.Contains(marker, "image=24.04") || !strings.Contains(marker, "role=shared-server") {
		if err == nil {
			err = errors.New("ownership marker không hợp lệ")
		}
		return err
	}
	info, err := inspectInstance(ctx, executable, name)
	if err != nil {
		return err
	}
	if !strings.EqualFold(info.State, "Running") {
		return fmt.Errorf("máy ảo đang ở trạng thái %s", info.State)
	}
	if hasMounts(info.Mounts) {
		return errors.New("máy ảo có host mount; từ chối sao chép để giữ cách ly")
	}
	osRelease, err := execInVM(ctx, executable, name, "cat", "/etc/os-release")
	if err != nil || !strings.Contains(osRelease, "ID=ubuntu") || !strings.Contains(osRelease, `VERSION_ID="24.04"`) {
		if err == nil {
			err = errors.New("yêu cầu Ubuntu 24.04")
		}
		return err
	}
	architecture, err := execInVM(ctx, executable, name, "uname", "-m")
	if err != nil || (strings.TrimSpace(architecture) != "x86_64" && strings.TrimSpace(architecture) != "amd64") {
		if err == nil {
			err = fmt.Errorf("kiến trúc không hỗ trợ: %s", strings.TrimSpace(architecture))
		}
		return err
	}
	return nil
}

func safeGuestSnapshotPath(path string) bool {
	return safeGuestSnapshot.MatchString(path)
}

func transferFailed(message string, err error) TransferResult {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return TransferResult{Message: message, Detail: detail}
}
