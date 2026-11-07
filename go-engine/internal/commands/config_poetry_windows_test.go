// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

// captureAndRestorePoetry captures the settings file of a source profile with the
// given catalog and restores the bundle onto an empty profile. The Windows
// coordinates resolve only on a Windows host, so this file builds only there.
func captureAndRestorePoetry(t *testing.T, captureCatalog map[string]*modules.Module, selection captureSelection) (restored *RestoreData, target string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	t.Setenv("APPDATA", filepath.Join(source, "Roaming"))
	t.Setenv("USERPROFILE", source)
	t.Setenv("HOME", source)
	settings := filepath.Join(source, "Roaming", "pypoetry", "config.toml")
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, poetryWindowsSettings, 0600); err != nil {
		t.Fatal(err)
	}
	planning := planCaptureConfigWithValidation(captureCatalog, nil, nil, nil, selection)
	manifestPath := filepath.Join(root, "capture.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"Poetry","apps":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "capture.zip")
	if _, err := bundle.CreateCaptureBundle(bundle.CaptureBundleRequest{ManifestPath: manifestPath, OutputPath: archive, Modules: planning.Modules, GenerationPlans: planning.GenerationPlans}); err != nil {
		t.Fatal(err)
	}
	extracted, err := bundle.ExtractBundle(archive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(extracted)) })

	// Restore onto a machine whose settings file does not exist yet, with the shipped catalog.
	targetHome := filepath.Join(root, "target")
	t.Setenv("APPDATA", filepath.Join(targetHome, "Roaming"))
	t.Setenv("USERPROFILE", targetHome)
	t.Setenv("HOME", targetHome)
	oldRoot, oldLoader := resolveRepoRootFn, loadConfigRestoreCatalogFn
	resolveRepoRootFn = func() string { return root }
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		production, err := modules.GetCatalog(filepath.Join("..", "..", ".."))
		if err != nil {
			return nil, nil, err
		}
		return modules.FilterCatalogForPlatform(map[string]*modules.Module{"apps.poetry": production["apps.poetry"]}, "windows"), nil, nil
	}
	t.Cleanup(func() { resolveRepoRootFn = oldRoot; loadConfigRestoreCatalogFn = oldLoader })
	result, restoreErr := RunRestore(RestoreFlags{Manifest: extracted, EnableRestore: true})
	if restoreErr != nil {
		t.Fatalf("restore failed: %+v", restoreErr)
	}
	return result.(*RestoreData), filepath.Join(targetHome, "Roaming", "pypoetry", "config.toml")
}

var poetryWindowsSettings = []byte("[virtualenvs]\nin-project = true\n")

// Poetry moved from a flat schema-v1 module to schema-v3 default/g1. A capture made
// before that change must still restore to the same Windows settings file.
func TestWindowsRestoresPoetryCaptureMadeBeforeTheSchemaV3Conversion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "poetry-legacy-module.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.EffectiveSchemaVersion() != 1 {
		t.Fatalf("the frozen module is not the legacy module: schema %d", legacy.EffectiveSchemaVersion())
	}
	restored, target := captureAndRestorePoetry(t, map[string]*modules.Module{legacy.ID: legacy}, captureSelection{moduleIDs: []string{legacy.ID}})
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, poetryWindowsSettings) {
		t.Fatalf("legacy capture restored %q, err=%v; want %q", got, err, poetryWindowsSettings)
	}
	if restored.ConfigResultFields == nil || len(restored.ConfigResolutions) != 1 || restored.ConfigResolutions[0].Status != planner.StatusRestored {
		t.Fatalf("legacy capture resolution: %+v", restored.ConfigResultFields)
	}
}

// A capture made after the conversion carries default/g1 provenance and restores directly.
func TestWindowsRestoresPoetryDefaultGenerationCapture(t *testing.T) {
	production, err := modules.GetCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	flat := modules.FilterCatalogForPlatform(map[string]*modules.Module{"apps.poetry": production["apps.poetry"]}, "windows")
	restored, target := captureAndRestorePoetry(t, flat, captureSelection{})
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, poetryWindowsSettings) {
		t.Fatalf("default/g1 capture restored %q, err=%v; want %q", got, err, poetryWindowsSettings)
	}
	if restored.ConfigResultFields == nil || len(restored.ConfigResolutions) != 1 ||
		restored.ConfigResolutions[0].Status != planner.StatusRestored || restored.ConfigResolutions[0].Resolution != planner.ResolutionDirect {
		t.Fatalf("default/g1 capture resolution: %+v", restored.ConfigResultFields)
	}
}
