// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// testGenerationCapturePlanWithValidate builds a schema-v2 capture plan whose
// single generation declares a `validate` block, mirroring real modules like
// windows-terminal ({ "type": "json-parse", "path": "settings.json" }).
func testGenerationCapturePlanWithValidate(t *testing.T, moduleID, instanceID, root string, validate []map[string]any) ConfigSetCapturePlan {
	t.Helper()
	moduleValue := map[string]any{
		"moduleSchemaVersion": 2,
		"id":                  moduleID,
		"displayName":         "Fixture App",
		"sensitivity":         "low",
		"matches":             map[string]any{},
		"config": map[string]any{
			"sets": []any{map[string]any{
				"id": "preferences",
				"generations": []any{map[string]any{
					"id": "g1", "order": 1,
					"capture": map[string]any{"files": []any{map[string]any{
						"source": `${instance.root}/settings.json`, "dest": "settings.json",
					}}},
					"validate": validate,
				}},
			}},
		},
	}
	data, err := json.Marshal(moduleValue)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	set := &mod.Config.Sets[0]
	generation := &set.Generations[0]
	return ConfigSetCapturePlan{
		Module:     mod,
		Set:        set,
		Generation: generation,
		Instance: modules.ConfigInstance{
			ID: instanceID, ModuleID: mod.ID, DetectorID: "path", Root: root,
			Version:  modules.NewVersionEvidence("1.0"),
			Evidence: modules.InstanceEvidence{Type: "path", Path: root},
		},
	}
}

// A captured payload that violates its module's declarative validation is kept
// (restore staging would refuse it anyway; dropping it would silently strip the
// user's settings) but produces a friendly, jargon-free capture warning.
func TestCreateCaptureBundleWarnsOnUnrestorablePayload(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "app-root")
	writeCaptureFile(t, filepath.Join(root, "settings.json"), []byte("this is not json"))

	plan := testGenerationCapturePlanWithValidate(t, "apps.fixture", "instance-a", root,
		[]map[string]any{{"type": "json-parse", "path": "settings.json"}})
	request := testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module}, []ConfigSetCapturePlan{plan})

	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}

	// Kept, not dropped: the set is still a v2 capture, with no failure diagnostic.
	if result.ManifestVersion != 2 || len(result.ConfigCaptures) != 1 {
		t.Fatalf("payload was not kept: manifestVersion=%d captures=%d", result.ManifestVersion, len(result.ConfigCaptures))
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unrestorable payload must warn, not fail: diagnostics=%+v", result.Diagnostics)
	}

	want := "Settings for Fixture App were saved but may not restore cleanly on another machine."
	if !containsString(result.CaptureWarnings, want) {
		t.Fatalf("capture warnings %+v missing friendly unrestorable warning %q", result.CaptureWarnings, want)
	}
	for _, warning := range result.CaptureWarnings {
		if strings.Contains(warning, "json-parse") || strings.Contains(warning, "captureId=") {
			t.Fatalf("capture warning leaked jargon to the user: %q", warning)
		}
	}

	_, metadata := loadCaptureBundle(t, request.OutputPath)
	if !containsString(metadata.CaptureWarnings, want) {
		t.Fatalf("artifact metadata warnings %+v missing %q", metadata.CaptureWarnings, want)
	}
}

// A captured payload that satisfies its module's declarative validation is
// captured with no validation warning.
func TestCreateCaptureBundleValidPayloadEmitsNoWarning(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "app-root")
	writeCaptureFile(t, filepath.Join(root, "settings.json"), []byte(`{"theme":"dark"}`))

	plan := testGenerationCapturePlanWithValidate(t, "apps.fixture", "instance-a", root,
		[]map[string]any{{"type": "json-parse", "path": "settings.json"}})
	request := testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module}, []ConfigSetCapturePlan{plan})

	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}
	if result.ManifestVersion != 2 || len(result.ConfigCaptures) != 1 {
		t.Fatalf("valid payload not captured: manifestVersion=%d captures=%d", result.ManifestVersion, len(result.ConfigCaptures))
	}
	if len(result.CaptureWarnings) != 0 {
		t.Fatalf("valid payload must not warn: warnings=%+v", result.CaptureWarnings)
	}
}
