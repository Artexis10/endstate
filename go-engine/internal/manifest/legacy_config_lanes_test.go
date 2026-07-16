// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"path/filepath"
	"testing"
)

func addValidLegacyLane(value map[string]any, captureID, moduleID string) {
	value["legacyConfigLanes"] = []any{map[string]any{
		"captureId":           captureID,
		"moduleId":            moduleID,
		"moduleSchemaVersion": 1,
		"payloadRoot":         "configs/" + captureID,
	}}
	value["configModules"] = []any{moduleID}
	value["restore"] = []any{map[string]any{
		"type":            "copy",
		"source":          "./configs/" + captureID + "/prefs.json",
		"target":          "~/.example/prefs.json",
		"fromModule":      moduleID,
		"legacyCaptureId": captureID,
	}}
}

func firstLegacyLane(value map[string]any) map[string]any {
	return value["legacyConfigLanes"].([]any)[0].(map[string]any)
}

func firstLegacyRestore(value map[string]any) map[string]any {
	return value["restore"].([]any)[0].(map[string]any)
}

func TestLoadManifestV1RejectsNonemptyV2LegacyAssociationFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"legacy lanes", func(value map[string]any) {
			value["legacyConfigLanes"] = []any{map[string]any{
				"captureId": "legacy-a", "moduleId": "legacy.example", "moduleSchemaVersion": 1, "payloadRoot": "configs/legacy-a",
			}}
		}},
		{"restore legacy capture id", func(value map[string]any) {
			value["restore"] = []any{map[string]any{
				"type": "registry-set", "key": `HKCU\Software\Vendor`, "legacyCaptureId": "legacy-a",
			}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := map[string]any{"version": 1, "apps": []any{}}
			tt.mutate(value)
			if _, err := LoadManifest(writeManifestValue(t, value)); ManifestDiagnosticCode(err) != ManifestDiagnosticInvalidConfigCapture {
				t.Fatalf("v1 association error = %T %v code=%q", err, err, ManifestDiagnosticCode(err))
			}
		})
	}
}

func TestLoadManifestV2CarriesStrictLegacyAssociation(t *testing.T) {
	value := validManifestV2Value("capture-a")
	addValidLegacyLane(value, "legacy-a", "legacy.example")
	loaded, err := LoadManifest(writeManifestValue(t, value))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.LegacyConfigLanes) != 1 {
		t.Fatalf("legacy lanes = %+v", loaded.LegacyConfigLanes)
	}
	lane := loaded.LegacyConfigLanes[0]
	if lane.CaptureID != "legacy-a" || lane.ModuleID != "legacy.example" || lane.ModuleSchemaVersion != 1 || lane.PayloadRoot != "configs/legacy-a" {
		t.Fatalf("legacy lane = %+v", lane)
	}
	if len(loaded.Restore) != 1 || loaded.Restore[0].LegacyCaptureID != lane.CaptureID || loaded.Restore[0].FromModule != lane.ModuleID {
		t.Fatalf("legacy restore association = %+v", loaded.Restore)
	}
}

func TestLoadManifestV2RejectsNullLegacyLaneArray(t *testing.T) {
	value := validManifestV2Value("capture-a")
	value["legacyConfigLanes"] = nil
	if _, err := LoadManifest(writeManifestValue(t, value)); ManifestDiagnosticCode(err) != ManifestDiagnosticInvalidConfigCapture {
		t.Fatalf("null legacy lanes error = %T %v code=%q", err, err, ManifestDiagnosticCode(err))
	}
}

