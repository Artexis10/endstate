// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

type v2RebuildEvidence struct {
	Apply struct {
		Actions []struct {
			ID, Driver, Source, Ref, Name, Version, Status, Reason string
		} `json:"actions"`
		Summary                 v2ApplySummary                  `json:"summary"`
		ConfigResolutionSummary planner.ConfigResolutionSummary `json:"configResolutionSummary"`
		ConfigResolutions       []planner.ConfigResolution      `json:"configResolutions"`
		RestoreItems            []restore.RestoreResult         `json:"restoreItems"`
	} `json:"apply"`
	ConfigResolutionSummary planner.ConfigResolutionSummary `json:"configResolutionSummary"`
	ConfigResolutions       []planner.ConfigResolution      `json:"configResolutions"`
	RestoreItems            []restore.RestoreResult         `json:"restoreItems"`
	Verify                  json.RawMessage                 `json:"verify"`
}

type v2ApplySummary struct{ Total, Success, Skipped, Failed int }

func validateV2RebuildEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime, iteration int) (v2RebuildEvidence, *Failure) {
	if runtime != nil && runtime.V2Plan != nil && runtime.V2Plan.Compiled.Migration != nil {
		return validateV2MigrationRebuildEvidence(raw, events, runtime, iteration)
	}
	return validateV2DirectRebuildEvidence(raw, events, runtime, iteration)
}

func validateV2DirectRebuildEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime, iteration int) (v2RebuildEvidence, *Failure) {
	var data v2RebuildEvidence
	if runtime == nil || runtime.V2Plan == nil || iteration < 0 || iteration > 2 || json.Unmarshal(raw, &data) != nil {
		return data, fail(CodeEnvelopeContract, "rebuild", "data", "schema-v2 rebuild evidence is malformed")
	}
	if data.Apply.Summary != (v2ApplySummary{Total: 1, Skipped: 1}) || len(data.Apply.Actions) != 1 {
		return data, fail(CodeEnvelopeContract, "rebuild", "apply", "rebuild did not exercise exactly one already-present app")
	}
	action := data.Apply.Actions[0]
	expectedSource := runtime.Inventory.Source
	if expectedSource == "" && !strings.EqualFold(runtime.Inventory.Driver, "validation") {
		expectedSource = runtime.Inventory.Driver
	}
	if action.ID != runtime.Inventory.AppID || !strings.EqualFold(action.Driver, runtime.Inventory.Driver) ||
		action.Source != expectedSource || action.Ref != runtime.Inventory.Ref ||
		action.Name != runtime.Inventory.DisplayName || action.Version != runtime.Inventory.Version || action.Status != "present" || action.Reason != "already_installed" {
		return data, fail(CodeEnvelopeContract, "rebuild", "apply.actions", fmt.Sprintf("app action differs from exact validation inventory: got=%+v", action))
	}
	if !reflect.DeepEqual(data.ConfigResolutionSummary, data.Apply.ConfigResolutionSummary) ||
		!reflect.DeepEqual(data.ConfigResolutions, data.Apply.ConfigResolutions) || !reflect.DeepEqual(data.RestoreItems, data.Apply.RestoreItems) {
		return data, fail(CodeEnvelopeContract, "rebuild", "apply", "nested and outer config evidence differ")
	}
	repeat := iteration == 2
	wantSummary := planner.ConfigResolutionSummary{Total: 1, Direct: 1, Selected: 1}
	if repeat {
		wantSummary.Selected, wantSummary.Skipped = 0, 1
	}
	if data.ConfigResolutionSummary != wantSummary || len(data.ConfigResolutions) != 1 || len(data.RestoreItems) != len(runtime.V2Plan.Targets) {
		return data, fail(CodeEnvelopeContract, "rebuild", "configResolutionSummary", "direct resolution accounting is not exact")
	}
	resolution := data.ConfigResolutions[0]
	if failure := validateV2DirectResolution(resolution, runtime, repeat); failure != nil {
		return data, failure
	}
	if failure := validateV2RestoreItems(data.RestoreItems, runtime, resolution, repeat); failure != nil {
		return data, failure
	}
	if failure := validateVerifyEvidence(data.Verify, runtime, "rebuild"); failure != nil {
		return data, failure
	}
	if failure := validateV2DirectRebuildEvents(events, runtime, resolution, data.RestoreItems, repeat); failure != nil {
		return data, failure
	}
	return data, nil
}

