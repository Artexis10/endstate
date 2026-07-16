// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestConfigRestorePlanningSessionFinalReplacesAbsentPreviewAndIsOnlyExecutable(t *testing.T) {
	runtime, loadCount := planningTestRuntime(t, "", planningTestModule(
		"apps.example", modules.InstanceDetectorDef{ID: "installed", Type: "package"},
	))
	session := newConfigRestorePlanningSession(runtime)
	if _, ok := session.ExecutionPlan(); ok {
		t.Fatal("preview-less session exposed an execution plan")
	}

	preview := session.Preview(configRestoreDetectionEvidence{})
	assertPlanningReason(t, preview, "capture-example", planner.ReasonTargetNotDetected)
	if _, ok := session.ExecutionPlan(); ok {
		t.Fatal("preview became eligible for execution")
	}
	preview.Sets[0].TargetInstances = append(preview.Sets[0].TargetInstances, planner.TargetInstance{ID: "caller-stale"})

	final := session.Final(configRestoreDetectionEvidence{PackagesByModule: map[string][]modules.PackageEvidence{
		"apps.example": {{AppID: "apps.example", Backend: "winget", Ref: "Vendor.Example", RawVersion: "2.5"}},
	}})
	if len(final.Sets) != 1 || final.Sets[0].Resolution.Resolution != planner.ResolutionDirect ||
		final.Sets[0].Resolution.Reason != nil || len(final.Sets[0].TargetInstances) != 1 ||
		final.Sets[0].TargetInstances[0].RawVersion != "2.5" {
		t.Fatalf("final plan = %+v", final)
	}
	if got := *loadCount; got != 1 {
		t.Fatalf("catalog load count = %d, want 1", got)
	}

	final.Sets[0].TargetInstances[0].RawVersion = "caller-mutated"
	final.Sets[0].Resolution.TargetCandidates[0].RawVersion = "caller-mutated-candidate"
	final.Sets[0].TargetGenerationDef.Restore[0].Target = "caller-mutated-target"
	final.Sets[0].TargetGenerationDef.Restore[0].Exclude[0] = "caller-mutated-exclude"
	executable, ok := session.ExecutionPlan()
	if !ok || executable.Sets[0].TargetInstances[0].RawVersion != "2.5" ||
		executable.Sets[0].Resolution.TargetCandidates[0].RawVersion != "2.5" ||
		executable.Sets[0].TargetGenerationDef.Restore[0].Target != "preferences.json" ||
		executable.Sets[0].TargetGenerationDef.Restore[0].Exclude[0] != "cache/**" {
		t.Fatalf("execution plan was absent or aliased final return: ok=%v plan=%+v", ok, executable)
	}
	executable.Sets[0].TargetInstances[0].RawVersion = "execution-caller-mutated"
	again, ok := session.ExecutionPlan()
	if !ok || again.Sets[0].TargetInstances[0].RawVersion != "2.5" {
		t.Fatalf("execution plan return aliases session state: ok=%v plan=%+v", ok, again)
	}
}

