// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestRunApplyDryRunExecutesFinalGenerationPlanningAndReturnsConfigFields(t *testing.T) {
	manifestDir := t.TempDir()
	payloadRoot := filepath.Join(manifestDir, "configs", "capture-a")
	if err := os.MkdirAll(payloadRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payloadRoot, "settings.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadManifest, err := bundle.BuildPayloadManifest(payloadRoot)
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "Example 1.0")
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	captureRevision := strings.Repeat("c", 64)
	mf := manifest.Manifest{
		Version: 2, Name: "generation-dry-run", Apps: []manifest.App{}, Restore: []manifest.RestoreEntry{},
		ConfigCaptures: []manifest.ConfigCapture{{
			CaptureID: "capture-a", ModuleID: "apps.example", ConfigSetID: "preferences",
			SourceInstance: manifest.ConfigSourceInstance{
				ID: "source-a", DetectorID: "source", RawVersion: "1.0", NormalizedVersion: "1",
				Evidence: &manifest.ConfigSourceInstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.App"},
			},
			SourceGeneration: "g1", SourceGenerationFingerprint: digest,
			CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 2, ContentHash: captureRevision, SnapshotPath: "provenance/modules/apps.example.json"},
			PayloadRoot:   "configs/capture-a", PayloadManifest: payloadManifest,
		}},
	}
	manifestPath := filepath.Join(manifestDir, "manifest.jsonc")
	encoded, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	module := &modules.Module{
		ID: "apps.example", ModuleSchemaVersion: 2, Revision: digest,
		Config: &modules.ConfigDef{
			InstanceDetectors: []modules.InstanceDetectorDef{{
				ID: "profiles", Type: "path", Glob: filepath.Join(filepath.Dir(targetRoot), "Example *"),
				VersionPattern: `^Example (?P<version>[0-9.]+)$`,
			}},
			Sets: []modules.ConfigSetDef{{
				ID: "preferences", Generations: []modules.GenerationDef{{
					ID: "g1", Order: 1, Fingerprint: digest,
					Matches: []modules.VersionSelectorDef{{VersionPattern: `^1\.0$`}},
					Restore: []modules.RestoreDef{{Type: "copy", Source: "settings.json", Target: `${instance.root}/settings.json`}},
				}},
			}},
		},
	}
	originalConfigCatalog := loadConfigRestoreCatalogFn
	originalModuleCatalog := loadModuleCatalogFn
	originalRoot := resolveRepoRootFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{module.ID: module}, nil, nil
	}
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) {
		return map[string]*modules.Module{module.ID: module}, nil
	}
	resolveRepoRootFn = func() string { return t.TempDir() }
	t.Cleanup(func() {
		loadConfigRestoreCatalogFn = originalConfigCatalog
		loadModuleCatalogFn = originalModuleCatalog
		resolveRepoRootFn = originalRoot
	})

	var got interface{}
	var envErr *envelope.Error
	withMockDriver(&mockDriver{installed: map[string]bool{}}, func() {
		got, envErr = RunApply(ApplyFlags{Manifest: manifestPath, DryRun: true, EnableRestore: true})
	})
	if envErr != nil {
		t.Fatalf("RunApply: %+v", envErr)
	}
	result := got.(*ApplyResult)
	if result.ConfigResultFields == nil || len(result.ConfigResolutions) != 1 || len(result.RestoreItems) != 1 {
		t.Fatalf("config fields = %+v", result.ConfigResultFields)
	}
	resolution := result.ConfigResolutions[0]
	if resolution.CaptureID != "capture-a" || resolution.Resolution != planner.ResolutionDirect || resolution.Status != planner.StatusPlanned {
		t.Fatalf("resolution = %+v", resolution)
	}
	if result.RestoreItems[0].CaptureID != "capture-a" || result.RestoreItems[0].SourceGeneration != "g1" {
		t.Fatalf("restore item = %+v", result.RestoreItems[0])
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run changed target: %v", err)
	}
}