func validateV2MigrationRebuildEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime, iteration int) (v2RebuildEvidence, *Failure) {
	var data v2RebuildEvidence
	if runtime == nil || runtime.V2Plan == nil || runtime.V2Plan.Compiled.Migration == nil || iteration < 0 || iteration > 2 || json.Unmarshal(raw, &data) != nil {
		return data, fail(CodeEnvelopeContract, "rebuild", "data", "schema-v2 migration rebuild evidence is malformed")
	}
	if data.Apply.Summary != (v2ApplySummary{Total: 1, Skipped: 1}) || len(data.Apply.Actions) != 1 {
		return data, fail(CodeEnvelopeContract, "rebuild", "apply", "migration rebuild did not exercise exactly one already-present target app")
	}
	action := data.Apply.Actions[0]
	expectedSource := runtime.Inventory.Source
	if expectedSource == "" && !strings.EqualFold(runtime.Inventory.Driver, "validation") {
		expectedSource = runtime.Inventory.Driver
	}
	if action.ID != runtime.Inventory.AppID || !strings.EqualFold(action.Driver, runtime.Inventory.Driver) ||
		action.Source != expectedSource || action.Ref != runtime.Inventory.Ref || action.Name != runtime.Inventory.DisplayName ||
		action.Version != runtime.V2Plan.Compiled.Definition.TargetVersion || action.Status != "present" || action.Reason != "already_installed" {
		return data, fail(CodeEnvelopeContract, "rebuild", "apply.actions", fmt.Sprintf("target app action differs from exact 2.5 validation inventory: got=%+v", action))
	}
	if !reflect.DeepEqual(data.ConfigResolutionSummary, data.Apply.ConfigResolutionSummary) ||
		!reflect.DeepEqual(data.ConfigResolutions, data.Apply.ConfigResolutions) || !reflect.DeepEqual(data.RestoreItems, data.Apply.RestoreItems) {
		return data, fail(CodeEnvelopeContract, "rebuild", "apply", "nested and outer migration config evidence differ")
	}
	repeat := iteration == 2
	wantSummary := planner.ConfigResolutionSummary{Total: 1, Migrate: 1, Selected: 1}
	if repeat {
		wantSummary.Selected, wantSummary.Skipped = 0, 1
	}
	if data.ConfigResolutionSummary != wantSummary || len(data.ConfigResolutions) != 1 || len(data.RestoreItems) != len(runtime.V2Plan.Targets) {
		return data, fail(CodeEnvelopeContract, "rebuild", "configResolutionSummary", "migration resolution accounting is not exact")
	}
	resolution := data.ConfigResolutions[0]
	if failure := validateV2MigrationResolution(resolution, runtime, repeat); failure != nil {
		return data, failure
	}
	if failure := validateV2RestoreItems(data.RestoreItems, runtime, resolution, repeat); failure != nil {
		return data, failure
	}
	if failure := validateVerifyEvidence(data.Verify, runtime, "rebuild"); failure != nil {
		return data, failure
	}
	if failure := validateV2MigrationRebuildEvents(events, runtime, resolution, data.RestoreItems, repeat); failure != nil {
		return data, failure
	}
	return data, nil
}

