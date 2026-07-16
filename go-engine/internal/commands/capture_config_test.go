// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/driver/brew"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

func TestPlanCaptureConfigStrictlyPartitionsLegacyAndPlansPackageAndPathInstances(t *testing.T) {
	dir := t.TempDir()
	pathRoot := filepath.Join(dir, "profiles")
	for _, name := range []string{"App 27.4", "App 28.1"} {
		if err := os.MkdirAll(filepath.Join(pathRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	legacy := &modules.Module{
		ID: "apps.legacy", DisplayName: "Legacy",
		Matches: modules.MatchCriteria{Winget: []string{"Legacy.App"}},
		Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "legacy", Dest: "legacy.json"}}},
	}
	packageModule := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.vendor", WingetRef: "Vendor.App",
		Detectors:           []modules.InstanceDetectorDef{{ID: "installed", Type: "package"}},
		Sets:                []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{{ID: "g1", Capture: true}}}},
		TopLevelCaptureTrap: true,
	})
	unmatchedPackageModule := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.unmatched", WingetRef: "Unmatched.App",
		Detectors: []modules.InstanceDetectorDef{{ID: "installed", Type: "package"}},
		Sets:      []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{{ID: "g1", Capture: true}}}},
	})
	pathModule := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.path-only", PathMatch: pathRoot,
		Detectors: []modules.InstanceDetectorDef{{
			ID: "profiles", Type: "path", Glob: filepath.Join(pathRoot, "App *"), VersionPattern: `^App (?P<version>[0-9.]+)$`,
		}},
		Sets: []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{
			{ID: "g1", Range: ">=27 <28", Capture: true},
			{ID: "g2", Range: ">=28 <29", Capture: true},
		}}},
	})
	apps := []manifest.App{
		{ID: "legacy", Refs: map[string]string{"windows": "Legacy.App"}, Installed: true, InstalledVersion: "1", Backend: "winget"},
		{ID: "vendor", Refs: map[string]string{"windows": "Vendor.App"}, Installed: true, InstalledVersion: "27.4", Backend: "winget"},
		{ID: "vendor-desired-only", Refs: map[string]string{"windows": "Vendor.App"}, Installed: false, Version: "99"},
		{ID: "unrelated", Refs: map[string]string{"windows": "Other.App"}, Installed: true, InstalledVersion: "88", Backend: "winget"},
	}

	got := planCaptureConfig(map[string]*modules.Module{
		legacy.ID: legacy, packageModule.ID: packageModule, pathModule.ID: pathModule, unmatchedPackageModule.ID: unmatchedPackageModule,
	}, apps, nil)
	if len(got.LegacyModules) != 1 || got.LegacyModules[0] != legacy {
		t.Fatalf("legacy partition = %+v", got.LegacyModules)
	}
	if len(got.GenerationPlans) != 3 || len(got.PreplanningDiagnostics) != 0 {
		t.Fatalf("generation planning = plans %+v diagnostics %+v", got.GenerationPlans, got.PreplanningDiagnostics)
	}
	if containsCapturePlanModule(got.GenerationPlans, packageModule.ID, "", "") != 1 {
		t.Fatalf("package plans = %+v", got.GenerationPlans)
	}
	packagePlan := capturePlanForModule(t, got.GenerationPlans, packageModule.ID)
	if packagePlan.Instance.Version.Raw != "27.4" || packagePlan.Instance.Evidence.Ref != "Vendor.App" || packagePlan.Instance.Evidence.Backend != "winget" {
		t.Fatalf("package evidence = %+v", packagePlan.Instance)
	}
	if containsCapturePlanModule(got.GenerationPlans, pathModule.ID, "preferences", "g1") != 1 ||
		containsCapturePlanModule(got.GenerationPlans, pathModule.ID, "preferences", "g2") != 1 {
		t.Fatalf("side-by-side path plans = %+v", got.GenerationPlans)
	}
	if len(got.Modules) != 3 || got.Modules[0].ID != "apps.legacy" || got.Modules[1].ID != "apps.path-only" || got.Modules[2].ID != "apps.vendor" {
		t.Fatalf("bundle module partition/order = %+v", moduleIDs(got.Modules))
	}
}

