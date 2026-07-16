// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestLegacyCaptureIDIsStableDomainSeparatedAndPortable(t *testing.T) {
	got := LegacyCaptureID("apps.legacy")
	if got != LegacyCaptureID("apps.legacy") || !regexp.MustCompile(`^legacy-[0-9a-f]{64}$`).MatchString(got) {
		t.Fatalf("LegacyCaptureID = %q", got)
	}
	if got == LegacyCaptureID("apps.other") || got == strings.Replace(CaptureID("apps.legacy", "", ""), "capture-", "legacy-", 1) {
		t.Fatal("legacy identity is not module-scoped and domain-separated")
	}
}

func TestCreateCaptureBundleVersionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, dir string) CaptureBundleRequest
		wantVersion int
		wantSchema  string
		wantCapture int
		wantWarning int
	}{
		{"install only", func(t *testing.T, dir string) CaptureBundleRequest {
			return testCaptureBundleRequest(t, dir, nil, nil)
		}, 1, "1.0", 0, 0},
		{"v1 only", func(t *testing.T, dir string) CaptureBundleRequest {
			legacy := testLegacyCaptureModule(t, dir, "apps.legacy", "legacy")
			return testCaptureBundleRequest(t, dir, []*modules.Module{legacy}, nil)
		}, 1, "1.0", 0, 0},
		{"all v2 fail", func(t *testing.T, dir string) CaptureBundleRequest {
			plan := testGenerationCapturePlan(t, "apps.v2", "instance-missing", filepath.Join(dir, "missing-root"), false, false)
			return testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module}, []ConfigSetCapturePlan{plan})
		}, 1, "1.0", 0, 1},
		{"pure v2", func(t *testing.T, dir string) CaptureBundleRequest {
			root := filepath.Join(dir, "v2-root")
			writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("v2"))
			plan := testGenerationCapturePlan(t, "apps.v2", "instance-a", root, false, false)
			return testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module}, []ConfigSetCapturePlan{plan})
		}, 2, "2.0", 1, 0},
		{"mixed", func(t *testing.T, dir string) CaptureBundleRequest {
			root := filepath.Join(dir, "v2-root")
			writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("v2"))
			plan := testGenerationCapturePlan(t, "apps.v2", "instance-a", root, false, false)
			legacy := testLegacyCaptureModule(t, dir, "apps.legacy", "legacy")
			return testCaptureBundleRequest(t, dir, []*modules.Module{legacy, plan.Module}, []ConfigSetCapturePlan{plan})
		}, 2, "2.0", 1, 0},
		{"partial v2 success", func(t *testing.T, dir string) CaptureBundleRequest {
			root := filepath.Join(dir, "v2-root")
			writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("v2"))
			good := testGenerationCapturePlan(t, "apps.v2", "instance-good", root, false, false)
			bad := good
			bad.Instance.ID = "instance-missing"
			bad.Instance.Root = filepath.Join(dir, "missing-root")
			return testCaptureBundleRequest(t, dir, []*modules.Module{good.Module}, []ConfigSetCapturePlan{bad, good})
		}, 2, "2.0", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			request := tt.setup(t, dir)
			result, err := CreateCaptureBundle(request)
			if err != nil {
				t.Fatalf("CreateCaptureBundle: %v", err)
			}
			if result.ManifestVersion != tt.wantVersion || result.BundleSchemaVersion != tt.wantSchema || len(result.ConfigCaptures) != tt.wantCapture {
				t.Fatalf("result = %+v, want manifest=%d schema=%s captures=%d", result, tt.wantVersion, tt.wantSchema, tt.wantCapture)
			}
			loaded, metadata := loadCaptureBundle(t, request.OutputPath)
			if loaded.Version != tt.wantVersion || metadata.SchemaVersion != tt.wantSchema {
				t.Fatalf("artifact versions = manifest %#v metadata %+v", loaded.Version, metadata)
			}
			keys := captureMetadataKeys(t, request.OutputPath)
			_, hasManifestVersion := keys["manifestVersion"]
			_, hasConfigCaptures := keys["configCapturesIncluded"]
			if tt.wantVersion == 1 {
				if metadata.ManifestVersion != 0 || hasManifestVersion || hasConfigCaptures {
					t.Fatalf("metadata v1 leaked v2 keys: decoded=%+v raw=%v", metadata, keys)
				}
			} else if metadata.ManifestVersion != 2 || !hasManifestVersion || !hasConfigCaptures {
				t.Fatalf("metadata v2 missing generation keys: decoded=%+v raw=%v", metadata, keys)
			}
			if len(loaded.ConfigCaptures) != tt.wantCapture || len(metadata.ConfigCapturesIncluded) != tt.wantCapture {
				t.Fatalf("artifact captures = manifest %+v metadata %+v", loaded.ConfigCaptures, metadata.ConfigCapturesIncluded)
			}
			if len(metadata.CaptureWarnings) != tt.wantWarning || len(result.Diagnostics) != tt.wantWarning {
				t.Fatalf("diagnostic projection = typed %+v metadata %+v, want %d", result.Diagnostics, metadata.CaptureWarnings, tt.wantWarning)
			}
			for _, diagnostic := range result.Diagnostics {
				want := captureBundleDiagnosticWarning(diagnostic)
				if !containsString(metadata.CaptureWarnings, want) {
					t.Fatalf("metadata warnings %q missing typed diagnostic %q", metadata.CaptureWarnings, want)
				}
			}
		})
	}
}

