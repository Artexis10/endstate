// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestProjectConfigResolution_CopiesPortableFactsAndIsIdempotent(t *testing.T) {
	reason := ReasonCommitFailed
	set := PlanSet{
		Source: SourceCapture{
			CaptureID:      "capture-a",
			ModuleID:       "apps.example",
			ConfigSetID:    "preferences",
			ModuleRevision: "capture-revision",
			Instance: SourceInstance{
				ID:                "source-a",
				DetectorID:        "source-detector",
				RawVersion:        "1.0",
				NormalizedVersion: "1",
				Evidence:          InstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.App"},
			},
			Generation:            "g1",
			GenerationFingerprint: "fingerprint-g1",
		},
		TargetInstances: []TargetInstance{{
			ID:                    "target-a",
			ModuleID:              "apps.example",
			DetectorID:            "target-detector",
			RawVersion:            "2.0",
			NormalizedVersion:     "2",
			Evidence:              InstanceEvidence{Type: "path", Ref: "portable-ref"},
			Generation:            "g2",
			GenerationFingerprint: "fingerprint-g2",
			ModuleRevision:        "restore-revision",
			Root:                  `C:\private\target`,
		}},
		Resolution: ConfigResolution{
			TargetInstanceID:      "target-a",
			TargetGeneration:      "g2",
			Resolution:            ResolutionMigrate,
			Reason:                &reason,
			MigrationPath:         []string{"g1", "g2"},
			RestoreModuleRevision: "restore-revision",
			ResolvedTargets:       []string{"portable-display"},
			Status:                StatusRolledBack,
		},
	}

	got := ProjectConfigResolution(set)
	if got.CaptureID != set.Source.CaptureID || got.ModuleID != set.Source.ModuleID ||
		got.ConfigSetID != set.Source.ConfigSetID || got.SourceGeneration != set.Source.Generation ||
		got.SourceGenerationFingerprint != set.Source.GenerationFingerprint ||
		got.CaptureModuleRevision != set.Source.ModuleRevision {
		t.Fatalf("source facts were not projected: %+v", got)
	}
	if got.SourceInstance == nil || got.SourceInstanceID != "source-a" || !reflect.DeepEqual(*got.SourceInstance, set.Source.Instance) {
		t.Fatalf("source instance = %+v/%q, want defensive source-a copy", got.SourceInstance, got.SourceInstanceID)
	}
	if len(got.TargetCandidates) != 1 || got.TargetCandidates[0].Root != "" {
		t.Fatalf("target candidates = %+v, want one candidate without host root", got.TargetCandidates)
	}
	if got.Label != "Will be upgraded" || got.Message != "The settings transaction failed while writing the target configuration." ||
		got.Remediation == nil || *got.Remediation != "Review the target write failure, resolve its cause, and retry." {
		t.Fatalf("presentation = %q/%q/%v", got.Label, got.Message, got.Remediation)
	}

	set.Source.Instance.RawVersion = "mutated-source"
	set.TargetInstances[0].RawVersion = "mutated-target"
	set.Resolution.MigrationPath[0] = "mutated-path"
	set.Resolution.ResolvedTargets[0] = "mutated-resolved"
	if got.SourceInstance.RawVersion != "1.0" || got.TargetCandidates[0].RawVersion != "2.0" ||
		got.MigrationPath[0] != "g1" || got.ResolvedTargets[0] != "portable-display" {
		t.Fatalf("input mutation leaked into projection: %+v", got)
	}

	stableSet := set
	stableSet.Source.Instance = *got.SourceInstance
	stableSet.TargetInstances = []TargetInstance{{
		ID:                    got.TargetCandidates[0].ID,
		ModuleID:              got.TargetCandidates[0].ModuleID,
		DetectorID:            got.TargetCandidates[0].DetectorID,
		RawVersion:            got.TargetCandidates[0].RawVersion,
		NormalizedVersion:     got.TargetCandidates[0].NormalizedVersion,
		Evidence:              got.TargetCandidates[0].Evidence,
		Generation:            got.TargetCandidates[0].Generation,
		GenerationFingerprint: got.TargetCandidates[0].GenerationFingerprint,
		ModuleRevision:        got.TargetCandidates[0].ModuleRevision,
	}}
	stableSet.Resolution = got
	if twice := ProjectConfigResolution(stableSet); !reflect.DeepEqual(twice, got) {
		t.Fatalf("projection is not idempotent:\n first=%+v\nsecond=%+v", got, twice)
	}
}