func TestPlanCaptureConfigRefusesUnknownAmbiguousAndSelectedGenerationWithoutCapture(t *testing.T) {
	dir := t.TempDir()
	instanceRoot := filepath.Join(dir, "App 27")
	if err := os.MkdirAll(instanceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.refusal", PathMatch: instanceRoot,
		Detectors: []modules.InstanceDetectorDef{{
			ID: "profiles", Type: "path", Glob: filepath.Join(dir, "App *"), VersionPattern: `^App (?P<version>[0-9.]+)$`,
		}},
		Sets: []testCaptureSet{
			{ID: "unknown", Generations: []testCaptureGeneration{{ID: "g1", Range: ">=40 <41", Capture: true}}},
			{ID: "ambiguous", Generations: []testCaptureGeneration{{ID: "g1", Capture: true}, {ID: "g2", Capture: true}}},
			{ID: "no-capture", Generations: []testCaptureGeneration{{ID: "g1"}}},
		},
	})

	got := planCaptureConfig(map[string]*modules.Module{mod.ID: mod}, nil, nil)
	if len(got.GenerationPlans) != 0 || len(got.PreplanningDiagnostics) != 3 {
		t.Fatalf("refusal planning = plans %+v diagnostics %+v", got.GenerationPlans, got.PreplanningDiagnostics)
	}
	wantCodes := []string{CapturePlanningAmbiguousGeneration, CapturePlanningNoCapture, CapturePlanningUnknownGeneration}
	gotCodes := make([]string, 0, len(got.PreplanningDiagnostics))
	seenCaptureIDs := map[string]bool{}
	for _, diagnostic := range got.PreplanningDiagnostics {
		gotCodes = append(gotCodes, diagnostic.Code)
		if diagnostic.Status != bundle.CaptureBundleStatusSkipped || diagnostic.CaptureID == "" || seenCaptureIDs[diagnostic.CaptureID] {
			t.Fatalf("unstable refusal diagnostic = %+v", diagnostic)
		}
		seenCaptureIDs[diagnostic.CaptureID] = true
	}
	sort.Strings(gotCodes)
	sort.Strings(wantCodes)
	if !equalStrings(gotCodes, wantCodes) {
		t.Fatalf("diagnostic codes = %v, want %v", gotCodes, wantCodes)
	}
	if len(got.Candidates) != 3 {
		t.Fatalf("refused config-set candidates = %+v", got.Candidates)
	}
}

func TestPlanCaptureConfigProjectsCatalogDiagnosticWithoutLegacyFallback(t *testing.T) {
	diagnostic := modules.CatalogDiagnostic{
		Code: "INVALID_CONFIG_GENERATION", Severity: "error", ModuleID: "apps.invalid", FilePath: "invalid/module.jsonc", Message: "invalid generation",
	}
	got := planCaptureConfig(nil, []manifest.App{{ID: "invalid", Installed: true}}, []modules.CatalogDiagnostic{diagnostic})
	if len(got.Modules) != 0 || len(got.GenerationPlans) != 0 || len(got.PreplanningDiagnostics) != 1 {
		t.Fatalf("catalog refusal = %+v", got)
	}
	projected := got.PreplanningDiagnostics[0]
	if projected.ModuleID != diagnostic.ModuleID || projected.Code != diagnostic.Code || projected.Status != bundle.CaptureBundleStatusFailed || projected.Detail != diagnostic.Message {
		t.Fatalf("catalog diagnostic projection = %+v", projected)
	}
	if unrelated := planCaptureConfig(nil, nil, []modules.CatalogDiagnostic{diagnostic}); len(unrelated.PreplanningDiagnostics) != 0 {
		t.Fatalf("unrelated catalog diagnostic leaked into capture = %+v", unrelated.PreplanningDiagnostics)
	}
}

