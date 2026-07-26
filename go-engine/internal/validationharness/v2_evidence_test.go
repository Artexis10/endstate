// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestV2DirectResolutionRejectsWrongFingerprintTargetAndDetectorEvidence(t *testing.T) {
	runtime, resolution, _ := v2EvidenceFixture(t)
	if failure := validateV2DirectResolution(resolution, runtime, false); failure != nil {
		t.Fatalf("valid direct resolution: %+v", failure)
	}

	tests := []struct {
		name       string
		mutate     func(*planner.ConfigResolution)
		coordinate string
	}{
		{name: "wrong fingerprint", coordinate: "configResolutions[0]", mutate: func(value *planner.ConfigResolution) {
			value.SourceGenerationFingerprint = strings.Repeat("f", 64)
		}},
		{name: "wrong resolved target", coordinate: "configResolutions[0].resolvedTargets", mutate: func(value *planner.ConfigResolution) {
			value.ResolvedTargets[0] = "$ENDSTATE_ROOT/foreign/settings.ini"
		}},
		{name: "wrong source detector evidence", coordinate: "configResolutions[0].sourceInstance", mutate: func(value *planner.ConfigResolution) {
			value.SourceInstance.Evidence.Type = "path"
		}},
		{name: "missing target candidate", coordinate: "configResolutions[0]", mutate: func(value *planner.ConfigResolution) {
			value.TargetCandidates = nil
		}},
		{name: "direct changed to migrate", coordinate: "configResolutions[0]", mutate: func(value *planner.ConfigResolution) {
			value.Resolution = planner.ResolutionMigrate
		}},
		{name: "direct gained migration path", coordinate: "configResolutions[0]", mutate: func(value *planner.ConfigResolution) {
			value.MigrationPath = []string{"g1", "g2"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneV2Resolution(t, resolution)
			test.mutate(&candidate)
			if failure := validateV2DirectResolution(candidate, runtime, false); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v, want coordinate %q", failure, test.coordinate)
			}
		})
	}
}

func TestV2MigrationResolutionBindsDistinctGenerationsPathAndTarget(t *testing.T) {
	runtime, resolution, _ := v2MigrationEvidenceFixture(t)
	if failure := validateV2MigrationResolution(resolution, runtime, false); failure != nil {
		t.Fatalf("valid migration resolution: %+v", failure)
	}

	tests := []struct {
		name       string
		mutate     func(*planner.ConfigResolution)
		coordinate string
	}{
		{name: "direct instead of migrate", coordinate: "configResolutions[0]", mutate: func(value *planner.ConfigResolution) {
			value.Resolution = planner.ResolutionDirect
		}},
		{name: "wrong target generation", coordinate: "configResolutions[0]", mutate: func(value *planner.ConfigResolution) {
			value.TargetGeneration = "g1"
		}},
		{name: "wrong path", coordinate: "configResolutions[0]", mutate: func(value *planner.ConfigResolution) {
			value.MigrationPath = []string{"g1", "g3"}
		}},
		{name: "wrong target fingerprint", coordinate: "configResolutions[0].targetCandidates", mutate: func(value *planner.ConfigResolution) {
			value.TargetCandidates[0].GenerationFingerprint = strings.Repeat("f", 64)
		}},
		{name: "wrong target version", coordinate: "configResolutions[0].targetCandidates", mutate: func(value *planner.ConfigResolution) {
			value.TargetCandidates[0].RawVersion = "2.4"
		}},
		{name: "source host path reused", coordinate: "configResolutions[0].resolvedTargets", mutate: func(value *planner.ConfigResolution) {
			value.ResolvedTargets[0] = "$ENDSTATE_ROOT/host/localappdata/ownCloud/owncloud.cfg"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneV2Resolution(t, resolution)
			test.mutate(&candidate)
			if failure := validateV2MigrationResolution(candidate, runtime, false); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v, want coordinate %q", failure, test.coordinate)
			}
		})
	}
}

