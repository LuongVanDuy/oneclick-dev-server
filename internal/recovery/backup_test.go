package recovery

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultRemotePathUsesDirectAdminDomainLayout(t *testing.T) {
	for input, expected := range map[string]string{
		"cunchici.vn":      "/domains/cunchici.vn/public_html",
		"ftp.CUNCHICI.vn.": "/domains/cunchici.vn/public_html",
		"127.0.0.1":        "/public_html",
	} {
		if actual := DefaultRemotePath(input); actual != expected {
			t.Fatalf("DefaultRemotePath(%q) = %q, want %q", input, actual, expected)
		}
	}
	repository, err := New(filepath.Join(t.TempDir(), "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.Save(ProjectInput{Name: "DirectAdmin", Host: "cunchici.vn", Username: "operator", Protocol: "ftps", Port: 21, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if project.RemotePath != "/domains/cunchici.vn/public_html" {
		t.Fatalf("backend did not apply derived path: %+v", project)
	}
}

func TestBackupDownloadsNeutralBlobsBuildsManifestAndScans(t *testing.T) {
	files := map[string][]byte{
		"/domains/example.com/public_html/index.php":                    []byte("<?php echo 'safe';"),
		"/domains/example.com/public_html/wp-content/uploads/photo.jpg": []byte("GIF89a<?php eval(base64_decode($_POST['x']));"),
	}
	server := newFakeBackupFTP(t, files)
	root := t.TempDir()
	project := Project{
		ID: "0123456789abcdef01234567", Name: "Example", Host: "127.0.0.1", Port: server.port,
		Username: "operator", Protocol: "ftp", RemotePath: "/domains/example.com/public_html", Workers: 2,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := Backup(ctx, project, "secret-value", filepath.Join(root, "project"), nil)
	if !result.Success {
		t.Fatalf("backup failed: %+v", result)
	}
	if result.Files != 2 || result.Critical == 0 || result.Findings == 0 {
		t.Fatalf("unexpected backup summary: %+v", result)
	}
	manifest, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "secret-value") || !strings.Contains(string(manifest), "photo.jpg") {
		t.Fatalf("manifest secret/path contract failed: %s", manifest)
	}
	report, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "hidden_php_upload") || strings.Contains(string(report), "secret-value") {
		t.Fatalf("unexpected scan report: %s", report)
	}
	err = filepath.Walk(result.Path, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".php") {
			t.Fatalf("untrusted executable extension was persisted: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type fakeBackupFTP struct {
	port     int
	listener net.Listener
	files    map[string][]byte
	wg       sync.WaitGroup
}

func newFakeBackupFTP(t *testing.T, files map[string][]byte) *fakeBackupFTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &fakeBackupFTP{port: listener.Addr().(*net.TCPAddr).Port, listener: listener, files: files}
	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			server.wg.Add(1)
			go func() {
				defer server.wg.Done()
				server.handle(connection)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		server.wg.Wait()
	})
	return server
}

func (server *fakeBackupFTP) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	write := func(value string) bool {
		if _, err := writer.WriteString(value + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !write("220 backup test ready") {
		return
	}
	var passive net.Listener
	defer func() {
		if passive != nil {
			_ = passive.Close()
		}
	}()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		command, argument, _ := strings.Cut(line, " ")
		switch command {
		case "USER":
			if argument != "operator" || !write("331 password required") {
				return
			}
		case "PASS":
			if argument != "secret-value" || !write("230 authenticated") {
				return
			}
		case "EPSV":
			if passive != nil {
				_ = passive.Close()
			}
			passive, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return
			}
			port := passive.Addr().(*net.TCPAddr).Port
			if !write(fmt.Sprintf("229 Entering Extended Passive Mode (|||%d|)", port)) {
				return
			}
		case "MLSD":
			listing := server.listing(argument)
			if passive == nil || !write("150 opening data") {
				return
			}
			data, acceptErr := passive.Accept()
			if acceptErr != nil {
				return
			}
			_, _ = data.Write([]byte(listing))
			_ = data.Close()
			_ = passive.Close()
			passive = nil
			if !write("226 transfer complete") {
				return
			}
		case "TYPE":
			if !write("200 type set") {
				return
			}
		case "RETR":
			content, ok := server.files[argument]
			if !ok {
				_ = write("550 missing")
				continue
			}
			if passive == nil || !write("150 opening data") {
				return
			}
			data, acceptErr := passive.Accept()
			if acceptErr != nil {
				return
			}
			_, _ = data.Write(content)
			_ = data.Close()
			_ = passive.Close()
			passive = nil
			if !write("226 transfer complete") {
				return
			}
		case "QUIT":
			_ = write("221 bye")
			return
		default:
			if !write("502 unsupported") {
				return
			}
		}
	}
}

func (server *fakeBackupFTP) listing(directory string) string {
	switch directory {
	case "/domains/example.com/public_html":
		return fmt.Sprintf("type=file;size=%d; index.php\r\ntype=dir; wp-content\r\n", len(server.files[directory+"/index.php"]))
	case "/domains/example.com/public_html/wp-content":
		return "type=dir; uploads\r\n"
	case "/domains/example.com/public_html/wp-content/uploads":
		return fmt.Sprintf("type=file;size=%d; photo.jpg\r\n", len(server.files[directory+"/photo.jpg"]))
	default:
		return ""
	}
}
