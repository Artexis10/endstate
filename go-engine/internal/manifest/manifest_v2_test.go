// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifestValue(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.jsonc")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func validConfigCaptureValue(captureID string) map[string]any {
	return map[string]any{
		"captureId":   captureID,
		"moduleId":    "apps.example",
		"configSetId": "preferences",
		"sourceInstance": map[string]any{
			"id":                "instance-a",
			"detectorId":        "installed-package",
			"rawVersion":        "027.04.0",
			"normalizedVersion": "27.4",
			"evidence": map[string]any{
				"type":     "package",
				"backend":  "winget",
				"platform": "windows",
				"ref":      "Vendor.Example",
				"driver":   "winget",
			},
		},
		"sourceGeneration":            "g1",
		"sourceGenerationFingerprint": strings.Repeat("a", 64),
		"captureModule": map[string]any{
			"schemaVersion": 2,
			"contentHash":   strings.Repeat("b", 64),
			"snapshotPath":  "provenance/modules/apps.example.json",
		},
		"payloadRoot": "configs/" + captureID,
		"payloadManifest": []any{
			map[string]any{"relativePath": "prefs/a.json", "size": 0, "sha256": strings.Repeat("c", 64)},
			map[string]any{"relativePath": "prefs/b.json", "size": 12, "sha256": strings.Repeat("d", 64)},
		},
	}
}

func validManifestV2Value(captureIDs ...string) map[string]any {
	captures := make([]any, 0, len(captureIDs))
	for _, captureID := range captureIDs {
		captures = append(captures, validConfigCaptureValue(captureID))
	}
	return map[string]any{
		"version":        2,
		"apps":           []any{},
		"configCaptures": captures,
	}
}

func firstCapture(value map[string]any) map[string]any {
	return value["configCaptures"].([]any)[0].(map[string]any)
}

func TestLoadManifestVersionDispatch(t *testing.T) {
	validV2 := validManifestV2Value("capture-a")
	accepted := []struct {
		name  string
		value any
		want  int
	}{
		{"v1", map[string]any{"version": 1, "apps": []any{}}, 1},
		{"v2", validV2, 2},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			loaded, err := LoadManifest(writeManifestValue(t, tt.value))
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			if loaded.Version != tt.want {
				t.Fatalf("version = %#v, want %d", loaded.Version, tt.want)
			}
		})
	}

	rejected := []struct {
		name string
		raw  string
		code string
	}{
		{"missing", `{"apps":[]}`, "MISSING_VERSION"},
		{"string", `{"version":"2","apps":[]}`, "INVALID_VERSION_TYPE"},
		{"boolean", `{"version":true,"apps":[]}`, "INVALID_VERSION_TYPE"},
		{"fractional", `{"version":1.5,"apps":[]}`, "UNSUPPORTED_VERSION"},
		{"zero", `{"version":0,"apps":[]}`, "UNSUPPORTED_VERSION"},
		{"negative", `{"version":-1,"apps":[]}`, "UNSUPPORTED_VERSION"},
		{"future before typed apps", `{"version":3,"apps":"not-an-array"}`, "UNSUPPORTED_VERSION"},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.jsonc")
			if err := os.WriteFile(path, []byte(tt.raw), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadManifest(path)
			if err == nil {
				t.Fatal("LoadManifest accepted invalid version")
			}
			var manifestErr *ManifestValidationError
			if !errors.As(err, &manifestErr) {
				t.Fatalf("error type = %T, want *ManifestValidationError: %v", err, err)
			}
			if code := ManifestDiagnosticCode(err); code != tt.code {
				t.Fatalf("diagnostic code = %q, want %q: %v", code, tt.code, err)
			}
		})
	}
}

func TestLoadManifestV2CarriesProvenanceAndAllowsUnknownFields(t *testing.T) {
	value := validManifestV2Value("capture-a")
	value["futureTopLevel"] = map[string]any{"enabled": true}
	firstCapture(value)["futureCaptureField"] = "preserved-extensibility"

	loaded, err := LoadManifest(writeManifestValue(t, value))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(loaded.ConfigCaptures) != 1 {
		t.Fatalf("configCaptures = %+v", loaded.ConfigCaptures)
	}
	capture := loaded.ConfigCaptures[0]
	if capture.CaptureID != "capture-a" || capture.ModuleID != "apps.example" || capture.ConfigSetID != "preferences" {
		t.Fatalf("capture identity = %+v", capture)
	}
	if capture.SourceInstance.ID != "instance-a" || capture.SourceInstance.DetectorID != "installed-package" ||
		capture.SourceInstance.RawVersion != "027.04.0" || capture.SourceInstance.NormalizedVersion != "27.4" {
		t.Fatalf("source instance = %+v", capture.SourceInstance)
	}
	if capture.SourceInstance.Evidence == nil || capture.SourceInstance.Evidence.Backend != "winget" || capture.SourceInstance.Evidence.Ref != "Vendor.Example" {
		t.Fatalf("source evidence = %+v", capture.SourceInstance.Evidence)
	}
	if capture.SourceGeneration != "g1" || capture.SourceGenerationFingerprint != strings.Repeat("a", 64) {
		t.Fatalf("source generation = %+v", capture)
	}
	if capture.CaptureModule.SchemaVersion != 2 || capture.CaptureModule.ContentHash != strings.Repeat("b", 64) ||
		capture.CaptureModule.SnapshotPath != "provenance/modules/apps.example.json" {
		t.Fatalf("capture module = %+v", capture.CaptureModule)
	}
	if capture.PayloadRoot != "configs/capture-a" || len(capture.PayloadManifest) != 2 {
		t.Fatalf("payload = root %q manifest %+v", capture.PayloadRoot, capture.PayloadManifest)
	}
}