func TestV2RestoreAndRevertEvidenceRejectsWrongBindingFailureAndRepeatDrift(t *testing.T) {
	runtime, resolution, item := v2EvidenceFixture(t)
	if failure := validateV2RestoreItems([]restore.RestoreResult{item}, runtime, resolution, false); failure != nil {
		t.Fatalf("valid restore item: %+v", failure)
	}

	tests := []struct {
		name       string
		items      []restore.RestoreResult
		repeat     bool
		coordinate string
	}{
		{name: "missing target", coordinate: "restoreItems", items: nil},
		{name: "wrong target", coordinate: "restoreItems", items: []restore.RestoreResult{v2MutateRestoreItem(item, func(value *restore.RestoreResult) { value.Target = `%APPDATA%\Foreign\settings.ini` })}},
		{name: "wrong source", coordinate: "restoreItems", items: []restore.RestoreResult{v2MutateRestoreItem(item, func(value *restore.RestoreResult) { value.Source = "foreign.ini" })}},
		{name: "failed nested item", coordinate: "restoreItems", items: []restore.RestoreResult{v2MutateRestoreItem(item, func(value *restore.RestoreResult) { value.Error = "failed" })}},
		{name: "foreign backup subtree", coordinate: "restoreItems", items: []restore.RestoreResult{v2MutateRestoreItem(item, func(value *restore.RestoreResult) {
			value.BackupPath = "$ENDSTATE_ROOT/state/config-restore/v1/transactions/foreign/snapshots/999999/prior"
		})}},
		{name: "repeat created backup", coordinate: "restoreItems", repeat: true, items: []restore.RestoreResult{v2MutateRestoreItem(item, func(value *restore.RestoreResult) { value.Status = "skipped_up_to_date" })}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if failure := validateV2RestoreItems(test.items, runtime, resolution, test.repeat); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v, want coordinate %q", failure, test.coordinate)
			}
		})
	}

	display, err := runtime.validationContext().DisplayPath(runtime.V2Plan.Targets[0].Resolved)
	if err != nil {
		t.Fatal(err)
	}
	validRaw := []byte(`{"journalUsed":"","results":[{"target":` + mustV2JSONString(t, display) + `,"action":"reverted"}]}`)
	validEvents := []map[string]any{
		{"event": "phase", "phase": "restore"},
		{"event": "item", "id": display, "driver": "restore", "status": "installed", "reason": ""},
		{"event": "summary", "success": json.Number("1")},
	}
	if failure := validateV2RevertEvidence(validRaw, validEvents, runtime); failure != nil {
		t.Fatalf("valid revert evidence: %+v", failure)
	}
	failedRaw := []byte(`{"journalUsed":"","results":[{"target":` + mustV2JSONString(t, display) + `,"action":"failed"}]}`)
	if failure := validateV2RevertEvidence(failedRaw, validEvents, runtime); failure == nil || failure.Code != CodeRevertFailure {
		t.Fatalf("failed revert was accepted: %+v", failure)
	}
	foreignRaw := []byte(`{"journalUsed":"","results":[{"target":"%APPDATA%\\Foreign\\settings.ini","action":"reverted"}]}`)
	if failure := validateV2RevertEvidence(foreignRaw, validEvents, runtime); failure == nil || failure.Code != CodeRevertFailure {
		t.Fatalf("foreign revert target was accepted: %+v", failure)
	}
}

