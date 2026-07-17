// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type countedCaptureEnumerator struct {
	calls    *int
	packages []driver.InstalledPackage
}

func TestRunApply_AlternatePackagePathsRetainPackageModuleMap(t *testing.T) {
	matchPath := filepath.Join(t.TempDir(), "matched")
	if err := os.WriteFile(matchPath, []byte("present"), 0o600); err != nil {
		t.Fatal(err)
	}
	module := &modules.Module{
		ID: "apps.git",
		Matches: modules.MatchCriteria{
			Winget:     []string{"Git.Git"},
			Chocolatey: []string{"git.install"},
			PathExists: []string{matchPath},
		},
		Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
	}
	wantLegacy := map[string]string{"Git.Git": "apps.git"}
	wantPackages := map[string][]string{
		"winget:Git.Git":         {"apps.git"},
		"chocolatey:git.install": {"apps.git"},
	}

	assertMaps := func(t *testing.T, raw interface{}, runErr *envelope.Error) {
		t.Helper()
		if runErr != nil {
			t.Fatalf("RunApply: %v", runErr)
		}
		result := raw.(*ApplyResult)
		if !reflect.DeepEqual(result.ConfigModuleMap, wantLegacy) {
			t.Fatalf("legacy configModuleMap = %v, want %v", result.ConfigModuleMap, wantLegacy)
		}
		if !reflect.DeepEqual(result.PackageModuleMap, wantPackages) {
			t.Fatalf("packageModuleMap = %v, want %v", result.PackageModuleMap, wantPackages)
		}
	}

	t.Run("realizer", func(t *testing.T) {
		manifestPath := writeTempManifest(t, replaceGOOS(`{
  "version": 1, "name": "realizer-map",
  "apps": [{"id":"ripgrep","refs":{"GOOS":"nixpkgs#ripgrep"}}]
}`))
		withMockCatalog(map[string]*modules.Module{module.ID: module}, nil, func() {
			withFakeRealizer(&fakeRealizer{}, func() {
				raw, runErr := RunApply(ApplyFlags{Manifest: manifestPath, DryRun: true})
				assertMaps(t, raw, runErr)
			})
		})
	})

	t.Run("brew-only", func(t *testing.T) {
		manifestPath := writeTempManifest(t, `{
  "version": 1, "name": "brew-map",
  "apps": [{"id":"hello","driver":"brew","refs":{"darwin":"hello"}}]
}`)
		withMockCatalog(map[string]*modules.Module{module.ID: module}, nil, func() {
			withRealizerAndBrew(&fakeRealizer{}, func() (driver.Driver, error) {
				return &fakeBrewDriver{installed: map[string]bool{}}, nil
			}, func() {
				raw, runErr := RunApply(ApplyFlags{Manifest: manifestPath, DryRun: true})
				assertMaps(t, raw, runErr)
			})
		})
	})
}

