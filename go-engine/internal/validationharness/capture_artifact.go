// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"archive/zip"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func inspectCaptureContractArtifact(runtime *scenarioRuntime, zipPath string) (captureContractEvidence, *Failure) {
	if runtime == nil || runtime.CapturePlan == nil || runtime.validationContext() == nil ||
		runtime.validationContext().ValidateSandboxPath(zipPath) != nil || !fixtureContained(runtime.Root, zipPath) {
		return captureContractEvidence{}, fail(CodeIsolationFailure, "capture", "artifact", "capture contract artifact left validation authority")
	}
	info, err := os.Lstat(zipPath)
	if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
		return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract artifact is absent, linked, or not regular")
	}
	entries, failure := readCaptureArtifactEntries(zipPath)
	if failure != nil {
		return captureContractEvidence{}, failure
	}
	if len(runtime.CapturePlan.Targets) == 0 || len(entries) != len(runtime.CapturePlan.Targets)+2 {
		return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP does not contain the exact payload set")
	}
	expectedPayloads := captureContractPayloadPaths(runtime)
	expectedPayloadKeys := make(map[string]struct{}, len(runtime.CapturePlan.Targets))
	for _, payload := range expectedPayloads {
		expectedPayloadKeys[strings.ToLower(payload)] = struct{}{}
	}
	if !captureContractArtifactNamesExact(zipPath, captureContractArtifactNames(runtime)...) {
		return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP member casing or portable names are not exact")
	}
	for name, data := range entries {
		if name != "manifest.jsonc" && name != "metadata.json" {
			if _, exists := expectedPayloadKeys[name]; !exists {
				return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP contains a foreign member")
			}
		}
		if leaked(data, runtime.forbiddenOutputValues()...) {
			return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP leaks validation authority")
		}
	}
	for _, target := range runtime.CapturePlan.Targets {
		payload, exists := entries[strings.ToLower(v1ArtifactPayloadPath(runtime.Module.ID, target.Destination))]
		if !exists || !reflect.DeepEqual(payload, target.Content) {
			return captureContractEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "capture contract ZIP lacks the exact deterministic payload")
		}
	}
	if failure := validateCaptureContractManifest(runtime, entries["manifest.jsonc"]); failure != nil {
		return captureContractEvidence{}, failure
	}
	if failure := validateCaptureContractMetadata(runtime, entries["metadata.json"]); failure != nil {
		return captureContractEvidence{}, failure
	}
	count := len(runtime.CapturePlan.Targets)
	return captureContractEvidence{ArtifactPath: zipPath, AssertionCounts: map[string]int{
		validationmatrix.AssertionCaptured: count, validationmatrix.AssertionContent: count,
		validationmatrix.AssertionPayload: count, validationmatrix.AssertionProvenance: count,
	}}, nil
}

func validateCaptureContractManifest(runtime *scenarioRuntime, raw []byte) *Failure {
	var fields map[string]json.RawMessage
	clean := manifest.StripJsoncComments(raw)
	wantFields := []string{"version", "name", "captured", "apps", "verify", "configModules"}
	if len(runtime.CapturePlan.Restores) != 0 {
		wantFields = append(wantFields, "restore")
	}
	if rejectDuplicateJSONFields(clean) != nil || json.Unmarshal(clean, &fields) != nil || !exactRawFields(fields, wantFields...) {
		return fail(CodeArtifactContract, "capture", "manifest", "capture contract manifest has a foreign, absent, or explicitly empty legacy field")
	}
	var version int
	var name, capturedAt string
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 || json.Unmarshal(fields["name"], &name) != nil || name != "captured" ||
		json.Unmarshal(fields["captured"], &capturedAt) != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture contract manifest identity is not exact schema v1")
	}
	if _, err := time.Parse(time.RFC3339, capturedAt); err != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture contract manifest timestamp is invalid")
	}
	var apps []map[string]json.RawMessage
	if json.Unmarshal(fields["apps"], &apps) != nil || len(apps) != 1 || !exactRawFields(apps[0], "id", "refs", "displayName") {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture contract app projection is not an exact singleton")
	}
	var app struct {
		ID, DisplayName string
		Refs            map[string]string
	}
	appRaw, _ := json.Marshal(apps[0])
	if json.Unmarshal(appRaw, &app) != nil || app.ID != runtime.Inventory.AppID || app.DisplayName != runtime.Inventory.DisplayName ||
		len(app.Refs) != 1 || app.Refs["windows"] != runtime.Inventory.Ref {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture contract app identity differs from validation inventory")
	}
	if !captureContractVerifierProjectionExact(fields["verify"], runtime) {
		return fail(CodeArtifactContract, "capture", "manifest.verify", "capture contract verifier differs from production")
	}
	var moduleIDs []string
	if json.Unmarshal(fields["configModules"], &moduleIDs) != nil || len(moduleIDs) != 1 || moduleIDs[0] != runtime.CapturePlan.ModuleID {
		return fail(CodeArtifactContract, "capture", "manifest.configModules", "capture contract module provenance differs from production")
	}
	if len(runtime.CapturePlan.Restores) != 0 && !captureContractRestoreProjectionExact(fields["restore"], runtime) {
		return fail(CodeArtifactContract, "capture", "manifest.restore", "capture contract restore projection differs from production")
	}
	return nil
}

