package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"oneclick-dev-server/internal/hostexec"
)

const maxImportBytes int64 = 4 * 1024 * 1024 * 1024

var (
	safeProjectID   = regexp.MustCompile(`^[a-f0-9]{16,64}$`)
	safeDBName      = regexp.MustCompile(`^[A-Za-z0-9_$][A-Za-z0-9_$.-]{0,63}$`)
	safeTablePrefix = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	definePattern   = regexp.MustCompile(`(?m)define\s*\(\s*['"](DB_NAME|DB_USER|DB_PASSWORD|DB_HOST)['"]\s*,\s*(?:'((?:\\.|[^\\'\r\n])*)'|"((?:\\.|[^\\"\r\n])*)")\s*\)\s*;`)
	prefixPattern   = regexp.MustCompile(`(?m)^\s*\$table_prefix\s*=\s*(?:'((?:\\.|[^\\'\r\n])*)'|"((?:\\.|[^\\"\r\n])*)")\s*;`)
	optionsPattern  = regexp.MustCompile("(?i)(?:CREATE TABLE(?: IF NOT EXISTS)?|INSERT INTO)\\s+(?:`[A-Za-z0-9_$.-]+`\\.)?`?([A-Za-z0-9_]{1,64})options`?")
)

type sourceConfig struct {
	Database     string
	User         string
	Password     string
	Host         string
	Port         string
	TablePrefix  string
	ConfigPath   string
	DumpTool     string
	Stack        string
	ServerTool   string
	ServerConfig string
}

var sourceDatabaseStartMu sync.Mutex

