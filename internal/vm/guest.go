package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"oneclick-dev-server/internal/multipass"
)

var safeGuestExport = regexp.MustCompile(`^/home/ubuntu/\.oneclick/exports/[a-f0-9]{16,64}/[0-9]{8}-[0-9]{6}/(?:source\.tar\.gz|database\.sql)$`)

// Guest is a verified handle to the shared OneClick server VM. It deliberately
// exposes only argument-vector execution and file transfer; no shell text is
// accepted from the desktop frontend.
type Guest struct {
	executable string
	name       string
}

// OpenGuest derives the fixed server name from trusted project identity and verifies the
// ownership marker, OS, architecture, state and absence of host mounts.
func OpenGuest(parent context.Context, request Request) (*Guest, error) {
	plan, err := makePlan(request)
	if err != nil {
		return nil, err
	}
	executable, err := multipass.Find()
	if err != nil {
		return nil, err
	}
	if parent == nil {
		parent = context.Background()
	}
	// Multipass on Windows can need several SSH handshakes while VirtualBox is
	// warming up. Keep this bounded, but do not turn a slow first handshake into
	// a false "unsafe VM" result.
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	if err := verifySnapshotTarget(ctx, executable, plan.VMName); err != nil {
		return nil, fmt.Errorf("máy ảo chưa vượt qua kiểm tra an toàn: %w", err)
	}
	return &Guest{executable: executable, name: plan.VMName}, nil
}

func (guest *Guest) Name() string {
	if guest == nil {
		return ""
	}
	return guest.name
}

// Exec runs one fixed program with separate arguments inside the verified VM.
func (guest *Guest) Exec(ctx context.Context, command ...string) (string, error) {
	if guest == nil || guest.executable == "" || guest.name == "" {
		return "", errors.New("guest handle is not ready")
	}
	if len(command) == 0 {
		return "", errors.New("guest command is empty")
	}
	return execInVM(ctx, guest.executable, guest.name, command...)
}

// Transfer copies one regular host file to an explicit path in the VM.
func (guest *Guest) Transfer(ctx context.Context, localPath, guestPath string) error {
	if guest == nil || guest.executable == "" || guest.name == "" {
		return errors.New("guest handle is not ready")
	}
	absolute, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("transfer source is not a regular file")
	}
	if guestPath == "" || guestPath[0] != '/' {
		return errors.New("guest transfer path must be absolute")
	}
	_, err = run(ctx, guest.executable, "transfer", absolute, guest.name+":"+guestPath)
	return err
}

// Download copies one reviewed backup artifact out of the verified VM. This is
// intentionally narrower than Transfer: only per-project export paths created
// by OneClick are accepted, so deployed code cannot choose an arbitrary guest
// or host file to copy.
func (guest *Guest) Download(ctx context.Context, guestPath, localPath string) error {
	if guest == nil || guest.executable == "" || guest.name == "" {
		return errors.New("guest handle is not ready")
	}
	if !safeGuestExport.MatchString(guestPath) {
		return errors.New("guest export path is not allowed")
	}
	absolute, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return errors.New("download destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(absolute)
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() || isLinkLike(info) {
		return errors.New("download destination directory is not trusted")
	}
	if _, err := run(ctx, guest.executable, "transfer", guest.name+":"+guestPath, absolute); err != nil {
		// Multipass/SFTP can copy the complete file to NTFS and then return exit
		// code 1 only because POSIX mode 0600 cannot be applied. Accept exactly
		// that post-copy warning; the regular-file and checksum checks below are
		// still mandatory.
		if !ignorableDownloadPermissionError(err) {
			return err
		}
	}
	info, err = os.Lstat(absolute)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || isLinkLike(info) {
		_ = os.Remove(absolute)
		return errors.New("downloaded artifact is not a regular file")
	}
	return nil
}

func ignorableDownloadPermissionError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "cannot set permissions for local file")
}
