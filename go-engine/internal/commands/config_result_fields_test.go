// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestConfigResultFieldsOmittedWhenNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data any
		want string
	}{
		{
			name: "apply",
			data: ApplyResult{},
			want: `{"dryRun":false,"manifest":{"path":"","name":"","hash":""},"summary":{"total":0,"success":0,"skipped":0,"failed":0},"actions":null}`,
		},
		{
			name: "restore",
			data: RestoreData{},
			want: `{"results":null,"dryRun":false}`,
		},
		{
			name: "rebuild",
			data: RebuildResult{},
			want: `{"from":"","dryRun":false,"restore":"","apply":null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertExactJSON(t, tt.data, tt.want)
		})
	}
}

func TestConfigResultFieldsPresentWithEmptyArrays(t *testing.T) {
	t.Parallel()

	fields := NewConfigResultFields(nil, nil)
	tests := []struct {
		name string
		data any
		want string
	}{
		{
			name: "apply",
			data: ApplyResult{ConfigResultFields: fields},
			want: `{"dryRun":false,"manifest":{"path":"","name":"","hash":""},"summary":{"total":0,"success":0,"skipped":0,"failed":0},"actions":null,"configResolutions":[],"configResolutionSummary":{"total":0,"direct":0,"migrate":0,"incompatible":0,"unknown":0,"legacyUnverified":0,"selected":0,"skipped":0,"failed":0},"restoreItems":[]}`,
		},
		{
			name: "restore",
			data: RestoreData{ConfigResultFields: fields},
			want: `{"results":null,"dryRun":false,"configResolutions":[],"configResolutionSummary":{"total":0,"direct":0,"migrate":0,"incompatible":0,"unknown":0,"legacyUnverified":0,"selected":0,"skipped":0,"failed":0},"restoreItems":[]}`,
		},
		{
			name: "rebuild",
			data: RebuildResult{ConfigResultFields: fields},
			want: `{"from":"","dryRun":false,"restore":"","apply":null,"configResolutions":[],"configResolutionSummary":{"total":0,"direct":0,"migrate":0,"incompatible":0,"unknown":0,"legacyUnverified":0,"selected":0,"skipped":0,"failed":0},"restoreItems":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertExactJSON(t, tt.data, tt.want)
		})
	}
}

func TestConfigResultFieldsProjectsResolutionAndLinkedRestoreItem(t *testing.T) {
	t.Parallel()

	set := linkedPlanSet()
	item := linkedRestoreItem()
	result := ApplyResult{ConfigResultFields: NewConfigResultFields([]planner.PlanSet{set}, []restore.RestoreResult{item})}

	assertExactJSON(t, result,
		`{"dryRun":false,"manifest":{"path":"","name":"","hash":""},"summary":{"total":0,"success":0,"skipped":0,"failed":0},"actions":null,"configResolutions":[{"captureId":"capture-1","moduleId":"apps.photoshop","configSetId":"preferences","sourceInstance":{"id":"photoshop-2024","detectorId":"creative-cloud","rawVersion":"25.0","normalizedVersion":"25.0.0","evidence":{"type":"application","appId":"photoshop"}},"sourceInstanceId":"photoshop-2024","targetInstanceId":"photoshop-2025","targetCandidates":[{"id":"photoshop-2025","moduleId":"apps.photoshop","detectorId":"creative-cloud","rawVersion":"26.0","normalizedVersion":"26.0.0","evidence":{"type":"application","appId":"photoshop"},"targetGeneration":"g2","targetGenerationFingerprint":"sha256:g2","restoreModuleRevision":"restore-rev"}],"sourceGeneration":"g1","sourceGenerationFingerprint":"sha256:g1","targetGeneration":"g2","resolution":"migrate","reason":null,"migrationPath":["g1","g2"],"captureModuleRevision":"capture-rev","restoreModuleRevision":"restore-rev","resolvedTargets":["settings/preferences.json"],"status":"restored","label":"Will be upgraded","message":"Settings will be upgraded from g1 to g2 before restore.","remediation":null}],"configResolutionSummary":{"total":1,"direct":0,"migrate":1,"incompatible":0,"unknown":0,"legacyUnverified":0,"selected":1,"skipped":0,"failed":0},"restoreItems":[{"id":"copy:preferences","source":"configs/capture-1/preferences.json","target":"settings/preferences.json","status":"restored","backupPath":"backups/preferences.json","backupCreated":true,"targetExistedBefore":true,"warnings":["sensitive target reviewed"],"restoreType":"copy","captureId":"capture-1","configSetId":"preferences","targetInstanceId":"photoshop-2025","sourceGeneration":"g1","targetGeneration":"g2"}]}`)
}

func TestConfigResultFieldsUseIdenticalVocabularyAcrossRestoreCapableCommands(t *testing.T) {
	t.Parallel()

	fields := NewConfigResultFields([]planner.PlanSet{linkedPlanSet()}, []restore.RestoreResult{linkedRestoreItem()})
	results := []any{
		ApplyResult{ConfigResultFields: fields},
		RestoreData{ConfigResultFields: fields},
		RebuildResult{ConfigResultFields: fields},
	}

	var want map[string]json.RawMessage
	for index, result := range results {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal result %d: %v", index, err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal result %d: %v", index, err)
		}
		configFields := map[string]json.RawMessage{
			"configResolutions":       decoded["configResolutions"],
			"configResolutionSummary": decoded["configResolutionSummary"],
			"restoreItems":            decoded["restoreItems"],
		}
		if index == 0 {
			want = configFields
			continue
		}
		if !reflect.DeepEqual(configFields, want) {
			t.Fatalf("config vocabulary differs for result %d\n got: %#v\nwant: %#v", index, configFields, want)
		}
	}
}

