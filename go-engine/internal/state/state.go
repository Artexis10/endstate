// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package state provides persistence for Endstate engine run state and history.
// State files are written atomically using a temp+rename pattern to prevent
// corruption on crash or power loss.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/config"
)

// State represents the engine's persisted state (state/state.json).
type State struct {
	SchemaVersion string `json:"schemaVersion"`
	LastRunID     string `json:"lastRunId"`
	LastCommand   string `json:"lastCommand"`
	LastTimestamp  string `json:"lastTimestamp"`
	RunCount      int    `json:"runCount"`
}

// StateDir returns the state directory path.
// It uses the "state" subdirectory under the repo root resolved by
// config.ResolveRepoRoot(). If the repo root cannot be determined it falls
// back to a "state" directory relative to the current working directory.
func StateDir() string {
	root := config.ResolveRepoRoot()
	if root != "" {
		return filepath.Join(root, "state")
	}
	return filepath.Join(".", "state")
}

// ReadState reads state.json from path. If the file does not exist a default
// State with SchemaVersion "1.0" is returned (no error). Any other I/O or
// parse error is returned as-is.
func ReadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{SchemaVersion: "1.0"}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WriteState writes state to path using the atomic temp+rename pattern.
// The parent directory is created if it does not exist.
func WriteState(path string, s *State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
