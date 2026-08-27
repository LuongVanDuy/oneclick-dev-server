package recovery

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPlainFTPConnectionChecksCredentialsAndRemoteDirectory(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		writer := bufio.NewWriter(connection)
		write := func(value string) error {
			if _, err := writer.WriteString(value + "\r\n"); err != nil {
				return err
			}
			return writer.Flush()
		}
		if err := write("220 test FTP ready"); err != nil {
			done <- err
			return
		}
		expected := []string{"USER operator", "PASS secret", "CWD /public_html", "PWD", "QUIT"}
		responses := []string{"331 password required", "230 authenticated", "250 changed", `257 "/public_html"`, "221 bye"}
		for index := range expected {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				done <- readErr
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line != expected[index] {
				done <- fmt.Errorf("expected %q, got %q", expected[index], line)
				return
			}
			if err := write(responses[index]); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	project := Project{Host: "127.0.0.1", Port: port, Username: "operator", Protocol: "ftp", RemotePath: "/public_html"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	remotePath, err := testFTPConnection(ctx, project, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	if remotePath != "/public_html" {
		t.Fatalf("unexpected remote path %q", remotePath)
	}
	if serverErr := <-done; serverErr != nil {
		t.Fatal(serverErr)
	}
}

func TestConnectionResultNeverContainsPassword(t *testing.T) {
	project := Project{Host: "127.0.0.1", Port: 1, Username: "operator", Protocol: "ftps", RemotePath: "/"}
	result := TestConnection(context.Background(), project, "super-secret-value")
	if result.Success {
		t.Fatal("connection to closed port unexpectedly succeeded")
	}
	if strings.Contains(result.Detail, "super-secret-value") {
		t.Fatalf("connection detail leaked password: %s", result.Detail)
	}
}