func TestProjectConfigResolution_NormalizesLegacyWireShapeWithoutFabricatingInstances(t *testing.T) {
	projected := ProjectConfigResolution(PlanSet{
		Source: SourceCapture{
			CaptureID:             "legacy-capture",
			ModuleID:              "apps.legacy",
			ConfigSetID:           "legacy",
			Instance:              SourceInstance{ID: "fabricated-source"},
			Generation:            "fabricated-g1",
			GenerationFingerprint: "fabricated-fingerprint",
			ModuleRevision:        "fabricated-capture-revision",
		},
		TargetInstances: []TargetInstance{{
			ID:             "fabricated-target",
			Generation:     "fabricated-g2",
			ModuleRevision: "fabricated-restore-revision",
		}},
		Resolution: ConfigResolution{
			SourceInstanceID:            "stale-source",
			TargetInstanceID:            "stale-target",
			SourceGeneration:            "stale-g1",
			SourceGenerationFingerprint: "stale-fingerprint",
			TargetGeneration:            "stale-g2",
			Resolution:                  ResolutionLegacyUnverified,
			MigrationPath:               []string{"stale-g1", "stale-g2"},
			CaptureModuleRevision:       "stale-capture-revision",
			RestoreModuleRevision:       "stale-restore-revision",
			ResolvedTargets:             []string{"legacy-target"},
			Status:                      StatusSkipped,
		},
	})

	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"captureId":"legacy-capture","moduleId":"apps.legacy","configSetId":"legacy","targetCandidates":[],"resolution":"legacy_unverified","reason":null,"migrationPath":[],"resolvedTargets":["legacy-target"],"status":"skipped","label":"Compatibility unknown","message":"These settings predate compatibility checks, so compatibility could not be verified.","remediation":"Review the legacy settings and enable restore only if you trust this backup."}`
	if got := string(encoded); got != want {
		t.Fatalf("legacy projection JSON:\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(string(encoded), "sourceInstance") || strings.Contains(string(encoded), "targetInstanceId") ||
		strings.Contains(string(encoded), "sourceGeneration") || strings.Contains(string(encoded), "targetGeneration") ||
		strings.Contains(string(encoded), "ModuleRevision") || strings.Contains(string(encoded), "fingerprint") {
		t.Fatalf("legacy projection fabricated instance/generation fields: %s", encoded)
	}
}

func TestProjectConfigResolution_TargetRootCannotCrossJSONBoundary(t *testing.T) {
	projected := ProjectConfigResolution(PlanSet{
		TargetInstances: []TargetInstance{{ID: "target-a", Root: `C:\private\target`}},
		Resolution:      ConfigResolution{Resolution: ResolutionUnknown},
	})
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "root") || strings.Contains(string(encoded), `C:\private`) {
		t.Fatalf("host root leaked into JSON: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"restoreModuleRevision":""`) {
		t.Fatalf("target candidate omitted required restoreModuleRevision: %s", encoded)
	}

	var decoded ConfigResolution
	malicious := `{"captureId":"capture-a","moduleId":"apps.example","configSetId":"preferences","targetCandidates":[{"id":"target-a","root":"C:\\injected"}],"resolution":"unknown","reason":null,"migrationPath":[],"resolvedTargets":[],"status":"skipped","label":"Compatibility unknown","message":"unknown","remediation":null}`
	if err := json.Unmarshal([]byte(malicious), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.TargetCandidates) != 1 || decoded.TargetCandidates[0].Root != "" {
		t.Fatalf("host root was unmarshalable: %+v", decoded.TargetCandidates)
	}
}

