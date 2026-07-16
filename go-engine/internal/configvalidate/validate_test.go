// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configvalidate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestValidateSupportsClosedPrimitiveSet(t *testing.T) {
	root := safeValidationTestRoot(t)
	writeValidationFile(t, root, "plain.txt", "exists")
	writeValidationFile(t, root, "settings.json", `{"theme":"dark","nested":{"list":[{"enabled":true}]}}`)
	writeValidationFile(t, root, "settings.ini", "[Settings]\nTheme=dark\n")

	validations := []modules.ValidationDef{
		{Type: "file-exists", Path: "plain.txt"},
		{Type: "json-parse", Path: "settings.json"},
		{Type: "json-path-exists", Path: "settings.json", JSONPath: "$.nested.list[0].enabled"},
		{Type: "ini-parse", Path: "settings.ini"},
		{Type: "ini-key-exists", Path: "settings.ini", Section: "Settings", Key: "Theme"},
	}
	if err := ValidateStaging(root, validations); err != nil {
		t.Fatalf("ValidateStaging: %v", err)
	}
}

func TestValidateReportsStableCodeIndexAndDefinition(t *testing.T) {
	root := safeValidationTestRoot(t)
	writeValidationFile(t, root, "exists.txt", "exists")
	wantDefinition := modules.ValidationDef{Type: "json-parse", Path: "missing.json"}
	err := ValidateStaging(root, []modules.ValidationDef{
		{Type: "file-exists", Path: "exists.txt"},
		wantDefinition,
	})
	if CodeOf(err) != CodePathNotFound {
		t.Fatalf("Validate error = %v, code = %q, want %q", err, CodeOf(err), CodePathNotFound)
	}
	var validationError *Error
	if !errors.As(err, &validationError) {
		t.Fatalf("Validate error type = %T, want *Error", err)
	}
	if validationError.Index != 1 || !reflect.DeepEqual(validationError.Validation, wantDefinition) {
		t.Fatalf("error context = index %d, validation %#v", validationError.Index, validationError.Validation)
	}
}

func TestValidatePrimitiveFailures(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		validation modules.ValidationDef
		wantCode   Code
	}{
		{"unsupported validation", "value", modules.ValidationDef{Type: "command", Path: "../outside"}, CodeUnsupportedValidation},
		{"malformed JSON", `{"broken":`, modules.ValidationDef{Type: "json-parse", Path: "input"}, CodeMalformedJSON},
		{"invalid JSON path", `{}`, modules.ValidationDef{Type: "json-path-exists", Path: "input", JSONPath: "$[*]"}, CodeInvalidJSONPath},
		{"missing JSON path", `{}`, modules.ValidationDef{Type: "json-path-exists", Path: "input", JSONPath: "$.missing"}, CodeJSONPathMissing},
		{"malformed INI", "key:value\n", modules.ValidationDef{Type: "ini-parse", Path: "input"}, CodeMalformedINI},
		{"invalid INI address", "[Settings]\nTheme=dark\n", modules.ValidationDef{Type: "ini-key-exists", Path: "input", Section: " Settings", Key: "Theme"}, CodeInvalidINIAddress},
		{"missing exact-case INI key", "[Settings]\nTheme=dark\n", modules.ValidationDef{Type: "ini-key-exists", Path: "input", Section: "Settings", Key: "theme"}, CodeINIKeyMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := safeValidationTestRoot(t)
			writeValidationFile(t, root, "input", tt.content)
			err := ValidateStaging(root, []modules.ValidationDef{tt.validation})
			if CodeOf(err) != tt.wantCode {
				t.Fatalf("Validate error = %v, code = %q, want %q", err, CodeOf(err), tt.wantCode)
			}
		})
	}
}

func TestValidateRejectsUnsafePathForms(t *testing.T) {
	root := safeValidationTestRoot(t)
	unsafePaths := []string{
		"../outside", "/absolute", `C:\outside`, `\\server\share\file`,
		"file:stream", "${instance.root}/file", "%APPDATA%/file", "~/file",
	}
	for _, path := range unsafePaths {
		err := ValidateStaging(root, []modules.ValidationDef{{Type: "file-exists", Path: path}})
		if CodeOf(err) != CodeUnsafePath {
			t.Errorf("Validate path %q error = %v, code = %q", path, err, CodeOf(err))
		}
	}
}