func TestV2DirectEventsBindEveryConfigAndRestoreField(t *testing.T) {
	runtime, resolution, item := v2EvidenceFixture(t)
	events := v2DirectEventsFixture(t, runtime, resolution, item)
	if failure := validateV2DirectRebuildEvents(events, runtime, resolution, []restore.RestoreResult{item}, false); failure != nil {
		t.Fatalf("valid direct events: %+v", failure)
	}
	tests := []struct {
		name       string
		index      int
		field      string
		value      any
		coordinate string
	}{
		{name: "migration capture", index: 4, field: "captureId", value: "capture-foreign", coordinate: "config-migration"},
		{name: "migration set", index: 4, field: "configSetId", value: "foreign", coordinate: "config-migration"},
		{name: "migration from generation", index: 4, field: "fromGeneration", value: "g0", coordinate: "config-migration"},
		{name: "failed migration", index: 4, field: "status", value: "failed", coordinate: "config-migration"},
		{name: "migration reason", index: 4, field: "reason", value: "foreign", coordinate: "config-migration"},
		{name: "resolution source instance", index: 8, field: "sourceInstanceId", value: "instance-foreign", coordinate: "config-resolution"},
		{name: "resolution target instance", index: 8, field: "targetInstanceId", value: "instance-foreign", coordinate: "config-resolution"},
		{name: "resolution reason", index: 8, field: "reason", value: "already_up_to_date", coordinate: "config-resolution"},
		{name: "restore module", index: 9, field: "module", value: "apps.foreign", coordinate: "restore-item"},
		{name: "restore restorer", index: 9, field: "restorer", value: "delete", coordinate: "restore-item"},
		{name: "restore source", index: 9, field: "source", value: "foreign.ini", coordinate: "restore-item"},
		{name: "restore target", index: 9, field: "target", value: "$ENDSTATE_ROOT/foreign.ini", coordinate: "restore-item"},
		{name: "restore backup cross binding", index: 14, field: "backupPath", value: "$ENDSTATE_ROOT/state/config-restore/v1/transactions/foreign/snapshots/999999/prior", coordinate: "restore-item"},
		{name: "restore target existed", index: 9, field: "targetExisted", value: false, coordinate: "restore-item"},
		{name: "restore reason", index: 9, field: "reason", value: "foreign", coordinate: "restore-item"},
		{name: "restore capture", index: 9, field: "captureId", value: "capture-foreign", coordinate: "restore-item"},
		{name: "restore set", index: 9, field: "configSetId", value: "foreign", coordinate: "restore-item"},
		{name: "restore source generation", index: 9, field: "sourceGeneration", value: "g0", coordinate: "restore-item"},
		{name: "restore target generation", index: 9, field: "targetGeneration", value: "g2", coordinate: "restore-item"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := make([]map[string]any, len(events))
			for index := range events {
				candidate[index] = cloneV2EventMap(t, events[index])
			}
			candidate[test.index][test.field] = test.value
			if failure := validateV2DirectRebuildEvents(candidate, runtime, resolution, []restore.RestoreResult{item}, false); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("mutated %s accepted: %+v", test.field, failure)
			}
		})
	}
}

func TestV2MigrationEventsBindEdgeAndTargetValidationSequence(t *testing.T) {
	runtime, resolution, item := v2MigrationEvidenceFixture(t)
	events := v2MigrationEventsFixture(t, runtime, resolution, item)
	if failure := validateV2MigrationRebuildEvents(events, runtime, resolution, []restore.RestoreResult{item}, false); failure != nil {
		t.Fatalf("valid migration events: %+v", failure)
	}
	tests := []struct {
		name       string
		index      int
		field      string
		value      any
		coordinate string
	}{
		{name: "edge from", index: 6, field: "fromGeneration", value: "g0", coordinate: "config-migration"},
		{name: "edge target", index: 6, field: "toGeneration", value: "g3", coordinate: "config-migration"},
		{name: "missing edge completion", index: 7, field: "stage", value: "validation", coordinate: "config-migration"},
		{name: "failed edge validation", index: 9, field: "status", value: "failed", coordinate: "config-migration"},
		{name: "failed target validation", index: 11, field: "status", value: "failed", coordinate: "config-migration"},
		{name: "resolution changed direct", index: 12, field: "resolution", value: "direct", coordinate: "config-resolution"},
		{name: "migration path", index: 12, field: "migrationPath", value: []any{"g1"}, coordinate: "config-resolution"},
		{name: "restore target generation", index: 13, field: "targetGeneration", value: "g1", coordinate: "restore-item"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := make([]map[string]any, len(events))
			for index := range events {
				candidate[index] = cloneV2EventMap(t, events[index])
			}
			candidate[test.index][test.field] = test.value
			if failure := validateV2MigrationRebuildEvents(candidate, runtime, resolution, []restore.RestoreResult{item}, false); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("mutated %s accepted: %+v", test.name, failure)
			}
		})
	}
}

