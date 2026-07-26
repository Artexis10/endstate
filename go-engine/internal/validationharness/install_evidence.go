// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func validateInstallApplyEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime) *Failure {
	if runtime == nil || runtime.InstallPlan == nil {
		return fail(CodeEnvelopeContract, "apply", "data", "install plan authority is absent")
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || !exactRawFields(data, "dryRun", "manifest", "summary", "actions", "packageModuleMap") {
		return fail(CodeEnvelopeContract, "apply", "data", "install apply result has a malformed or foreign field shape")
	}
	var dryRun bool
	if json.Unmarshal(data["dryRun"], &dryRun) != nil || !dryRun {
		return fail(CodeEnvelopeContract, "apply", "dryRun", "install contract must exercise apply dry-run")
	}
	if failure := validateInstallManifestRef(data["manifest"], runtime, true); failure != nil {
		return failure
	}
	var summary struct{ Total, Success, Skipped, Failed int }
	if json.Unmarshal(data["summary"], &summary) != nil || !rawHasExactFields(data["summary"], "total", "success", "skipped", "failed") ||
		summary.Total != 1 || summary.Success != 0 || summary.Skipped != 1 || summary.Failed != 0 {
		return fail(CodeEnvelopeContract, "apply", "summary", "install dry-run summary is not one already-present no-install action")
	}
	var actions []map[string]json.RawMessage
	if json.Unmarshal(data["actions"], &actions) != nil || len(actions) != 1 || !exactRawFields(actions[0], "id", "ref", "driver", "source", "name", "status", "reason", "message", "manual") {
		return fail(CodeEnvelopeContract, "apply", "actions", "install dry-run action multiset is not exact")
	}
	var action struct {
		ID, Ref, Driver, Source, Name, Status, Reason, Message string
		Manual                                                 any
	}
	actionRaw, _ := json.Marshal(actions[0])
	if json.Unmarshal(actionRaw, &action) != nil || action.ID != runtime.Inventory.AppID || action.Ref != runtime.Inventory.Ref ||
		!strings.EqualFold(action.Driver, runtime.Inventory.Driver) || action.Source != runtime.Inventory.Source || action.Name != runtime.Inventory.DisplayName ||
		action.Status != "present" || action.Reason != "already_installed" || action.Message != "Already installed" || action.Manual != nil {
		return fail(CodeEnvelopeContract, "apply", "actions", "install dry-run action is not the selected already-present package")
	}
	var packageOwners map[string][]string
	packageKey := strings.ToLower(runtime.Inventory.Driver) + ":" + runtime.Inventory.Ref
	if json.Unmarshal(data["packageModuleMap"], &packageOwners) != nil || len(packageOwners) != 1 || len(packageOwners[packageKey]) != 1 || packageOwners[packageKey][0] != runtime.InstallPlan.ModuleID {
		return fail(CodeEnvelopeContract, "apply", "packageModuleMap", "package ownership is not exactly the production module")
	}
	return validateInstallEventEvidence(events, runtime, "plan", false)
}

func validateInstallVerifyEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime, commandPass bool) *Failure {
	if runtime == nil || runtime.InstallPlan == nil {
		return fail(CodeEnvelopeContract, "verify", "data", "install plan authority is absent")
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || !exactRawFields(data, "manifest", "summary", "results") {
		return fail(CodeEnvelopeContract, "verify", "data", "install verify result has a malformed or foreign field shape")
	}
	if failure := validateInstallManifestRef(data["manifest"], runtime, false); failure != nil {
		return failure
	}
	var summary struct{ Total, Pass, Fail, Skipped int }
	wantPass, wantFail := 1, 1
	if commandPass {
		wantPass, wantFail = 2, 0
	}
	if json.Unmarshal(data["summary"], &summary) != nil || !rawHasExactFields(data["summary"], "total", "pass", "fail") ||
		summary.Total != 2 || summary.Pass != wantPass || summary.Fail != wantFail || summary.Skipped != 0 {
		return fail(CodeEnvelopeContract, "verify", "summary", "install verify summary differs from the exact package and verifier result")
	}
	var results []map[string]json.RawMessage
	if json.Unmarshal(data["results"], &results) != nil || len(results) != 2 {
		return fail(CodeEnvelopeContract, "verify", "results", "install verify result multiset is not exact")
	}
	if !exactRawFields(results[0], "type", "id", "ref", "driver", "name", "status") {
		return fail(CodeEnvelopeContract, "verify", "results", "install package verifier has a foreign field shape: "+strings.Join(rawFieldNames(results[0]), ","))
	}
	var app struct{ Type, ID, Ref, Driver, Name, Status string }
	appRaw, _ := json.Marshal(results[0])
	if json.Unmarshal(appRaw, &app) != nil || app.Type != "app" || app.ID != runtime.Inventory.AppID || app.Ref != runtime.Inventory.Ref ||
		!strings.EqualFold(app.Driver, runtime.Inventory.Driver) || app.Name != runtime.Inventory.DisplayName || app.Status != "pass" {
		return fail(CodeEnvelopeContract, "verify", "results.app", "install package verification is not the exact selected passing package")
	}
	if !exactRawFields(results[1], "type", "status", "message") {
		return fail(CodeEnvelopeContract, "verify", "results.command", "install command verifier has a foreign field shape")
	}
	var command struct{ Type, Status, Message string }
	commandRaw, _ := json.Marshal(results[1])
	if json.Unmarshal(commandRaw, &command) != nil || command.Type != runtime.InstallPlan.Verifiers[0].Type {
		return fail(CodeEnvelopeContract, "verify", "results.command", "install command verifier identity differs from production")
	}
	wantStatus := "fail"
	wantMessage := "Command not found: " + runtime.InstallPlan.Verifiers[0].Command
	if commandPass {
		wantStatus = "pass"
		wantMessage = "Command exists: $ENDSTATE_ROOT/state/validation-tools/" + runtime.InstallPlan.CommandExecutable
	}
	if command.Status != wantStatus || strings.ReplaceAll(command.Message, `\`, "/") != wantMessage {
		return fail(CodeEnvelopeContract, "verify", "results.command", "install command verifier status or ToolRoot resolution is not exact")
	}
	return validateInstallEventEvidence(events, runtime, "verify", !commandPass)
}

func validateInstallManifestRef(raw json.RawMessage, runtime *scenarioRuntime, apply bool) *Failure {
	want := []string{"path", "name"}
	if apply {
		want = append(want, "hash")
	}
	var fields map[string]json.RawMessage
	var value struct{ Path, Name, Hash string }
	if json.Unmarshal(raw, &fields) != nil || !exactRawFields(fields, want...) || json.Unmarshal(raw, &value) != nil ||
		strings.ReplaceAll(value.Path, `\`, "/") != "$ENDSTATE_ROOT/manifests/install-v1.jsonc" || value.Name != "Endstate validation "+runtime.InstallPlan.ModuleID || value.Hash != "" {
		return fail(CodeEnvelopeContract, map[bool]string{false: "verify", true: "apply"}[apply], "manifest", "install manifest reference is not exact")
	}
	return nil
}

func validateInstallEventEvidence(events []map[string]any, runtime *scenarioRuntime, phase string, negative bool) *Failure {
	if len(events) != 3 || events[0]["event"] != "phase" || events[0]["phase"] != phase || events[1]["event"] != "item" || events[2]["event"] != "summary" || events[2]["phase"] != phase {
		return fail(CodeEventContract, phase, "events", "install event sequence is not the exact phase/item/summary segment")
	}
	reason, message := "", "Verified installed"
	total, success, failed := 2, 2, 0
	if phase == "plan" {
		reason, message, total, success = "already_installed", "Already installed", 1, 1
	} else if negative {
		success, failed = 1, 1
	}
	item := events[1]
	if item["id"] != runtime.Inventory.Ref || !strings.EqualFold(fmt.Sprint(item["driver"]), runtime.Inventory.Driver) || item["name"] != runtime.Inventory.DisplayName ||
		item["status"] != "present" || item["reason"] != reason || item["message"] != message {
		return fail(CodeEventContract, phase, "item", "install package event identity or status is not exact")
	}
	gotTotal, totalOK := eventInteger(events[2], "total")
	gotSuccess, successOK := eventInteger(events[2], "success")
	gotSkipped, skippedOK := eventInteger(events[2], "skipped")
	gotFailed, failedOK := eventInteger(events[2], "failed")
	if !totalOK || !successOK || !skippedOK || !failedOK || gotTotal != total || gotSuccess != success || gotSkipped != 0 || gotFailed != failed {
		return fail(CodeEventContract, phase, "summary", "install event summary is not exact")
	}
	return nil
}

func exactRawFields(value map[string]json.RawMessage, names ...string) bool {
	if len(value) != len(names) {
		return false
	}
	for _, name := range names {
		if _, exists := value[name]; !exists {
			return false
		}
	}
	return true
}

func rawHasExactFields(raw json.RawMessage, names ...string) bool {
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && exactRawFields(value, names...)
}

func rawFieldNames(value map[string]json.RawMessage) []string {
	names := make([]string, 0, len(value))
	for name := range value {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