func TestValidateRejectsLinksAndNonRegularFiles(t *testing.T) {
	root := safeValidationTestRoot(t)
	writeValidationFile(t, root, "target", "value")
	if err := os.Symlink(filepath.Join(root, "target"), filepath.Join(root, "link")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}
	if err := ValidateStaging(root, []modules.ValidationDef{{Type: "file-exists", Path: "link"}}); CodeOf(err) != CodeLinkUnsupported {
		t.Fatalf("link Validate error = %v, code = %q", err, CodeOf(err))
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStaging(root, []modules.ValidationDef{{Type: "file-exists", Path: "directory"}}); CodeOf(err) != CodeUnsupportedFileType {
		t.Fatalf("directory Validate error = %v, code = %q", err, CodeOf(err))
	}
}

func TestValidateResolvedSupportsIndependentHostTargets(t *testing.T) {
	root := safeValidationTestRoot(t)
	writeValidationFile(t, root, "first/settings.json", `{"theme":"dark"}`)
	writeValidationFile(t, root, "second/settings.ini", "[Settings]\nTheme=dark\n")
	validations := []ResolvedValidation{
		{
			Definition: modules.ValidationDef{Type: "file-exists", Path: "logical/settings.json"},
			HostPath:   filepath.Join(root, "first", "settings.json"),
		},
		{
			Definition: modules.ValidationDef{Type: "json-path-exists", Path: "logical/settings.json", JSONPath: "$.theme"},
			HostPath:   filepath.Join(root, "first", "settings.json"),
		},
		{
			Definition: modules.ValidationDef{Type: "ini-key-exists", Path: "logical/settings.ini", Section: "Settings", Key: "Theme"},
			HostPath:   filepath.Join(root, "second", "settings.ini"),
		},
	}
	if err := ValidateResolved(validations); err != nil {
		t.Fatalf("ValidateResolved: %v", err)
	}
}

func TestValidateResolvedReportsPathAndPrimitiveFailures(t *testing.T) {
	tests := []struct {
		name       string
		content    *string
		definition modules.ValidationDef
		wantCode   Code
		asDir      bool
	}{
		{"missing", nil, modules.ValidationDef{Type: "file-exists", Path: "logical"}, CodePathNotFound, false},
		{"special directory", nil, modules.ValidationDef{Type: "file-exists", Path: "logical"}, CodeUnsupportedFileType, true},
		{"malformed JSON", stringPointer(`{"broken":`), modules.ValidationDef{Type: "json-parse", Path: "logical"}, CodeMalformedJSON, false},
		{"missing JSON path", stringPointer(`{"theme":"dark"}`), modules.ValidationDef{Type: "json-path-exists", Path: "logical", JSONPath: "$.missing"}, CodeJSONPathMissing, false},
		{"malformed INI", stringPointer("key:value\n"), modules.ValidationDef{Type: "ini-parse", Path: "logical"}, CodeMalformedINI, false},
		{"missing exact INI key", stringPointer("[Settings]\nTheme=dark\n"), modules.ValidationDef{Type: "ini-key-exists", Path: "logical", Section: "Settings", Key: "theme"}, CodeINIKeyMissing, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := safeValidationTestRoot(t)
			hostPath := filepath.Join(root, "target")
			switch {
			case tt.asDir:
				if err := os.Mkdir(hostPath, 0o755); err != nil {
					t.Fatal(err)
				}
			case tt.content != nil:
				writeValidationFile(t, root, "target", *tt.content)
			}
			err := ValidateResolved([]ResolvedValidation{{Definition: tt.definition, HostPath: hostPath}})
			if CodeOf(err) != tt.wantCode {
				t.Fatalf("ValidateResolved error = %v, code = %q, want %q", err, CodeOf(err), tt.wantCode)
			}
			var validationError *Error
			if !errors.As(err, &validationError) || validationError.HostPath != hostPath {
				t.Fatalf("resolved error context = %#v, want host path %q", validationError, hostPath)
			}
		})
	}
}

func TestValidateResolvedRejectsRelativeAndLinkedHostPaths(t *testing.T) {
	definition := modules.ValidationDef{Type: "file-exists", Path: "logical"}
	if err := ValidateResolved([]ResolvedValidation{{Definition: definition, HostPath: "relative"}}); CodeOf(err) != CodeUnsafePath {
		t.Fatalf("relative ValidateResolved error = %v, code = %q", err, CodeOf(err))
	}

	root := safeValidationTestRoot(t)
	writeValidationFile(t, root, "target", "value")
	linked := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "target"), linked); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}
	if err := ValidateResolved([]ResolvedValidation{{Definition: definition, HostPath: linked}}); CodeOf(err) != CodeLinkUnsupported {
		t.Fatalf("link ValidateResolved error = %v, code = %q", err, CodeOf(err))
	}

	realDirectory := filepath.Join(root, "real-directory")
	if err := os.Mkdir(realDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeValidationFile(t, realDirectory, "target", "value")
	linkedDirectory := filepath.Join(root, "linked-directory")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	linkedChild := filepath.Join(linkedDirectory, "target")
	if err := ValidateResolved([]ResolvedValidation{{Definition: definition, HostPath: linkedChild}}); CodeOf(err) != CodeLinkUnsupported {
		t.Fatalf("linked parent ValidateResolved error = %v, code = %q", err, CodeOf(err))
	}
}

func stringPointer(value string) *string { return &value }

func safeValidationTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writeValidationFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
