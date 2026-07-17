// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestProjectLegacyConfigRestoresV1ProducesExactEnvelopeAndLinkedItems(t *testing.T) {
	t.Parallel()

	moduleID := "apps.legacy"
	captureID := bundle.LegacyCaptureID(moduleID)
	inputs := buildLegacyTestInputs(t, &manifest.Manifest{Version: 1, Restore: []manifest.RestoreEntry{{
		Type: "copy", Source: "configs/legacy/settings.json", Target: "settings.json", FromModule: moduleID,
	}}}, "")
	results := []restore.RestoreResult{{
		ID: "legacy-copy", Source: "configs/legacy/settings.json", Target: "settings.json", Status: "restored",
		Warnings: []string{"legacy warning"}, TargetInstanceID: "fabricated-target",
		SourceGeneration: "fabricated-source", TargetGeneration: "fabricated-target-generation",
	}}

	projection, err := projectLegacyConfigRestores(inputs, true, configRestoreLegacyExecution{
		ResultsByCaptureID: map[string][]restore.RestoreResult{captureID: results},
	})
	if err != nil {
		t.Fatalf("projectLegacyConfigRestores: %v", err)
	}
	fields := NewConfigResultFields(projection.Plan.Sets, projection.RestoreItems)
	want := fmt.Sprintf(
		`{"configResolutions":[{"captureId":%q,"moduleId":"apps.legacy","configSetId":"legacy","targetCandidates":[],"resolution":"legacy_unverified","reason":null,"migrationPath":[],"resolvedTargets":[],"status":"restored","label":"Compatibility unknown","message":"These settings predate compatibility checks, so compatibility could not be verified.","remediation":"Review the legacy settings and enable restore only if you trust this backup."}],"configResolutionSummary":{"total":1,"direct":0,"migrate":0,"incompatible":0,"unknown":0,"legacyUnverified":1,"selected":1,"skipped":0,"failed":0},"restoreItems":[{"id":"legacy-copy","source":"configs/legacy/settings.json","target":"settings.json","status":"restored","backupCreated":false,"targetExistedBefore":false,"warnings":["legacy warning"],"captureId":%q,"configSetId":"legacy"}]}`,
		captureID, captureID,
	)
	assertLegacyExactJSON(t, fields, want)

	results[0].Warnings[0] = "mutated"
	results[0].CaptureID = "mutated"
	if projection.RestoreItems[0].Warnings[0] != "legacy warning" || projection.RestoreItems[0].CaptureID != captureID {
		t.Fatalf("linked restore items alias caller results: %+v", projection.RestoreItems)
	}
}

