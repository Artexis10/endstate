// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestLiveJourneyProjectionIsSanitizedAndProofFree(t *testing.T) {
	typeOfProjection := reflect.TypeOf(liveJourneyProjection{})
	for index := 0; index < typeOfProjection.NumField(); index++ {
		name := strings.ToLower(typeOfProjection.Field(index).Name)
		for _, forbidden := range []string{"proof", "path", "journal", "output"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("liveJourneyProjection exposes %q", typeOfProjection.Field(index).Name)
			}
		}
	}
	encoded, err := json.Marshal(liveJourneyProjection{ModuleID: "apps.notepad-plus-plus", Ref: "Notepad++.Notepad++"})
	if err != nil || strings.Contains(strings.ToLower(string(encoded)), "journal") || strings.Contains(strings.ToLower(string(encoded)), "proof") {
		t.Fatalf("sanitized projection JSON = %q, %v", encoded, err)
	}
}

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
	verify.EmitSummary("verify", 2, 2, 0, 0)

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

func TestDecodeLiveEventProjectsOnlyTypedCaptureArtifact(t *testing.T) {
	raw := []byte(`{"version":1,"runId":"capture","timestamp":"2026-07-26T12:00:00Z","event":"artifact","phase":"capture","kind":"manifest","path":"C:\\attempt\\capture.zip"}`)
	record, ok := decodeLiveEvent(raw)
	if !ok || record.artifact == nil || record.artifact.Phase != "capture" || record.artifact.Kind != "manifest" || record.artifact.Path != `C:\attempt\capture.zip` {
		t.Fatalf("decodeLiveEvent() = %#v, %v; want official capture artifact projection", record, ok)
	}
	for _, mutation := range [][2]string{{`"phase":"capture"`, `"phase":"rebuild"`}, {`"kind":"manifest"`, `"kind":"bundle"`}} {
		candidate := bytes.Replace(raw, []byte(mutation[0]), []byte(mutation[1]), 1)
		record, ok := decodeLiveEvent(candidate)
		if ok && record.artifact != nil {
			t.Fatal("decodeLiveEvent() projected a non-capture artifact")
		}
	}
}

