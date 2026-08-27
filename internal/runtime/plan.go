package runtimeenv

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var safeToken = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
var safeSnapshotPath = regexp.MustCompile(`^/var/lib/oneclick/deployments/[a-f0-9]{16,64}/[a-f0-9]{16,64}$`)

func GetPlan(config Config, existing bool) (Plan, error) {
	if err := validateConfig(config); err != nil {
		return Plan{}, err
	}
	lock, err := loadVersionLock()
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		ProjectPath: config.ProjectPath,
		Adapter:     "WordPress",
		Runtime:     "PHP 8.3 FPM + Nginx",
		Database:    "MariaDB dùng chung · database và tài khoản riêng cho website",
		Services:    2,
		Packages:    packageLabels(lock.Packages),
		Download:    "Chỉ cài một lần trên Ubuntu; dự án sau dùng lại",
		Resources:   "PHP riêng: 512 MB · 128 tiến trình",
		Network:     "PHP chỉ dùng Unix socket; không truy cập Internet/LAN",
		Public:      false,
		Existing:    existing,
	}, nil
}

func validateConfig(config Config) error {
	if !safeToken.MatchString(config.ProjectID) || !safeToken.MatchString(config.SnapshotID) {
		return errors.New("runtime project or snapshot identity is invalid")
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(config.SnapshotHash) {
		return errors.New("runtime snapshot checksum is invalid")
	}
	if !safeSnapshotPath.MatchString(config.SnapshotPath) {
		return errors.New("runtime snapshot path is outside the immutable deployment store")
	}
	if config.ProjectKind != "wordpress" {
		return fmt.Errorf("loại website %q chưa có bộ cài runtime an toàn; phiên bản này hỗ trợ WordPress", config.ProjectKind)
	}
	if config.PHPVersion != "" && config.PHPVersion != "auto" && config.PHPVersion != "8.3" {
		return fmt.Errorf("runtime Ubuntu dùng PHP 8.3; cấu hình hiện tại yêu cầu PHP %s", config.PHPVersion)
	}
	if strings.TrimSpace(config.ProjectPath) == "" || strings.TrimSpace(config.ProjectName) == "" || strings.TrimSpace(config.VMName) == "" {
		return errors.New("runtime project metadata is incomplete")
	}
	return nil
}

func packageLabels(packages []string) []string {
	labels := make([]string, 0, 3)
	for _, name := range []string{"nginx", "php8.3-fpm", "mariadb-server"} {
		for _, installed := range packages {
			if installed == name {
				labels = append(labels, name+" (Ubuntu signed package)")
				break
			}
		}
	}
	return labels
}
