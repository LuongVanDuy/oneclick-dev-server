package hostopen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Directory opens one exact, existing local directory in the operating
// system's file manager. Callers must obtain the path from trusted backend
// state; no command text is accepted.
func Directory(path string) error {
	absolute, err := ValidateDirectory(path)
	if err != nil {
		return err
	}
	return openDirectory(absolute)
}

func ValidateDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("đường dẫn thư mục trống")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("không tìm thấy thư mục: %w", err)
	}
	if !info.IsDir() || isLinkLike(info) {
		return "", errors.New("đường dẫn không phải thư mục tin cậy")
	}
	return filepath.Clean(absolute), nil
}
