package recoverytool

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const (
	maxLegacyFiles = 500_000
	maxLegacyBytes = int64(200 * 1024 * 1024 * 1024)
)

var legacyDirectories = []string{"sites", "backups", "reports", "repairs", "logs", ".wpclean-cache"}

type legacyEntry struct {
	Source   string
	Target   string
	Relative string
	Size     int64
	Mode     fs.FileMode
	Modified time.Time
	Profile  bool
}

func (manager *Manager) LegacyImportPlan(source string) ImportPlan {
	paths, err := ensureBundle(manager.root)
	if err != nil {
		return ImportPlan{SourcePath: source, Message: "Không chuẩn bị được OneClick", Detail: err.Error()}
	}
	plan, _, err := scanLegacySource(source, paths.Workspace)
	if err != nil {
		plan.Message = "Không đọc được dữ liệu WP Clean Rebuild cũ"
		plan.Detail = err.Error()
		return plan
	}
	if plan.Conflicts > 0 {
		plan.Ready = false
		plan.Message = "Dữ liệu đích đã có file khác nội dung"
		plan.Detail = "OneClick không tự ghi đè dữ liệu đang dùng. Hãy giữ bản hiện tại hoặc chọn đúng thư mục cũ."
		return plan
	}
	plan.Ready = true
	plan.Message = "Có thể nhập dữ liệu vào OneClick"
	return plan
}

func (manager *Manager) ImportLegacy(ctx context.Context, source string, report ImportReporter) ImportResult {
	if !manager.action.TryLock() {
		return ImportResult{Message: "Khôi phục WP đang có thao tác khác", Detail: "Đợi thao tác hiện tại hoàn tất rồi thử lại."}
	}
	defer manager.action.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if report == nil {
		report = func(ImportProgress) {}
	}
	manager.mu.Lock()
	running := manager.command != nil || manager.proxy != nil
	manager.mu.Unlock()
	if running {
		return ImportResult{Message: "Khôi phục WP đang mở", Detail: "Đóng và mở lại OneClick, sau đó nhập dữ liệu trước khi vào menu Khôi phục WP."}
	}
	paths, err := ensureBundle(manager.root)
	if err != nil {
		return ImportResult{Message: "Không chuẩn bị được OneClick", Detail: err.Error()}
	}
	plan, entries, err := scanLegacySource(source, paths.Workspace)
	if err != nil {
		return ImportResult{Message: "Không đọc được dữ liệu cũ", Detail: err.Error(), DestinationPath: paths.Workspace}
	}
	if plan.Conflicts > 0 {
		return ImportResult{Message: "Không ghi đè dữ liệu đang dùng", Detail: fmt.Sprintf("Có %d file xung đột trong OneClick.", plan.Conflicts), DestinationPath: paths.Workspace}
	}
	report(ImportProgress{Stage: "copy", Message: "Bắt đầu sao chép dữ liệu cũ…", Percent: 1, FilesTotal: plan.Files, BytesTotal: plan.Bytes})
	var copiedFiles, skipped int
	var copiedBytes, processedBytes int64
	lastReport := time.Time{}
	for index, entry := range entries {
		select {
		case <-ctx.Done():
			return ImportResult{Message: "Đã dừng nhập dữ liệu", Detail: ctx.Err().Error(), Files: copiedFiles, Bytes: copiedBytes, Skipped: skipped, DestinationPath: paths.Workspace}
		default:
		}
		wasSkipped := false
		if entry.Profile {
			wasSkipped, err = importLegacyProfile(entry)
		} else {
			wasSkipped, err = copyLegacyFile(entry)
		}
		if err != nil {
			return ImportResult{
				Message: "Nhập dữ liệu bị dừng an toàn", Detail: fmt.Sprintf("%s: %v", entry.Relative, err),
				Files: copiedFiles, Bytes: copiedBytes, Skipped: skipped, DestinationPath: paths.Workspace,
			}
		}
		processedBytes += entry.Size
		if wasSkipped {
			skipped++
		} else {
			copiedFiles++
			copiedBytes += entry.Size
		}
		now := time.Now()
		if index == len(entries)-1 || lastReport.IsZero() || now.Sub(lastReport) >= 250*time.Millisecond {
			percent := 2
			if plan.Bytes > 0 {
				percent = 2 + int(processedBytes*96/plan.Bytes)
			} else if plan.Files > 0 {
				percent = 2 + (index+1)*96/plan.Files
			}
			if percent > 98 {
				percent = 98
			}
			report(ImportProgress{
				Stage: "copy", Message: fmt.Sprintf("Đang sao chép %d/%d file…", index+1, plan.Files), Percent: percent,
				FilesDone: index + 1, FilesTotal: plan.Files, BytesDone: processedBytes, BytesTotal: plan.Bytes,
			})
			lastReport = now
		}
	}
	journal, _ := json.MarshalIndent(map[string]any{
		"schema": 1, "sourcePath": plan.SourcePath, "importedAt": time.Now().UTC().Format(time.RFC3339),
		"files": plan.Files, "bytes": plan.Bytes, "copied": copiedFiles, "skipped": skipped,
	}, "", "  ")
	if err := writeAtomic(filepath.Join(paths.Workspace, ".oneclick-import.json"), append(journal, '\n'), 0o600); err != nil {
		return ImportResult{Message: "Dữ liệu đã sao chép nhưng chưa ghi được lịch sử", Detail: err.Error(), Files: copiedFiles, Bytes: copiedBytes, Skipped: skipped, DestinationPath: paths.Workspace}
	}
	report(ImportProgress{Stage: "complete", Message: "Đã nhập dữ liệu vào OneClick", Percent: 100, FilesDone: plan.Files, FilesTotal: plan.Files, BytesDone: plan.Bytes, BytesTotal: plan.Bytes})
	return ImportResult{
		Success: true, Message: "Đã nhập dữ liệu WP Clean Rebuild", Files: copiedFiles,
		Bytes: copiedBytes, Skipped: skipped, DestinationPath: paths.Workspace,
	}
}