func TestLoadManifestV2AcceptsRawOnlyIrregularVersionEvidence(t *testing.T) {
	for _, rawVersion := range []string{"", "release-27"} {
		t.Run(rawVersion, func(t *testing.T) {
			value := validManifestV2Value("capture-a")
			source := firstCapture(value)["sourceInstance"].(map[string]any)
			source["rawVersion"] = rawVersion
			source["normalizedVersion"] = ""
			if _, err := LoadManifest(writeManifestValue(t, value)); err != nil {
				t.Fatalf("LoadManifest rejected raw-only version evidence: %v", err)
			}
		})
	}
}

func TestLoadManifestV2StrictValidation(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		mutate func(map[string]any)
	}{
		{"v1 rejects captures", "CONFIG_CAPTURES_REQUIRE_V2", func(v map[string]any) { v["version"] = 1 }},
		{"v2 requires capture provenance", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { v["configCaptures"] = []any{} }},
		{"missing required capture field", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { delete(firstCapture(v), "sourceGenerationFingerprint") }},
		{"missing raw version field", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { delete(firstCapture(v)["sourceInstance"].(map[string]any), "rawVersion") }},
		{"missing normalized version field", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			delete(firstCapture(v)["sourceInstance"].(map[string]any), "normalizedVersion")
		}},
		{"missing evidence field", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { delete(firstCapture(v)["sourceInstance"].(map[string]any), "evidence") }},
		{"numeric raw version requires canonical normalized version", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			firstCapture(v)["sourceInstance"].(map[string]any)["normalizedVersion"] = "027.04.0"
		}},
		{"irregular raw version requires empty normalized version", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			source := firstCapture(v)["sourceInstance"].(map[string]any)
			source["rawVersion"] = "release-27"
			source["normalizedVersion"] = "27"
		}},
		{"empty raw version requires empty normalized version", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			source := firstCapture(v)["sourceInstance"].(map[string]any)
			source["rawVersion"] = ""
			source["normalizedVersion"] = "27"
		}},
		{"unstable capture id", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["captureId"] = "Capture-A" }},
		{"unstable module id", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["moduleId"] = "Apps.Example" }},
		{"unstable config set id", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["configSetId"] = "Preferences" }},
		{"unstable detector id", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			firstCapture(v)["sourceInstance"].(map[string]any)["detectorId"] = "Installed Package"
		}},
		{"unstable generation id", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["sourceGeneration"] = "G1" }},
		{"uppercase fingerprint", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["sourceGenerationFingerprint"] = strings.Repeat("A", 64) }},
		{"short module hash", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["captureModule"].(map[string]any)["contentHash"] = "abc" }},
		{"wrong module schema", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["captureModule"].(map[string]any)["schemaVersion"] = 1 }},
		{"wrong isolated payload root", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["payloadRoot"] = "configs/other" }},
		{"traversing payload root", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["payloadRoot"] = "configs/capture-a/../other" }},
		{"absolute snapshot path", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			firstCapture(v)["captureModule"].(map[string]any)["snapshotPath"] = `C:\provenance\module.json`
		}},
		{"snapshot outside provenance modules", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			firstCapture(v)["captureModule"].(map[string]any)["snapshotPath"] = "provenance/apps.example.json"
		}},
		{"absolute payload entry", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			firstCapture(v)["payloadManifest"].([]any)[0].(map[string]any)["relativePath"] = "/prefs/a.json"
		}},
		{"traversing payload entry", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			firstCapture(v)["payloadManifest"].([]any)[0].(map[string]any)["relativePath"] = "../a.json"
		}},
		{"unsorted payload entries", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			entries := firstCapture(v)["payloadManifest"].([]any)
			entries[0], entries[1] = entries[1], entries[0]
		}},
		{"duplicate payload entries", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			entries := firstCapture(v)["payloadManifest"].([]any)
			entries[1].(map[string]any)["relativePath"] = entries[0].(map[string]any)["relativePath"]
		}},
		{"negative payload size", "INVALID_CONFIG_CAPTURE", func(v map[string]any) { firstCapture(v)["payloadManifest"].([]any)[0].(map[string]any)["size"] = -1 }},
		{"package evidence missing backend", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			delete(firstCapture(v)["sourceInstance"].(map[string]any)["evidence"].(map[string]any), "backend")
		}},
		{"legacy restore cannot enter generation payload", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			v["configModules"] = []any{"legacy.example"}
			v["restore"] = []any{map[string]any{
				"type": "copy", "source": "./configs/capture-a/prefs/a.json", "target": "~/.example/a.json", "fromModule": "legacy.example",
			}}
		}},
		{"legacy restore requires attribution", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			v["configModules"] = []any{"legacy.example"}
			v["restore"] = []any{map[string]any{
				"type": "copy", "source": "./configs/legacy.example/a.json", "target": "~/.example/a.json",
			}}
		}},
		{"legacy restore attribution must be listed", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			v["configModules"] = []any{"legacy.example"}
			v["restore"] = []any{map[string]any{
				"type": "copy", "source": "./configs/legacy.other/a.json", "target": "~/.example/a.json", "fromModule": "legacy.other",
			}}
		}},
		{"legacy restore cannot claim generation module", "INVALID_CONFIG_CAPTURE", func(v map[string]any) {
			v["configModules"] = []any{"apps.example"}
			v["restore"] = []any{map[string]any{
				"type": "copy", "source": "./configs/legacy.example/a.json", "target": "~/.example/a.json", "fromModule": "apps.example",
			}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validManifestV2Value("capture-a")
			tt.mutate(value)
			_, err := LoadManifest(writeManifestValue(t, value))
			if err == nil {
				t.Fatal("LoadManifest accepted invalid v2 provenance")
			}
			if code := ManifestDiagnosticCode(err); code != tt.code {
				t.Fatalf("diagnostic code = %q, want %q: %v", code, tt.code, err)
			}
		})
	}
}

