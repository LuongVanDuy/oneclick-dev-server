package recovery

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	connectionTimeout = 35 * time.Second
	maxReplyLine      = 8 * 1024
)

type ftpSession struct {
	connection net.Conn
	reader     *bufio.Reader
	writer     *bufio.Writer
}

type authenticatedFTPSession struct {
	project    Project
	connection net.Conn
	session    *ftpSession
	tlsConfig  *tls.Config
	secure     bool
}

// TestConnection verifies only the control channel, authentication and exact
// remote directory. It does not list, download, modify or execute remote data.
func TestConnection(parent context.Context, project Project, password string) ConnectionResult {
	checkedAt := nowText()
	if password == "" {
		return ConnectionResult{Message: "Chưa có mật khẩu kết nối", Detail: "Mật khẩu FTP/FTPS chưa được lưu trong kho bí mật của hệ điều hành.", CheckedAt: checkedAt}
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, connectionTimeout)
	defer cancel()
	remotePath, err := testFTPConnection(ctx, project, password, nil)
	if err != nil {
		detail := cleanConnectionError(err)
		return ConnectionResult{Message: "Không kết nối được website", Detail: detail, Secure: project.Protocol == "ftps", CheckedAt: checkedAt}
	}
	return ConnectionResult{
		Success: true, Message: "Kết nối website thành công", RemotePath: remotePath,
		Secure: project.Protocol == "ftps", CheckedAt: checkedAt,
	}
}

func testFTPConnection(ctx context.Context, project Project, password string, tlsConfig *tls.Config) (string, error) {
	client, err := openAuthenticatedFTPSession(ctx, project, password, tlsConfig)
	if err != nil {
		return "", err
	}
	defer client.close()
	session := client.session
	if err := session.writeCommand("CWD " + project.RemotePath); err != nil {
		return "", err
	}
	code, _, err := session.readReply()
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("không truy cập được thư mục website %s", project.RemotePath)
	}
	remotePath := project.RemotePath
	if err := session.writeCommand("PWD"); err == nil {
		if pwdCode, message, replyErr := session.readReply(); replyErr == nil && pwdCode == 257 {
			if parsed := parsePWD(message); parsed != "" {
				remotePath = parsed
			}
		}
	}
	return remotePath, nil
}

func openAuthenticatedFTPSession(ctx context.Context, project Project, password string, tlsConfig *tls.Config) (_ *authenticatedFTPSession, resultErr error) {
	address := net.JoinHostPort(project.Host, strconv.Itoa(project.Port))
	dialer := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 15 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("không mở được kết nối tới máy chủ: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = connection.Close()
		}
	}()
	deadline := time.Now().Add(connectionTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, err
	}
	session := newFTPSession(connection)
	if code, _, err := session.readReply(); err != nil || code != 220 {
		if err != nil {
			return nil, fmt.Errorf("máy chủ không trả lời đúng giao thức FTP: %w", err)
		}
		return nil, fmt.Errorf("máy chủ từ chối kết nối FTP (mã %d)", code)
	}

	var dataTLSConfig *tls.Config
	if project.Protocol == "ftps" {
		if err := session.writeCommand("AUTH TLS"); err != nil {
			return nil, err
		}
		code, _, err := session.readReply()
		if err != nil || code != 234 {
			return nil, errors.New("máy chủ không hỗ trợ FTPS explicit an toàn")
		}
		config := tlsConfig
		if config == nil {
			config = &tls.Config{ServerName: project.Host, MinVersion: tls.VersionTLS12}
		} else {
			config = config.Clone()
			config.ServerName = project.Host
			if config.MinVersion < tls.VersionTLS12 {
				config.MinVersion = tls.VersionTLS12
			}
		}
		secureConnection := tls.Client(connection, config)
		if err := secureConnection.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("chứng chỉ hoặc kết nối TLS không hợp lệ: %w", err)
		}
		connection = secureConnection
		session = newFTPSession(secureConnection)
		dataTLSConfig = config.Clone()
	}

	if err := session.writeCommand("USER " + project.Username); err != nil {
		return nil, err
	}
	code, _, err := session.readReply()
	if err != nil {
		return nil, err
	}
	if code == 331 {
		if err := session.writeCommand("PASS " + password); err != nil {
			return nil, err
		}
		code, _, err = session.readReply()
		if err != nil {
			return nil, err
		}
	}
	if code != 230 {
		return nil, fmt.Errorf("máy chủ từ chối tài khoản hoặc mật khẩu (mã %d)", code)
	}

	if project.Protocol == "ftps" {
		for _, command := range []string{"PBSZ 0", "PROT P"} {
			if err := session.writeCommand(command); err != nil {
				return nil, err
			}
			code, _, err = session.readReply()
			if err != nil || code/100 != 2 {
				return nil, errors.New("máy chủ không bật được kênh dữ liệu FTPS mã hóa")
			}
		}
	}
	return &authenticatedFTPSession{project: project, connection: connection, session: session, tlsConfig: dataTLSConfig, secure: project.Protocol == "ftps"}, nil
}

