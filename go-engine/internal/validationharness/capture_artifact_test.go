// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestCaptureContractArtifactIsExactSchemaV1PayloadAndProvenance(t *testing.T) {
	runtime, entries := captureContractArtifactFixture(t)
	artifact := writeV2ArtifactZip(t, runtime.Root, "capture-contract.zip", entries)
	evidence, failure := inspectCaptureContractArtifact(runtime, artifact)
	if failure != nil {
		t.Fatalf("valid capture contract artifact: %+v", failure)
	}
	want := map[string]int{
		validationmatrix.AssertionCaptured: 1, validationmatrix.AssertionContent: 1,
		validationmatrix.AssertionPayload: 1, validationmatrix.AssertionProvenance: 1,
	}
	if evidence.ArtifactPath != artifact || !reflect.DeepEqual(evidence.AssertionCounts, want) {
		t.Fatalf("capture evidence = %+v", evidence)
	}
}

func TestCaptureContractArtifactRejectsForeignMembersFieldsPayloadAndAuthorityLeaks(t *testing.T) {
	runtime, entries := captureContractArtifactFixture(t)
	tests := []struct {
		name       string
		mutate     func(map[string][]byte)
		coordinate string
	}{
		{"foreign member", func(values map[string][]byte) { values["provenance/foreign.json"] = []byte("{}") }, "artifact"},
		{"foreign directory member", func(values map[string][]byte) { values["foreign/"] = []byte{} }, "artifact"},
		{"wrong payload", func(values map[string][]byte) { values["configs/mgba/config.ini"] = []byte("foreign") }, "capture.files[0]"},
		{"authority leak", func(values map[string][]byte) { values["metadata.json"] = []byte(runtime.Root) }, "artifact"},
		{"missing module provenance", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { delete(value, "configModules") })
		}, "manifest"},
		{"foreign verifier", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) {
				value["verify"] = []any{map[string]any{"type": "file-exists", "path": `%APPDATA%\mGBA\foreign.ini`}}
			})
		}, "manifest.verify"},
		{"missing app display name", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { delete(value["apps"].([]any)[0].(map[string]any), "displayName") })
		}, "manifest.apps"},
		{"empty app display name", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["displayName"] = "" })
		}, "manifest.apps"},
		{"wrong app display name", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["displayName"] = "Foreign" })
		}, "manifest.apps"},
		{"duplicate app display name", func(values map[string][]byte) {
			values["manifest.jsonc"] = []byte(strings.Replace(string(values["manifest.jsonc"]), `"displayName":"mGBA"`, `"displayName":"mGBA","displayName":"mGBA"`, 1))
		}, "manifest"},
		{"foreign app field", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["future"] = true })
		}, "manifest.apps"},
		{"wrong app id", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["id"] = "foreign" })
		}, "manifest.apps"},
		{"wrong app ref", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) {
				value["apps"].([]any)[0].(map[string]any)["refs"] = map[string]any{"windows": "Foreign.mGBA"}
			})
		}, "manifest.apps"},
	}
	for _, field := range []string{"restore", "legacyConfigLanes", "configCaptures"} {
		field := field
		tests = append(tests, struct {
			name       string
			mutate     func(map[string][]byte)
			coordinate string
		}{"explicit empty " + field, func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value[field] = []any{} })
		}, "manifest"})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneV2Entries(entries)
			test.mutate(candidate)
			artifact := writeV2ArtifactZip(t, runtime.Root, strings.ReplaceAll(test.name, " ", "-")+".zip", candidate)
			if _, failure := inspectCaptureContractArtifact(runtime, artifact); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v, want coordinate %q", failure, test.coordinate)
			}
		})
	}
}

func captureContractArtifactFixture(t *testing.T) (*scenarioRuntime, map[string][]byte) {
	t.Helper()
	plan := productionMGBACapturePlan(t)
	validationContext := fixtureValidationContext(t, plan.ModuleID, plan.ScenarioID)
	plan.context, plan.root = validationContext, validationContext.Root()
	resolved, err := validationContext.ResolveHostPath(plan.Targets[0].AuthoredSource, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	plan.Targets[0].Resolved = resolved
	_, mod, scenario := func() (*validationmatrix.Catalog, *modules.Module, validationmatrix.Scenario) {
		repo := filepath.Clean(filepath.Join("..", "..", ".."))
		catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return catalog, catalog.Modules["apps.mgba"], catalog.Records["apps.mgba"].Synthetic.Scenarios[0]
	}()
	runtime := &scenarioRuntime{Module: mod, Scenario: scenario, CapturePlan: plan, Root: plan.root, Inventory: plan.Inventory}
	captured := manifest.Manifest{
		Version: 1, Name: "captured", Captured: time.Now().UTC().Format(time.RFC3339),
		Apps:          []manifest.App{{ID: plan.Inventory.AppID, DisplayName: plan.Inventory.DisplayName, Refs: map[string]string{"windows": plan.Inventory.Ref}}},
		Verify:        []manifest.VerifyEntry{{Type: "file-exists", Path: plan.Verifiers[0].Path}},
		ConfigModules: []string{plan.ModuleID},
	}
	metadata := bundle.BundleMetadata{
		SchemaVersion: "1.0", CapturedAt: time.Now().UTC().Format(time.RFC3339), MachineName: "validation-host", EndstateVersion: "test", OS: "windows",
		ConfigModulesIncluded: []string{"mgba"}, ConfigModulesSkipped: []string{}, CaptureWarnings: []string{},
	}
	return runtime, map[string][]byte{
		"configs/":                {},
		"configs/mgba/":           {},
		"manifest.jsonc":          mustV2JSON(t, captured),
		"metadata.json":           mustV2JSON(t, metadata),
		"configs/mgba/config.ini": append([]byte(nil), plan.Targets[0].Content...),
	}
}

func mutateCaptureManifest(t *testing.T, entries map[string][]byte, mutate func(map[string]any)) {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(entries["manifest.jsonc"], &value); err != nil {
		t.Fatal(err)
	}
	mutate(value)
	entries["manifest.jsonc"] = mustV2JSON(t, value)
}