func TestCreateCaptureBundleZeroFilePlanIsTypedSkipAndCannotEnableV2(t *testing.T) {
	dir := t.TempDir()
	plan := testGenerationCapturePlan(t, "apps.v2", "instance-empty", filepath.Join(dir, "root"), true, false)
	request := testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module}, []ConfigSetCapturePlan{plan})
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestVersion != 1 || len(result.Diagnostics) != 1 {
		t.Fatalf("empty capture result = %+v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Status != CaptureBundleStatusSkipped || diagnostic.Code != CaptureBundleDiagnosticEmpty || diagnostic.CaptureID != CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID) {
		t.Fatalf("empty capture diagnostic = %+v", diagnostic)
	}
	loaded, metadata := loadCaptureBundle(t, request.OutputPath)
	if len(loaded.ConfigCaptures) != 0 {
		t.Fatalf("empty generation capture enabled v2: %+v", loaded.ConfigCaptures)
	}
	if len(metadata.CaptureWarnings) != 1 || metadata.CaptureWarnings[0] != captureBundleDiagnosticWarning(diagnostic) {
		t.Fatalf("empty capture metadata warning = %q", metadata.CaptureWarnings)
	}
}

func TestCreateCaptureBundleIncludesPreplanningDiagnosticInResultAndMetadata(t *testing.T) {
	dir := t.TempDir()
	diagnostic := CaptureBundleDiagnostic{
		CaptureID:   CaptureID("apps.v2", "preferences", "instance-a"),
		ModuleID:    "apps.v2",
		ConfigSetID: "preferences",
		InstanceID:  "instance-a",
		Status:      CaptureBundleStatusSkipped,
		Code:        "unknown_generation",
		Detail:      "config set has no matching generation",
	}
	request := testCaptureBundleRequest(t, dir, nil, nil)
	request.PreplanningDiagnostics = []CaptureBundleDiagnostic{diagnostic}
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestVersion != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0] != diagnostic {
		t.Fatalf("preplanning result = %+v", result)
	}
	if len(result.CaptureWarnings) != 1 || result.CaptureWarnings[0] != captureBundleDiagnosticWarning(diagnostic) {
		t.Fatalf("preplanning result warnings = %q", result.CaptureWarnings)
	}
	_, metadata := loadCaptureBundle(t, request.OutputPath)
	if len(metadata.CaptureWarnings) != 1 || metadata.CaptureWarnings[0] != captureBundleDiagnosticWarning(diagnostic) {
		t.Fatalf("preplanning metadata warning = %q", metadata.CaptureWarnings)
	}
}

func TestCreateCaptureBundleReportsGenerationSecretExclusionsFromPublicationPass(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	secret := filepath.Join(root, "token.json")
	writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("prefs"))
	writeCaptureFile(t, secret, []byte("secret"))
	plan := testConfigSetCapturePlanWithSecrets(root, &modules.CaptureDef{
		Files: []modules.CaptureFile{{Source: `${instance.root}`, Dest: "preferences"}},
	}, &modules.SecretsDef{Files: []string{secret}})
	plan.Instance.Evidence = modules.InstanceEvidence{Type: "path", Path: root}
	request := testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module}, []ConfigSetCapturePlan{plan})
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.SensitiveExcluded != 1 || len(result.ConfigCaptures) != 1 || len(result.ConfigCaptures[0].PayloadManifest) != 1 {
		t.Fatalf("single-pass generation facts = %+v", result)
	}
}

