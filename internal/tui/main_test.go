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
	hermeticGit(root)

	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

// hermeticGit points git at a configuration of our own for the whole package.
// Without this the tests inherit whatever the machine happens to have: the
// system config that ships with Git for Windows sets core.autocrlf=true, which
// checks files out with CRLF and makes a fresh clone look modified. Isolating
// HOME is not enough, because that only hides the *global* config and lets the
// system one through.
func hermeticGit(root string) {
	config := filepath.Join(root, "gitconfig")
	contents := "[user]\n\tname = repohop test\n\temail = test@example.invalid\n" +
		"[init]\n\tdefaultBranch = master\n" +
		"[core]\n\tautocrlf = false\n\tsymlinks = false\n" +
		"[commit]\n\tgpgsign = false\n" +
		"[protocol \"file\"]\n\tallow = always\n"
	if err := os.WriteFile(config, []byte(contents), 0o600); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Setenv("GIT_CONFIG_GLOBAL", config)

	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	os.Setenv("HOME", filepath.Join(root, "home"))
	os.Setenv("USERPROFILE", filepath.Join(root, "home"))
}
