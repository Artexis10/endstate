// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestDecodeLiveEventsAcceptsProductionEmitterTopology(t *testing.T) {
	var stream bytes.Buffer
	apply := events.NewEmitterWithWriter("apply-rebuild", true, &stream)
	apply.EmitPhase("plan")
	apply.EmitItem("notepad-plus-plus", "winget", "to_install", "", "", "")
	apply.EmitSummary("plan", 1, 0, 0, 1)
	apply.EmitPhase("apply")
	apply.EmitItem("notepad-plus-plus", "winget", "installed", "", "", "")
	apply.EmitSummary("apply", 1, 1, 0, 0)
	apply.EmitPhase("restore")
	apply.EmitSummary("restore", 0, 0, 0, 0)
	apply.EmitPhase("verify")
	apply.EmitItem("notepad-plus-plus", "winget", "present", "", "", "")
	apply.EmitSummary("verify", 1, 1, 0, 0)
	verify := events.NewEmitterWithWriter("verify-rebuild", true, &stream)
	verify.EmitPhase("verify")
	verify.EmitItem("notepad-plus-plus", "winget", "present", "", "", "")
	verify.EmitSummary("verify", 1, 1, 0, 0)

	if failure := decodeLiveEvents(stream.Bytes(), "rebuild", "rebuild-envelope"); failure != nil {
		t.Fatalf("decodeLiveEvents() rejected production topology: %+v", failure)
	}
}

func TestDecodeLiveEventsAllowsIndependentEnvelopeAndEmitterRunIDs(t *testing.T) {
	if failure := decodeLiveEvents(liveEvents("verify", "verify-emitter"), "verify", "verify-envelope"); failure != nil {
		t.Fatalf("decodeLiveEvents() rejected independently generated run ID: %+v", failure)
	}
}

func TestDecodeLiveEventsRejectsUnknownTypedMigrationStage(t *testing.T) {
	var stream bytes.Buffer
	emitter := events.NewEmitterWithWriter("capture-events", true, &stream)
	emitter.EmitPhase("capture")
	emitter.EmitConfigMigration(events.ConfigMigrationProgress{CaptureID: "capture", ConfigSetID: "set", Stage: events.ConfigMigrationStaging, Status: events.ConfigProgressStarted})
	emitter.EmitSummary("capture", 0, 0, 0, 0)
	bad := []byte(strings.Replace(string(stream.Bytes()), `"stage":"staging"`, `"stage":"unknown"`, 1))
	if failure := decodeLiveEvents(bad, "capture", "capture-envelope"); failure == nil {
		t.Fatal("decodeLiveEvents accepted an unknown typed migration stage")
	}
}

func TestDecodeLiveEventRejectsNestedConfigResolutionForgery(t *testing.T) {
	valid := []byte(`{"version":1,"runId":"apply-events","timestamp":"2026-07-26T12:00:00Z","event":"config-resolution","captureId":"capture","moduleId":"apps.notepad-plus-plus","configSetId":"set","sourceInstance":{"id":"source","detectorId":"winget","rawVersion":"1","normalizedVersion":"1","evidence":{"type":"package","ref":"Notepad++.Notepad++"}},"targetCandidates":[{"id":"target","moduleId":"apps.notepad-plus-plus","detectorId":"winget","rawVersion":"1","normalizedVersion":"1","evidence":{"type":"package","ref":"Notepad++.Notepad++"},"restoreModuleRevision":"revision"}],"resolution":"direct","reason":null,"migrationPath":[],"label":"","message":"","remediation":null}`)
	if _, ok := decodeLiveEvent(valid); !ok {
		t.Fatal("valid production-form config-resolution rejected")
	}
	for _, mutation := range []struct{ name, old, new string }{
		{"source object", `"detectorId":"winget"`, `"futureAuthorization":true,"detectorId":"winget"`},
		{"target object", `"restoreModuleRevision":"revision"`, `"futureAuthorization":true,"restoreModuleRevision":"revision"`},
		{"nested evidence", `"type":"package","ref"`, `"type":"package","futureAuthorization":true,"ref"`},
		{"invented reason", `"reason":null`, `"reason":"future_authorization"`},
		{"null capture id", `"captureId":"capture"`, `"captureId":null`},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			if _, ok := decodeLiveEvent([]byte(strings.Replace(string(valid), mutation.old, mutation.new, 1))); ok {
				t.Fatal("forged config-resolution accepted")
			}
		})
	}
}