func TestProjectConfigResolution_AuthorsLockedResolutionCopy(t *testing.T) {
	tests := []struct {
		name        string
		resolution  Resolution
		source      string
		target      string
		wantLabel   string
		wantMessage string
		wantFix     *string
	}{
		{name: "direct", resolution: ResolutionDirect, wantLabel: "Compatible", wantMessage: "Settings are compatible with the selected target."},
		{name: "migrate", resolution: ResolutionMigrate, source: "g1", target: "g2", wantLabel: "Will be upgraded", wantMessage: "Settings will be upgraded from g1 to g2 before restore."},
		{name: "migrate without generations", resolution: ResolutionMigrate, wantLabel: "Will be upgraded", wantMessage: "Settings will be upgraded before restore."},
		{name: "incompatible", resolution: ResolutionIncompatible, wantLabel: "Not supported", wantMessage: "These settings cannot be restored to the selected target.", wantFix: textPointer("Choose a compatible target version or restore without these settings.")},
		{name: "unknown", resolution: ResolutionUnknown, wantLabel: "Compatibility unknown", wantMessage: "Settings compatibility could not be verified.", wantFix: textPointer("Review the compatibility details or choose another detected target.")},
		{name: "legacy", resolution: ResolutionLegacyUnverified, wantLabel: "Compatibility unknown", wantMessage: "These settings predate compatibility checks, so compatibility could not be verified.", wantFix: textPointer("Review the legacy settings and enable restore only if you trust this backup.")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProjectConfigResolution(PlanSet{Resolution: ConfigResolution{
				Resolution:       tt.resolution,
				SourceGeneration: tt.source,
				TargetGeneration: tt.target,
			}})
			if got.Label != tt.wantLabel || got.Message != tt.wantMessage || !reflect.DeepEqual(got.Remediation, tt.wantFix) {
				t.Fatalf("copy = %q/%q/%v, want %q/%q/%v", got.Label, got.Message, got.Remediation, tt.wantLabel, tt.wantMessage, tt.wantFix)
			}
		})
	}
}