func TestConfigRestorePlanningSessionFinalDropsRemovedPreviewCandidate(t *testing.T) {
	runtime, _ := planningTestRuntime(t, "", planningTestModule(
		"apps.example", modules.InstanceDetectorDef{ID: "installed", Type: "package"},
	))
	session := newConfigRestorePlanningSession(runtime)
	preview := session.Preview(configRestoreDetectionEvidence{PackagesByModule: map[string][]modules.PackageEvidence{
		"apps.example": {
			{AppID: "apps.example", Backend: "winget", Ref: "Vendor.Example.1", RawVersion: "1.5"},
			{AppID: "apps.example", Backend: "winget", Ref: "Vendor.Example.2", RawVersion: "2.5"},
		},
	}})
	assertPlanningReason(t, preview, "capture-example", planner.ReasonAmbiguousTargetInstance)
	if len(preview.Sets[0].TargetInstances) != 2 {
		t.Fatalf("preview candidates = %+v", preview.Sets[0].TargetInstances)
	}
	removedID := ""
	for _, target := range preview.Sets[0].TargetInstances {
		if target.Evidence.Ref == "Vendor.Example.2" {
			removedID = target.ID
		}
	}
	if removedID == "" {
		t.Fatalf("preview missing removable candidate: %+v", preview.Sets[0].TargetInstances)
	}

	final := session.Final(configRestoreDetectionEvidence{PackagesByModule: map[string][]modules.PackageEvidence{
		"apps.example": {{AppID: "apps.example", Backend: "winget", Ref: "Vendor.Example.1", RawVersion: "1.5"}},
	}})
	if len(final.Sets[0].TargetInstances) != 1 || final.Sets[0].TargetInstances[0].ID == removedID ||
		final.Sets[0].Resolution.TargetInstanceID == removedID || final.Sets[0].Resolution.Resolution != planner.ResolutionDirect {
		t.Fatalf("final retained removed preview candidate: %+v", final.Sets[0])
	}
}

func TestConfigRestorePlanningSessionFinalPreservesSideBySideTargets(t *testing.T) {
	runtime, _ := planningTestRuntime(t, "", planningTestModule(
		"apps.example", modules.InstanceDetectorDef{ID: "installed", Type: "package"},
	))
	session := newConfigRestorePlanningSession(runtime)
	_ = session.Preview(configRestoreDetectionEvidence{PackagesByModule: map[string][]modules.PackageEvidence{
		"apps.example": {{AppID: "apps.example", Backend: "winget", Ref: "Vendor.Example.1", RawVersion: "1.5"}},
	}})
	final := session.Final(configRestoreDetectionEvidence{PackagesByModule: map[string][]modules.PackageEvidence{
		"apps.example": {
			{AppID: "apps.example", Backend: "winget", Ref: "Vendor.Example.1", RawVersion: "1.5"},
			{AppID: "apps.example", Backend: "winget", Ref: "Vendor.Example.2", RawVersion: "2.5"},
		},
	}})
	assertPlanningReason(t, final, "capture-example", planner.ReasonAmbiguousTargetInstance)
	if len(final.Sets[0].TargetInstances) != 2 {
		t.Fatalf("side-by-side final targets = %+v", final.Sets[0].TargetInstances)
	}
}

func TestConfigRestorePlanningSessionFinalDetectionFailureReplacesPreviewAndIsIsolated(t *testing.T) {
	runtime, _ := planningTestRuntime(t, "alpha,beta",
		planningTestModule("apps.alpha", modules.InstanceDetectorDef{ID: "installed", Type: "package"}),
		planningTestModule("apps.beta", modules.InstanceDetectorDef{ID: "profiles", Type: "path", Glob: "beta/*"}),
		planningTestModule("apps.filtered", modules.InstanceDetectorDef{ID: "profiles", Type: "path", Glob: "filtered/*"}),
	)
	session := newConfigRestorePlanningSession(runtime)
	preview := session.Preview(configRestoreDetectionEvidence{
		PackagesByModule: map[string][]modules.PackageEvidence{
			"apps.alpha": {{AppID: "apps.alpha", Backend: "winget", Ref: "Vendor.Alpha", RawVersion: "1.5"}},
		},
		Glob: func(pattern string) ([]string, error) {
			if pattern == "filtered/*" {
				t.Fatal("filtered lane was detected")
			}
			return []string{filepath.Join(t.TempDir(), "1.5")}, nil
		},
	})
	if len(preview.Sets) != 2 {
		t.Fatalf("preview planned filtered sources: %+v", preview.Sets)
	}

	final := session.Final(configRestoreDetectionEvidence{
		PackagesByModule: map[string][]modules.PackageEvidence{
			"apps.alpha": {{AppID: "apps.alpha", Backend: "winget", Ref: "Vendor.Alpha", RawVersion: "2.5"}},
		},
		Glob: func(pattern string) ([]string, error) {
			if pattern == "filtered/*" {
				t.Fatal("filtered lane was detected")
			}
			return nil, errors.New("beta detector unavailable")
		},
	})
	if len(final.Sets) != 2 {
		t.Fatalf("final sets = %+v", final.Sets)
	}
	alpha := planningSetByCapture(t, final, "capture-alpha")
	if alpha.Resolution.Resolution != planner.ResolutionDirect || alpha.Resolution.Reason != nil ||
		len(alpha.TargetInstances) != 1 || alpha.TargetInstances[0].RawVersion != "2.5" {
		t.Fatalf("independent set was poisoned: %+v", alpha)
	}
	beta := planningSetByCapture(t, final, "capture-beta")
	if beta.Resolution.Resolution != planner.ResolutionUnknown || beta.Resolution.Status != planner.StatusSkipped ||
		beta.Resolution.Reason == nil || *beta.Resolution.Reason != planner.ReasonTargetDetectionFailed ||
		len(beta.TargetInstances) != 0 || beta.Resolution.TargetInstanceID != "" {
		t.Fatalf("failed detector retained preview state or wrong outcome: %+v", beta)
	}
	executable, ok := session.ExecutionPlan()
	if !ok {
		t.Fatal("final plan was not executable")
	}
	assertPlanningReason(t, executable, "capture-beta", planner.ReasonTargetDetectionFailed)
}