func TestLiveDecoderAcceptsProductionCommandEncoders(t *testing.T) {
	ref := "Notepad++.Notepad++"
	definition := LiveDefinition{ModuleID: "apps.notepad-plus-plus", WingetRef: ref, Comparator: ExactBytesComparator{Mappings: []ComparatorMapping{{Identity: "apps/notepad-plus-plus/config.xml"}, {Identity: "apps/notepad-plus-plus/shortcuts.xml"}}}}
	apply, err := envelope.Marshal(envelope.NewSuccess("apply", "apply-production", "1.0", "0.1.0", commands.ApplyResult{
		Manifest: commands.ApplyManifestRef{Path: "$ENDSTATE_ROOT/manifests/captured.jsonc", Name: "captured", Hash: "sha256:fixture"},
		Summary:  commands.ApplySummary{Total: 1, Success: 1},
		Actions:  []commands.ApplyAction{{ID: "notepad++-notepad++", Ref: &ref, Driver: "winget", Status: "installed", Manual: nil}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateLiveApply(liveCommandOutput{Stdout: apply, Stderr: liveEvents("apply", "apply-production")}, definition, false); failure != nil {
		t.Fatalf("production apply rejected: %+v", failure)
	}

	verify, err := envelope.Marshal(envelope.NewSuccess("verify", "verify-production", "1.0", "0.1.0", commands.VerifyResult{
		Manifest: commands.VerifyManifestRef{Path: "$ENDSTATE_ROOT/manifests/captured.jsonc", Name: "captured"},
		Summary:  commands.VerifySummary{Total: 1, Pass: 1},
		Results:  []commands.VerifyItem{{Type: "command-exists", Status: "pass"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateLiveVerify(liveCommandOutput{Stdout: verify, Stderr: liveEvents("verify", "verify-production")}, definition, 0); failure != nil {
		t.Fatalf("production verify rejected: %+v", failure)
	}

	capture, err := envelope.Marshal(envelope.NewSuccess("capture", "capture-production", "1.0", "0.1.0", commands.CaptureResult{
		AppsIncluded:    []commands.CaptureApp{{Source: "winget", ID: ref, ManifestID: "notepad++-notepad++"}},
		ConfigModules:   []commands.CaptureModuleResult{{DisplayName: "Notepad++", WingetRefs: []string{ref}, ChocolateyRefs: []string{}, AppID: "notepad-plus-plus", ID: definition.ModuleID, Paths: []string{"apps/notepad-plus-plus/config.xml", "apps/notepad-plus-plus/shortcuts.xml"}, FilesCaptured: 2, Status: "captured"}},
		ConfigModuleMap: map[string]string{"notepad-plus-plus": definition.ModuleID}, PackageModuleMap: map[string][]string{ref: {definition.ModuleID}}, OutputPath: "$ENDSTATE_ROOT/manifests/captured.zip", OutputFormat: "zip", ConfigsIncluded: []string{"notepad-plus-plus"}, ConfigsSkipped: []string{}, ConfigsCaptureErrors: []string{}, Sanitized: true, IsExample: false, Counts: commands.CaptureCountsFull{Included: 1, TotalFound: 1}, CaptureWarnings: []string{}, Manifest: commands.CaptureManifest{Name: "captured", Path: "$ENDSTATE_ROOT/manifests/captured.zip"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := liveExpectedMappings(definition.Comparator.Mappings)
	if _, failure := validateLiveCapture(liveCommandOutput{Stdout: capture, Stderr: liveEvents("capture", "capture-production")}, definition, expected); failure != nil {
		t.Fatalf("production capture rejected: %+v", failure)
	}
}

func TestLiveDecoderAcceptsOnlySeededOptionalMappings(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	allowed, ok := liveExpectedMappings(definition.Comparator.Mappings)
	if !ok {
		t.Fatal("production comparator mappings are invalid")
	}
	captured, failure := validateLiveCapture(liveCommandOutput{Stdout: liveTestEnvelope("capture", "capture-seeded", liveCaptureData()), Stderr: liveEvents("capture", "capture-seeded")}, definition, allowed)
	if failure != nil {
		t.Fatalf("seeded optional capture rejected: %+v", failure)
	}
	if len(captured) != 2 {
		t.Fatalf("captured optional mappings = %#v, want only the two seeded files", captured)
	}
}

func TestLiveDecoderAcceptsProductionRebuildEncoder(t *testing.T) {
	definition := LiveDefinition{ModuleID: "apps.notepad-plus-plus", WingetRef: "Notepad++.Notepad++", Comparator: ExactBytesComparator{Mappings: []ComparatorMapping{{Identity: "apps/notepad-plus-plus/config.xml", RestoreTemplate: `%APPDATA%\Notepad++\config.xml`}}}}
	resolution := planner.ConfigResolution{
		CaptureID:        "legacy:apps.notepad-plus-plus",
		ModuleID:         definition.ModuleID,
		ConfigSetID:      "legacy",
		TargetCandidates: []planner.TargetInstance{},
		Resolution:       planner.ResolutionLegacyUnverified,
		MigrationPath:    []string{},
		ResolvedTargets:  []string{},
		Status:           planner.StatusRestored,
	}
	item := restore.RestoreResult{ID: "restore-config", Source: "configs/notepad-plus-plus/config.xml", Target: definition.Comparator.Mappings[0].RestoreTemplate, Status: "restored", BackupCreated: false, TargetExistedBefore: false, RestoreType: "copy"}
	fields := &commands.ConfigResultFields{
		ConfigResolutions:       []planner.ConfigResolution{resolution},
		ConfigResolutionSummary: planner.ConfigResolutionSummary{Total: 1, LegacyUnverified: 1, Selected: 1},
		RestoreItems:            []restore.RestoreResult{item},
	}
	ref := definition.WingetRef
	apply := &commands.ApplyResult{
		Manifest:           commands.ApplyManifestRef{Path: "$ENDSTATE_ROOT/manifests/captured.jsonc", Name: "captured", Hash: "sha256:fixture"},
		Summary:            commands.ApplySummary{Total: 1, Success: 1},
		Actions:            []commands.ApplyAction{{ID: "notepad++-notepad++", Ref: &ref, Driver: "winget", Status: "installed"}},
		ConfigResultFields: fields,
	}
	verify := &commands.VerifyResult{
		Manifest: commands.VerifyManifestRef{Path: "$ENDSTATE_ROOT/manifests/captured.jsonc", Name: "captured"},
		Summary:  commands.VerifySummary{Total: 1, Pass: 1},
		Results:  []commands.VerifyItem{{Type: "command-exists", Status: "pass"}},
	}
	encoded, err := envelope.Marshal(envelope.NewSuccess("rebuild", "rebuild-production", "1.0", "0.1.0", &commands.RebuildResult{
		From:               "$ENDSTATE_ROOT/manifests/captured.zip",
		DryRun:             false,
		Restore:            "enabled",
		Apply:              apply,
		Verify:             verify,
		ConfigResultFields: fields,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateLiveRebuild(liveCommandOutput{Stdout: encoded, Stderr: liveEvents("rebuild", "rebuild-production")}, definition, map[string]struct{}{definition.Comparator.Mappings[0].Identity: {}}, true, false); failure != nil {
		t.Fatalf("production rebuild rejected: %+v", failure)
	}
}

func TestLiveDecoderAcceptsProductionConvergedRebuildEncoder(t *testing.T) {
	definition := LiveDefinition{ModuleID: "apps.notepad-plus-plus", WingetRef: "Notepad++.Notepad++", Comparator: ExactBytesComparator{Mappings: []ComparatorMapping{{Identity: "apps/notepad-plus-plus/config.xml", RestoreTemplate: `%APPDATA%\Notepad++\config.xml`}}}}
	reason := planner.ReasonAlreadyUpToDate
	resolution := planner.ConfigResolution{
		CaptureID:        "legacy:apps.notepad-plus-plus",
		ModuleID:         definition.ModuleID,
		ConfigSetID:      "legacy",
		TargetCandidates: []planner.TargetInstance{},
		Resolution:       planner.ResolutionLegacyUnverified,
		Reason:           &reason,
		MigrationPath:    []string{},
		ResolvedTargets:  []string{},
		Status:           planner.StatusSkipped,
	}
	item := restore.RestoreResult{ID: "restore-config", Source: "configs/notepad-plus-plus/config.xml", Target: definition.Comparator.Mappings[0].RestoreTemplate, Status: "skipped_up_to_date", BackupCreated: false, TargetExistedBefore: true, RestoreType: "copy"}
	fields := &commands.ConfigResultFields{
		ConfigResolutions:       []planner.ConfigResolution{resolution},
		ConfigResolutionSummary: planner.ConfigResolutionSummary{Total: 1, LegacyUnverified: 1, Skipped: 1},
		RestoreItems:            []restore.RestoreResult{item},
	}
	ref := definition.WingetRef
	apply := &commands.ApplyResult{
		Manifest:           commands.ApplyManifestRef{Path: "$ENDSTATE_ROOT/manifests/captured.jsonc", Name: "captured", Hash: "sha256:fixture"},
		Summary:            commands.ApplySummary{Total: 1, Skipped: 1},
		Actions:            []commands.ApplyAction{{ID: "notepad++-notepad++", Ref: &ref, Driver: "winget", Status: "present", Reason: "already_installed"}},
		ConfigResultFields: fields,
	}
	verify := &commands.VerifyResult{
		Manifest: commands.VerifyManifestRef{Path: "$ENDSTATE_ROOT/manifests/captured.jsonc", Name: "captured"},
		Summary:  commands.VerifySummary{Total: 1, Pass: 1},
		Results:  []commands.VerifyItem{{Type: "command-exists", Status: "pass"}},
	}
	encoded, err := envelope.Marshal(envelope.NewSuccess("rebuild", "rebuild-production-converged", "1.0", "0.1.0", &commands.RebuildResult{
		From:               "$ENDSTATE_ROOT/manifests/captured.zip",
		DryRun:             false,
		Restore:            "enabled",
		Apply:              apply,
		Verify:             verify,
		ConfigResultFields: fields,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateLiveRebuild(liveCommandOutput{Stdout: encoded, Stderr: liveEvents("rebuild", "rebuild-production-converged")}, definition, map[string]struct{}{definition.Comparator.Mappings[0].Identity: {}}, false, true); failure != nil {
		t.Fatalf("production converged rebuild rejected: %+v", failure)
	}
}

func TestDecodeLiveJourneyProjectsStrictHostedNotepadJourney(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	inputs := liveJourneyOutputs{
		ScenarioID:         liveConfigRoundtripScenarioID,
		InitialApply:       liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-initial", liveApplyData("installed")), Stderr: liveEvents("apply", "apply-initial")},
		Verify:             liveCommandOutput{Stdout: liveTestEnvelope("verify", "verify-initial", liveVerifyData()), Stderr: liveEvents("verify", "verify-initial")},
		Capture:            liveCommandOutput{Stdout: liveTestEnvelope("capture", "capture-initial", liveCaptureData()), Stderr: liveEvents("capture", "capture-initial")},
		RestoreRebuild:     liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-restore", liveRebuildData("installed", "restored")), Stderr: liveEvents("rebuild", "rebuild-restore")},
		Revert:             liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-config", liveRevertData()), Stderr: liveEvents("revert", "revert-config")},
		RestoreJournalID:   "opaque-journal",
		RecoveryRebuild:    liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-recovery", liveRebuildData("present", "restored")), Stderr: liveEvents("rebuild", "rebuild-recovery")},
		PackageAfterRevert: PackageObservation{Ref: "Notepad++.Notepad++", Version: "8.7.1", Status: "present"},
		ConvergenceRebuild: liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-converged", liveRebuildData("present", "skipped_up_to_date")), Stderr: liveEvents("rebuild", "rebuild-converged")},
	}

	projection, failure := decodeLiveJourney(definition, inputs)
	if failure != nil {
		t.Fatalf("decodeLiveJourney() failure = %+v", failure)
	}
	if projection.ModuleID != definition.ModuleID || projection.Ref != definition.WingetRef || projection.CapturedMappings != 2 || projection.RestoredMappings != 2 || !projection.PackagePresentAfterRevert || !projection.ConvergenceEnvelopeObserved {
		t.Fatalf("projection = %+v", projection)
	}
	encoded := fmt.Sprintf("%+v", projection)
	for _, leaked := range []string{"C:\\", "%APPDATA%", "secret", "config.xml"} {
		if strings.Contains(strings.ToLower(encoded), strings.ToLower(leaked)) {
			t.Fatalf("projection leaked %q: %s", leaked, encoded)
		}
	}
}

func TestDecodeLiveJourneyFailsClosedOnForgedOrIncompleteLiveProof(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	valid := liveJourneyOutputs{
		ScenarioID:         liveConfigRoundtripScenarioID,
		InitialApply:       liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-initial", liveApplyData("installed")), Stderr: liveEvents("apply", "apply-initial")},
		Verify:             liveCommandOutput{Stdout: liveTestEnvelope("verify", "verify-initial", liveVerifyData()), Stderr: liveEvents("verify", "verify-initial")},
		Capture:            liveCommandOutput{Stdout: liveTestEnvelope("capture", "capture-initial", liveCaptureData()), Stderr: liveEvents("capture", "capture-initial")},
		RestoreRebuild:     liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-restore", liveRebuildData("installed", "restored")), Stderr: liveEvents("rebuild", "rebuild-restore")},
		Revert:             liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-config", liveRevertData()), Stderr: liveEvents("revert", "revert-config")},
		RestoreJournalID:   "opaque-journal",
		RecoveryRebuild:    liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-recovery", liveRebuildData("present", "restored")), Stderr: liveEvents("rebuild", "rebuild-recovery")},
		PackageAfterRevert: PackageObservation{Ref: "Notepad++.Notepad++", Status: "present"},
		ConvergenceRebuild: liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-converged", liveRebuildData("present", "skipped_up_to_date")), Stderr: liveEvents("rebuild", "rebuild-converged")},
	}
	cases := []struct {
		name   string
		mutate func(*liveJourneyOutputs)
	}{
		{"test fixture envelope", func(value *liveJourneyOutputs) {
			value.Verify.Stdout = []byte(strings.Replace(string(value.Verify.Stdout), `,"data":`, `,"testMode":{"active":true,"scenarioId":"fixture","moduleId":"apps.fixture"},"data":`, 1))
		}},
		{"wrong scenario", func(value *liveJourneyOutputs) { value.ScenarioID = "default-v1" }},
		{"already present initial install", func(value *liveJourneyOutputs) {
			value.InitialApply.Stdout = []byte(strings.Replace(string(value.InitialApply.Stdout), `"success":1,"skipped":0`, `"success":0,"skipped":1`, 1))
		}},
		{"verify lacks settings", func(value *liveJourneyOutputs) {
			value.Verify.Stdout = liveTestEnvelope("verify", "verify-initial", `{"summary":{"total":1,"pass":1,"fail":0,"skipped":0},"results":[{"type":"app","id":"notepad-plus-plus","ref":"Notepad++.Notepad++","driver":"winget","status":"pass","reason":""}]}`)
		}},
		{"capture has foreign mapping", func(value *liveJourneyOutputs) {
			value.Capture.Stdout = []byte(strings.Replace(string(value.Capture.Stdout), "shortcuts.xml", "foreign.xml", 1))
		}},
		{"nested rebuild contradiction", func(value *liveJourneyOutputs) {
			value.RestoreRebuild.Stdout = []byte(strings.Replace(string(value.RestoreRebuild.Stdout), `"selected":1`, `"selected":0`, 1))
		}},
		{"outer and nested config fields differ", func(value *liveJourneyOutputs) {
			value.RestoreRebuild.Stdout = []byte(strings.Replace(string(value.RestoreRebuild.Stdout), `"resolvedTargets":[]`, `"resolvedTargets":["%APPDATA%\\Notepad++\\config.xml"]`, 1))
		}},
		{"revert uses uncaptured mapping", func(value *liveJourneyOutputs) {
			value.Revert.Stdout = []byte(strings.Replace(string(value.Revert.Stdout), `%APPDATA%\\Notepad++\\shortcuts.xml`, `%APPDATA%\\Notepad++\\langs.xml`, 1))
		}},
		{"package absent after revert", func(value *liveJourneyOutputs) { value.PackageAfterRevert.Status = "absent" }},
		{"converged rebuild mutation", func(value *liveJourneyOutputs) {
			value.ConvergenceRebuild.Stdout = []byte(strings.Replace(string(value.ConvergenceRebuild.Stdout), `"success":0,"skipped":1`, `"success":1,"skipped":0`, 1))
		}},
		{"missing terminal event", func(value *liveJourneyOutputs) {
			value.Capture.Stderr = []byte(`{"version":1,"runId":"capture-initial","timestamp":"2026-07-26T12:00:00Z","event":"phase","phase":"capture"}`)
		}},
		{"outer rebuild run id reused by nested stream", func(value *liveJourneyOutputs) {
			value.RestoreRebuild.Stderr = []byte(strings.Replace(string(value.RestoreRebuild.Stderr), `"runId":"apply-rebuild-restore"`, `"runId":"rebuild-restore"`, 1))
		}},
		{"duplicate JSON key", func(value *liveJourneyOutputs) {
			value.Verify.Stdout = []byte(strings.Replace(string(value.Verify.Stdout), `"runId":"verify-initial"`, `"runId":"verify-initial","runId":"forged"`, 1))
		}},
		{"unexpected data field", func(value *liveJourneyOutputs) {
			value.Capture.Stdout = []byte(strings.Replace(string(value.Capture.Stdout), `"counts":`, `"future":true,"counts":`, 1))
		}},
		{"unexpected nested field", func(value *liveJourneyOutputs) {
			value.Verify.Stdout = []byte(strings.Replace(string(value.Verify.Stdout), `"pass":1`, `"pass":1,"future":true`, 1))
		}},
		{"multiple stdout envelopes", func(value *liveJourneyOutputs) { value.Revert.Stdout = append(value.Revert.Stdout, []byte(` {}`)...) }},
		{"oversized output", func(value *liveJourneyOutputs) {
			value.Revert.Stderr = []byte(strings.Repeat("x", maxLiveDecodeOutputBytes+1))
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if _, failure := decodeLiveJourney(definition, candidate); failure == nil {
				t.Fatal("decodeLiveJourney accepted forged or incomplete proof")
			}
		})
	}
}

func liveTestEnvelope(command, runID, data string) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":"1.0","cliVersion":"0.1.0","command":%q,"runId":%q,"timestampUtc":"2026-07-26T12:00:00Z","success":true,"data":%s,"error":null}`, command, runID, data))
}

func liveEvents(command, runID string) []byte {
	var stream bytes.Buffer
	emit := func(emitter *events.Emitter, phases ...string) {
		for _, phase := range phases {
			emitter.EmitPhase(phase)
			if command == "capture" {
				emitter.EmitProgress("capture", "inventory")
				emitter.EmitItem("Notepad++.Notepad++", "winget", "present", "detected", "", "Notepad++")
				emitter.EmitArtifact("capture", "manifest", "captured.zip")
			}
			emitter.EmitSummary(phase, 1, 1, 0, 0)
		}
	}
	if command == "rebuild" {
		emit(events.NewEmitterWithWriter("apply-"+runID, true, &stream), "plan", "apply", "restore", "verify")
		emit(events.NewEmitterWithWriter("verify-"+runID, true, &stream), "verify")
		return stream.Bytes()
	}
	phaseSets := map[string][]string{"apply": {"plan", "apply", "verify"}, "verify": {"verify"}, "capture": {"capture"}, "revert": {"restore"}}
	emit(events.NewEmitterWithWriter(runID, true, &stream), phaseSets[command]...)
	return stream.Bytes()
}

func liveVerifyData() string {
	return `{"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured"},"summary":{"total":1,"pass":1,"fail":0,"skipped":0},"results":[{"type":"command-exists","status":"pass"}]}`
}

func liveCaptureData() string {
	return `{"appsIncluded":[{"source":"winget","id":"Notepad++.Notepad++","manifestId":"notepad++-notepad++"}],"configModules":[{"displayName":"Notepad++","wingetRefs":["Notepad++.Notepad++"],"chocolateyRefs":[],"appId":"notepad-plus-plus","id":"apps.notepad-plus-plus","paths":["apps/notepad-plus-plus/config.xml","apps/notepad-plus-plus/shortcuts.xml"],"filesCaptured":2,"status":"captured"}],"configModuleMap":{"notepad-plus-plus":"apps.notepad-plus-plus"},"packageModuleMap":{"Notepad++.Notepad++":["apps.notepad-plus-plus"]},"outputPath":"$ENDSTATE_ROOT/manifests/captured.zip","outputFormat":"zip","configsIncluded":["notepad-plus-plus"],"configsSkipped":[],"configsCaptureErrors":[],"sanitized":true,"isExample":false,"counts":{"filteredRuntimes":0,"included":1,"totalFound":1,"sensitiveExcludedCount":0,"filteredStoreApps":0,"skipped":0},"captureWarnings":[],"manifest":{"name":"captured","path":"$ENDSTATE_ROOT/manifests/captured.zip"}}`
}

func productionLiveDecoderDefinition(t *testing.T) LiveDefinition {
	t.Helper()
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func liveRebuildData(packageStatus, restoreStatus string) string {
	success, skipped, reason := 0, 1, "already_installed"
	if packageStatus == "installed" {
		success, skipped, reason = 1, 0, ""
	}
	selected, configSkipped, resolution, resolutionReason, activeTargetExisted := 1, 0, "restored", "null", "false"
	if restoreStatus == "skipped_up_to_date" {
		selected, configSkipped, resolution, resolutionReason, activeTargetExisted = 0, 1, "skipped", `"already_up_to_date"`, "true"
	}
	config := fmt.Sprintf(`"configResolutionSummary":{"total":1,"direct":0,"migrate":0,"incompatible":0,"unknown":0,"legacyUnverified":1,"selected":%d,"skipped":%d,"failed":0},"configResolutions":[{"captureId":"legacy:apps.notepad-plus-plus","moduleId":"apps.notepad-plus-plus","configSetId":"legacy","targetCandidates":[],"resolution":"legacy_unverified","reason":%s,"migrationPath":[],"resolvedTargets":[],"status":%q,"label":"","message":"","remediation":null}],"restoreItems":[{"id":"restore-config","source":"configs/notepad-plus-plus/config.xml","target":"%%APPDATA%%\\Notepad++\\config.xml","status":%q,"backupCreated":false,"targetExistedBefore":%s,"restoreType":"copy"},{"id":"restore-shortcuts","source":"configs/notepad-plus-plus/shortcuts.xml","target":"%%APPDATA%%\\Notepad++\\shortcuts.xml","status":%q,"backupCreated":false,"targetExistedBefore":%s,"restoreType":"copy"},{"id":"restore-langs","source":"configs/notepad-plus-plus/langs.xml","target":"%%APPDATA%%\\Notepad++\\langs.xml","status":"skipped_missing_source","backupCreated":false,"targetExistedBefore":false,"restoreType":"copy"},{"id":"restore-stylers","source":"configs/notepad-plus-plus/stylers.xml","target":"%%APPDATA%%\\Notepad++\\stylers.xml","status":"skipped_missing_source","backupCreated":false,"targetExistedBefore":false,"restoreType":"copy"},{"id":"restore-user-defined-langs","source":"configs/notepad-plus-plus/userDefineLangs","target":"%%APPDATA%%\\Notepad++\\userDefineLangs","status":"skipped_missing_source","backupCreated":false,"targetExistedBefore":false,"restoreType":"copy"},{"id":"restore-context-menu","source":"configs/notepad-plus-plus/contextMenu.xml","target":"%%APPDATA%%\\Notepad++\\contextMenu.xml","status":"skipped_missing_source","backupCreated":false,"targetExistedBefore":false,"restoreType":"copy"}]`, selected, configSkipped, resolutionReason, resolution, restoreStatus, activeTargetExisted, restoreStatus, activeTargetExisted)
	return fmt.Sprintf(`{"from":"$ENDSTATE_ROOT/manifests/captured.zip","dryRun":false,"restore":"enabled","apply":{"dryRun":false,"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured","hash":"sha256:fixture"},"summary":{"total":1,"success":%d,"skipped":%d,"failed":0},"actions":[{"id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","status":%q,"reason":%q,"manual":null}],%s},%s,"verify":%s}`, success, skipped, packageStatus, reason, config, config, liveVerifyData())
}

func liveApplyData(status string) string {
	success, skipped, reason := 0, 1, "already_installed"
	if status == "installed" {
		success, skipped, reason = 1, 0, ""
	}
	return fmt.Sprintf(`{"dryRun":false,"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured","hash":"sha256:fixture"},"summary":{"total":1,"success":%d,"skipped":%d,"failed":0},"actions":[{"id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","status":%q,"reason":%q,"manual":null}]}`, success, skipped, status, reason)
}

func liveRevertData() string {
	return `{"journalUsed":"opaque-journal","results":[{"target":"%APPDATA%\\Notepad++\\shortcuts.xml","action":"deleted"},{"target":"%APPDATA%\\Notepad++\\config.xml","action":"deleted"}]}`
}
