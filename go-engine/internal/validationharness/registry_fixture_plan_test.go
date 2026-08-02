// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestCompositeFixturePlanComposesFileAndRegistryTargetsBehindClosedProductionGate(t *testing.T) {
	mod := mixedRegistryFixtureModule()
	scenario := fixtureScenario()
	if _, failure := compileFixtureDefinitions(mod, scenario); failure == nil || failure.Coordinate != "capture.registry" {
		t.Fatalf("production file compiler failure = %+v", failure)
	}

	fixture := &recordingRegistryFixture{}
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, fixture)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.Targets) != 1 || len(plan.RegistryTargets) != 1 || plan.OperationCount() != 2 {
		t.Fatalf("composite fixture plan = %+v", plan)
	}
	registry := plan.RegistryTargets[0]
	if registry.Authored != `HKCU\Software\Fixture` || registry.Destination != "apps/fixture/settings.reg" || registry.Source != "./payload/apps/fixture/settings.reg" || registry.Strategy != "registry-import" || !registry.Optional {
		t.Fatalf("registry fixture target = %+v", registry)
	}

	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.CompareCaptureSeed(); failure != nil {
		t.Fatal(failure)
	}
	if !plan.HasOptionalTargets() {
		t.Fatal("mixed plan did not report its optional registry target")
	}
	if failure := plan.MaterializeOptionalAbsent(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.Mutate(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.CompareMutated(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.MaterializeRestored(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.CompareRestored(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.Mutate(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.CompareMutated(); failure != nil {
		t.Fatal(failure)
	}
	if got, want := fixture.calls, []string{"replace", "snapshot", "remove", "prove-absent", "replace", "snapshot", "replace", "snapshot", "replace", "snapshot"}; !exactStrings(got, want) {
		t.Fatalf("registry fixture calls = %v, want %v", got, want)
	}
}

func TestCompositeFixturePlanRetainsFileOnlyPlans(t *testing.T) {
	mod := mixedRegistryFixtureModule()
	mod.Capture.RegistryKeys = nil
	mod.Restore = mod.Restore[1:]
	scenario := fixtureScenario()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, nil)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.Targets) != 1 || len(plan.RegistryTargets) != 0 || plan.OperationCount() != 1 {
		t.Fatalf("file-only composite plan = %+v", plan)
	}
}

func TestCompositeFixturePlanBindsCoveredRegistryKeyVerifierIntoEveryState(t *testing.T) {
	mod := registryFixtureModule()
	mod.Verify = []modules.VerifyDef{{Type: "registry-key-exists", Path: `HKCU:\Software\Fixture\Child`}, {Type: "registry-key-exists", Path: `HKCU\Software\Fixture\Verifier\Nested`}, {Type: "registry-key-exists", Path: `HKCU\Software\Other`}}
	scenario := fixtureScenario()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	for _, state := range []validationmode.RegistryState{plan.RegistryTargets[0].Captured, plan.RegistryTargets[0].Mutated, plan.RegistryTargets[0].Restored} {
		paths := map[string]bool{}
		for _, key := range state.Keys() {
			paths[key.Path] = true
		}
		for _, want := range []string{"", "child", "verifier", `verifier\nested`} {
			found := false
			for path := range paths {
				found = found || strings.EqualFold(path, want)
			}
			if !found {
				t.Fatalf("registry verifier state keys = %#v, missing %q", paths, want)
			}
		}
		if paths[`software\other`] {
			t.Fatalf("uncovered verifier leaked into captured subtree: %#v", paths)
		}
	}
}

func TestRegistryArtifactPayloadSetRequiresEveryCompositeTarget(t *testing.T) {
	mod := mixedRegistryFixtureModule()
	scenario := fixtureScenario()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	runtime := &scenarioRuntime{Module: mod, Plan: plan}
	entries := map[string][]byte{}
	for _, target := range plan.Targets {
		name, ok := targetArtifactPayloadName(mod.ID, target)
		if !ok {
			t.Fatal("file fixture payload was not representable")
		}
		entries[strings.ToLower(name)] = []byte(target.Captured)
	}
	if failure := validateArtifactConfigPayloadSet(runtime, entries); failure == nil || failure.Coordinate != "capture.registryKeys[0]" {
		t.Fatalf("registry payload omission failure = %+v", failure)
	}
}

func TestRegistryRebuildEvidenceDoesNotPermitAnOmittedCompositeOperation(t *testing.T) {
	mod := mixedRegistryFixtureModule()
	scenario := fixtureScenario()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	runtime := &scenarioRuntime{Module: mod, Scenario: scenario, Plan: plan, Inventory: validationInventory(mod)}
	items := make([]any, 0, plan.OperationCount())
	for index, target := range plan.RestoreTargets() {
		items = append(items, map[string]any{
			"target": target.Authored, "source": v1RestoreSource(mod.ID, target.Destination), "restoreType": target.Strategy,
			"targetExistedBefore": true, "status": "restored", "backupCreated": true,
			"backupPath": fmt.Sprintf("$ENDSTATE_ROOT/state/backups/registry/item-%d.reg", index),
		})
	}
	payload := map[string]any{
		"apply": map[string]any{
			"summary": map[string]any{"total": 1, "success": 0, "skipped": 1, "failed": 0},
			"actions": []any{map[string]any{"id": runtime.Inventory.AppID, "driver": runtime.Inventory.Driver, "status": "present", "reason": "already_installed"}},
		},
		"configResolutionSummary": map[string]any{"total": 1, "selected": 1, "skipped": 0, "failed": 0},
		"configResolutions":       []any{map[string]any{"status": "restored", "resolution": "legacy_unverified", "reason": nil}},
		"restoreItems":            items,
		"verify":                  json.RawMessage(validVerifyEvidenceData(runtime)),
	}
	payload["apply"].(map[string]any)["configResolutionSummary"] = payload["configResolutionSummary"]
	payload["apply"].(map[string]any)["configResolutions"] = payload["configResolutions"]
	payload["apply"].(map[string]any)["restoreItems"] = payload["restoreItems"]
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateRebuildEvidence(raw, runtime, 0); failure != nil {
		t.Fatalf("composite evidence rejected: %+v", failure)
	}
	payload["restoreItems"] = items[:len(items)-1]
	payload["apply"].(map[string]any)["restoreItems"] = payload["restoreItems"]
	raw, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateRebuildEvidence(raw, runtime, 0); failure == nil || failure.Coordinate != "config" {
		t.Fatalf("omitted registry evidence failure = %+v", failure)
	}
}

func TestRegistryRestoreProjectionUsesAcceptedRestoreSpelling(t *testing.T) {
	mod := registryFixtureModule()
	mod.Capture.RegistryKeys[0].Key = `HKEY_CURRENT_USER\Software\Fixture`
	mod.Restore[0].Target = `HKCU:\Software\Fixture`
	scenario := fixtureScenario()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	registry := plan.RegistryTargets[0]
	projection := plan.RestoreTargets()[0]
	if projection.Authored != registry.Target || projection.Authored == registry.Authored {
		t.Fatalf("registry restore projection = %+v registry=%+v", projection, registry)
	}
	if source, ok := expectedCapturedRestoreSource(plan, mod.ID, mod.Restore[0]); !ok || source != v1RestoreSource(mod.ID, registry.Destination) {
		t.Fatalf("captured registry restore source = %q ok=%t", source, ok)
	}
	runtime := &scenarioRuntime{Module: mod, Plan: plan}
	runtime.Inventory = validationInventory(mod)
	evidence := map[string]any{
		"apply": map[string]any{
			"summary": map[string]any{"total": 1, "success": 0, "skipped": 1, "failed": 0},
			"actions": []any{map[string]any{"id": runtime.Inventory.AppID, "driver": runtime.Inventory.Driver, "status": "present", "reason": "already_installed"}},
		},
		"configResolutionSummary": map[string]any{"total": 1, "selected": 1, "skipped": 0, "failed": 0},
		"configResolutions":       []any{map[string]any{"status": "restored", "resolution": "legacy_unverified", "reason": nil}},
		"restoreItems": []any{map[string]any{
			"target": projection.Authored, "source": v1RestoreSource(mod.ID, projection.Destination), "restoreType": projection.Strategy,
			"targetExistedBefore": true, "status": "restored", "backupCreated": true, "backupPath": "$ENDSTATE_ROOT/state/backups/fixture.reg",
		}},
		"verify": json.RawMessage(validVerifyEvidenceData(runtime)),
	}
	evidence["apply"].(map[string]any)["configResolutionSummary"] = evidence["configResolutionSummary"]
	evidence["apply"].(map[string]any)["configResolutions"] = evidence["configResolutions"]
	evidence["apply"].(map[string]any)["restoreItems"] = evidence["restoreItems"]
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateRebuildEvidence(raw, runtime, 0); failure != nil {
		t.Fatalf("alias registry evidence binding = %+v", failure)
	}
	journal := &restore.Journal{Entries: []restore.JournalEntry{{
		ResolvedSourcePath: v1RestoreSource(mod.ID, projection.Destination), TargetPath: projection.Authored,
		TargetExistedBefore: true, BackupRequested: true, BackupCreated: true, BackupPath: "$ENDSTATE_ROOT/state/backups/fixture.reg",
		Action: "restored", RestoreType: "registry-import",
	}}}
	binding := rebuildEvidenceBinding{
		SourcesByTarget: map[string]string{strings.ToLower(projection.Authored): v1RestoreSource(mod.ID, projection.Destination)},
		BackupsByTarget: map[string]string{strings.ToLower(projection.Authored): "$ENDSTATE_ROOT/state/backups/fixture.reg"},
	}
	if failure := validateJournalEntries(runtime, journal, binding, false); failure != nil {
		t.Fatalf("alias registry journal binding = %+v", failure)
	}
	journal.Entries[0].TargetPath = registry.Authored
	if failure := validateJournalEntries(runtime, journal, binding, false); failure == nil || failure.Coordinate != "journal.entries" {
		t.Fatalf("capture spelling was accepted as a registry restore target: %+v", failure)
	}
}

func TestRegistryBackupRequiresNonemptyScopedREGDocument(t *testing.T) {
	mod := registryFixtureModule()
	scenario := fixtureScenario()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	target := plan.RestoreTargets()[0]
	runtime := &scenarioRuntime{Module: mod, Plan: plan}
	backup := filepath.Join(t.TempDir(), "fixture.reg")
	valid := "Windows Registry Editor Version 5.00\n\n[HKEY_CURRENT_USER\\Software\\Fixture]\n@=\"captured\"\n"
	if err := os.WriteFile(backup, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := validateFixtureBackup(runtime, target, backup); failure != nil {
		t.Fatalf("scoped registry backup rejected: %+v", failure)
	}
	foreign := "Windows Registry Editor Version 5.00\n\n[HKEY_CURRENT_USER\\Software\\Foreign]\n@=\"captured\"\n"
	if err := os.WriteFile(backup, []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := validateFixtureBackup(runtime, target, backup); failure == nil || failure.Coordinate != "capture.registryKeys[0]" {
		t.Fatalf("foreign registry backup failure = %+v", failure)
	}
}

func TestRegistryFixtureSentinelsAndCoordinatesAreForbiddenOutput(t *testing.T) {
	mod := registryFixtureModule()
	scenario := fixtureScenario()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	runtime := &scenarioRuntime{Module: mod, Scenario: scenario, Plan: plan}
	forbidden := runtime.forbiddenOutputValues()
	value := registryFixtureString(plan.RegistryTargets[0].Captured.Keys()[0].Values[0].Data)
	seenValue, seenCoordinate := false, false
	for _, candidate := range forbidden {
		seenValue = seenValue || candidate == value
		seenCoordinate = seenCoordinate || candidate == plan.RegistryTargets[0].Coordinate
	}
	if !seenValue || !seenCoordinate {
		t.Fatalf("forbidden values omit registry state or coordinate: %q %q", value, plan.RegistryTargets[0].Coordinate)
	}
	if _, failure := decodeEnvelope([]byte(`{"debug":`+mustContractJSON(t, value)+`}`), "rebuild", mod.ID, scenario.ID, forbidden...); failure == nil || failure.Code != CodeIsolationFailure {
		t.Fatalf("registry fixture sentinel leaked through output envelope: %+v", failure)
	}
}

func TestCompositeFixturePlanRegistryStatesAreClosureCompleteAndTyped(t *testing.T) {
	mod := registryFixtureModule()
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, fixtureScenario().ID), mod, fixtureScenario(), &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	target := plan.RegistryTargets[0]
	if !registryStateIdentitiesEqual(target.Captured, target.Mutated) || target.Captured.Equal(target.Mutated) || !target.Captured.Equal(target.Restored) {
		t.Fatalf("registry state identity/equality = captured:%+v mutated:%+v restored:%+v", target.Captured.Keys(), target.Mutated.Keys(), target.Restored.Keys())
	}
	captured, mutated := target.Captured.Keys(), target.Mutated.Keys()
	if len(captured) != 2 || captured[0].Path != "" || !strings.EqualFold(captured[1].Path, "child") {
		t.Fatalf("captured closure = %+v", captured)
	}
	if captured[0].Values[0].Name != "" || captured[0].Values[0].Type != validationmode.RegistryTypeString || mutated[0].Values[0].Type != validationmode.RegistryTypeDWORD {
		t.Fatalf("default value type transition = captured:%+v mutated:%+v", captured[0].Values[0], mutated[0].Values[0])
	}
	if captured[1].Values[0].Type != validationmode.RegistryTypeBinary || mutated[1].Values[0].Type != validationmode.RegistryTypeBinary || string(captured[1].Values[0].Data) == string(mutated[1].Values[0].Data) {
		t.Fatalf("binary value transition = captured:%+v mutated:%+v", captured[1].Values[0], mutated[1].Values[0])
	}
}

func TestCompositeFixturePlanRegistryComparisonFailsAtTargetWithoutSentinelLeak(t *testing.T) {
	mod := registryFixtureModule()
	fixture := &recordingRegistryFixture{}
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, fixtureScenario().ID), mod, fixtureScenario(), fixture)
	if failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	state := plan.RegistryTargets[0].Captured.Keys()
	state[0].Values[0].Data = make([]byte, 4)
	binary.LittleEndian.PutUint32(state[0].Values[0].Data, 7)
	state[0].Values[0].Type = validationmode.RegistryTypeDWORD
	wrong, err := validationmode.NewRegistryState(state)
	if err != nil {
		t.Fatal(err)
	}
	fixture.states[plan.RegistryTargets[0].Authored] = wrong
	if failure := plan.CompareCaptured(); failure == nil || failure.Code != CodeContentMismatch || failure.Coordinate != "capture.registryKeys[0]" || strings.Contains(failure.Detail, "endstate-validation-v1:") || strings.Contains(failure.Detail, plan.context.Descriptor().Nonce) {
		t.Fatalf("registry comparison failure = %+v", failure)
	}
}

func TestCompositeFixturePlanRejectsWrongMissingExtraTypeAndDataRegistrySnapshots(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func([]validationmode.RegistryKey) []validationmode.RegistryKey
	}{
		{"wrong", func(keys []validationmode.RegistryKey) []validationmode.RegistryKey {
			keys[1].Path = "Other"
			return keys
		}},
		{"missing", func([]validationmode.RegistryKey) []validationmode.RegistryKey { return nil }},
		{"extra", func(keys []validationmode.RegistryKey) []validationmode.RegistryKey {
			return append(keys, validationmode.RegistryKey{Path: "Extra", Values: []validationmode.RegistryValue{{Name: "value", Type: validationmode.RegistryTypeBinary, Data: []byte{1}}}})
		}},
		{"type", func(keys []validationmode.RegistryKey) []validationmode.RegistryKey {
			keys[0].Values[0].Type = validationmode.RegistryTypeDWORD
			keys[0].Values[0].Data = []byte{1, 0, 0, 0}
			return keys
		}},
		{"data", func(keys []validationmode.RegistryKey) []validationmode.RegistryKey {
			keys[1].Values[0].Data[0] ^= 0xff
			return keys
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mod := registryFixtureModule()
			fixture := &recordingRegistryFixture{}
			plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, fixtureScenario().ID), mod, fixtureScenario(), fixture)
			if failure != nil {
				t.Fatal(failure)
			}
			if failure := plan.MaterializeCaptured(); failure != nil {
				t.Fatal(failure)
			}
			keys := tt.mutate(plan.RegistryTargets[0].Captured.Keys())
			if keys == nil {
				fixture.states[plan.RegistryTargets[0].Authored] = validationmode.RegistryState{}
			} else {
				state, err := validationmode.NewRegistryState(keys)
				if err != nil {
					t.Fatal(err)
				}
				fixture.states[plan.RegistryTargets[0].Authored] = state
			}
			if failure := plan.CompareCaptured(); failure == nil || failure.Code != CodeContentMismatch || failure.Coordinate != "capture.registryKeys[0]" {
				t.Fatalf("registry comparison failure = %+v", failure)
			}
		})
	}
}

