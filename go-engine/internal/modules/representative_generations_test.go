// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestRepresentativeConfigGenerationModules(t *testing.T) {
	repoRoot := representativeRepoRoot(t)
	catalog, diagnostics, err := LoadCatalogWithDiagnostics(filepath.Join(repoRoot, "modules", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("repository module diagnostics = %+v", diagnostics)
	}

	t.Run("Windows Terminal stable selectorless generation", func(t *testing.T) {
		mod := requireRepresentativeModule(t, catalog, "apps.windows-terminal", 1)
		if !reflect.DeepEqual(mod.Matches.Winget, []string{"Microsoft.WindowsTerminal"}) {
			t.Fatalf("winget matches = %v", mod.Matches.Winget)
		}
		assertPackageDetector(t, mod, "installed")
		set := &mod.Config.Sets[0]
		generation := &set.Generations[0]
		if set.ID != "preferences" || generation.ID != "g1" || generation.Order != 1 || len(generation.Matches) != 0 || len(set.Migrations) != 0 {
			t.Fatalf("stable generation shape = set %+v generation %+v", set, generation)
		}
		selected, err := SelectGeneration(set, NewVersionEvidence("vendor-version-unneeded"))
		if err != nil || selected != generation {
			t.Fatalf("selectorless SelectGeneration = %+v, %v", selected, err)
		}
		wantSource := `%LOCALAPPDATA%\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json`
		assertSingleFileGeneration(t, generation, wantSource, "settings.json", "settings.json", wantSource, "json-parse")
	})

	t.Run("Studio One side-by-side versions 5 through 7", func(t *testing.T) {
		mod := requireRepresentativeModule(t, catalog, "apps.studio-one", 1)
		if !reflect.DeepEqual(mod.Matches.PathExists, []string{
			`%APPDATA%\PreSonus\Studio One 7`,
			`%APPDATA%\PreSonus\Studio One 6`,
			`%APPDATA%\PreSonus\Studio One 5`,
		}) {
			t.Fatalf("path matches changed = %v", mod.Matches.PathExists)
		}
		if len(mod.Verify) != 0 {
			t.Fatalf("Studio One top-level verify must not reject versions 5 or 6: %+v", mod.Verify)
		}
		if len(mod.Config.InstanceDetectors) != 1 {
			t.Fatalf("detectors = %+v", mod.Config.InstanceDetectors)
		}
		detector := mod.Config.InstanceDetectors[0]
		if detector.ID != "versions" || detector.Type != "path" || detector.Glob != `%APPDATA%\PreSonus\Studio One *` || detector.VersionPattern != `^Studio One (?P<version>[0-9]+)$` {
			t.Fatalf("path detector = %+v", detector)
		}
		set := &mod.Config.Sets[0]
		generation := &set.Generations[0]
		if set.ID != "preferences" || len(generation.Matches) != 1 || generation.Matches[0].VersionRange != ">=5 <8" || len(set.Migrations) != 0 {
			t.Fatalf("Studio One generation = set %+v generation %+v", set, generation)
		}
		for _, version := range []string{"5", "6.6", "7"} {
			selected, err := SelectGeneration(set, NewVersionEvidence(version))
			if err != nil || selected != generation {
				t.Errorf("SelectGeneration(%s) = %+v, %v", version, selected, err)
			}
		}
		if selected, err := SelectGeneration(set, NewVersionEvidence("8")); err == nil || selected != nil {
			t.Fatalf("SelectGeneration(8) = %+v, %v; version 8 must remain unsupported", selected, err)
		}
		if generation.Capture == nil || len(generation.Capture.Files) != 1 || generation.Capture.Files[0].Source != `${instance.root}` || generation.Capture.Files[0].Dest != "settings" || !generation.Capture.Files[0].Optional {
			t.Fatalf("Studio One capture = %+v", generation.Capture)
		}
		if len(generation.Restore) != 1 || generation.Restore[0].Source != "settings" || generation.Restore[0].Target != `${instance.root}` || !generation.Restore[0].Backup || !generation.Restore[0].Optional {
			t.Fatalf("Studio One restore = %+v", generation.Restore)
		}
		wantExcludes := []string{
			`**\PluginBlacklist.settings`,
			`**\PlugInScanner.log`,
			`**\*.log`,
			`**\Cache\**`,
			`**\*Cache*`,
			`**\Temp\**`,
			`**\*.lock`,
			`**\user.license`,
		}
		if !reflect.DeepEqual(generation.Capture.ExcludeGlobs, wantExcludes) || !reflect.DeepEqual(generation.Restore[0].Exclude, wantExcludes) {
			t.Fatalf("Studio One exclusions changed: capture=%v restore=%v", generation.Capture.ExcludeGlobs, generation.Restore[0].Exclude)
		}
		if mod.Secrets == nil || mod.Secrets.Restorer != "warn-only" || len(mod.Secrets.Files) != 3 {
			t.Fatalf("Studio One secret boundary changed: %+v", mod.Secrets)
		}
	})

	t.Run("ownCloud forward config relocation", func(t *testing.T) {
		mod := requireRepresentativeModule(t, catalog, "apps.owncloud", 2)
		if !reflect.DeepEqual(mod.Matches.Winget, []string{"ownCloud.ownCloudDesktop"}) || !reflect.DeepEqual(mod.Matches.PathExists, []string{
			`%APPDATA%\ownCloud\owncloud.cfg`,
			`%LOCALAPPDATA%\ownCloud\owncloud.cfg`,
		}) {
			t.Fatalf("ownCloud matches changed: %+v", mod.Matches)
		}
		if len(mod.Verify) != 0 {
			t.Fatalf("ownCloud top-level verify must not reject one valid generation: %+v", mod.Verify)
		}
		assertPackageDetector(t, mod, "installed")
		set := &mod.Config.Sets[0]
		if set.ID != "preferences" {
			t.Fatalf("config set id = %q", set.ID)
		}
		g1 := &set.Generations[0]
		g2 := &set.Generations[1]
		if len(g1.Matches) != 1 || g1.Matches[0].VersionRange != "<2.5" || len(g2.Matches) != 1 || g2.Matches[0].VersionRange != ">=2.5" {
			t.Fatalf("ownCloud generation ranges = g1 %+v g2 %+v", g1.Matches, g2.Matches)
		}
		assertSingleFileGeneration(t, g1, `%LOCALAPPDATA%\ownCloud\owncloud.cfg`, "local/owncloud.cfg", "local/owncloud.cfg", `%LOCALAPPDATA%\ownCloud\owncloud.cfg`, "ini-parse")
		assertSingleFileGeneration(t, g2, `%APPDATA%\ownCloud\owncloud.cfg`, "roaming/owncloud.cfg", "roaming/owncloud.cfg", `%APPDATA%\ownCloud\owncloud.cfg`, "ini-parse")
		if g1.RequiresAppClosed || g2.RequiresAppClosed {
			t.Fatal("ownCloud generations invented an app-close requirement")
		}
		if len(set.Migrations) != 1 {
			t.Fatalf("ownCloud migrations = %+v", set.Migrations)
		}
		edge := set.Migrations[0]
		if edge.From != "g1" || edge.To != "g2" || len(edge.Operations) != 1 || edge.Operations[0] != (MigrationOperationDef{Type: "file-move", Source: "local/owncloud.cfg", Target: "roaming/owncloud.cfg"}) {
			t.Fatalf("ownCloud migration edge = %+v", edge)
		}
		if len(edge.Validate) != 1 || edge.Validate[0] != (ValidationDef{Type: "ini-parse", Path: "roaming/owncloud.cfg"}) {
			t.Fatalf("ownCloud migration validation = %+v", edge.Validate)
		}
		if mod.Secrets == nil || mod.Secrets.Restorer != "warn-only" || !reflect.DeepEqual(mod.Secrets.Files, []string{`%LOCALAPPDATA%\ownCloud\cookies*.db`}) {
			t.Fatalf("ownCloud secret boundary changed: %+v", mod.Secrets)
		}
	})
}