func TestPlanCaptureConfigProjectsParseDiagnosticForInstalledModule(t *testing.T) {
	modulesRoot := t.TempDir()
	moduleDir := filepath.Join(modulesRoot, "photoshop")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(moduleDir, "module.jsonc"),
		[]byte(`{
			"moduleSchemaVersion": 2,
			"id": "apps.photoshop",
			"displayName": "Adobe Photoshop",
			"sensitivity": "low",
			"matches": {"winget": ["Adobe.Photoshop"]},
			"unexpected": true
		}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, diagnostics, err := modules.LoadCatalogWithDiagnostics(modulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("parse diagnostics = %+v, want one", diagnostics)
	}

	got := planCaptureConfig(
		nil,
		[]manifest.App{{
			ID: "adobe-photoshop", Refs: map[string]string{"windows": "Adobe.Photoshop"}, Installed: true,
		}},
		diagnostics,
	)
	if len(got.PreplanningDiagnostics) != 1 {
		t.Fatalf("installed module parse diagnostic was suppressed: %+v", got)
	}
	projected := got.PreplanningDiagnostics[0]
	if projected.ModuleID != "apps.photoshop" || projected.Code != modules.DiagnosticInvalidJSON {
		t.Fatalf("projected parse diagnostic = %+v", projected)
	}
}

func TestPlanCaptureConfigProjectsValidationDiagnosticByWingetRef(t *testing.T) {
	modulesRoot := t.TempDir()
	moduleDir := filepath.Join(modulesRoot, "vscode")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(moduleDir, "module.jsonc"),
		[]byte(`{
			"moduleSchemaVersion": 2,
			"id": "apps.vscode",
			"sensitivity": "low",
			"matches": {"winget": ["Microsoft.VisualStudioCode"]}
		}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, diagnostics, err := modules.LoadCatalogWithDiagnostics(modulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("validation diagnostics = %+v, want one", diagnostics)
	}
	apps := []manifest.App{{
		ID:        "microsoft-visualstudiocode",
		Refs:      map[string]string{"windows": "Microsoft.VisualStudioCode"},
		Installed: true,
	}}
	got := planCaptureConfig(nil, apps, diagnostics)
	if len(got.PreplanningDiagnostics) != 1 {
		t.Fatalf("winget-associated catalog diagnostic was suppressed: %+v", got)
	}
	if got.PreplanningDiagnostics[0].ModuleID != "apps.vscode" {
		t.Fatalf("projected catalog diagnostic = %+v", got.PreplanningDiagnostics[0])
	}
}

func TestPlanCaptureConfigFailsVisibleForUnassociableMalformedModule(t *testing.T) {
	modulesRoot := t.TempDir()
	moduleDir := filepath.Join(modulesRoot, "vscode")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(moduleDir, "module.jsonc"),
		[]byte(`{"id":"apps.vscode","matches":{"winget":["Microsoft.VisualStudioCode"]}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, diagnostics, err := modules.LoadCatalogWithDiagnostics(modulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || !diagnostics[0].AssociationUnknown {
		t.Fatalf("malformed diagnostics = %+v, want unknown association", diagnostics)
	}
	got := planCaptureConfig(nil, nil, diagnostics)
	if len(got.PreplanningDiagnostics) != 1 {
		t.Fatalf("unassociable malformed-module diagnostic was suppressed: %+v", got)
	}
	if got.PreplanningDiagnostics[0].ModuleID != "apps.vscode" || got.PreplanningDiagnostics[0].Code != modules.DiagnosticInvalidJSON {
		t.Fatalf("projected malformed diagnostic = %+v", got.PreplanningDiagnostics[0])
	}
}

func TestPlanCaptureConfigProjectsRejectedPathOnlyModule(t *testing.T) {
	modulesRoot := t.TempDir()
	moduleDir := filepath.Join(modulesRoot, "studio-one")
	instanceRoot := filepath.Join(t.TempDir(), "Studio One 7")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(instanceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	declaration, err := json.Marshal(map[string]any{
		"moduleSchemaVersion": 2,
		"id":                  "apps.studio-one",
		"displayName":         "PreSonus Studio One",
		"sensitivity":         "low",
		"matches":             map[string]any{"pathExists": []string{instanceRoot}},
		"config": map[string]any{
			"instanceDetectors": []map[string]any{{
				"id": "versions", "type": "path", "glob": filepath.Join(filepath.Dir(instanceRoot), "Studio One *"),
			}},
			"sets": []map[string]any{{
				"id":          "preferences",
				"generations": []map[string]any{{"id": "g1", "order": 1}},
				"migrations": []map[string]any{{
					"from": "g1", "to": "g2", "validate": []any{},
					"operations": []map[string]any{{"type": "json-set", "path": "settings.json", "unexpected": true}},
				}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "module.jsonc"), declaration, 0o644); err != nil {
		t.Fatal(err)
	}

	_, diagnostics, err := modules.LoadCatalogWithDiagnostics(modulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].AssociationUnknown || len(diagnostics[0].InstanceDetectors) != 1 {
		t.Fatalf("path-only diagnostics = %+v, want preserved detector", diagnostics)
	}

	got := planCaptureConfig(nil, nil, diagnostics)
	if len(got.PreplanningDiagnostics) != 1 {
		t.Fatalf("rejected path-only module diagnostic was suppressed: %+v", got)
	}
	if got.PreplanningDiagnostics[0].ModuleID != "apps.studio-one" {
		t.Fatalf("projected path-only diagnostic = %+v", got.PreplanningDiagnostics[0])
	}
}

func TestCaptureResultLegacyJSONOmitsGenerationFieldsAndKeepsWarningsArray(t *testing.T) {
	encoded, err := json.Marshal(CaptureResult{
		AppsIncluded: []CaptureApp{}, ConfigModules: []CaptureModuleResult{}, ConfigModuleMap: map[string]string{},
		ConfigsIncluded: []string{}, ConfigsSkipped: []string{}, ConfigsCaptureErrors: []string{}, CaptureWarnings: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"bundleSchemaVersion", "manifestVersion", "configCapture"} {
		if _, exists := wire[key]; exists {
			t.Fatalf("legacy capture JSON unexpectedly contains %q: %s", key, encoded)
		}
	}
	if got := string(wire["captureWarnings"]); got != "[]" {
		t.Fatalf("legacy captureWarnings = %s, want []", got)
	}
}

func TestCaptureResultSchemaV1ModuleJSONKeepsOnlyExistingModuleMetadata(t *testing.T) {
	summary := CaptureConfigSummary{Modules: []CaptureConfigModule{{ID: "apps.legacy", DisplayName: "Legacy", Entries: 1, Files: []string{"prefs.json"}}}}
	encoded, err := json.Marshal(CaptureResult{CaptureWarnings: []string{}, ConfigCapture: &summary})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	var configWire map[string]json.RawMessage
	if err := json.Unmarshal(wire["configCapture"], &configWire); err != nil {
		t.Fatal(err)
	}
	if len(configWire) != 1 || string(configWire["modules"]) == "null" {
		t.Fatalf("schema-v1 configCapture shape = %s", wire["configCapture"])
	}
	for _, key := range []string{"configSets", "counts", "diagnostics"} {
		if _, exists := configWire[key]; exists {
			t.Fatalf("schema-v1 configCapture unexpectedly contains %q: %s", key, wire["configCapture"])
		}
	}
}

func TestCaptureResultGenerationJSONUsesLockedConfigSetShape(t *testing.T) {
	reason := CapturePlanningUnknownGeneration
	summary := CaptureConfigSummary{
		Modules: []CaptureConfigModule{},
		ConfigSets: []CaptureConfigSetResult{
			{CaptureID: "captured", ModuleID: "apps.v2", ConfigSetID: "prefs", DisplayName: "Preferences", SourceGeneration: "g1", SourceGenerationFingerprint: "sha256:g1", Status: CaptureConfigStatusCaptured},
			{CaptureID: "refused", ModuleID: "apps.v2", ConfigSetID: "prefs", DisplayName: "Preferences", Status: CaptureConfigStatusSkipped, Reason: &reason},
		},
		Diagnostics:     []bundle.CaptureBundleDiagnostic{},
		generationAware: true,
	}
	encoded, err := json.Marshal(CaptureResult{
		BundleSchemaVersion: "2.0", ManifestVersion: 2, CaptureWarnings: []string{}, ConfigCapture: &summary,
	})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		CaptureWarnings []string `json:"captureWarnings"`
		ConfigCapture   struct {
			Modules     json.RawMessage              `json:"modules"`
			ConfigSets  []map[string]json.RawMessage `json:"configSets"`
			Diagnostics json.RawMessage              `json:"diagnostics"`
		} `json:"configCapture"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.CaptureWarnings == nil || string(wire.ConfigCapture.Modules) == "null" || wire.ConfigCapture.ConfigSets == nil || string(wire.ConfigCapture.Diagnostics) == "null" {
		t.Fatalf("generation arrays must be non-null: %s", encoded)
	}
	if len(wire.ConfigCapture.ConfigSets) != 2 {
		t.Fatalf("configSets = %s", encoded)
	}
	captured := wire.ConfigCapture.ConfigSets[0]
	if string(captured["sourceGenerationFingerprint"]) != `"sha256:g1"` {
		t.Fatalf("sourceGenerationFingerprint = %s", captured["sourceGenerationFingerprint"])
	}
	if _, exists := captured["fingerprint"]; exists {
		t.Fatalf("deprecated fingerprint key present: %s", encoded)
	}
	if string(captured["reason"]) != "null" || string(wire.ConfigCapture.ConfigSets[1]["reason"]) != `"`+reason+`"` {
		t.Fatalf("captured/refusal reason shape = %s", encoded)
	}
}

func TestFinalizeCaptureConfigBuildsSideBySideV2SummaryFromWrittenPayloadsWithoutRecollection(t *testing.T) {
	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles")
	for _, name := range []string{"App 27.4", "App 28.1"} {
		root := filepath.Join(profiles, name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "prefs.json"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.side-by-side", PathMatch: profiles,
		Detectors: []modules.InstanceDetectorDef{{
			ID: "profiles", Type: "path", Glob: filepath.Join(profiles, "App *"), VersionPattern: `^App (?P<version>[0-9.]+)$`,
		}},
		Sets: []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{
			{ID: "g1", Range: ">=27 <28", Capture: true},
			{ID: "g2", Range: ">=28 <29", Capture: true},
		}}},
	})
	withCaptureCatalogLoader(t, map[string]*modules.Module{mod.ID: mod}, nil)
	manifestPath := writeCaptureInputManifest(t, dir)

	originalCreate := createCaptureBundleFn
	createCalls := 0
	createCaptureBundleFn = func(request bundle.CaptureBundleRequest) (*bundle.CaptureBundleResult, error) {
		createCalls++
		result, err := bundle.CreateCaptureBundle(request)
		if err == nil {
			if removeErr := os.RemoveAll(profiles); removeErr != nil {
				t.Fatalf("remove source after bundle publication: %v", removeErr)
			}
		}
		return result, err
	}
	t.Cleanup(func() { createCaptureBundleFn = originalCreate })

	got, err := finalizeCaptureConfig(captureConfigFinalizeRequest{
		Flags: CaptureFlags{Out: manifestPath}, ManifestPath: manifestPath, Apps: []manifest.App{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 || got.OutputFormat != "zip" || got.BundleSchemaVersion != "2.0" || got.ManifestVersion != 2 {
		t.Fatalf("v2 finalization = calls %d result %+v", createCalls, got)
	}
	if got.OutputPath != strings.TrimSuffix(manifestPath, ".jsonc")+".zip" {
		t.Fatalf("output path = %q", got.OutputPath)
	}
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("intermediate manifest survived v2 bundle: %v", statErr)
	}
	if len(got.ConfigCapture.ConfigSets) != 2 || got.ConfigCapture.Counts.Total != 2 || got.ConfigCapture.Counts.Captured != 2 || len(got.ConfigCapture.Diagnostics) != 0 {
		t.Fatalf("side-by-side config summary = %+v", got.ConfigCapture)
	}
	if got.ConfigCapture.ConfigSets[0].CaptureID == got.ConfigCapture.ConfigSets[1].CaptureID {
		t.Fatalf("side-by-side rows share capture id: %+v", got.ConfigCapture.ConfigSets)
	}
	for _, row := range got.ConfigCapture.ConfigSets {
		if row.Status != CaptureConfigStatusCaptured || row.FilesCaptured != 1 || row.Reason != nil || row.SourceInstance.ID == "" || row.CaptureModuleRevision != mod.Revision {
			t.Fatalf("captured config-set row = %+v", row)
		}
	}
	loaded := loadManifestFromCaptureZip(t, got.OutputPath)
	if loaded.Version != 2 || len(loaded.ConfigCaptures) != 2 {
		t.Fatalf("written v2 manifest = version %#v captures %+v", loaded.Version, loaded.ConfigCaptures)
	}
}

func TestFinalizeCaptureConfigDefaultsInstallOnlyToV1Zip(t *testing.T) {
	dir := t.TempDir()
	withCaptureCatalogLoader(t, map[string]*modules.Module{}, nil)
	manifestPath := writeCaptureInputManifest(t, dir)
	got, err := finalizeCaptureConfig(captureConfigFinalizeRequest{
		Flags: CaptureFlags{Out: manifestPath}, ManifestPath: manifestPath, Apps: []manifest.App{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.OutputFormat != "zip" || got.BundleSchemaVersion != "1.0" || got.ManifestVersion != 1 || got.OutputPath != strings.TrimSuffix(manifestPath, ".jsonc")+".zip" {
		t.Fatalf("install-only finalization = %+v", got)
	}
	if got.ConfigCapture.ConfigSets == nil || got.ConfigCapture.Modules == nil || got.ConfigCapture.Diagnostics == nil || got.CaptureWarnings == nil {
		t.Fatalf("install-only collections contain nil: modules=%t sets=%t diagnostics=%t warnings=%t result=%+v",
			got.ConfigCapture.Modules == nil, got.ConfigCapture.ConfigSets == nil, got.ConfigCapture.Diagnostics == nil, got.CaptureWarnings == nil, got)
	}
	loaded := loadManifestFromCaptureZip(t, got.OutputPath)
	if loaded.Version != 1 || len(loaded.ConfigCaptures) != 0 {
		t.Fatalf("install-only written manifest = %+v", loaded)
	}
}

func TestFinalizeCaptureConfigReportsPreplanningWarningAndSkippedConfigSet(t *testing.T) {
	dir := t.TempDir()
	instanceRoot := filepath.Join(dir, "App 27")
	if err := os.MkdirAll(instanceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.unknown", PathMatch: instanceRoot,
		Detectors: []modules.InstanceDetectorDef{{
			ID: "profiles", Type: "path", Glob: filepath.Join(dir, "App *"), VersionPattern: `^App (?P<version>[0-9.]+)$`,
		}},
		Sets: []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{{ID: "g1", Range: ">=40 <41", Capture: true}}}},
	})
	withCaptureCatalogLoader(t, map[string]*modules.Module{mod.ID: mod}, nil)
	manifestPath := writeCaptureInputManifest(t, dir)
	got, err := finalizeCaptureConfig(captureConfigFinalizeRequest{
		Flags: CaptureFlags{Out: manifestPath}, ManifestPath: manifestPath, Apps: []manifest.App{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ManifestVersion != 1 || len(got.CaptureWarnings) != 1 || len(got.ConfigCapture.Diagnostics) != 1 || len(got.ConfigCapture.ConfigSets) != 1 {
		t.Fatalf("preplanning finalization = %+v", got)
	}
	row := got.ConfigCapture.ConfigSets[0]
	if row.Status != CaptureConfigStatusSkipped || row.Reason == nil || *row.Reason != CapturePlanningUnknownGeneration || row.SourceGeneration != "" {
		t.Fatalf("unknown-generation row = %+v", row)
	}
	metadata := loadCaptureMetadata(t, got.OutputPath)
	if len(metadata.CaptureWarnings) != 1 || metadata.CaptureWarnings[0] != got.CaptureWarnings[0] {
		t.Fatalf("metadata warnings = %q, result = %q", metadata.CaptureWarnings, got.CaptureWarnings)
	}
}

func TestFinalizeCaptureConfigSanitizedRemainsJSONCWithoutCatalogOrConfigCapture(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeCaptureInputManifest(t, dir)
	originalLoader := loadCaptureModuleCatalogFn
	loadCalls := 0
	loadCaptureModuleCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		loadCalls++
		return nil, nil, nil
	}
	t.Cleanup(func() { loadCaptureModuleCatalogFn = originalLoader })
	got, err := finalizeCaptureConfig(captureConfigFinalizeRequest{
		Flags: CaptureFlags{Out: manifestPath, Sanitize: true}, ManifestPath: manifestPath, Apps: []manifest.App{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loadCalls != 0 || got.OutputFormat != "jsonc" || got.OutputPath != manifestPath || got.BundleSchemaVersion != "" || got.ManifestVersion != 1 {
		t.Fatalf("sanitized finalization = loadCalls %d result %+v", loadCalls, got)
	}
	if len(got.ConfigCapture.ConfigSets) != 0 || len(got.ConfigCapture.Diagnostics) != 0 {
		t.Fatalf("sanitized config capture = %+v", got.ConfigCapture)
	}
}

func TestRunCaptureWingetReportsGenerationBundleFromActualArtifact(t *testing.T) {
	dir := t.TempDir()
	configRoot := filepath.Join(dir, "config")
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "prefs.json"), []byte("winget"), 0o644); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(configRoot, "token.json")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_CAPTURE_CONFIG", configRoot)
	mod := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.vendor", WingetRef: "Vendor.App",
		Detectors:     []modules.InstanceDetectorDef{{ID: "installed", Type: "package"}},
		Sets:          []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{{ID: "g1", Capture: true}}}},
		CaptureSource: captureTestEnvPath("ENDSTATE_CAPTURE_CONFIG", ""),
		SecretFiles:   []string{secretPath},
	})
	withCaptureCatalogLoader(t, map[string]*modules.Module{mod.ID: mod}, nil)
	withMockInstalledApps(t, []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor", Version: "27.4", Source: "winget"}})
	out := filepath.Join(dir, "winget.jsonc")
	var raw interface{}
	var captureErr *envelope.Error
	withMockSnapshot([]snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor", Source: "winget"}}, nil, func() {
		raw, captureErr = RunCapture(CaptureFlags{Out: out})
	})
	if captureErr != nil {
		t.Fatalf("RunCapture: %+v", captureErr)
	}
	result := raw.(*CaptureResult)
	if result.OutputFormat != "zip" || result.BundleSchemaVersion != "2.0" || result.ManifestVersion != 2 || result.OutputPath != strings.TrimSuffix(out, ".jsonc")+".zip" {
		t.Fatalf("Winget generation result = %+v", result)
	}
	if len(result.ConfigCapture.ConfigSets) != 1 || result.ConfigCapture.ConfigSets[0].SourceInstance.Evidence.Backend != "winget" ||
		result.ConfigCapture.ConfigSets[0].FilesCaptured != 1 || result.ConfigCapture.ConfigSets[0].Status != CaptureConfigStatusCaptured {
		t.Fatalf("Winget config capture = %+v", result.ConfigCapture)
	}
	if len(result.ConfigModules) != 1 || result.ConfigModules[0].FilesCaptured != 1 || result.ConfigModules[0].Status != "captured" ||
		!equalStrings(result.ConfigsIncluded, []string{"vendor"}) || result.ConfigsSkipped == nil || result.ConfigsCaptureErrors == nil {
		t.Fatalf("legacy envelope compatibility = modules %+v included %v skipped %v errors %v", result.ConfigModules, result.ConfigsIncluded, result.ConfigsSkipped, result.ConfigsCaptureErrors)
	}
	if result.Counts.SensitiveExcludedCount != 1 {
		t.Fatalf("generation sensitive exclusions = %d, want 1", result.Counts.SensitiveExcludedCount)
	}
	loaded := loadManifestFromCaptureZip(t, result.OutputPath)
	if loaded.Version != 2 || len(loaded.ConfigCaptures) != 1 {
		t.Fatalf("Winget artifact manifest = %+v", loaded)
	}
}

func TestRunCaptureInstallOnlyHonorsExplicitZipOutput(t *testing.T) {
	dir := t.TempDir()
	withCaptureCatalogLoader(t, map[string]*modules.Module{}, nil)
	withMockInstalledApps(t, []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor", Version: "27.4", Source: "winget"}})
	out := filepath.Join(dir, "capture.zip")
	var raw interface{}
	var captureErr *envelope.Error
	withMockSnapshot([]snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor", Source: "winget"}}, nil, func() {
		raw, captureErr = RunCapture(CaptureFlags{Out: out})
	})
	if captureErr != nil {
		t.Fatalf("RunCapture: %+v", captureErr)
	}
	result := raw.(*CaptureResult)
	if result.OutputPath != out || result.OutputFormat != "zip" || result.ManifestVersion != 0 || result.BundleSchemaVersion != "" || result.ConfigCapture != nil {
		t.Fatalf("explicit install-only zip result = %+v", result)
	}
	if len(readManifestApps(t, out)) != 1 {
		t.Fatal("explicit zip does not contain the captured app manifest")
	}
}

func TestRunCaptureRealizerSuppliesInstalledNixAndBrewPackageEvidence(t *testing.T) {
	dir := t.TempDir()
	configRoot := filepath.Join(dir, "config")
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "prefs.json"), []byte("portable"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_CAPTURE_CONFIG", configRoot)
	makeModule := func(id string) *modules.Module {
		return testCaptureGenerationModule(t, captureGenerationModuleSpec{
			ID: id, PathMatch: configRoot,
			Detectors:     []modules.InstanceDetectorDef{{ID: "installed", Type: "package"}},
			Sets:          []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{{ID: "g1", Capture: true}}}},
			CaptureSource: captureTestEnvPath("ENDSTATE_CAPTURE_CONFIG", "prefs.json"),
		})
	}
	nixModule := makeModule("apps.ripgrep")
	brewModule := makeModule("apps.hello")
	withCaptureCatalogLoader(t, map[string]*modules.Module{nixModule.ID: nixModule, brewModule.ID: brewModule}, nil)
	fr := &fakeRealizer{currentSet: nixSetWithVersion("ripgrep", "14.1.0")}
	fbe := &fakeBrewEnumerator{apps: []brew.InstalledApp{{Name: "hello", Ref: "hello", Version: "2.12"}}}
	out := filepath.Join(dir, "realizer.jsonc")
	var raw interface{}
	var captureErr *envelope.Error
	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		raw, captureErr = RunCapture(CaptureFlags{Out: out})
	})
	if captureErr != nil {
		t.Fatalf("RunCapture realizer: %+v", captureErr)
	}
	result := raw.(*CaptureResult)
	if result.ManifestVersion != 2 || len(result.ConfigCapture.ConfigSets) != 2 {
		t.Fatalf("realizer config capture = %+v", result)
	}
	evidence := map[string]manifest.ConfigSourceInstance{}
	for _, row := range result.ConfigCapture.ConfigSets {
		evidence[row.ModuleID] = row.SourceInstance
	}
	if got := evidence[nixModule.ID]; got.RawVersion != "14.1.0" || got.Evidence == nil || got.Evidence.Backend != "nix" || got.Evidence.Platform != "darwin" {
		t.Fatalf("Nix package evidence = %+v", got)
	}
	if got := evidence[brewModule.ID]; got.RawVersion != "2.12" || got.Evidence == nil || got.Evidence.Backend != "brew" || got.Evidence.Driver != "brew" {
		t.Fatalf("Brew package evidence = %+v", got)
	}
}