func GetPlan(config Config, request Request) (Plan, error) {
	if err := validateConfig(config); err != nil {
		return Plan{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = "automatic"
	}
	plan := Plan{
		ProjectPath:       config.ProjectPath,
		Mode:              mode,
		ReplacementNotice: "Database đích trong máy ảo sẽ được thay thế. Database trên máy không bị chỉnh sửa.",
	}
	switch mode {
	case "automatic":
		source, err := detectWordPressSource(config.ProjectPath)
		if err != nil {
			plan.Source = "WordPress trên máy"
			plan.AutomaticMessage = err.Error()
			return plan, nil
		}
		plan.Ready = true
		plan.Source = "WordPress trên máy"
		plan.Database = source.Database
		plan.TablePrefix = source.TablePrefix
		plan.Tool = filepath.Base(source.DumpTool)
		if sourceDatabaseOnline(context.Background(), source.Host, source.Port) {
			plan.AutomaticMessage = "Đã nhận diện WordPress và database cục bộ đang chạy."
		} else if source.ServerTool != "" && source.ServerConfig != "" {
			plan.AutomaticMessage = "Database cục bộ đang tắt; OneClick sẽ tự khởi động khi sao chép."
		} else {
			plan.AutomaticMessage = "Đã nhận diện WordPress; cần bật database cục bộ trước khi sao chép."
		}
		return plan, nil
	case "file":
		path, size, prefix, err := validateImportFile(request.ImportPath)
		if err != nil {
			return Plan{}, err
		}
		plan.Ready = true
		plan.Source = "File database"
		plan.ImportPath = path
		plan.ImportBytes = size
		plan.TablePrefix = prefix
		if prefix == "" {
			plan.AutomaticMessage = "Chưa nhận diện được tiền tố bảng; file sẽ được kiểm tra lại trước khi nhập."
		}
		return plan, nil
	default:
		return Plan{}, errors.New("cách lấy database không được hỗ trợ")
	}
}

func validateConfig(config Config) error {
	if !safeProjectID.MatchString(config.ProjectID) {
		return errors.New("database project identity is invalid")
	}
	if strings.TrimSpace(config.ProjectPath) == "" || strings.TrimSpace(config.ProjectName) == "" || strings.TrimSpace(config.VMName) == "" {
		return errors.New("thông tin website hoặc máy ảo chưa đầy đủ")
	}
	if config.ProjectKind != "wordpress" {
		return fmt.Errorf("loại website %q chưa hỗ trợ tự nhập database; phiên bản này hỗ trợ WordPress", config.ProjectKind)
	}
	if config.RuntimeState != "running" || config.RuntimeHealth != "healthy" {
		return errors.New("runtime chưa sẵn sàng để nhập database")
	}
	return nil
}

func detectWordPressSource(projectPath string) (sourceConfig, error) {
	absolute, err := filepath.Abs(projectPath)
	if err != nil {
		return sourceConfig{}, err
	}
	configPath := filepath.Join(absolute, "wp-config.php")
	info, err := os.Lstat(configPath)
	if err != nil {
		return sourceConfig{}, errors.New("không tìm thấy wp-config.php; hãy chọn file .sql hoặc .sql.gz")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 1024*1024 {
		return sourceConfig{}, errors.New("wp-config.php không phải file cấu hình thông thường")
	}
	file, err := os.Open(configPath)
	if err != nil {
		return sourceConfig{}, errors.New("không đọc được wp-config.php")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return sourceConfig{}, errors.New("wp-config.php đã thay đổi trong lúc kiểm tra")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return sourceConfig{}, errors.New("không đọc được wp-config.php trong giới hạn an toàn")
	}
	finalInfo, err := file.Stat()
	if err != nil || finalInfo.Size() != openedInfo.Size() || !finalInfo.ModTime().Equal(openedInfo.ModTime()) {
		return sourceConfig{}, errors.New("wp-config.php đã thay đổi trong lúc đọc")
	}
	values := map[string]string{}
	found := map[string]bool{}
	for _, match := range definePattern.FindAllSubmatch(data, -1) {
		quote, encoded := "'", string(match[2])
		if match[3] != nil {
			quote, encoded = `"`, string(match[3])
		}
		value, decodeErr := decodePHPString(quote, encoded)
		if decodeErr != nil {
			return sourceConfig{}, fmt.Errorf("không đọc được %s trong wp-config.php", match[1])
		}
		name := string(match[1])
		values[name] = value
		found[name] = true
	}
	for _, name := range []string{"DB_NAME", "DB_USER", "DB_PASSWORD", "DB_HOST"} {
		if !found[name] {
			return sourceConfig{}, fmt.Errorf("%s không phải giá trị trực tiếp trong wp-config.php; hãy chọn file .sql hoặc .sql.gz", name)
		}
	}
	for _, name := range []string{"DB_NAME", "DB_USER", "DB_HOST"} {
		if values[name] == "" {
			return sourceConfig{}, fmt.Errorf("%s đang trống trong wp-config.php", name)
		}
	}
	if !safeDBName.MatchString(values["DB_NAME"]) {
		return sourceConfig{}, errors.New("tên database trong wp-config.php không an toàn để tự động xuất")
	}
	if strings.ContainsAny(values["DB_USER"]+values["DB_PASSWORD"], "\r\n\x00") {
		return sourceConfig{}, errors.New("tài khoản database chứa ký tự không được hỗ trợ")
	}
	host, port, err := parseLocalHost(values["DB_HOST"])
	if err != nil {
		return sourceConfig{}, err
	}
	prefix := ""
	if match := prefixPattern.FindSubmatch(data); len(match) == 3 {
		quote, encoded := "'", string(match[1])
		if match[2] != nil {
			quote, encoded = `"`, string(match[2])
		}
		prefix, err = decodePHPString(quote, encoded)
		if err != nil || !safeTablePrefix.MatchString(prefix) {
			return sourceConfig{}, errors.New("tiền tố bảng trong wp-config.php không hợp lệ")
		}
	}
	if prefix == "" {
		return sourceConfig{}, errors.New("không nhận diện được tiền tố bảng WordPress trong wp-config.php")
	}
	tool, err := findDumpTool(absolute)
	if err != nil {
		return sourceConfig{}, err
	}
	stack, serverTool, serverConfig := findLocalDatabaseServer(absolute, tool)
	return sourceConfig{
		Database: values["DB_NAME"], User: values["DB_USER"], Password: values["DB_PASSWORD"],
		Host: host, Port: port, TablePrefix: prefix, ConfigPath: configPath, DumpTool: tool,
		Stack: stack, ServerTool: serverTool, ServerConfig: serverConfig,
	}, nil
}

func findLocalDatabaseServer(projectPath, dumpTool string) (string, string, string) {
	if root := stackRoot(projectPath, "laragon", "www"); root != "" {
		server := filepath.Join(filepath.Dir(dumpTool), executableName("mysqld"))
		config := filepath.Join(filepath.Dir(filepath.Dir(dumpTool)), "my.ini")
		if regularExecutable(server) && regularConfig(config) {
			return "Laragon", server, config
		}
	}
	if root := stackRoot(projectPath, "xampp", "htdocs"); root != "" {
		server := filepath.Join(filepath.Dir(dumpTool), executableName("mysqld"))
		config := filepath.Join(filepath.Dir(dumpTool), "my.ini")
		if regularExecutable(server) && regularConfig(config) {
			return "XAMPP", server, config
		}
	}
	return "", "", ""
}

func executableName(name string) string {
	if filepath.Ext(os.Args[0]) == ".exe" || filepath.Separator == '\\' {
		return name + ".exe"
	}
	return name
}

func regularConfig(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Size() > 0 && info.Size() <= 1024*1024
}

func sourceDatabaseOnline(ctx context.Context, host, port string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func ensureSourceDatabase(ctx context.Context, source sourceConfig) error {
	if sourceDatabaseOnline(ctx, source.Host, source.Port) {
		return nil
	}
	sourceDatabaseStartMu.Lock()
	defer sourceDatabaseStartMu.Unlock()
	if sourceDatabaseOnline(ctx, source.Host, source.Port) {
		return nil
	}
	if source.ServerTool == "" || source.ServerConfig == "" || !regularExecutable(source.ServerTool) || !regularConfig(source.ServerConfig) {
		return errors.New("database cục bộ đang tắt và OneClick không tìm thấy bộ khởi động an toàn; hãy bật MySQL/MariaDB trong Laragon hoặc XAMPP rồi thử lại")
	}
	arguments := databaseServerArguments(source)
	command := hostexec.Command(source.ServerTool, arguments...)
	command.Dir = filepath.Dir(source.ServerTool)
	if err := command.Start(); err != nil {
		return fmt.Errorf("không tự khởi động được MySQL của %s: %w", source.Stack, err)
	}
	_ = command.Process.Release()
	deadline := time.NewTimer(75 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("đã dừng trong lúc chờ database cục bộ khởi động")
		case <-deadline.C:
			return fmt.Errorf("MySQL của %s chưa phản hồi sau 75 giây; mở %s, bật MySQL rồi thử lại", source.Stack, source.Stack)
		case <-ticker.C:
			if sourceDatabaseOnline(ctx, source.Host, source.Port) {
				return nil
			}
		}
	}
}

func databaseServerArguments(source sourceConfig) []string {
	// Override permissive local-stack defaults: OneClick only needs loopback
	// access while creating a read-only dump of the selected WordPress DB.
	arguments := []string{"--defaults-file=" + source.ServerConfig, "--bind-address=127.0.0.1"}
	versionDirectory := strings.ToLower(filepath.Base(filepath.Dir(filepath.Dir(source.ServerTool))))
	if source.Stack == "Laragon" && strings.HasPrefix(versionDirectory, "mysql-8") {
		arguments = append(arguments, "--mysqlx=OFF")
	}
	if filepath.Separator == '\\' {
		arguments = append(arguments, "--standalone")
	}
	return arguments
}

func decodePHPString(quote, value string) (string, error) {
	if quote == "'" {
		var decoded strings.Builder
		for index := 0; index < len(value); index++ {
			if value[index] == '\\' && index+1 < len(value) && (value[index+1] == '\\' || value[index+1] == '\'') {
				index++
			}
			decoded.WriteByte(value[index])
		}
		return decoded.String(), nil
	}
	if quote != `"` {
		return "", errors.New("unsupported PHP string")
	}
	var decoded strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) {
			decoded.WriteByte(value[index])
			continue
		}
		index++
		switch value[index] {
		case '\\', '"', '$':
			decoded.WriteByte(value[index])
		case 'n':
			decoded.WriteByte('\n')
		case 'r':
			decoded.WriteByte('\r')
		case 't':
			decoded.WriteByte('\t')
		default:
			decoded.WriteByte('\\')
			decoded.WriteByte(value[index])
		}
	}
	return decoded.String(), nil
}