func TestProjectLiveCaptureClaimsCrossBindsOfficialCaptureEvidence(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	definition.Comparator.Mappings = []ComparatorMapping{{Identity: "apps/notepad-plus-plus/config.xml"}, {Identity: "apps/notepad-plus-plus/shortcuts.xml"}}
	catalog, err := validationmatrix.LoadCatalog(productionLiveRepoRoot(t), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	module := catalog.Modules[definition.ModuleID]
	path := `C:\attempt\capture.zip`
	data := strings.ReplaceAll(liveCaptureData(), "$ENDSTATE_ROOT/manifests/captured.zip", strings.ReplaceAll(path, `\`, `\\`))
	stderr := bytes.ReplaceAll(liveEvents("capture", "capture-run"), []byte("captured.zip"), []byte(strings.ReplaceAll(path, `\`, `\\`)))
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationEngineCapture, 1, liveReceiptTestNonce(72))
	if err != nil {
		t.Fatal(err)
	}
	receipt := liveUnsealedReceiptForTest(t, admission, liveTestEnvelope("capture", "capture-run", data), stderr, "")
	receipt.args = []string{"capture", "apps.fixture", "--out", path}
	receipt.requestSHA256 = receipt.requestDigest()
	liveTestCommitReceipt(t, admission, receipt)
	if err := issuer.sealFn(receipt); err != nil {
		t.Fatal(err)
	}
	claims, failure := projectLiveCaptureClaims(issuer, definition, module, receipt, 1, admission.nonce, "runner", "windows")
	if failure != nil || claims.OutputPath != path || claims.EventPath != path || claims.Receipt.Path != path || claims.ModuleRevision != module.Revision || claims.ReceiptCreated != receipt.created || claims.ReceiptFinished != receipt.finished {
		t.Fatalf("projectLiveCaptureClaims() = %#v, %+v", claims, failure)
	}
	if _, failure := projectLiveCaptureClaims(issuer, definition, module, receipt, 1, admission.nonce, "runner", "windows"); failure == nil {
		t.Fatal("projectLiveCaptureClaims() accepted replayed receipt projection")
	}
	if !issuer.consumeBatchFn([]liveReceiptExpectation{{receipt: receipt, operation: liveOperationEngineCapture, sequence: 1, nonce: admission.nonce}}) {
		t.Fatal("capture projection consumed final receipt")
	}
}

func TestProjectLiveCaptureClaimsBoundsSecondPrecisionTimestampToReceiptSecond(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	definition.Comparator.Mappings = []ComparatorMapping{{Identity: "apps/notepad-plus-plus/config.xml"}, {Identity: "apps/notepad-plus-plus/shortcuts.xml"}}
	catalog, err := validationmatrix.LoadCatalog(productionLiveRepoRoot(t), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	module := catalog.Modules[definition.ModuleID]
	path := `C:\attempt\capture.zip`
	data := strings.ReplaceAll(liveCaptureData(), "$ENDSTATE_ROOT/manifests/captured.zip", strings.ReplaceAll(path, `\`, `\\`))
	stderr := bytes.ReplaceAll(liveEvents("capture", "capture-run"), []byte("captured.zip"), []byte(strings.ReplaceAll(path, `\`, `\\`)))
	for _, test := range []struct {
		name      string
		timestamp string
		wantFail  bool
	}{
		{name: "containing receipt second", timestamp: "2026-07-26T12:00:00Z"},
		{name: "previous second", timestamp: "2026-07-26T11:59:59Z", wantFail: true},
		{name: "next second", timestamp: "2026-07-26T12:00:01Z", wantFail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			issuer := newLiveReceiptIssuer()
			admission, err := issuer.admit(liveOperationEngineCapture, 1, liveReceiptTestNonce(73))
			if err != nil {
				t.Fatal(err)
			}
			stdout := bytes.Replace(liveTestEnvelope("capture", "capture-run", data), []byte(`"timestampUtc":"2026-07-26T12:00:00Z"`), []byte(`"timestampUtc":"`+test.timestamp+`"`), 1)
			receipt := liveUnsealedReceiptForTest(t, admission, stdout, stderr, "")
			receipt.created = time.Date(2026, 7, 26, 12, 0, 0, 100_000_000, time.UTC)
			receipt.started = receipt.created
			receipt.finished = time.Date(2026, 7, 26, 12, 0, 0, 900_000_000, time.UTC)
			receipt.args = []string{"capture", "apps.fixture", "--out", path}
			receipt.requestSHA256 = receipt.requestDigest()
			receipt.resultSHA256 = receipt.resultDigest()
			liveTestCommitReceipt(t, admission, receipt)
			if err := issuer.sealFn(receipt); err != nil {
				t.Fatal(err)
			}
			_, failure := projectLiveCaptureClaims(issuer, definition, module, receipt, 1, admission.nonce, "runner", "windows")
			if (failure != nil) != test.wantFail {
				t.Fatalf("projectLiveCaptureClaims() failure = %+v, wantFail %v", failure, test.wantFail)
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
		Results: []commands.VerifyItem{
			{Type: "app", ID: "notepad++-notepad++", Ref: ref, Driver: "winget", Name: "Notepad++", Status: "pass", Version: "8.7.1"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateLiveVerify(liveCommandOutput{Stdout: verify, Stderr: liveEvents("verify", "verify-production")}, definition, 0); failure != nil {
		t.Fatalf("production verify rejected: %+v", failure)
	}

	capture, err := envelope.Marshal(envelope.NewSuccess("capture", "capture-production", "1.0", "0.1.0", commands.CaptureResult{
		AppsIncluded:    []commands.CaptureApp{{Source: "winget", ID: ref, ManifestID: "notepad++-notepad++"}},
		ConfigModules:   []commands.CaptureModuleResult{{DisplayName: "Notepad++", WingetRefs: []string{ref}, ChocolateyRefs: []string{}, AppID: "notepad-plus-plus", ID: definition.ModuleID, Paths: []string{"configs/notepad-plus-plus/config.xml", "configs/notepad-plus-plus/shortcuts.xml"}, FilesCaptured: 2, Status: "captured"}},
		ConfigModuleMap: map[string]string{"notepad-plus-plus": definition.ModuleID}, PackageModuleMap: map[string][]string{ref: {definition.ModuleID}}, OutputPath: "$ENDSTATE_ROOT/manifests/captured.zip", OutputFormat: "zip", ConfigsIncluded: []string{"notepad-plus-plus"}, ConfigsSkipped: []string{}, ConfigsCaptureErrors: []string{}, Sanitized: true, IsExample: false, Counts: commands.CaptureCountsFull{Included: 1, TotalFound: 1}, CaptureWarnings: []string{}, Manifest: commands.CaptureManifest{Name: "captured", Path: "$ENDSTATE_ROOT/manifests/captured.zip"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := liveExpectedMappings(definition.Comparator.Mappings)
	captured, failure := validateLiveCapture(liveCommandOutput{Stdout: capture, Stderr: liveEvents("capture", "capture-production")}, definition, expected)
	if failure != nil {
		t.Fatalf("production capture rejected: %+v", failure)
	}
	if !liveSameStringSet(captured, expected) {
		t.Fatalf("captured production mappings = %#v, want comparator identities %#v", captured, expected)
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
	for _, identity := range []string{"apps/notepad-plus-plus/config.xml", "apps/notepad-plus-plus/shortcuts.xml"} {
		if _, ok := captured[identity]; !ok {
			t.Fatalf("captured optional mappings = %#v, want %q", captured, identity)
		}
	}
}

func TestLiveCapturedMappingsRequiresExactProductionBundlePaths(t *testing.T) {
	expected := map[string]struct{}{
		"apps/notepad-plus-plus/config.xml": {},
	}
	for _, paths := range [][]string{
		{"apps/notepad-plus-plus/config.xml"},
		{"configs/notepad-plus-plus/foreign.xml"},
		{"configs/notepad-plus-plus/../config.xml"},
		{"configs/Notepad-plus-plus/config.xml"},
		{"configs/notepad-plus-plus/config.xml", "configs/notepad-plus-plus/config.xml"},
	} {
		raw, err := json.Marshal(paths)
		if err != nil {
			t.Fatal(err)
		}
		if captured, ok := liveCapturedMappings(raw, expected); ok || captured != nil {
			t.Fatalf("liveCapturedMappings() accepted %#v: %#v", paths, captured)
		}
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
		Results: []commands.VerifyItem{
			{Type: "app", ID: "notepad++-notepad++", Ref: definition.WingetRef, Driver: "winget", Name: "Notepad++", Status: "pass", Version: "8.7.1"},
		},
	}
	encoded, err := envelope.Marshal(envelope.NewSuccess("rebuild", "rebuild-production", "1.0", "0.1.0", &commands.RebuildResult{
		From:               "$ENDSTATE_ROOT/manifests/captured.zip",
		Bundle:             &commands.RebuildBundleInfo{Extracted: true, SchemaVersion: "1", CapturedAt: "2026-07-26T12:00:00Z", MachineName: "hosted-live", EndstateVersion: "0.1.0", ConfigModulesIncluded: []string{"notepad-plus-plus"}},
		DryRun:             false,
		Restore:            "enabled",
		Apply:              apply,
		Verify:             verify,
		ConfigResultFields: fields,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := validateLiveRebuild(liveCommandOutput{Stdout: encoded, Stderr: liveEvents("rebuild", "rebuild-production")}, definition, map[string]struct{}{definition.Comparator.Mappings[0].Identity: {}}, true, false, nil); failure != nil {
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
		Results: []commands.VerifyItem{
			{Type: "app", ID: "notepad++-notepad++", Ref: definition.WingetRef, Driver: "winget", Name: "Notepad++", Status: "pass", Version: "8.7.1"},
		},
	}
	encoded, err := envelope.Marshal(envelope.NewSuccess("rebuild", "rebuild-production-converged", "1.0", "0.1.0", &commands.RebuildResult{
		From:               "$ENDSTATE_ROOT/manifests/captured.zip",
		Bundle:             &commands.RebuildBundleInfo{Extracted: true, SchemaVersion: "1", CapturedAt: "2026-07-26T12:00:00Z", MachineName: "hosted-live", EndstateVersion: "0.1.0", ConfigModulesIncluded: []string{"notepad-plus-plus"}},
		DryRun:             false,
		Restore:            "enabled",
		Apply:              apply,
		Verify:             verify,
		ConfigResultFields: fields,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := validateLiveRebuild(liveCommandOutput{Stdout: encoded, Stderr: liveEvents("rebuild", "rebuild-production-converged")}, definition, map[string]struct{}{definition.Comparator.Mappings[0].Identity: {}}, false, true, nil); failure != nil {
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

func TestDecodeLiveJourneyAcceptsConcreteRuntimeRestorePaths(t *testing.T) {
	definition, inputs, targets := liveConcreteRestoreJourneyForTest(t)
	if len(targets) != 6 {
		t.Fatalf("runtime restore targets = %#v", targets)
	}
	inputs.runtimeRestoreTargets = targets
	authority, ok := newLiveRuntimeRestoreAuthority(definition, targets)
	if !ok {
		t.Fatal("runtime restore authority rejected test targets")
	}
	envelope, failure := decodeLiveEnvelope(inputs.RestoreRebuild.Stdout, "rebuild")
	if failure != nil {
		t.Fatalf("decodeLiveEnvelope() failure = %+v", failure)
	}
	data, err := liveObjectAllowed(envelope.Data, []string{"from", "dryRun", "restore", "apply", "configResolutionSummary", "configResolutions", "restoreItems", "verify"}, []string{"from", "bundle", "dryRun", "restore", "apply", "configResolutionSummary", "configResolutions", "restoreItems", "verify"})
	if err != nil {
		t.Fatal(err)
	}
	expected, ok := liveExpectedMappings(definition.Comparator.Mappings)
	if !ok {
		t.Fatal("expected mappings are invalid")
	}
	captured, captureFailure := validateLiveCapture(inputs.Capture, definition, expected)
	inventory, inventoryOK := liveRestoreInventory(definition)
	order, orderOK := liveRuntimeRestoreOrder(definition, inventory)
	items, itemsErr := liveArray(data["restoreItems"])
	if captureFailure != nil || !inventoryOK || !orderOK || itemsErr != nil {
		t.Fatalf("runtime test setup capture=%+v inventory=%v order=%v items=%v", captureFailure, inventoryOK, orderOK, itemsErr)
	}
	for index, raw := range items {
		item, err := liveObjectAllowed(raw, []string{"id", "source", "target", "status", "backupCreated", "targetExistedBefore"}, []string{"id", "source", "target", "status", "backupPath", "backupCreated", "targetExistedBefore", "restoreType", "warnings", "error", "captureId", "configSetId", "targetInstanceId", "sourceGeneration", "targetGeneration"})
		if err != nil {
			t.Fatalf("item %d object: %v", index, err)
		}
		var source, target string
		if liveString(item["source"], &source) != nil || liveString(item["target"], &target) != nil {
			t.Fatalf("item %d strings", index)
		}
		semantic, expectedItem, root, found := liveRuntimeRestoreSource(inventory, source)
		if !found || semantic != order[index] || target != authority.targets[expectedItem.identity] || root != `C:\trusted\extract` {
			t.Fatalf("item %d runtime binding semantic=%q order=%q target=%q want=%q root=%q found=%v", index, semantic, order[index], target, authority.targets[expectedItem.identity], root, found)
		}
	}
	if !validateLiveConfigFields(data, definition, captured, false, authority) {
		t.Fatal("concrete runtime restore fields rejected")
	}
	if _, failure := decodeLiveJourney(definition, inputs); failure != nil {
		t.Fatalf("decodeLiveJourney() rejected concrete runtime restore paths: %+v", failure)
	}
}

func TestDecodeLiveJourneyRejectsInvalidConcreteRuntimeRestorePaths(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*liveJourneyOutputs, map[string]string)
	}{
		{"wrong target", func(inputs *liveJourneyOutputs, targets map[string]string) {
			inputs.RestoreRebuild.Stdout = []byte(strings.Replace(string(inputs.RestoreRebuild.Stdout), liveJSONPath(targets["apps/notepad-plus-plus/config.xml"]), liveJSONPath(`C:\Users\runner\AppData\Roaming\Notepad++\foreign.xml`), 1))
		}},
		{"relative source", func(inputs *liveJourneyOutputs, _ map[string]string) {
			inputs.RestoreRebuild.Stdout = []byte(strings.Replace(string(inputs.RestoreRebuild.Stdout), liveJSONPath(`C:\trusted\extract\configs\notepad-plus-plus\config.xml`), `configs/notepad-plus-plus/config.xml`, 1))
		}},
		{"suffix confusion", func(inputs *liveJourneyOutputs, _ map[string]string) {
			inputs.RestoreRebuild.Stdout = []byte(strings.Replace(string(inputs.RestoreRebuild.Stdout), liveJSONPath(`C:\trusted\extract\configs\notepad-plus-plus\config.xml`), liveJSONPath(`C:\trusted\extract\configs\notepad-plus-plus\foreign-config.xml`), 1))
		}},
		{"split extraction roots", func(inputs *liveJourneyOutputs, _ map[string]string) {
			inputs.RecoveryRebuild.Stdout = []byte(strings.Replace(string(inputs.RecoveryRebuild.Stdout), liveJSONPath(`C:\trusted\extract\configs\notepad-plus-plus\config.xml`), liveJSONPath(`C:\other\extract\configs\notepad-plus-plus\config.xml`), 1))
		}},
		{"missing runtime binding", func(inputs *liveJourneyOutputs, targets map[string]string) {
			delete(targets, "apps/notepad-plus-plus/contextMenu.xml")
		}},
		{"extra runtime binding", func(inputs *liveJourneyOutputs, targets map[string]string) {
			targets["apps/notepad-plus-plus/foreign.xml"] = `C:\Users\runner\AppData\Roaming\Notepad++\foreign.xml`
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition, inputs, targets := liveConcreteRestoreJourneyForTest(t)
			inputs.runtimeRestoreTargets = targets
			test.mutate(&inputs, targets)
			if _, failure := decodeLiveJourney(definition, inputs); failure == nil {
				t.Fatal("decodeLiveJourney() accepted an invalid concrete runtime restore path")
			}
		})
	}
}

func TestDecodeLiveJourneyRetainsSemanticRestoreContractWithoutRuntimeAuthority(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	if _, failure := decodeLiveJourney(definition, liveDecoderJourneyOutputs()); failure != nil {
		t.Fatalf("decodeLiveJourney() rejected semantic validation-mode restore results: %+v", failure)
	}
}

func TestDecodeLiveJourneyRejectsMissingOrAdditiveBundleMetadata(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	for _, test := range []struct {
		name   string
		mutate func(*liveJourneyOutputs)
	}{
		{"missing bundle", func(inputs *liveJourneyOutputs) {
			inputs.RestoreRebuild.Stdout = []byte(strings.Replace(string(inputs.RestoreRebuild.Stdout), `"bundle":`, `"omittedBundle":`, 1))
		}},
		{"additive bundle field", func(inputs *liveJourneyOutputs) {
			inputs.RestoreRebuild.Stdout = []byte(strings.Replace(string(inputs.RestoreRebuild.Stdout), `"extracted":true`, `"extracted":true,"unexpected":true`, 1))
		}},
		{"missing bundle metadata", func(inputs *liveJourneyOutputs) {
			inputs.RestoreRebuild.Stdout = []byte(strings.Replace(string(inputs.RestoreRebuild.Stdout), `"machineName":"hosted-live",`, "", 1))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputs := liveDecoderJourneyOutputs()
			test.mutate(&inputs)
			if _, failure := decodeLiveJourney(definition, inputs); failure == nil {
				t.Fatal("decodeLiveJourney() accepted incomplete bundle metadata")
			}
		})
	}
}

func TestDecodeLiveJourneyRejectsBackupPathWhenNoBackupWasCreated(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	inputs := liveDecoderJourneyOutputs()
	inputs.RestoreRebuild.Stdout = []byte(strings.Replace(string(inputs.RestoreRebuild.Stdout), `"backupCreated":false,"targetExistedBefore":false`, `"backupPath":"C:\\trusted\\backup","backupCreated":false,"targetExistedBefore":false`, 1))
	if _, failure := decodeLiveJourney(definition, inputs); failure == nil {
		t.Fatal("decodeLiveJourney() accepted a backup path without a created backup")
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
			value.Verify.Stdout = []byte(strings.Replace(string(value.Verify.Stdout), `"version":"8.7.1"`, `"version":"8.7.1","future":true`, 1))
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

func liveDecoderJourneyOutputs() liveJourneyOutputs {
	return liveJourneyOutputs{
		ScenarioID:         liveConfigRoundtripScenarioID,
		InitialApply:       liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-initial", liveApplyData("installed")), Stderr: liveEvents("apply", "apply-initial")},
		Verify:             liveCommandOutput{Stdout: liveTestEnvelope("verify", "verify-initial", liveVerifyData()), Stderr: liveEvents("verify", "verify-initial")},
		Capture:            liveCommandOutput{Stdout: liveTestEnvelope("capture", "capture-initial", liveCaptureData()), Stderr: liveEvents("capture", "capture-initial")},
		RestoreRebuild:     liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-restore", liveRebuildData("installed", "restored")), Stderr: liveEvents("rebuild", "rebuild-restore")},
		Revert:             liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-config", liveRevertData()), Stderr: liveEvents("revert", "revert-config")},
		RecoveryRebuild:    liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-recovery", liveRebuildData("present", "restored")), Stderr: liveEvents("rebuild", "rebuild-recovery")},
		PackageAfterRevert: PackageObservation{Ref: "Notepad++.Notepad++", Version: "8.7.1", Status: "present"},
		ConvergenceRebuild: liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-converged", liveRebuildData("present", "skipped_up_to_date")), Stderr: liveEvents("rebuild", "rebuild-converged")},
	}
}

func liveConcreteRestoreJourneyForTest(t *testing.T) (LiveDefinition, liveJourneyOutputs, map[string]string) {
	t.Helper()
	definition := productionLiveDecoderDefinition(t)
	inputs := liveDecoderJourneyOutputs()
	targets := map[string]string{
		"apps/notepad-plus-plus/config.xml":      `C:\Users\runner\AppData\Roaming\Notepad++\config.xml`,
		"apps/notepad-plus-plus/shortcuts.xml":   `C:\Users\runner\AppData\Roaming\Notepad++\shortcuts.xml`,
		"apps/notepad-plus-plus/langs.xml":       `C:\Users\runner\AppData\Roaming\Notepad++\langs.xml`,
		"apps/notepad-plus-plus/stylers.xml":     `C:\Users\runner\AppData\Roaming\Notepad++\stylers.xml`,
		"apps/notepad-plus-plus/userDefineLangs": `C:\Users\runner\AppData\Roaming\Notepad++\userDefineLangs`,
		"apps/notepad-plus-plus/contextMenu.xml": `C:\Users\runner\AppData\Roaming\Notepad++\contextMenu.xml`,
	}
	semantic := map[string]string{
		"apps/notepad-plus-plus/config.xml":      "configs/notepad-plus-plus/config.xml",
		"apps/notepad-plus-plus/shortcuts.xml":   "configs/notepad-plus-plus/shortcuts.xml",
		"apps/notepad-plus-plus/langs.xml":       "configs/notepad-plus-plus/langs.xml",
		"apps/notepad-plus-plus/stylers.xml":     "configs/notepad-plus-plus/stylers.xml",
		"apps/notepad-plus-plus/userDefineLangs": "configs/notepad-plus-plus/userDefineLangs",
		"apps/notepad-plus-plus/contextMenu.xml": `configs/notepad-plus-plus/contextMenu.xml`,
	}
	for identity, source := range semantic {
		concreteSource := `C:\trusted\extract\` + strings.ReplaceAll(source, "/", `\`)
		for _, output := range []*liveCommandOutput{&inputs.RestoreRebuild, &inputs.RecoveryRebuild, &inputs.ConvergenceRebuild} {
			output.Stdout = []byte(strings.ReplaceAll(string(output.Stdout), source, liveJSONPath(concreteSource)))
			output.Stdout = []byte(strings.ReplaceAll(string(output.Stdout), liveJSONPath(liveConcreteRestoreTemplate(identity)), liveJSONPath(targets[identity])))
		}
		inputs.Revert.Stdout = []byte(strings.ReplaceAll(string(inputs.Revert.Stdout), liveJSONPath(liveConcreteRestoreTemplate(identity)), liveJSONPath(targets[identity])))
	}
	return definition, inputs, targets
}

func liveConcreteRestoreTemplate(identity string) string {
	return `%APPDATA%\Notepad++\` + strings.TrimPrefix(identity, "apps/notepad-plus-plus/")
}

func liveJSONPath(path string) string { return strings.ReplaceAll(path, `\`, `\\`) }

func liveEvents(command, runID string) []byte {
	var stream bytes.Buffer
	emit := func(emitter *events.Emitter, verifyTotal int, phases ...string) {
		for _, phase := range phases {
			emitter.EmitPhase(phase)
			if command == "capture" {
				emitter.EmitProgress("capture", "inventory")
				emitter.EmitItem("Notepad++.Notepad++", "winget", "present", "detected", "", "Notepad++")
				emitter.EmitArtifact("capture", "manifest", "captured.zip")
			}
			total := 1
			if phase == "verify" {
				total = verifyTotal
			}
			emitter.EmitSummary(phase, total, total, 0, 0)
		}
	}
	if command == "rebuild" {
		emit(events.NewEmitterWithWriter("apply-"+runID, true, &stream), 1, "plan", "apply", "restore", "verify")
		emit(events.NewEmitterWithWriter("verify-"+runID, true, &stream), 2, "verify")
		return stream.Bytes()
	}
	phaseSets := map[string][]string{"apply": {"plan", "apply", "verify"}, "verify": {"verify"}, "capture": {"capture"}, "revert": {"restore"}}
	verifyTotal := 1
	emit(events.NewEmitterWithWriter(runID, true, &stream), verifyTotal, phaseSets[command]...)
	return stream.Bytes()
}

func liveVerifyData() string {
	return `{"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured"},"summary":{"total":1,"pass":1,"fail":0,"skipped":0},"results":[{"type":"app","id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","name":"Notepad++","status":"pass","version":"8.7.1"}]}`
}

func TestValidateLiveVerifyDataRequiresProductionInventory(t *testing.T) {
	definition := productionLiveDecoderDefinition(t)
	valid := []byte(liveVerifyData())
	if failure := validateLiveVerifyData(valid, definition, 0); failure != nil {
		t.Fatalf("validateLiveVerifyData() rejected production inventory: %+v", failure)
	}
	for _, test := range []struct {
		name string
		data string
	}{
		{"command only", `{"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured"},"summary":{"total":1,"pass":1,"fail":0,"skipped":0},"results":[{"type":"command-exists","status":"pass","message":"notepad++ found"}]}`},
		{"extra verifier", `{"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured"},"summary":{"total":2,"pass":2,"fail":0,"skipped":0},"results":[{"type":"app","id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","status":"pass"},{"type":"command-exists","status":"pass","message":"notepad++ found"}]}`},
		{"duplicate app", `{"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured"},"summary":{"total":2,"pass":2,"fail":0,"skipped":0},"results":[{"type":"app","id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","status":"pass"},{"type":"app","id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","status":"pass"}]}`},
		{"wrong app identity", strings.Replace(liveVerifyData(), `"notepad++-notepad++"`, `"foreign-app"`, 1)},
		{"summary mismatch", strings.Replace(liveVerifyData(), `"total":1,"pass":1`, `"total":1,"pass":0`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if failure := validateLiveVerifyData([]byte(test.data), definition, 0); failure == nil {
				t.Fatal("validateLiveVerifyData() accepted a hostile verify inventory")
			}
		})
	}
}

func liveCaptureData() string {
	return `{"appsIncluded":[{"source":"winget","id":"Notepad++.Notepad++","manifestId":"notepad++-notepad++"}],"configModules":[{"displayName":"Notepad++","wingetRefs":["Notepad++.Notepad++"],"chocolateyRefs":[],"appId":"notepad-plus-plus","id":"apps.notepad-plus-plus","paths":["configs/notepad-plus-plus/config.xml","configs/notepad-plus-plus/shortcuts.xml"],"filesCaptured":2,"status":"captured"}],"configModuleMap":{"notepad-plus-plus":"apps.notepad-plus-plus"},"packageModuleMap":{"Notepad++.Notepad++":["apps.notepad-plus-plus"]},"outputPath":"$ENDSTATE_ROOT/manifests/captured.zip","outputFormat":"zip","configsIncluded":["notepad-plus-plus"],"configsSkipped":[],"configsCaptureErrors":[],"sanitized":true,"isExample":false,"counts":{"filteredRuntimes":0,"included":1,"totalFound":1,"sensitiveExcludedCount":0,"filteredStoreApps":0,"skipped":0},"captureWarnings":[],"manifest":{"name":"captured","path":"$ENDSTATE_ROOT/manifests/captured.zip"}}`
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
	return fmt.Sprintf(`{"from":"$ENDSTATE_ROOT/manifests/captured.zip","bundle":{"extracted":true,"schemaVersion":"1","capturedAt":"2026-07-26T12:00:00Z","machineName":"hosted-live","endstateVersion":"0.1.0","configModulesIncluded":["notepad-plus-plus"]},"dryRun":false,"restore":"enabled","apply":{"dryRun":false,"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured","hash":"sha256:fixture"},"summary":{"total":1,"success":%d,"skipped":%d,"failed":0},"actions":[{"id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","status":%q,"reason":%q,"manual":null}],%s},%s,"verify":%s}`, success, skipped, packageStatus, reason, config, config, liveVerifyData())
}

func liveApplyData(status string) string {
	success, skipped, reason := 0, 1, "already_installed"
	if status == "installed" {
		success, skipped, reason = 1, 0, ""
	}
	return fmt.Sprintf(`{"dryRun":false,"manifest":{"path":"$ENDSTATE_ROOT/manifests/captured.jsonc","name":"captured","hash":"sha256:fixture"},"summary":{"total":1,"success":%d,"skipped":%d,"failed":0},"actions":[{"id":"notepad++-notepad++","ref":"Notepad++.Notepad++","driver":"winget","status":%q,"reason":%q,"manual":null}]}`, success, skipped, status, reason)
}

func liveRevertData() string {
	return `{"journalUsed":"C:\\trusted\\restore\\journal.json","results":[{"target":"%APPDATA%\\Notepad++\\shortcuts.xml","action":"deleted"},{"target":"%APPDATA%\\Notepad++\\config.xml","action":"deleted"}]}`
}