func scanLegacySource(source, destination string) (ImportPlan, []legacyEntry, error) {
	plan := ImportPlan{SourcePath: strings.TrimSpace(source), DestinationPath: destination}
	if plan.SourcePath == "" {
		return plan, nil, errors.New("chưa chọn thư mục WP Clean Rebuild cũ")
	}
	absolute, err := filepath.Abs(plan.SourcePath)
	if err != nil {
		return plan, nil, err
	}
	absolute = filepath.Clean(absolute)
	plan.SourcePath = absolute
	if samePath(absolute, destination) || pathWithin(absolute, destination) || pathWithin(destination, absolute) {
		return plan, nil, errors.New("thư mục cũ và kho dữ liệu OneClick không được lồng vào nhau")
	}
	rootInfo, err := os.Lstat(absolute)
	if err != nil || !rootInfo.IsDir() || isLinkLike(rootInfo) {
		return plan, nil, errors.New("thư mục cũ không tồn tại hoặc là liên kết/reparse point")
	}
	for _, marker := range []string{"pyproject.toml", filepath.Join("src", "wpclean", "gui_server.py")} {
		if !regularFile(filepath.Join(absolute, marker)) {
			return plan, nil, errors.New("thư mục đã chọn không phải WP Clean Rebuild hoàn chỉnh")
		}
	}
	var entries []legacyEntry
	for _, directoryName := range legacyDirectories {
		directory := filepath.Join(absolute, directoryName)
		info, statErr := os.Lstat(directory)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil || !info.IsDir() || isLinkLike(info) {
			return plan, nil, fmt.Errorf("%s không phải thư mục dữ liệu an toàn", directoryName)
		}
		err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relativeToDirectory, relErr := filepath.Rel(directory, path)
			if relErr != nil {
				return relErr
			}
			if relativeToDirectory == "." {
				return nil
			}
			if entry.IsDir() && strings.EqualFold(entry.Name(), "__pycache__") {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				entryInfo, infoErr := entry.Info()
				if infoErr != nil || isLinkLike(entryInfo) {
					return fmt.Errorf("từ chối link/reparse: %s", path)
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(entry.Name()), ".pyc") || strings.EqualFold(filepath.Ext(entry.Name()), ".pyo") {
				return nil
			}
			entryInfo, infoErr := entry.Info()
			if infoErr != nil || !entryInfo.Mode().IsRegular() || isLinkLike(entryInfo) {
				return fmt.Errorf("từ chối file đặc biệt/link: %s", path)
			}
			if entryInfo.Size() < 0 {
				return fmt.Errorf("dung lượng file không hợp lệ: %s", path)
			}
			relative := filepath.Join(directoryName, relativeToDirectory)
			target := filepath.Join(destination, relative)
			if !pathWithin(destination, target) {
				return errors.New("đường dẫn dữ liệu cũ thoát khỏi kho OneClick")
			}
			item := legacyEntry{
				Source: path, Target: target, Relative: relative, Size: entryInfo.Size(), Mode: entryInfo.Mode(), Modified: entryInfo.ModTime(),
				Profile: directoryName == "sites" && strings.EqualFold(filepath.Ext(entry.Name()), ".json"),
			}
			entries = append(entries, item)
			plan.Files++
			plan.Bytes += item.Size
			if item.Profile {
				plan.Profiles++
				if hasProfilePassword(path) {
					plan.Secrets++
				}
			}
			if plan.Files > maxLegacyFiles || plan.Bytes > maxLegacyBytes {
				return errors.New("dữ liệu cũ vượt giới hạn 500.000 file hoặc 200 GB")
			}
			if destinationInfo, destinationErr := os.Lstat(target); destinationErr == nil {
				plan.ExistingFiles++
				if item.Profile {
					if !profilesEquivalent(path, target) {
						plan.Conflicts++
					}
				} else if !destinationInfo.Mode().IsRegular() || isLinkLike(destinationInfo) || destinationInfo.Size() != item.Size || !filesEquivalent(path, target, item.Size) {
					plan.Conflicts++
				}
			} else if !errors.Is(destinationErr, os.ErrNotExist) {
				return destinationErr
			}
			return nil
		})
		if err != nil {
			return plan, nil, err
		}
	}
	if plan.Files == 0 {
		return plan, nil, errors.New("không tìm thấy sites, backup, report, repair, log hoặc cache để nhập")
	}
	return plan, entries, nil
}