func TestRepositoryCatalogValidatesMixedSchemaGenerations(t *testing.T) {
	repoRoot := representativeRepoRoot(t)
	catalog, diagnostics, err := LoadCatalogWithDiagnostics(filepath.Join(repoRoot, "modules", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("repository module diagnostics = %+v", diagnostics)
	}
	v1Count := 0
	v2IDs := make(map[string]struct{})
	for moduleID, mod := range catalog {
		switch mod.EffectiveSchemaVersion() {
		case 1:
			v1Count++
		case 2:
			v2IDs[moduleID] = struct{}{}
		default:
			t.Fatalf("module %s has unsupported effective schema %d", moduleID, mod.EffectiveSchemaVersion())
		}
	}
	if v1Count == 0 {
		t.Fatal("mixed catalog has no schema-v1 modules")
	}
	wantV2 := []string{"apps.owncloud", "apps.studio-one", "apps.windows-terminal"}
	if len(v2IDs) != len(wantV2) {
		t.Fatalf("schema-v2 module IDs = %v, want %v", v2IDs, wantV2)
	}
	for _, moduleID := range wantV2 {
		if _, exists := v2IDs[moduleID]; !exists {
			t.Errorf("schema-v2 catalog missing %s", moduleID)
		}
	}
	history, err := LoadGenerationHistory(filepath.Join(repoRoot, "modules", "generation-history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRepositoryGenerationHistory(catalog, history); err != nil {
		t.Fatal(err)
	}
}

func requireRepresentativeModule(t *testing.T, catalog map[string]*Module, moduleID string, generations int) *Module {
	t.Helper()
	mod := catalog[moduleID]
	if mod == nil {
		t.Fatalf("catalog missing %s", moduleID)
	}
	if mod.EffectiveSchemaVersion() != 2 || mod.Unversioned || mod.Config == nil || len(mod.Config.Sets) != 1 {
		t.Fatalf("%s is not one-set schema v2: %+v", moduleID, mod)
	}
	if mod.Capture != nil || len(mod.Restore) != 0 {
		t.Fatalf("%s leaked schema-v2 config through top-level legacy fields: capture=%+v restore=%+v", moduleID, mod.Capture, mod.Restore)
	}
	if got := len(mod.Config.Sets[0].Generations); got != generations {
		t.Fatalf("%s generations = %d, want %d", moduleID, got, generations)
	}
	return mod
}

func assertPackageDetector(t *testing.T, mod *Module, detectorID string) {
	t.Helper()
	if len(mod.Config.InstanceDetectors) != 1 || mod.Config.InstanceDetectors[0] != (InstanceDetectorDef{ID: detectorID, Type: "package"}) {
		t.Fatalf("%s package detector = %+v", mod.ID, mod.Config.InstanceDetectors)
	}
}

func assertSingleFileGeneration(t *testing.T, generation *GenerationDef, captureSource, captureDest, restoreSource, restoreTarget, validationType string) {
	t.Helper()
	if generation.Capture == nil || len(generation.Capture.Files) != 1 {
		t.Fatalf("%s capture = %+v", generation.ID, generation.Capture)
	}
	capture := generation.Capture.Files[0]
	if capture.Source != captureSource || capture.Dest != captureDest || !capture.Optional {
		t.Fatalf("%s capture file = %+v, want source=%q dest=%q optional", generation.ID, capture, captureSource, captureDest)
	}
	if len(generation.Restore) != 1 {
		t.Fatalf("%s restore = %+v", generation.ID, generation.Restore)
	}
	restore := generation.Restore[0]
	if restore.Type != "copy" || restore.Source != restoreSource || restore.Target != restoreTarget || !restore.Backup || !restore.Optional {
		t.Fatalf("%s restore = %+v", generation.ID, restore)
	}
	if len(generation.Validate) != 1 || generation.Validate[0] != (ValidationDef{Type: validationType, Path: captureDest}) {
		t.Fatalf("%s validation = %+v", generation.ID, generation.Validate)
	}
}

func representativeRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}
