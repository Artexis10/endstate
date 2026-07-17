// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

type fakeConfigRestoreCatalogSource struct {
	calls       []string
	catalog     map[string]*modules.Module
	diagnostics []modules.CatalogDiagnostic
	err         error
}

func (fake *fakeConfigRestoreCatalogSource) LoadConfigRestoreCatalog(repoRoot string) (
	map[string]*modules.Module,
	[]modules.CatalogDiagnostic,
	error,
) {
	fake.calls = append(fake.calls, repoRoot)
	return fake.catalog, fake.diagnostics, fake.err
}

type fakeConfigRestoreEvidenceSource struct {
	requests  []configRestoreDetectionRequest
	evidence  map[configRestoreDetectionPass]configRestoreDetectionEvidence
	errByPass map[configRestoreDetectionPass]error
	mutate    func(configRestoreDetectionRequest)
}

func (fake *fakeConfigRestoreEvidenceSource) Snapshot(
	_ context.Context,
	request configRestoreDetectionRequest,
) (configRestoreDetectionEvidence, error) {
	fake.requests = append(fake.requests, cloneConfigRestoreDetectionRequest(request))
	if fake.mutate != nil {
		fake.mutate(request)
	}
	if err := fake.errByPass[request.Pass]; err != nil {
		return configRestoreDetectionEvidence{}, err
	}
	return fake.evidence[request.Pass], nil
}

func cloneConfigRestoreDetectionRequest(request configRestoreDetectionRequest) configRestoreDetectionRequest {
	return configRestoreDetectionRequest{
		Pass:    request.Pass,
		Modules: cloneConfigModuleCatalog(request.Modules),
	}
}

func TestConfigCatalogSnapshotOwnsOneDefensiveModuleCatalog(t *testing.T) {
	module := configRestoreCoordinatorModule("apps.pinned", "1")
	catalog := map[string]*modules.Module{module.ID: module}
	diagnostics := []modules.CatalogDiagnostic{{Code: "ORIGINAL", ModuleID: module.ID, FilePath: "module.jsonc"}}

	snapshot := newConfigCatalogSnapshot(catalog, diagnostics)

	delete(catalog, module.ID)
	module.DisplayName = "mutated"
	module.Matches.Winget[0] = "Vendor.Mutated"
	module.Restore[0].Exclude[0] = "mutated/**"
	module.Config.Sets[0].Generations[0].ID = "mutated"
	diagnostics[0].Code = "MUTATED"

	first := snapshot.ModuleCatalog()
	got := first["apps.pinned"]
	if got == nil || got.DisplayName != "Pinned" || got.Matches.Winget[0] != "Vendor.Pinned" ||
		got.Restore[0].Exclude[0] != "cache/**" || got.Config.Sets[0].Generations[0].ID != "g1" {
		t.Fatalf("snapshot catalog was not pinned: %+v", got)
	}
	if snapshot.diagnostics[0].Code != "ORIGINAL" {
		t.Fatalf("snapshot diagnostics were not pinned: %+v", snapshot.diagnostics)
	}

	first["apps.pinned"].Matches.Winget[0] = "Caller.Mutated"
	delete(first, "apps.pinned")
	second := snapshot.ModuleCatalog()
	if second["apps.pinned"].Matches.Winget[0] != "Vendor.Pinned" {
		t.Fatalf("caller mutated snapshot-owned catalog: %+v", second["apps.pinned"].Matches)
	}

	plan := snapshot.resolver.ResolveCandidate(configRestoreCoordinatorSource("apps.pinned"), planner.TargetInstance{
		ID: "installed", ModuleID: "apps.pinned", DetectorID: "package", RawVersion: "1",
	})
	if plan.Resolution.Resolution != planner.ResolutionDirect || plan.Resolution.RestoreModuleRevision != "revision-pinned" {
		t.Fatalf("resolver was not built from the same pinned snapshot: %+v", plan.Resolution)
	}
}