func TestCompositeFixturePlanRegistryErrorsAreStableAndRedacted(t *testing.T) {
	mod := registryFixtureModule()
	fixture := &recordingRegistryFixture{err: errors.New("endstate-validation-v1: raw sentinel nonce")}
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, fixtureScenario().ID), mod, fixtureScenario(), fixture)
	if failure != nil {
		t.Fatal(failure)
	}
	failure = plan.MaterializeCaptured()
	if failure == nil || failure.Code != CodeIsolationFailure || failure.Coordinate != "capture.registryKeys[0]" || failure.Detail != "registry fixture operation failed" || strings.Contains(failure.Detail, "endstate-validation-v1:") || strings.Contains(failure.Detail, plan.context.Descriptor().Nonce) {
		t.Fatalf("registry operation failure = %+v", failure)
	}
}

func TestCompositeFixturePlanRegistryTargetsRemainDistinct(t *testing.T) {
	mod := mixedRegistryFixtureModule()
	mod.Capture.RegistryKeys = append(mod.Capture.RegistryKeys, modules.CaptureRegistryKey{Key: `HKCU\Software\Other`, Dest: "apps/fixture/other.reg", Optional: true})
	mod.Restore = append(mod.Restore, modules.RestoreDef{Type: "registry-import", Source: "./payload/apps/fixture/other.reg", Target: `HKCU\Software\Other`, Optional: true, Backup: true})
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, fixtureScenario().ID), mod, fixtureScenario(), &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.RegistryTargets) != 2 || plan.RegistryTargets[0].Authored == plan.RegistryTargets[1].Authored || plan.RegistryTargets[0].Captured.Equal(plan.RegistryTargets[1].Captured) {
		t.Fatalf("registry targets = %+v", plan.RegistryTargets)
	}
	second, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, fixtureScenario().ID), mod, fixtureScenario(), &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	for index := range plan.RegistryTargets {
		if plan.RegistryTargets[index].Authored != second.RegistryTargets[index].Authored || !plan.RegistryTargets[index].Captured.Equal(second.RegistryTargets[index].Captured) || !plan.RegistryTargets[index].Mutated.Equal(second.RegistryTargets[index].Mutated) {
			t.Fatalf("registry target %d is not deterministic: first=%+v second=%+v", index, plan.RegistryTargets[index], second.RegistryTargets[index])
		}
	}
}