func validateV2DirectResolution(resolution planner.ConfigResolution, runtime *scenarioRuntime, repeat bool) *Failure {
	plan, instance := runtime.V2Plan, runtime.V2Plan.Instance
	wantStatus := planner.StatusRestored
	var wantReason *planner.ResolutionReason
	if repeat {
		value := planner.ReasonAlreadyUpToDate
		wantReason, wantStatus = &value, planner.StatusSkipped
	}
	if resolution.CaptureID != plan.CaptureID || resolution.ModuleID != runtime.Module.ID || resolution.ConfigSetID != plan.Compiled.Set.ID ||
		resolution.SourceInstance == nil || resolution.SourceInstanceID != instance.ID || resolution.TargetInstanceID != instance.ID ||
		resolution.SourceGeneration != plan.Compiled.Generation.ID || resolution.SourceGenerationFingerprint != plan.Compiled.Generation.Fingerprint ||
		resolution.TargetGeneration != plan.Compiled.Generation.ID || resolution.Resolution != planner.ResolutionDirect ||
		resolution.Status != wantStatus || !reflect.DeepEqual(resolution.Reason, wantReason) || len(resolution.MigrationPath) != 0 ||
		resolution.CaptureModuleRevision != runtime.Module.Revision || resolution.RestoreModuleRevision != runtime.Module.Revision || len(resolution.TargetCandidates) != 1 {
		return fail(CodeGenerationContract, "rebuild", "configResolutions[0]", "direct generation identity, status, or immutable provenance differs")
	}
	source := resolution.SourceInstance
	if source.ID != instance.ID || source.DetectorID != instance.DetectorID || source.RawVersion != instance.Version.Raw ||
		source.NormalizedVersion != instance.Version.Normalized || !exactPlannerEvidence(source.Evidence, instance) {
		return fail(CodeGenerationContract, "rebuild", "configResolutions[0].sourceInstance", "source detector evidence differs")
	}
	candidate := resolution.TargetCandidates[0]
	if candidate.ID != instance.ID || candidate.ModuleID != runtime.Module.ID || candidate.DetectorID != instance.DetectorID ||
		candidate.RawVersion != instance.Version.Raw || candidate.NormalizedVersion != instance.Version.Normalized ||
		candidate.Generation != plan.Compiled.Generation.ID || candidate.GenerationFingerprint != plan.Compiled.Generation.Fingerprint ||
		candidate.ModuleRevision != runtime.Module.Revision || !exactPlannerEvidence(candidate.Evidence, instance) {
		return fail(CodeGenerationContract, "rebuild", "configResolutions[0].targetCandidates", "selected production target instance differs")
	}
	if len(resolution.ResolvedTargets) != len(plan.Targets) {
		return fail(CodeGenerationContract, "rebuild", "configResolutions[0].resolvedTargets", "resolved target count differs")
	}
	for index, target := range plan.Targets {
		relative, err := filepath.Rel(runtime.Root, target.Resolved)
		display := "$ENDSTATE_ROOT/" + filepath.ToSlash(relative)
		if err != nil || relative == "." || strings.HasPrefix(filepath.ToSlash(relative), "../") || !strings.EqualFold(filepath.ToSlash(resolution.ResolvedTargets[index]), display) {
			return fail(CodeGenerationContract, "rebuild", "configResolutions[0].resolvedTargets", fmt.Sprintf("resolved target is not detector-derived: got=%q want=%q", resolution.ResolvedTargets[index], display))
		}
	}
	return nil
}

func validateV2MigrationResolution(resolution planner.ConfigResolution, runtime *scenarioRuntime, repeat bool) *Failure {
	plan := runtime.V2Plan
	sourceInstance, targetInstance := plan.Instance, plan.TargetInstance
	wantStatus := planner.StatusRestored
	var wantReason *planner.ResolutionReason
	if repeat {
		value := planner.ReasonAlreadyUpToDate
		wantReason, wantStatus = &value, planner.StatusSkipped
	}
	if plan.Compiled.Migration == nil ||
		resolution.CaptureID != plan.CaptureID || resolution.ModuleID != runtime.Module.ID || resolution.ConfigSetID != plan.Compiled.Set.ID ||
		resolution.SourceInstance == nil || resolution.SourceInstanceID != sourceInstance.ID || resolution.TargetInstanceID != targetInstance.ID ||
		resolution.SourceGeneration != plan.Compiled.Generation.ID || resolution.SourceGenerationFingerprint != plan.Compiled.Generation.Fingerprint ||
		resolution.TargetGeneration != plan.Compiled.TargetGeneration.ID || resolution.Resolution != planner.ResolutionMigrate ||
		resolution.Status != wantStatus || !reflect.DeepEqual(resolution.Reason, wantReason) ||
		!reflect.DeepEqual(resolution.MigrationPath, []string{plan.Compiled.Generation.ID, plan.Compiled.TargetGeneration.ID}) ||
		resolution.CaptureModuleRevision != runtime.Module.Revision || resolution.RestoreModuleRevision != runtime.Module.Revision || len(resolution.TargetCandidates) != 1 {
		return fail(CodeMigrationContract, "rebuild", "configResolutions[0]", "migration generation identity, path, status, or immutable provenance differs")
	}
	source := resolution.SourceInstance
	if source.ID != sourceInstance.ID || source.DetectorID != sourceInstance.DetectorID || source.RawVersion != sourceInstance.Version.Raw ||
		source.NormalizedVersion != sourceInstance.Version.Normalized || !exactPlannerEvidence(source.Evidence, sourceInstance) {
		return fail(CodeMigrationContract, "rebuild", "configResolutions[0].sourceInstance", "migration source detector evidence differs")
	}
	candidate := resolution.TargetCandidates[0]
	if candidate.ID != targetInstance.ID || candidate.ModuleID != runtime.Module.ID || candidate.DetectorID != targetInstance.DetectorID ||
		candidate.RawVersion != targetInstance.Version.Raw || candidate.NormalizedVersion != targetInstance.Version.Normalized ||
		candidate.Generation != plan.Compiled.TargetGeneration.ID || candidate.GenerationFingerprint != plan.Compiled.TargetGeneration.Fingerprint ||
		candidate.ModuleRevision != runtime.Module.Revision || !exactPlannerEvidence(candidate.Evidence, targetInstance) {
		return fail(CodeMigrationContract, "rebuild", "configResolutions[0].targetCandidates", "migration target detector or generation evidence differs")
	}
	if len(resolution.ResolvedTargets) != len(plan.Targets) {
		return fail(CodeMigrationContract, "rebuild", "configResolutions[0].resolvedTargets", "migration resolved target count differs")
	}
	for index, target := range plan.Targets {
		relative, err := filepath.Rel(runtime.Root, target.Resolved)
		display := "$ENDSTATE_ROOT/" + filepath.ToSlash(relative)
		if err != nil || relative == "." || strings.HasPrefix(filepath.ToSlash(relative), "../") || !strings.EqualFold(filepath.ToSlash(resolution.ResolvedTargets[index]), display) {
			return fail(CodeMigrationContract, "rebuild", "configResolutions[0].resolvedTargets", fmt.Sprintf("migration target is not detector-derived: got=%q want=%q", resolution.ResolvedTargets[index], display))
		}
	}
	return nil
}