func TestCreateCaptureBundlePureV2HasNoFlatFallbackAndIgnoresTopLevelV2Config(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "v2-root")
	writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("generation"))
	writeCaptureFile(t, filepath.Join(root, "top-level.json"), []byte("must-not-capture"))
	plan := testGenerationCapturePlan(t, "apps.v2", "instance-a", root, false, true)
	request := testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module}, []ConfigSetCapturePlan{plan})
	if _, err := CreateCaptureBundle(request); err != nil {
		t.Fatal(err)
	}
	loaded, _ := loadCaptureBundle(t, request.OutputPath)
	if len(loaded.Restore) != 0 || len(loaded.LegacyConfigLanes) != 0 || len(loaded.ConfigModules) != 0 || len(loaded.ConfigCaptures) != 1 {
		t.Fatalf("pure v2 structural isolation = restore %+v lanes %+v modules %+v captures %+v", loaded.Restore, loaded.LegacyConfigLanes, loaded.ConfigModules, loaded.ConfigCaptures)
	}
	entries := zipEntryNames(t, request.OutputPath)
	if containsSuffix(entries, "top-level.json") {
		t.Fatalf("schema-v2 top-level capture leaked into bundle: %v", entries)
	}
}

func TestCreateCaptureBundleMixedAssociatesOnlyLegacyFlatActions(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "v2-root")
	writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("generation"))
	plan := testGenerationCapturePlan(t, "apps.v2", "instance-a", root, false, false)
	legacy := testLegacyCaptureModule(t, dir, "apps.legacy", "legacy")
	request := testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module, legacy}, []ConfigSetCapturePlan{plan})
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	legacyID := LegacyCaptureID(legacy.ID)
	if len(result.LegacyModules) != 1 {
		t.Fatalf("legacy module collection results = %+v", result.LegacyModules)
	}
	legacyResult := result.LegacyModules[0]
	if legacyResult.ModuleID != legacy.ID || legacyResult.Status != LegacyCaptureStatusCaptured || legacyResult.FilesCaptured != 1 ||
		len(legacyResult.Paths) != 1 || legacyResult.Paths[0] != "configs/"+legacyID+"/legacy.json" {
		t.Fatalf("legacy module collection result = %+v", legacyResult)
	}
	loaded, _ := loadCaptureBundle(t, request.OutputPath)
	if len(loaded.LegacyConfigLanes) != 1 || loaded.LegacyConfigLanes[0].CaptureID != legacyID || loaded.LegacyConfigLanes[0].PayloadRoot != "configs/"+legacyID {
		t.Fatalf("legacy lanes = %+v", loaded.LegacyConfigLanes)
	}
	if len(loaded.Restore) != 1 || loaded.Restore[0].LegacyCaptureID != legacyID || loaded.Restore[0].FromModule != legacy.ID {
		t.Fatalf("legacy restores = %+v", loaded.Restore)
	}

	data, err := os.ReadFile(filepath.Join(filepath.Dir(extractCaptureBundle(t, request.OutputPath)), "manifest.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var frozen struct {
		Restore []struct {
			Source     string `json:"source"`
			FromModule string `json:"fromModule"`
		} `json:"restore"`
	}
	if err := json.Unmarshal(manifest.StripJsoncComments(data), &frozen); err != nil {
		t.Fatal(err)
	}
	if len(frozen.Restore) != 1 || frozen.Restore[0].FromModule != legacy.ID {
		t.Fatalf("frozen legacy flat actions = %+v", frozen.Restore)
	}
	generationRoot := loaded.ConfigCaptures[0].PayloadRoot
	source := strings.TrimPrefix(strings.ReplaceAll(frozen.Restore[0].Source, `\`, "/"), "./")
	if source == generationRoot || strings.HasPrefix(source, generationRoot+"/") {
		t.Fatalf("legacy decoder reaches generation root %q through %+v", generationRoot, frozen.Restore)
	}
}

func TestCreateCaptureBundlePerSetFailureIsIsolatedAndSideBySideIDsRemainDistinct(t *testing.T) {
	dir := t.TempDir()
	rootA := filepath.Join(dir, "root-a")
	rootB := filepath.Join(dir, "root-b")
	writeCaptureFile(t, filepath.Join(rootA, "prefs.json"), []byte("a"))
	writeCaptureFile(t, filepath.Join(rootB, "prefs.json"), []byte("b"))
	planA := testGenerationCapturePlan(t, "apps.v2", "instance-a", rootA, false, false)
	planB := planA
	planB.Instance.ID = "instance-b"
	planB.Instance.Root = rootB
	failed := planA
	failed.Instance.ID = "instance-failed"
	failed.Instance.Root = filepath.Join(dir, "missing")
	request := testCaptureBundleRequest(t, dir, []*modules.Module{planA.Module}, []ConfigSetCapturePlan{failed, planB, planA})
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ConfigCaptures) != 2 || result.ConfigCaptures[0].CaptureID == result.ConfigCaptures[1].CaptureID {
		t.Fatalf("side-by-side captures = %+v", result.ConfigCaptures)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Status != CaptureBundleStatusFailed || result.Diagnostics[0].CaptureID != CaptureID(failed.Module.ID, failed.Set.ID, failed.Instance.ID) {
		t.Fatalf("failed set diagnostic = %+v", result.Diagnostics)
	}
	loaded, _ := loadCaptureBundle(t, request.OutputPath)
	if len(loaded.ConfigCaptures) != 2 {
		t.Fatalf("successful captures lost after isolated failure: %+v", loaded.ConfigCaptures)
	}
	entries := zipEntryNames(t, request.OutputPath)
	failedRoot := "configs/" + CaptureID(failed.Module.ID, failed.Set.ID, failed.Instance.ID) + "/"
	for _, entry := range entries {
		if strings.HasPrefix(entry, failedRoot) {
			t.Fatalf("failed set payload survived: %v", entries)
		}
	}
}

func TestCreateCaptureBundleAtomicallyReplacesExistingArtifact(t *testing.T) {
	dir := t.TempDir()
	request := testCaptureBundleRequest(t, dir, nil, nil)
	old := []byte("old bundle must be replaced")
	if err := os.WriteFile(request.OutputPath, old, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCaptureBundle(request); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(request.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, old) {
		t.Fatal("existing bundle was not replaced")
	}
	loadCaptureBundle(t, request.OutputPath)
	assertNoCaptureBundleTemps(t, request.OutputPath)
}

func TestWriteCaptureZipAtomicallyPreservesExistingArtifactOnPublishFailure(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCaptureFile(t, filepath.Join(staging, "manifest.jsonc"), []byte(`{"version":1,"name":"capture","apps":[]}`))
	output := filepath.Join(dir, "capture.zip")
	old := []byte("existing artifact")
	if err := os.WriteFile(output, old, 0o644); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("injected publish failure")
	err := writeCaptureZipAtomicallyUsing(staging, output, func(_, _ string) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("writeCaptureZipAtomicallyUsing error = %v, want %v", err, wantErr)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, old) {
		t.Fatalf("existing artifact changed after failed publication: %q", got)
	}
	assertNoCaptureBundleTemps(t, output)
}

func TestWriteCaptureZipAtomicallyRejectsStagedLinkAndPreservesExistingArtifact(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside-secret.txt")
	writeCaptureFile(t, outside, []byte("must not enter bundle"))
	if err := os.Symlink(outside, filepath.Join(staging, "linked-secret.txt")); err != nil {
		t.Skipf("creating a symlink/reparse point requires local privilege: %v", err)
	}
	output := filepath.Join(dir, "capture.zip")
	old := []byte("existing artifact")
	if err := os.WriteFile(output, old, 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeCaptureZipAtomically(staging, output)
	if err == nil || !strings.Contains(err.Error(), "link or reparse point") {
		t.Fatalf("writeCaptureZipAtomically error = %v, want staged-link rejection", err)
	}
	got, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, old) {
		t.Fatalf("existing artifact changed after staged-link rejection: %q", got)
	}
	assertNoCaptureBundleTemps(t, output)
}

func TestWriteCaptureZipAtomicallyUsesUniqueTempsForConcurrentPublishers(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCaptureFile(t, filepath.Join(staging, "manifest.jsonc"), []byte(`{"version":1,"name":"capture","apps":[]}`))
	output := filepath.Join(dir, "capture.zip")
	const publishers = 8
	start := make(chan struct{})
	errs := make(chan error, publishers)
	wantErr := errors.New("injected publish failure")
	seen := make(map[string]struct{}, publishers)
	var seenMu sync.Mutex
	var ready sync.WaitGroup
	ready.Add(publishers)
	for i := 0; i < publishers; i++ {
		go func() {
			ready.Done()
			<-start
			err := writeCaptureZipAtomicallyUsing(staging, output, func(temporary, _ string) error {
				seenMu.Lock()
				seen[temporary] = struct{}{}
				seenMu.Unlock()
				return wantErr
			})
			errs <- err
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < publishers; i++ {
		if err := <-errs; !errors.Is(err, wantErr) {
			t.Fatalf("concurrent publication %d error = %v, want %v", i, err, wantErr)
		}
	}
	if len(seen) != publishers {
		t.Fatalf("concurrent publishers used %d unique temp files, want %d: %v", len(seen), publishers, seen)
	}
	assertNoCaptureBundleTemps(t, output)
}

func testCaptureBundleRequest(t *testing.T, dir string, mods []*modules.Module, plans []ConfigSetCapturePlan) CaptureBundleRequest {
	t.Helper()
	manifestPath := filepath.Join(dir, "input.jsonc")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"capture","apps":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return CaptureBundleRequest{
		ManifestPath:    manifestPath,
		OutputPath:      filepath.Join(dir, "capture.zip"),
		EndstateVersion: "test-version",
		Modules:         mods,
		GenerationPlans: plans,
	}
}

func testLegacyCaptureModule(t *testing.T, dir, moduleID, leaf string) *modules.Module {
	t.Helper()
	source := filepath.Join(dir, leaf+".json")
	writeCaptureFile(t, source, []byte("legacy"))
	return &modules.Module{
		ID:          moduleID,
		DisplayName: "Legacy",
		Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
			Source: source, Dest: "apps/" + strings.TrimPrefix(moduleID, "apps.") + "/" + leaf + ".json",
		}}},
		Restore: []modules.RestoreDef{{
			Type: "copy", Source: "./payload/apps/" + strings.TrimPrefix(moduleID, "apps.") + "/" + leaf + ".json", Target: "~/.legacy/" + leaf + ".json", Backup: true,
		}},
	}
}

func testGenerationCapturePlan(t *testing.T, moduleID, instanceID, root string, optional, topLevelTrap bool) ConfigSetCapturePlan {
	t.Helper()
	moduleValue := map[string]any{
		"moduleSchemaVersion": 2,
		"id":                  moduleID,
		"displayName":         "V2",
		"sensitivity":         "low",
		"matches":             map[string]any{},
		"config": map[string]any{
			"sets": []any{map[string]any{
				"id": "preferences",
				"generations": []any{map[string]any{
					"id": "g1", "order": 1,
					"capture": map[string]any{"files": []any{map[string]any{
						"source": `${instance.root}/prefs.json`, "dest": "prefs.json", "optional": optional,
					}}},
				}},
			}},
		},
	}
	if topLevelTrap {
		moduleValue["capture"] = map[string]any{"files": []any{map[string]any{
			"source": `${instance.root}/top-level.json`, "dest": "top-level.json",
		}}}
		moduleValue["restore"] = []any{map[string]any{
			"type": "copy", "source": "./payload/apps/v2/top-level.json", "target": "~/.v2/top-level.json", "backup": true,
		}}
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
		Module: mod, Set: set, Generation: generation,
		Instance: modules.ConfigInstance{
			ID: instanceID, ModuleID: moduleID, DetectorID: "path", Root: root,
			Version:  modules.NewVersionEvidence("27.4"),
			Evidence: modules.InstanceEvidence{Type: "path", Path: root},
		},
	}
}

func loadCaptureBundle(t *testing.T, zipPath string) (*manifest.Manifest, BundleMetadata) {
	t.Helper()
	manifestPath := extractCaptureBundle(t, zipPath)
	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest from bundle: %v", err)
	}
	metadataData, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata BundleMetadata
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		t.Fatal(err)
	}
	return loaded, metadata
}

func captureMetadataKeys(t *testing.T, zipPath string) map[string]json.RawMessage {
	t.Helper()
	manifestPath := extractCaptureBundle(t, zipPath)
	metadataData, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(metadataData, &keys); err != nil {
		t.Fatal(err)
	}
	return keys
}

func assertNoCaptureBundleTemps(t *testing.T, outputPath string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outputPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("fixed temp still exists: %s", outputPath+".tmp")
	}
	if len(matches) != 0 {
		t.Fatalf("temporary bundle files leaked: %v", matches)
	}
}

func extractCaptureBundle(t *testing.T, zipPath string) string {
	t.Helper()
	manifestPath, err := ExtractBundle(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(manifestPath)) })
	return manifestPath
}

func zipEntryNames(t *testing.T, zipPath string) []string {
	t.Helper()
	manifestPath := extractCaptureBundle(t, zipPath)
	var names []string
	err := filepath.Walk(filepath.Dir(manifestPath), func(hostPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(filepath.Dir(manifestPath), hostPath)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}

func containsSuffix(values []string, suffix string) bool {
	for _, value := range values {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