func TestLoadManifestV2RejectsInvalidLegacyLaneMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing lane field", func(value map[string]any) { delete(firstLegacyLane(value), "payloadRoot") }},
		{"unstable lane id", func(value map[string]any) { firstLegacyLane(value)["captureId"] = "Legacy A" }},
		{"unstable module id", func(value map[string]any) { firstLegacyLane(value)["moduleId"] = "Legacy Example" }},
		{"wrong module schema", func(value map[string]any) { firstLegacyLane(value)["moduleSchemaVersion"] = 2 }},
		{"root outside configs", func(value map[string]any) { firstLegacyLane(value)["payloadRoot"] = "payload/legacy-a" }},
		{"root does not match lane id", func(value map[string]any) {
			firstLegacyLane(value)["payloadRoot"] = "configs/legacy-other"
			firstLegacyRestore(value)["source"] = "./configs/legacy-other/prefs.json"
		}},
		{"duplicate lane id", func(value map[string]any) {
			value["legacyConfigLanes"] = append(value["legacyConfigLanes"].([]any), map[string]any{
				"captureId": "legacy-a", "moduleId": "legacy.other", "moduleSchemaVersion": 1, "payloadRoot": "configs/legacy-other",
			})
			value["configModules"] = []any{"legacy.example", "legacy.other"}
			value["restore"] = append(value["restore"].([]any), map[string]any{
				"type": "registry-set", "key": `HKCU\Software\Other`, "fromModule": "legacy.other", "legacyCaptureId": "legacy-a",
			})
		}},
		{"duplicate lane module", func(value map[string]any) {
			value["legacyConfigLanes"] = append(value["legacyConfigLanes"].([]any), map[string]any{
				"captureId": "legacy-b", "moduleId": "legacy.example", "moduleSchemaVersion": 1, "payloadRoot": "configs/legacy-b",
			})
		}},
		{"id collides with generation capture", func(value map[string]any) {
			firstLegacyLane(value)["captureId"] = "capture-a"
			firstLegacyRestore(value)["legacyCaptureId"] = "capture-a"
		}},
		{"module also generation aware", func(value map[string]any) {
			firstLegacyLane(value)["moduleId"] = "apps.example"
			firstLegacyRestore(value)["fromModule"] = "apps.example"
			value["configModules"] = []any{"apps.example"}
		}},
		{"root equals generation root", func(value map[string]any) { firstLegacyLane(value)["payloadRoot"] = "configs/capture-a" }},
		{"root contains generation root", func(value map[string]any) { firstLegacyLane(value)["payloadRoot"] = "configs/capture-a/legacy" }},
		{"generation root contains legacy root", func(value map[string]any) {
			firstLegacyLane(value)["payloadRoot"] = "configs"
		}},
		{"restore missing association", func(value map[string]any) { delete(firstLegacyRestore(value), "legacyCaptureId") }},
		{"restore references unknown lane", func(value map[string]any) { firstLegacyRestore(value)["legacyCaptureId"] = "legacy-unknown" }},
		{"restore module mismatch", func(value map[string]any) { firstLegacyRestore(value)["fromModule"] = "legacy.other" }},
		{"restore source outside lane", func(value map[string]any) { firstLegacyRestore(value)["source"] = "./configs/other/prefs.json" }},
		{"registry restore missing association", func(value map[string]any) {
			value["restore"] = []any{map[string]any{
				"type": "registry-set", "key": `HKCU\Software\Vendor`, "fromModule": "legacy.example",
			}}
		}},
		{"unused lane", func(value map[string]any) { value["restore"] = []any{} }},
		{"config modules missing lane module", func(value map[string]any) { value["configModules"] = []any{} }},
		{"config modules has extra", func(value map[string]any) { value["configModules"] = []any{"legacy.example", "legacy.extra"} }},
		{"config modules duplicate", func(value map[string]any) { value["configModules"] = []any{"legacy.example", "legacy.example"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validManifestV2Value("capture-a")
			addValidLegacyLane(value, "legacy-a", "legacy.example")
			tt.mutate(value)
			_, err := LoadManifest(writeManifestValue(t, value))
			if ManifestDiagnosticCode(err) != ManifestDiagnosticInvalidConfigCapture {
				t.Fatalf("invalid legacy lane accepted/code mismatch: %T %v code=%q", err, err, ManifestDiagnosticCode(err))
			}
		})
	}
}

func TestLoadManifestV2RejectsWindowsADSPaths(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"generation payload entry", func(value map[string]any) {
			firstCapture(value)["payloadManifest"].([]any)[0].(map[string]any)["relativePath"] = "prefs/a.json:stream"
		}},
		{"legacy restore source", func(value map[string]any) {
			addValidLegacyLane(value, "legacy-a", "legacy.example")
			firstLegacyRestore(value)["source"] = "./configs/legacy-a/prefs.json:stream"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validManifestV2Value("capture-a")
			tt.mutate(value)
			if _, err := LoadManifest(writeManifestValue(t, value)); ManifestDiagnosticCode(err) != ManifestDiagnosticInvalidConfigCapture {
				t.Fatalf("ADS path error = %T %v code=%q", err, err, ManifestDiagnosticCode(err))
			}
		})
	}
}

func TestLoadManifestV2RegistryOnlyRestoreRemainsAssociated(t *testing.T) {
	value := validManifestV2Value("capture-a")
	addValidLegacyLane(value, "legacy-a", "legacy.example")
	value["restore"] = []any{map[string]any{
		"type": "registry-set", "key": `HKCU\Software\Vendor`, "valueName": "Theme", "fromModule": "legacy.example", "legacyCaptureId": "legacy-a",
	}}
	loaded, err := LoadManifest(writeManifestValue(t, value))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Restore) != 1 || loaded.Restore[0].Source != "" || loaded.Restore[0].LegacyCaptureID != "legacy-a" {
		t.Fatalf("registry association = %+v", loaded.Restore)
	}
}

func TestLoadManifestV2IncludesMergeLegacyAssociationsParentFirst(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.jsonc")
	parentPath := filepath.Join(dir, "parent.jsonc")
	child := validManifestV2Value("capture-b")
	addValidLegacyLane(child, "legacy-b", "legacy.beta")
	parent := validManifestV2Value("capture-a")
	addValidLegacyLane(parent, "legacy-a", "legacy.alpha")
	parent["includes"] = []any{"child.jsonc"}
	if err := writeJSONValue(childPath, child); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONValue(parentPath, parent); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.LegacyConfigLanes) != 2 || loaded.LegacyConfigLanes[0].CaptureID != "legacy-a" || loaded.LegacyConfigLanes[1].CaptureID != "legacy-b" {
		t.Fatalf("legacy lane merge order = %+v", loaded.LegacyConfigLanes)
	}
	if len(loaded.Restore) != 2 || loaded.Restore[0].LegacyCaptureID != "legacy-a" || loaded.Restore[1].LegacyCaptureID != "legacy-b" {
		t.Fatalf("legacy restore merge order = %+v", loaded.Restore)
	}
	if len(loaded.ConfigModules) != 2 || loaded.ConfigModules[0] != "legacy.alpha" || loaded.ConfigModules[1] != "legacy.beta" {
		t.Fatalf("config module merge order = %+v", loaded.ConfigModules)
	}
}
