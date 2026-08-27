package recoverytool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const maxProxyBody = 16 * 1024 * 1024

type localProxy struct {
	target    *url.URL
	workspace string
	client    *http.Client
}

func startLocalProxy(targetValue, workspace string) (*http.Server, string, error) {
	target, err := parseTarget(targetValue)
	if err != nil {
		return nil, "", err
	}
	if !probeWPClean(strings.TrimSuffix(targetValue, "/")) {
		return nil, "", errors.New("engine Khôi phục WP không đúng nhận diện hoặc chưa phản hồi")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	handler := &localProxy{
		target: target, workspace: workspace,
		client: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	return server, "http://" + listener.Addr().String() + "/", nil
}

func (proxy *localProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Host == "" || request.URL == nil || request.URL.IsAbs() || request.URL.Host != "" {
		http.Error(writer, "Yêu cầu nội bộ không hợp lệ.", http.StatusBadRequest)
		return
	}
	body, err := readLimitedBody(request.Body, maxProxyBody)
	if err != nil {
		http.Error(writer, "Dữ liệu yêu cầu quá lớn.", http.StatusRequestEntityTooLarge)
		return
	}
	upstreamURL := *proxy.target
	upstreamURL.Path = request.URL.Path
	upstreamURL.RawPath = request.URL.RawPath
	upstreamURL.RawQuery = request.URL.RawQuery
	upstreamRequest, err := http.NewRequestWithContext(request.Context(), request.Method, upstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(writer, "Không tạo được yêu cầu nội bộ.", http.StatusBadGateway)
		return
	}
	copyRequestHeaders(upstreamRequest.Header, request.Header)
	response, err := proxy.client.Do(upstreamRequest)
	if err != nil {
		http.Error(writer, "Engine Khôi phục WP chưa phản hồi.", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	responseBody, err := readLimitedBody(response.Body, maxProxyBody)
	if err != nil {
		http.Error(writer, "Phản hồi Khôi phục WP quá lớn.", http.StatusBadGateway)
		return
	}
	if request.Method == http.MethodPost && strings.TrimSuffix(request.URL.Path, "/") == "/api/projects" && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := proxy.persistSubmittedSecret(body, responseBody); err != nil {
			http.Error(writer, "Không lưu được mật khẩu vào kho bí mật hệ điều hành.", http.StatusBadGateway)
			return
		}
	}
	if freshInstallProfileWrite(request.Method, request.URL.Path) && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := proxy.persistFreshInstallSecrets(body, responseBody); err != nil {
			http.Error(writer, "Không lưu được thông tin cài WordPress vào kho bí mật hệ điều hành.", http.StatusBadGateway)
			return
		}
	}
	if projectName, deleting := deletionProjectName(request.Method, request.URL.Path); deleting && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := deleteEmbeddedRecoveryPassword(projectName); err != nil {
			http.Error(writer, "Dữ liệu local đang được xóa nhưng chưa dọn được mật khẩu trong kho bí mật hệ điều hành. Hãy thử lại thao tác xóa.", http.StatusBadGateway)
			return
		}
	}
	if siteName, deleting := freshInstallDeletionName(request.Method, request.URL.Path); deleting && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := deleteEmbeddedRecoveryPassword("fresh-" + siteName); err != nil {
			http.Error(writer, "Website đã được xóa khỏi danh sách nhưng chưa dọn được mật khẩu trong kho bí mật hệ điều hành. Hãy thử lại thao tác xóa.", http.StatusBadGateway)
			return
		}
	}
	copyResponseHeaders(writer.Header(), response.Header)
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") {
		writer.Header().Del("X-Frame-Options")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'")
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(responseBody)))
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(responseBody)
}

func deletionProjectName(method, path string) (string, bool) {
	if method != http.MethodPost {
		return "", false
	}
	trimmed := strings.TrimSuffix(path, "/")
	const prefix = "/api/projects/"
	const suffix = "/delete"
	if !strings.HasPrefix(trimmed, prefix) || !strings.HasSuffix(trimmed, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), suffix)
	if name == "" || strings.ContainsAny(name, "/\\") {
		return "", false
	}
	return name, true
}

