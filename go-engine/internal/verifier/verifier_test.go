// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package verifier

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ---------------------------------------------------------------------------
// CheckFileExists tests
// ---------------------------------------------------------------------------

func TestCheckFileExists_ExistingFile(t *testing.T) {
	// Create a temporary file to verify against.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testfile.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	entry := manifest.VerifyEntry{Type: "file-exists", Path: tmpFile}
	result := CheckFileExists(entry)

	if !result.Pass {
		t.Errorf("expected pass=true for existing file, got false: %s", result.Message)
	}
	if result.Type != "file-exists" {
		t.Errorf("expected type=file-exists, got %q", result.Type)
	}
}

func TestCheckFileExists_ExistingDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	entry := manifest.VerifyEntry{Type: "file-exists", Path: tmpDir}
	result := CheckFileExists(entry)

	if !result.Pass {
		t.Errorf("expected pass=true for existing directory, got false: %s", result.Message)
	}
}

func TestCheckFileExists_NonExistent(t *testing.T) {
	entry := manifest.VerifyEntry{
		Type: "file-exists",
		Path: filepath.Join(t.TempDir(), "does-not-exist.txt"),
	}
	result := CheckFileExists(entry)

	if result.Pass {
		t.Error("expected pass=false for non-existent path, got true")
	}
	if result.Message == "" {
		t.Error("expected a non-empty message for failed check")
	}
}

