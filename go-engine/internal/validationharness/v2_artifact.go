// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type v2CaptureEvidence struct {
	Capture manifest.ConfigCapture
	Entries map[string][]byte
}

func inspectV2CaptureArtifact(runtime *scenarioRuntime, zipPath string, captureEnvelope []byte) (captureEvidence, *Failure) {
	if runtime == nil || runtime.V2Plan == nil || runtime.validationContext().ValidateSandboxPath(zipPath) != nil {
		return captureEvidence{}, fail(CodeIsolationFailure, "capture", "artifact", "schema-v2 capture artifact left validation authority")
	}
	entries, failure := readCaptureArtifactEntries(zipPath)
	if failure != nil {
		return captureEvidence{}, failure
	}
	manifestBytes := entries["manifest.jsonc"]
	if len(manifestBytes) == 0 || leaked(manifestBytes, runtime.forbiddenOutputValues()...) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "manifest", "pure-v2 manifest is absent or leaks physical fixture authority")
	}
	var rawManifest map[string]json.RawMessage
	if json.Unmarshal(manifest.StripJsoncComments(manifestBytes), &rawManifest) != nil {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "manifest", "pure-v2 manifest is malformed")
	}
	for field := range rawManifest {
		for _, forbiddenField := range []string{"restore", "configModules", "legacyConfigLanes"} {
			if strings.EqualFold(field, forbiddenField) {
				return captureEvidence{}, fail(CodeArtifactContract, "capture", "manifest", "pure-v2 legacy fields must be absent, not merely empty")
			}
		}
	}
	var captured manifest.Manifest
	if failure := decodeCapturedManifest(manifestBytes, &captured); failure != nil {
		return captureEvidence{}, failure
	}
	version, versionOK := captured.Version.(float64)
	if !versionOK || version != 2 || len(captured.Restore) != 0 || len(captured.ConfigModules) != 0 || len(captured.LegacyConfigLanes) != 0 || len(captured.ConfigCaptures) != 1 {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "manifest", fmt.Sprintf("artifact is not exact pure-v2: version=%T:%v restore=%d modules=%d legacy=%d captures=%d %s", captured.Version, captured.Version, len(captured.Restore), len(captured.ConfigModules), len(captured.LegacyConfigLanes), len(captured.ConfigCaptures), v2CaptureFailureSummary(captureEnvelope)))
	}
	if len(captured.Apps) != 1 {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "manifest.apps", "pure-v2 capture must select exactly one app")
	}
	app := captured.Apps[0]
	if app.ID != runtime.Inventory.AppID || app.Refs["windows"] != runtime.Inventory.Ref || !strings.EqualFold(defaultDriver(app.Driver), runtime.Inventory.Driver) || app.Version != "" || !capturedAppSourceMatches(app, runtime.Inventory) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "manifest.apps", "captured app identity is not the exact unpinned validation inventory")
	}
	capture := captured.ConfigCaptures[0]
	plan := runtime.V2Plan
	instance := plan.Instance
	if capture.CaptureID != plan.CaptureID || capture.ModuleID != runtime.Module.ID || capture.ConfigSetID != plan.Compiled.Set.ID ||
		capture.SourceInstance.ID != instance.ID || capture.SourceInstance.DetectorID != instance.DetectorID ||
		capture.SourceInstance.RawVersion != instance.Version.Raw || capture.SourceInstance.NormalizedVersion != instance.Version.Normalized ||
		capture.SourceGeneration != plan.Compiled.Generation.ID || capture.SourceGenerationFingerprint != plan.Compiled.Generation.Fingerprint ||
		capture.CaptureModule.SchemaVersion != 2 || capture.CaptureModule.ContentHash != runtime.Module.Revision ||
		capture.PayloadRoot != bundle.ConfigPayloadRoot(runtime.Module.ID, plan.CaptureID) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "configCaptures[0]", "config capture identity or immutable generation provenance differs")
	}
	if !exactV2SourceEvidence(capture.SourceInstance.Evidence, instance) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "configCaptures[0].sourceInstance.evidence", "portable detector evidence differs or leaks a physical root")
	}
	snapshotPath := filepath.ToSlash(capture.CaptureModule.SnapshotPath)
	if !strings.HasPrefix(snapshotPath, "provenance/modules/") || entries[strings.ToLower(snapshotPath)] == nil || !bytes.Equal(entries[strings.ToLower(snapshotPath)], runtime.Module.CanonicalSnapshot()) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "configCaptures[0].captureModule", "frozen production module snapshot is absent or byte-inexact")
	}

	expectedPayload := map[string][]byte{}
	for _, target := range plan.CaptureTargets {
		for _, member := range target.Members {
			relative := target.Destination
			if target.Directory {
				relative = strings.TrimSuffix(relative, "/") + "/" + filepath.ToSlash(member.Relative)
			}
			expectedPayload[filepath.ToSlash(relative)] = member.Captured
		}
		for _, excluded := range target.Excluded {
			if len(excluded.CapturePatterns) == 0 {
				expectedPayload[strings.TrimSuffix(filepath.ToSlash(target.Destination), "/")+"/"+filepath.ToSlash(excluded.Relative)] = excluded.Captured
			}
		}
	}
	if len(capture.PayloadManifest) != len(expectedPayload) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "configCaptures[0].payloadManifest", "payload manifest count differs from the exact fixture tree")
	}
	seenPayload := map[string]struct{}{}
	previous := ""
	for _, item := range capture.PayloadManifest {
		want, ok := expectedPayload[item.RelativePath]
		digest := sha256.Sum256(want)
		zipName := strings.ToLower(capture.PayloadRoot + "/" + filepath.ToSlash(item.RelativePath))
		if !ok || previous != "" && item.RelativePath <= previous || item.Size != int64(len(want)) || item.SHA256 != hex.EncodeToString(digest[:]) || !bytes.Equal(entries[zipName], want) {
			return captureEvidence{}, fail(CodeArtifactContract, "capture", "configCaptures[0].payloadManifest", "payload member bytes, digest, size, or order differ")
		}
		previous = item.RelativePath
		seenPayload[item.RelativePath] = struct{}{}
	}
	if len(seenPayload) != len(expectedPayload) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "configs", "payload manifest omitted a fixture member")
	}
	allowed := map[string]struct{}{"manifest.jsonc": {}, "metadata.json": {}, strings.ToLower(snapshotPath): {}}
	for relative := range expectedPayload {
		allowed[strings.ToLower(capture.PayloadRoot+"/"+relative)] = struct{}{}
	}
	for name := range entries {
		if _, ok := allowed[name]; !ok {
			return captureEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "pure-v2 bundle contains a foreign member")
		}
	}
	if failure := validateV2Metadata(entries["metadata.json"], plan.CaptureID); failure != nil {
		return captureEvidence{}, failure
	}
	if failure := validateV2CaptureEnvelope(captureEnvelope, runtime, capture); failure != nil {
		return captureEvidence{}, failure
	}

	verifyManifest := filepath.Join(runtime.Root, "manifests", "captured-verify.jsonc")
	if err := runtime.validationContext().ValidateSandboxPath(verifyManifest); err != nil {
		return captureEvidence{}, fail(CodeIsolationFailure, "capture", "verifyManifest", "verify manifest left validation authority")
	}
	if err := writeV2VerifyManifest(verifyManifest, manifestBytes); err != nil {
		return captureEvidence{}, fail(CodeIsolationFailure, "capture", "verifyManifest", "persist pure-v2 verify manifest")
	}
	counts := map[string]int{
		validationmatrix.AssertionCaptured: len(expectedPayload), validationmatrix.AssertionPayload: len(expectedPayload),
		validationmatrix.AssertionProvenance: 1, validationmatrix.AssertionRewrittenRestore: len(plan.Targets),
		validationmatrix.AssertionGeneration: 1,
		validationmatrix.AssertionValidation: plan.MigrationValidations + plan.Validations,
	}
	if plan.Compiled.Migration != nil {
		counts[validationmatrix.AssertionMigration] = 1
	}
	return captureEvidence{ArtifactPath: zipPath, VerifyManifest: verifyManifest, AssertionCounts: counts, V2: &v2CaptureEvidence{Capture: capture, Entries: entries}}, nil
}