func planningTestRuntime(t *testing.T, restoreFilter string, catalogModules ...*modules.Module) (*configRestoreRuntime, *int) {
	t.Helper()
	manifestDir := t.TempDir()
	captures := make([]manifest.ConfigCapture, len(catalogModules))
	catalog := make(map[string]*modules.Module, len(catalogModules))
	for index, module := range catalogModules {
		name := strings.TrimPrefix(module.ID, "apps.")
		captures[index] = commandTestConfigCapture(t, manifestDir, "capture-"+name, module.ID, "preferences")
		catalog[module.ID] = module
	}
	loadCount := 0
	originalLoader := loadConfigRestoreCatalogFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		loadCount++
		return catalog, nil, nil
	}
	t.Cleanup(func() { loadConfigRestoreCatalogFn = originalLoader })
	runtime, envErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: captures},
		ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"), RestoreFilter: restoreFilter,
	})
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntime: %+v", envErr)
	}
	return runtime, &loadCount
}

func planningTestModule(moduleID string, detector modules.InstanceDetectorDef) *modules.Module {
	return &modules.Module{
		ModuleSchemaVersion: 2,
		ID:                  moduleID,
		Revision:            "restore-revision-" + moduleID,
		Config: &modules.ConfigDef{
			InstanceDetectors: []modules.InstanceDetectorDef{detector},
			Sets: []modules.ConfigSetDef{{
				ID: "preferences",
				Generations: []modules.GenerationDef{{
					ID: "g1", Order: 1, Matches: []modules.VersionSelectorDef{{VersionRange: ">=1 <3"}},
					Fingerprint: strings.Repeat("a", 64),
					Restore: []modules.RestoreDef{{
						Type: "copy", Source: "preferences.json", Target: "preferences.json",
						Exclude: []string{"cache/**"},
					}},
				}},
			}},
		},
	}
}

func assertPlanningReason(t *testing.T, plan planner.ConfigPlan, captureID string, want planner.ResolutionReason) {
	t.Helper()
	set := planningSetByCapture(t, plan, captureID)
	if set.Resolution.Reason == nil || *set.Resolution.Reason != want {
		t.Fatalf("capture %q reason = %v, want %q; set=%+v", captureID, set.Resolution.Reason, want, set)
	}
}

func planningSetByCapture(t *testing.T, plan planner.ConfigPlan, captureID string) planner.PlanSet {
	t.Helper()
	for _, set := range plan.Sets {
		if set.Source.CaptureID == captureID {
			return set
		}
	}
	t.Fatalf("capture %q missing from plan %+v", captureID, plan)
	return planner.PlanSet{}
}
