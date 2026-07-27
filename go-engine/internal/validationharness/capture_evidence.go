// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"fmt"
	"strings"
)

func validateCaptureContractCommandEvidence(raw []byte, events []map[string]any, runtime *scenarioRuntime, artifactBase string) *Failure {
	if runtime == nil || runtime.CapturePlan == nil || len(runtime.CapturePlan.Targets) != 1 {
		return fail(CodeEnvelopeContract, "capture", "data", "capture contract runtime authority is absent")
	}
	var data map[string]json.RawMessage
	if rejectDuplicateJSONFields(raw) != nil || json.Unmarshal(raw, &data) != nil || !exactRawFields(data,
		"appsIncluded", "configModules", "configModuleMap", "packageModuleMap", "outputPath", "outputFormat",
		"configsIncluded", "configsSkipped", "configsCaptureErrors", "sanitized", "isExample", "counts",
		"captureWarnings", "configCapture", "manifest") {
		return fail(CodeEnvelopeContract, "capture", "data", "capture result has a malformed, duplicate, or foreign field shape")
	}
	wantArtifact := "$ENDSTATE_ROOT/manifests/" + artifactBase
	if failure := validateCaptureContractApp(data["appsIncluded"], runtime); failure != nil {
		return failure
	}
	if failure := validateCaptureContractModule(data["configModules"], runtime); failure != nil {
		return failure
	}
	var moduleMap map[string]string
	if json.Unmarshal(data["configModuleMap"], &moduleMap) != nil || len(moduleMap) != 1 || moduleMap[strings.ToLower(runtime.Inventory.Ref)] != runtime.Module.ID {
		return fail(CodeEnvelopeContract, "capture", "configModuleMap", "capture module ownership is not exact")
	}
	var packageMap map[string][]string
	packageKey := strings.ToLower(runtime.Inventory.Driver) + ":" + strings.ToLower(runtime.Inventory.Ref)
	if json.Unmarshal(data["packageModuleMap"], &packageMap) != nil || len(packageMap) != 1 || !exactStrings(packageMap[packageKey], []string{runtime.Module.ID}) {
		return fail(CodeEnvelopeContract, "capture", "packageModuleMap", "capture package ownership is not exact")
	}
	var outputPath, outputFormat string
	if json.Unmarshal(data["outputPath"], &outputPath) != nil || portableCapturePath(outputPath) != wantArtifact ||
		json.Unmarshal(data["outputFormat"], &outputFormat) != nil || outputFormat != "zip" {
		return fail(CodeEnvelopeContract, "capture", "outputPath", "capture artifact reference is not the exact ZIP")
	}
	if !rawStringArrayEquals(data["configsIncluded"], []string{"mgba"}) || !rawStringArrayEquals(data["configsSkipped"], []string{}) ||
		!rawStringArrayEquals(data["configsCaptureErrors"], []string{}) || !rawStringArrayEquals(data["captureWarnings"], []string{}) {
		return fail(CodeEnvelopeContract, "capture", "configsIncluded", "capture module outcome is not one successful mGBA config")
	}
	var sanitized, isExample bool
	if json.Unmarshal(data["sanitized"], &sanitized) != nil || sanitized || json.Unmarshal(data["isExample"], &isExample) != nil || isExample {
		return fail(CodeEnvelopeContract, "capture", "flags", "capture contract used a sanitized or example projection")
	}
	var counts struct {
		FilteredRuntimes, Included, TotalFound, SensitiveExcludedCount, FilteredStoreApps, Skipped int
	}
	if json.Unmarshal(data["counts"], &counts) != nil || !rawHasExactFields(data["counts"], "filteredRuntimes", "included", "totalFound", "sensitiveExcludedCount", "filteredStoreApps", "skipped") ||
		counts.FilteredRuntimes != 0 || counts.Included != 1 || counts.TotalFound != 1 || counts.SensitiveExcludedCount != 0 || counts.FilteredStoreApps != 0 || counts.Skipped != 0 {
		return fail(CodeEnvelopeContract, "capture", "counts", "capture inventory counts are not the exact singleton")
	}
	if failure := validateCaptureContractDeclaration(data["configCapture"], runtime); failure != nil {
		return failure
	}
	var manifestRef map[string]json.RawMessage
	var manifestValue struct{ Name, Path string }
	if json.Unmarshal(data["manifest"], &manifestRef) != nil || !exactRawFields(manifestRef, "name", "path") || json.Unmarshal(data["manifest"], &manifestValue) != nil ||
		manifestValue.Name != "captured" || portableCapturePath(manifestValue.Path) != wantArtifact {
		return fail(CodeEnvelopeContract, "capture", "manifest", "capture manifest reference is not exact")
	}
	return validateCaptureContractEvents(events, runtime, wantArtifact)
}

func validateCaptureContractApp(raw json.RawMessage, runtime *scenarioRuntime) *Failure {
	var values []map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != 1 || !exactRawFields(values[0], "source", "name", "id", "manifestId") {
		return fail(CodeEnvelopeContract, "capture", "appsIncluded", "captured app evidence is not an exact singleton")
	}
	var app struct{ Source, Name, ID, ManifestID string }
	value, _ := json.Marshal(values[0])
	if json.Unmarshal(value, &app) != nil || app.Source != runtime.Inventory.Source || app.Name != runtime.Inventory.DisplayName ||
		app.ID != runtime.Inventory.Ref || app.ManifestID != runtime.Inventory.AppID {
		return fail(CodeEnvelopeContract, "capture", "appsIncluded", "captured app identity differs from validation inventory")
	}
	return nil
}

