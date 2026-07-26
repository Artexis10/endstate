// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestForValidationCaptureAdmitsOnlyExactSyntheticApp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captured.jsonc")
	raw := []byte(`{"version":1,"apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation","displayName":"PreSonus Studio One"}]}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("ordinary authored loader accepted private validation driver")
	}
	expected := App{ID: "studio-one", Refs: map[string]string{"windows": "studio-one"}, Driver: "validation", DisplayName: "PreSonus Studio One"}
	loaded, err := LoadManifestForValidationCapture(path, expected)
	if err != nil || len(loaded.Apps) != 1 || loaded.Apps[0].Driver != "validation" {
		t.Fatalf("validation capture load = %+v, %v", loaded, err)
	}
	expected.ID = "foreign"
	if _, err := LoadManifestForValidationCapture(path, expected); err == nil {
		t.Fatal("validation capture loader accepted mismatched descriptor identity")
	}
}

func TestLoadManifestForValidationCaptureRejectsIncludesBeforeReadingThem(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "external.jsonc")
	if err := os.WriteFile(external, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "captured.jsonc")
	raw := `{"version":1,"apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation"}],"includes":[` + mustValidationJSONString(t, external) + `]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := App{ID: "studio-one", Refs: map[string]string{"windows": "studio-one"}, Driver: "validation"}
	_, err := LoadManifestForValidationCapture(path, expected)
	if err == nil || !strings.Contains(err.Error(), "must not declare includes") {
		t.Fatalf("private validation include error = %v", err)
	}
	if strings.Contains(err.Error(), external) {
		t.Fatalf("private validation loader read or disclosed external include path: %v", err)
	}
}

func TestLoadManifestForValidationCaptureRejectsForeignAppAndRestoreShapes(t *testing.T) {
	expected := App{ID: "studio-one", Refs: map[string]string{"windows": "studio-one"}, Driver: "validation"}
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "foreign app", raw: `{"version":1,"apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation"},{"id":"foreign","refs":{"windows":"Foreign.App"}}]}`},
		{name: "foreign restore", raw: `{"version":1,"apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation"}],"restore":[{"type":"copy","source":"foreign.txt","target":"C:\\\\foreign.txt"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "captured.jsonc")
			if err := os.WriteFile(path, []byte(test.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifestForValidationCapture(path, expected); err == nil {
				t.Fatalf("private validation loader accepted %s", test.name)
			}
		})
	}
}

func mustValidationJSONString(t *testing.T, value string) string {
	t.Helper()
	quoted, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(quoted)
}
