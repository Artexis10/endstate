// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package config provides version reading and path resolution for the Endstate
// Go engine.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	fallbackVersion       = "0.0.0-dev"
	fallbackSchemaVersion = "1.0"
)

// ReadVersion returns the Endstate CLI version string from the VERSION file at
// repoRoot. If the file cannot be read, it returns fallbackVersion.
func ReadVersion(repoRoot string) string {
	return readTrimmedFile(filepath.Join(repoRoot, "VERSION"), fallbackVersion)
}

// ReadSchemaVersion returns the JSON schema version string from the
// SCHEMA_VERSION file at repoRoot. If the file cannot be read, it returns
// fallbackSchemaVersion.
func ReadSchemaVersion(repoRoot string) string {
	return readTrimmedFile(filepath.Join(repoRoot, "SCHEMA_VERSION"), fallbackSchemaVersion)
}

// readTrimmedFile reads the named file, trims whitespace, and returns the result.
// Returns fallback if the file cannot be opened or is empty after trimming.
func readTrimmedFile(path, fallback string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return fallback
	}
	return s
}