func TestCompositeFixturePlanAtPreservesDeclarativeFilesystemContract(t *testing.T) {
	repo := t.TempDir()
	fixturePath := filepath.Join(repo, "fixtures", "mixed.json")
	good := []byte(`{"schemaVersion":1,"entries":[{"coordinate":"capture.files[0]","kind":"directory"}]}`)
	if err := os.MkdirAll(filepath.Dir(fixturePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, good, 0o600); err != nil {
		t.Fatal(err)
	}
	mod := mixedRegistryFixtureModule()
	scenario := fixtureScenario()
	scenario.Fixture = validationmatrix.Fixture{Type: validationmatrix.FixtureDeclarative, Path: "fixtures/mixed.json", SHA256: fixtureSHA256(good)}
	validationRoot := filepath.Join(t.TempDir(), "endstate-validation-reusable-fixture")
	context, restore := reusableFixtureValidationContext(t, validationRoot, mod.ID, scenario.ID)
	defer restore()
	plan, failure := compileCompositeFixturePlanAt(repo, context, mod, scenario, &recordingRegistryFixture{})
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.Targets) != 1 || !plan.Targets[0].Directory {
		t.Fatalf("declarative mixed plan = %+v", plan)
	}

	if err := os.WriteFile(fixturePath, append(good, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, failure := compileCompositeFixturePlanAt(repo, context, mod, scenario, &recordingRegistryFixture{}); failure == nil || failure.Coordinate != "fixture.sha256" {
		t.Fatalf("tampered declarative fixture failure = %+v", failure)
	}
	wrongKind := []byte(`{"schemaVersion":1,"entries":[{"coordinate":"capture.files[0]","kind":"unsupported"}]}`)
	if err := os.WriteFile(fixturePath, wrongKind, 0o600); err != nil {
		t.Fatal(err)
	}
	scenario.Fixture.SHA256 = fixtureSHA256(wrongKind)
	if _, failure := compileCompositeFixturePlanAt(repo, context, mod, scenario, &recordingRegistryFixture{}); failure == nil || failure.Coordinate != "capture.files[0]" {
		t.Fatalf("declarative kind failure = %+v", failure)
	}
}

func TestCompositeFixturePlanAtRejectsOrphanRegistryRestoresAndAllowsFileOnly(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mutate     func(*modules.Module)
		coordinate string
	}{
		{"orphan registry import", func(mod *modules.Module) { mod.Capture.RegistryKeys = nil }, "restore[0]"},
		{"orphan registry set", func(mod *modules.Module) { mod.Capture.RegistryKeys = nil; mod.Restore[0].Type = "registry-set" }, "restore[0]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mod := mixedRegistryFixtureModule()
			tt.mutate(mod)
			scenario := fixtureScenario()
			if _, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, &recordingRegistryFixture{}); failure == nil || failure.Coordinate != tt.coordinate {
				t.Fatalf("orphan registry failure = %+v", failure)
			}
		})
	}
	mod := mixedRegistryFixtureModule()
	mod.Capture.RegistryKeys = nil
	mod.Restore = mod.Restore[1:]
	scenario := fixtureScenario()
	if plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, nil); failure != nil || len(plan.Targets) != 1 || len(plan.RegistryTargets) != 0 {
		t.Fatalf("file-only composite plan = %+v failure=%+v", plan, failure)
	}
}