func importLegacyProfile(entry legacyEntry) (bool, error) {
	data, err := os.ReadFile(entry.Source)
	if err != nil {
		return false, err
	}
	if len(data) > 1024*1024 {
		return false, errors.New("profile JSON vượt quá 1 MB")
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, err
	}
	name := strings.TrimSuffix(filepath.Base(entry.Target), filepath.Ext(entry.Target))
	if password, _ := raw["password"].(string); password != "" {
		if err := saveEmbeddedRecoveryPassword(name, password); err != nil {
			return false, err
		}
	}
	delete(raw, "password")
	sanitized, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return false, err
	}
	if info, statErr := os.Lstat(entry.Target); statErr == nil {
		if !info.Mode().IsRegular() || isLinkLike(info) || !profilesEquivalent(entry.Source, entry.Target) {
			return false, errors.New("profile đích đã có nội dung khác")
		}
		return true, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}
	if err := writeAtomic(entry.Target, append(sanitized, '\n'), 0o600); err != nil {
		return false, err
	}
	return false, verifySourceIdentity(entry)
}

func copyLegacyFile(entry legacyEntry) (bool, error) {
	if info, err := os.Lstat(entry.Target); err == nil {
		if info.Mode().IsRegular() && !isLinkLike(info) && info.Size() == entry.Size && filesEquivalent(entry.Source, entry.Target, entry.Size) {
			return true, nil
		}
		return false, errors.New("file đích đã tồn tại với nội dung khác")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(entry.Target), 0o700); err != nil {
		return false, err
	}
	source, err := os.Open(entry.Source)
	if err != nil {
		return false, err
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(entry.Target), ".import-*.tmp")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return false, err
	}
	written, copyErr := io.CopyBuffer(temporary, source, make([]byte, 1024*1024))
	if copyErr == nil && written != entry.Size {
		copyErr = fmt.Errorf("số byte sao chép không khớp: %d/%d", written, entry.Size)
	}
	if copyErr == nil {
		copyErr = temporary.Sync()
	}
	if closeErr := temporary.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return false, copyErr
	}
	if err := verifySourceIdentity(entry); err != nil {
		return false, err
	}
	if err := os.Chtimes(temporaryPath, entry.Modified, entry.Modified); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryPath, entry.Target); err != nil {
		return false, err
	}
	return false, nil
}

func verifySourceIdentity(entry legacyEntry) error {
	info, err := os.Lstat(entry.Source)
	if err != nil || !info.Mode().IsRegular() || isLinkLike(info) || info.Size() != entry.Size || !sameModifiedTime(info.ModTime(), entry.Modified) {
		return errors.New("file nguồn thay đổi trong lúc sao chép; hãy dừng app cũ rồi thử lại")
	}
	return nil
}

func hasProfilePassword(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1024*1024 {
		return false
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	password, _ := raw["password"].(string)
	return password != ""
}

func profilesEquivalent(source, destination string) bool {
	read := func(path string) map[string]any {
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 1024*1024 {
			return nil
		}
		var raw map[string]any
		if json.Unmarshal(data, &raw) != nil {
			return nil
		}
		delete(raw, "password")
		return raw
	}
	left, right := read(source), read(destination)
	return left != nil && right != nil && reflect.DeepEqual(left, right)
}

func filesEquivalent(source, destination string, expectedSize int64) bool {
	hashFile := func(path string) ([sha256.Size]byte, error) {
		var result [sha256.Size]byte
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || isLinkLike(info) || info.Size() != expectedSize {
			return result, errors.New("file không còn đúng nhận diện")
		}
		file, err := os.Open(path)
		if err != nil {
			return result, err
		}
		defer file.Close()
		digest := sha256.New()
		written, err := io.CopyBuffer(digest, file, make([]byte, 1024*1024))
		if err != nil || written != expectedSize {
			return result, errors.New("không đọc đủ file để so sánh")
		}
		copy(result[:], digest.Sum(nil))
		return result, nil
	}
	left, leftErr := hashFile(source)
	right, rightErr := hashFile(destination)
	return leftErr == nil && rightErr == nil && left == right
}

func sameModifiedTime(left, right time.Time) bool {
	delta := left.Sub(right)
	if delta < 0 {
		delta = -delta
	}
	return delta <= 2*time.Second
}

func samePath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(leftAbsolute), filepath.Clean(rightAbsolute))
}
