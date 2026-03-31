// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// NOTE: The ldflags code path (version/schemaVersion vars non-empty) cannot be
// tested via unit tests because the vars are set at link time. That path is
// verified by the build-level integration test in the task verification step:
//   go build -ldflags "-X ...config.version=99.0.0" && ./test-endstate.exe capabilities --json

// ---------------------------------------------------------------------------
// ReadVersion tests (ldflags unset — file-based and fallback paths)
// ---------------------------------------------------------------------------

func TestReadVersion_FallbackWhenNoFileAndNoLdflags(t *testing.T) {
	// With no VERSION file and no ldflags, ReadVersion should return the
	// fallback constant.
	got := ReadVersion(t.TempDir())
	if got != fallbackVersion {
		t.Errorf("ReadVersion() = %q, want %q", got, fallbackVersion)
	}
}

func TestReadVersion_ReadsFileWhenLdflagsUnset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1.2.3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := ReadVersion(dir)
	if got != "1.2.3" {
		t.Errorf("ReadVersion() = %q, want %q", got, "1.2.3")
	}
}

func TestReadVersion_EmptyFileReturnsFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("  \n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := ReadVersion(dir)
	if got != fallbackVersion {
		t.Errorf("ReadVersion() = %q, want %q", got, fallbackVersion)
	}
}

// ---------------------------------------------------------------------------
// ReadSchemaVersion tests (ldflags unset — file-based and fallback paths)
// ---------------------------------------------------------------------------

func TestReadSchemaVersion_FallbackWhenNoFileAndNoLdflags(t *testing.T) {
	got := ReadSchemaVersion(t.TempDir())
	if got != fallbackSchemaVersion {
		t.Errorf("ReadSchemaVersion() = %q, want %q", got, fallbackSchemaVersion)
	}
}

func TestReadSchemaVersion_ReadsFileWhenLdflagsUnset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA_VERSION"), []byte("2.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := ReadSchemaVersion(dir)
	if got != "2.1" {
		t.Errorf("ReadSchemaVersion() = %q, want %q", got, "2.1")
	}
}

func TestReadSchemaVersion_EmptyFileReturnsFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA_VERSION"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	got := ReadSchemaVersion(dir)
	if got != fallbackSchemaVersion {
		t.Errorf("ReadSchemaVersion() = %q, want %q", got, fallbackSchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// EmbeddedVersion / EmbeddedSchemaVersion tests
// ---------------------------------------------------------------------------

func TestEmbeddedVersion_EmptyWhenLdflagsUnset(t *testing.T) {
	// In normal test builds, ldflags are not set so the vars are zero-value.
	got := EmbeddedVersion()
	if got != "" {
		t.Errorf("EmbeddedVersion() = %q, want empty string", got)
	}
}

func TestEmbeddedSchemaVersion_EmptyWhenLdflagsUnset(t *testing.T) {
	got := EmbeddedSchemaVersion()
	if got != "" {
		t.Errorf("EmbeddedSchemaVersion() = %q, want empty string", got)
	}
}