func TestRunApplyAndRebuild_ChocolateyMigrationUsesFinalDriverEvidence(t *testing.T) {
	for _, command := range []string{"apply", "rebuild"} {
		t.Run(command, func(t *testing.T) {
			manifestPath, targetRoot, module := chocolateyPostInstallGenerationFixture(t)
			repoRoot := t.TempDir()
			chocolatey := &laneTestDriver{
				name: "chocolatey", installed: map[string]bool{}, versions: map[string]string{},
			}
			winget := &laneTestDriver{
				name: "winget", installed: map[string]bool{"Git.Install": true}, versions: map[string]string{"Git.Install": "9.9"},
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
			withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": chocolatey}, nil, func() {
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

			if len(chocolatey.installVersionCalls) != 1 || chocolatey.installVersionCalls[0] != "Git.Install@2.0" {
				t.Fatalf("Chocolatey install-version calls = %v", chocolatey.installVersionCalls)
			}
			if winget.batchCalls != 0 || len(winget.detectCalls) != 0 || len(winget.installCalls) != 0 || len(winget.installVersionCalls) != 0 {
				t.Fatalf("Winget fallback calls batch=%d detect=%v install=%v installVersion=%v", winget.batchCalls, winget.detectCalls, winget.installCalls, winget.installVersionCalls)
			}
			if fields == nil || len(fields.ConfigResolutions) != 1 || len(fields.RestoreItems) != 1 {
				t.Fatalf("config fields = %+v; Chocolatey batch=%d detect=%v versions=%v", fields, chocolatey.batchCalls, chocolatey.detectCalls, chocolatey.versions)
			}
			resolution := fields.ConfigResolutions[0]
			if resolution.Resolution != planner.ResolutionMigrate || resolution.TargetGeneration != "g2" ||
				resolution.Status != planner.StatusRestored || len(resolution.TargetCandidates) != 1 ||
				resolution.TargetCandidates[0].Evidence.Backend != "chocolatey" || resolution.TargetCandidates[0].Evidence.Driver != "chocolatey" {
				t.Fatalf("final Chocolatey resolution = %+v", resolution)
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

func TestRunRestore_UsesManifestDeclaredChocolateyEvidence(t *testing.T) {
	manifestPath, targetRoot, module := chocolateyPostInstallGenerationFixture(t)
	repoRoot := t.TempDir()
	chocolatey := &laneTestDriver{
		name: "chocolatey", installed: map[string]bool{"Git.Install": true}, versions: map[string]string{"Git.Install": "2.0"},
	}
	winget := &laneTestDriver{
		name: "winget", installed: map[string]bool{"Git.Install": true}, versions: map[string]string{"Git.Install": "9.9"},
	}
	originalConfigCatalog := loadConfigRestoreCatalogFn
	originalRoot := resolveRepoRootFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{module.ID: module}, nil, nil
	}
	resolveRepoRootFn = func() string { return repoRoot }
	t.Cleanup(func() {
		loadConfigRestoreCatalogFn = originalConfigCatalog
		resolveRepoRootFn = originalRoot
	})

	var result *RestoreData
	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": chocolatey}, nil, func() {
		got, envErr := RunRestore(RestoreFlags{Manifest: manifestPath, EnableRestore: true})
		if envErr != nil {
			t.Fatalf("RunRestore: %+v", envErr)
		}
		result = got.(*RestoreData)
	})

	if winget.batchCalls != 0 || len(winget.detectCalls) != 0 {
		t.Fatalf("standalone restore fell back to Winget: batch=%d detect=%v", winget.batchCalls, winget.detectCalls)
	}
	if chocolatey.batchCalls != 2 {
		t.Fatalf("Chocolatey preview/final queries = %d, want 2", chocolatey.batchCalls)
	}
	if result == nil || result.ConfigResultFields == nil || len(result.ConfigResolutions) != 1 {
		t.Fatalf("restore result = %+v", result)
	}
	resolution := result.ConfigResolutions[0]
	if resolution.Status != planner.StatusRestored || resolution.TargetGeneration != "g2" ||
		len(resolution.TargetCandidates) != 1 || resolution.TargetCandidates[0].Evidence.Driver != "chocolatey" {
		t.Fatalf("standalone Chocolatey resolution = %+v", resolution)
	}
	data, err := os.ReadFile(filepath.Join(targetRoot, "settings.json"))
	if err != nil || !strings.Contains(string(data), `"schema": 2`) {
		t.Fatalf("standalone restored target = %q, %v", data, err)
	}
}

func chocolateyPostInstallGenerationFixture(t *testing.T) (string, string, *modules.Module) {
	t.Helper()
	manifestPath, targetRoot, module := postInstallGenerationFixture(t)
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_CHOCOLATEY_TARGET", targetRoot)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var captured manifest.Manifest
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	captured.Apps[0].Driver = "chocolatey"
	captured.Apps[0].Version = "2.0"
	captured.Apps[0].Refs["windows"] = "Git.Install"
	captured.ConfigCaptures[0].SourceInstance.Evidence.Backend = "chocolatey"
	captured.ConfigCaptures[0].SourceInstance.Evidence.Driver = "chocolatey"
	captured.ConfigCaptures[0].SourceInstance.Evidence.Ref = "Git.Install"
	encoded, err := json.Marshal(captured)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	module.Matches = modules.MatchCriteria{Chocolatey: []string{"git.install"}}
	module.Config.InstanceDetectors = []modules.InstanceDetectorDef{{ID: "installed", Type: "package"}}
	target := captureTestEnvPath("ENDSTATE_CHOCOLATEY_TARGET", "settings.json")
	for generationIndex := range module.Config.Sets[0].Generations {
		module.Config.Sets[0].Generations[generationIndex].Restore[0].Target = target
	}
	return manifestPath, targetRoot, module
}

func (enumerator countedCaptureEnumerator) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	*enumerator.calls++
	return enumerator.packages, nil
}

func TestDriverLaneConfigRestoreEvidenceAggregatesAndRefreshesEveryLane(t *testing.T) {
	winget := &laneTestDriver{
		name: "winget", installed: map[string]bool{"Vendor.App": true}, versions: map[string]string{"Vendor.App": "1.0"},
	}
	chocolatey := &laneTestDriver{
		name: "chocolatey", installed: map[string]bool{"Git.Install": true}, versions: map[string]string{"Git.Install": "2.47.1"},
	}
	apps := []manifest.App{
		{ID: "vendor", Refs: map[string]string{"windows": "Vendor.App"}},
		{ID: "git", Driver: "chocolatey", Refs: map[string]string{"windows": "Git.Install"}},
	}
	lanes := []packageDriverLane{
		{name: "winget", drv: winget, apps: []*routedDriverApp{{app: apps[0], ref: "Vendor.App", driverName: "winget", drv: winget}}},
		{name: "chocolatey", drv: chocolatey, apps: []*routedDriverApp{{app: apps[1], ref: "Git.Install", driverName: "chocolatey", drv: chocolatey}}},
	}
	request := configRestoreDetectionRequest{Modules: map[string]*modules.Module{
		"apps.vendor": {ID: "apps.vendor", Matches: modules.MatchCriteria{Winget: []string{"Vendor.App"}}},
		"apps.git":    {ID: "apps.git", Matches: modules.MatchCriteria{Chocolatey: []string{"git.install"}}},
	}}
	source := newDriverLaneConfigRestoreEvidenceSource(lanes)

	preview, err := source.Snapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	winget.versions["Vendor.App"] = "1.1"
	chocolatey.versions["Git.Install"] = "3.0"
	final, err := source.Snapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	if winget.batchCalls != 2 || chocolatey.batchCalls != 2 {
		t.Fatalf("fresh lane queries winget=%d chocolatey=%d", winget.batchCalls, chocolatey.batchCalls)
	}
	assertPackageEvidence := func(label string, snapshot configRestoreDetectionEvidence, moduleID, backend, version string) {
		t.Helper()
		got := snapshot.PackagesByModule[moduleID]
		if len(got) != 1 || got[0].Backend != backend || got[0].Driver != backend || got[0].RawVersion != version {
			t.Fatalf("%s %s evidence = %+v", label, moduleID, got)
		}
	}
	assertPackageEvidence("preview", preview, "apps.vendor", "winget", "1.0")
	assertPackageEvidence("preview", preview, "apps.git", "chocolatey", "2.47.1")
	assertPackageEvidence("final", final, "apps.vendor", "winget", "1.1")
	assertPackageEvidence("final", final, "apps.git", "chocolatey", "3.0")
}

func TestDriverLaneConfigRestoreEvidenceIsolatesUnavailableLaneByOwnership(t *testing.T) {
	winget := &laneTestDriver{
		name: "winget", installed: map[string]bool{"Vendor.App": true}, versions: map[string]string{"Vendor.App": "1.0"},
	}
	apps := []manifest.App{
		{ID: "vendor", Refs: map[string]string{"windows": "Vendor.App"}},
		{ID: "git", Driver: "chocolatey", Refs: map[string]string{"windows": "Git.Install"}},
	}
	lanes := []packageDriverLane{
		{name: "winget", drv: winget, apps: []*routedDriverApp{{app: apps[0], ref: "Vendor.App", driverName: "winget", drv: winget}}},
		{name: "chocolatey", err: errors.New("chocolatey unavailable"), apps: []*routedDriverApp{{app: apps[1], ref: "Git.Install", driverName: "chocolatey", err: errors.New("chocolatey unavailable")}}},
	}
	request := configRestoreDetectionRequest{Modules: map[string]*modules.Module{
		"apps.vendor":    {ID: "apps.vendor", Matches: modules.MatchCriteria{Winget: []string{"Vendor.App"}}},
		"apps.git":       {ID: "apps.git", Matches: modules.MatchCriteria{Chocolatey: []string{"git.install"}}},
		"apps.path-only": {ID: "apps.path-only", Matches: modules.MatchCriteria{PathExists: []string{"unused"}}},
	}}

	evidence, err := newDriverLaneConfigRestoreEvidenceSource(lanes).Snapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := evidence.FailedModules["apps.git"]; !failed {
		t.Fatalf("Chocolatey-owned module not marked failed: %+v", evidence.FailedModules)
	}
	if _, failed := evidence.FailedModules["apps.vendor"]; failed {
		t.Fatalf("Winget-owned module poisoned by Chocolatey failure: %+v", evidence.FailedModules)
	}
	if _, failed := evidence.FailedModules["apps.path-only"]; failed {
		t.Fatalf("path-only module poisoned by Chocolatey failure: %+v", evidence.FailedModules)
	}
	got := evidence.PackagesByModule["apps.vendor"]
	if len(got) != 1 || got[0].Driver != "winget" || got[0].RawVersion != "1.0" {
		t.Fatalf("unrelated Winget evidence = %+v", got)
	}
}

func TestRunCapture_ChocolateyGenerationUsesDriverQualifiedEvidence(t *testing.T) {
	dir := t.TempDir()
	configRoot := filepath.Join(dir, "config")
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "prefs.json"), []byte("chocolatey"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_CAPTURE_CONFIG", configRoot)

	mod := testCaptureGenerationModule(t, captureGenerationModuleSpec{
		ID: "apps.git", ChocolateyRef: "git.install",
		Detectors: []modules.InstanceDetectorDef{{ID: "installed", Type: "package"}},
		Sets: []testCaptureSet{{ID: "preferences", Generations: []testCaptureGeneration{
			{ID: "g1", Range: ">=2.0 <3.0", Capture: true},
			{ID: "g2", Range: ">=3.0 <4.0", Capture: true},
		}}},
		CaptureSource: captureTestEnvPath("ENDSTATE_CAPTURE_CONFIG", "prefs.json"),
	})
	withCaptureCatalogLoader(t, map[string]*modules.Module{mod.ID: mod}, nil)

	originalResolve := resolveCaptureEnumeratorFn
	originalRealizer := newRealizerFn
	originalGOOS := captureGOOSFn
	wingetCalls, chocolateyCalls := 0, 0
	resolveCaptureEnumeratorFn = func(name string, _ bool) (driver.InstalledEnumerator, error) {
		switch strings.ToLower(name) {
		case "chocolatey":
			return countedCaptureEnumerator{calls: &chocolateyCalls, packages: []driver.InstalledPackage{{
				Ref: "Git.Install", DisplayName: "Git", Version: "2.47.1",
			}}}, nil
		case "winget":
			wingetCalls++
			return nil, errors.New("Winget must not be consulted for explicit Chocolatey capture")
		default:
			return nil, errors.New("unexpected capture driver: " + name)
		}
	}
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = originalResolve
		newRealizerFn = originalRealizer
		captureGOOSFn = originalGOOS
	})

	raw, captureErr := RunCapture(CaptureFlags{
		Out: filepath.Join(dir, "capture.jsonc"), Drivers: []string{"chocolatey"},
	})
	if captureErr != nil {
		t.Fatalf("RunCapture: %+v", captureErr)
	}
	result := raw.(*CaptureResult)
	if wingetCalls != 0 || chocolateyCalls != 1 {
		t.Fatalf("enumerator calls winget=%d chocolatey=%d", wingetCalls, chocolateyCalls)
	}
	if result.ConfigCapture == nil || len(result.ConfigCapture.ConfigSets) != 1 {
		t.Fatalf("config capture = %+v", result.ConfigCapture)
	}
	row := result.ConfigCapture.ConfigSets[0]
	if row.Status != CaptureConfigStatusCaptured || row.SourceGeneration != "g1" {
		t.Fatalf("captured generation = %+v", row)
	}
	evidence := row.SourceInstance.Evidence
	if evidence == nil || evidence.Backend != "chocolatey" || evidence.Driver != "chocolatey" ||
		!strings.EqualFold(evidence.Ref, "git.install") || row.SourceInstance.RawVersion != "2.47.1" {
		t.Fatalf("Chocolatey source evidence = %+v", row.SourceInstance)
	}
}