func TestCheckFileExists_EnvVarExpansion(t *testing.T) {
	// Set a temporary env var and use it in the path.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "envtest.txt")
	if err := os.WriteFile(tmpFile, []byte("env"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	t.Setenv("ENDSTATE_TEST_DIR", tmpDir)

	var pathWithEnv string
	if runtime.GOOS == "windows" {
		pathWithEnv = "%ENDSTATE_TEST_DIR%\\envtest.txt"
		// os.ExpandEnv uses $VAR or ${VAR} syntax, not %VAR% — but on
		// Windows we need to handle the $env: or %VAR% form. Since Go's
		// os.ExpandEnv only supports $VAR/${VAR}, we test with that syntax.
		pathWithEnv = "${ENDSTATE_TEST_DIR}\\envtest.txt"
	} else {
		pathWithEnv = "${ENDSTATE_TEST_DIR}/envtest.txt"
	}

	entry := manifest.VerifyEntry{Type: "file-exists", Path: pathWithEnv}
	result := CheckFileExists(entry)

	if !result.Pass {
		t.Errorf("expected pass=true with env var expansion, got false: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// CheckCommandExists tests
// ---------------------------------------------------------------------------

func TestCheckCommandExists_KnownCommand(t *testing.T) {
	// "go" should be on PATH in the test environment.
	entry := manifest.VerifyEntry{Type: "command-exists", Command: "go"}
	result := CheckCommandExists(entry)

	if !result.Pass {
		t.Errorf("expected pass=true for 'go' command, got false: %s", result.Message)
	}
	if result.Command != "go" {
		t.Errorf("expected command=go, got %q", result.Command)
	}
}

func TestCheckCommandExists_UnknownCommand(t *testing.T) {
	entry := manifest.VerifyEntry{Type: "command-exists", Command: "nonexistent-cmd-xyz"}
	result := CheckCommandExists(entry)

	if result.Pass {
		t.Error("expected pass=false for nonexistent command, got true")
	}
	if result.Message == "" {
		t.Error("expected non-empty message for failed command check")
	}
}

// ---------------------------------------------------------------------------
// RunVerify dispatcher tests
// ---------------------------------------------------------------------------

func TestRunVerify_DispatchByType(t *testing.T) {
	// Create a temporary file for the file-exists check.
	tmpFile := filepath.Join(t.TempDir(), "dispatch-test.txt")
	if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	entries := []manifest.VerifyEntry{
		{Type: "file-exists", Path: tmpFile},
		{Type: "command-exists", Command: "go"},
	}

	results := RunVerify(entries)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if !results[0].Pass {
		t.Errorf("file-exists check should pass: %s", results[0].Message)
	}
	if !results[1].Pass {
		t.Errorf("command-exists check should pass: %s", results[1].Message)
	}
}

func TestRunVerify_UnknownType(t *testing.T) {
	entries := []manifest.VerifyEntry{
		{Type: "unicorn-check"},
	}

	results := RunVerify(entries)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Pass {
		t.Error("expected pass=false for unknown type, got true")
	}
	if results[0].Type != "unicorn-check" {
		t.Errorf("expected type=unicorn-check, got %q", results[0].Type)
	}
}

func TestRunVerify_EmptyEntries(t *testing.T) {
	results := RunVerify(nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil entries, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Registry verifier tests (platform-aware)
// ---------------------------------------------------------------------------

func TestCheckRegistryKeyExists_PlatformBehavior(t *testing.T) {
	entry := manifest.VerifyEntry{
		Type: "registry-key-exists",
		Path: "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion",
	}

	result := CheckRegistryKeyExists(entry)

	if runtime.GOOS != "windows" {
		// On non-Windows platforms the stub should return fail.
		if result.Pass {
			t.Error("expected pass=false on non-Windows platform, got true")
		}
		if result.Message != "Registry checks only supported on Windows" {
			t.Errorf("unexpected message: %s", result.Message)
		}
	} else {
		// On Windows, this well-known key should exist.
		if !result.Pass {
			t.Errorf("expected pass=true for well-known registry key, got false: %s", result.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// Registry verifier — HKLM keys (Windows-only)
// ---------------------------------------------------------------------------

func TestCheckRegistryKeyExists_HKLM_KnownKey(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry tests only run on Windows")
	}

	// HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion always exists.
	entry := manifest.VerifyEntry{
		Type: "registry-key-exists",
		Path: "HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion",
	}
	result := CheckRegistryKeyExists(entry)

	if !result.Pass {
		t.Errorf("expected pass=true for HKLM known key, got false: %s", result.Message)
	}
}

func TestCheckRegistryKeyExists_NonExistentKey(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry tests only run on Windows")
	}

	entry := manifest.VerifyEntry{
		Type: "registry-key-exists",
		Path: "HKLM\\SOFTWARE\\NonExistentKey12345",
	}
	result := CheckRegistryKeyExists(entry)

	if result.Pass {
		t.Error("expected pass=false for non-existent registry key, got true")
	}
}

func TestCheckRegistryKeyExists_SpecificValue(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry tests only run on Windows")
	}

	// ProgramFilesDir always exists under HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion.
	entry := manifest.VerifyEntry{
		Type:      "registry-key-exists",
		Path:      "HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion",
		ValueName: "ProgramFilesDir",
	}
	result := CheckRegistryKeyExists(entry)

	if !result.Pass {
		t.Errorf("expected pass=true for ProgramFilesDir value, got false: %s", result.Message)
	}
	if result.ValueName != "ProgramFilesDir" {
		t.Errorf("expected valueName=ProgramFilesDir, got %q", result.ValueName)
	}
}

// ---------------------------------------------------------------------------
// Result structure contract tests (mirrors Pester Verifier.ResultStructure)
// ---------------------------------------------------------------------------

func TestVerifyResult_ContainsRequiredFields_FileExists(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "result-structure.txt")
	if err := os.WriteFile(tmpFile, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	entry := manifest.VerifyEntry{Type: "file-exists", Path: tmpFile}
	result := CheckFileExists(entry)

	// Must have Type, Pass (bool), and Message (non-empty string).
	if result.Type != "file-exists" {
		t.Errorf("expected type=file-exists, got %q", result.Type)
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
	// Path must be populated for file-exists.
	if result.Path == "" {
		t.Error("expected Path to be set for file-exists result")
	}
}

func TestVerifyResult_ContainsRequiredFields_CommandExists(t *testing.T) {
	entry := manifest.VerifyEntry{Type: "command-exists", Command: "go"}
	result := CheckCommandExists(entry)

	if result.Type != "command-exists" {
		t.Errorf("expected type=command-exists, got %q", result.Type)
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
	// Command must be populated for command-exists.
	if result.Command == "" {
		t.Error("expected Command to be set for command-exists result")
	}
}

// ---------------------------------------------------------------------------
// Determinism tests (mirrors Pester Verifier.Determinism)
// ---------------------------------------------------------------------------

func TestVerifyDeterminism_FileExists(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "determ.txt")
	if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	entry := manifest.VerifyEntry{Type: "file-exists", Path: tmpFile}
	r1 := CheckFileExists(entry)
	r2 := CheckFileExists(entry)

	if r1.Pass != r2.Pass {
		t.Errorf("repeated file-exists calls produced different results: %v vs %v", r1.Pass, r2.Pass)
	}
}

func TestVerifyDeterminism_CommandExists(t *testing.T) {
	entry := manifest.VerifyEntry{Type: "command-exists", Command: "go"}
	r1 := CheckCommandExists(entry)
	r2 := CheckCommandExists(entry)

	if r1.Pass != r2.Pass {
		t.Errorf("repeated command-exists calls produced different results: %v vs %v", r1.Pass, r2.Pass)
	}
}

// ---------------------------------------------------------------------------
// File-exists verifier — missing file message contains "not found"
// ---------------------------------------------------------------------------

func TestCheckFileExists_MissingFile_MessageContainsNotFound(t *testing.T) {
	entry := manifest.VerifyEntry{
		Type: "file-exists",
		Path: filepath.Join(t.TempDir(), "nonexistent-file-xyz.txt"),
	}
	result := CheckFileExists(entry)

	if result.Pass {
		t.Error("expected pass=false for missing file")
	}
	if result.Message == "" {
		t.Error("expected non-empty message for missing file")
	}
	// Pester asserts message matches "does not exist" — Go uses "not found".
	if !contains(result.Message, "not found") && !contains(result.Message, "does not exist") {
		t.Errorf("expected message to mention not found, got %q", result.Message)
	}
}

// ---------------------------------------------------------------------------
// Command-exists verifier — message for not-found command
// ---------------------------------------------------------------------------

func TestCheckCommandExists_NotFound_MessageContainsNotFound(t *testing.T) {
	entry := manifest.VerifyEntry{Type: "command-exists", Command: "nonexistent-command-xyz-12345"}
	result := CheckCommandExists(entry)

	if result.Pass {
		t.Error("expected pass=false for nonexistent command")
	}
	if !contains(result.Message, "not found") {
		t.Errorf("expected message to contain 'not found', got %q", result.Message)
	}
}

// contains is a small helper to check substring presence.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
