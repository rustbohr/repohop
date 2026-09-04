package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DirConfigName is the per-directory config file, intended to be committed
// into a workspace repository so a team clones and has the project defined.
const DirConfigName = ".repohop.yaml"

// UserConfigDir is where repohop keeps its own config:
// $XDG_CONFIG_HOME/repohop, falling back to ~/.config/repohop on Unix and
// %AppData%\repohop on Windows.
func UserConfigDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "repohop"), nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "repohop"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "repohop"), nil
}

// UserConfigPath is the user's config file. It need not exist.
func UserConfigPath() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// StateDir is where the remembered active project lives: deliberately not the
// config directory, so a committed config never fights over whose project is
// selected.
func StateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "repohop"), nil
	}
	if runtime.GOOS == "windows" {
		if dir := os.Getenv("LocalAppData"); dir != "" {
			return filepath.Join(dir, "repohop"), nil
		}
		return UserConfigDir()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "repohop"), nil
}

// StatePath is the state file.
func StatePath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.yaml"), nil
}

// FindDirConfig walks up from start looking for a directory config, returning
// the first hit. An empty result means there is none.
func FindDirConfig(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, DirConfigName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ExpandPath expands a leading ~ and any $VAR references, leaving the result
// unresolved otherwise. Paths are stored in config as written, so a committed
// file stays portable across machines with different home directories.
func ExpandPath(path string) string {
	path = os.ExpandEnv(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path[1:], string(filepath.Separator)))
		}
	}
	return filepath.Clean(path)
}

// resolvePath makes path absolute: expanded first, then resolved against base
// (itself resolved against the config file's directory) when still relative.
func resolvePath(path, base, configDir string) string {
	path = ExpandPath(path)
	if filepath.IsAbs(path) {
		return path
	}
	if base != "" {
		base = ExpandPath(base)
		if !filepath.IsAbs(base) {
			base = filepath.Join(configDir, base)
		}
		return filepath.Join(base, path)
	}
	return filepath.Join(configDir, path)
}
