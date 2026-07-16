// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

var (
	_ fmt.Stringer = ResolutionDirect
	_ fmt.Stringer = StatusPlanned
	_ fmt.Stringer = ReasonUnknownGeneration
)

func TestConfigResolutionEnums_UseLockedValues(t *testing.T) {
	resolutions := []Resolution{
		ResolutionDirect,
		ResolutionMigrate,
		ResolutionIncompatible,
		ResolutionUnknown,
		ResolutionLegacyUnverified,
	}
	wantResolutions := []string{"direct", "migrate", "incompatible", "unknown", "legacy_unverified"}
	if got := stringifyValues(resolutions); !reflect.DeepEqual(got, wantResolutions) {
		t.Fatalf("resolution strings = %#v, want %#v", got, wantResolutions)
	}

	statuses := []TerminalStatus{
		StatusPlanned,
		StatusRestored,
		StatusSkipped,
		StatusFailed,
		StatusRolledBack,
		StatusRollbackFailed,
	}
	wantStatuses := []string{"planned", "restored", "skipped", "failed", "rolled_back", "rollback_failed"}
	if got := stringifyValues(statuses); !reflect.DeepEqual(got, wantStatuses) {
		t.Fatalf("terminal status strings = %#v, want %#v", got, wantStatuses)
	}

	reasons := []ResolutionReason{
		ReasonUnknownGeneration,
		ReasonAmbiguousGeneration,
		ReasonDowngradeUnsupported,
		ReasonMigrationPathMissing,
		ReasonAmbiguousTargetInstance,
		ReasonTargetNotDetected,
		ReasonMappedTargetNotDetected,
		ReasonMappedTargetIncompatible,
		ReasonTargetCollision,
		ReasonPayloadIntegrityFailed,
		ReasonUnsupportedModuleSchema,
		ReasonCatalogModuleMissing,
		ReasonConfigSetMissing,
		ReasonSourceGenerationUnknown,
		ReasonSourceGenerationDefinitionChanged,
		ReasonAppRunning,
		ReasonRecoveryRequired,
		ReasonRestoreFiltered,
		ReasonRestoreNotEnabled,
		ReasonTargetDetectionFailed,
		ReasonStagingValidationFailed,
		ReasonBackupFailed,
		ReasonJournalIntentFailed,
		ReasonCommitFailed,
		ReasonTargetValidationFailed,
		ReasonJournalCompletionFailed,
		ReasonAlreadyUpToDate,
	}
	wantReasons := []string{
		"unknown_generation",
		"ambiguous_generation",
		"downgrade_unsupported",
		"migration_path_missing",
		"ambiguous_target_instance",
		"target_not_detected",
		"mapped_target_not_detected",
		"mapped_target_incompatible",
		"target_collision",
		"payload_integrity_failed",
		"unsupported_module_schema",
		"catalog_module_missing",
		"config_set_missing",
		"source_generation_unknown",
		"source_generation_definition_changed",
		"app_running",
		"recovery_required",
		"restore_filtered",
		"restore_not_enabled",
		"target_detection_failed",
		"staging_validation_failed",
		"backup_failed",
		"journal_intent_failed",
		"commit_failed",
		"target_validation_failed",
		"journal_completion_failed",
		"already_up_to_date",
	}
	if got := stringifyValues(reasons); !reflect.DeepEqual(got, wantReasons) {
		t.Fatalf("reason strings = %#v, want %#v", got, wantReasons)
	}
}

