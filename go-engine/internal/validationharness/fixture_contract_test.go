// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestCompileRegistryDefinitionsAcceptsSafeWholeKey(t *testing.T) {
	definitions, failure := compileRegistryDefinitions(registryFixtureModule(), fixtureScenario())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(definitions.Entries) != 1 {
		t.Fatalf("registry definitions = %+v, want one entry", definitions)
	}
	definition := definitions.Entries[0]
	if definition.Coordinate != "capture.registryKeys[0]" || definition.Key != `HKCU\Software\Fixture` || definition.Destination != "apps/fixture/settings.reg" || definition.Target != `HKCU\Software\Fixture` {
		t.Fatalf("registry definition = %+v", definition)
	}
}

func TestCompileRegistryDefinitionsRejectsCaseInsensitiveFlattenedPayloadCollision(t *testing.T) {
	mod := registryFixtureModule()
	mod.Capture.RegistryKeys = append(mod.Capture.RegistryKeys, modules.CaptureRegistryKey{
		Key: `HKCU\Software\Sibling`, Dest: "apps/fixture/nested/SETTINGS.REG", Optional: true,
	})
	mod.Restore = append(mod.Restore, modules.RestoreDef{
		Type: "registry-import", Source: "./payload/apps/fixture/nested/SETTINGS.REG", Target: `HKCU\Software\Sibling`, Optional: true, Backup: true,
	})
	if _, failure := compileRegistryDefinitions(mod, fixtureScenario()); failure == nil || failure.Code != CodeUnsupportedFixture || failure.Phase != "fixture" || failure.Coordinate != "capture.registryKeys[1].dest" || failure.Detail != "capture destinations collide after the production flattened payload rewrite" {
		t.Fatalf("registry payload collision failure = %+v", failure)
	}
}

func TestCompileRegistryDefinitionsDoesNotConsultCurrentDirectory(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "apps")); err != nil {
		t.Skipf("symlink setup unavailable: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })

	if _, failure := compileRegistryDefinitions(registryFixtureModule(), fixtureScenario()); failure != nil {
		t.Fatalf("registry classification consulted current directory: %+v", failure)
	}
}

func TestCompileRegistryDefinitionsRejectsUnsafeContracts(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*modules.Module)
		coordinate string
	}{
		{"HKLM capture", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Key = `HKLM\Software\Fixture` }, "capture.registryKeys[0].key"},
		{"named value capture", func(mod *modules.Module) {
			mod.Capture.RegistryValues = []modules.CaptureRegistryValue{{Key: `HKCU\Software\Fixture`, ValueName: "setting"}}
		}, "capture.registryValues[0]"},
		{"registry set", func(mod *modules.Module) { mod.Restore[0].Type = "registry-set" }, "restore[0]"},
		{"nonportable destination", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Dest = "../settings.reg" }, "capture.registryKeys[0].dest"},
		{"home destination", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Dest = "~/settings.reg" }, "capture.registryKeys[0].dest"},
		{"environment destination", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Dest = "%APPDATA%/settings.reg" }, "capture.registryKeys[0].dest"},
		{"dollar destination", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Dest = "$HOME/settings.reg" }, "capture.registryKeys[0].dest"},
		{"control character destination", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Dest = "apps/fixture/settings\x00.reg" }, "capture.registryKeys[0].dest"},
		{"padded destination component", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Dest = "apps/fixture/ settings.reg" }, "capture.registryKeys[0].dest"},
		{"wrong payload source", func(mod *modules.Module) { mod.Restore[0].Source = "payload/apps/fixture/settings.reg" }, "restore[0].source"},
		{"mismatched target", func(mod *modules.Module) { mod.Restore[0].Target = `HKCU\Software\Other` }, "capture.registryKeys[0].key"},
		{"wildcard restore target", func(mod *modules.Module) { mod.Restore[0].Target = `HKCU\Software\Fixture*` }, "restore[0].target"},
		{"missing backup", func(mod *modules.Module) { mod.Restore[0].Backup = false }, "restore[0]"},
		{"required capture", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Optional = false }, "capture.registryKeys[0]"},
		{"required restore", func(mod *modules.Module) { mod.Restore[0].Optional = false }, "restore[0]"},
		{"duplicate import", func(mod *modules.Module) { mod.Restore = append(mod.Restore, mod.Restore[0]) }, "restore[1]"},
		{"unmatched import", func(mod *modules.Module) {
			mod.Restore = append(mod.Restore, modules.RestoreDef{Type: "registry-import", Source: "./payload/apps/fixture/other.reg", Target: `HKCU\Software\Other`, Optional: true, Backup: true})
		}, "restore[1]"},
		{"overlapping roots", func(mod *modules.Module) {
			mod.Capture.RegistryKeys = append(mod.Capture.RegistryKeys, modules.CaptureRegistryKey{Key: `HKCU\Software\Fixture\Child`, Dest: "apps/fixture/child.reg", Optional: true})
			mod.Restore = append(mod.Restore, modules.RestoreDef{Type: "registry-import", Source: "./payload/apps/fixture/child.reg", Target: `HKCU\Software\Fixture\Child`, Optional: true, Backup: true})
		}, "capture.registryKeys[1].key"},
		{"secret equal to key", func(mod *modules.Module) { mod.Secrets = &modules.SecretsDef{Files: []string{`HKCU\Software\Fixture`}} }, "capture.registryKeys[0].key"},
		{"secret below key", func(mod *modules.Module) {
			mod.Secrets = &modules.SecretsDef{Files: []string{`HKCU\Software\Fixture\Token`}}
		}, "capture.registryKeys[0].key"},
		{"wildcard identity", func(mod *modules.Module) { mod.Capture.RegistryKeys[0].Key = `HKCU\Software\Fixture*` }, "capture.registryKeys[0].key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := registryFixtureModule()
			tt.mutate(mod)
			_, failure := compileRegistryDefinitions(mod, fixtureScenario())
			if failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != tt.coordinate {
				t.Fatalf("failure = %+v, want unsupported fixture at %q", failure, tt.coordinate)
			}
		})
	}
}

