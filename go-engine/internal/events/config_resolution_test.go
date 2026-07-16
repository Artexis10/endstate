// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestEmitConfigResolutionExactJSON(t *testing.T) {
	tests := []struct {
		name string
		set  planner.PlanSet
		want string
	}{
		{
			name: "direct",
			set:  configResolutionTestSet(planner.ResolutionDirect),
			want: `{
				"version":1,"runId":"apply-resolution","timestamp":"<timestamp>","event":"config-resolution",
				"captureId":"capture-a","moduleId":"apps.example","configSetId":"preferences",
				"sourceInstance":{"id":"source-a","detectorId":"installed","rawVersion":"1.5","normalizedVersion":"1.5","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"}},
				"sourceInstanceId":"source-a","targetInstanceId":"target-a",
				"targetCandidates":[{"id":"target-a","moduleId":"apps.example","detectorId":"installed","rawVersion":"1.9","normalizedVersion":"1.9","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"},"targetGeneration":"g1","targetGenerationFingerprint":"fingerprint-g1","restoreModuleRevision":"restore-revision"}],
				"sourceGeneration":"g1","sourceGenerationFingerprint":"fingerprint-g1","targetGeneration":"g1",
				"resolution":"direct","reason":null,"migrationPath":[],"captureModuleRevision":"capture-revision","restoreModuleRevision":"restore-revision",
				"label":"Compatible","message":"Settings are compatible with the selected target.","remediation":null
			}`,
		},
		{
			name: "migrate",
			set:  configResolutionMigrationTestSet(),
			want: `{
				"version":1,"runId":"apply-resolution","timestamp":"<timestamp>","event":"config-resolution",
				"captureId":"capture-a","moduleId":"apps.example","configSetId":"preferences",
				"sourceInstance":{"id":"source-a","detectorId":"installed","rawVersion":"1.5","normalizedVersion":"1.5","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"}},
				"sourceInstanceId":"source-a","targetInstanceId":"target-a",
				"targetCandidates":[{"id":"target-a","moduleId":"apps.example","detectorId":"installed","rawVersion":"1.9","normalizedVersion":"1.9","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"},"targetGeneration":"g3","targetGenerationFingerprint":"fingerprint-g3","restoreModuleRevision":"restore-revision"}],
				"sourceGeneration":"g1","sourceGenerationFingerprint":"fingerprint-g1","targetGeneration":"g3",
				"resolution":"migrate","reason":null,"migrationPath":["g1","g2","g3"],"captureModuleRevision":"capture-revision","restoreModuleRevision":"restore-revision",
				"label":"Will be upgraded","message":"Settings will be upgraded from g1 to g3 before restore.","remediation":null
			}`,
		},
		{
			name: "unknown",
			set:  configResolutionUnknownTestSet(),
			want: `{
				"version":1,"runId":"apply-resolution","timestamp":"<timestamp>","event":"config-resolution",
				"captureId":"capture-a","moduleId":"apps.example","configSetId":"preferences",
				"sourceInstance":{"id":"source-a","detectorId":"installed","rawVersion":"1.5","normalizedVersion":"1.5","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"}},
				"sourceInstanceId":"source-a","targetCandidates":[],
				"sourceGeneration":"g1","sourceGenerationFingerprint":"fingerprint-g1",
				"resolution":"unknown","reason":"target_detection_failed","migrationPath":[],"captureModuleRevision":"capture-revision",
				"label":"Compatibility unknown","message":"Target detection failed, so compatibility could not be determined.","remediation":"Review the detection failure, resolve its cause, and retry."
			}`,
		},
		{
			name: "legacy",
			set:  configResolutionLegacyTestSet(),
			want: `{
				"version":1,"runId":"apply-resolution","timestamp":"<timestamp>","event":"config-resolution",
				"captureId":"legacy-capture","moduleId":"apps.legacy","configSetId":"legacy",
				"targetCandidates":[],"resolution":"legacy_unverified","reason":null,"migrationPath":[],
				"label":"Compatibility unknown","message":"These settings predate compatibility checks, so compatibility could not be verified.","remediation":"Review the legacy settings and enable restore only if you trust this backup."
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter, buffer := captureEmitter("apply-resolution")
			emitter.EmitConfigResolution(tt.set)
			assertExactEventJSON(t, lastLine(buffer), tt.want)
			if strings.Contains(buffer.String(), `C:\Users\secret`) || strings.Contains(buffer.String(), "resolvedTargets") ||
				strings.Contains(buffer.String(), `"status"`) {
				t.Fatalf("config-resolution leaked internal/envelope-only fields: %s", buffer.String())
			}
		})
	}
}

func TestMigrationStageObserverEmitsOrderedClosedProgress(t *testing.T) {
	emitter, buffer := captureEmitter("apply-migration")
	observer := NewMigrationStageObserver(emitter, "preferences")
	progress := []migration.StageProgress{
		{CaptureID: "capture-a", Stage: migration.ProgressStaging, Status: migration.ProgressStarted, EdgeIndex: -1},
		{CaptureID: "capture-a", Stage: migration.ProgressStaging, Status: migration.ProgressCompleted, EdgeIndex: -1},
		{CaptureID: "capture-a", Stage: migration.ProgressEdge, Status: migration.ProgressStarted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-a", Stage: migration.ProgressEdge, Status: migration.ProgressCompleted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-a", Stage: migration.ProgressValidation, Status: migration.ProgressStarted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-a", Stage: migration.ProgressValidation, Status: migration.ProgressCompleted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-a", Stage: migration.ProgressEdge, Status: migration.ProgressStarted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
		{CaptureID: "capture-a", Stage: migration.ProgressEdge, Status: migration.ProgressCompleted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
		{CaptureID: "capture-a", Stage: migration.ProgressValidation, Status: migration.ProgressStarted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
		{CaptureID: "capture-a", Stage: migration.ProgressValidation, Status: migration.ProgressCompleted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
	}
	for _, transition := range progress {
		observer.ObserveStageProgress(transition)
	}

	events := parseEventLines(t, buffer.String())
	if len(events) != len(progress) {
		t.Fatalf("event count = %d, want %d", len(events), len(progress))
	}
	wantStages := []string{"staging", "staging", "edge", "edge", "validation", "validation", "edge", "edge", "validation", "validation"}
	wantStatuses := []string{"started", "completed", "started", "completed", "started", "completed", "started", "completed", "started", "completed"}
	wantMessages := []string{
		"staging settings payload", "settings payload staged",
		"applying migration edge", "migration edge validated",
		"validating staged settings", "staged settings validated",
		"applying migration edge", "migration edge validated",
		"validating staged settings", "staged settings validated",
	}
	for index, event := range events {
		if event["event"] != "config-migration" || event["stage"] != wantStages[index] || event["status"] != wantStatuses[index] ||
			event["captureId"] != "capture-a" || event["configSetId"] != "preferences" ||
			event["message"] != wantMessages[index] {
			t.Fatalf("event %d = %#v", index, event)
		}
		_, hasFrom := event["fromGeneration"]
		_, hasTo := event["toGeneration"]
		if wantStages[index] == "edge" {
			if !hasFrom || !hasTo {
				t.Fatalf("edge %d omitted generation pair: %#v", index, event)
			}
		} else if hasFrom || hasTo {
			t.Fatalf("non-edge %d leaked generation pair: %#v", index, event)
		}
		if event["reason"] != nil || event["remediation"] != nil {
			t.Fatalf("successful transition %d null shape = %#v", index, event)
		}
	}
	if events[2]["fromGeneration"] != "g1" || events[2]["toGeneration"] != "g2" ||
		events[6]["fromGeneration"] != "g2" || events[6]["toGeneration"] != "g3" {
		t.Fatalf("multi-edge order = %#v", events)
	}
}

func TestMigrationStageObserverRejectsUnknownInternalVocabulary(t *testing.T) {
	emitter, buffer := captureEmitter("apply-invalid-progress")
	observer := NewMigrationStageObserver(emitter, "preferences")
	observer.ObserveStageProgress(migration.StageProgress{
		CaptureID: "capture-a", Stage: migration.ProgressStage("commit"), Status: migration.ProgressStarted,
	})
	observer.ObserveStageProgress(migration.StageProgress{
		CaptureID: "capture-a", Stage: migration.ProgressStaging, Status: migration.ProgressStatus("running"),
	})
	if buffer.Len() != 0 {
		t.Fatalf("unknown migration vocabulary emitted %q", buffer.String())
	}
}

func TestMigrationStageObserverMapsFailuresThroughPlannerPresentation(t *testing.T) {
	tests := []struct {
		name        string
		progress    migration.StageProgress
		wantReason  string
		wantMessage string
		wantFix     string
	}{
		{
			name: "payload integrity", progress: migration.StageProgress{
				CaptureID: "capture-a", Stage: migration.ProgressStaging, Status: migration.ProgressFailed,
				EdgeIndex: -1, Code: migration.CodePayloadIntegrityFailed,
			},
			wantReason:  "payload_integrity_failed",
			wantMessage: "The captured settings payload failed integrity verification.",
			wantFix:     "Create a new backup or use an untampered backup.",
		},
		{
			name: "validation", progress: migration.StageProgress{
				CaptureID: "capture-a", Stage: migration.ProgressValidation, Status: migration.ProgressFailed,
				EdgeIndex: 0, Code: migration.CodeValidationFailed,
			},
			wantReason:  "staging_validation_failed",
			wantMessage: "The staged settings failed validation before target changes began.",
			wantFix:     "Review the staging validation failure, resolve its cause, and retry.",
		},
		{
			name: "other staging", progress: migration.StageProgress{
				CaptureID: "capture-a", Stage: migration.ProgressEdge, Status: migration.ProgressFailed,
				EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2", Code: migration.CodeIO,
			},
			wantReason:  "staging_validation_failed",
			wantMessage: "The staged settings failed validation before target changes began.",
			wantFix:     "Review the staging validation failure, resolve its cause, and retry.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter, buffer := captureEmitter("apply-failure")
			observer := NewMigrationStageObserver(emitter, "preferences")
			observer.ObserveStageProgress(tt.progress)
			event := parseEvent(t, lastLine(buffer))
			if event["reason"] != tt.wantReason || event["message"] != tt.wantMessage || event["remediation"] != tt.wantFix ||
				event["status"] != "failed" {
				t.Fatalf("failure event = %#v", event)
			}
		})
	}
}

func TestConfigResolutionPrecedesMigrationWhenInvokedInContractOrder(t *testing.T) {
	emitter, buffer := captureEmitter("apply-order")
	emitter.EmitConfigResolution(configResolutionMigrationTestSet())
	NewMigrationStageObserver(emitter, "preferences").ObserveStageProgress(migration.StageProgress{
		CaptureID: "capture-a", Stage: migration.ProgressStaging, Status: migration.ProgressStarted, EdgeIndex: -1,
	})
	events := parseEventLines(t, buffer.String())
	if len(events) != 2 || events[0]["event"] != "config-resolution" || events[1]["event"] != "config-migration" {
		t.Fatalf("contract order = %#v", events)
	}
}

func TestConfigResolutionAndMigrationObserverDisabledAreNoOp(t *testing.T) {
	emitter, buffer := captureEmitter("disabled-config")
	emitter.enabled = false
	emitter.EmitConfigResolution(configResolutionTestSet(planner.ResolutionDirect))
	NewMigrationStageObserver(emitter, "preferences").ObserveStageProgress(migration.StageProgress{
		CaptureID: "capture-a", Stage: migration.ProgressStaging, Status: migration.ProgressStarted, EdgeIndex: -1,
	})
	if buffer.Len() != 0 {
		t.Fatalf("disabled config events wrote %q", buffer.String())
	}
}

func configResolutionTestSet(resolution planner.Resolution) planner.PlanSet {
	target := planner.TargetInstance{
		ID: "target-a", ModuleID: "apps.example", DetectorID: "installed",
		RawVersion: "1.9", NormalizedVersion: "1.9",
		Evidence:   planner.InstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.Example"},
		Generation: "g1", GenerationFingerprint: "fingerprint-g1", ModuleRevision: "restore-revision",
		Root: `C:\Users\secret\Example`,
	}
	return planner.PlanSet{
		Source: planner.SourceCapture{
			CaptureID: "capture-a", ModuleID: "apps.example", ConfigSetID: "preferences",
			Instance: planner.SourceInstance{
				ID: "source-a", DetectorID: "installed", RawVersion: "1.5", NormalizedVersion: "1.5",
				Evidence: planner.InstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.Example"},
			},
			Generation: "g1", GenerationFingerprint: "fingerprint-g1", ModuleRevision: "capture-revision",
		},
		TargetInstances: []planner.TargetInstance{target},
		Resolution: planner.ConfigResolution{
			TargetInstanceID: "target-a", TargetGeneration: "g1", Resolution: resolution,
			MigrationPath: []string{}, RestoreModuleRevision: "restore-revision",
			ResolvedTargets: []string{`C:\Users\secret\Example\preferences.json`}, Status: planner.StatusRestored,
		},
	}
}

func configResolutionMigrationTestSet() planner.PlanSet {
	set := configResolutionTestSet(planner.ResolutionMigrate)
	set.TargetInstances[0].Generation = "g3"
	set.TargetInstances[0].GenerationFingerprint = "fingerprint-g3"
	set.Resolution.TargetGeneration = "g3"
	set.Resolution.MigrationPath = []string{"g1", "g2", "g3"}
	return set
}

func configResolutionUnknownTestSet() planner.PlanSet {
	set := configResolutionTestSet(planner.ResolutionUnknown)
	reason := planner.ReasonTargetDetectionFailed
	set.TargetInstances = []planner.TargetInstance{}
	set.Resolution.TargetInstanceID = ""
	set.Resolution.TargetGeneration = ""
	set.Resolution.RestoreModuleRevision = ""
	set.Resolution.Reason = &reason
	set.Resolution.Status = planner.StatusSkipped
	return set
}

func configResolutionLegacyTestSet() planner.PlanSet {
	return planner.PlanSet{
		Source:          planner.SourceCapture{CaptureID: "legacy-capture", ModuleID: "apps.legacy", ConfigSetID: "legacy"},
		TargetInstances: []planner.TargetInstance{},
		Resolution: planner.ConfigResolution{
			Resolution: planner.ResolutionLegacyUnverified, MigrationPath: []string{},
			ResolvedTargets: []string{}, Status: planner.StatusRestored,
		},
	}
}

func assertExactEventJSON(t *testing.T, line, want string) {
	t.Helper()
	var gotValue map[string]any
	if err := json.Unmarshal([]byte(line), &gotValue); err != nil {
		t.Fatalf("invalid event JSON: %v\n%s", err, line)
	}
	timestamp, ok := gotValue["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp = %#v", gotValue["timestamp"])
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		t.Fatalf("timestamp %q: %v", timestamp, err)
	}
	gotValue["timestamp"] = "<timestamp>"

	var wantValue map[string]any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("invalid expected JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		gotJSON, _ := json.MarshalIndent(gotValue, "", "  ")
		wantJSON, _ := json.MarshalIndent(wantValue, "", "  ")
		t.Fatalf("event JSON mismatch\ngot:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
}

func parseEventLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return []map[string]any{}
	}
	events := make([]map[string]any, len(lines))
	for index, line := range lines {
		events[index] = parseEvent(t, line)
	}
	return events
}
