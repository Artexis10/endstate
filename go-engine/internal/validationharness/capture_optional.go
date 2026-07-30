// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

func validateCaptureContractOptionalAbsentOutcome(raw []byte, events []map[string]any, runtime *scenarioRuntime, zipPath string) *Failure {
	if runtime == nil || runtime.CapturePlan == nil || len(runtime.CapturePlan.Targets) != 1 || !runtime.CapturePlan.Targets[0].Optional {
		return fail(CodeAssertionContract, "capture", "optional", "all-optional capture authority is absent")
	}
	var data map[string]json.RawMessage
	if rejectDuplicateJSONFields(raw) != nil || json.Unmarshal(raw, &data) != nil || !exactRawFields(data,
		"appsIncluded", "configModules", "configModuleMap", "packageModuleMap", "outputPath", "outputFormat",
		"configsIncluded", "configsSkipped", "configsCaptureErrors", "sanitized", "isExample", "counts",
		"captureWarnings", "warnings", "configCapture", "manifest") {
		return fail(CodeEnvelopeContract, "capture", "data", "optional-absence result has a malformed or foreign field shape")
	}
	if failure := validateCaptureContractWarnings(data["warnings"]); failure != nil {
		return failure
	}
	if failure := validateCaptureContractApp(data["appsIncluded"], runtime); failure != nil {
		return failure
	}
	if failure := validateOptionalCaptureModule(data["configModules"], runtime); failure != nil {
		return failure
	}
	var moduleMap map[string]string
	if json.Unmarshal(data["configModuleMap"], &moduleMap) != nil || len(moduleMap) != 1 || moduleMap[runtime.Inventory.Ref] != runtime.Module.ID {
		return fail(CodeEnvelopeContract, "capture", "configModuleMap", "optional-absence module ownership is not exact")
	}
	var packageMap map[string][]string
	packageKey := runtime.Inventory.Driver + ":" + runtime.Inventory.Ref
	if json.Unmarshal(data["packageModuleMap"], &packageMap) != nil || len(packageMap) != 1 || !exactStrings(packageMap[packageKey], []string{runtime.Module.ID}) {
		return fail(CodeEnvelopeContract, "capture", "packageModuleMap", "optional-absence package ownership is not exact")
	}
	wantArtifact := "$ENDSTATE_ROOT/manifests/" + filepath.Base(zipPath)
	var outputPath, outputFormat string
	if json.Unmarshal(data["outputPath"], &outputPath) != nil || portableCapturePath(outputPath) != wantArtifact || json.Unmarshal(data["outputFormat"], &outputFormat) != nil || outputFormat != "zip" {
		return fail(CodeEnvelopeContract, "capture", "outputPath", "optional-absence artifact reference is not exact")
	}
	if !rawStringArrayEquals(data["configsIncluded"], []string{}) || !rawStringArrayEquals(data["configsSkipped"], []string{captureContractModuleName(runtime)}) ||
		!rawStringArrayEquals(data["configsCaptureErrors"], []string{}) || !rawStringArrayEquals(data["captureWarnings"], []string{}) {
		return fail(CodeEnvelopeContract, "capture", "configsSkipped", "optional-absence module was not exactly skipped")
	}
	var sanitized, isExample bool
	if json.Unmarshal(data["sanitized"], &sanitized) != nil || sanitized || json.Unmarshal(data["isExample"], &isExample) != nil || isExample {
		return fail(CodeEnvelopeContract, "capture", "flags", "optional-absence used a sanitized or example projection")
	}
	var counts struct{ FilteredRuntimes, Included, TotalFound, SensitiveExcludedCount, FilteredStoreApps, Skipped int }
	if json.Unmarshal(data["counts"], &counts) != nil || !rawHasExactFields(data["counts"], "filteredRuntimes", "included", "totalFound", "sensitiveExcludedCount", "filteredStoreApps", "skipped") ||
		counts.FilteredRuntimes != 0 || counts.Included != 1 || counts.TotalFound != 1 || counts.SensitiveExcludedCount != 0 || counts.FilteredStoreApps != 0 || counts.Skipped != 0 {
		return fail(CodeEnvelopeContract, "capture", "counts", "optional-absence package counts are not exact")
	}
	if failure := validateCaptureContractDeclaration(data["configCapture"], runtime); failure != nil {
		return failure
	}
	var manifestRef map[string]json.RawMessage
	var manifestValue struct{ Name, Path string }
	if json.Unmarshal(data["manifest"], &manifestRef) != nil || !exactRawFields(manifestRef, "name", "path") || json.Unmarshal(data["manifest"], &manifestValue) != nil || manifestValue.Name != "captured" || portableCapturePath(manifestValue.Path) != wantArtifact {
		return fail(CodeEnvelopeContract, "capture", "manifest", "optional-absence manifest reference is not exact")
	}
	if failure := validateCaptureContractEvents(events, runtime, wantArtifact); failure != nil {
		return failure
	}
	return validateOptionalCaptureArtifact(runtime, zipPath)
}