func TestCompileRegistryDefinitionsRejectsRegistryImportStrategyFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*modules.RestoreDef)
	}{
		{"pattern", func(restore *modules.RestoreDef) { restore.Pattern = "*.reg" }},
		{"reason", func(restore *modules.RestoreDef) { restore.Reason = "test" }},
		{"exclude", func(restore *modules.RestoreDef) { restore.Exclude = []string{"*.reg"} }},
		{"key", func(restore *modules.RestoreDef) { restore.Key = `HKCU\Software\Fixture` }},
		{"value name", func(restore *modules.RestoreDef) { restore.ValueName = "setting" }},
		{"value type", func(restore *modules.RestoreDef) { restore.ValueType = "REG_SZ" }},
		{"data", func(restore *modules.RestoreDef) { restore.Data = "value" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := registryFixtureModule()
			tt.mutate(&mod.Restore[0])
			_, failure := compileRegistryDefinitions(mod, fixtureScenario())
			if failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "restore[0]" {
				t.Fatalf("failure = %+v, want unsupported fixture at restore[0]", failure)
			}
		})
	}
}

func TestCompileRegistryDefinitionsAcceptsSiblingSecret(t *testing.T) {
	mod := registryFixtureModule()
	mod.Secrets = &modules.SecretsDef{Files: []string{`HKCU\Software\Sibling\Token`}}
	if _, failure := compileRegistryDefinitions(mod, fixtureScenario()); failure != nil {
		t.Fatal(failure)
	}
}

func TestCompileRegistryDefinitionsAcceptsUnrelatedHKLMSecretDenyMetadata(t *testing.T) {
	mod := registryFixtureModule()
	mod.Secrets = &modules.SecretsDef{
		Files:        []string{`HKLM\Software\Sibling\LegacyToken`},
		RegistryKeys: []string{`HKLM\Software\Sibling\TypedToken`},
	}
	if _, failure := compileRegistryDefinitions(mod, fixtureScenario()); failure != nil {
		t.Fatal(failure)
	}
}