func captureContractVerifierProjectionExact(raw json.RawMessage, runtime *scenarioRuntime) bool {
	if runtime == nil || runtime.CapturePlan == nil {
		return false
	}
	want := make([]manifest.VerifyEntry, 0, len(runtime.CapturePlan.Verifiers))
	for _, verifier := range runtime.CapturePlan.Verifiers {
		want = append(want, manifest.VerifyEntry{Type: verifier.Type, Command: verifier.Command, Path: verifier.Path, ValueName: verifier.ValueName, ValueType: verifier.ValueType, Data: verifier.Data})
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		return false
	}
	var gotValue, wantValue any
	return json.Unmarshal(raw, &gotValue) == nil && json.Unmarshal(encoded, &wantValue) == nil && reflect.DeepEqual(gotValue, wantValue)
}

func captureContractRestoreProjectionExact(raw json.RawMessage, runtime *scenarioRuntime) bool {
	if runtime == nil || runtime.CapturePlan == nil {
		return false
	}
	want := captureContractRestoreProjection(runtime)
	encoded, err := json.Marshal(want)
	if err != nil {
		return false
	}
	var gotValue, wantValue any
	return json.Unmarshal(raw, &gotValue) == nil && json.Unmarshal(encoded, &wantValue) == nil && reflect.DeepEqual(gotValue, wantValue)
}

func captureContractRestoreProjection(runtime *scenarioRuntime) []manifest.RestoreEntry {
	entries := make([]manifest.RestoreEntry, 0, len(runtime.CapturePlan.Restores))
	for _, restore := range runtime.CapturePlan.Restores {
		entries = append(entries, manifest.RestoreEntry{
			Type: restore.Type, Source: v1RestoreSource(runtime.Module.ID, payloadDestination(restore.Source)), Target: restore.Target,
			Backup: restore.Backup, Optional: restore.Optional, FromModule: runtime.Module.ID,
		})
	}
	sort.SliceStable(entries, func(left, right int) bool {
		return strings.Join([]string{entries[left].Type, entries[left].Source, entries[left].Target}, "\x00") < strings.Join([]string{entries[right].Type, entries[right].Source, entries[right].Target}, "\x00")
	})
	return entries
}

func validateCaptureContractMetadata(runtime *scenarioRuntime, raw []byte) *Failure {
	var fields map[string]json.RawMessage
	if rejectDuplicateJSONFields(raw) != nil || json.Unmarshal(raw, &fields) != nil || !exactRawFields(fields, "schemaVersion", "capturedAt", "machineName", "endstateVersion", "configModulesIncluded", "configModulesSkipped", "captureWarnings", "os") {
		return fail(CodeArtifactContract, "capture", "metadata", "capture contract metadata has a foreign or absent field")
	}
	var metadata bundle.BundleMetadata
	if json.Unmarshal(raw, &metadata) != nil || metadata.SchemaVersion != "1.0" || metadata.ManifestVersion != 0 || metadata.OS != "windows" ||
		metadata.MachineName == "" || metadata.EndstateVersion == "" || !exactStrings(metadata.ConfigModulesIncluded, []string{captureContractModuleName(runtime)}) ||
		len(metadata.ConfigModulesSkipped) != 0 || len(metadata.CaptureWarnings) != 0 || len(metadata.ConfigCapturesIncluded) != 0 || metadata.Share || metadata.Redaction != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "capture contract metadata identity or provenance is not exact")
	}
	if _, err := time.Parse(time.RFC3339, metadata.CapturedAt); err != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "capture contract metadata timestamp is invalid")
	}
	return nil
}

func captureContractModuleName(runtime *scenarioRuntime) string {
	return strings.TrimPrefix(runtime.Module.ID, "apps.")
}

func captureContractArtifactNames(runtime *scenarioRuntime) []string {
	moduleDirectory := "configs/" + captureContractModuleName(runtime) + "/"
	payloads := append([]string(nil), captureContractPayloadPaths(runtime)...)
	sort.Strings(payloads)
	return append(append([]string{"configs/", moduleDirectory}, payloads...), "manifest.jsonc", "metadata.json")
}

func captureContractArtifactNamesExact(zipPath string, expected ...string) bool {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return false
	}
	defer reader.Close()
	if len(reader.File) != len(expected) {
		return false
	}
	for index, file := range reader.File {
		if file.Name != expected[index] {
			return false
		}
	}
	return true
}
