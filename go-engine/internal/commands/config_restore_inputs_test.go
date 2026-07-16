// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func TestBuildConfigRestoreInputsCopiesPortableSourceFacts(t *testing.T) {
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-source", "apps.source", "preferences")
	capture.SourceInstance.RawVersion = "25.4-beta"
	capture.SourceInstance.NormalizedVersion = ""
	capture.SourceInstance.Evidence = &manifest.ConfigSourceInstanceEvidence{
		Type: "package", AppID: "source", Backend: "winget", Platform: "windows", Ref: "Vendor.Source", Driver: "winget",
	}
	capture.PayloadManifest = []manifest.PayloadManifestEntry{{RelativePath: "nested/preferences.json", Size: 41, SHA256: strings.Repeat("b", 64)}}
	mf := &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}}

	inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
		Manifest: mf, ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"),
	})
	if envErr != nil {
		t.Fatalf("buildConfigRestoreInputs: %+v", envErr)
	}
	if len(inputs.generationSources) != 1 {
		t.Fatalf("generation sources = %d, want 1", len(inputs.generationSources))
	}
	got := inputs.generationSources[0]
	if !filepath.IsAbs(got.payloadRoot) || got.source.CaptureID != capture.CaptureID ||
		got.source.ModuleID != capture.ModuleID || got.source.ConfigSetID != capture.ConfigSetID ||
		got.source.Instance.ID != capture.SourceInstance.ID || got.source.Instance.DetectorID != capture.SourceInstance.DetectorID ||
		got.source.Instance.RawVersion != "25.4-beta" || got.source.Instance.NormalizedVersion != "" ||
		got.source.Instance.Evidence.Type != "package" || got.source.Instance.Evidence.Ref != "Vendor.Source" ||
		got.source.Generation != capture.SourceGeneration || got.source.GenerationFingerprint != capture.SourceGenerationFingerprint ||
		got.source.ModuleRevision != capture.CaptureModule.ContentHash || got.source.CaptureModuleSchemaVersion != 2 {
		t.Fatalf("source facts lost fidelity: %+v", got)
	}
	if !reflect.DeepEqual(got.payloadManifest, capture.PayloadManifest) {
		t.Fatalf("payload manifest = %#v, want %#v", got.payloadManifest, capture.PayloadManifest)
	}

	// The command snapshot must not alias mutable manifest memory.
	mf.ConfigCaptures[0].SourceInstance.RawVersion = "mutated"
	mf.ConfigCaptures[0].SourceInstance.Evidence.Ref = "Mutated.Source"
	mf.ConfigCaptures[0].PayloadManifest[0].RelativePath = "mutated.json"
	if got.source.Instance.RawVersion != "25.4-beta" || got.source.Instance.Evidence.Ref != "Vendor.Source" ||
		got.payloadManifest[0].RelativePath != "nested/preferences.json" {
		t.Fatalf("input snapshot changed after manifest mutation: %+v", got)
	}
}

func TestBuildConfigRestoreInputsSeparatesMixedV2Lanes(t *testing.T) {
	manifestDir := t.TempDir()
	generation := commandTestConfigCapture(t, manifestDir, "capture-generation", "apps.generation", "preferences")
	legacyID := bundle.LegacyCaptureID("apps.legacy")
	legacyRoot := path.Join("configs", legacyID)
	if err := os.MkdirAll(filepath.Join(manifestDir, filepath.FromSlash(legacyRoot)), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyRestore := manifest.RestoreEntry{
		Type: "copy", Source: "./" + path.Join(legacyRoot, "settings.json"), Target: "%APPDATA%/Legacy/settings.json",
		FromModule: "apps.legacy", LegacyCaptureID: legacyID, Exclude: []string{"cache/**"},
	}
	mf := &manifest.Manifest{
		Version:        2,
		ConfigCaptures: []manifest.ConfigCapture{generation},
		LegacyConfigLanes: []manifest.LegacyConfigLane{{
			CaptureID: legacyID, ModuleID: "apps.legacy", ModuleSchemaVersion: 1, PayloadRoot: legacyRoot,
		}},
		Restore: []manifest.RestoreEntry{legacyRestore},
	}

	inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
		Manifest: mf, ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"),
	})
	if envErr != nil {
		t.Fatalf("buildConfigRestoreInputs: %+v", envErr)
	}
	if !inputs.hasConfigPayloads || len(inputs.generationSources) != 1 || len(inputs.legacyLanes) != 1 || len(inputs.ordinaryRestores) != 0 {
		t.Fatalf("mixed inputs = %+v", inputs)
	}
	lane := inputs.legacyLanes[0]
	if lane.captureID != legacyID || lane.moduleID != "apps.legacy" || lane.configSetID != "legacy" ||
		!filepath.IsAbs(lane.payloadRoot) || len(lane.restoreEntries) != 1 {
		t.Fatalf("legacy lane = %+v", lane)
	}
	mf.Restore[0].Exclude[0] = "mutated/**"
	if lane.restoreEntries[0].Exclude[0] != "cache/**" {
		t.Fatal("legacy restore entry aliases manifest memory")
	}
}

