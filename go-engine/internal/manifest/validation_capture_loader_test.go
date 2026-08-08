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

func TestLoadProjectedManifestForValidationCaptureAdmitsStrictFinalProjection(t *testing.T) {
	expected := App{
		ID: "wave-link", Refs: map[string]string{"windows": "wave-link"},
		Driver: "validation", DisplayName: "Elgato Wave Link",
	}
	path := filepath.Join(t.TempDir(), "captured.jsonc")
	raw := []byte(`{
  "version": 1,
  "apps": [{"id":"wave-link","refs":{"windows":"wave-link"},"driver":"validation","displayName":"Elgato Wave Link"}],
  "configModules": ["apps.wave-link"],
  "restore": [{"type":"copy","source":"./configs/apps.wave-link/settings.json","target":"%APPDATA%\\Elgato\\WaveLink\\settings.json","backup":true,"fromModule":"apps.wave-link"}],
  "verify": [{"type":"file-exists","path":"%APPDATA%\\Elgato\\WaveLink\\settings.json"}]
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifestForValidationCapture(path, expected); err == nil {
		t.Fatal("intermediate validation capture loader accepted a final config projection")
	}
	loaded, err := LoadProjectedManifestForValidationCapture(path, expected)
	if err != nil {
		t.Fatalf("projected validation capture load: %v", err)
	}
	if len(loaded.Apps) != 1 || len(loaded.ConfigModules) != 1 || len(loaded.Restore) != 1 || len(loaded.Verify) != 1 {
		t.Fatalf("projected validation capture = %+v", loaded)
	}

	wrong := expected
	wrong.DisplayName = "Foreign"
	if _, err := LoadProjectedManifestForValidationCapture(path, wrong); err == nil {
		t.Fatal("projected validation capture loader accepted mismatched descriptor identity")
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