func TestApplyAndRebuildReplaceAbsentPreviewWithPostInstallGeneration(t *testing.T) {
	for _, command := range []string{"apply", "rebuild"} {
		t.Run(command, func(t *testing.T) {
			manifestPath, targetRoot, module := postInstallGenerationFixture(t)
			repoRoot := t.TempDir()
			backend := &mockDriver{installed: map[string]bool{}, versions: map[string]string{}}
			backend.afterInstall = func(ref string) {
				if err := os.MkdirAll(targetRoot, 0o700); err != nil {
					t.Fatalf("install target root: %v", err)
				}
				backend.versions[ref] = "2.0"
			}
			originalConfigCatalog := loadConfigRestoreCatalogFn
			originalModuleCatalog := loadModuleCatalogFn
			originalRoot := resolveRepoRootFn
			loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
				return map[string]*modules.Module{module.ID: module}, nil, nil
			}
			loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) {
				return map[string]*modules.Module{module.ID: module}, nil
			}
			resolveRepoRootFn = func() string { return repoRoot }
			t.Cleanup(func() {
				loadConfigRestoreCatalogFn = originalConfigCatalog
				loadModuleCatalogFn = originalModuleCatalog
				resolveRepoRootFn = originalRoot
			})

			var fields *ConfigResultFields
			withMockDriver(backend, func() {
				if command == "apply" {
					got, envErr := RunApply(ApplyFlags{Manifest: manifestPath, EnableRestore: true})
					if envErr != nil {
						t.Fatalf("RunApply: %+v", envErr)
					}
					fields = got.(*ApplyResult).ConfigResultFields
					return
				}
				got, envErr := RunRebuild(RebuildFlags{From: manifestPath, Confirm: true})
				if envErr != nil {
					t.Fatalf("RunRebuild: %+v", envErr)
				}
				fields = got.(*RebuildResult).ConfigResultFields
			})
			if backend.installCalls != 1 {
				t.Fatalf("install calls = %d", backend.installCalls)
			}
			if fields == nil || len(fields.ConfigResolutions) != 1 || len(fields.RestoreItems) != 1 {
				t.Fatalf("config fields = %+v", fields)
			}
			resolution := fields.ConfigResolutions[0]
			if resolution.Resolution != planner.ResolutionMigrate || resolution.TargetGeneration != "g2" ||
				resolution.Status != planner.StatusRestored {
				t.Fatalf("final post-install resolution = %+v", resolution)
			}
			if fields.RestoreItems[0].TargetGeneration != "g2" {
				t.Fatalf("final restore item = %+v", fields.RestoreItems[0])
			}
			data, err := os.ReadFile(filepath.Join(targetRoot, "settings.json"))
			var document map[string]any
			if err == nil {
				err = json.Unmarshal(data, &document)
			}
			if err != nil || document["theme"] != "dark" || document["schema"] != float64(2) {
				t.Fatalf("restored target = %q, %v", data, err)
			}
		})
	}
}

