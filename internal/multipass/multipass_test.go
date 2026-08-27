package multipass

import "testing"

func TestSupportedVirtualBoxVersionIsPinned(t *testing.T) {
	for value, want := range map[string]bool{
		"7.1.18r173720":   true,
		" 7.1.18r173720 ": true,
		"7.1.16r1":        false,
		"7.2.16r174877":   false,
		"":                false,
	} {
		if got := isSupportedVirtualBoxVersion(value); got != want {
			t.Fatalf("isSupportedVirtualBoxVersion(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestSupportedVBoxDriverVersionIsPinned(t *testing.T) {
	for value, want := range map[string]bool{
		"7.1.18":        true,
		"7.1.18.173720": true,
		" 7.1.18.1 ":    true,
		"7.1.180.1":     false,
		"7.2.16.174877": false,
		"":              false,
	} {
		if got := isSupportedVBoxDriverVersion(value, supportedVirtualBoxVersion); got != want {
			t.Fatalf("isSupportedVBoxDriverVersion(%q) = %v, want %v", value, got, want)
		}
	}
}