func validateOptionalCaptureModule(raw json.RawMessage, runtime *scenarioRuntime) *Failure {
	var values []map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != 1 || !exactRawFields(values[0], "displayName", "wingetRefs", "chocolateyRefs", "appId", "id", "paths", "filesCaptured", "status") {
		return fail(CodeEnvelopeContract, "capture", "configModules", "optional-absence module result is not exact")
	}
	var value struct {
		DisplayName, AppID, ID, Status    string
		WingetRefs, ChocolateyRefs, Paths []string
		FilesCaptured                     int
	}
	encoded, _ := json.Marshal(values[0])
	if json.Unmarshal(encoded, &value) != nil || value.DisplayName != runtime.Module.DisplayName || value.AppID != captureContractModuleName(runtime) || value.ID != runtime.Module.ID || value.Status != "skipped" ||
		value.FilesCaptured != 0 || !captureContractReferencesExact(value.WingetRefs, value.ChocolateyRefs, runtime) || len(value.Paths) != 0 {
		return fail(CodeEnvelopeContract, "capture", "configModules", "all-optional absence minted captured config evidence")
	}
	return nil
}

func validateOptionalCaptureArtifact(runtime *scenarioRuntime, zipPath string) *Failure {
	if runtime.validationContext() == nil || runtime.validationContext().ValidateSandboxPath(zipPath) != nil || !fixtureContained(runtime.Root, zipPath) {
		return fail(CodeIsolationFailure, "capture", "artifact", "optional-absence artifact left validation authority")
	}
	info, err := os.Lstat(zipPath)
	if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() || !captureContractArtifactNamesExact(zipPath, "manifest.jsonc", "metadata.json") {
		return fail(CodeArtifactContract, "capture", "artifact", "optional-absence artifact is not the exact two-file ZIP")
	}
	entries, failure := readCaptureArtifactEntries(zipPath)
	if failure != nil {
		return failure
	}
	if len(entries) != 2 {
		return fail(CodeArtifactContract, "capture", "artifact", "optional-absence artifact contains a config payload")
	}
	for _, data := range entries {
		if leaked(data, runtime.forbiddenOutputValues()...) {
			return fail(CodeArtifactContract, "capture", "artifact", "optional-absence artifact leaks validation authority")
		}
	}
	if failure := validateOptionalCaptureManifest(runtime, entries["manifest.jsonc"]); failure != nil {
		return failure
	}
	return validateOptionalCaptureMetadata(runtime, entries["metadata.json"])
}

func validateOptionalCaptureManifest(runtime *scenarioRuntime, raw []byte) *Failure {
	clean := manifest.StripJsoncComments(raw)
	var fields map[string]json.RawMessage
	if rejectDuplicateJSONFields(clean) != nil || json.Unmarshal(clean, &fields) != nil || !exactRawFields(fields, "version", "name", "captured", "apps") {
		return fail(CodeArtifactContract, "capture", "manifest", "optional-absence manifest contains a config projection or foreign field")
	}
	var version int
	var name, captured string
	var apps []map[string]json.RawMessage
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 || json.Unmarshal(fields["name"], &name) != nil || name != "captured" ||
		json.Unmarshal(fields["captured"], &captured) != nil || json.Unmarshal(fields["apps"], &apps) != nil || len(apps) != 1 || !exactRawFields(apps[0], "id", "refs", "displayName") {
		return fail(CodeArtifactContract, "capture", "manifest", "optional-absence manifest identity is not exact")
	}
	if _, err := time.Parse(time.RFC3339, captured); err != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "optional-absence manifest timestamp is invalid")
	}
	var app struct {
		ID, DisplayName string
		Refs            map[string]string
	}
	appRaw, _ := json.Marshal(apps[0])
	if json.Unmarshal(appRaw, &app) != nil || app.ID != runtime.Inventory.AppID || app.DisplayName != runtime.Inventory.DisplayName || len(app.Refs) != 1 || app.Refs["windows"] != runtime.Inventory.Ref {
		return fail(CodeArtifactContract, "capture", "manifest", "optional-absence app identity differs from inventory")
	}
	return nil
}

func validateOptionalCaptureMetadata(runtime *scenarioRuntime, raw []byte) *Failure {
	var fields map[string]json.RawMessage
	if rejectDuplicateJSONFields(raw) != nil || json.Unmarshal(raw, &fields) != nil || !exactRawFields(fields, "schemaVersion", "capturedAt", "machineName", "endstateVersion", "configModulesIncluded", "configModulesSkipped", "captureWarnings", "os") {
		return fail(CodeArtifactContract, "capture", "metadata", "optional-absence metadata has a foreign or absent field")
	}
	var value bundle.BundleMetadata
	if json.Unmarshal(raw, &value) != nil || value.SchemaVersion != "1.0" || value.OS != "windows" || value.MachineName == "" || value.EndstateVersion == "" || len(value.ConfigModulesIncluded) != 0 ||
		!exactStrings(value.ConfigModulesSkipped, []string{captureContractModuleName(runtime)}) || len(value.CaptureWarnings) != 0 {
		return fail(CodeArtifactContract, "capture", "metadata", "optional-absence metadata did not record the exact skipped module")
	}
	if _, err := time.Parse(time.RFC3339, value.CapturedAt); err != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "optional-absence metadata timestamp is invalid")
	}
	return nil
}