func TestRunApplySideBySideRequiresAndHonorsExplicitTarget(t *testing.T) {
	manifestPath, targetA, module := postInstallGenerationFixture(t)
	module.Config.InstanceDetectors[0].VersionPattern = `^Example (?P<version>[0-9.]+)(?: [AB])?$`
	targetB := filepath.Join(filepath.Dir(targetA), "Example 2.0 B")
	for _, target := range []string{targetA, targetB} {
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	installConfigRestoreCommandCatalog(t, module, t.TempDir())
	backend := &mockDriver{
		installed: map[string]bool{"Vendor.App": true},
		versions:  map[string]string{"Vendor.App": "2.0"},
	}

	var ambiguous *ApplyResult
	withMockDriver(backend, func() {
		got, envErr := RunApply(ApplyFlags{Manifest: manifestPath, DryRun: true, EnableRestore: true})
		if envErr != nil {
			t.Fatalf("ambiguous RunApply: %+v", envErr)
		}
		ambiguous = got.(*ApplyResult)
	})
	if ambiguous.ConfigResultFields == nil || len(ambiguous.ConfigResolutions) != 1 {
		t.Fatalf("ambiguous config fields = %+v", ambiguous.ConfigResultFields)
	}
	resolution := ambiguous.ConfigResolutions[0]
	if resolution.Reason == nil || *resolution.Reason != planner.ReasonAmbiguousTargetInstance ||
		resolution.Status != planner.StatusSkipped || len(resolution.TargetCandidates) != 2 || len(ambiguous.RestoreItems) != 0 {
		t.Fatalf("ambiguous resolution = %+v items=%+v", resolution, ambiguous.RestoreItems)
	}
	selectedID := resolution.TargetCandidates[1].ID

	var selected *ApplyResult
	withMockDriver(backend, func() {
		got, envErr := RunApply(ApplyFlags{
			Manifest: manifestPath, DryRun: true, EnableRestore: true,
			RestoreTargets: []string{"capture-a=" + selectedID},
		})
		if envErr != nil {
			t.Fatalf("selected RunApply: %+v", envErr)
		}
		selected = got.(*ApplyResult)
	})
	if selected.ConfigResultFields == nil || len(selected.ConfigResolutions) != 1 || len(selected.RestoreItems) != 1 {
		t.Fatalf("selected config fields = %+v", selected.ConfigResultFields)
	}
	selectedResolution := selected.ConfigResolutions[0]
	if selectedResolution.TargetInstanceID != selectedID || selectedResolution.Resolution != planner.ResolutionMigrate ||
		selectedResolution.Status != planner.StatusPlanned || selected.RestoreItems[0].TargetInstanceID != selectedID {
		t.Fatalf("selected resolution = %+v item=%+v", selectedResolution, selected.RestoreItems[0])
	}
	for _, target := range []string{targetA, targetB} {
		if _, err := os.Stat(filepath.Join(target, "settings.json")); !os.IsNotExist(err) {
			t.Fatalf("dry-run changed side-by-side target %q: %v", target, err)
		}
	}
}

func TestRunApplyTamperedGenerationPayloadIsSkippedWithoutMutation(t *testing.T) {
	manifestPath, targetRoot, module := postInstallGenerationFixture(t)
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(filepath.Dir(manifestPath), "configs", "capture-a", "settings.json")
	if err := os.WriteFile(payload, []byte(`{"theme":"tampered"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installConfigRestoreCommandCatalog(t, module, t.TempDir())
	backend := &mockDriver{
		installed: map[string]bool{"Vendor.App": true},
		versions:  map[string]string{"Vendor.App": "2.0"},
	}

	var result *ApplyResult
	withMockDriver(backend, func() {
		got, envErr := RunApply(ApplyFlags{Manifest: manifestPath, DryRun: true, EnableRestore: true})
		if envErr != nil {
			t.Fatalf("RunApply: %+v", envErr)
		}
		result = got.(*ApplyResult)
	})
	if result.ConfigResultFields == nil || len(result.ConfigResolutions) != 1 {
		t.Fatalf("config fields = %+v", result.ConfigResultFields)
	}
	resolution := result.ConfigResolutions[0]
	if resolution.Reason == nil || *resolution.Reason != planner.ReasonPayloadIntegrityFailed ||
		resolution.Status != planner.StatusFailed || len(result.RestoreItems) != 0 {
		t.Fatalf("tampered resolution = %+v items=%+v", resolution, result.RestoreItems)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("tampered payload changed target: %v", err)
	}
}

func installConfigRestoreCommandCatalog(t *testing.T, module *modules.Module, repoRoot string) {
	t.Helper()
	originalConfigCatalog := loadConfigRestoreCatalogFn
	originalModuleCatalog := loadModuleCatalogFn
	originalRoot := resolveRepoRootFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{module.ID: module}, nil, nil
	}
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) {
		return map[string]*modules.Module{module.ID: module}, nil
	}
	resolveRepoRootFn = func() string { return repoRoot }
	t.Cleanup(func() {
		loadConfigRestoreCatalogFn = originalConfigCatalog
		loadModuleCatalogFn = originalModuleCatalog
		resolveRepoRootFn = originalRoot
	})
}

func postInstallGenerationFixture(t *testing.T) (string, string, *modules.Module) {
	t.Helper()
	manifestDir := t.TempDir()
	payloadRoot := filepath.Join(manifestDir, "configs", "capture-a")
	if err := os.MkdirAll(payloadRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payloadRoot, "settings.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadManifest, err := bundle.BuildPayloadManifest(payloadRoot)
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), "Example 2.0")
	g1Fingerprint := strings.Repeat("a", 64)
	g2Fingerprint := strings.Repeat("b", 64)
	moduleRevision := strings.Repeat("c", 64)
	mf := manifest.Manifest{
		Version: 2, Name: "post-install-generation",
		Apps:    []manifest.App{{ID: "example", Refs: map[string]string{"windows": "Vendor.App"}}},
		Restore: []manifest.RestoreEntry{},
		ConfigCaptures: []manifest.ConfigCapture{{
			CaptureID: "capture-a", ModuleID: "apps.example", ConfigSetID: "preferences",
			SourceInstance: manifest.ConfigSourceInstance{
				ID: "source-a", DetectorID: "source", RawVersion: "1.0", NormalizedVersion: "1",
				Evidence: &manifest.ConfigSourceInstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.App"},
			},
			SourceGeneration: "g1", SourceGenerationFingerprint: g1Fingerprint,
			CaptureModule: manifest.CaptureModuleProvenance{
				SchemaVersion: 2, ContentHash: moduleRevision, SnapshotPath: "provenance/modules/apps.example.json",
			},
			PayloadRoot: "configs/capture-a", PayloadManifest: payloadManifest,
		}},
	}
	manifestPath := filepath.Join(manifestDir, "manifest.jsonc")
	encoded, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	module := &modules.Module{
		ID: "apps.example", ModuleSchemaVersion: 2, Revision: moduleRevision,
		Matches: modules.MatchCriteria{Winget: []string{"Vendor.App"}},
		Config: &modules.ConfigDef{
			InstanceDetectors: []modules.InstanceDetectorDef{{
				ID: "profiles", Type: "path", Glob: filepath.Join(filepath.Dir(targetRoot), "Example *"),
				VersionPattern: `^Example (?P<version>[0-9.]+)$`,
			}},
			Sets: []modules.ConfigSetDef{{
				ID: "preferences",
				Generations: []modules.GenerationDef{
					{ID: "g1", Order: 1, Fingerprint: g1Fingerprint, Matches: []modules.VersionSelectorDef{{VersionPattern: `^1\.0$`}}, Restore: []modules.RestoreDef{{Type: "copy", Source: "settings.json", Target: `${instance.root}/settings.json`}}},
					{ID: "g2", Order: 2, Fingerprint: g2Fingerprint, Matches: []modules.VersionSelectorDef{{VersionPattern: `^2\.0$`}}, Restore: []modules.RestoreDef{{Type: "copy", Source: "settings.json", Target: `${instance.root}/settings.json`}}},
				},
				Migrations: []modules.MigrationEdgeDef{{From: "g1", To: "g2", Operations: []modules.MigrationOperationDef{{
					Type: "json-set", Path: "settings.json", JSONPath: "$.schema", Value: 2,
				}}, Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}}}},
			}},
		},
	}
	staged, stageErr := migration.NewEngine().Stage(context.Background(), migration.StageRequest{
		CaptureID: "capture-a", PayloadRoot: payloadRoot, PayloadManifest: payloadManifest,
		SourceGeneration: "g1", TargetGeneration: &module.Config.Sets[0].Generations[1],
		MigrationEdges: module.Config.Sets[0].Migrations,
	})
	if stageErr != nil {
		t.Fatalf("fixture staging: %v", stageErr)
	}
	_ = staged.Close()
	return manifestPath, targetRoot, module
}
