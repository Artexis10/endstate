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

type verifyEvidence struct {
	Summary struct {
		Total   int `json:"total"`
		Pass    int `json:"pass"`
		Fail    int `json:"fail"`
		Skipped int `json:"skipped"`
	} `json:"summary"`
	Results []verifyEvidenceItem `json:"results"`
}

type verifyEvidenceItem struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Ref    string `json:"ref"`
	Driver string `json:"driver"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func validateVerifyEvidence(raw []byte, runtime *scenarioRuntime, phase string) *Failure {
	var data verifyEvidence
	if runtime == nil || runtime.Module == nil || json.Unmarshal(raw, &data) != nil {
		return fail(CodeEnvelopeContract, phase, "verify", "verifier result is malformed")
	}
	expectedTotal := 1 + len(runtime.Module.Verify)
	if data.Summary.Total != expectedTotal || data.Summary.Pass != expectedTotal || data.Summary.Fail != 0 || data.Summary.Skipped != 0 || len(data.Results) != expectedTotal {
		return fail(CodeEnvelopeContract, phase, "verify.summary", "verifier summary is not the exact app and module proof set")
	}
	expectedTypes := make(map[string]int, len(runtime.Module.Verify))
	appCount := 0
	for _, verifier := range runtime.Module.Verify {
		expectedTypes[verifier.Type]++
	}
	for _, item := range data.Results {
		if item.Status != "pass" || item.Reason != "" {
			return fail(CodeEnvelopeContract, phase, "verify.results", "verifier item did not pass")
		}
		if item.Type == "app" {
			appCount++
			if item.ID != runtime.Inventory.AppID || item.Ref != runtime.Inventory.Ref || !strings.EqualFold(item.Driver, runtime.Inventory.Driver) {
				return fail(CodeEnvelopeContract, phase, "verify.app", "app verification is not attributed to the selected inventory item")
			}
			continue
		}
		if item.ID != "" || item.Ref != "" || item.Driver != "" || expectedTypes[item.Type] == 0 {
			return fail(CodeEnvelopeContract, phase, "verify.results", "module verifier result has foreign attribution or type")
		}
		expectedTypes[item.Type]--
	}
	if appCount != 1 {
		return fail(CodeEnvelopeContract, phase, "verify.app", "verifier result must contain exactly one selected app")
	}
	for _, remaining := range expectedTypes {
		if remaining != 0 {
			return fail(CodeEnvelopeContract, phase, "verify.results", "module verifier type multiset differs from the production module")
		}
	}
	return nil
}

type rebuildEvidence struct {
	Apply struct {
		Summary struct {
			Total   int `json:"total"`
			Success int `json:"success"`
			Skipped int `json:"skipped"`
			Failed  int `json:"failed"`
		} `json:"summary"`
		Actions []struct {
			ID     string `json:"id"`
			Driver string `json:"driver"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"actions"`
		ConfigResolutionSummary struct {
			Total    int `json:"total"`
			Selected int `json:"selected"`
			Skipped  int `json:"skipped"`
			Failed   int `json:"failed"`
		} `json:"configResolutionSummary"`
		ConfigResolutions []struct {
			Status     string  `json:"status"`
			Resolution string  `json:"resolution"`
			Reason     *string `json:"reason"`
		} `json:"configResolutions"`
		RestoreItems []struct {
			Source              string `json:"source"`
			Target              string `json:"target"`
			Status              string `json:"status"`
			BackupPath          string `json:"backupPath"`
			BackupCreated       bool   `json:"backupCreated"`
			TargetExistedBefore bool   `json:"targetExistedBefore"`
			RestoreType         string `json:"restoreType"`
		} `json:"restoreItems"`
	} `json:"apply"`
	ConfigResolutionSummary struct {
		Total    int `json:"total"`
		Selected int `json:"selected"`
		Skipped  int `json:"skipped"`
		Failed   int `json:"failed"`
	} `json:"configResolutionSummary"`
	ConfigResolutions []struct {
		Status     string  `json:"status"`
		Resolution string  `json:"resolution"`
		Reason     *string `json:"reason"`
	} `json:"configResolutions"`
	RestoreItems []struct {
		Source              string `json:"source"`
		Target              string `json:"target"`
		Status              string `json:"status"`
		BackupPath          string `json:"backupPath"`
		BackupCreated       bool   `json:"backupCreated"`
		TargetExistedBefore bool   `json:"targetExistedBefore"`
		RestoreType         string `json:"restoreType"`
	} `json:"restoreItems"`
	Verify json.RawMessage `json:"verify"`
}

type rebuildEvidenceBinding struct {
	Journal         string
	StoreMemberID   string
	BackupsByTarget map[string]string
	SourcesByTarget map[string]string
}

func rebuildBindingFromEvidence(raw []byte) (rebuildEvidenceBinding, error) {
	var data rebuildEvidence
	if err := json.Unmarshal(raw, &data); err != nil {
		return rebuildEvidenceBinding{}, err
	}
	binding := rebuildEvidenceBinding{
		BackupsByTarget: make(map[string]string, len(data.RestoreItems)),
		SourcesByTarget: make(map[string]string, len(data.RestoreItems)),
	}
	for _, item := range data.RestoreItems {
		key := strings.ToLower(item.Target)
		binding.SourcesByTarget[key] = item.Source
		if item.BackupPath != "" {
			binding.BackupsByTarget[key] = item.BackupPath
		}
	}
	return binding, nil
}

func validateRebuildEvidence(raw []byte, runtime *scenarioRuntime, iteration int) *Failure {
	var data rebuildEvidence
	if runtime == nil || runtime.Plan == nil || iteration < 0 || iteration > 2 || json.Unmarshal(raw, &data) != nil {
		return fail(CodeEnvelopeContract, "rebuild", "data", "rebuild evidence is malformed")
	}
	if data.Apply.Summary.Total != 1 || data.Apply.Summary.Success != 0 || data.Apply.Summary.Skipped != 1 || data.Apply.Summary.Failed != 0 || len(data.Apply.Actions) != 1 {
		return fail(CodeEnvelopeContract, "rebuild", "apply", "rebuild did not exercise exactly one already-present selected app")
	}
	action := data.Apply.Actions[0]
	if action.ID != runtime.Inventory.AppID || !strings.EqualFold(action.Driver, runtime.Inventory.Driver) || action.Status != "present" || action.Reason != "already_installed" {
		return fail(CodeEnvelopeContract, "rebuild", "apply.actions", "rebuild app action is not the selected already-present inventory item")
	}
	if failure := validateVerifyEvidence(data.Verify, runtime, "rebuild"); failure != nil {
		return failure
	}
	if !reflect.DeepEqual(data.ConfigResolutionSummary, data.Apply.ConfigResolutionSummary) ||
		!reflect.DeepEqual(data.ConfigResolutions, data.Apply.ConfigResolutions) || !reflect.DeepEqual(data.RestoreItems, data.Apply.RestoreItems) {
		return fail(CodeEnvelopeContract, "rebuild", "apply", "nested and outer restore evidence differ")
	}
	if len(data.ConfigResolutions) != 1 || data.ConfigResolutionSummary.Total != 1 || data.ConfigResolutionSummary.Failed != 0 || len(data.RestoreItems) != runtime.Plan.OperationCount() {
		return fail(CodeEnvelopeContract, "rebuild", "config", "rebuild config evidence does not cover the exact fixture plan")
	}
	repeat := iteration == 2
	if repeat {
		if data.ConfigResolutionSummary.Selected != 0 || data.ConfigResolutionSummary.Skipped != 1 ||
			data.ConfigResolutions[0].Status != "skipped" || !exactOptionalString(data.ConfigResolutions[0].Reason, "already_up_to_date") {
			return fail(CodeEnvelopeContract, "rebuild", "configResolutions", "repeat rebuild did not report exact convergence")
		}
	} else if data.ConfigResolutionSummary.Selected != 1 || data.ConfigResolutionSummary.Skipped != 0 ||
		data.ConfigResolutions[0].Status != "restored" || data.ConfigResolutions[0].Reason != nil {
		return fail(CodeEnvelopeContract, "rebuild", "configResolutions", "restoring rebuild did not report an exact selected restore")
	}
	if data.ConfigResolutions[0].Resolution != "legacy_unverified" {
		return fail(CodeEnvelopeContract, "rebuild", "configResolutions", "config resolution differs from the schema-v1 legacy contract")
	}

	type expectedRestore struct {
		source   string
		strategy string
	}
	expected := make(map[string]expectedRestore, runtime.Plan.OperationCount())
	for _, target := range runtime.Plan.RestoreTargets() {
		expected[strings.ToLower(target.Authored)] = expectedRestore{source: v1RestoreSource(runtime.Module.ID, target.Destination), strategy: target.Strategy}
	}
	if len(expected) != runtime.Plan.OperationCount() {
		return fail(CodeEnvelopeContract, "rebuild", "restoreItems", "fixture restore target identities are not unique")
	}
	for _, item := range data.RestoreItems {
		expectedItem, ok := expected[strings.ToLower(item.Target)]
		restoreType := item.RestoreType
		if restoreType == "" {
			restoreType = "copy"
		}
		if !ok || filepath.ToSlash(item.Source) != expectedItem.source || restoreType != expectedItem.strategy || !item.TargetExistedBefore {
			return fail(CodeEnvelopeContract, "rebuild", "restoreItems", fmt.Sprintf(
				"restore item attribution differs: targetKnown=%t sourceMatches=%t type=%q targetExisted=%t",
				ok, filepath.ToSlash(item.Source) == expectedItem.source, item.RestoreType, item.TargetExistedBefore))
		}
		delete(expected, strings.ToLower(item.Target))
		if repeat {
			if item.Status != "skipped_up_to_date" || item.BackupCreated || item.BackupPath != "" {
				return fail(CodeEnvelopeContract, "rebuild", "restoreItems", fmt.Sprintf(
					"repeat rebuild was not backup-free convergence: status=%q backupCreated=%t backupPathPresent=%t",
					item.Status, item.BackupCreated, item.BackupPath != ""))
			}
		} else if item.Status != "restored" || !item.BackupCreated || strings.TrimSpace(item.BackupPath) == "" {
			return fail(CodeEnvelopeContract, "rebuild", "restoreItems", "restoring rebuild lacks per-target backup evidence")
		}
	}
	if len(expected) != 0 {
		return fail(CodeEnvelopeContract, "rebuild", "restoreItems", "restore item multiset omitted a fixture target")
	}
	return nil
}

func validateV1RebuildEvents(events []map[string]any, runtime *scenarioRuntime, iteration int, binding rebuildEvidenceBinding) *Failure {
	if runtime == nil || runtime.Module == nil || runtime.Plan == nil {
		return fail(CodeEventContract, "rebuild", "events", "schema-v1 event authority is absent")
	}
	start, end := -1, -1
	for index, event := range events {
		if event["event"] == "phase" && event["phase"] == "restore" {
			start = index
		}
		if start >= 0 && event["event"] == "summary" && event["phase"] == "restore" {
			end = index
			break
		}
	}
	if start < 0 || end < 0 {
		return fail(CodeEventContract, "rebuild", "restore", "schema-v1 restore event segment is absent")
	}
	repeat := iteration == 2
	perTarget := 2
	segment := events[start : end+1]
	wantCount := 3 + runtime.Plan.OperationCount()*perTarget
	if len(segment) != wantCount || segment[0]["event"] != "phase" || segment[1]["event"] != "config-resolution" {
		return fail(CodeEventContract, "rebuild", "restore", fmt.Sprintf("schema-v1 restore event segment differs: got=%d want=%d", len(segment), wantCount))
	}
	expected := make(map[string]FixtureRestoreTarget, runtime.Plan.OperationCount())
	for _, target := range runtime.Plan.RestoreTargets() {
		expected[strings.ToLower(target.Authored)] = target
	}
	if len(expected) != runtime.Plan.OperationCount() {
		return fail(CodeEventContract, "rebuild", "restore-item", "fixture restore target identities are not unique")
	}
	for index := 2; index < len(segment)-1; {
		event := segment[index]
		if event["event"] != "restore-item" {
			return fail(CodeEventContract, "rebuild", "restore-item", "schema-v1 restore item event is absent")
		}
		targetText := restoreEventString(event["target"])
		target, ok := expected[strings.ToLower(targetText)]
		if !ok || event["module"] != runtime.Module.ID || event["restorer"] != target.Strategy ||
			filepath.ToSlash(restoreEventString(event["source"])) != v1RestoreSource(runtime.Module.ID, target.Destination) || event["targetExisted"] != true {
			return fail(CodeEventContract, "rebuild", "restore-item", "schema-v1 restore item attribution differs")
		}
		if event["status"] != "restoring" || event["reason"] != nil || event["backupPath"] != nil || index+1 >= len(segment)-1 {
			return fail(CodeEventContract, "rebuild", "restore-item", "schema-v1 restoring event differs")
		}
		terminal := segment[index+1]
		backup, backupOK := terminal["backupPath"].(string)
		expectedBackup, backupBound := binding.BackupsByTarget[strings.ToLower(target.Authored)]
		if terminal["event"] != "restore-item" || terminal["module"] != runtime.Module.ID || terminal["restorer"] != target.Strategy ||
			filepath.ToSlash(restoreEventString(terminal["source"])) != v1RestoreSource(runtime.Module.ID, target.Destination) || terminal["target"] != target.Authored ||
			terminal["targetExisted"] != true || (!repeat && (terminal["status"] != "restored" || terminal["reason"] != nil || !backupOK || strings.TrimSpace(backup) == "" || !backupBound || backup != expectedBackup)) ||
			(repeat && (terminal["status"] != "skipped_up_to_date" || terminal["reason"] != "already_up_to_date" || terminal["backupPath"] != nil)) {
			return fail(CodeEventContract, "rebuild", "restore-item", "schema-v1 terminal restore event differs")
		}
		delete(expected, strings.ToLower(targetText))
		index += 2
	}
	if len(expected) != 0 {
		return fail(CodeEventContract, "rebuild", "restore-item", "schema-v1 restore event multiset omitted a target")
	}
	summary := segment[len(segment)-1]
	success, skipped := runtime.Plan.OperationCount(), 0
	if repeat {
		success, skipped = 0, runtime.Plan.OperationCount()
	}
	if !v2SummaryEventExact(summary, "restore", runtime.Plan.OperationCount(), success, skipped, 0) {
		return fail(CodeEventContract, "rebuild", "restore.summary", "schema-v1 restore summary differs")
	}
	return nil
}

func exactOptionalString(value *string, expected string) bool {
	return value != nil && *value == expected
}

func validateRevertEvidence(raw []byte, runtime *scenarioRuntime, binding rebuildEvidenceBinding) *Failure {
	var data struct {
		JournalUsed string `json:"journalUsed"`
		Results     []struct {
			Target     string `json:"target"`
			Action     string `json:"action"`
			BackupUsed string `json:"backupUsed"`
		} `json:"results"`
	}
	if runtime == nil || runtime.Plan == nil || json.Unmarshal(raw, &data) != nil || strings.TrimSpace(data.JournalUsed) == "" ||
		data.JournalUsed != binding.Journal || len(data.Results) != runtime.Plan.OperationCount() || len(binding.BackupsByTarget) != runtime.Plan.OperationCount() {
		return fail(CodeEnvelopeContract, "revert", "data", "revert lacks an exact nonempty journal result")
	}
	expected := make(map[string]struct{}, runtime.Plan.OperationCount())
	for _, target := range runtime.Plan.RestoreTargets() {
		expected[strings.ToLower(target.Authored)] = struct{}{}
	}
	if len(expected) != runtime.Plan.OperationCount() {
		return fail(CodeEnvelopeContract, "revert", "results", "fixture restore target identities are not unique")
	}
	for _, result := range data.Results {
		key := strings.ToLower(result.Target)
		if _, ok := expected[key]; !ok || result.Action != "reverted" || result.BackupUsed == "" || result.BackupUsed != binding.BackupsByTarget[key] {
			return fail(CodeEnvelopeContract, "revert", "results", "revert action is not exact backup restoration for a fixture target")
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		return fail(CodeEnvelopeContract, "revert", "results", "revert result multiset omitted a fixture target")
	}
	return nil
}
