// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

// This proof uses the production versioned module and real collection/restore bytes.
func TestLinuxVersionedConfigCaptureRestoreRetainsPlatformProvenance(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux host-coordinate workflow")
	}
	root := t.TempDir()
	sourceHome := filepath.Join(root, "source[home]")
	t.Setenv("HOME", sourceHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sourceHome, ".config"))
	contents := map[string][]byte{".ripgreprc": []byte("--hidden\n--line-number\n"), ".config/ripgrep/config": []byte("--smart-case\n--line-number\n")}
	for name, data := range contents {
		target := filepath.Join(sourceHome, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../../../modules/apps/ripgrep/module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	catalog := modules.FilterCatalogForPlatform(map[string]*modules.Module{mod.ID: mod}, "linux")
	planning := planCaptureConfig(catalog, nil, nil)
	if len(planning.GenerationPlans) != 1 || len(planning.LegacyModules) != 0 {
		t.Fatalf("flat Linux module must use one generation capture without legacy fallback: generation=%d legacy=%d diagnostics=%+v", len(planning.GenerationPlans), len(planning.LegacyModules), planning.PreplanningDiagnostics)
	}
	manifestPath := filepath.Join(root, "capture.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"Platform fixture","apps":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	captured, err := bundle.CreateCaptureBundle(bundle.CaptureBundleRequest{ManifestPath: manifestPath, OutputPath: filepath.Join(root, "capture.zip"), Modules: planning.Modules, GenerationPlans: planning.GenerationPlans})
	if err != nil {
		t.Fatal(err)
	}
	if len(captured.ConfigCaptures) != 1 || len(captured.ConfigCaptures[0].PayloadManifest) != 2 || len(captured.LegacyConfigLanes) != 0 {
		t.Fatalf("capture did not retain both files in one generation: %+v", captured)
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
	if len(loaded.Apps) != 0 || len(loaded.Restore) != 0 || len(loaded.LegacyConfigLanes) != 0 {
		t.Fatalf("flat settings fabricated app or legacy authority: %+v", loaded)
	}
	capture := loaded.ConfigCaptures[0]
	if capture.CaptureModule.SchemaVersion != 3 || capture.SourcePlatform != "linux" || capture.ConfigSetID != "default" || capture.SourceGeneration != "g2" {
		t.Fatalf("authored provenance was lost: %+v", capture)
	}
	targetHome := filepath.Join(root, "target{home}")
	if err := os.MkdirAll(targetHome, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", targetHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(targetHome, ".config"))
	oldRoot, oldGOOS, oldLoader := resolveRepoRootFn, captureGOOSFn, loadConfigRestoreCatalogFn
	resolveRepoRootFn = func() string { return root }
	captureGOOSFn = func() string { return "linux" }
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return catalog, nil, nil
	}
	t.Cleanup(func() { resolveRepoRootFn = oldRoot; captureGOOSFn = oldGOOS; loadConfigRestoreCatalogFn = oldLoader })
	result, envErr := RunRestore(RestoreFlags{Manifest: extracted, EnableRestore: true})
	if envErr != nil {
		t.Fatalf("restore failed: %+v", envErr)
	}
	restored := result.(*RestoreData)
	if len(restored.ConfigResolutions) != 1 || restored.ConfigResolutions[0].Status != planner.StatusRestored || restored.ConfigResolutions[0].Resolution != planner.ResolutionDirect || restored.ConfigResolutions[0].SourcePlatform != "linux" || restored.ConfigResolutions[0].TargetPlatform != "linux" {
		t.Fatalf("same-platform restore resolution: %+v", restored.ConfigResolutions)
	}
	for name, want := range contents {
		got, err := os.ReadFile(filepath.Join(targetHome, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("restored %s = %q, err=%v; want %q", name, got, err, want)
		}
	}
	// A target's accepted fingerprint cannot authorize a cross-platform write.
	windows := modules.ProjectModuleForPlatform(mod, "windows")
	variant := mod.Platforms["windows"]
	variant.Config, variant.Capture, variant.Restore = windows.Config, nil, nil
	variant.Config.Sets[0].Generations[0].AcceptsSourceFingerprints = []string{capture.SourceGenerationFingerprint}
	mod.Platforms["windows"] = variant
	trustedBytes, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := modules.ParseModuleJSON(trustedBytes)
	if err != nil {
		t.Fatal(err)
	}
	windows = modules.ProjectModuleForPlatform(trusted, "windows")
	crossRoot := filepath.Join(root, "cross-platform")
	crossHome := filepath.Join(crossRoot, "home")
	if err := os.MkdirAll(crossHome, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("existing-target-must-stay-exact\n")
	crossTarget := filepath.Join(crossHome, ".ripgreprc")
	if err := os.WriteFile(crossTarget, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", crossHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(crossHome, ".config"))
	resolveRepoRootFn = func() string { return crossRoot }
	captureGOOSFn = func() string { return "windows" }
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{windows.ID: windows}, nil, nil
	}
	instances, err := modules.DiscoverInstances(windows, nil, modules.DiscoveryOptions{})
	if err != nil || len(instances) != 1 {
		t.Fatalf("Windows target: %v, %v", instances, err)
	}
	for _, explicit := range []bool{false, true} {
		flags := RestoreFlags{Manifest: extracted, EnableRestore: true, Events: "jsonl"}
		if explicit {
			flags.RestoreTargets = []string{capture.CaptureID + "=" + instances[0].ID}
		}
		var value interface{}
		eventOutput := captureStderr(t, func() { value, envErr = RunRestore(flags) })
		if envErr != nil {
			t.Fatalf("cross-platform refusal: %+v", envErr)
		}
		resolution := value.(*RestoreData).ConfigResolutions[0]
		if resolution.Reason == nil || *resolution.Reason != planner.ReasonPlatformMappingMissing || resolution.Status != planner.StatusSkipped || resolution.SourcePlatform != "linux" || resolution.TargetPlatform != "windows" {
			t.Fatalf("cross-platform resolution (explicit=%v): %+v", explicit, resolution)
		}
		found := false
		for _, event := range roundTripParseJSONL(t, eventOutput) {
			if event["event"] == "config-resolution" {
				found = event["sourcePlatform"] == "linux" && event["targetPlatform"] == "windows" && event["reason"] == "platform_mapping_missing"
			}
		}
		if !found {
			t.Fatalf("platform fields missing from events: %s", eventOutput)
		}
	}
	if got, err := os.ReadFile(crossTarget); err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("cross-platform write: %q, %v", got, err)
	}
	transactions, err := filepath.Glob(filepath.Join(crossRoot, "state", "config-restore", "v1", "transactions", "*"))
	if err != nil || len(transactions) != 0 {
		t.Fatalf("cross-platform transaction created: %v, %v", transactions, err)
	}

	// Relabeling a genuine schema-v3 snapshot as schema 2 must fail integrity.
	loaded.ConfigCaptures[0].CaptureModule.SchemaVersion = 2
	loaded.ConfigCaptures[0].SourcePlatform = ""
	attacked, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extracted, attacked, 0600); err != nil {
		t.Fatal(err)
	}
	value, envErr := RunRestore(RestoreFlags{Manifest: extracted, EnableRestore: true})
	if envErr != nil {
		t.Fatalf("downgrade attack should be isolated to its capture: %+v", envErr)
	}
	resolution := value.(*RestoreData).ConfigResolutions[0]
	if resolution.Reason == nil || *resolution.Reason != planner.ReasonPayloadIntegrityFailed || resolution.Status != planner.StatusFailed {
		t.Fatalf("schema downgrade accepted: %+v", resolution)
	}
	if got, err := os.ReadFile(crossTarget); err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("downgrade attack wrote target: %q, %v", got, err)
	}

}