func exactPlannerEvidence(evidence planner.InstanceEvidence, instance modules.ConfigInstance) bool {
	want := instance.Evidence
	return evidence.Type == want.Type && evidence.AppID == want.AppID && evidence.Backend == want.Backend &&
		evidence.Platform == want.Platform && evidence.Ref == want.Ref && evidence.Driver == want.Driver
}

func validateV2RestoreItems(items []restore.RestoreResult, runtime *scenarioRuntime, resolution planner.ConfigResolution, repeat bool) *Failure {
	expected := map[string]V2FixtureTarget{}
	targetGenerationID := runtime.V2Plan.Compiled.TargetGeneration.ID
	if targetGenerationID == "" {
		targetGenerationID = runtime.V2Plan.Compiled.Generation.ID
	}
	for _, target := range runtime.V2Plan.Targets {
		display, err := runtime.validationContext().DisplayPath(target.Resolved)
		if err != nil {
			return fail(CodeIsolationFailure, "rebuild", target.Coordinate, "direct target cannot be projected inside validation authority")
		}
		expected[strings.ToLower(display)] = target
	}
	for _, item := range items {
		target, ok := expected[strings.ToLower(item.Target)]
		wantSource := ""
		if ok {
			wantSource = filepath.ToSlash(strings.TrimPrefix(target.Destination, "configs/"))
		}
		if !ok || filepath.ToSlash(item.Source) != wantSource || item.ID == "" || item.RestoreType != "copy" || !item.TargetExistedBefore ||
			item.Error != "" || len(item.Warnings) != 0 || item.CaptureID != runtime.V2Plan.CaptureID || item.ConfigSetID != runtime.V2Plan.Compiled.Set.ID ||
			item.TargetInstanceID != resolution.TargetInstanceID || item.SourceGeneration != runtime.V2Plan.Compiled.Generation.ID || item.TargetGeneration != targetGenerationID {
			return fail(CodeEnvelopeContract, "rebuild", "restoreItems", fmt.Sprintf("restore item does not bind the exact schema-v2 target action: got=%+v wantSource=%q", item, wantSource))
		}
		if repeat {
			if item.Status != "skipped_up_to_date" || item.BackupCreated || item.BackupPath != "" {
				return fail(CodeEnvelopeContract, "rebuild", "restoreItems", fmt.Sprintf("repeat direct restore is not backup-free convergence: status=%q created=%t path=%q", item.Status, item.BackupCreated, item.BackupPath))
			}
		} else if item.Status != "restored" || !item.BackupCreated || item.BackupPath == "" || !v2BackupPathBindsRestoreItem(runtime, item) {
			return fail(CodeEnvelopeContract, "rebuild", "restoreItems", "direct restore lacks a physical prior snapshot")
		}
		delete(expected, strings.ToLower(item.Target))
	}
	if len(expected) != 0 {
		return fail(CodeEnvelopeContract, "rebuild", "restoreItems", "restore item omitted a production target")
	}
	return nil
}