func freshInstallDeletionName(method, path string) (string, bool) {
	if method != http.MethodPost {
		return "", false
	}
	trimmed := strings.TrimSuffix(path, "/")
	const prefix = "/api/fresh-installs/"
	const suffix = "/delete"
	if !strings.HasPrefix(trimmed, prefix) || !strings.HasSuffix(trimmed, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), suffix)
	if name == "" || strings.ContainsAny(name, "/\\") {
		return "", false
	}
	return name, true
}

func (proxy *localProxy) persistSubmittedSecret(requestBody, responseBody []byte) error {
	var requestData map[string]any
	if err := json.Unmarshal(requestBody, &requestData); err != nil {
		return nil
	}
	password, _ := requestData["password"].(string)
	if password == "" {
		return nil
	}
	var responseData map[string]any
	if err := json.Unmarshal(responseBody, &responseData); err != nil {
		return err
	}
	name, _ := responseData["name"].(string)
	if err := saveEmbeddedRecoveryPassword(name, password); err != nil {
		return err
	}
	profile := filepath.Join(proxy.workspace, "sites", name+".json")
	_, _, err := migrateProfileSecret(profile, name)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (proxy *localProxy) persistFreshInstallSecrets(requestBody, responseBody []byte) error {
	var requestData map[string]any
	if err := json.Unmarshal(requestBody, &requestData); err != nil {
		return err
	}
	var responseData map[string]any
	if err := json.Unmarshal(responseBody, &responseData); err != nil {
		return err
	}
	name, _ := responseData["name"].(string)
	if name == "" {
		return errors.New("engine không trả về tên website cài mới")
	}
	credentialKey := "fresh-" + name
	credentials := map[string]string{}
	if existing, readErr := getEmbeddedRecoveryPassword(credentialKey); readErr == nil && existing != "" {
		_ = json.Unmarshal([]byte(existing), &credentials)
	}
	for field, key := range map[string]string{
		"password": "ftpPassword", "dbPassword": "dbPassword", "adminPassword": "adminPassword",
	} {
		value, _ := requestData[field].(string)
		if value != "" {
			credentials[key] = value
		}
		if credentials[key] == "" {
			return fmt.Errorf("thiếu thông tin bí mật %s", field)
		}
	}
	// Fresh WordPress intentionally uses the FTP password for database and
	// WordPress admin so the compact form has one password field. The value is
	// still kept only in the native credential vault/runtime secret bundle.
	credentials["dbPassword"] = credentials["ftpPassword"]
	credentials["adminPassword"] = credentials["ftpPassword"]
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	return saveEmbeddedRecoveryPassword(credentialKey, string(encoded))
}

func freshInstallProfileWrite(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	trimmed := strings.Trim(strings.TrimSuffix(path, "/"), "/")
	if trimmed == "api/fresh-installs/probe-workers" {
		return false
	}
	if trimmed == "api/fresh-installs" {
		return true
	}
	const prefix = "api/fresh-installs/"
	return strings.HasPrefix(trimmed, prefix) && !strings.Contains(strings.TrimPrefix(trimmed, prefix), "/")
}

func probeWPClean(target string) bool {
	client := &http.Client{Timeout: 650 * time.Millisecond}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, strings.TrimSuffix(target, "/")+"/", nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		!strings.Contains(response.Header.Get("Server"), "WPCleanGUI/1.0") ||
		response.Header.Get("X-OneClick-Recovery") != "1" {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return false
	}
	return bytes.Contains(body, []byte("/api/projects"))
}

func readLimitedBody(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("body too large")
	}
	return data, nil
}

func copyRequestHeaders(destination, source http.Header) {
	for key, values := range source {
		if hopHeader(key) || strings.EqualFold(key, "Cookie") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func copyResponseHeaders(destination, source http.Header) {
	for key, values := range source {
		if hopHeader(key) || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Security-Policy") || strings.EqualFold(key, "X-Frame-Options") {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func hopHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