func TestCompositeFixturePlanAtCompilesSafeRegistryCatalogPlans(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repoRoot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	validationRoot := filepath.Join(t.TempDir(), "endstate-validation-reusable-fixture")
	accepted, registryTargets, fileTargets := 0, 0, 0
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
		context, restore := reusableFixtureValidationContext(t, validationRoot, mod.ID, scenario.ID)
		plan, failure := compileCompositeFixturePlanAt(repoRoot, context, mod, scenario, &recordingRegistryFixture{})
		restore()
		if failure != nil {
			rejected[id] = failure.Coordinate
			continue
		}
		accepted++
		registryTargets += len(plan.RegistryTargets)
		fileTargets += len(plan.Targets)
	}
	if accepted != 25 || registryTargets != 32 || fileTargets != 36 {
		t.Fatalf("registry catalog = %d modules, %d registry targets, %d file targets; want 25, 32, 36", accepted, registryTargets, fileTargets)
	}
	wantRejected := map[string]string{"apps.ccleaner": "capture.registryKeys[0].key", "apps.displayfusion": "capture.registryKeys[0].key", "apps.revo-uninstaller": "capture.registryKeys[0].key", "apps.tableplus": "capture.registryKeys[0].key"}
	if fmt.Sprint(rejected) != fmt.Sprint(wantRejected) {
		t.Fatalf("rejected registry modules = %+v, want %+v", rejected, wantRejected)
	}
}