func TestCompileRegistryDefinitionsClassifiesProductionCatalog(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repoRoot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	acceptedModules, acceptedDefinitions, existingFileCaptures := 0, 0, 0
	rejected := map[string]string{}
	for id, mod := range catalog.Modules {
		if mod.Capture == nil || len(mod.Capture.RegistryKeys) == 0 {
			continue
		}
		var scenario validationmatrix.Scenario
		for _, candidate := range catalog.Records[id].Synthetic.Scenarios {
			if candidate.Mode == validationmatrix.ScenarioConfigRoundtripV1 {
				scenario = candidate
				break
			}
		}
		if scenario.ID == "" {
			continue
		}
		definitions, failure := compileRegistryDefinitions(mod, scenario)
		if failure != nil {
			rejected[id] = failure.Coordinate
			continue
		}
		acceptedModules++
		acceptedDefinitions += len(definitions.Entries)
		existingFileCaptures += len(mod.Capture.Files)
		if _, fileFailure := compileFixtureDefinitionsAt(repoRoot, mod, scenario); fileFailure == nil || fileFailure.Coordinate != "capture.registry" {
			t.Fatalf("registry module %q changed file fixture failure = %+v", id, fileFailure)
		}
	}
	if acceptedModules != 25 || acceptedDefinitions != 32 || existingFileCaptures != 36 {
		t.Fatalf("registry catalog = %d modules, %d definitions, %d file captures; want 25, 32, 36", acceptedModules, acceptedDefinitions, existingFileCaptures)
	}
	wantRejected := map[string]string{
		"apps.ccleaner":         "capture.registryKeys[0].key",
		"apps.displayfusion":    "capture.registryKeys[0].key",
		"apps.revo-uninstaller": "capture.registryKeys[0].key",
		"apps.tableplus":        "capture.registryKeys[0].key",
	}
	wantDetails := map[string]string{
		"apps.ccleaner":         "registry capture contains a declared secret descendant",
		"apps.displayfusion":    "registry capture is contained by a declared secret ancestor",
		"apps.revo-uninstaller": "registry capture contains a declared secret descendant",
		"apps.tableplus":        "registry capture contains a declared secret descendant",
	}
	if len(rejected) != len(wantRejected) {
		t.Fatalf("rejected registry modules = %+v, want %+v", rejected, wantRejected)
	}
	for id, coordinate := range wantRejected {
		if rejected[id] != coordinate {
			t.Fatalf("registry module %q failure = %q, want %q", id, rejected[id], coordinate)
		}
		mod := catalog.Modules[id]
		_, failure := compileRegistryDefinitions(mod, catalog.Records[id].Synthetic.Scenarios[0])
		if failure == nil || failure.Detail != wantDetails[id] {
			t.Fatalf("registry module %q failure = %+v, want %q", id, failure, wantDetails[id])
		}
		captureKey, err := validationmode.NormalizeHKCU(mod.Capture.RegistryKeys[0].Key)
		if err != nil {
			t.Fatal(err)
		}
		hasOverlap := false
		for _, secrets := range [][]string{mod.Secrets.Files, mod.Secrets.RegistryKeys} {
			for _, secret := range secrets {
				kind, normalized := modules.ClassifySecretCoordinate(secret)
				if kind != modules.SecretCoordinateRegistry {
					continue
				}
				secretKey, err := validationmode.NormalizeHKCU(normalized)
				if err == nil && (registryKeyContains(captureKey, secretKey) || registryKeyContains(secretKey, captureKey)) {
					hasOverlap = true
				}
			}
		}
		if !hasOverlap {
			t.Fatalf("registry module %q has no declared secret overlap", id)
		}
	}
}

func registryFixtureModule() *modules.Module {
	return &modules.Module{
		ID: "apps.fixture",
		Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{
			Key: `HKCU\Software\Fixture`, Dest: "apps/fixture/settings.reg", Optional: true,
		}}},
		Restore: []modules.RestoreDef{{
			Type: "registry-import", Source: "./payload/apps/fixture/settings.reg", Target: `HKCU\Software\Fixture`, Optional: true, Backup: true,
		}},
	}
}