func TestBuildConfigRestoreInputsGroupsV1ModulesAndLeavesAnonymousActionsOrdinary(t *testing.T) {
	mf := &manifest.Manifest{Version: 1, Restore: []manifest.RestoreEntry{
		{Type: "copy", Source: "z", Target: "z-target", FromModule: "apps.zed"},
		{Type: "copy", Source: "inline", Target: "inline-target", Exclude: []string{"tmp/**"}},
		{Type: "copy", Source: "a", Target: "a-target", FromModule: "apps.alpha"},
		{Type: "copy", Source: "z-two", Target: "z-two-target", FromModule: "apps.zed"},
	}}

	inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
		Manifest: mf, ManifestPath: filepath.Join(t.TempDir(), "manifest.jsonc"),
	})
	if envErr != nil {
		t.Fatalf("buildConfigRestoreInputs: %+v", envErr)
	}
	if len(inputs.generationSources) != 0 || len(inputs.legacyLanes) != 2 || len(inputs.ordinaryRestores) != 1 {
		t.Fatalf("v1 inputs = %+v", inputs)
	}
	if inputs.legacyLanes[0].moduleID != "apps.alpha" || inputs.legacyLanes[1].moduleID != "apps.zed" {
		t.Fatalf("legacy ordering = %q, %q", inputs.legacyLanes[0].moduleID, inputs.legacyLanes[1].moduleID)
	}
	for _, lane := range inputs.legacyLanes {
		if lane.captureID != bundle.LegacyCaptureID(lane.moduleID) || lane.configSetID != "legacy" || lane.payloadRoot != "" {
			t.Fatalf("v1 lane identity invented or unstable: %+v", lane)
		}
	}
	if len(inputs.legacyLanes[1].restoreEntries) != 2 || inputs.ordinaryRestores[0].FromModule != "" {
		t.Fatalf("v1 restore grouping = %+v", inputs)
	}
	mf.Restore[1].Exclude[0] = "mutated/**"
	if inputs.ordinaryRestores[0].Exclude[0] != "tmp/**" {
		t.Fatal("ordinary restore entry aliases manifest memory")
	}
}

func TestBuildConfigRestoreInputsRetainsFilteredV1LaneForSkippedProjection(t *testing.T) {
	mf := &manifest.Manifest{Version: 1, Restore: []manifest.RestoreEntry{
		{Type: "copy", Source: "beta", Target: "beta-target", FromModule: "apps.beta"},
		{Type: "copy", Source: "alpha", Target: "alpha-target", FromModule: "apps.alpha"},
	}}

	inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
		Manifest: mf, ManifestPath: filepath.Join(t.TempDir(), "manifest.jsonc"), RestoreFilter: "alpha",
	})
	if envErr != nil {
		t.Fatalf("buildConfigRestoreInputs: %+v", envErr)
	}
	if len(inputs.legacyLanes) != 2 {
		t.Fatalf("legacy lanes = %+v, want selected and filtered identities", inputs.legacyLanes)
	}
	if inputs.legacyLanes[0].moduleID != "apps.alpha" || !inputs.legacyLanes[0].selected ||
		inputs.legacyLanes[1].moduleID != "apps.beta" || inputs.legacyLanes[1].selected {
		t.Fatalf("legacy selection state = %+v", inputs.legacyLanes)
	}
	if len(inputs.targetMappings) != 0 {
		t.Fatalf("v1 lane acquired executable target mappings: %#v", inputs.targetMappings)
	}
}

func TestBuildConfigRestoreInputsRejectsAmbiguousV1ModuleIdentity(t *testing.T) {
	inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
		Manifest: &manifest.Manifest{Version: 1, Restore: []manifest.RestoreEntry{{
			Type: "copy", Source: "a", Target: "b", FromModule: " apps.git ",
		}}},
		ManifestPath: filepath.Join(t.TempDir(), "manifest.jsonc"),
	})
	if envErr == nil || envErr.Code != envelope.ErrManifestValidationError {
		t.Fatalf("error = %+v, want %s", envErr, envelope.ErrManifestValidationError)
	}
	if inputs.hasConfigPayloads || len(inputs.legacyLanes) != 0 {
		t.Fatalf("ambiguous v1 identity produced a lane: %+v", inputs)
	}
}