func TestFixturePlanRejectsEmptyFilePlanAndCompositePlan(t *testing.T) {
	mod := &modules.Module{ID: "apps.fixture", Capture: &modules.CaptureDef{}}
	scenario := fixtureScenario()
	context := fixtureValidationContext(t, mod.ID, scenario.ID)
	if _, failure := compileFixturePlan(context, mod, scenario, fixtureDefinitions{}); failure == nil || failure.Coordinate != "operations" {
		t.Fatalf("empty file plan failure = %+v", failure)
	}
	if _, failure := compileCompositeFixturePlanAt("", context, mod, scenario, nil); failure == nil || failure.Coordinate != "operations" {
		t.Fatalf("empty composite plan failure = %+v", failure)
	}
}

func fixtureSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest)
}

func reusableFixtureValidationContext(t *testing.T, root, moduleID, scenarioID string) (*validationmode.Context, func()) {
	t.Helper()
	nonce := strings.TrimPrefix(filepath.Base(root), "endstate-validation-")
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: scenarioID, Nonce: nonce, ModuleID: moduleID,
		Inventory: validationmode.Inventory{AppID: "vendor-fixture", Driver: "winget", Ref: "Vendor.Fixture", DisplayName: "Fixture", InitialState: "present"},
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	context, err := validationmode.LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	restore, err := context.Activate()
	if err != nil {
		t.Fatal(err)
	}
	return context, func() {
		if err := restore(); err != nil {
			t.Fatal(err)
		}
	}
}