func parseLocalHost(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	host, port := value, "3306"
	if strings.HasPrefix(value, "[") {
		end := strings.Index(value, "]")
		if end < 0 {
			return "", "", errors.New("DB_HOST không hợp lệ")
		}
		host = value[1:end]
		if len(value) > end+1 {
			if value[end+1] != ':' {
				return "", "", errors.New("DB_HOST không hợp lệ")
			}
			port = value[end+2:]
		}
	} else if strings.Count(value, ":") == 1 {
		parts := strings.SplitN(value, ":", 2)
		host, port = parts[0], parts[1]
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
	default:
		return "", "", errors.New("database không nằm trên máy này; hãy xuất file .sql rồi chọn file để nhập")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", errors.New("cổng database trong DB_HOST không hợp lệ")
	}
	// Force TCP so the dump command cannot follow a source-controlled socket path.
	if strings.EqualFold(host, "localhost") || host == "::1" {
		host = "127.0.0.1"
	}
	return host, strconv.Itoa(portNumber), nil
}

func findDumpTool(projectPath string) (string, error) {
	if root := stackRoot(projectPath, "laragon", "www"); root != "" {
		if tool := newestDumpTool(filepath.Join(root, "bin", "mysql", "*", "bin")); tool != "" {
			return tool, nil
		}
	}
	if root := stackRoot(projectPath, "xampp", "htdocs"); root != "" {
		for _, name := range []string{"mariadb-dump.exe", "mysqldump.exe", "mariadb-dump", "mysqldump"} {
			candidate := filepath.Join(root, "mysql", "bin", name)
			if regularExecutable(candidate) {
				return candidate, nil
			}
		}
	}
	for _, name := range []string{"mariadb-dump", "mysqldump", "mariadb-dump.exe", "mysqldump.exe"} {
		if path, err := exec.LookPath(name); err == nil && regularExecutable(path) {
			return path, nil
		}
	}
	return "", errors.New("chưa tìm thấy mysqldump hoặc mariadb-dump; hãy chọn file .sql hoặc .sql.gz")
}