func TestProjectConfigResolution_AuthorsLockedReasonCopy(t *testing.T) {
	tests := []struct {
		reason      ResolutionReason
		message     string
		remediation *string
	}{
		{ReasonUnknownGeneration, "The target version does not match a known configuration generation.", textPointer("Install or select a supported target version, or update the module catalog.")},
		{ReasonAmbiguousGeneration, "The target version matches more than one configuration generation.", textPointer("Update the module catalog so the target version matches exactly one generation.")},
		{ReasonDowngradeUnsupported, "Settings cannot be restored to an older configuration generation.", textPointer("Choose a target with the same or a newer supported configuration generation.")},
		{ReasonMigrationPathMissing, "No supported migration path exists between the source and target generations.", textPointer("Choose a compatible target version or add a reviewed forward migration to the module catalog.")},
		{ReasonAmbiguousTargetInstance, "More than one compatible target instance was detected.", textPointer("Select a target instance explicitly.")},
		{ReasonTargetNotDetected, "No target instance was detected for these settings.", textPointer("Install the application or select a detected target, then retry.")},
		{ReasonMappedTargetNotDetected, "The selected target instance is no longer detected.", textPointer("Refresh detection and select an available target instance.")},
		{ReasonMappedTargetIncompatible, "The selected target instance is not compatible with these settings.", textPointer("Select a compatible target instance.")},
		{ReasonTargetCollision, "This config set overlaps another selected restore target.", textPointer("Restore only one of the colliding config sets or change the target selection.")},
		{ReasonPayloadIntegrityFailed, "The captured settings payload failed integrity verification.", textPointer("Create a new backup or use an untampered backup.")},
		{ReasonUnsupportedModuleSchema, "The current engine cannot safely interpret this module schema.", textPointer("Update Endstate or use a module schema supported by this engine.")},
		{ReasonCatalogModuleMissing, "The current module catalog does not contain this application module.", textPointer("Install or update the module catalog, then retry.")},
		{ReasonConfigSetMissing, "The current module no longer defines this config set.", textPointer("Update the module catalog or restore without this config set.")},
		{ReasonSourceGenerationUnknown, "The current module does not recognize the captured configuration generation.", textPointer("Update the module catalog with the captured generation history.")},
		{ReasonSourceGenerationDefinitionChanged, "The captured generation fingerprint is not accepted by the current module.", textPointer("Use a catalog that accepts this historical fingerprint or create a new backup.")},
		{ReasonAppRunning, "The application must be closed before these settings can be restored.", textPointer("Close the application and retry.")},
		{ReasonRecoveryRequired, "A previous config restore requires recovery before new changes can begin.", textPointer("Review the recovery failure, restore a safe state, and retry.")},
		{ReasonRestoreFiltered, "This config set was excluded by the restore filter.", textPointer("Change the restore filter to include this module and retry.")},
		{ReasonRestoreNotEnabled, "Settings restore is not enabled for this invocation.", textPointer("Enable settings restore and retry.")},
		{ReasonTargetDetectionFailed, "Target detection failed, so compatibility could not be determined.", textPointer("Review the detection failure, resolve its cause, and retry.")},
		{ReasonStagingValidationFailed, "The staged settings failed validation before target changes began.", textPointer("Review the staging validation failure, resolve its cause, and retry.")},
		{ReasonBackupFailed, "Required target backups could not be created.", textPointer("Review the backup failure, resolve its cause, and retry.")},
		{ReasonJournalIntentFailed, "The restore journal could not be written before target changes began.", textPointer("Review the journal storage failure, resolve its cause, and retry.")},
		{ReasonCommitFailed, "The settings transaction failed while writing the target configuration.", textPointer("Review the target write failure, resolve its cause, and retry.")},
		{ReasonTargetValidationFailed, "The restored target configuration failed validation.", textPointer("Review the target validation failure before retrying.")},
		{ReasonJournalCompletionFailed, "The restore journal could not record transaction completion.", textPointer("Review the journal storage failure before retrying.")},
		{ReasonAlreadyUpToDate, "The target already has the desired settings.", nil},
	}

	for _, tt := range tests {
		t.Run(tt.reason.String(), func(t *testing.T) {
			reason := tt.reason
			got := ProjectConfigResolution(PlanSet{Resolution: ConfigResolution{
				Resolution: ResolutionDirect,
				Reason:     &reason,
			}})
			if got.Label != "Compatible" || got.Message != tt.message || !reflect.DeepEqual(got.Remediation, tt.remediation) {
				t.Fatalf("copy = %q/%q/%v, want Compatible/%q/%v", got.Label, got.Message, got.Remediation, tt.message, tt.remediation)
			}
		})
	}
}

func TestCompatibilityResolverResolveSources_ProjectsAtOutputBoundary(t *testing.T) {
	current := selectionTestModule(true)
	resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	source := selectionTestSource("g1", "", "")
	plan := resolver.ResolveSources(
		[]SourceCapture{source},
		map[string][]TargetInstance{current.ID: {selectionTestTarget("target-g2", "2.5", "2.5")}},
		nil,
	)

	got := plan.Sets[0].Resolution
	if got.SourceInstance == nil || got.SourceInstanceID != source.Instance.ID || len(got.TargetCandidates) != 1 ||
		got.Label != "Will be upgraded" || got.Message != "Settings will be upgraded from g1 to g2 before restore." || got.Remediation != nil {
		t.Fatalf("projected boundary resolution = %+v", got)
	}
}

func textPointer(value string) *string { return &value }