type recordingRegistryFixture struct {
	calls  []string
	states map[string]validationmode.RegistryState
	err    error
}

func (fixture *recordingRegistryFixture) Replace(key string, state validationmode.RegistryState) error {
	fixture.calls = append(fixture.calls, "replace")
	if fixture.err != nil {
		return fixture.err
	}
	if fixture.states == nil {
		fixture.states = map[string]validationmode.RegistryState{}
	}
	fixture.states[key] = state
	return nil
}

func (fixture *recordingRegistryFixture) Snapshot(key string) (validationmode.RegistryState, error) {
	fixture.calls = append(fixture.calls, "snapshot")
	if fixture.err != nil {
		return validationmode.RegistryState{}, fixture.err
	}
	state, exists := fixture.states[key]
	if !exists {
		return validationmode.RegistryState{}, errors.New("missing registry fixture state")
	}
	return state, nil
}

func (fixture *recordingRegistryFixture) Remove(key string) error {
	fixture.calls = append(fixture.calls, "remove")
	if fixture.err != nil {
		return fixture.err
	}
	delete(fixture.states, key)
	return nil
}

func (fixture *recordingRegistryFixture) ProveAbsent(key string) error {
	fixture.calls = append(fixture.calls, "prove-absent")
	if fixture.err != nil {
		return fixture.err
	}
	if _, exists := fixture.states[key]; exists {
		return errors.New("registry fixture remains")
	}
	return nil
}

func registryStateIdentitiesEqual(left, right validationmode.RegistryState) bool {
	leftKeys, rightKeys := left.Keys(), right.Keys()
	if len(leftKeys) != len(rightKeys) {
		return false
	}
	for index := range leftKeys {
		if leftKeys[index].Path != rightKeys[index].Path || len(leftKeys[index].Values) != len(rightKeys[index].Values) {
			return false
		}
		for valueIndex := range leftKeys[index].Values {
			if leftKeys[index].Values[valueIndex].Name != rightKeys[index].Values[valueIndex].Name {
				return false
			}
		}
	}
	return true
}

func mixedRegistryFixtureModule() *modules.Module {
	mod := registryFixtureModule()
	mod.Capture.Files = []modules.CaptureFile{{Source: `%APPDATA%\Fixture\settings.json`, Dest: "apps/fixture/settings.json", Optional: true}}
	mod.Restore = append(mod.Restore, modules.RestoreDef{Type: "copy", Source: "./payload/apps/fixture/settings.json", Target: `%APPDATA%\Fixture\settings.json`, Optional: true, Backup: true})
	return mod
}
