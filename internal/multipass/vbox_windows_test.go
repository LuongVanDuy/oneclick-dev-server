//go:build windows

package multipass

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveVBoxDriverInspection(t *testing.T) {
	expectReboot := os.Getenv("ONECLICK_LIVE_EXPECT_VBOX_REBOOT") == "1"
	expectReady := os.Getenv("ONECLICK_LIVE_EXPECT_VBOX_READY") == "1"
	if !expectReboot && !expectReady {
		t.Skip("set ONECLICK_LIVE_EXPECT_VBOX_REBOOT=1 or ONECLICK_LIVE_EXPECT_VBOX_READY=1")
	}
	inspection, err := inspectVBoxDrivers(supportedVirtualBoxVersion)
	if err != nil {
		t.Fatal(err)
	}
	if expectReboot {
		if inspection.Ready || !inspection.RebootRequired {
			t.Fatalf("expected reboot-required driver mismatch, got %#v", inspection)
		}
		if !strings.Contains(inspection.Detail, "VBoxSup.sys 7.2.16.") {
			t.Fatalf("expected old VBoxSup evidence, got %q", inspection.Detail)
		}
	} else if !inspection.Ready || inspection.RebootRequired {
		t.Fatalf("expected one clean pinned VirtualBox installation, got %#v", inspection)
	}
	executable, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backend, err := InspectBackend(ctx, executable)
	if err != nil {
		t.Fatal(err)
	}
	if expectReboot && (backend.Ready || !backend.RebootRequired || backend.MissingAction != "") {
		t.Fatalf("backend must block VM creation until reboot, got %#v", backend)
	}
	if expectReady && (!backend.Ready || backend.RebootRequired || backend.MissingAction != "") {
		t.Fatalf("backend must be ready after clean install, got %#v", backend)
	}
}