func stackRoot(projectPath, stackName, documentDirectory string) string {
	current := filepath.Clean(projectPath)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		if strings.EqualFold(filepath.Base(current), documentDirectory) && strings.EqualFold(filepath.Base(parent), stackName) {
			return parent
		}
		current = parent
	}
}

func newestDumpTool(directoryPattern string) string {
	directories, _ := filepath.Glob(directoryPattern)
	for index := len(directories) - 1; index >= 0; index-- {
		for _, name := range []string{"mariadb-dump.exe", "mysqldump.exe", "mariadb-dump", "mysqldump"} {
			candidate := filepath.Join(directories[index], name)
			if regularExecutable(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func regularExecutable(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func validateImportFile(path string) (string, int64, string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil || strings.TrimSpace(path) == "" {
		return "", 0, "", errors.New("chưa chọn file database")
	}
	lower := strings.ToLower(absolute)
	if !strings.HasSuffix(lower, ".sql") && !strings.HasSuffix(lower, ".sql.gz") {
		return "", 0, "", errors.New("chỉ hỗ trợ file .sql hoặc .sql.gz")
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", 0, "", errors.New("không đọc được file database đã chọn")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", 0, "", errors.New("file database phải là file thông thường")
	}
	if info.Size() <= 0 || info.Size() > maxImportBytes {
		return "", 0, "", fmt.Errorf("file database phải lớn hơn 0 B và không quá %s", formatBytes(maxImportBytes))
	}
	prefix := ""
	if strings.HasSuffix(lower, ".sql") {
		file, openErr := os.Open(absolute)
		if openErr == nil {
			defer file.Close()
			buffer := make([]byte, 2*1024*1024)
			count, _ := file.Read(buffer)
			if match := optionsPattern.FindSubmatch(buffer[:count]); len(match) == 2 {
				prefix = string(match[1])
			}
		}
	}
	return filepath.Clean(absolute), info.Size(), prefix, nil
}

func formatBytes(value int64) string {
	return fmt.Sprintf("%.1f GB", float64(value)/(1024*1024*1024))
}
