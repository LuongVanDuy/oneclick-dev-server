package tunnel

import (
	"errors"
	"regexp"
	"strings"
)

var safeProjectID = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
var safeHostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{1,251}[a-z0-9])$`)

func GetPlan(config Config, existing bool) (Plan, error) {
	if err := validateConfig(config, false); err != nil {
		return Plan{}, err
	}
	lock, err := loadVersionLock()
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		ProjectPath: config.ProjectPath,
		Hostname:    config.Hostname,
		Provider:    "Cloudflare",
		Mode:        "Named Tunnel",
		Address:     "https://" + config.Hostname,
		Image:       "cloudflared " + lock.Binary.Version + " (đã khóa SHA-256)",
		Download:    "Cài một lần trên Ubuntu; các domain sau dùng lại binary",
		Network:     "Connector user riêng; PHP/database không có Internet",
		Exposure:    "HTTPS công khai bằng domain riêng; không mở port trên máy",
		Existing:    existing,
	}, nil
}

func validateConfig(config Config, requireToken bool) error {
	if !safeProjectID.MatchString(config.ProjectID) {
		return errors.New("tunnel project identity is invalid")
	}
	hostname := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(config.Hostname), "."))
	if len(hostname) > 253 || !safeHostname.MatchString(hostname) || !strings.Contains(hostname, ".") || strings.Contains(hostname, "..") {
		return errors.New("tunnel hostname is invalid")
	}
	if strings.TrimSpace(config.ProjectPath) == "" || strings.TrimSpace(config.ProjectName) == "" || strings.TrimSpace(config.VMName) == "" {
		return errors.New("tunnel project metadata is incomplete")
	}
	if config.RuntimeState != "running" || config.RuntimeHealth != "healthy" {
		return errors.New("runtime chưa ở trạng thái healthy")
	}
	if requireToken {
		token := strings.TrimSpace(config.TunnelToken)
		if len(token) < 32 || len(token) > 4096 || strings.ContainsAny(token, "\r\n\t ") {
			return errors.New("Cloudflare connector token không hợp lệ")
		}
	}
	return nil
}
