// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

func validateRestoreContractRebuildEvidence(raw []byte, runtime *scenarioRuntime) *Failure {
	var data rebuildEvidence
	if runtime == nil || runtime.Module == nil || runtime.RestorePlan == nil || runtime.Plan == nil || len(runtime.Plan.Targets) != 1 || json.Unmarshal(raw, &data) != nil {
		return fail(CodeEnvelopeContract, "rebuild", "data", "restore-contract rebuild evidence is malformed")
	}
	if data.Apply.Summary.Total != 1 || data.Apply.Summary.Success != 0 || data.Apply.Summary.Skipped != 1 || data.Apply.Summary.Failed != 0 || len(data.Apply.Actions) != 1 {
		return fail(CodeEnvelopeContract, "rebuild", "apply", "restore-contract rebuild did not exercise one already-present selected app")
	}
	action := data.Apply.Actions[0]
	if action.ID != runtime.Inventory.AppID || !strings.EqualFold(action.Driver, runtime.Inventory.Driver) || action.Status != "present" || action.Reason != "already_installed" {
		return fail(CodeEnvelopeContract, "rebuild", "apply.actions", "restore-contract app action differs from selected inventory authority")
	}
	if !reflect.DeepEqual(data.ConfigResolutionSummary, data.Apply.ConfigResolutionSummary) ||
		!reflect.DeepEqual(data.ConfigResolutions, data.Apply.ConfigResolutions) || !reflect.DeepEqual(data.RestoreItems, data.Apply.RestoreItems) {
		return fail(CodeEnvelopeContract, "rebuild", "apply", "nested and outer restore-contract evidence differ")
	}
	if len(data.ConfigResolutions) != 1 || data.ConfigResolutionSummary.Total != 1 || data.ConfigResolutionSummary.Selected != 1 || data.ConfigResolutionSummary.Skipped != 0 || data.ConfigResolutionSummary.Failed != 0 || data.ConfigResolutions[0].Status != "restored" || data.ConfigResolutions[0].Resolution != "legacy_unverified" || data.ConfigResolutions[0].Reason != nil {
		return fail(CodeEnvelopeContract, "rebuild", "configResolutions", "restore-contract nested config summary is not one successful legacy restore")
	}
	if len(data.RestoreItems) != 1 {
		return fail(CodeEnvelopeContract, "rebuild", "restoreItems", "restore-contract restore item multiset is not exact")
	}
	item := data.RestoreItems[0]
	if item.Target != runtime.RestorePlan.Restore.Target || filepath.ToSlash(item.Source) != runtime.RestorePlan.Restore.Source || item.RestoreType != "" || item.Status != "restored" || !item.TargetExistedBefore || !item.BackupCreated || strings.TrimSpace(item.BackupPath) == "" {
		return fail(CodeEnvelopeContract, "rebuild", "restoreItems", fmt.Sprintf(
			"restore-contract item differs: target=%t source=%t type=%q status=%q targetExisted=%t backupCreated=%t backupPathPresent=%t",
			item.Target == runtime.RestorePlan.Restore.Target, filepath.ToSlash(item.Source) == runtime.RestorePlan.Restore.Source,
			item.RestoreType, item.Status, item.TargetExistedBefore, item.BackupCreated, strings.TrimSpace(item.BackupPath) != ""))
	}
	if failure := validateVerifyEvidence(data.Verify, runtime, "rebuild"); failure != nil {
		return failure
	}
	return nil
}

func validateRestoreContractRebuildEvents(events []map[string]any, runtime *scenarioRuntime) *Failure {
	if runtime == nil || runtime.Module == nil || runtime.RestorePlan == nil {
		return fail(CodeEventContract, "rebuild", "events", "restore-contract event authority is absent")
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
		return fail(CodeEventContract, "rebuild", "restore", "restore-contract restore event segment is absent")
	}
	segment := events[restoreStart : restoreEnd+1]
	want := []string{"phase", "config-resolution", "restore-item", "restore-item", "summary"}
	if len(segment) != len(want) {
		return fail(CodeEventContract, "rebuild", "restore", fmt.Sprintf("restore-contract event count=%d want=%d", len(segment), len(want)))
	}
	for index, eventType := range want {
		if segment[index]["event"] != eventType {
			return fail(CodeEventContract, "rebuild", "restore", "restore-contract event order differs")
		}
	}
	resolution := segment[1]
	if resolution["moduleId"] != runtime.Module.ID || resolution["resolution"] != "legacy_unverified" || resolution["reason"] != nil {
		return fail(CodeEventContract, "rebuild", "config-resolution", "restore-contract resolution event differs")
	}
	restore := runtime.RestorePlan.Restore
	for index, status := range []string{"restoring", "restored"} {
		event := segment[index+2]
		backup := event["backupPath"]
		backupOK := backup == nil
		if status == "restored" {
			value, ok := backup.(string)
			backupOK = ok && strings.TrimSpace(value) != ""
		}
		if event["module"] != runtime.Module.ID || event["restorer"] != restore.Type || filepath.ToSlash(restoreEventString(event["source"])) != restore.Source || event["target"] != restore.Target || event["status"] != status || event["reason"] != nil || event["targetExisted"] != true || !backupOK {
			return fail(CodeEventContract, "rebuild", "restore-item", "restore-contract item event lifecycle differs")
		}
	}
	if !v2SummaryEventExact(segment[4], "restore", 1, 1, 0, 0) {
		return fail(CodeEventContract, "rebuild", "restore.summary", "restore-contract restore event summary differs")
	}
	return validateV2VerifyEventSegments(events, runtime, true)
}

func restoreEventString(value any) string {
	text, _ := value.(string)
	return text
}

func validateRestoreContractRevertEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime, binding rebuildEvidenceBinding) *Failure {
	if failure := validateRevertEvidence(raw, runtime, binding); failure != nil {
		return failure
	}
	if runtime == nil || runtime.RestorePlan == nil || len(events) != 3 || events[0]["event"] != "phase" || events[0]["phase"] != "restore" {
		return fail(CodeEventContract, "revert", "events", "restore-contract revert event segment is malformed")
	}
	target := runtime.RestorePlan.Restore.Target
	item := events[1]
	if item["event"] != "item" || item["id"] != target || item["driver"] != "restore" || item["status"] != "installed" || item["reason"] != "" {
		return fail(CodeEventContract, "revert", "events", "restore-contract revert item identity differs")
	}
	if !v2SummaryEventExact(events[2], "restore", 1, 1, 0, 0) {
		return fail(CodeEventContract, "revert", "events", "restore-contract revert summary differs")
	}
	return nil
}