func TestV2DirectRestoreAndEmbeddedVerifySummariesAreExact(t *testing.T) {
	runtime, resolution, item := v2EvidenceFixture(t)
	events := v2DirectEventsFixture(t, runtime, resolution, item)
	for _, test := range []struct {
		name       string
		index      int
		field      string
		value      json.Number
		coordinate string
	}{
		{name: "restore total", index: 15, field: "total", value: "2", coordinate: "restore.summary"},
		{name: "restore success", index: 15, field: "success", value: "0", coordinate: "restore.summary"},
		{name: "restore skipped", index: 15, field: "skipped", value: "1", coordinate: "restore.summary"},
		{name: "restore failed", index: 15, field: "failed", value: "1", coordinate: "restore.summary"},
		{name: "pre-restore verify total", index: 2, field: "total", value: "2", coordinate: "verify.summary"},
		{name: "pre-restore verify skipped", index: 2, field: "skipped", value: "1", coordinate: "verify.summary"},
		{name: "post-restore verify failed", index: 18, field: "failed", value: "1", coordinate: "verify.summary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := make([]map[string]any, len(events))
			for index := range events {
				candidate[index] = cloneV2EventMap(t, events[index])
			}
			candidate[test.index][test.field] = test.value
			if failure := validateV2DirectRebuildEvents(candidate, runtime, resolution, []restore.RestoreResult{item}, false); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("mutated %s summary was accepted: %+v", test.name, failure)
			}
		})
	}
}

func TestV2StandaloneVerifySummaryIsExact(t *testing.T) {
	runtime, resolution, item := v2EvidenceFixture(t)
	standalone := v2DirectEventsFixture(t, runtime, resolution, item)[16:]
	for _, field := range []string{"total", "success", "skipped", "failed"} {
		t.Run(field, func(t *testing.T) {
			candidate := make([]map[string]any, len(standalone))
			for index := range standalone {
				candidate[index] = cloneV2EventMap(t, standalone[index])
			}
			candidate[2][field] = json.Number("9")
			if failure := validateV2VerifyEventSegments(candidate, runtime, false); failure == nil || failure.Coordinate != "verify.summary" {
				t.Fatalf("mutated standalone verify %s was accepted: %+v", field, failure)
			}
		})
	}
}

func TestV2RepeatRestoreSummaryCountsEveryTargetAsSkipped(t *testing.T) {
	runtime, resolution, item := v2EvidenceFixture(t)
	events, repeatResolution, repeatItem := v2RepeatEventsFixture(t, runtime, resolution, item)
	if failure := validateV2DirectRebuildEvents(events, runtime, repeatResolution, []restore.RestoreResult{repeatItem}, true); failure != nil {
		t.Fatalf("valid repeat events: %+v", failure)
	}
	for _, field := range []string{"total", "success", "skipped", "failed"} {
		t.Run(field, func(t *testing.T) {
			candidate := make([]map[string]any, len(events))
			for index := range events {
				candidate[index] = cloneV2EventMap(t, events[index])
			}
			candidate[11][field] = json.Number("9")
			if failure := validateV2DirectRebuildEvents(candidate, runtime, repeatResolution, []restore.RestoreResult{repeatItem}, true); failure == nil || failure.Coordinate != "restore.summary" {
				t.Fatalf("mutated repeat %s was accepted: %+v", field, failure)
			}
		})
	}
}