func TestProjectLegacyConfigRestoresMixedV2AdaptsOnlyExplicitLegacyLane(t *testing.T) {
	t.Parallel()

	manifestDir := t.TempDir()
	legacyModuleID := "apps.legacy"
	legacyCaptureID := bundle.LegacyCaptureID(legacyModuleID)
	legacyRoot := path.Join("configs", legacyCaptureID)
	generationRoot := path.Join("configs", "capture-generation")
	for _, root := range []string{legacyRoot, generationRoot} {
		if err := os.MkdirAll(filepath.Join(manifestDir, filepath.FromSlash(root)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	inputs := buildLegacyTestInputs(t, &manifest.Manifest{
		Version: 2,
		ConfigCaptures: []manifest.ConfigCapture{{
			CaptureID: "capture-generation", ModuleID: "apps.generation", ConfigSetID: "preferences",
			SourceInstance: manifest.ConfigSourceInstance{
				ID: "source", DetectorID: "installed", RawVersion: "2.0", NormalizedVersion: "2",
				Evidence: &manifest.ConfigSourceInstanceEvidence{Type: "package", Backend: "winget", Ref: "Vendor.Generation"},
			},
			SourceGeneration: "g2", SourceGenerationFingerprint: strings.Repeat("a", 64),
			CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 2, ContentHash: strings.Repeat("b", 64)},
			PayloadRoot:   generationRoot, PayloadManifest: []manifest.PayloadManifestEntry{},
		}},
		LegacyConfigLanes: []manifest.LegacyConfigLane{{
			CaptureID: legacyCaptureID, ModuleID: legacyModuleID, ModuleSchemaVersion: 1, PayloadRoot: legacyRoot,
		}},
		Restore: []manifest.RestoreEntry{{
			Type: "copy", Source: "./" + path.Join(legacyRoot, "settings.json"), Target: "legacy-target",
			FromModule: legacyModuleID, LegacyCaptureID: legacyCaptureID,
		}},
	}, filepath.Join(manifestDir, "manifest.jsonc"))

	projection, err := projectLegacyConfigRestores(inputs, true, configRestoreLegacyExecution{
		DryRun: true,
		ResultsByCaptureID: map[string][]restore.RestoreResult{
			legacyCaptureID: {{ID: "preview", Status: "restored"}},
		},
	})
	if err != nil {
		t.Fatalf("projectLegacyConfigRestores: %v", err)
	}
	if len(projection.Plan.Sets) != 1 || projection.Plan.Sets[0].Source.CaptureID != legacyCaptureID ||
		projection.Plan.Sets[0].Source.ModuleID != legacyModuleID ||
		projection.Plan.Sets[0].Resolution.Status != planner.StatusPlanned {
		t.Fatalf("mixed-v2 legacy projection = %+v", projection.Plan)
	}
	assertLegacyResolutionHasNoFabricatedFacts(t, projection.Plan.Sets[0])
}

func TestProjectLegacyConfigRestoresFilterAndConsentOverrideSuppliedResults(t *testing.T) {
	t.Parallel()

	filtered := legacyTestLane("apps.filtered", false)
	selected := legacyTestLane("apps.selected", true)
	inputs := configRestoreInputs{
		hasConfigPayloads: true,
		generationSources: []configRestoreSource{}, legacyLanes: []configRestoreLegacyLane{selected, filtered},
		ordinaryRestores: []manifest.RestoreEntry{}, targetMappings: map[string]string{},
	}
	bogusResults := map[string][]restore.RestoreResult{
		filtered.captureID: {{ID: "ignored-filter", Status: "unknown"}},
		selected.captureID: {{ID: "ignored-consent", Status: "unknown"}},
	}

	projection, err := projectLegacyConfigRestores(inputs, false, configRestoreLegacyExecution{ResultsByCaptureID: bogusResults})
	if err != nil {
		t.Fatalf("filtered/no-consent outcomes must be ignored: %v", err)
	}
	if len(projection.Plan.Sets) != 2 || len(projection.RestoreItems) != 0 {
		t.Fatalf("projection = %+v items=%+v", projection.Plan, projection.RestoreItems)
	}
	byModule := legacySetsByModule(projection.Plan.Sets)
	assertLegacyStatusReason(t, byModule["apps.filtered"], planner.StatusSkipped, planner.ReasonRestoreFiltered)
	assertLegacyStatusReason(t, byModule["apps.selected"], planner.StatusSkipped, planner.ReasonRestoreNotEnabled)
}

func TestProjectLegacyConfigRestoresDerivesTerminalStatusFromConcreteResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dryRun     bool
		results    []restore.RestoreResult
		wantStatus planner.TerminalStatus
		wantReason *planner.ResolutionReason
	}{
		{name: "live restored", results: []restore.RestoreResult{{Status: "restored"}}, wantStatus: planner.StatusRestored},
		{name: "live failed before mutation", results: []restore.RestoreResult{{Status: "failed"}}, wantStatus: planner.StatusFailed},
		{name: "dry run planned", dryRun: true, results: []restore.RestoreResult{{Status: "restored"}}, wantStatus: planner.StatusPlanned},
		{name: "dry run failed", dryRun: true, results: []restore.RestoreResult{{Status: "restored"}, {Status: "failed"}}, wantStatus: planner.StatusFailed},
		{name: "live partial mutation is rollback failed", results: []restore.RestoreResult{{Status: "restored"}, {Status: "failed"}}, wantStatus: planner.StatusRollbackFailed},
		{name: "all up to date", results: []restore.RestoreResult{{Status: "skipped_up_to_date"}, {Status: "skipped_up_to_date"}}, wantStatus: planner.StatusSkipped, wantReason: legacyReasonPtr(planner.ReasonAlreadyUpToDate)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lane := legacyTestLane("apps.example", true)
			projection, err := projectLegacyConfigRestores(configRestoreInputs{
				hasConfigPayloads: true,
				generationSources: []configRestoreSource{}, legacyLanes: []configRestoreLegacyLane{lane},
				ordinaryRestores: []manifest.RestoreEntry{}, targetMappings: map[string]string{},
			}, true, configRestoreLegacyExecution{
				DryRun: tt.dryRun, ResultsByCaptureID: map[string][]restore.RestoreResult{lane.captureID: tt.results},
			})
			if err != nil {
				t.Fatalf("projectLegacyConfigRestores: %v", err)
			}
			set := projection.Plan.Sets[0]
			if set.Resolution.Status != tt.wantStatus || !legacyReasonsEqual(set.Resolution.Reason, tt.wantReason) {
				t.Fatalf("status/reason = %q/%v, want %q/%v", set.Resolution.Status, set.Resolution.Reason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

func TestProjectLegacyConfigRestoresRequiresExplicitNonEmptyEnabledOutcomes(t *testing.T) {
	t.Parallel()

	lane := legacyTestLane("apps.required", true)
	inputs := configRestoreInputs{
		hasConfigPayloads: true,
		generationSources: []configRestoreSource{}, legacyLanes: []configRestoreLegacyLane{lane},
		ordinaryRestores: []manifest.RestoreEntry{}, targetMappings: map[string]string{},
	}
	for _, results := range []map[string][]restore.RestoreResult{
		nil,
		{lane.captureID: {}},
	} {
		if _, err := projectLegacyConfigRestores(inputs, true, configRestoreLegacyExecution{ResultsByCaptureID: results}); err == nil {
			t.Fatalf("results %#v did not fail", results)
		}
	}
}

func TestProjectLegacyConfigRestoresRejectsUnknownOutcomesAndNonCanonicalLaneIdentity(t *testing.T) {
	t.Parallel()

	lane := legacyTestLane("apps.example", true)
	inputs := configRestoreInputs{
		hasConfigPayloads: true,
		generationSources: []configRestoreSource{}, legacyLanes: []configRestoreLegacyLane{lane},
		ordinaryRestores: []manifest.RestoreEntry{}, targetMappings: map[string]string{},
	}
	if _, err := projectLegacyConfigRestores(inputs, true, configRestoreLegacyExecution{ResultsByCaptureID: map[string][]restore.RestoreResult{
		lane.captureID: {{Status: "restored"}}, "legacy-unknown": {{Status: "restored"}},
	}}); err == nil {
		t.Fatal("unknown outcome capture ID was accepted")
	}

	inputs.legacyLanes[0].captureID = "legacy-noncanonical"
	if _, err := projectLegacyConfigRestores(inputs, false, configRestoreLegacyExecution{}); err == nil {
		t.Fatal("non-canonical legacy lane identity was accepted")
	}
}

func TestProjectLegacyConfigRestoresAnonymousOnlyInputRemainsConfigFree(t *testing.T) {
	t.Parallel()

	inputs := buildLegacyTestInputs(t, &manifest.Manifest{Version: 1, Restore: []manifest.RestoreEntry{{
		Type: "copy", Source: "inline", Target: "inline-target",
	}}}, "")
	projection, err := projectLegacyConfigRestores(inputs, true, configRestoreLegacyExecution{})
	if err != nil {
		t.Fatalf("projectLegacyConfigRestores: %v", err)
	}
	if inputs.hasConfigPayloads || len(projection.Plan.Sets) != 0 || projection.Plan.Sets == nil ||
		len(projection.RestoreItems) != 0 || projection.RestoreItems == nil {
		t.Fatalf("anonymous-only projection invented config state: inputs=%+v projection=%+v", inputs, projection)
	}
	result := ApplyResult{}
	if inputs.hasConfigPayloads {
		result.ConfigResultFields = NewConfigResultFields(projection.Plan.Sets, projection.RestoreItems)
	}
	assertLegacyExactJSON(t, result,
		`{"dryRun":false,"manifest":{"path":"","name":"","hash":""},"summary":{"total":0,"success":0,"skipped":0,"failed":0},"actions":null}`)
}

func TestProjectLegacyConfigRestoresOrdersLanesAndItemsDeterministically(t *testing.T) {
	t.Parallel()

	lanes := []configRestoreLegacyLane{
		legacyTestLane("apps.zeta", true),
		legacyTestLane("apps.alpha", true),
		legacyTestLane("apps.middle", true),
	}
	results := make(map[string][]restore.RestoreResult, len(lanes))
	wantIDs := make([]string, len(lanes))
	for index, lane := range lanes {
		results[lane.captureID] = []restore.RestoreResult{{ID: lane.moduleID, Status: "restored"}}
		wantIDs[index] = lane.captureID
	}
	sort.Strings(wantIDs)
	projection, err := projectLegacyConfigRestores(configRestoreInputs{
		hasConfigPayloads: true,
		generationSources: []configRestoreSource{}, legacyLanes: lanes,
		ordinaryRestores: []manifest.RestoreEntry{}, targetMappings: map[string]string{},
	}, true, configRestoreLegacyExecution{ResultsByCaptureID: results})
	if err != nil {
		t.Fatalf("projectLegacyConfigRestores: %v", err)
	}
	for index, wantID := range wantIDs {
		if projection.Plan.Sets[index].Source.CaptureID != wantID || projection.RestoreItems[index].CaptureID != wantID {
			t.Fatalf("order at %d = set %q item %q, want %q", index,
				projection.Plan.Sets[index].Source.CaptureID, projection.RestoreItems[index].CaptureID, wantID)
		}
	}
}

func buildLegacyTestInputs(t *testing.T, value *manifest.Manifest, manifestPath string) configRestoreInputs {
	t.Helper()
	if manifestPath == "" {
		manifestPath = filepath.Join(t.TempDir(), "manifest.jsonc")
	}
	inputs, envErr := buildConfigRestoreInputs(configRestoreBuildRequest{Manifest: value, ManifestPath: manifestPath})
	if envErr != nil {
		t.Fatalf("buildConfigRestoreInputs: %+v", envErr)
	}
	return inputs
}

func legacyTestLane(moduleID string, selected bool) configRestoreLegacyLane {
	return configRestoreLegacyLane{
		captureID: bundle.LegacyCaptureID(moduleID), moduleID: moduleID, configSetID: "legacy",
		restoreEntries: []manifest.RestoreEntry{{Type: "copy", FromModule: moduleID}}, selected: selected,
	}
}

func legacySetsByModule(sets []planner.PlanSet) map[string]planner.PlanSet {
	result := make(map[string]planner.PlanSet, len(sets))
	for _, set := range sets {
		result[set.Source.ModuleID] = set
	}
	return result
}

func assertLegacyStatusReason(t *testing.T, set planner.PlanSet, status planner.TerminalStatus, reason planner.ResolutionReason) {
	t.Helper()
	if set.Resolution.Status != status || set.Resolution.Reason == nil || *set.Resolution.Reason != reason {
		t.Fatalf("status/reason = %q/%v, want %q/%q; set=%+v", set.Resolution.Status, set.Resolution.Reason, status, reason, set)
	}
}

func assertLegacyResolutionHasNoFabricatedFacts(t *testing.T, set planner.PlanSet) {
	t.Helper()
	encoded, err := json.Marshal(planner.ProjectConfigResolution(set))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"sourceInstance", "targetInstanceId", "sourceGeneration", "targetGeneration",
		"Fingerprint", "ModuleRevision", "fabricated", "rawVersion", "normalizedVersion",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("legacy projection contains %q: %s", forbidden, encoded)
		}
	}
	if len(set.TargetInstances) != 0 || set.Resolution.TargetCandidates == nil ||
		set.Resolution.MigrationPath == nil || set.Resolution.ResolvedTargets == nil {
		t.Fatalf("legacy internal collections are nil or populated: %+v", set)
	}
}

func legacyReasonPtr(value planner.ResolutionReason) *planner.ResolutionReason { return &value }

func legacyReasonsEqual(left, right *planner.ResolutionReason) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func assertLegacyExactJSON(t *testing.T, value any, want string) {
	t.Helper()
	got, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if string(got) != want {
		t.Fatalf("unexpected JSON\n got: %s\nwant: %s", got, want)
	}
}