func TestNewConfigResultFieldsDefensivelyClonesInputs(t *testing.T) {
	t.Parallel()

	reason := planner.ReasonCommitFailed
	sets := []planner.PlanSet{linkedPlanSet()}
	sets[0].Resolution.Reason = &reason
	items := []restore.RestoreResult{linkedRestoreItem()}
	fields := NewConfigResultFields(sets, items)

	sets[0].Source.Instance.RawVersion = "mutated source"
	sets[0].TargetInstances[0].ID = "mutated target"
	sets[0].Resolution.MigrationPath[0] = "mutated path"
	sets[0].Resolution.ResolvedTargets[0] = "mutated resolved target"
	reason = planner.ReasonBackupFailed
	items[0].Warnings[0] = "mutated warning"
	items[0].CaptureID = "mutated capture"

	resolution := fields.ConfigResolutions[0]
	if resolution.SourceInstance.RawVersion != "25.0" {
		t.Fatalf("source instance aliased caller input: %q", resolution.SourceInstance.RawVersion)
	}
	if resolution.TargetCandidates[0].ID != "photoshop-2025" {
		t.Fatalf("target candidates aliased caller input: %q", resolution.TargetCandidates[0].ID)
	}
	if resolution.MigrationPath[0] != "g1" {
		t.Fatalf("migration path aliased caller input: %q", resolution.MigrationPath[0])
	}
	if resolution.ResolvedTargets[0] != "settings/preferences.json" {
		t.Fatalf("resolved targets aliased caller input: %q", resolution.ResolvedTargets[0])
	}
	if resolution.Reason == nil || *resolution.Reason != planner.ReasonCommitFailed {
		t.Fatalf("reason aliased caller input: %v", resolution.Reason)
	}
	if fields.RestoreItems[0].Warnings[0] != "sensitive target reviewed" {
		t.Fatalf("restore warnings aliased caller input: %q", fields.RestoreItems[0].Warnings[0])
	}
	if fields.RestoreItems[0].CaptureID != "capture-1" {
		t.Fatalf("restore item aliased caller input: %q", fields.RestoreItems[0].CaptureID)
	}
}

func linkedPlanSet() planner.PlanSet {
	return planner.PlanSet{
		Source: planner.SourceCapture{
			CaptureID:             "capture-1",
			ModuleID:              "apps.photoshop",
			ConfigSetID:           "preferences",
			Generation:            "g1",
			GenerationFingerprint: "sha256:g1",
			ModuleRevision:        "capture-rev",
			Instance: planner.SourceInstance{
				ID:                "photoshop-2024",
				DetectorID:        "creative-cloud",
				RawVersion:        "25.0",
				NormalizedVersion: "25.0.0",
				Evidence: planner.InstanceEvidence{
					Type:  "application",
					AppID: "photoshop",
				},
			},
		},
		TargetInstances: []planner.TargetInstance{{
			ID:                    "photoshop-2025",
			ModuleID:              "apps.photoshop",
			DetectorID:            "creative-cloud",
			RawVersion:            "26.0",
			NormalizedVersion:     "26.0.0",
			Generation:            "g2",
			GenerationFingerprint: "sha256:g2",
			ModuleRevision:        "restore-rev",
			Root:                  `C:\Users\person\AppData\Roaming\Adobe`,
			Evidence: planner.InstanceEvidence{
				Type:  "application",
				AppID: "photoshop",
			},
		}},
		Resolution: planner.ConfigResolution{
			TargetInstanceID:      "photoshop-2025",
			TargetGeneration:      "g2",
			Resolution:            planner.ResolutionMigrate,
			MigrationPath:         []string{"g1", "g2"},
			RestoreModuleRevision: "restore-rev",
			ResolvedTargets:       []string{"settings/preferences.json"},
			Status:                planner.StatusRestored,
		},
	}
}

func linkedRestoreItem() restore.RestoreResult {
	return restore.RestoreResult{
		ID:                  "copy:preferences",
		Source:              "configs/capture-1/preferences.json",
		Target:              "settings/preferences.json",
		Status:              "restored",
		BackupPath:          "backups/preferences.json",
		BackupCreated:       true,
		TargetExistedBefore: true,
		Warnings:            []string{"sensitive target reviewed"},
		RestoreType:         "copy",
		CaptureID:           "capture-1",
		ConfigSetID:         "preferences",
		TargetInstanceID:    "photoshop-2025",
		SourceGeneration:    "g1",
		TargetGeneration:    "g2",
	}
}

func assertExactJSON(t *testing.T, value any, want string) {
	t.Helper()
	got, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if string(got) != want {
		t.Fatalf("unexpected JSON\n got: %s\nwant: %s", got, want)
	}
}