func cloneV2EventMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw := mustV2JSON(t, value)
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var cloned map[string]any
	if err := decoder.Decode(&cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func TestV2CaptureMetadataEnvelopeAndEventsRejectExtraMissingOrFailedEvidence(t *testing.T) {
	runtime, resolution, item := v2EvidenceFixture(t)
	capture := manifest.ConfigCapture{
		CaptureID: runtime.V2Plan.CaptureID, ModuleID: runtime.Module.ID, ConfigSetID: runtime.V2Plan.Compiled.Set.ID,
		SourceGeneration: runtime.V2Plan.Compiled.Generation.ID, SourceGenerationFingerprint: runtime.V2Plan.Compiled.Generation.Fingerprint,
		CaptureModule:   manifest.CaptureModuleProvenance{ContentHash: runtime.Module.Revision},
		PayloadManifest: []manifest.PayloadManifestEntry{{RelativePath: "settings.ini", Size: 1, SHA256: strings.Repeat("c", 64)}},
	}
	metadata := bundle.BundleMetadata{
		SchemaVersion: "2.0", ManifestVersion: 2, CapturedAt: time.Now().UTC().Format(time.RFC3339), OS: "windows",
		ConfigCapturesIncluded: []string{capture.CaptureID}, ConfigModulesIncluded: []string{}, ConfigModulesSkipped: []string{},
	}
	metadataRaw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateV2Metadata(metadataRaw, capture.CaptureID); failure != nil {
		t.Fatalf("valid metadata: %+v", failure)
	}
	for _, mutate := range []func(*bundle.BundleMetadata){
		func(value *bundle.BundleMetadata) { value.ConfigCapturesIncluded = nil },
		func(value *bundle.BundleMetadata) {
			value.ConfigCapturesIncluded = append(value.ConfigCapturesIncluded, "capture-foreign")
		},
		func(value *bundle.BundleMetadata) { value.ConfigModulesIncluded = []string{"apps.foreign"} },
	} {
		candidate := metadata
		candidate.ConfigCapturesIncluded = append([]string(nil), metadata.ConfigCapturesIncluded...)
		mutate(&candidate)
		raw, _ := json.Marshal(candidate)
		if failure := validateV2Metadata(raw, capture.CaptureID); failure == nil || failure.Code != CodeArtifactContract {
			t.Fatalf("invalid metadata was accepted: %+v", candidate)
		}
	}

	validEnvelope := v2CaptureEnvelopeFixture(runtime, capture)
	if failure := validateV2CaptureEnvelope(mustV2JSON(t, validEnvelope), runtime, capture); failure != nil {
		t.Fatalf("valid capture envelope: %+v", failure)
	}
	configCapture := validEnvelope["configCapture"].(map[string]any)
	rows := configCapture["configSets"].([]any)
	for name, mutate := range map[string]func(map[string]any){
		"zero config captures": func(value map[string]any) { value["configSets"] = []any{} },
		"extra config capture": func(value map[string]any) { value["configSets"] = append(append([]any(nil), rows...), rows[0]) },
		"wrong provenance": func(value map[string]any) {
			clonedRows := cloneV2MapSlice(t, rows)
			clonedRows[0].(map[string]any)["captureModuleRevision"] = strings.Repeat("d", 64)
			value["configSets"] = clonedRows
		},
		"wrong fingerprint": func(value map[string]any) {
			clonedRows := cloneV2MapSlice(t, rows)
			clonedRows[0].(map[string]any)["sourceGenerationFingerprint"] = strings.Repeat("e", 64)
			value["configSets"] = clonedRows
		},
		"nested failed capture": func(value map[string]any) {
			clonedRows := cloneV2MapSlice(t, rows)
			clonedRows[0].(map[string]any)["status"] = "failed"
			value["configSets"] = clonedRows
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneV2Map(t, validEnvelope)
			mutate(candidate["configCapture"].(map[string]any))
			if failure := validateV2CaptureEnvelope(mustV2JSON(t, candidate), runtime, capture); failure == nil {
				t.Fatal("invalid capture envelope was accepted")
			}
		})
	}

	nestedFailure := map[string]any{"apply": map[string]any{
		"summary": map[string]any{"total": 1, "success": 0, "skipped": 0, "failed": 1},
		"actions": []any{map[string]any{}},
	}}
	if _, failure := validateV2DirectRebuildEvidence(mustV2JSON(t, nestedFailure), nil, runtime, 0); failure == nil || failure.Code != CodeEnvelopeContract {
		t.Fatalf("outer-success/nested-failure evidence was accepted: %+v", failure)
	}
	if failure := validateV2DirectRebuildEvents(nil, runtime, resolution, []restore.RestoreResult{item}, false); failure == nil || failure.Code != CodeEventContract {
		t.Fatalf("missing config event stream was accepted: %+v", failure)
	}
}