func v2BackupPathBindsRestoreItem(runtime *scenarioRuntime, item restore.RestoreResult) bool {
	transactionRoot := filepath.Join(runtime.Root, "state", "config-restore", "v1", "transactions")
	physical, err := resolveV2SemanticPath(runtime, item.BackupPath, transactionRoot)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(transactionRoot, physical)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	itemParts := strings.Split(item.ID, ":")
	return len(parts) == 4 && v2OpaqueID(parts[0]) && parts[1] == "snapshots" && parts[3] == "prior" &&
		len(itemParts) == 3 && itemParts[0] == "config" && itemParts[1] == item.CaptureID && parts[2] == itemParts[2]
}

func validateV2DirectRebuildEvents(events []map[string]any, runtime *scenarioRuntime, resolution planner.ConfigResolution, items []restore.RestoreResult, repeat bool) *Failure {
	if len(events) < 15 {
		return fail(CodeEventContract, "rebuild", "events", "direct event stream is incomplete")
	}
	// The generic decoder has already locked field shapes, two run-id segments,
	// and phase framing. Bind the schema-v2 restore segment exactly here.
	restoreStart, restoreEnd := -1, -1
	for index, event := range events {
		if event["event"] == "phase" && event["phase"] == "restore" {
			restoreStart = index
		}
		if restoreStart >= 0 && event["event"] == "summary" && event["phase"] == "restore" {
			restoreEnd = index
			break
		}
	}
	if restoreStart < 0 || restoreEnd < 0 {
		return fail(CodeEventContract, "rebuild", "restore", "direct restore event segment is absent")
	}
	want := []string{"phase"}
	want = append(want, "config-migration", "config-migration", "config-migration", "config-migration")
	want = append(want, "config-resolution")
	if !repeat {
		want = append(want, "restore-item")
		want = append(want, "config-migration", "config-migration", "config-migration", "config-migration")
	}
	want = append(want, "restore-item", "summary")
	segment := events[restoreStart : restoreEnd+1]
	if len(segment) != len(want) {
		return fail(CodeEventContract, "rebuild", "restore", fmt.Sprintf("direct restore event count=%d want=%d", len(segment), len(want)))
	}
	for index := range want {
		if segment[index]["event"] != want[index] {
			return fail(CodeEventContract, "rebuild", "restore", "direct restore event order differs")
		}
	}
	stages := []struct {
		index         int
		stage, status string
	}{{1, "staging", "started"}, {2, "staging", "completed"}, {3, "validation", "started"}, {4, "validation", "completed"}}
	if !repeat {
		stages = append(stages, []struct {
			index         int
			stage, status string
		}{{7, "commit", "started"}, {8, "commit", "completed"}, {9, "validation", "started"}, {10, "validation", "completed"}}...)
	}
	resolutionIndex := 5
	for _, step := range stages {
		event := segment[step.index]
		_, hasFromGeneration := event["fromGeneration"]
		_, hasToGeneration := event["toGeneration"]
		if event["stage"] != step.stage || event["status"] != step.status ||
			event["captureId"] != resolution.CaptureID || event["configSetId"] != resolution.ConfigSetID ||
			hasFromGeneration || hasToGeneration || event["reason"] != nil || event["remediation"] != nil ||
			event["message"] != v2DirectMigrationMessage(step.stage, step.status, step.index > resolutionIndex) {
			return fail(CodeEventContract, "rebuild", "config-migration", "direct generation stage/status sequence differs")
		}
	}
	resolutionEvent := segment[resolutionIndex]
	if resolutionEvent["captureId"] != resolution.CaptureID || resolutionEvent["moduleId"] != runtime.Module.ID || resolutionEvent["configSetId"] != resolution.ConfigSetID ||
		resolutionEvent["sourceInstanceId"] != resolution.SourceInstanceID || resolutionEvent["targetInstanceId"] != resolution.TargetInstanceID ||
		resolutionEvent["sourceGeneration"] != resolution.SourceGeneration || resolutionEvent["sourceGenerationFingerprint"] != resolution.SourceGenerationFingerprint ||
		resolutionEvent["targetGeneration"] != resolution.TargetGeneration || resolutionEvent["resolution"] != "direct" ||
		resolutionEvent["captureModuleRevision"] != resolution.CaptureModuleRevision || resolutionEvent["restoreModuleRevision"] != resolution.RestoreModuleRevision {
		return fail(CodeEventContract, "rebuild", "config-resolution", "direct resolution event identity differs")
	}
	if !eventValueMatches(resolutionEvent["sourceInstance"], resolution.SourceInstance) || !eventValueMatches(resolutionEvent["reason"], resolution.Reason) ||
		!eventStringSliceExact(resolutionEvent["migrationPath"], nil) || !eventCandidatesMatch(resolutionEvent["targetCandidates"], resolution.TargetCandidates) ||
		resolutionEvent["label"] != resolution.Label || resolutionEvent["message"] != resolution.Message || !eventValueMatches(resolutionEvent["remediation"], resolution.Remediation) {
		return fail(CodeEventContract, "rebuild", "config-resolution", "direct resolution event path/candidates differ")
	}
	firstItem, lastItem := segment[resolutionIndex+1], segment[len(segment)-2]
	wantFirst := "restoring"
	if repeat {
		wantFirst = "skipped_up_to_date"
	}
	if !v2RestoreEventMatches(firstItem, items[0], runtime.Module.ID, wantFirst) || !repeat && !v2RestoreEventMatches(lastItem, items[0], runtime.Module.ID, "restored") {
		return fail(CodeEventContract, "rebuild", "restore-item", "restore item event lifecycle differs")
	}
	wantRestoreSuccess, wantRestoreSkipped := len(items), 0
	if repeat {
		wantRestoreSuccess, wantRestoreSkipped = 0, len(items)
	}
	if !v2SummaryEventExact(segment[len(segment)-1], "restore", len(items), wantRestoreSuccess, wantRestoreSkipped, 0) {
		return fail(CodeEventContract, "rebuild", "restore.summary", "direct restore summary accounting differs")
	}
	return validateV2VerifyEventSegments(events, runtime, true)
}

