package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain isolates the XDG environment: these tests run the real commands,
// and must not read or write the developer's own config or state.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "repohop-cli-tests")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	os.Setenv("HOME", filepath.Join(root, "home"))
	os.Setenv("USERPROFILE", filepath.Join(root, "home"))

	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

// run executes the root command as the binary would, capturing its output.
// Tests run with stdout redirected, so this always takes the non-interactive
// path — which is exactly the one worth testing here.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	flagConfig, flagProject = "", ""

	var out bytes.Buffer
	root := newRootCmd("test")
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestPlainRepohopWithoutATerminal(t *testing.T) {
	// `repohop` with no arguments in a pipe falls back to the status table.
	// It used to build that command by hand and hand it a nil context, which
	// panicked inside the first git call — a crash on the most ordinary
	// invocation there is.
	out, err := run(t)

	if err == nil {
		t.Fatalf("expected the no-projects usage error, got none. Output:\n%s", out)
	}
	if got := err.Error(); !strings.Contains(got, "no projects configured") {
		t.Errorf("error = %q, want the no-projects advice", got)
	}
	if exitCodeFor(err) != exitUsage {
		t.Errorf("exit code = %d, want %d", exitCodeFor(err), exitUsage)
	}
}

func TestPlainRepohopListsRepositories(t *testing.T) {
	config := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "repohop", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "not-there")
	body := "projects:\n  - name: solo\n    repos: [" + filepath.ToSlash(missing) + "]\n"
	if err := os.WriteFile(config, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(config) })

	// One project resolves without being asked, so the table is printed. The
	// repository is missing, which is a partial failure, not a crash.
	out, err := run(t)
	if !strings.Contains(out, "REPO") || !strings.Contains(out, "not-there") {
		t.Errorf("output is not the status table:\n%s", out)
	}
	if err == nil || exitCodeFor(err) != exitPartial {
		t.Errorf("err = %v, want a partial failure (exit %d)", err, exitPartial)
	}
}

func TestConfigPathNamesEveryLocation(t *testing.T) {
	out, err := run(t, "config", "path")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"user", "state", "log", "config.yaml", "state.yaml", "repohop.log"} {
		if !strings.Contains(out, want) {
			t.Errorf("config path does not mention %q:\n%s", want, out)
		}
	}
}

func TestOrdinaryAdviceIsNotLogged(t *testing.T) {
	// The log is for failures. A first run being told there is nothing
	// configured yet should not leave a log file behind.
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"no projects yet", usagef("no projects configured yet"), false},
		{"unknown project", usageError{errors.New(`unknown project "x"`)}, false},
		{"a repository failed", partialError{errors.New("2 of 3 repositories failed")}, true},
		{"something unexpected", errors.New("boom"), true},
	}
	for _, tt := range tests {
		if got := worthLogging(tt.err); got != tt.want {
			t.Errorf("%s: worthLogging(%v) = %v, want %v", tt.name, tt.err, got, tt.want)
		}
	}
}