func TestConfigResolutionJSON_KeepsReasonAndCollectionsStable(t *testing.T) {
	resolution := ProjectConfigResolution(PlanSet{
		Source: SourceCapture{
			CaptureID:   "capture-a",
			ModuleID:    "apps.example",
			ConfigSetID: "preferences",
			Instance: SourceInstance{
				ID:                "source-a",
				DetectorID:        "installed-package",
				RawVersion:        "1.5",
				NormalizedVersion: "1.5",
				Evidence:          InstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.Example"},
			},
			Generation:            "g1",
			GenerationFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModuleRevision:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		TargetInstances: []TargetInstance{{
			ID:                    "target-a",
			ModuleID:              "apps.example",
			DetectorID:            "installed-package",
			RawVersion:            "1.5",
			NormalizedVersion:     "1.5",
			Evidence:              InstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.Example"},
			Generation:            "g1",
			GenerationFingerprint: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			ModuleRevision:        "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Root:                  `C:\private\target`,
		}},
		Resolution: ConfigResolution{
			TargetInstanceID:      "target-a",
			TargetGeneration:      "g1",
			Resolution:            ResolutionDirect,
			RestoreModuleRevision: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Status:                StatusRestored,
		},
	})

	encoded, err := json.Marshal(resolution)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"captureId":"capture-a","moduleId":"apps.example","configSetId":"preferences","sourceInstance":{"id":"source-a","detectorId":"installed-package","rawVersion":"1.5","normalizedVersion":"1.5","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"}},"sourceInstanceId":"source-a","targetInstanceId":"target-a","targetCandidates":[{"id":"target-a","moduleId":"apps.example","detectorId":"installed-package","rawVersion":"1.5","normalizedVersion":"1.5","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"},"targetGeneration":"g1","targetGenerationFingerprint":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","restoreModuleRevision":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}],"sourceGeneration":"g1","sourceGenerationFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","targetGeneration":"g1","resolution":"direct","reason":null,"migrationPath":[],"captureModuleRevision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","restoreModuleRevision":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","resolvedTargets":[],"status":"restored","label":"Compatible","message":"Settings are compatible with the selected target.","remediation":null}`
	if got := string(encoded); got != want {
		t.Fatalf("ConfigResolution JSON:\n got: %s\nwant: %s", got, want)
	}
}

func TestConfigPlanJSON_PreservesNormalizedSourceAndTargetEvidence(t *testing.T) {
	reason := ReasonMigrationPathMissing
	plan := ConfigPlan{
		Sets: []PlanSet{{
			Source: SourceCapture{
				CaptureID:   "capture-a",
				ModuleID:    "apps.example",
				ConfigSetID: "preferences",
				Instance: SourceInstance{
					ID:                "source-a",
					DetectorID:        "installed-package",
					RawVersion:        "v27.04",
					NormalizedVersion: "27.4",
					Evidence: InstanceEvidence{
						Type:    "package",
						Backend: "winget",
						Ref:     "Vendor.Example",
					},
				},
				Generation:            "g1",
				GenerationFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ModuleRevision:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
			TargetInstances: []TargetInstance{{
				ID:                "target-a",
				ModuleID:          "apps.example",
				DetectorID:        "installed-package",
				RawVersion:        "29.1",
				NormalizedVersion: "29.1",
				Evidence: InstanceEvidence{
					Type:    "package",
					Backend: "winget",
					Ref:     "Vendor.Example",
				},
				Generation:            "g2",
				GenerationFingerprint: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				ModuleRevision:        "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			}},
			Resolution: ConfigResolution{
				TargetInstanceID:      "target-a",
				TargetGeneration:      "g2",
				Resolution:            ResolutionIncompatible,
				Reason:                &reason,
				RestoreModuleRevision: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				Status:                StatusSkipped,
			},
		}},
	}
	plan.Sets[0].Resolution = ProjectConfigResolution(plan.Sets[0])

	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sets":[{"source":{"captureId":"capture-a","moduleId":"apps.example","configSetId":"preferences","sourceInstance":{"id":"source-a","detectorId":"installed-package","rawVersion":"v27.04","normalizedVersion":"27.4","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"}},"sourceGeneration":"g1","sourceGenerationFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","captureModuleRevision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"targetInstances":[{"id":"target-a","moduleId":"apps.example","detectorId":"installed-package","rawVersion":"29.1","normalizedVersion":"29.1","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"},"targetGeneration":"g2","targetGenerationFingerprint":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","restoreModuleRevision":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}],"resolution":{"captureId":"capture-a","moduleId":"apps.example","configSetId":"preferences","sourceInstance":{"id":"source-a","detectorId":"installed-package","rawVersion":"v27.04","normalizedVersion":"27.4","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"}},"sourceInstanceId":"source-a","targetInstanceId":"target-a","targetCandidates":[{"id":"target-a","moduleId":"apps.example","detectorId":"installed-package","rawVersion":"29.1","normalizedVersion":"29.1","evidence":{"type":"package","backend":"winget","ref":"Vendor.Example"},"targetGeneration":"g2","targetGenerationFingerprint":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","restoreModuleRevision":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}],"sourceGeneration":"g1","sourceGenerationFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","targetGeneration":"g2","resolution":"incompatible","reason":"migration_path_missing","migrationPath":[],"captureModuleRevision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","restoreModuleRevision":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","resolvedTargets":[],"status":"skipped","label":"Not supported","message":"No supported migration path exists between the source and target generations.","remediation":"Choose a compatible target version or add a reviewed forward migration to the module catalog."}}],"summary":{"total":0,"direct":0,"migrate":0,"incompatible":0,"unknown":0,"legacyUnverified":0,"selected":0,"skipped":0,"failed":0}}`
	if got := string(encoded); got != want {
		t.Fatalf("ConfigPlan JSON:\n got: %s\nwant: %s", got, want)
	}
}

func TestSummarizeConfigResolutions_UsesLockedStatusAccounting(t *testing.T) {
	resolutions := []ConfigResolution{
		{Resolution: ResolutionDirect, Status: StatusPlanned},
		{Resolution: ResolutionMigrate, Status: StatusRestored},
		{Resolution: ResolutionIncompatible, Status: StatusSkipped},
		{Resolution: ResolutionUnknown, Status: StatusFailed},
		{Resolution: ResolutionLegacyUnverified, Status: StatusRolledBack},
		{Resolution: ResolutionDirect, Status: StatusRollbackFailed},
	}

	got := SummarizeConfigResolutions(resolutions)
	want := ConfigResolutionSummary{
		Total:            6,
		Direct:           2,
		Migrate:          1,
		Incompatible:     1,
		Unknown:          1,
		LegacyUnverified: 1,
		Selected:         5,
		Skipped:          1,
		Failed:           3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
}

func stringifyValues[T fmt.Stringer](values []T) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}
