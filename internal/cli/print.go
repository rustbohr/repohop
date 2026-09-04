package cli

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// fmtLine writes one tab-separated row.
func fmtLine(w io.Writer, fields ...string) {
	io.WriteString(w, strings.Join(fields, "\t")+"\n")
}

func itoa(n int) string { return strconv.Itoa(n) }

// existsNote annotates a path that is not there yet.
func existsNote(path string) string {
	if _, err := os.Stat(path); err != nil {
		return "(does not exist)"
	}
	return ""
}

// shortenPath replaces the home directory with ~ for display.
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	rest := strings.TrimPrefix(path, home)
	if rest == "" {
		return "~"
	}
	if !strings.HasPrefix(rest, string(filepath.Separator)) {
		return path
	}
	return "~" + rest
}