func v2CaptureFailureSummary(raw []byte) string {
	var data struct {
		ConfigCapture struct {
			ConfigSets []struct {
				Status        string  `json:"status"`
				Reason        *string `json:"reason"`
				FilesCaptured int     `json:"filesCaptured"`
			} `json:"configSets"`
			Counts      struct{ Total, Captured, Skipped, Failed int } `json:"counts"`
			Diagnostics []struct{ Code, Status string }                `json:"diagnostics"`
		} `json:"configCapture"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return "captureEnvelope=malformed"
	}
	return fmt.Sprintf("configSets=%v counts=%+v diagnostics=%v", data.ConfigCapture.ConfigSets, data.ConfigCapture.Counts, data.ConfigCapture.Diagnostics)
}

func defaultDriver(value string) string {
	if value == "" {
		return "winget"
	}
	return value
}

func exactV2SourceEvidence(evidence *manifest.ConfigSourceInstanceEvidence, instance modules.ConfigInstance) bool {
	if evidence == nil || evidence.Type != instance.Evidence.Type {
		return false
	}
	if evidence.Type == "path" {
		return evidence.AppID == "" && evidence.Backend == "" && evidence.Platform == "" && evidence.Ref == "" && evidence.Driver == ""
	}
	return evidence.AppID == instance.Evidence.AppID && evidence.Backend == instance.Evidence.Backend && evidence.Platform == instance.Evidence.Platform && evidence.Ref == instance.Evidence.Ref && evidence.Driver == instance.Evidence.Driver
}

func validateV2Metadata(raw []byte, captureID string) *Failure {
	var metadata bundle.BundleMetadata
	if json.Unmarshal(raw, &metadata) != nil || metadata.SchemaVersion != "2.0" || metadata.ManifestVersion != 2 ||
		!exactStrings(metadata.ConfigCapturesIncluded, []string{captureID}) || len(metadata.ConfigModulesIncluded) != 0 || len(metadata.ConfigModulesSkipped) != 0 || metadata.OS != "windows" || metadata.Share || metadata.Redaction != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "pure-v2 bundle metadata differs from the exact generation contract")
	}
	if _, err := time.Parse(time.RFC3339, metadata.CapturedAt); err != nil {
		return fail(CodeArtifactContract, "capture", "metadata.capturedAt", "bundle capture timestamp is invalid")
	}
	return nil
}

func validateV2CaptureEnvelope(raw []byte, runtime *scenarioRuntime, capture manifest.ConfigCapture) *Failure {
	var data struct {
		OutputFormat        string `json:"outputFormat"`
		BundleSchemaVersion string `json:"bundleSchemaVersion"`
		ManifestVersion     int    `json:"manifestVersion"`
		ConfigCapture       struct {
			ConfigSets []struct {
				CaptureID                   string  `json:"captureId"`
				ModuleID                    string  `json:"moduleId"`
				ConfigSetID                 string  `json:"configSetId"`
				SourceGeneration            string  `json:"sourceGeneration"`
				SourceGenerationFingerprint string  `json:"sourceGenerationFingerprint"`
				CaptureModuleRevision       string  `json:"captureModuleRevision"`
				FilesCaptured               int     `json:"filesCaptured"`
				Status                      string  `json:"status"`
				Reason                      *string `json:"reason"`
			} `json:"configSets"`
			Counts      struct{ Total, Captured, Skipped, Failed int } `json:"counts"`
			Diagnostics []any                                          `json:"diagnostics"`
		} `json:"configCapture"`
	}
	if json.Unmarshal(raw, &data) != nil || data.OutputFormat != "zip" || data.BundleSchemaVersion != "2.0" || data.ManifestVersion != 2 || len(data.ConfigCapture.ConfigSets) != 1 {
		return fail(CodeEnvelopeContract, "capture", "data.configCapture", "capture envelope lacks one exact schema-v2 result")
	}
	row := data.ConfigCapture.ConfigSets[0]
	if row.CaptureID != capture.CaptureID || row.ModuleID != runtime.Module.ID || row.ConfigSetID != runtime.V2Plan.Compiled.Set.ID || row.SourceGeneration != runtime.V2Plan.Compiled.Generation.ID || row.SourceGenerationFingerprint != runtime.V2Plan.Compiled.Generation.Fingerprint || row.CaptureModuleRevision != runtime.Module.Revision || row.FilesCaptured != len(capture.PayloadManifest) || row.Status != "captured" || row.Reason != nil || data.ConfigCapture.Counts.Total != 1 || data.ConfigCapture.Counts.Captured != 1 || data.ConfigCapture.Counts.Skipped != 0 || data.ConfigCapture.Counts.Failed != 0 || len(data.ConfigCapture.Diagnostics) != 0 {
		return fail(CodeEnvelopeContract, "capture", "data.configCapture", "capture envelope generation evidence is incomplete or inconsistent")
	}
	return nil
}

func writeV2VerifyManifest(path string, data []byte) error {
	// Keep the write helper narrow so artifact tests can substitute malformed
	// bytes without weakening ordinary manifest parsing.
	return safepath.AtomicWriteFile(path, data, 0o600)
}