func TestConfigCatalogSnapshotPreservesCanonicalModuleBytes(t *testing.T) {
	module, err := modules.ParseModuleJSON([]byte(`{
		"moduleSchemaVersion": 2,
		"id": "apps.canonical",
		"displayName": "Canonical",
		"sensitivity": "low",
		"matches": {"winget": ["Vendor.Canonical"]},
		"config": {
			"instanceDetectors": [{"id": "package", "type": "package"}],
			"sets": [{"id": "preferences", "generations": [{"id": "g1", "order": 1, "matches": [{"versionPattern": "^.+$"}]}]}]
		}
	}`))
	if err != nil {
		t.Fatalf("ParseModuleJSON: %v", err)
	}
	want := module.CanonicalSnapshot()
	snapshot := newConfigCatalogSnapshot(map[string]*modules.Module{module.ID: module}, nil)

	first := snapshot.ModuleCatalog()[module.ID]
	if first == nil || !bytes.Equal(first.CanonicalSnapshot(), want) {
		t.Fatalf("canonical bytes were not retained: got=%q want=%q", first.CanonicalSnapshot(), want)
	}
	first.CanonicalSnapshot()[0] = '!'
	second := snapshot.ModuleCatalog()[module.ID]
	if !bytes.Equal(second.CanonicalSnapshot(), want) {
		t.Fatalf("canonical bytes alias a caller-owned copy: got=%q want=%q", second.CanonicalSnapshot(), want)
	}
}

func TestNewConfigRestoreRuntimeWithCatalogSourceLoadsOnce(t *testing.T) {
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-pinned", "apps.pinned", "preferences")
	loader := &fakeConfigRestoreCatalogSource{catalog: map[string]*modules.Module{
		"apps.pinned": configRestoreCoordinatorModule("apps.pinned", capture.SourceGenerationFingerprint),
	}}

	runtime, envErr := newConfigRestoreRuntimeWithCatalogSource(configRestoreBuildRequest{
		Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}},
		ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"), RepoRoot: "trusted-root",
	}, loader)
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntimeWithCatalogSource: %+v", envErr)
	}
	if !reflect.DeepEqual(loader.calls, []string{"trusted-root"}) {
		t.Fatalf("catalog loads = %v", loader.calls)
	}
	if runtime.catalog.resolver == nil || runtime.catalog.ModuleCatalog()["apps.pinned"] == nil {
		t.Fatalf("runtime did not retain the pinned catalog snapshot: %+v", runtime.catalog)
	}
}

func TestNewConfigRestoreRuntimeWithCatalogSnapshotDoesNotReload(t *testing.T) {
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-pinned", "apps.pinned", "preferences")
	snapshot := newConfigCatalogSnapshot(map[string]*modules.Module{
		"apps.pinned": configRestoreCoordinatorModule("apps.pinned", capture.SourceGenerationFingerprint),
	}, nil)
	loadCount := 0
	originalLoader := loadConfigRestoreCatalogFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		loadCount++
		return nil, nil, nil
	}
	t.Cleanup(func() { loadConfigRestoreCatalogFn = originalLoader })

	runtime, envErr := newConfigRestoreRuntimeWithCatalogSnapshot(configRestoreBuildRequest{
		Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}},
		ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"), RepoRoot: "trusted-root",
	}, snapshot)
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntimeWithCatalogSnapshot: %+v", envErr)
	}
	if loadCount != 0 || runtime.catalog.resolver == nil || runtime.catalog.ModuleCatalog()["apps.pinned"] == nil {
		t.Fatalf("prepared runtime reloaded or discarded snapshot: loads=%d catalog=%+v", loadCount, runtime.catalog)
	}
}