type captureGenerationModuleSpec struct {
	ID                  string
	WingetRef           string
	PathMatch           string
	Detectors           []modules.InstanceDetectorDef
	Sets                []testCaptureSet
	TopLevelCaptureTrap bool
	CaptureSource       string
	SecretFiles         []string
}

type testCaptureSet struct {
	ID          string
	Generations []testCaptureGeneration
}

type testCaptureGeneration struct {
	ID      string
	Range   string
	Capture bool
}

func testCaptureGenerationModule(t *testing.T, spec captureGenerationModuleSpec) *modules.Module {
	t.Helper()
	matches := map[string]any{}
	if spec.WingetRef != "" {
		matches["winget"] = []string{spec.WingetRef}
	}
	if spec.PathMatch != "" {
		matches["pathExists"] = []string{spec.PathMatch}
	}
	sets := make([]any, 0, len(spec.Sets))
	for _, setSpec := range spec.Sets {
		generations := make([]any, 0, len(setSpec.Generations))
		for index, generationSpec := range setSpec.Generations {
			generation := map[string]any{"id": generationSpec.ID, "order": index + 1}
			if generationSpec.Range != "" {
				generation["matches"] = []any{map[string]any{"versionRange": generationSpec.Range}}
			}
			if generationSpec.Capture {
				source := spec.CaptureSource
				if source == "" {
					source = `${instance.root}/prefs.json`
				}
				generation["capture"] = map[string]any{"files": []any{map[string]any{
					"source": source, "dest": "prefs.json", "optional": true,
				}}}
			}
			generations = append(generations, generation)
		}
		sets = append(sets, map[string]any{"id": setSpec.ID, "displayName": setSpec.ID, "generations": generations})
	}
	value := map[string]any{
		"moduleSchemaVersion": 2,
		"id":                  spec.ID,
		"displayName":         spec.ID,
		"sensitivity":         "low",
		"matches":             matches,
		"config": map[string]any{
			"instanceDetectors": spec.Detectors,
			"sets":              sets,
		},
	}
	if len(spec.SecretFiles) > 0 {
		value["secrets"] = map[string]any{"files": spec.SecretFiles}
	}
	if spec.TopLevelCaptureTrap {
		value["capture"] = map[string]any{"files": []any{map[string]any{"source": "trap", "dest": "trap.json"}}}
		value["restore"] = []any{map[string]any{"type": "copy", "source": "trap.json", "target": "trap.json"}}
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return mod
}

func captureTestEnvPath(name, leaf string) string {
	if runtime.GOOS == "windows" {
		return "%" + name + "%/" + leaf
	}
	return "$" + name + "/" + leaf
}

func withCaptureCatalogLoader(t *testing.T, catalog map[string]*modules.Module, diagnostics []modules.CatalogDiagnostic) {
	t.Helper()
	originalLoader := loadCaptureModuleCatalogFn
	originalRoot := resolveRepoRootFn
	loadCaptureModuleCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return catalog, diagnostics, nil
	}
	resolveRepoRootFn = func() string { return t.TempDir() }
	t.Cleanup(func() {
		loadCaptureModuleCatalogFn = originalLoader
		resolveRepoRootFn = originalRoot
	})
}

