package recoverytool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const bundlePrefix = "tool/"

type bundlePaths struct {
	Version      string
	Bundle       string
	Workspace    string
	Runtime      string
	RuntimeCache string
	SecretDir    string
}

var (
	bundleHashOnce sync.Once
	bundleHash     string
	bundleHashErr  error
)

func embeddedVersion() (string, error) {
	bundleHashOnce.Do(func() {
		hasher := sha256.New()
		var names []string
		bundleHashErr = fs.WalkDir(embeddedTool, "tool", func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				names = append(names, path)
			}
			return nil
		})
		if bundleHashErr != nil {
			return
		}
		sort.Strings(names)
		for _, name := range names {
			data, err := embeddedTool.ReadFile(name)
			if err != nil {
				bundleHashErr = err
				return
			}
			hasher.Write([]byte(name))
			hasher.Write([]byte{0})
			hasher.Write(data)
			hasher.Write([]byte{0})
		}
		bundleHash = hex.EncodeToString(hasher.Sum(nil))
	})
	return bundleHash, bundleHashErr
}

func ensureBundle(root string) (bundlePaths, error) {
	version, err := embeddedVersion()
	if err != nil {
		return bundlePaths{}, fmt.Errorf("không đọc được mã nguồn Khôi phục WP trong OneClick: %w", err)
	}
	if len(version) < 16 {
		return bundlePaths{}, errors.New("mã phiên bản Khôi phục WP không hợp lệ")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return bundlePaths{}, err
	}
	paths := bundlePaths{
		Version:      version,
		Bundle:       filepath.Join(root, "bundles", version[:16]),
		Workspace:    filepath.Join(root, "workspace"),
		Runtime:      filepath.Join(root, "runtime"),
		RuntimeCache: filepath.Join(root, "cache", "uv"),
		SecretDir:    filepath.Join(root, "runtime", "secrets"),
	}
	for _, directory := range []string{root, filepath.Dir(paths.Bundle), paths.Workspace, paths.Runtime, paths.RuntimeCache} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return bundlePaths{}, fmt.Errorf("không tạo được thư mục Khôi phục WP: %w", err)
		}
	}
	for _, name := range []string{"sites", "backups", "reports", "repairs", "logs", ".wpclean-cache", filepath.Join("fresh-installs", "sites")} {
		if err := os.MkdirAll(filepath.Join(paths.Workspace, name), 0o700); err != nil {
			return bundlePaths{}, fmt.Errorf("không tạo được kho dữ liệu Khôi phục WP: %w", err)
		}
	}
	marker := filepath.Join(paths.Bundle, ".oneclick-bundle-version")
	if data, readErr := os.ReadFile(marker); readErr == nil && strings.TrimSpace(string(data)) == version {
		return paths, nil
	}
	if info, statErr := os.Lstat(paths.Bundle); statErr == nil {
		if info.IsDir() {
			return bundlePaths{}, errors.New("mã nguồn Khôi phục WP đã giải nén nhưng không còn nguyên vẹn; giữ nguyên thư mục lỗi để kỹ thuật kiểm tra")
		}
		return bundlePaths{}, errors.New("đường dẫn mã nguồn Khôi phục WP bị chiếm dụng")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return bundlePaths{}, statErr
	}
	temporary, err := os.MkdirTemp(filepath.Dir(paths.Bundle), ".extract-")
	if err != nil {
		return bundlePaths{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := fs.WalkDir(embeddedTool, "tool", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "tool" {
			return nil
		}
		relative := strings.TrimPrefix(path, bundlePrefix)
		if relative == path || relative == "" || filepath.IsAbs(relative) {
			return fmt.Errorf("đường dẫn bundle không hợp lệ: %s", path)
		}
		destination := filepath.Join(temporary, filepath.FromSlash(relative))
		if !pathWithin(temporary, destination) {
			return fmt.Errorf("đường dẫn bundle thoát khỏi thư mục đích: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		data, readErr := embeddedTool.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}); err != nil {
		return bundlePaths{}, fmt.Errorf("không giải nén được mã nguồn Khôi phục WP: %w", err)
	}
	if err := os.WriteFile(filepath.Join(temporary, ".oneclick-bundle-version"), []byte(version+"\n"), 0o600); err != nil {
		return bundlePaths{}, err
	}
	if err := os.Rename(temporary, paths.Bundle); err != nil {
		return bundlePaths{}, fmt.Errorf("không hoàn tất bundle Khôi phục WP: %w", err)
	}
	committed = true
	return paths, nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
