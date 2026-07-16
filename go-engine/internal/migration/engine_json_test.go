// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestEngineJSONOperationsProduceDeterministicBytes(t *testing.T) {
	root := safeMigrationTestRoot(t)
	writeMigrationFile(t, root, "settings.json", `{"z":0,"large":900719925474099312345,"array":[1,2],"old":{"value":"move"},"settings":{}}`)
	operations := []modules.MigrationOperationDef{
		{Type: "json-set", Path: "settings.json", JSONPath: "$.settings.theme", Value: "system"},
		{Type: "json-set", Path: "settings.json", JSONPath: "$.array[1]", Value: 20},
		{Type: "json-move", Path: "settings.json", From: "$.old.value", To: "$.settings.moved"},
		{Type: "json-delete", Path: "settings.json", JSONPath: "$.z"},
	}
	if err := NewEngine().Apply(root, operations); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := "{\n  \"array\": [\n    1,\n    20\n  ],\n  \"large\": 900719925474099312345,\n  \"old\": {},\n  \"settings\": {\n    \"moved\": \"move\",\n    \"theme\": \"system\"\n  }\n}\n"
	assertMigrationFile(t, root, "settings.json", want)
	assertNoMigrationTemps(t, root)
}

func TestEngineJSONFailuresLeaveTargetBytesUnchanged(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		operation modules.MigrationOperationDef
		wantCode  ErrorCode
	}{
		{
			name: "malformed document", content: `{"broken":`,
			operation: modules.MigrationOperationDef{Type: "json-set", Path: "settings.json", JSONPath: "$.value", Value: true},
			wantCode:  CodeMalformedJSON,
		},
		{
			name: "invalid path grammar", content: `{"value":1}`,
			operation: modules.MigrationOperationDef{Type: "json-set", Path: "settings.json", JSONPath: "$..value", Value: true},
			wantCode:  CodeInvalidJSONPath,
		},
		{
			name: "missing parent", content: `{"value":1}`,
			operation: modules.MigrationOperationDef{Type: "json-set", Path: "settings.json", JSONPath: "$.missing.value", Value: true},
			wantCode:  CodeJSONParentMissing,
		},
		{
			name: "missing delete source", content: `{"value":1}`,
			operation: modules.MigrationOperationDef{Type: "json-delete", Path: "settings.json", JSONPath: "$.missing"},
			wantCode:  CodeJSONSourceMissing,
		},
		{
			name: "missing move source", content: `{"target":{}}`,
			operation: modules.MigrationOperationDef{Type: "json-move", Path: "settings.json", From: "$.missing", To: "$.target.value"},
			wantCode:  CodeJSONSourceMissing,
		},
		{
			name: "move destination exists", content: `{"source":1,"target":2}`,
			operation: modules.MigrationOperationDef{Type: "json-move", Path: "settings.json", From: "$.source", To: "$.target"},
			wantCode:  CodeJSONDestinationExists,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := safeMigrationTestRoot(t)
			writeMigrationFile(t, root, "settings.json", tt.content)
			before, err := os.ReadFile(filepath.Join(root, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			err = NewEngine().Apply(root, []modules.MigrationOperationDef{tt.operation})
			if CodeOf(err) != tt.wantCode {
				t.Fatalf("Apply error = %v, code = %q, want %q", err, CodeOf(err), tt.wantCode)
			}
			after, err := os.ReadFile(filepath.Join(root, "settings.json"))
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

func TestEngineJSONSetDeepCopiesOperationValue(t *testing.T) {
	root := safeMigrationTestRoot(t)
	writeMigrationFile(t, root, "settings.json", `{"settings":{}}`)
	value := map[string]any{"nested": []any{"original"}}
	operation := modules.MigrationOperationDef{
		Type: "json-set", Path: "settings.json", JSONPath: "$.settings.value", Value: value,
	}
	if err := NewEngine().Apply(root, []modules.MigrationOperationDef{operation}); err != nil {
		t.Fatal(err)
	}
	value["nested"].([]any)[0] = "mutated"
	assertMigrationFile(t, root, "settings.json", "{\n  \"settings\": {\n    \"value\": {\n      \"nested\": [\n        \"original\"\n      ]\n    }\n  }\n}\n")
}