func v2EvidenceFixture(t *testing.T) (*scenarioRuntime, planner.ConfigResolution, restore.RestoreResult) {
	t.Helper()
	context := fixtureValidationContext(t, "apps.fixture", "generation-g1")
	appData, ok := context.VirtualRoot("APPDATA")
	if !ok {
		t.Fatal("APPDATA validation root is absent")
	}
	resolved := filepath.Join(appData, "Vendor", "settings.ini")
	instance := modules.ConfigInstance{
		ID: "instance-fixture", ModuleID: "apps.fixture", DetectorID: "package", Version: modules.NewVersionEvidence("1.2"),
		Evidence: modules.InstanceEvidence{Type: "package", AppID: "vendor-fixture", Backend: "validation", Platform: "windows", Ref: "Vendor.Fixture", Driver: "validation"},
	}
	plan := &V2FixturePlan{
		context: context, CaptureID: "capture-fixture", Instance: instance,
		Compiled:       v2CompiledFixture{Set: modules.ConfigSetDef{ID: "preferences"}, Generation: modules.GenerationDef{ID: "g1", Fingerprint: strings.Repeat("b", 64)}},
		CaptureTargets: []V2FixtureTarget{{Coordinate: "capture.files[0]", Authored: `%APPDATA%\Vendor\settings.ini`, Destination: "settings.ini", Resolved: resolved}},
		Targets:        []V2FixtureTarget{{Coordinate: "capture.files[0]", Authored: `%APPDATA%\Vendor\settings.ini`, Destination: "settings.ini", Resolved: resolved}},
	}
	runtime := &scenarioRuntime{
		Module: &modules.Module{ID: "apps.fixture", Revision: strings.Repeat("a", 64)}, Root: context.Root(), V2Plan: plan,
		Inventory: validationmode.Inventory{AppID: "vendor-fixture", Driver: "validation", Ref: "Vendor.Fixture", DisplayName: "Fixture", Version: "1.2", InitialState: "present"},
	}
	evidence := planner.InstanceEvidence{Type: "package", AppID: "vendor-fixture", Backend: "validation", Platform: "windows", Ref: "Vendor.Fixture", Driver: "validation"}
	source := &planner.SourceInstance{ID: instance.ID, DetectorID: instance.DetectorID, RawVersion: "1.2", NormalizedVersion: "1.2", Evidence: evidence}
	resolution := planner.ConfigResolution{
		CaptureID: plan.CaptureID, ModuleID: runtime.Module.ID, ConfigSetID: plan.Compiled.Set.ID,
		SourceInstance: source, SourceInstanceID: instance.ID, TargetInstanceID: instance.ID,
		SourceGeneration: "g1", SourceGenerationFingerprint: plan.Compiled.Generation.Fingerprint, TargetGeneration: "g1",
		Resolution: planner.ResolutionDirect, Status: planner.StatusRestored, MigrationPath: []string{},
		CaptureModuleRevision: runtime.Module.Revision, RestoreModuleRevision: runtime.Module.Revision,
		TargetCandidates: []planner.TargetInstance{{
			ID: instance.ID, ModuleID: runtime.Module.ID, DetectorID: instance.DetectorID, RawVersion: "1.2", NormalizedVersion: "1.2",
			Evidence: evidence, Generation: "g1", GenerationFingerprint: plan.Compiled.Generation.Fingerprint, ModuleRevision: runtime.Module.Revision,
		}},
		ResolvedTargets: []string{"$ENDSTATE_ROOT/" + filepath.ToSlash(mustV2Relative(t, context.Root(), resolved))},
	}
	display, err := context.DisplayPath(resolved)
	if err != nil {
		t.Fatal(err)
	}
	item := restore.RestoreResult{
		ID: "config:capture-fixture:000000", Source: "settings.ini", Target: display, Status: "restored",
		BackupPath: "$ENDSTATE_ROOT/state/config-restore/v1/transactions/0123456789abcdef0123456789abcdef/snapshots/000000/prior", BackupCreated: true, TargetExistedBefore: true,
		RestoreType: "copy", CaptureID: plan.CaptureID, ConfigSetID: plan.Compiled.Set.ID, TargetInstanceID: instance.ID,
		SourceGeneration: "g1", TargetGeneration: "g1",
	}
	return runtime, resolution, item
}

