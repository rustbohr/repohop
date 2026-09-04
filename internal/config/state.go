package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// State is repohop's small bit of remembered UI state. It lives outside the
// config directory precisely so a shared, committed config never carries it.
type State struct {
	ActiveProject string `yaml:"active_project,omitempty"`
}

// LoadState reads the state file. A missing file is the zero State.
func LoadState() (State, error) {
	path, err := StatePath()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := yaml.Unmarshal(data, &state); err != nil {
		// A corrupt state file is not worth failing over; it is only a
		// remembered selection.
		return State{}, nil
	}
	return state, nil
}

// SaveState writes the state file.
func SaveState(state State) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}
