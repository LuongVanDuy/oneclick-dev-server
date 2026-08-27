package readiness

import (
	"math"
	"testing"
)

func TestFirstLine(t *testing.T) {
	t.Parallel()

	if got := firstLine("multipass 1.16.1\nmultipassd 1.16.1"); got != "multipass 1.16.1" {
		t.Fatalf("firstLine() = %q", got)
	}
}

func TestToGB(t *testing.T) {
	t.Parallel()

	got := toGB(8 * 1024 * 1024 * 1024)
	if math.Abs(got-8) > 0.001 {
		t.Fatalf("toGB() = %f, want 8", got)
	}
}

func TestPlatformCheckHasStableContract(t *testing.T) {
	t.Parallel()

	check := checkPlatform()
	if check.ID != "platform" {
		t.Fatalf("check ID = %q, want platform", check.ID)
	}
	if !check.Required {
		t.Fatal("platform check must be required")
	}
	if check.Status == "" || check.Summary == "" {
		t.Fatal("platform check must include status and human-readable summary")
	}
}