func TestLoadManifestV2AllowsExplicitlyAssociatedLegacyRestore(t *testing.T) {
	value := validManifestV2Value("capture-a")
	value["configModules"] = []any{"legacy.example"}
	value["restore"] = []any{map[string]any{
		"type":       "copy",
		"source":     "./configs/legacy.example/a.json",
		"target":     "~/.example/a.json",
		"fromModule": "legacy.example",
	}}
	loaded, err := LoadManifest(writeManifestValue(t, value))
	if err != nil {
		t.Fatalf("LoadManifest rejected explicit legacy lane: %v", err)
	}
	if len(loaded.Restore) != 1 || loaded.Restore[0].FromModule != "legacy.example" {
		t.Fatalf("legacy lane association lost: %+v", loaded.Restore)
	}
}

func TestLoadManifestIncludesRequireCompatibleVersionAndMergeV2CapturesParentFirst(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.jsonc")
	parentPath := filepath.Join(dir, "parent.jsonc")

	child := validManifestV2Value("capture-b")
	child["apps"] = []any{map[string]any{"id": "child-app", "refs": map[string]any{"windows": "Child.App"}}}
	parent := validManifestV2Value("capture-a")
	parent["includes"] = []any{"child.jsonc"}
	if err := writeJSONValue(childPath, child); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONValue(parentPath, parent); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(parentPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(loaded.ConfigCaptures) != 2 || loaded.ConfigCaptures[0].CaptureID != "capture-a" || loaded.ConfigCaptures[1].CaptureID != "capture-b" {
		t.Fatalf("merged config capture order = %+v", loaded.ConfigCaptures)
	}

	delete(child, "version")
	if err := writeJSONValue(childPath, child); err != nil {
		t.Fatal(err)
	}
	if inherited, err := LoadManifest(parentPath); err != nil || len(inherited.ConfigCaptures) != 2 {
		t.Fatalf("versionless include did not inherit v2: captures=%+v err=%v", inherited, err)
	}

	child["version"] = 1
	if err := writeJSONValue(childPath, child); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(parentPath); ManifestDiagnosticCode(err) != "MANIFEST_VERSION_MISMATCH" {
		t.Fatalf("explicit incompatible include error = %v", err)
	}

	child = validManifestV2Value("capture-a")
	if err := writeJSONValue(childPath, child); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(parentPath); ManifestDiagnosticCode(err) != "INVALID_CONFIG_CAPTURE" {
		t.Fatalf("duplicate merged capture error = %v", err)
	}
}

func TestValidateProfileAcceptsManifestVersionTwo(t *testing.T) {
	result := ValidateProfile(writeManifestValue(t, validManifestV2Value("capture-a")))
	if !result.Valid {
		t.Fatalf("version 2 profile rejected: %+v", result.Errors)
	}
}

func TestValidateProfileRejectsMalformedManifestVersionTwo(t *testing.T) {
	value := validManifestV2Value("capture-a")
	delete(firstCapture(value), "sourceGenerationFingerprint")
	result := ValidateProfile(writeManifestValue(t, value))
	if result.Valid || len(result.Errors) != 1 || result.Errors[0].Code != "INVALID_CONFIG_CAPTURE" {
		t.Fatalf("malformed version 2 validation = valid %v errors %+v", result.Valid, result.Errors)
	}
}

func writeJSONValue(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