func TestConfigRestoreCoordinatorUsesFreshPinnedEvidenceForPreviewAndFinal(t *testing.T) {
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-pinned", "apps.pinned", "preferences")
	runtime := configRestoreCoordinatorRuntime(t, manifestDir, capture)
	evidence := &fakeConfigRestoreEvidenceSource{
		evidence: map[configRestoreDetectionPass]configRestoreDetectionEvidence{
			configRestoreDetectionPreview: {PackagesByModule: map[string][]modules.PackageEvidence{
				"apps.pinned": {{AppID: "pinned", Backend: "winget", Platform: "windows", Ref: "Vendor.Pinned", RawVersion: "1.0-vendor"}},
			}},
			configRestoreDetectionFinal: {PackagesByModule: map[string][]modules.PackageEvidence{
				"apps.pinned": {{AppID: "pinned", Backend: "winget", Platform: "windows", Ref: "Vendor.Pinned", RawVersion: "2.0-vendor"}},
			}},
		},
		mutate: func(request configRestoreDetectionRequest) {
			request.Modules["apps.pinned"].Matches.Winget[0] = "Caller.Mutated"
		},
	}
	coordinator := newConfigRestoreCoordinator(runtime, evidence)

	preview, err := coordinator.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	final, err := coordinator.Final(context.Background(), false)
	if err != nil {
		t.Fatalf("Final: %v", err)
	}

	if len(evidence.requests) != 2 || evidence.requests[0].Pass != configRestoreDetectionPreview ||
		evidence.requests[1].Pass != configRestoreDetectionFinal {
		t.Fatalf("detection requests = %+v", evidence.requests)
	}
	for _, request := range evidence.requests {
		if len(request.Modules) != 1 || request.Modules["apps.pinned"] == nil ||
			request.Modules["apps.pinned"].Matches.Winget[0] != "Vendor.Pinned" {
			t.Fatalf("request did not receive a defensive pinned catalog: %+v", request.Modules)
		}
	}
	if preview.Sets[0].TargetInstances[0].RawVersion != "1.0-vendor" {
		t.Fatalf("preview raw version = %+v", preview.Sets[0].TargetInstances)
	}
	if final.Sets[0].TargetInstances[0].RawVersion != "2.0-vendor" {
		t.Fatalf("final did not replace preview evidence: %+v", final.Sets[0].TargetInstances)
	}
	if final.Sets[0].Resolution.Resolution != planner.ResolutionDirect ||
		final.Sets[0].Resolution.Status != planner.StatusSkipped ||
		final.Sets[0].Resolution.Reason == nil || *final.Sets[0].Resolution.Reason != planner.ReasonRestoreNotEnabled {
		t.Fatalf("restore-disabled overlay discarded compatibility or status: %+v", final.Sets[0].Resolution)
	}
	if _, ok := coordinator.ExecutionPlan(); ok {
		t.Fatal("restore-disabled final plan became executable")
	}
}

func TestConfigRestoreCoordinatorExecutionPlanPublishesFinalAndPreviewInvalidatesIt(t *testing.T) {
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-pinned", "apps.pinned", "preferences")
	runtime := configRestoreCoordinatorRuntime(t, manifestDir, capture)
	evidence := &fakeConfigRestoreEvidenceSource{evidence: map[configRestoreDetectionPass]configRestoreDetectionEvidence{
		configRestoreDetectionFinal: {PackagesByModule: map[string][]modules.PackageEvidence{
			"apps.pinned": {{AppID: "pinned", Backend: "winget", Ref: "Vendor.Pinned", RawVersion: "fresh-final-vendor"}},
		}},
		configRestoreDetectionPreview: {PackagesByModule: map[string][]modules.PackageEvidence{
			"apps.pinned": {{AppID: "pinned", Backend: "winget", Ref: "Vendor.Pinned", RawVersion: "later-preview-vendor"}},
		}},
	}}
	coordinator := newConfigRestoreCoordinator(runtime, evidence)

	final, err := coordinator.Final(context.Background(), true)
	if err != nil {
		t.Fatalf("Final: %v", err)
	}
	execution, ok := coordinator.ExecutionPlan()
	if !ok || len(execution.Sets) != 1 || execution.Sets[0].TargetInstances[0].RawVersion != "fresh-final-vendor" {
		t.Fatalf("execution plan did not publish fresh final evidence: ok=%t plan=%+v", ok, execution)
	}
	if final.Sets[0].TargetInstances[0].RawVersion != execution.Sets[0].TargetInstances[0].RawVersion {
		t.Fatalf("final and execution plans diverged: final=%+v execution=%+v", final, execution)
	}

	preview, err := coordinator.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Sets[0].TargetInstances[0].RawVersion != "later-preview-vendor" {
		t.Fatalf("preview did not use fresh evidence: %+v", preview)
	}
	if stale, stillExecutable := coordinator.ExecutionPlan(); stillExecutable || len(stale.Sets) != 0 {
		t.Fatalf("preview left stale final plan executable: ok=%t plan=%+v", stillExecutable, stale)
	}
}

