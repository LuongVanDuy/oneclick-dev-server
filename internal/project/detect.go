package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxComposerBytes = 1024 * 1024

// Detect inspects only well-known files in the selected directory. It does not
// execute project code, recurse through the project or modify any file.
func Detect(path string) (Info, error) {
	if strings.TrimSpace(path) == "" {
		return Info{}, errors.New("chưa chọn thư mục website")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Info{}, fmt.Errorf("đường dẫn không hợp lệ: %w", err)
	}
	absolute = filepath.Clean(absolute)
	stat, err := os.Stat(absolute)
	if err != nil {
		return Info{}, fmt.Errorf("không mở được thư mục: %w", err)
	}
	if !stat.IsDir() {
		return Info{}, errors.New("đường dẫn đã chọn không phải thư mục")
	}

	info := Info{
		Path:         absolute,
		Name:         filepath.Base(absolute),
		Kind:         "unknown",
		KindLabel:    "Chưa nhận diện",
		DocumentRoot: ".",
		Warnings:     []string{},
	}

	hasComposer := isFile(filepath.Join(absolute, "composer.json"))
	if hasComposer {
		info.PHPRequirement = readPHPRequirement(filepath.Join(absolute, "composer.json"))
	}

	switch {
	case hasComposer && isFile(filepath.Join(absolute, "artisan")):
		info.Kind, info.KindLabel, info.DocumentRoot, info.Ready = "laravel", "Laravel", "public", true
	case isFile(filepath.Join(absolute, "wp-config.php")) || isFile(filepath.Join(absolute, "wp-config-sample.php")):
		info.Kind, info.KindLabel, info.Ready = "wordpress", "WordPress", true
	case hasComposer && isFile(filepath.Join(absolute, "public", "index.php")):
		info.Kind, info.KindLabel, info.DocumentRoot, info.Ready = "php-framework", "PHP Framework", "public", true
	case isFile(filepath.Join(absolute, "index.php")):
		info.Kind, info.KindLabel, info.Ready = "php", "PHP", true
	case isFile(filepath.Join(absolute, "index.html")) || isFile(filepath.Join(absolute, "index.htm")):
		info.Kind, info.KindLabel, info.Ready = "static", "Website tĩnh", true
	default:
		info.Warnings = append(info.Warnings, "Không tìm thấy file khởi động; hãy chọn loại website và thư mục chạy.")
	}

	if info.DocumentRoot == "public" && !isDirectory(filepath.Join(absolute, "public")) {
		info.Ready = false
		info.Warnings = append(info.Warnings, "Không tìm thấy thư mục public.")
	}
	return info, nil
}

func isFile(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func isDirectory(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

func readPHPRequirement(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxComposerBytes))
	if err != nil {
		return ""
	}
	var composer struct {
		Require map[string]string `json:"require"`
	}
	if json.Unmarshal(data, &composer) != nil {
		return ""
	}
	return strings.TrimSpace(composer.Require["php"])
}
