// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestEngineINIOperationsProduceDeterministicBytes(t *testing.T) {
	root := safeMigrationTestRoot(t)
	writeMigrationFile(t, root, "settings.ini", "; comment\r\n[source]\r\nremove=gone\r\nkey=value\r\n[target]\r\nexisting=keep\r\n")
	operations := []modules.MigrationOperationDef{
		{Type: "ini-set", Path: "settings.ini", Section: "created", Key: "new", Value: "set"},
		{Type: "ini-move", Path: "settings.ini", FromSection: "source", FromKey: "key", ToSection: "target", ToKey: "moved"},
		{Type: "ini-delete", Path: "settings.ini", Section: "source", Key: "remove"},
	}
	if err := NewEngine().Apply(root, operations); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "[created]\nnew=set\n\n[source]\n\n[target]\nexisting=keep\nmoved=value\n"
	assertMigrationFile(t, root, "settings.ini", want)
	assertNoMigrationTemps(t, root)
}

func TestEngineINIFailuresLeaveTargetBytesUnchanged(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		operation modules.MigrationOperationDef
		wantCode  ErrorCode
	}{
		{
			name: "malformed document", content: "key:value\n",
			operation: modules.MigrationOperationDef{Type: "ini-set", Path: "settings.ini", Section: "section", Key: "key", Value: "value"},
			wantCode:  CodeMalformedINI,
		},
		{
			name: "set value is not string", content: "[section]\nkey=value\n",
			operation: modules.MigrationOperationDef{Type: "ini-set", Path: "settings.ini", Section: "section", Key: "key", Value: 42},
			wantCode:  CodeInvalidINIValue,
		},
		{
			name: "set value contains newline", content: "[section]\nkey=value\n",
			operation: modules.MigrationOperationDef{Type: "ini-set", Path: "settings.ini", Section: "section", Key: "key", Value: "line one\nline two"},
			wantCode:  CodeInvalidINIValue,
		},
		{
			name: "delete source missing", content: "[section]\nkey=value\n",
			operation: modules.MigrationOperationDef{Type: "ini-delete", Path: "settings.ini", Section: "section", Key: "missing"},
			wantCode:  CodeINISourceMissing,
		},
		{
			name: "move source missing", content: "[target]\nkey=value\n",
			operation: modules.MigrationOperationDef{Type: "ini-move", Path: "settings.ini", FromSection: "source", FromKey: "key", ToSection: "target", ToKey: "moved"},
			wantCode:  CodeINISourceMissing,
		},
		{
			name: "move destination exists", content: "[source]\nkey=value\n[target]\nkey=value\n",
			operation: modules.MigrationOperationDef{Type: "ini-move", Path: "settings.ini", FromSection: "source", FromKey: "key", ToSection: "target", ToKey: "key"},
			wantCode:  CodeINIDestinationExists,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := safeMigrationTestRoot(t)
			writeMigrationFile(t, root, "settings.ini", tt.content)
			path := filepath.Join(root, "settings.ini")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			err = NewEngine().Apply(root, []modules.MigrationOperationDef{tt.operation})
			if CodeOf(err) != tt.wantCode {
				t.Fatalf("Apply error = %v, code = %q, want %q", err, CodeOf(err), tt.wantCode)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("target changed on failure:\n%s\nwant:\n%s", after, before)
			}
			assertNoMigrationTemps(t, root)
		})
	}
}

func TestEngineDocumentOperationsRejectUnsafePaths(t *testing.T) {
	tests := []modules.MigrationOperationDef{
		{Type: "json-set", Path: "../outside.json", JSONPath: "$.value", Value: true},
		{Type: "ini-set", Path: `C:\outside.ini`, Section: "section", Key: "key", Value: "value"},
	}
	for _, operation := range tests {
		root := safeMigrationTestRoot(t)
		err := NewEngine().Apply(root, []modules.MigrationOperationDef{operation})
		if CodeOf(err) != CodeUnsafePath {
			t.Fatalf("%s error = %v, code = %q", operation.Type, err, CodeOf(err))
		}
	}
}