func validateV2MigrationRebuildEvents(events []map[string]any, runtime *scenarioRuntime, resolution planner.ConfigResolution, items []restore.RestoreResult, repeat bool) *Failure {
	if len(events) < 19 || len(items) != 1 {
		return fail(CodeEventContract, "rebuild", "events", "migration event stream is incomplete")
	}
	restoreStart, restoreEnd := -1, -1
	for index, event := range events {
		if event["event"] == "phase" && event["phase"] == "restore" {
			restoreStart = index
		}
		if restoreStart >= 0 && event["event"] == "summary" && event["phase"] == "restore" {
			restoreEnd = index
			break
		}
	}
	if restoreStart < 0 || restoreEnd < 0 {
		return fail(CodeEventContract, "rebuild", "restore", "migration restore event segment is absent")
	}
	want := []string{"phase"}
	for range 8 {
		want = append(want, "config-migration")
	}
	want = append(want, "config-resolution")
	if !repeat {
		want = append(want, "restore-item")
		for range 4 {
			want = append(want, "config-migration")
		}
	}
	want = append(want, "restore-item", "summary")
	segment := events[restoreStart : restoreEnd+1]
	if len(segment) != len(want) {
		return fail(CodeEventContract, "rebuild", "restore", fmt.Sprintf("migration restore event count=%d want=%d", len(segment), len(want)))
	}
	for index := range want {
		if segment[index]["event"] != want[index] {
			return fail(CodeEventContract, "rebuild", "restore", "migration restore event order differs")
		}
	}
	type migrationStep struct {
		index                  int
		stage, status, message string
		from, to               string
	}
	steps := []migrationStep{
		{1, "staging", "started", "staging settings payload", "", ""},
		{2, "staging", "completed", "settings payload staged", "", ""},
		{3, "edge", "started", "applying migration edge", runtime.V2Plan.Compiled.Generation.ID, runtime.V2Plan.Compiled.TargetGeneration.ID},
		{4, "edge", "completed", "migration edge validated", runtime.V2Plan.Compiled.Generation.ID, runtime.V2Plan.Compiled.TargetGeneration.ID},
		{5, "validation", "started", "validating staged settings", "", ""},
		{6, "validation", "completed", "staged settings validated", "", ""},
		{7, "validation", "started", "validating staged settings", "", ""},
		{8, "validation", "completed", "staged settings validated", "", ""},
	}
	if !repeat {
		steps = append(steps,
			migrationStep{11, "commit", "started", "committing settings", "", ""},
			migrationStep{12, "commit", "completed", "settings committed", "", ""},
			migrationStep{13, "validation", "started", "validating restored settings", "", ""},
			migrationStep{14, "validation", "completed", "restored settings validated", "", ""},
		)
	}
	for _, step := range steps {
		event := segment[step.index]
		from, hasFrom := event["fromGeneration"]
		to, hasTo := event["toGeneration"]
		wantEdge := step.from != ""
		if event["stage"] != step.stage || event["status"] != step.status || event["message"] != step.message ||
			event["captureId"] != resolution.CaptureID || event["configSetId"] != resolution.ConfigSetID ||
			event["reason"] != nil || event["remediation"] != nil || hasFrom != wantEdge || hasTo != wantEdge ||
			wantEdge && (from != step.from || to != step.to) {
			return fail(CodeEventContract, "rebuild", "config-migration", "migration edge/validation stage sequence differs")
		}
	}
	resolutionEvent := segment[9]
	if resolutionEvent["captureId"] != resolution.CaptureID || resolutionEvent["moduleId"] != runtime.Module.ID || resolutionEvent["configSetId"] != resolution.ConfigSetID ||
		resolutionEvent["sourceInstanceId"] != resolution.SourceInstanceID || resolutionEvent["targetInstanceId"] != resolution.TargetInstanceID ||
		resolutionEvent["sourceGeneration"] != resolution.SourceGeneration || resolutionEvent["sourceGenerationFingerprint"] != resolution.SourceGenerationFingerprint ||
		resolutionEvent["targetGeneration"] != resolution.TargetGeneration || resolutionEvent["resolution"] != "migrate" ||
		resolutionEvent["captureModuleRevision"] != resolution.CaptureModuleRevision || resolutionEvent["restoreModuleRevision"] != resolution.RestoreModuleRevision {
		return fail(CodeEventContract, "rebuild", "config-resolution", "migration resolution event identity differs")
	}
	if !eventValueMatches(resolutionEvent["sourceInstance"], resolution.SourceInstance) || !eventValueMatches(resolutionEvent["reason"], resolution.Reason) ||
		!eventStringSliceExact(resolutionEvent["migrationPath"], resolution.MigrationPath) || !eventCandidatesMatch(resolutionEvent["targetCandidates"], resolution.TargetCandidates) ||
		resolutionEvent["label"] != resolution.Label || resolutionEvent["message"] != resolution.Message || !eventValueMatches(resolutionEvent["remediation"], resolution.Remediation) {
		return fail(CodeEventContract, "rebuild", "config-resolution", "migration resolution event path/candidates differ")
	}
	firstItem, lastItem := segment[10], segment[len(segment)-2]
	wantFirst := "restoring"
	if repeat {
		wantFirst = "skipped_up_to_date"
	}
	if !v2RestoreEventMatches(firstItem, items[0], runtime.Module.ID, wantFirst) || !repeat && !v2RestoreEventMatches(lastItem, items[0], runtime.Module.ID, "restored") {
		return fail(CodeEventContract, "rebuild", "restore-item", "migration restore item lifecycle differs")
	}
	wantRestoreSuccess, wantRestoreSkipped := len(items), 0
	if repeat {
		wantRestoreSuccess, wantRestoreSkipped = 0, len(items)
	}
	if !v2SummaryEventExact(segment[len(segment)-1], "restore", len(items), wantRestoreSuccess, wantRestoreSkipped, 0) {
		return fail(CodeEventContract, "rebuild", "restore.summary", "migration restore summary accounting differs")
	}
	return validateV2VerifyEventSegments(events, runtime, true)
}

