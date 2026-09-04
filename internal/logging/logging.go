// Package logging writes repohop's crash and error records to a file, so a
// failure the user saw for half a second in a terminal can still be looked at
// afterwards.
package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rustbohr/repohop/internal/config"
)

// maxSize caps the log.
const maxSize = 1 << 20 // 1 MiB

// Logger appends timestamped entries to a file. A Logger whose file could not
// be opened still works — it simply drops what it is given, because failing to
// log must never be the reason a tool stops working.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	path string
}

var std = &Logger{}

// Log is the process-wide logger.
func Log() *Logger { return std }

// Path is the log file's location, whether or not it could be opened.
func Path() string {
	if path, err := filePath(); err == nil {
		return path
	}
	return ""
}

// Init tells the process-wide logger where to write. The file itself is only
// created when there is something to record, so a run that goes well leaves
// nothing behind. It is safe to call more than once, and safe to ignore the
// error: the returned logger works either way.
func Init() (*Logger, error) {
	path, err := filePath()
	if err != nil {
		return std, err
	}
	std.mu.Lock()
	defer std.mu.Unlock()
	std.path = path
	return std, nil
}

// open creates the log file on first use. The caller holds the lock.
func (l *Logger) open() error {
	if l.file != nil {
		return nil
	}
	if l.path == "" {
		return errors.New("no log file configured")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}

	// Past the cap the log is started again: this is a record of what just
	// went wrong, not an audit trail.
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(l.path); err == nil && info.Size() > maxSize {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	file, err := os.OpenFile(l.path, flags, 0o600)
	if err != nil {
		return err
	}
	l.file = file
	return nil
}

// Close releases the log file.
func Close() error {
	std.mu.Lock()
	defer std.mu.Unlock()
	if std.file == nil {
		return nil
	}
	err := std.file.Close()
	std.file = nil
	return err
}

func filePath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "repohop.log"), nil
}

// Path is where this logger writes.
func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Printf records one entry.
func (l *Logger) Printf(format string, args ...any) {
	l.write(fmt.Sprintf(format, args...))
}

// Error records a failure that the UI has already shown the user.
func (l *Logger) Error(context string, err error) {
	if err == nil {
		return
	}
	l.write(fmt.Sprintf("error %s: %v", context, err))
}

// Panic records a recovered panic with its stack.
func (l *Logger) Panic(context string, recovered any, stack []byte) {
	l.write(fmt.Sprintf("panic %s: %v\n%s", context, recovered, stack))
}

func (l *Logger) write(message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.open(); err != nil {
		// Nowhere to write is not worth failing over; the user has already
		// been told what went wrong on screen.
		return
	}
	fmt.Fprintf(l.file, "%s %s\n", time.Now().Format(time.RFC3339), message)
}
