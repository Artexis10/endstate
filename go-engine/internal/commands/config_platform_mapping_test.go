// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestLinuxCaptureMappedRestoreAndRevert(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux collection with a Darwin host seam")
	}
	root := t.TempDir()
	home := filepath.Join(root, "source")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	contents := []byte("{\"theme\":\"dark\"}\n")
	if err := os.WriteFile(filepath.Join(home, "settings.json"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"moduleSchemaVersion":3,"id":"apps.mapping","displayName":"Mapping fixture","sensitivity":"low","platforms":{"linux":{"realization":"endstate-restore","matches":{"pathExists":["${home}/settings.json"]},"capture":{"files":[{"source":"${home}/settings.json","dest":"settings.json"}]},"restore":[{"type":"copy","source":"./payload/settings.json","target":"${home}/settings.json"}]}}}`)
	mod, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	source := modules.ProjectModuleForPlatform(mod, "linux")
	planning := planCaptureConfig(map[string]*modules.Module{mod.ID: source}, nil, nil)
	manifestPath := filepath.Join(root, "capture.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"Mapping fixture","apps":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = bundle.CreateCaptureBundle(bundle.CaptureBundleRequest{ManifestPath: manifestPath, OutputPath: filepath.Join(root, "capture.zip"), Modules: planning.Modules, GenerationPlans: planning.GenerationPlans})
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := bundle.ExtractBundle(filepath.Join(root, "capture.zip"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(extracted)) })
	loaded, err := manifest.LoadManifest(extracted)
	if err != nil {
		t.Fatal(err)
	}
	capture := loaded.ConfigCaptures[0]
	var authored map[string]any
	if err := json.Unmarshal(raw, &authored); err != nil {
		t.Fatal(err)
	}
	authored["platforms"].(map[string]any)["darwin"] = map[string]any{
		"realization": "endstate-restore", "matches": map[string]any{"pathExists": []string{"${home}"}},
		"config":   map[string]any{"instanceDetectors": []any{map[string]any{"id": "default", "type": "path", "glob": "${home}"}}, "sets": []any{map[string]any{"id": "renamed", "generations": []any{map[string]any{"id": "g1", "order": 1, "restore": []any{map[string]any{"type": "copy", "source": "settings.json", "target": "${home}/settings.json"}}}}}}},
		"mappings": []any{map[string]any{"id": "linux-to-darwin", "from": map[string]any{"platform": "linux", "configSetId": "default", "generationId": "g1"}, "to": map[string]any{"platform": "darwin", "configSetId": "renamed", "generationId": "g1"}, "acceptsSourceFingerprints": []string{capture.SourceGenerationFingerprint}, "operations": []any{}, "validate": []any{map[string]any{"type": "json-parse", "path": "settings.json"}}}},
	}
	targetRaw, err := json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	targetModule, err := modules.ParseModuleJSON(targetRaw)
	if err != nil {
		t.Fatal(err)
	}
	targetModule = modules.ProjectModuleForPlatform(targetModule, "darwin")
	targetHome := filepath.Join(root, "target")
	if err := os.MkdirAll(targetHome, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", targetHome)
	old := []byte("{\"prior\":true}\r\n")
	targetPath := filepath.Join(targetHome, "settings.json")
	if err := os.WriteFile(targetPath, old, 0600); err != nil {
		t.Fatal(err)
	}
	oldRoot, oldGOOS, oldLoader := resolveRepoRootFn, captureGOOSFn, loadConfigRestoreCatalogFn
	resolveRepoRootFn = func() string { return root }
	captureGOOSFn = func() string { return "darwin" }
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{targetModule.ID: targetModule}, nil, nil
	}
	t.Cleanup(func() { resolveRepoRootFn = oldRoot; captureGOOSFn = oldGOOS; loadConfigRestoreCatalogFn = oldLoader })
	var result interface{}
	var restoreErr *envelope.Error
	eventOutput := captureStderr(t, func() {
		result, restoreErr = RunRestore(RestoreFlags{Manifest: extracted, EnableRestore: true, Events: "jsonl"})
	})
	envErr := restoreErr
	if envErr != nil {
		t.Fatalf("restore: %+v", envErr)
	}
	resolution := result.(*RestoreData).ConfigResolutions[0]
	if resolution.MappingID != "linux-to-darwin" || resolution.ConfigSetID != "default" || resolution.TargetConfigSetID != "renamed" || resolution.Status != planner.StatusRestored || resolution.Resolution != planner.ResolutionMigrate || resolution.SourcePlatform != "linux" || resolution.TargetPlatform != "darwin" {
		t.Fatalf("mapping resolution: %+v", resolution)
	}
	if got, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("mapped bytes %q, %v; want %q", got, err, contents)
	}
	found := false
	for _, event := range roundTripParseJSONL(t, eventOutput) {
		if event["event"] == "config-resolution" {
			found = event["mappingId"] == "linux-to-darwin" && event["targetConfigSetId"] == "renamed" && event["sourcePlatform"] == "linux" && event["targetPlatform"] == "darwin"
		}
	}
	if !found {
		t.Fatalf("mapping event lineage missing: %s", eventOutput)
	}
	intents, err := filepath.Glob(filepath.Join(root, "state", "config-restore", "v1", "transactions", "*", "journal", "intent.json"))
	if err != nil || len(intents) != 1 {
		t.Fatalf("mapped journal: %v, %v", intents, err)
	}
	intent, err := configrestore.ReadJournalIntent(context.Background(), filepath.Dir(filepath.Dir(intents[0])))
	if err != nil {
		t.Fatal(err)
	}
	lineage := intent.Lineage()
	if lineage.MappingID != "linux-to-darwin" || lineage.ConfigSetID != "default" || lineage.TargetConfigSetID != "renamed" || lineage.SourcePlatform != "linux" || lineage.TargetPlatform != "darwin" {
		t.Fatalf("journal lineage: %+v", lineage)
	}
	checks := []configvalidate.ResolvedValidation{}
	for _, check := range intent.Validations() {
		checks = append(checks, configvalidate.ResolvedValidation{Definition: modules.ValidationDef{Type: check.Type, Path: check.Path}, HostPath: check.HostPath})
	}
	if len(checks) != 1 {
		t.Fatalf("mapping validation not persisted: %+v", checks)
	}
	if err := configvalidate.ValidateResolved(checks); err != nil {
		t.Fatalf("verify committed target: %v", err)
	}
	if _, envErr := RunRevert(RevertFlags{}); envErr != nil {
		t.Fatalf("revert: %+v", envErr)
	}
	if got, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(got, old) {
		t.Fatalf("reverted bytes %q, %v; want %q", got, err, old)
	}
	// A staged JSON payload can become invalid when appended to existing JSON.
	// The mapping's committed validation must catch that and restore prior bytes.
	targetVariant := authored["platforms"].(map[string]any)["darwin"].(map[string]any)
	targetSet := targetVariant["config"].(map[string]any)["sets"].([]any)[0].(map[string]any)
	targetGeneration := targetSet["generations"].([]any)[0].(map[string]any)
	targetGeneration["restore"].([]any)[0].(map[string]any)["type"] = "append"
	targetRaw, err = json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	changedTarget, err := modules.ParseModuleJSON(targetRaw)
	if err != nil {
		t.Fatal(err)
	}
	targetModule = modules.ProjectModuleForPlatform(changedTarget, "darwin")
	failed, failedErr := RunRestore(RestoreFlags{Manifest: extracted, EnableRestore: true})
	if failed == nil {
		t.Fatalf("failed transaction missing result: %+v", failedErr)
	}
	failure := failed.(*RestoreData).ConfigResolutions[0]
	if failure.Status != planner.StatusRolledBack || failure.Reason == nil || *failure.Reason != planner.ReasonTargetValidationFailed {
		t.Fatalf("committed mapping validation bypassed: %+v, %+v", failure, failedErr)
	}
	if got, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(got, old) {
		t.Fatalf("failed mapping did not restore prior bytes: %q, %v", got, err)
	}

}