func writeCaptureInputManifest(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "capture.jsonc")
	if err := os.WriteFile(path, []byte(`{"version":1,"name":"capture","apps":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadManifestFromCaptureZip(t *testing.T, zipPath string) *manifest.Manifest {
	t.Helper()
	manifestPath, err := bundle.ExtractBundle(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(manifestPath)) })
	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func loadCaptureMetadata(t *testing.T, zipPath string) bundle.BundleMetadata {
	t.Helper()
	manifestPath, err := bundle.ExtractBundle(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(manifestPath)) })
	data, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata bundle.BundleMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	return metadata
}

func capturePlanForModule(t *testing.T, plans []bundle.ConfigSetCapturePlan, moduleID string) bundle.ConfigSetCapturePlan {
	t.Helper()
	for _, plan := range plans {
		if plan.Module.ID == moduleID {
			return plan
		}
	}
	t.Fatalf("no capture plan for module %q: %+v", moduleID, plans)
	return bundle.ConfigSetCapturePlan{}
}

func containsCapturePlanModule(plans []bundle.ConfigSetCapturePlan, moduleID, setID, generationID string) int {
	count := 0
	for _, plan := range plans {
		if plan.Module.ID != moduleID || (setID != "" && plan.Set.ID != setID) || (generationID != "" && plan.Generation.ID != generationID) {
			continue
		}
		count++
	}
	return count
}

func moduleIDs(values []*modules.Module) []string {
	ids := make([]string, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ID)
	}
	return ids
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