func v2MigrationEvidenceFixture(t *testing.T) (*scenarioRuntime, planner.ConfigResolution, restore.RestoreResult) {
	t.Helper()
	runtime, resolution, item := v2EvidenceFixture(t)
	plan := runtime.V2Plan
	plan.Compiled.TargetGeneration = modules.GenerationDef{ID: "g2", Fingerprint: strings.Repeat("c", 64)}
	plan.Compiled.Migration = &modules.MigrationEdgeDef{From: "g1", To: "g2"}
	plan.TargetInstance = plan.Instance
	plan.TargetInstance.Version = modules.NewVersionEvidence("2.5")
	plan.Instance.Version = modules.NewVersionEvidence("2.4")
	runtime.Inventory.Version = "2.4"
	appData, ok := plan.context.VirtualRoot("APPDATA")
	if !ok {
		t.Fatal("APPDATA validation root is absent")
	}
	plan.Targets[0].Resolved = filepath.Join(appData, "ownCloud", "owncloud.cfg")
	plan.Targets[0].Authored = `%APPDATA%\ownCloud\owncloud.cfg`
	plan.Targets[0].Destination = "ownCloud/owncloud.cfg"

	resolution.SourceInstance.RawVersion = "2.4"
	resolution.SourceInstance.NormalizedVersion = "2.4"
	resolution.TargetGeneration = "g2"
	resolution.Resolution = planner.ResolutionMigrate
	resolution.MigrationPath = []string{"g1", "g2"}
	resolution.TargetCandidates[0].RawVersion = "2.5"
	resolution.TargetCandidates[0].NormalizedVersion = "2.5"
	resolution.TargetCandidates[0].Generation = "g2"
	resolution.TargetCandidates[0].GenerationFingerprint = plan.Compiled.TargetGeneration.Fingerprint
	resolution.ResolvedTargets = []string{"$ENDSTATE_ROOT/" + filepath.ToSlash(mustV2Relative(t, plan.context.Root(), plan.Targets[0].Resolved))}
	display, err := plan.context.DisplayPath(plan.Targets[0].Resolved)
	if err != nil {
		t.Fatal(err)
	}
	item.Target = display
	item.Source = plan.Targets[0].Destination
	item.SourceGeneration = "g1"
	item.TargetGeneration = "g2"
	return runtime, resolution, item
}