func v2DirectMigrationMessage(stage, status string, transaction bool) string {
	if transaction {
		switch stage + "/" + status {
		case "commit/started":
			return "committing settings"
		case "commit/completed":
			return "settings committed"
		case "validation/started":
			return "validating restored settings"
		case "validation/completed":
			return "restored settings validated"
		}
	}
	switch stage + "/" + status {
	case "staging/started":
		return "staging settings payload"
	case "staging/completed":
		return "settings payload staged"
	case "validation/started":
		return "validating staged settings"
	case "validation/completed":
		return "staged settings validated"
	}
	return ""
}

func v2RestoreEventMatches(event map[string]any, item restore.RestoreResult, moduleID, status string) bool {
	restorer := item.RestoreType
	if restorer == "" {
		restorer = "copy"
	}
	var reason any
	var backup any
	message := "restoring settings"
	if status == "restored" {
		backup = item.BackupPath
		message = "settings restored"
	} else if status == "skipped_up_to_date" {
		reason = "already_up_to_date"
		message = "target settings are already current"
	}
	return event["id"] == item.ID && event["module"] == moduleID && event["restorer"] == restorer &&
		event["source"] == item.Source && event["target"] == item.Target && event["status"] == status &&
		eventValueMatches(event["reason"], reason) && eventValueMatches(event["backupPath"], backup) &&
		event["targetExisted"] == item.TargetExistedBefore && event["message"] == message &&
		event["captureId"] == item.CaptureID && event["configSetId"] == item.ConfigSetID &&
		event["targetInstanceId"] == item.TargetInstanceID && event["sourceGeneration"] == item.SourceGeneration &&
		event["targetGeneration"] == item.TargetGeneration
}