func (client *authenticatedFTPSession) close() {
	if client == nil || client.connection == nil {
		return
	}
	_ = client.session.writeCommand("QUIT")
	_ = client.connection.Close()
}

func newFTPSession(connection net.Conn) *ftpSession {
	return &ftpSession{connection: connection, reader: bufio.NewReaderSize(connection, 4096), writer: bufio.NewWriterSize(connection, 4096)}
}

func (session *ftpSession) writeCommand(command string) error {
	if strings.ContainsAny(command, "\r\n") {
		return errors.New("lệnh FTP chứa ký tự không hợp lệ")
	}
	_ = session.connection.SetDeadline(time.Now().Add(connectionTimeout))
	if _, err := session.writer.WriteString(command + "\r\n"); err != nil {
		return err
	}
	return session.writer.Flush()
}

func (session *ftpSession) readReply() (int, string, error) {
	_ = session.connection.SetDeadline(time.Now().Add(connectionTimeout))
	line, err := session.readLine()
	if err != nil {
		return 0, "", err
	}
	if len(line) < 3 {
		return 0, "", errors.New("phản hồi FTP quá ngắn")
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil || code < 100 || code > 599 {
		return 0, "", errors.New("mã phản hồi FTP không hợp lệ")
	}
	message := ""
	if len(line) > 4 {
		message = line[4:]
	}
	if len(line) == 3 || line[3] != '-' {
		return code, message, nil
	}
	terminator := line[:3] + " "
	for count := 0; count < 64; count++ {
		line, err = session.readLine()
		if err != nil {
			return 0, "", err
		}
		if strings.HasPrefix(line, terminator) {
			if len(line) > 4 {
				message = line[4:]
			}
			return code, message, nil
		}
	}
	return 0, "", errors.New("phản hồi FTP nhiều dòng vượt giới hạn")
}

func (session *ftpSession) readLine() (string, error) {
	line, err := session.reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line != "" {
			return "", errors.New("phản hồi FTP bị ngắt")
		}
		return "", err
	}
	if len(line) > maxReplyLine {
		return "", errors.New("phản hồi FTP vượt giới hạn")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func parsePWD(message string) string {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "\"") {
		return ""
	}
	message = message[1:]
	end := strings.Index(message, "\"")
	if end < 0 {
		return ""
	}
	value := message[:end]
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "..") || strings.ContainsAny(value, "\r\n\\") {
		return ""
	}
	return value
}

func cleanConnectionError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(value), "i/o timeout") {
		return "Máy chủ không phản hồi trong thời gian cho phép. Kiểm tra host, cổng và firewall của hosting."
	}
	if len([]rune(value)) > 600 {
		value = string([]rune(value)[:600]) + "…"
	}
	return value
}
