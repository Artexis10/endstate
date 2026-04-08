// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package config provides version reading and path resolution for the Endstate
// Go engine.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// version and schemaVersion are set at compile time via ldflags, e.g.:
//
//	go build -ldflags "-X github.com/Artexis10/endstate/go-engine/internal/config.version=1.7.2
//	  -X github.com/Artexis10/endstate/go-engine/internal/config.schemaVersion=1.0"
//
// When set, ReadVersion and ReadSchemaVersion return these values directly,
// bypassing the file-based lookup. This ensures production binaries always
// carry the correct version regardless of whether VERSION files are nearby.
var (
	version       string // set via -ldflags "-X ...config.version=<ver>"
	schemaVersion string // set via -ldflags "-X ...config.schemaVersion=<ver>"
)

const (
	fallbackVersion       = "0.0.0-dev"
	fallbackSchemaVersion = "1.0"
)

// EmbeddedVersion returns the version string injected at compile time via
// ldflags. It returns an empty string if no version was embedded, allowing
// callers to distinguish "compiled with version" from "fell back to file".
func EmbeddedVersion() string {
	return version
}

// EmbeddedSchemaVersion returns the schema version string injected at compile
// time via ldflags. It returns an empty string if no version was embedded.
func EmbeddedSchemaVersion() string {
	return schemaVersion
}

// ReadVersion returns the Endstate CLI version string.
// If a version was embedded via ldflags at compile time, it is returned
// directly. Otherwise, it falls back to reading .release-please-manifest.json
// at repoRoot (the single source of truth managed by release-please).
// If neither source is available, it returns fallbackVersion ("0.0.0-dev").
func ReadVersion(repoRoot string) string {
	if version != "" {
		return version
	}
	return readVersionFromManifest(filepath.Join(repoRoot, ".release-please-manifest.json"), fallbackVersion)
}

// ReadSchemaVersion returns the JSON schema version string.
// If a schema version was embedded via ldflags at compile time, it is returned
// directly. Otherwise, it falls back to reading the SCHEMA_VERSION file at
// repoRoot. If neither source is available, it returns fallbackSchemaVersion.
func ReadSchemaVersion(repoRoot string) string {
	if schemaVersion != "" {
		return schemaVersion
	}
	return readTrimmedFile(filepath.Join(repoRoot, "SCHEMA_VERSION"), fallbackSchemaVersion)
}

// readVersionFromManifest reads a release-please manifest JSON file (e.g.
// {"." : "1.7.6"}) and returns the version string for the root package (".").
// Returns fallback if the file is missing, malformed, or lacks a "." key.
func readVersionFromManifest(path, fallback string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return fallback
	}
	v := strings.TrimSpace(m["."])
	if v == "" {
		return fallback
	}
	return v
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
