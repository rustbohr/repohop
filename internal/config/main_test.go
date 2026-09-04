package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the whole package's tests at a throwaway XDG environment, so
// no test can read or write the developer's real config or state, whatever it
// forgets to isolate for itself.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "repohop-config-tests")
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
