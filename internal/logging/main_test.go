package logging

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps the tests away from the developer's real state directory.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "repohop-logging-tests")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows, so
	// isolating the home directory means setting both.
	os.Setenv("HOME", filepath.Join(root, "home"))
	os.Setenv("USERPROFILE", filepath.Join(root, "home"))

	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}