func TestBuildConfigRestoreInputsRejectsUnsafeMissingAndLinkedPayloadRoots(t *testing.T) {
	t.Run("unsafe traversal", func(t *testing.T) {
		manifestDir := t.TempDir()
		capture := commandTestConfigCapture(t, manifestDir, "capture-unsafe", "apps.unsafe", "preferences")
		capture.PayloadRoot = "../outside"
		_, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{Manifest: &manifest.Manifest{
			Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture},
		}, ManifestPath: filepath.Join(manifestDir, "manifest.jsonc")})
		if envErr == nil || envErr.Code != envelope.ErrManifestValidationError {
			t.Fatalf("error = %+v, want unsafe payload rejection", envErr)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		manifestDir := t.TempDir()
		capture := commandTestConfigCapture(t, manifestDir, "capture-missing", "apps.missing", "preferences")
		if err := os.RemoveAll(filepath.Join(manifestDir, filepath.FromSlash(capture.PayloadRoot))); err != nil {
			t.Fatal(err)
		}
		_, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{Manifest: &manifest.Manifest{
			Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture},
		}, ManifestPath: filepath.Join(manifestDir, "manifest.jsonc")})
		if envErr == nil || envErr.Code != envelope.ErrManifestValidationError {
			t.Fatalf("error = %+v, want missing payload rejection", envErr)
		}
	})

	t.Run("linked root", func(t *testing.T) {
		manifestDir := t.TempDir()
		captureID := "capture-linked"
		configsDir := filepath.Join(manifestDir, "configs")
		if err := os.MkdirAll(configsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(configsDir, captureID)); err != nil {
			t.Skipf("symlinks unavailable on this host: %v", err)
		}
		capture := commandConfigCapture(captureID, "apps.linked", "preferences")
		_, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{Manifest: &manifest.Manifest{
			Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture},
		}, ManifestPath: filepath.Join(manifestDir, "manifest.jsonc")})
		if envErr == nil || envErr.Code != envelope.ErrManifestValidationError {
			t.Fatalf("error = %+v, want linked payload rejection", envErr)
		}
	})
}

func TestBuildConfigRestoreInputsValidatesMappingsBeforeFiltering(t *testing.T) {
	manifestDir := t.TempDir()
	alpha := commandTestConfigCapture(t, manifestDir, "capture-alpha", "apps.alpha", "preferences")
	beta := commandTestConfigCapture(t, manifestDir, "capture-beta", "apps.beta", "preferences")
	mf := &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{beta, alpha}}

	t.Run("unknown filtered capture still fails", func(t *testing.T) {
		_, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
			Manifest: mf, ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"), RestoreFilter: "alpha",
			RestoreTargets: []string{"capture-unknown=instance-x"},
		})
		if envErr == nil || envErr.Code != envelope.ErrInvalidRestoreTarget {
			t.Fatalf("error = %+v, want %s", envErr, envelope.ErrInvalidRestoreTarget)
		}
	})

	t.Run("valid filtered mapping is ignored", func(t *testing.T) {
		inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
			Manifest: mf, ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"), RestoreFilter: "alpha",
			RestoreTargets: []string{"capture-beta=instance-beta", "capture-alpha=instance-alpha"},
		})
		if envErr != nil {
			t.Fatalf("buildConfigRestoreInputs: %+v", envErr)
		}
		if len(inputs.generationSources) != 2 || inputs.generationSources[0].source.CaptureID != "capture-alpha" ||
			!inputs.generationSources[0].selected || inputs.generationSources[1].source.CaptureID != "capture-beta" ||
			inputs.generationSources[1].selected {
			t.Fatalf("filtered sources = %+v", inputs.generationSources)
		}
		if !reflect.DeepEqual(inputs.targetMappings, map[string]string{"capture-alpha": "instance-alpha"}) {
			t.Fatalf("filtered mappings = %#v", inputs.targetMappings)
		}
	})
}

func TestBuildConfigRestoreInputsRejectsUnassociatedV2FlatRestore(t *testing.T) {
	inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{
		Manifest: &manifest.Manifest{Version: 2, Restore: []manifest.RestoreEntry{{
			Type: "copy", Source: "./configs/legacy/settings.json", Target: "target", FromModule: "apps.legacy",
		}}},
		ManifestPath: filepath.Join(t.TempDir(), "manifest.jsonc"),
	})
	if envErr == nil || envErr.Code != envelope.ErrManifestValidationError {
		t.Fatalf("error = %+v, want invalid v2 association", envErr)
	}
	if inputs.hasConfigPayloads || len(inputs.legacyLanes) != 0 {
		t.Fatalf("invalid v2 fell back to a legacy lane: %+v", inputs)
	}
}

func commandTestConfigCapture(t *testing.T, manifestDir, captureID, moduleID, configSetID string) manifest.ConfigCapture {
	t.Helper()
	capture := commandConfigCapture(captureID, moduleID, configSetID)
	if err := os.MkdirAll(filepath.Join(manifestDir, filepath.FromSlash(capture.PayloadRoot)), 0o755); err != nil {
		t.Fatal(err)
	}
	return capture
}

func commandConfigCapture(captureID, moduleID, configSetID string) manifest.ConfigCapture {
	return manifest.ConfigCapture{
		CaptureID: captureID, ModuleID: moduleID, ConfigSetID: configSetID,
		SourceInstance: manifest.ConfigSourceInstance{
			ID: "source-instance", DetectorID: "installed", RawVersion: "25.0", NormalizedVersion: "25",
			Evidence: &manifest.ConfigSourceInstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.App"},
		},
		SourceGeneration: "g1", SourceGenerationFingerprint: strings.Repeat("a", 64),
		CaptureModule: manifest.CaptureModuleProvenance{
			SchemaVersion: 2, ContentHash: strings.Repeat("c", 64), SnapshotPath: path.Join("provenance/modules", moduleID+".json"),
		},
		PayloadRoot: path.Join("configs", captureID), PayloadManifest: []manifest.PayloadManifestEntry{},
	}
}
