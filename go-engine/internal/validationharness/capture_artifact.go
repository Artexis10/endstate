// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
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
	if len(runtime.CapturePlan.Targets) != 1 || len(entries) != 3 {
		return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP is not the exact three-file mGBA artifact")
	}
	expectedPayload := strings.ToLower("configs/" + strings.TrimPrefix(filepath.ToSlash(runtime.CapturePlan.Targets[0].Destination), "apps/"))
	if !captureContractArtifactNamesExact(zipPath, "configs/", "configs/mgba/", "manifest.jsonc", "metadata.json", expectedPayload) {
		return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP member casing or portable names are not exact")
	}
	for name, data := range entries {
		if name != "manifest.jsonc" && name != "metadata.json" && name != expectedPayload {
			return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP contains a foreign member")
		}
		if leaked(data, runtime.forbiddenOutputValues()...) {
			return captureContractEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture contract ZIP leaks validation authority")
		}
	}
	target := runtime.CapturePlan.Targets[0]
	payload, exists := entries[expectedPayload]
	if !exists || string(payload) != string(target.Content) {
		return captureContractEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "capture contract ZIP lacks the exact deterministic mGBA payload")
	}
	if failure := validateCaptureContractManifest(runtime, entries["manifest.jsonc"]); failure != nil {
		return captureContractEvidence{}, failure
	}
	if failure := validateCaptureContractMetadata(runtime, entries["metadata.json"]); failure != nil {
		return captureContractEvidence{}, failure
	}
	return captureContractEvidence{ArtifactPath: zipPath, AssertionCounts: map[string]int{
		validationmatrix.AssertionCaptured: 1, validationmatrix.AssertionContent: 1,
		validationmatrix.AssertionPayload: 1, validationmatrix.AssertionProvenance: 1,
	}}, nil
}

func validateCaptureContractManifest(runtime *scenarioRuntime, raw []byte) *Failure {
	var fields map[string]json.RawMessage
	clean := manifest.StripJsoncComments(raw)
	if rejectDuplicateJSONFields(clean) != nil || json.Unmarshal(clean, &fields) != nil || !exactRawFields(fields, "version", "name", "captured", "apps", "verify", "configModules") {
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
	if json.Unmarshal(fields["apps"], &apps) != nil || len(apps) != 1 || !exactRawFields(apps[0], "id", "refs") {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture contract app projection is not an exact singleton")
	}
	var appID string
	var refs map[string]string
	if json.Unmarshal(apps[0]["id"], &appID) != nil || appID != runtime.Inventory.AppID || json.Unmarshal(apps[0]["refs"], &refs) != nil ||
		len(refs) != 1 || refs["windows"] != runtime.Inventory.Ref {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture contract app identity differs from validation inventory")
	}
	var verifiers []map[string]json.RawMessage
	if json.Unmarshal(fields["verify"], &verifiers) != nil || len(verifiers) != 1 || !exactRawFields(verifiers[0], "type", "path") {
		return fail(CodeArtifactContract, "capture", "manifest.verify", "capture contract verifier projection is not exact")
	}
	var verifier struct{ Type, Path string }
	verifierRaw, _ := json.Marshal(verifiers[0])
	if json.Unmarshal(verifierRaw, &verifier) != nil || verifier.Type != runtime.CapturePlan.Verifiers[0].Type || verifier.Path != runtime.CapturePlan.Verifiers[0].Path {
		return fail(CodeArtifactContract, "capture", "manifest.verify", "capture contract verifier differs from production")
	}
	var moduleIDs []string
	if json.Unmarshal(fields["configModules"], &moduleIDs) != nil || len(moduleIDs) != 1 || moduleIDs[0] != runtime.CapturePlan.ModuleID {
		return fail(CodeArtifactContract, "capture", "manifest.configModules", "capture contract module provenance differs from production")
	}
	return nil
}

func validateCaptureContractMetadata(runtime *scenarioRuntime, raw []byte) *Failure {
	var fields map[string]json.RawMessage
	if rejectDuplicateJSONFields(raw) != nil || json.Unmarshal(raw, &fields) != nil || !exactRawFields(fields, "schemaVersion", "capturedAt", "machineName", "endstateVersion", "configModulesIncluded", "configModulesSkipped", "captureWarnings", "os") {
		return fail(CodeArtifactContract, "capture", "metadata", "capture contract metadata has a foreign or absent field")
	}
	var metadata bundle.BundleMetadata
	if json.Unmarshal(raw, &metadata) != nil || metadata.SchemaVersion != "1.0" || metadata.ManifestVersion != 0 || metadata.OS != "windows" ||
		metadata.MachineName == "" || metadata.EndstateVersion == "" || !exactStrings(metadata.ConfigModulesIncluded, []string{strings.TrimPrefix(runtime.Module.ID, "apps.")}) ||
		len(metadata.ConfigModulesSkipped) != 0 || len(metadata.CaptureWarnings) != 0 || len(metadata.ConfigCapturesIncluded) != 0 || metadata.Share || metadata.Redaction != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "capture contract metadata identity or provenance is not exact")
	}
	if _, err := time.Parse(time.RFC3339, metadata.CapturedAt); err != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "capture contract metadata timestamp is invalid")
	}
	return nil
}

func captureContractArtifactNamesExact(zipPath string, expected ...string) bool {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return false
	}
	defer reader.Close()
	want := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		want[name] = struct{}{}
	}
	seen := map[string]struct{}{}
	for _, file := range reader.File {
		if _, ok := want[file.Name]; !ok {
			return false
		}
		if _, duplicate := seen[file.Name]; duplicate {
			return false
		}
		seen[file.Name] = struct{}{}
	}
	return len(seen) == len(want)
}
