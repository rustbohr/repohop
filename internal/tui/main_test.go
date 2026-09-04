package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the whole package's tests at a throwaway XDG environment.
// Individual tests still isolate what they need, but a test that forgets — or
// a code path that reaches for the user's config without being asked — must
// never be able to read or write the real one.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "repohop-tui-tests")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows, so
	// isolating the home directory means setting both.
	os.Setenv("HOME", filepath.Join(root, "home"))
	os.Setenv("USERPROFILE", filepath.Join(root, "home"))

	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}
