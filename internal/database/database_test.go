package database

import (
	"compress/gzip"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appstate "oneclick-dev-server/internal/state"
	"oneclick-dev-server/internal/vm"
)

func TestGuestDatabaseBashSyntax(t *testing.T) {
	bash := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
	if _, err := os.Stat(bash); err != nil {
		t.Skip("Git Bash is unavailable")
	}
	path := filepath.Join(t.TempDir(), "database.sh")
	if err := os.WriteFile(path, []byte(guestDatabaseScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
		if strings.Contains(string(output), "couldn't create signal pipe") {
			t.Skip("Git Bash is blocked by the current Windows sandbox")
		}
		t.Fatalf("bash syntax: %v: %s", err, output)
	}
}

func TestDetectWordPressSourceReadsLiteralConfigWithoutExecutingPHP(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "laragon", "www", "demo")
	toolPath := filepath.Join(root, "laragon", "bin", "mysql", "mysql-8.0", "bin", "mysqldump.exe")
	serverPath := filepath.Join(filepath.Dir(toolPath), "mysqld.exe")
	serverConfig := filepath.Join(filepath.Dir(filepath.Dir(toolPath)), "my.ini")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(toolPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverPath, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverConfig, []byte("[mysqld]\nport=3307\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `<?php
define( 'DB_NAME', 'demo_db' );
define('DB_USER', 'root');
define('DB_PASSWORD', 'p\'ass');
define('DB_HOST', 'localhost:3307');
$table_prefix = 'client_';
throw new RuntimeException('must never execute');
`
	if err := os.WriteFile(filepath.Join(projectPath, "wp-config.php"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := detectWordPressSource(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if source.Database != "demo_db" || source.User != "root" || source.Password != "p'ass" || source.Host != "127.0.0.1" || source.Port != "3307" || source.TablePrefix != "client_" {
		t.Fatalf("unexpected source: %#v", source)
	}
	if !strings.EqualFold(source.DumpTool, toolPath) {
		t.Fatalf("wrong dump tool: %q", source.DumpTool)
	}
	if source.Stack != "Laragon" || !strings.EqualFold(source.ServerTool, serverPath) || !strings.EqualFold(source.ServerConfig, serverConfig) {
		t.Fatalf("wrong local database launcher: %#v", source)
	}
}

func TestSourceDatabaseProbeRecognizesListeningLoopbackPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:")
	if !sourceDatabaseOnline(context.Background(), "127.0.0.1", port) {
		t.Fatal("listening local database port was not detected")
	}
}

func TestEnsureSourceDatabaseFailsClosedWithoutTrustedLauncher(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:")
	_ = listener.Close()
	err = ensureSourceDatabase(context.Background(), sourceConfig{Host: "127.0.0.1", Port: port})
	if err == nil || !strings.Contains(err.Error(), "không tìm thấy bộ khởi động an toàn") {
		t.Fatalf("offline database without trusted launcher was accepted: %v", err)
	}
}

func TestDatabaseServerArgumentsKeepAutoStartedServerOnLoopback(t *testing.T) {
	arguments := databaseServerArguments(sourceConfig{
		Stack:        "Laragon",
		ServerTool:   filepath.Join("D:\\laragon", "bin", "mysql", "mysql-8.0.30", "bin", "mysqld.exe"),
		ServerConfig: filepath.Join("D:\\laragon", "bin", "mysql", "mysql-8.0.30", "my.ini"),
	})
	joined := strings.Join(arguments, " ")
	for _, required := range []string{"--defaults-file=", "--bind-address=127.0.0.1", "--mysqlx=OFF"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("server arguments are missing %q: %v", required, arguments)
		}
	}
	if strings.Contains(joined, "0.0.0.0") {
		t.Fatalf("server arguments expose the source database: %v", arguments)
	}
}

func TestDetectWordPressSourceRejectsRemoteDatabase(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "laragon", "www", "demo")
	toolPath := filepath.Join(root, "laragon", "bin", "mysql", "mysql-8.0", "bin", "mysqldump.exe")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(toolPath), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(toolPath, []byte("fixture"), 0o700)
	config := `<?php
define('DB_NAME', 'demo');
define('DB_USER', 'root');
define('DB_PASSWORD', 'secret');
define('DB_HOST', 'db.internal.example:3306');
$table_prefix = 'wp_';
`
	if err := os.WriteFile(filepath.Join(projectPath, "wp-config.php"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := detectWordPressSource(projectPath); err == nil || !strings.Contains(err.Error(), "không nằm trên máy này") {
		t.Fatalf("remote database was accepted: %v", err)
	}
}

func TestDetectWordPressSourceAllowsEmptyLocalPassword(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "laragon", "www", "demo")
	toolPath := filepath.Join(root, "laragon", "bin", "mysql", "mysql-8.0", "bin", "mysqldump.exe")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(toolPath), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(toolPath, []byte("fixture"), 0o700)
	config := `<?php
define('DB_NAME', 'demo');
define('DB_USER', 'root');
define('DB_PASSWORD', '');
define('DB_HOST', '127.0.0.1');
$table_prefix = 'wp_';
`
	if err := os.WriteFile(filepath.Join(projectPath, "wp-config.php"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := detectWordPressSource(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if source.Password != "" || source.Database != "demo" || source.TablePrefix != "wp_" {
		t.Fatalf("unexpected empty-password source: %#v", source)
	}
}

func TestManualGzipPlanAndPrefixDetection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.sql.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	_, _ = compressed.Write([]byte("CREATE TABLE IF NOT EXISTS `demo`.`agency_options` (option_id bigint);\nCREATE TABLE `agency_posts` (ID bigint);"))
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	validated, size, prefix, err := validateImportFile(path)
	if err != nil || validated == "" || size <= 0 || prefix != "" {
		t.Fatalf("unexpected validation: path=%q size=%d prefix=%q err=%v", validated, size, prefix, err)
	}
	prefix, err = detectDumpPrefix(path)
	if err != nil || prefix != "agency_" {
		t.Fatalf("unexpected gzip prefix: %q, %v", prefix, err)
	}
}

func TestGuestDatabasePolicyKeepsSecretsOutOfArgumentsAndLimitsScope(t *testing.T) {
	for _, required := range []string{
		"--defaults-extra-file=\"$APP_CNF\"",
		"DROP DATABASE IF EXISTS $DB_NAME",
		"REVOKE ALL PRIVILEGES, GRANT OPTION",
		"CREATE TEMPORARY TABLES, LOCK TABLES ON $DB_NAME.*",
		"MAX_USER_CONNECTIONS 8 MAX_STATEMENT_TIME 30",
		"'$DB_USER'@'localhost'",
		"sha256sum",
		"gzip -t",
		"rm -f \"$VALUE_A\"",
		"table_name='${table_prefix}options'",
		"setfacl -b \"$SITE_ROOT/wp-config.php\"",
	} {
		if !strings.Contains(guestDatabaseScript, required) {
			t.Fatalf("guest database policy is missing %q", required)
		}
	}
	for _, forbidden := range []string{"-p$", "MYSQL_PWD", "network_mode: host", "SOURCE ", "docker compose", "'wordpress'@'%'", "GRANT ALL PRIVILEGES"} {
		if strings.Contains(guestDatabaseScript, forbidden) {
			t.Fatalf("guest database policy contains forbidden text %q", forbidden)
		}
	}
}

func TestOptionValueCannotInjectAnotherConfigLine(t *testing.T) {
	value := optionValue("a\\b\"c")
	if value != `a\\b\"c` {
		t.Fatalf("unexpected option escaping: %q", value)
	}
	if _, err := detectWordPressSource(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing config was accepted")
	}
}

func TestLiveAutomaticPlanAndGuestScriptSyntax(t *testing.T) {
	if os.Getenv("ONECLICK_LIVE_DATABASE") != "1" {
		t.Skip("set ONECLICK_LIVE_DATABASE=1 for the read-only host plan and guest syntax check")
	}
	projectPath := strings.TrimSpace(os.Getenv("ONECLICK_LIVE_PROJECT_PATH"))
	projectName := strings.TrimSpace(os.Getenv("ONECLICK_LIVE_PROJECT_NAME"))
	if projectPath == "" {
		repository, err := appstate.NewDefault()
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := repository.List()
		if err != nil {
			t.Fatal(err)
		}
		var record *appstate.ProjectRecord
		for index := range snapshot.Projects {
			candidate := &snapshot.Projects[index]
			if candidate.Kind == "wordpress" && candidate.VMName != "" && candidate.RuntimeState == "running" && candidate.RuntimeHealth == "healthy" {
				record = candidate
				break
			}
		}
		if record == nil {
			t.Fatal("no healthy WordPress runtime is available for the live database check")
		}
		projectPath, projectName = record.Path, record.Name
	}
	if projectName == "" {
		projectName = filepath.Base(projectPath)
	}
	source, err := detectWordPressSource(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if source.Database == "" || source.TablePrefix == "" || source.DumpTool == "" {
		t.Fatal("automatic database plan is incomplete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := ensureSourceDatabase(ctx, source); err != nil {
		t.Fatalf("source database is not ready: %v", err)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workingRoot := filepath.Join(repositoryRoot, "data", "transfers")
	if err := os.MkdirAll(workingRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.MkdirTemp(workingRoot, "live-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workingDirectory)
	dumpPath := filepath.Join(workingDirectory, "database.sql")
	if err := exportDatabase(ctx, source, dumpPath); err != nil {
		t.Fatalf("read-only source export failed: %v", err)
	}
	info, err := os.Stat(dumpPath)
	if err != nil || info.Size() <= 0 {
		t.Fatalf("source export is empty: info=%v err=%v", info, err)
	}
	prefix, err := detectDumpPrefix(dumpPath)
	if err != nil || prefix != source.TablePrefix {
		t.Fatalf("source export verification failed: prefix=%q err=%v", prefix, err)
	}
	file, err := os.CreateTemp("", "oneclick-database-syntax-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(guestDatabaseScript); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	guest, err := vm.OpenGuest(ctx, vm.Request{ProjectPath: projectPath, ProjectName: projectName})
	if err != nil {
		t.Fatal(err)
	}
	guestPath := "/home/ubuntu/.oneclick/database-live-syntax.sh"
	if err := guest.Transfer(ctx, path, guestPath); err != nil {
		t.Fatal(err)
	}
	defer guest.Exec(context.Background(), "rm", "-f", guestPath)
	if _, err := guest.Exec(ctx, "bash", "-n", guestPath); err != nil {
		t.Fatal(err)
	}
}