func cloneV2Resolution(t *testing.T, value planner.ConfigResolution) planner.ConfigResolution {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var cloned planner.ConfigResolution
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func v2MutateRestoreItem(value restore.RestoreResult, mutate func(*restore.RestoreResult)) restore.RestoreResult {
	mutate(&value)
	return value
}

func mustV2JSONString(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func mustV2Relative(t *testing.T, root, path string) string {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return relative
}

func v2CaptureEnvelopeFixture(runtime *scenarioRuntime, capture manifest.ConfigCapture) map[string]any {
	return map[string]any{
		"outputFormat": "zip", "bundleSchemaVersion": "2.0", "manifestVersion": 2,
		"configCapture": map[string]any{
			"configSets": []any{map[string]any{
				"captureId": capture.CaptureID, "moduleId": runtime.Module.ID, "configSetId": runtime.V2Plan.Compiled.Set.ID,
				"sourceGeneration": runtime.V2Plan.Compiled.Generation.ID, "sourceGenerationFingerprint": runtime.V2Plan.Compiled.Generation.Fingerprint,
				"captureModuleRevision": runtime.Module.Revision, "filesCaptured": len(capture.PayloadManifest), "status": "captured", "reason": nil,
			}},
			"counts": map[string]any{"total": 1, "captured": 1, "skipped": 0, "failed": 0}, "diagnostics": []any{},
		},
	}
}

func mustV2JSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func cloneV2Map(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw := mustV2JSON(t, value)
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func cloneV2MapSlice(t *testing.T, value []any) []any {
	t.Helper()
	raw := mustV2JSON(t, value)
	var cloned []any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func v2DirectEventsFixture(t *testing.T, runtime *scenarioRuntime, resolution planner.ConfigResolution, item restore.RestoreResult) []map[string]any {
	t.Helper()
	verify := func() []map[string]any {
		return []map[string]any{
			{"event": "phase", "phase": "verify"},
			{"event": "item", "id": runtime.Inventory.Ref, "driver": runtime.Inventory.Driver},
			{"event": "summary", "phase": "verify", "total": json.Number("1"), "success": json.Number("1"), "skipped": json.Number("0"), "failed": json.Number("0")},
		}
	}
	migration := func(stage, status, message string) map[string]any {
		return map[string]any{
			"event": "config-migration", "captureId": resolution.CaptureID, "configSetId": resolution.ConfigSetID,
			"stage": stage, "status": status,
			"reason": nil, "message": message, "remediation": nil,
		}
	}
	rawResolution := mustV2JSON(t, resolution)
	var resolutionEvent map[string]any
	if err := json.Unmarshal(rawResolution, &resolutionEvent); err != nil {
		t.Fatal(err)
	}
	delete(resolutionEvent, "resolvedTargets")
	delete(resolutionEvent, "status")
	resolutionEvent["event"] = "config-resolution"
	start := map[string]any{
		"event": "restore-item", "id": item.ID, "module": runtime.Module.ID, "restorer": item.RestoreType,
		"source": item.Source, "target": item.Target, "status": "restoring", "reason": nil, "backupPath": nil,
		"targetExisted": item.TargetExistedBefore, "message": "restoring settings", "captureId": item.CaptureID,
		"configSetId": item.ConfigSetID, "targetInstanceId": item.TargetInstanceID,
		"sourceGeneration": item.SourceGeneration, "targetGeneration": item.TargetGeneration,
	}
	completed := cloneV2Map(t, start)
	completed["status"] = "restored"
	completed["backupPath"] = item.BackupPath
	completed["message"] = "settings restored"
	events := verify()
	events = append(events,
		map[string]any{"event": "phase", "phase": "restore"},
		migration("staging", "started", "staging settings payload"),
		migration("staging", "completed", "settings payload staged"),
		migration("validation", "started", "validating staged settings"),
		migration("validation", "completed", "staged settings validated"),
		resolutionEvent, start,
		migration("commit", "started", "committing settings"),
		migration("commit", "completed", "settings committed"),
		migration("validation", "started", "validating restored settings"),
		migration("validation", "completed", "restored settings validated"),
		completed,
		map[string]any{"event": "summary", "phase": "restore", "total": json.Number("1"), "success": json.Number("1"), "skipped": json.Number("0"), "failed": json.Number("0")},
	)
	return append(events, verify()...)
}

func v2MigrationEventsFixture(t *testing.T, runtime *scenarioRuntime, resolution planner.ConfigResolution, item restore.RestoreResult) []map[string]any {
	t.Helper()
	direct := v2DirectEventsFixture(t, runtime, resolution, item)
	migration := func(stage, status, message string, edge bool) map[string]any {
		event := map[string]any{
			"event": "config-migration", "captureId": resolution.CaptureID, "configSetId": resolution.ConfigSetID,
			"stage": stage, "status": status, "reason": nil, "message": message, "remediation": nil,
		}
		if edge {
			event["fromGeneration"] = "g1"
			event["toGeneration"] = "g2"
		}
		return event
	}
	segment := []map[string]any{
		direct[3],
		migration("staging", "started", "staging settings payload", false),
		migration("staging", "completed", "settings payload staged", false),
		migration("edge", "started", "applying migration edge", true),
		migration("edge", "completed", "migration edge validated", true),
		migration("validation", "started", "validating staged settings", false),
		migration("validation", "completed", "staged settings validated", false),
		migration("validation", "started", "validating staged settings", false),
		migration("validation", "completed", "staged settings validated", false),
		direct[8], direct[9], direct[10], direct[11], direct[12], direct[13], direct[14], direct[15],
	}
	return append(append(append([]map[string]any{}, direct[:3]...), segment...), direct[16:]...)
}

func v2RepeatEventsFixture(t *testing.T, runtime *scenarioRuntime, resolution planner.ConfigResolution, item restore.RestoreResult) ([]map[string]any, planner.ConfigResolution, restore.RestoreResult) {
	t.Helper()
	initial := v2DirectEventsFixture(t, runtime, resolution, item)
	reason := planner.ReasonAlreadyUpToDate
	resolution.Status = planner.StatusSkipped
	resolution.Reason = &reason
	resolution.Message = "The target already has the desired settings."
	resolution.Remediation = nil
	rawResolution := mustV2JSON(t, resolution)
	var resolutionEvent map[string]any
	if err := json.Unmarshal(rawResolution, &resolutionEvent); err != nil {
		t.Fatal(err)
	}
	delete(resolutionEvent, "resolvedTargets")
	delete(resolutionEvent, "status")
	resolutionEvent["event"] = "config-resolution"
	item.Status = "skipped_up_to_date"
	item.BackupCreated = false
	item.BackupPath = ""
	skipped := cloneV2Map(t, initial[14])
	skipped["status"] = "skipped_up_to_date"
	skipped["reason"] = "already_up_to_date"
	skipped["backupPath"] = nil
	skipped["message"] = "target settings are already current"
	events := []map[string]any{{"event": "phase", "phase": "plan"}}
	events = append(events, initial[:3]...)
	events = append(events, initial[3:8]...)
	events = append(events, resolutionEvent, skipped,
		map[string]any{"event": "summary", "phase": "restore", "total": json.Number("1"), "success": json.Number("0"), "skipped": json.Number("1"), "failed": json.Number("0")})
	events = append(events, initial[16:]...)
	return events, resolution, item
}
