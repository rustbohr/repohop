package logging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritesEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	if _, err := Init(); err != nil {
		t.Fatal(err)
	}
	defer Close()

	Log().Printf("hello %s", "world")
	Log().Error("doing a thing", errors.New("it failed"))
	Log().Panic("drawing", "boom", []byte("stack line\n"))

	data, err := os.ReadFile(filepath.Join(root, "repohop", "repohop.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hello world", "error doing a thing: it failed", "panic drawing: boom", "stack line"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log is missing %q:\n%s", want, data)
		}
	}
	if got := strings.Count(string(data), "\n"); got < 4 {
		t.Errorf("expected one line per entry plus the stack, got:\n%s", data)
	}
}

func TestNilErrorIsNotLogged(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	if _, err := Init(); err != nil {
		t.Fatal(err)
	}
	defer Close()

	Log().Error("doing a thing", nil)
	data, _ := os.ReadFile(filepath.Join(root, "repohop", "repohop.log"))
	if len(data) != 0 {
		t.Errorf("a nil error was logged:\n%s", data)
	}
}

func TestOversizedLogIsStartedAgain(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	path := filepath.Join(root, "repohop", "repohop.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, maxSize+1), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Init(); err != nil {
		t.Fatal(err)
	}
	defer Close()
	Log().Printf("fresh")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1024 {
		t.Errorf("log is %d bytes, want it started again once past the cap", info.Size())
	}
}

func TestAnUnopenedLoggerDropsEntries(t *testing.T) {
	// Logging must never be the reason repohop stops working, so a logger with
	// nowhere to write simply discards what it is given.
	logger := &Logger{}
	logger.Printf("into the void")
	logger.Error("nowhere", errors.New("x"))
	logger.Panic("nowhere", "boom", nil)
	if logger.Path() != "" {
		t.Errorf("Path() = %q, want empty", logger.Path())
	}
}

func TestInitIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	defer Close()

	first, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("Init returned a different logger the second time")
	}
	Log().Printf("still working")

	data, err := os.ReadFile(filepath.Join(root, "repohop", "repohop.log"))
	if err != nil || !strings.Contains(string(data), "still working") {
		t.Fatalf("second Init broke the logger: %v\n%s", err, data)
	}
}

func TestNoFileIsCreatedUntilThereIsSomethingToSay(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	if _, err := Init(); err != nil {
		t.Fatal(err)
	}
	defer Close()

	path := filepath.Join(root, "repohop", "repohop.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Init created the log file; a run that goes well should leave nothing behind (%v)", err)
	}

	Log().Printf("now there is something")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the log file was not created on the first entry: %v", err)
	}
}