func eventValueMatches(actual, expected any) bool {
	actualJSON, actualErr := json.Marshal(actual)
	expectedJSON, expectedErr := json.Marshal(expected)
	if actualErr != nil || expectedErr != nil {
		return false
	}
	var actualValue, expectedValue any
	return json.Unmarshal(actualJSON, &actualValue) == nil && json.Unmarshal(expectedJSON, &expectedValue) == nil && reflect.DeepEqual(actualValue, expectedValue)
}

func validateV2VerifyEventSegments(events []map[string]any, runtime *scenarioRuntime, embedded bool) *Failure {
	segments := 0
	for index, event := range events {
		if event["event"] != "phase" || event["phase"] != "verify" {
			continue
		}
		segments++
		wantSuccess := 1 + len(runtime.Module.Verify)
		if embedded && segments == 1 {
			wantSuccess = 1
		}
		if index+2 >= len(events) || events[index+1]["event"] != "item" || events[index+2]["event"] != "summary" ||
			events[index+1]["id"] != runtime.Inventory.Ref || events[index+1]["driver"] != runtime.Inventory.Driver ||
			!v2SummaryEventExact(events[index+2], "verify", wantSuccess, wantSuccess, 0, 0) {
			return fail(CodeEventContract, "verify", "verify.summary", "verifier event segment or summary differs from exact app/module result set")
		}
	}
	want := 1
	if embedded {
		want = 2
	}
	if segments != want {
		return fail(CodeEventContract, "verify", "events", "verifier segment count differs")
	}
	return nil
}

func v2SummaryEventExact(event map[string]any, phase string, total, success, skipped, failed int) bool {
	return event["event"] == "summary" && event["phase"] == phase &&
		event["total"] == json.Number(fmt.Sprintf("%d", total)) &&
		event["success"] == json.Number(fmt.Sprintf("%d", success)) &&
		event["skipped"] == json.Number(fmt.Sprintf("%d", skipped)) &&
		event["failed"] == json.Number(fmt.Sprintf("%d", failed))
}

func validateV2RevertEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime) *Failure {
	var data struct {
		JournalUsed string                            `json:"journalUsed"`
		Results     []struct{ Target, Action string } `json:"results"`
	}
	if runtime == nil || runtime.V2Plan == nil || json.Unmarshal(raw, &data) != nil || data.JournalUsed != "" || len(data.Results) != len(runtime.V2Plan.Targets) {
		return fail(CodeEnvelopeContract, "revert", "data", "generation-only revert result is malformed")
	}
	expected := map[string]struct{}{}
	for _, target := range runtime.V2Plan.Targets {
		display, err := runtime.validationContext().DisplayPath(target.Resolved)
		if err != nil {
			return fail(CodeIsolationFailure, "revert", target.Coordinate, "generation target cannot be projected inside validation authority")
		}
		expected[strings.ToLower(display)] = struct{}{}
	}
	for _, result := range data.Results {
		if _, ok := expected[strings.ToLower(result.Target)]; !ok || result.Action != "reverted" {
			return fail(CodeRevertFailure, "revert", "results", "revert action does not bind the generation target")
		}
		delete(expected, strings.ToLower(result.Target))
	}
	if len(expected) != 0 {
		return fail(CodeRevertFailure, "revert", "results", "revert omitted a generation target")
	}
	if len(events) != 2+len(data.Results) || events[0]["event"] != "phase" || events[0]["phase"] != "restore" || events[len(events)-1]["event"] != "summary" || events[len(events)-1]["success"] != json.Number(fmt.Sprintf("%d", len(data.Results))) {
		return fail(CodeEventContract, "revert", "events", "generation revert event sequence differs")
	}
	for index, result := range data.Results {
		event := events[index+1]
		if event["event"] != "item" || event["id"] != result.Target || event["driver"] != "restore" || event["status"] != "installed" || event["reason"] != "" {
			return fail(CodeEventContract, "revert", "events", "generation revert item event differs")
		}
	}
	return nil
}

func eventStringSliceExact(value any, expected []string) bool {
	raw, ok := value.([]any)
	if !ok || len(raw) != len(expected) {
		return false
	}
	for index := range expected {
		if raw[index] != expected[index] {
			return false
		}
	}
	return true
}

func eventCandidatesMatch(value any, expected []planner.TargetInstance) bool {
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var actual []planner.TargetInstance
	return json.Unmarshal(raw, &actual) == nil && reflect.DeepEqual(actual, expected)
}