func validateCaptureContractModule(raw json.RawMessage, runtime *scenarioRuntime) *Failure {
	var values []map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != 1 || !exactRawFields(values[0], "displayName", "wingetRefs", "chocolateyRefs", "appId", "id", "paths", "filesCaptured", "status") {
		return fail(CodeEnvelopeContract, "capture", "configModules", "capture module result is not an exact singleton")
	}
	var value struct {
		DisplayName, AppID, ID, Status    string
		WingetRefs, ChocolateyRefs, Paths []string
		FilesCaptured                     int
	}
	rawValue, _ := json.Marshal(values[0])
	wantPath := v1ArtifactPayloadPath(runtime.Module.ID, runtime.CapturePlan.Targets[0].Destination)
	if json.Unmarshal(rawValue, &value) != nil || value.DisplayName != runtime.Module.DisplayName || value.AppID != "mgba" || value.ID != runtime.Module.ID ||
		value.Status != "captured" || value.FilesCaptured != 1 || !exactStrings(value.WingetRefs, []string{runtime.Inventory.Ref}) || len(value.ChocolateyRefs) != 0 || !exactStrings(value.Paths, []string{wantPath}) {
		return fail(CodeEnvelopeContract, "capture", "configModules", "capture module result is vacuous, foreign, or inexact")
	}
	return nil
}

func validateCaptureContractDeclaration(raw json.RawMessage, runtime *scenarioRuntime) *Failure {
	var summary map[string]json.RawMessage
	if json.Unmarshal(raw, &summary) != nil || !exactRawFields(summary, "modules") {
		return fail(CodeEnvelopeContract, "capture", "configCapture", "capture declaration summary has a foreign field")
	}
	var modules []map[string]json.RawMessage
	if json.Unmarshal(summary["modules"], &modules) != nil || len(modules) != 1 || !exactRawFields(modules[0], "id", "displayName", "entries", "files") {
		return fail(CodeEnvelopeContract, "capture", "configCapture", "capture declaration summary is not exact")
	}
	var value struct {
		ID, DisplayName string
		Entries         int
		Files           []string
	}
	rawValue, _ := json.Marshal(modules[0])
	if json.Unmarshal(rawValue, &value) != nil || value.ID != runtime.Module.ID || value.DisplayName != runtime.Module.DisplayName || value.Entries != 0 ||
		!exactStrings(value.Files, []string{runtime.CapturePlan.Targets[0].Destination}) {
		return fail(CodeEnvelopeContract, "capture", "configCapture", "capture declaration differs from production")
	}
	return nil
}

func validateCaptureContractEvents(events []map[string]any, runtime *scenarioRuntime, wantArtifact string) *Failure {
	if len(events) != 7 || events[0]["event"] != "phase" || events[0]["phase"] != "capture" ||
		events[1]["event"] != "progress" || events[1]["phase"] != "capture" || events[1]["stage"] != "inventory" ||
		events[2]["event"] != "item" || events[3]["event"] != "progress" || events[3]["phase"] != "capture" || events[3]["stage"] != "settings" ||
		events[4]["event"] != "progress" || events[4]["phase"] != "capture" || events[4]["stage"] != "packaging" ||
		events[5]["event"] != "artifact" || events[6]["event"] != "summary" {
		return fail(CodeEventContract, "capture", "events", "capture event sequence is not exact")
	}
	item := events[2]
	if item["id"] != runtime.Inventory.Ref || !strings.EqualFold(fmt.Sprint(item["driver"]), runtime.Inventory.Driver) || item["name"] != runtime.Inventory.DisplayName ||
		item["status"] != "present" || item["reason"] != "detected" || item["message"] != "Detected "+runtime.Inventory.DisplayName {
		return fail(CodeEventContract, "capture", "item", "capture item event identity or status is not exact")
	}
	artifact := events[5]
	if artifact["phase"] != "capture" || artifact["kind"] != "manifest" || portableCapturePath(fmt.Sprint(artifact["path"])) != wantArtifact {
		return fail(CodeEventContract, "capture", "artifact", "capture artifact event is not exact")
	}
	total, totalOK := eventInteger(events[6], "total")
	success, successOK := eventInteger(events[6], "success")
	skipped, skippedOK := eventInteger(events[6], "skipped")
	failed, failedOK := eventInteger(events[6], "failed")
	if events[6]["phase"] != "capture" || !totalOK || !successOK || !skippedOK || !failedOK || total != 1 || success != 1 || skipped != 0 || failed != 0 {
		return fail(CodeEventContract, "capture", "summary", "capture summary is not the exact singleton success")
	}
	return nil
}

func portableCapturePath(value string) string { return strings.ReplaceAll(value, `\`, "/") }

func rawStringArrayEquals(raw json.RawMessage, want []string) bool {
	var got []string
	return json.Unmarshal(raw, &got) == nil && exactStrings(got, want)
}