func TestConfigRestoreCoordinatorNeverDetectsFilteredOrLegacyLanes(t *testing.T) {
	manifestDir := t.TempDir()
	filtered := commandTestConfigCapture(t, manifestDir, "capture-filtered", "apps.filtered", "preferences")
	runtime, envErr := newConfigRestoreRuntimeWithCatalogSource(configRestoreBuildRequest{
		Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{filtered}},
		ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"), RestoreFilter: "apps.someone-else",
	}, &fakeConfigRestoreCatalogSource{catalog: map[string]*modules.Module{
		"apps.filtered": configRestoreCoordinatorModule("apps.filtered", filtered.SourceGenerationFingerprint),
	}})
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntimeWithCatalogSource: %+v", envErr)
	}
	evidence := &fakeConfigRestoreEvidenceSource{}
	coordinator := newConfigRestoreCoordinator(runtime, evidence)

	final, err := coordinator.Final(context.Background(), false)
	if err != nil {
		t.Fatalf("Final: %v", err)
	}
	if len(evidence.requests) != 0 {
		t.Fatalf("filtered lane triggered detection: %+v", evidence.requests)
	}
	if len(final.Sets) != 1 || final.Sets[0].Resolution.Resolution != planner.ResolutionUnknown ||
		final.Sets[0].Resolution.Reason == nil || *final.Sets[0].Resolution.Reason != planner.ReasonRestoreFiltered {
		t.Fatalf("filtered lane was overwritten by consent: %+v", final.Sets)
	}

	legacyRuntime, envErr := newConfigRestoreRuntimeWithCatalogSource(configRestoreBuildRequest{
		Manifest: &manifest.Manifest{Version: 1, Restore: []manifest.RestoreEntry{{
			Type: "file-copy", Source: "legacy", Target: "target", FromModule: "apps.legacy",
		}}},
		ManifestPath: filepath.Join(manifestDir, "legacy.jsonc"),
	}, &fakeConfigRestoreCatalogSource{catalog: map[string]*modules.Module{}})
	if envErr != nil {
		t.Fatalf("legacy runtime: %+v", envErr)
	}
	legacyEvidence := &fakeConfigRestoreEvidenceSource{}
	legacyPlan, err := newConfigRestoreCoordinator(legacyRuntime, legacyEvidence).Final(context.Background(), false)
	if err != nil {
		t.Fatalf("legacy Final: %v", err)
	}
	if len(legacyEvidence.requests) != 0 || len(legacyPlan.Sets) != 0 {
		t.Fatalf("legacy lane entered generation detection: requests=%+v plan=%+v", legacyEvidence.requests, legacyPlan)
	}
}

func configRestoreCoordinatorRuntime(
	t *testing.T,
	manifestDir string,
	capture manifest.ConfigCapture,
) *configRestoreRuntime {
	t.Helper()
	runtime, envErr := newConfigRestoreRuntimeWithCatalogSource(configRestoreBuildRequest{
		Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}},
		ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"),
	}, &fakeConfigRestoreCatalogSource{catalog: map[string]*modules.Module{
		capture.ModuleID: configRestoreCoordinatorModule(capture.ModuleID, capture.SourceGenerationFingerprint),
	}})
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntimeWithCatalogSource: %+v", envErr)
	}
	return runtime
}

func configRestoreCoordinatorModule(moduleID, generationFingerprint string) *modules.Module {
	return &modules.Module{
		ModuleSchemaVersion: 2,
		ID:                  moduleID,
		DisplayName:         "Pinned",
		Revision:            "revision-pinned",
		Matches: modules.MatchCriteria{
			Winget:     []string{"Vendor.Pinned"},
			Exe:        []string{"Pinned.exe"},
			PathExists: []string{"pinned/path"},
		},
		Restore: []modules.RestoreDef{{Type: "file-copy", Source: "source", Target: "target", Exclude: []string{"cache/**"}}},
		Config: &modules.ConfigDef{
			InstanceDetectors: []modules.InstanceDetectorDef{{ID: "package", Type: "package"}},
			Sets: []modules.ConfigSetDef{{
				ID: "preferences",
				Generations: []modules.GenerationDef{{
					ID: "g1", Order: 1, Fingerprint: generationFingerprint,
					Matches: []modules.VersionSelectorDef{{VersionPattern: `^.+$`}},
				}},
			}},
		},
	}
}

func configRestoreCoordinatorSource(moduleID string) planner.SourceCapture {
	return planner.SourceCapture{
		CaptureID: "capture-pinned", ModuleID: moduleID, ConfigSetID: "preferences",
		Instance:   planner.SourceInstance{ID: "source", RawVersion: "1"},
		Generation: "g1", GenerationFingerprint: "1", ModuleRevision: "capture-revision",
		CaptureModuleSchemaVersion: 2,
	}
}
