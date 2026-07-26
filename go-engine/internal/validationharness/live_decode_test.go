// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"fmt"
	"strings"
	"testing"
)

func TestDecodeLiveJourneyProjectsStrictHostedNotepadJourney(t *testing.T) {
	definition := LiveDefinition{
		ModuleID:  "apps.notepad-plus-plus",
		WingetRef: "Notepad++.Notepad++",
		Comparator: ExactBytesComparator{Mappings: []ComparatorMapping{
			{Identity: "apps/notepad-plus-plus/config.xml", RestoreTemplate: `%APPDATA%\Notepad++\config.xml`},
			{Identity: "apps/notepad-plus-plus/shortcuts.xml", RestoreTemplate: `%APPDATA%\Notepad++\shortcuts.xml`},
		}},
	}
	inputs := liveJourneyOutputs{
		ScenarioID:         liveConfigRoundtripScenarioID,
		InitialApply:       liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-initial", `{"dryRun":false,"summary":{"total":1,"success":1,"skipped":0,"failed":0},"actions":[{"id":"notepad-plus-plus","ref":"Notepad++.Notepad++","driver":"winget","status":"installed","reason":""}]}`), Stderr: liveEvents("apply", "apply-initial")},
		Verify:             liveCommandOutput{Stdout: liveTestEnvelope("verify", "verify-initial", liveVerifyData()), Stderr: liveEvents("verify", "verify-initial")},
		Capture:            liveCommandOutput{Stdout: liveTestEnvelope("capture", "capture-initial", liveCaptureData()), Stderr: liveEvents("capture", "capture-initial")},
		Rebuild:            liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-restore", liveRebuildData("restored", "restored")), Stderr: liveEvents("rebuild", "rebuild-restore")},
		Revert:             liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-config", liveRevertData()), Stderr: liveEvents("revert", "revert-config")},
		Recovery:           liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-recovery", liveRevertData()), Stderr: liveEvents("revert", "revert-recovery")},
		PackageAfterRevert: PackageObservation{Ref: "Notepad++.Notepad++", Version: "8.7.1", Status: "present"},
		FinalApply:         liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-converged", `{"dryRun":false,"summary":{"total":1,"success":0,"skipped":1,"failed":0},"actions":[{"id":"notepad-plus-plus","ref":"Notepad++.Notepad++","driver":"winget","status":"present","reason":"already_installed"}],"pruned":[]}`), Stderr: liveEvents("apply", "apply-converged")},
	}

	projection, failure := decodeLiveJourney(definition, inputs)
	if failure != nil {
		t.Fatalf("decodeLiveJourney() failure = %+v", failure)
	}
	if projection.ModuleID != definition.ModuleID || projection.Ref != definition.WingetRef || projection.CapturedMappings != 2 || projection.RestoredMappings != 2 || !projection.PackagePresentAfterRevert || !projection.ConvergedWithoutMutation {
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
	definition := LiveDefinition{ModuleID: "apps.notepad-plus-plus", WingetRef: "Notepad++.Notepad++", Comparator: ExactBytesComparator{Mappings: []ComparatorMapping{{Identity: "apps/notepad-plus-plus/config.xml", RestoreTemplate: `%APPDATA%\Notepad++\config.xml`}, {Identity: "apps/notepad-plus-plus/shortcuts.xml", RestoreTemplate: `%APPDATA%\Notepad++\shortcuts.xml`}}}}
	valid := liveJourneyOutputs{
		ScenarioID:         liveConfigRoundtripScenarioID,
		InitialApply:       liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-initial", `{"dryRun":false,"summary":{"total":1,"success":1,"skipped":0,"failed":0},"actions":[{"id":"notepad-plus-plus","ref":"Notepad++.Notepad++","driver":"winget","status":"installed","reason":""}]}`), Stderr: liveEvents("apply", "apply-initial")},
		Verify:             liveCommandOutput{Stdout: liveTestEnvelope("verify", "verify-initial", liveVerifyData()), Stderr: liveEvents("verify", "verify-initial")},
		Capture:            liveCommandOutput{Stdout: liveTestEnvelope("capture", "capture-initial", liveCaptureData()), Stderr: liveEvents("capture", "capture-initial")},
		Rebuild:            liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-restore", liveRebuildData("restored", "restored")), Stderr: liveEvents("rebuild", "rebuild-restore")},
		Revert:             liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-config", liveRevertData()), Stderr: liveEvents("revert", "revert-config")},
		Recovery:           liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-recovery", liveRevertData()), Stderr: liveEvents("revert", "revert-recovery")},
		PackageAfterRevert: PackageObservation{Ref: "Notepad++.Notepad++", Status: "present"},
		FinalApply:         liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-converged", `{"dryRun":false,"summary":{"total":1,"success":0,"skipped":1,"failed":0},"actions":[{"id":"notepad-plus-plus","ref":"Notepad++.Notepad++","driver":"winget","status":"present","reason":"already_installed"}],"pruned":[]}`), Stderr: liveEvents("apply", "apply-converged")},
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
			value.Rebuild.Stdout = []byte(strings.Replace(string(value.Rebuild.Stdout), `"selected":1`, `"selected":0`, 1))
		}},
		{"package absent after revert", func(value *liveJourneyOutputs) { value.PackageAfterRevert.Status = "absent" }},
		{"converged apply mutation", func(value *liveJourneyOutputs) {
			value.FinalApply.Stdout = []byte(strings.Replace(string(value.FinalApply.Stdout), `"success":0,"skipped":1`, `"success":1,"skipped":0`, 1))
		}},
		{"missing terminal event", func(value *liveJourneyOutputs) {
			value.Capture.Stderr = []byte(`{"version":1,"runId":"capture-initial","timestamp":"2026-07-26T12:00:00Z","event":"phase","phase":"capture"}`)
		}},
		{"mismatched run id", func(value *liveJourneyOutputs) { value.Rebuild.Stderr = liveEvents("rebuild", "foreign-run") }},
		{"duplicate JSON key", func(value *liveJourneyOutputs) {
			value.Verify.Stdout = []byte(strings.Replace(string(value.Verify.Stdout), `"runId":"verify-initial"`, `"runId":"verify-initial","runId":"forged"`, 1))
		}},
		{"unexpected data field", func(value *liveJourneyOutputs) {
			value.Capture.Stdout = []byte(strings.Replace(string(value.Capture.Stdout), `"counts":`, `"future":true,"counts":`, 1))
		}},
		{"unexpected nested field", func(value *liveJourneyOutputs) {
			value.Verify.Stdout = []byte(strings.Replace(string(value.Verify.Stdout), `"pass":3`, `"pass":3,"future":true`, 1))
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
	phase := map[string]string{"apply": "plan", "verify": "verify", "capture": "capture", "rebuild": "restore", "revert": "restore"}[command]
	return []byte(fmt.Sprintf(`{"version":1,"runId":%q,"timestamp":"2026-07-26T12:00:00Z","event":"phase","phase":%q}`+"\n"+`{"version":1,"runId":%q,"timestamp":"2026-07-26T12:00:01Z","event":"summary","phase":%q,"total":1,"success":1,"skipped":0,"failed":0}`+"\n", runID, phase, runID, phase))
}

func liveVerifyData() string {
	return `{"summary":{"total":3,"pass":3,"fail":0,"skipped":0},"results":[{"type":"app","id":"notepad-plus-plus","ref":"Notepad++.Notepad++","driver":"winget","status":"pass","reason":""},{"type":"file-exists","status":"pass","reason":""},{"type":"file-exists","status":"pass","reason":""}]}`
}

func liveCaptureData() string {
	return `{"appsIncluded":[{"id":"Notepad++.Notepad++","manifestId":"notepad-plus-plus"}],"configModules":[{"id":"apps.notepad-plus-plus","wingetRefs":["Notepad++.Notepad++"],"filesCaptured":2,"status":"captured","paths":["apps/notepad-plus-plus/config.xml","apps/notepad-plus-plus/shortcuts.xml"]}],"counts":{"included":1,"skipped":0,"totalFound":1},"configsIncluded":["apps.notepad-plus-plus"],"configsSkipped":[],"configsCaptureErrors":[]}`
}

func liveRebuildData(statusOne, statusTwo string) string {
	return fmt.Sprintf(`{"dryRun":false,"restore":"enabled","apply":{"dryRun":false,"summary":{"total":1,"success":0,"skipped":1,"failed":0},"actions":[{"id":"notepad-plus-plus","ref":"Notepad++.Notepad++","driver":"winget","status":"present","reason":"already_installed"}]},"configResolutionSummary":{"total":1,"selected":1,"skipped":0,"failed":0},"configResolutions":[{"status":"restored","resolution":"legacy_unverified","reason":null}],"restoreItems":[{"source":"apps/notepad-plus-plus/config.xml","target":"%%APPDATA%%\\Notepad++\\config.xml","status":%q,"backupCreated":true,"targetExistedBefore":true,"restoreType":"copy"},{"source":"apps/notepad-plus-plus/shortcuts.xml","target":"%%APPDATA%%\\Notepad++\\shortcuts.xml","status":%q,"backupCreated":true,"targetExistedBefore":true,"restoreType":"copy"}],"verify":%s}`, statusOne, statusTwo, liveVerifyData())
}

func liveRevertData() string {
	return `{"journalUsed":"opaque-journal","results":[{"action":"reverted"},{"action":"reverted"}]}`
}
