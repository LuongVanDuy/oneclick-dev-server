package secrets

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/zalando/go-keyring"
)

const keyringService = "OneClick Dev Server"

func cloudflareAccount(zone string) string {
	return "cloudflare-api-token:" + strings.ToLower(strings.TrimSpace(zone))
}

func recoveryAccount(projectID string) (string, error) {
	projectID = strings.ToLower(strings.TrimSpace(projectID))
	if len(projectID) != 24 {
		return "", errors.New("mã website khôi phục không hợp lệ")
	}
	for _, char := range projectID {
		if !(char >= 'a' && char <= 'f') && !(char >= '0' && char <= '9') {
			return "", errors.New("mã website khôi phục không hợp lệ")
		}
	}
	return "recovery-ftp-password:" + projectID, nil
}

func embeddedRecoveryAccount(projectName string) (string, error) {
	projectName = strings.ToLower(strings.TrimSpace(projectName))
	if projectName == "" || len(projectName) > 128 {
		return "", errors.New("tên dự án WP Clean Rebuild không hợp lệ")
	}
	for _, char := range projectName {
		if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && char != '.' && char != '_' && char != '-' {
			return "", errors.New("tên dự án WP Clean Rebuild không hợp lệ")
		}
	}
	return "wpclean-ftp-password:" + projectName, nil
}

// SaveCloudflareToken stores the API token in the signed-in user's native
// credential vault. The token is never written to state.json or application
// logs.
func SaveCloudflareToken(zone, token string) error {
	zone = strings.ToLower(strings.TrimSpace(zone))
	token = strings.TrimSpace(token)
	if zone == "" {
		return errors.New("tên domain trống")
	}
	if len(token) < 32 || len(token) > 2048 {
		return errors.New("Cloudflare API token không hợp lệ")
	}
	for _, char := range token {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return errors.New("Cloudflare API token chứa ký tự không hợp lệ")
		}
	}
	if err := keyring.Set(keyringService, cloudflareAccount(zone), token); err != nil {
		return fmt.Errorf("không lưu được token vào kho bí mật hệ điều hành: %w", err)
	}
	return nil
}

func GetCloudflareToken(zone string) (string, error) {
	value, err := keyring.Get(keyringService, cloudflareAccount(zone))
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errors.New("chưa lưu Cloudflare API token")
		}
		return "", fmt.Errorf("không đọc được token từ kho bí mật hệ điều hành: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("Cloudflare API token trong kho bí mật bị trống")
	}
	return value, nil
}

func DeleteCloudflareToken(zone string) error {
	err := keyring.Delete(keyringService, cloudflareAccount(zone))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// SaveRecoveryPassword keeps the remote password in the signed-in user's
// native credential vault. It is never written to recovery.json, process
// arguments, environment variables or application logs.
func SaveRecoveryPassword(projectID, password string) error {
	account, err := recoveryAccount(projectID)
	if err != nil {
		return err
	}
	if len(password) == 0 || len(password) > 4096 {
		return errors.New("mật khẩu FTP/FTPS phải có từ 1 đến 4096 ký tự")
	}
	for _, char := range password {
		if unicode.IsControl(char) {
			return errors.New("mật khẩu FTP/FTPS chứa ký tự điều khiển không được hỗ trợ")
		}
	}
	if err := keyring.Set(keyringService, account, password); err != nil {
		return fmt.Errorf("không lưu được mật khẩu vào kho bí mật hệ điều hành: %w", err)
	}
	return nil
}

func GetRecoveryPassword(projectID string) (string, error) {
	account, err := recoveryAccount(projectID)
	if err != nil {
		return "", err
	}
	value, err := keyring.Get(keyringService, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errors.New("chưa lưu mật khẩu FTP/FTPS")
		}
		return "", fmt.Errorf("không đọc được mật khẩu từ kho bí mật hệ điều hành: %w", err)
	}
	if value == "" {
		return "", errors.New("mật khẩu FTP/FTPS trong kho bí mật bị trống")
	}
	return value, nil
}

func HasRecoveryPassword(projectID string) bool {
	_, err := GetRecoveryPassword(projectID)
	return err == nil
}

func DeleteRecoveryPassword(projectID string) error {
	account, err := recoveryAccount(projectID)
	if err != nil {
		return err
	}
	err = keyring.Delete(keyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("không xóa được mật khẩu khỏi kho bí mật hệ điều hành: %w", err)
	}
	return nil
}

// SaveEmbeddedRecoveryPassword stores credentials used by the complete
// wp-clean-rebuild engine embedded in OneClick. The project JSON keeps only
// non-secret connection metadata.
func SaveEmbeddedRecoveryPassword(projectName, password string) error {
	account, err := embeddedRecoveryAccount(projectName)
	if err != nil {
		return err
	}
	if len(password) == 0 || len(password) > 4096 {
		return errors.New("mật khẩu FTP/FTPS phải có từ 1 đến 4096 ký tự")
	}
	for _, char := range password {
		if unicode.IsControl(char) {
			return errors.New("mật khẩu FTP/FTPS chứa ký tự điều khiển không được hỗ trợ")
		}
	}
	if err := keyring.Set(keyringService, account, password); err != nil {
		return fmt.Errorf("không lưu được mật khẩu WP Clean Rebuild vào kho bí mật hệ điều hành: %w", err)
	}
	return nil
}

func GetEmbeddedRecoveryPassword(projectName string) (string, error) {
	account, err := embeddedRecoveryAccount(projectName)
	if err != nil {
		return "", err
	}
	value, err := keyring.Get(keyringService, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errors.New("chưa lưu mật khẩu WP Clean Rebuild")
		}
		return "", fmt.Errorf("không đọc được mật khẩu WP Clean Rebuild từ kho bí mật hệ điều hành: %w", err)
	}
	if value == "" {
		return "", errors.New("mật khẩu WP Clean Rebuild trong kho bí mật bị trống")
	}
	return value, nil
}

func DeleteEmbeddedRecoveryPassword(projectName string) error {
	account, err := embeddedRecoveryAccount(projectName)
	if err != nil {
		return err
	}
	err = keyring.Delete(keyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("không xóa được mật khẩu WP Clean Rebuild: %w", err)
	}
	return nil
}
