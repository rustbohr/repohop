package git

import (
	"context"
	"errors"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in           string
		major, minor int
	}{
		{"2.34.1", 2, 34},
		{"2.11.0", 2, 11},
		{"3.0", 3, 0},
		{"2.39.5 (Apple Git-154)", 2, 39},
		{"nonsense", MinMajor, MinMinor},
		{"", MinMajor, MinMinor},
	}
	for _, tt := range tests {
		major, minor := parseVersion(tt.in)
		if major != tt.major || minor != tt.minor {
			t.Errorf("parseVersion(%q) = %d.%d, want %d.%d", tt.in, major, minor, tt.major, tt.minor)
		}
	}
}

func TestRequire(t *testing.T) {
	if err := testRunner().Require(context.Background()); err != nil {
		t.Fatalf("Require() with the real git = %v", err)
	}

	missing := &Runner{Binary: "definitely-not-git-repohop"}
	err := missing.Require(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Require() without git = %v, want ErrNotFound", err)
	}
}
