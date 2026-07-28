// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

const (
	liveConfigRoundtripScenarioID = "live-config-roundtrip"
	maxLiveDecodeOutputBytes      = 256 * 1024
	maxLiveDecodeEvents           = 128
)

// liveCommandOutput keeps each live journey phase independently bounded. It
// contains untrusted bytes only; no decoder projection returns these values.
type liveCommandOutput struct {
	Stdout []byte
	Stderr []byte
}

type liveJourneyOutputs struct {
	ScenarioID            string
	InitialApply          liveCommandOutput
	Verify                liveCommandOutput
	Capture               liveCommandOutput
	RestoreRebuild        liveCommandOutput
	Revert                liveCommandOutput
	RecoveryRebuild       liveCommandOutput
	PackageAfterRevert    PackageObservation
	ConvergenceRebuild    liveCommandOutput
	runtimeRestoreTargets map[string]string
}

// liveRuntimeRestoreAuthority binds the six production restore identities to
// their already-resolved host targets. It is private runner state: nil keeps
// the validation-mode semantic wire contract intact.
type liveRuntimeRestoreAuthority struct{ targets map[string]string }

// liveJourneyProjection is deliberately summary-only: host paths, setting
// content, journal names, raw command output, and credentials stay internal.
type liveJourneyProjection struct {
	ModuleID                  string
	Ref                       string
	CapturedMappings          int
	RestoredMappings          int
	PackagePresentAfterRevert bool
	// ConvergenceEnvelopeObserved is only an envelope claim. A later state
	// cross-binding must establish whether it involved no persistent mutation.
	ConvergenceEnvelopeObserved bool
}

type liveJourneyProof struct {
	restoreRebuild, recoveryRebuild, convergenceRebuild liveRebuildProof
	revert                                              commands.RevertData
	revertJournal                                       string
}

type liveRebuildProof struct {
	envelopeRunID string
	applyRunID    string
	bundle        commands.RebuildBundleInfo
	resolution    planner.ConfigResolution
	restoreItems  []restore.RestoreResult
}

type liveEnvelope struct {
	SchemaVersion string          `json:"schemaVersion"`
	CLIVersion    string          `json:"cliVersion"`
	Command       string          `json:"command"`
	RunID         string          `json:"runId"`
	TimestampUTC  string          `json:"timestampUtc"`
	Success       bool            `json:"success"`
	TestMode      json.RawMessage `json:"testMode"`
	Data          json.RawMessage `json:"data"`
	Error         json.RawMessage `json:"error"`
}

// liveRebuildWire preserves RebuildResult's interface-valued Apply and Verify
// until their raw proof inventories have been checked. The official command
// structs are decoded immediately afterwards; this private wrapper is not a
// second public result schema.
type liveRebuildWire struct {
	From                    string                          `json:"from"`
	Bundle                  json.RawMessage                 `json:"bundle"`
	DryRun                  bool                            `json:"dryRun"`
	Restore                 string                          `json:"restore"`
	Apply                   json.RawMessage                 `json:"apply"`
	Verify                  json.RawMessage                 `json:"verify"`
	ConfigResolutions       []planner.ConfigResolution      `json:"configResolutions"`
	ConfigResolutionSummary planner.ConfigResolutionSummary `json:"configResolutionSummary"`
	RestoreItems            []restore.RestoreResult         `json:"restoreItems"`
}

func decodeLiveJourney(definition LiveDefinition, inputs liveJourneyOutputs) (liveJourneyProjection, *Failure) {
	projection, _, failure := decodeLiveJourneyWithProof(definition, inputs)
	return projection, failure
}

func decodeLiveJourneyWithProof(definition LiveDefinition, inputs liveJourneyOutputs) (liveJourneyProjection, liveJourneyProof, *Failure) {
	if !validLiveModuleID(definition.ModuleID) || !validLiveObserverValue(definition.WingetRef) || len(definition.Comparator.Mappings) == 0 || len(definition.Comparator.Mappings) > maxLiveMappings || inputs.ScenarioID != liveConfigRoundtripScenarioID {
		return liveJourneyProjection{}, liveJourneyProof{}, fail(CodeEnvelopeContract, "live", "identity", "live journey identity is invalid")
	}
	expectedMappings, ok := liveExpectedMappings(definition.Comparator.Mappings)
	if !ok {
		return liveJourneyProjection{}, liveJourneyProof{}, fail(CodeEnvelopeContract, "live", "mappings", "live comparison mappings are invalid")
	}
	runtimeRestore, ok := newLiveRuntimeRestoreAuthority(definition, inputs.runtimeRestoreTargets)
	if !ok {
		return liveJourneyProjection{}, liveJourneyProof{}, fail(CodeEnvelopeContract, "live", "restore", "live runtime restore authority is invalid")
	}
	if failure := validateLiveApply(inputs.InitialApply, definition, false); failure != nil {
		return liveJourneyProjection{}, liveJourneyProof{}, failure
	}
	if failure := validateLiveVerify(inputs.Verify, definition, len(expectedMappings)); failure != nil {
		return liveJourneyProjection{}, liveJourneyProof{}, failure
	}
	capturedMappings, failure := validateLiveCapture(inputs.Capture, definition, expectedMappings)
	if failure != nil {
		return liveJourneyProjection{}, liveJourneyProof{}, failure
	}
	restoreRebuild, failure := validateLiveRebuild(inputs.RestoreRebuild, definition, capturedMappings, true, false, runtimeRestore)
	if failure != nil {
		return liveJourneyProjection{}, liveJourneyProof{}, failure
	}
	revert, failure := validateLiveRevert(inputs.Revert, definition, capturedMappings, "revert", runtimeRestore)
	if failure != nil {
		return liveJourneyProjection{}, liveJourneyProof{}, failure
	}
	recoveryRebuild, failure := validateLiveRebuild(inputs.RecoveryRebuild, definition, capturedMappings, false, false, runtimeRestore)
	if failure != nil {
		return liveJourneyProjection{}, liveJourneyProof{}, failure
	}
	if inputs.PackageAfterRevert.Ref != definition.WingetRef || inputs.PackageAfterRevert.Status != "present" || !validLiveObserverValue(inputs.PackageAfterRevert.Version) && inputs.PackageAfterRevert.Version != "" {
		return liveJourneyProjection{}, liveJourneyProof{}, fail(CodeEnvelopeContract, "revert", "package", "configuration revert lacks a present package observation")
	}
	convergenceRebuild, failure := validateLiveRebuild(inputs.ConvergenceRebuild, definition, capturedMappings, false, true, runtimeRestore)
	if failure != nil {
		return liveJourneyProjection{}, liveJourneyProof{}, failure
	}
	return liveJourneyProjection{
		ModuleID: definition.ModuleID, Ref: definition.WingetRef, CapturedMappings: len(capturedMappings), RestoredMappings: len(capturedMappings),
		PackagePresentAfterRevert: true, ConvergenceEnvelopeObserved: true,
	}, liveJourneyProof{restoreRebuild: restoreRebuild, revert: revert, revertJournal: revert.JournalUsed, recoveryRebuild: recoveryRebuild, convergenceRebuild: convergenceRebuild}, nil
}

func validateLiveApply(output liveCommandOutput, definition LiveDefinition, converged bool) *Failure {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "apply")
	if failure != nil {
		return failure
	}
	if failure := decodeLiveEvents(output.Stderr, "apply", envelope.RunID); failure != nil {
		return failure
	}
	data, err := liveObjectAllowed(envelope.Data, []string{"dryRun", "manifest", "summary", "actions"}, []string{"dryRun", "manifest", "summary", "actions", "configModuleMap", "packageModuleMap", "warnings", "restoreModulesAvailable", "pruned", "configResolutions", "configResolutionSummary", "restoreItems"})
	if err != nil {
		return liveDecodeFailure("apply", "data")
	}
	var official commands.ApplyResult
	if json.Unmarshal(envelope.Data, &official) != nil {
		return liveDecodeFailure("apply", "data")
	}
	if converged {
		if _, err := liveArray(data["pruned"]); err != nil {
			return liveDecodeFailure("apply", "convergence")
		}
	}
	if _, err := liveObject(data["manifest"], "path", "name", "hash"); err != nil {
		return liveDecodeFailure("apply", "manifest")
	}
	var dryRun bool
	if json.Unmarshal(data["dryRun"], &dryRun) != nil || dryRun {
		return liveDecodeFailure("apply", "dryRun")
	}
	if !liveApplySummary(data["summary"], converged) {
		return liveDecodeFailure("apply", "summary")
	}
	actions, err := liveArray(data["actions"])
	if err != nil || len(actions) != 1 {
		return liveDecodeFailure("apply", "actions")
	}
	action, err := liveObjectAllowed(actions[0], []string{"id", "ref", "driver", "status", "manual"}, []string{"id", "ref", "driver", "status", "manual", "source", "name", "reason", "message", "version", "rebootRequired"})
	if err != nil {
		return liveDecodeFailure("apply", "actions")
	}
	var id, ref, driver, status, reason string
	if liveString(action["id"], &id) != nil || liveString(action["ref"], &ref) != nil || liveString(action["driver"], &driver) != nil || liveString(action["status"], &status) != nil || liveOptionalMapString(action, "reason", &reason) != nil || !liveNull(action["manual"]) ||
		id != liveManifestAppID(definition.WingetRef) || ref != definition.WingetRef || !strings.EqualFold(driver, "winget") {
		return liveDecodeFailure("apply", "actions")
	}
	if converged && (status != "present" || reason != "already_installed") || !converged && (status != "installed" || reason != "") {
		return liveDecodeFailure("apply", "actions")
	}
	return nil
}

func validateLiveVerify(output liveCommandOutput, definition LiveDefinition, _ int) *Failure {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "verify")
	if failure != nil {
		return failure
	}
	if failure := decodeLiveEvents(output.Stderr, "verify", envelope.RunID); failure != nil {
		return failure
	}
	return validateLiveVerifyData(envelope.Data, definition, 0)
}

func validateLiveVerifyData(raw json.RawMessage, definition LiveDefinition, _ int) *Failure {
	data, err := liveObjectAllowed(raw, []string{"manifest", "summary", "results"}, []string{"manifest", "summary", "results", "warnings"})
	if err != nil {
		return liveDecodeFailure("verify", "data")
	}
	var official commands.VerifyResult
	if json.Unmarshal(raw, &official) != nil {
		return liveDecodeFailure("verify", "data")
	}
	if _, err := liveObject(data["manifest"], "path", "name"); err != nil {
		return liveDecodeFailure("verify", "manifest")
	}
	if _, err := liveObjectAllowed(data["summary"], []string{"total", "pass", "fail"}, []string{"total", "pass", "fail", "skipped"}); err != nil {
		return liveDecodeFailure("verify", "summary")
	}
	var summary struct{ Total, Pass, Fail, Skipped int }
	if json.Unmarshal(data["summary"], &summary) != nil || summary.Total != 1 || summary.Pass != 1 || summary.Fail != 0 || summary.Skipped != 0 {
		return liveDecodeFailure("verify", "summary")
	}
	results, err := liveArray(data["results"])
	if err != nil || len(results) != summary.Total {
		return liveDecodeFailure("verify", "results")
	}
	app, err := liveObjectAllowed(results[0], []string{"type", "id", "ref", "driver", "status"}, []string{"type", "id", "ref", "driver", "name", "status", "version"})
	if err != nil {
		return liveDecodeFailure("verify", "results")
	}
	var kind, id, ref, driver, status, name, version string
	if liveString(app["type"], &kind) != nil || liveString(app["id"], &id) != nil || liveString(app["ref"], &ref) != nil || liveString(app["driver"], &driver) != nil || liveString(app["status"], &status) != nil ||
		liveOptionalMapString(app, "name", &name) != nil || liveOptionalMapString(app, "version", &version) != nil || kind != "app" || id != liveManifestAppID(definition.WingetRef) || ref != definition.WingetRef || !strings.EqualFold(driver, "winget") || status != "pass" {
		return liveDecodeFailure("verify", "results")
	}
	return nil
}

func validateLiveCapture(output liveCommandOutput, definition LiveDefinition, expected map[string]struct{}) (map[string]struct{}, *Failure) {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "capture")
	if failure != nil {
		return nil, failure
	}
	if failure := decodeLiveEvents(output.Stderr, "capture", envelope.RunID); failure != nil {
		return nil, failure
	}
	data, err := liveObjectAllowed(envelope.Data, []string{"appsIncluded", "configModules", "configModuleMap", "packageModuleMap", "outputPath", "outputFormat", "configsIncluded", "configsSkipped", "configsCaptureErrors", "sanitized", "isExample", "counts", "captureWarnings", "manifest"}, []string{"appsIncluded", "configModules", "configModuleMap", "packageModuleMap", "warnings", "outputPath", "outputFormat", "configsIncluded", "configsSkipped", "configsCaptureErrors", "sanitized", "isExample", "counts", "bundleSchemaVersion", "manifestVersion", "captureWarnings", "configCapture", "manifest"})
	if err != nil {
		return nil, liveDecodeFailure("capture", "data")
	}
	var official commands.CaptureResult
	if json.Unmarshal(envelope.Data, &official) != nil {
		return nil, liveDecodeFailure("capture", "data")
	}
	apps, err := liveArray(data["appsIncluded"])
	modules, modulesErr := liveArray(data["configModules"])
	if err != nil || modulesErr != nil || len(apps) != 1 || len(modules) != 1 {
		return nil, liveDecodeFailure("capture", "modules")
	}
	app, err := liveObjectAllowed(apps[0], []string{"source", "id", "manifestId"}, []string{"source", "name", "id", "manifestId"})
	if err != nil {
		return nil, liveDecodeFailure("capture", "apps")
	}
	var appRef, appID string
	if liveString(app["id"], &appRef) != nil || liveString(app["manifestId"], &appID) != nil || appRef != definition.WingetRef || appID != liveManifestAppID(definition.WingetRef) {
		return nil, liveDecodeFailure("capture", "apps")
	}
	module, err := liveObjectAllowed(modules[0], []string{"displayName", "wingetRefs", "chocolateyRefs", "appId", "id", "paths", "filesCaptured", "status"}, []string{"displayName", "wingetRefs", "chocolateyRefs", "appId", "id", "paths", "filesCaptured", "status", "warnings", "errors"})
	if err != nil {
		return nil, liveDecodeFailure("capture", "modules")
	}
	var moduleID, moduleAppID, status string
	var captured int
	capturedMappings, ok := liveCapturedMappings(module["paths"], expected)
	if liveString(module["id"], &moduleID) != nil || liveString(module["appId"], &moduleAppID) != nil || liveString(module["status"], &status) != nil || json.Unmarshal(module["filesCaptured"], &captured) != nil || moduleID != definition.ModuleID || moduleAppID != liveAppID(definition.ModuleID) || status != "captured" || captured != len(capturedMappings) || captured < definition.Comparator.MinimumExistingMappings || captured == 0 || !ok || !liveExactStrings(module["wingetRefs"], []string{definition.WingetRef}) {
		return nil, liveDecodeFailure("capture", "modules")
	}
	if _, err := liveObject(data["counts"], "filteredRuntimes", "included", "totalFound", "sensitiveExcludedCount", "filteredStoreApps", "skipped"); err != nil {
		return nil, liveDecodeFailure("capture", "counts")
	}
	var counts struct{ Included, Skipped, TotalFound int }
	if json.Unmarshal(data["counts"], &counts) != nil || counts.Included != 1 || counts.Skipped != 0 || counts.TotalFound < 1 || !liveExactStrings(data["configsIncluded"], []string{liveAppID(definition.ModuleID)}) || !liveEmptyArray(data["configsSkipped"]) || !liveEmptyArray(data["configsCaptureErrors"]) {
		return nil, liveDecodeFailure("capture", "counts")
	}
	return capturedMappings, nil
}

func validateLiveRebuild(output liveCommandOutput, definition LiveDefinition, expected map[string]struct{}, requireInstall, converged bool, runtimeRestore *liveRuntimeRestoreAuthority) (liveRebuildProof, *Failure) {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "rebuild")
	if failure != nil {
		return liveRebuildProof{}, failure
	}
	records, failure := decodeLiveEventRecords(output.Stderr, "rebuild", envelope.RunID)
	if failure != nil || len(records) == 0 {
		return liveRebuildProof{}, liveDecodeFailure("rebuild", "events")
	}
	data, err := liveObjectAllowed(envelope.Data, []string{"from", "dryRun", "restore", "apply", "configResolutionSummary", "configResolutions", "restoreItems", "verify"}, []string{"from", "bundle", "dryRun", "restore", "apply", "configResolutionSummary", "configResolutions", "restoreItems", "verify"})
	if err != nil {
		return liveRebuildProof{}, liveDecodeFailure("rebuild", "data")
	}
	var wire liveRebuildWire
	var official commands.RebuildResult
	var officialApply commands.ApplyResult
	var officialVerify commands.VerifyResult
	if json.Unmarshal(envelope.Data, &wire) != nil || json.Unmarshal(envelope.Data, &official) != nil || json.Unmarshal(wire.Apply, &officialApply) != nil || json.Unmarshal(wire.Verify, &officialVerify) != nil || official.Bundle == nil {
		return liveRebuildProof{}, liveDecodeFailure("rebuild", "data")
	}
	if !validateLiveRebuildBundle(data, official.Bundle) {
		return liveRebuildProof{}, liveDecodeFailure("rebuild", "bundle")
	}
	nested, err := liveObjectAllowed(wire.Apply, []string{"dryRun", "manifest", "summary", "actions", "configResolutions", "configResolutionSummary", "restoreItems"}, []string{"dryRun", "manifest", "summary", "actions", "configModuleMap", "packageModuleMap", "warnings", "restoreModulesAvailable", "pruned", "configResolutions", "configResolutionSummary", "restoreItems"})
	if err != nil || officialApply.ConfigResultFields == nil || !reflect.DeepEqual(commands.ConfigResultFields{
		ConfigResolutions: wire.ConfigResolutions, ConfigResolutionSummary: wire.ConfigResolutionSummary, RestoreItems: wire.RestoreItems,
	}, *officialApply.ConfigResultFields) {
		return liveRebuildProof{}, liveDecodeFailure("rebuild", "config")
	}
	var dryRun bool
	var restore string
	if json.Unmarshal(data["dryRun"], &dryRun) != nil || dryRun || liveString(data["restore"], &restore) != nil || restore != "enabled" {
		return liveRebuildProof{}, liveDecodeFailure("rebuild", "restore")
	}
	if failure := validateLiveApplyData(data["apply"], definition, !requireInstall, false); failure != nil {
		return liveRebuildProof{}, failure
	}
	if !validateLiveConfigFields(data, definition, expected, converged, runtimeRestore) || !validateLiveConfigFields(nested, definition, expected, converged, runtimeRestore) {
		return liveRebuildProof{}, liveDecodeFailure("rebuild", "config")
	}
	if failure := validateLiveVerifyData(data["verify"], definition, len(expected)); failure != nil {
		return liveRebuildProof{}, failure
	}
	return liveRebuildProof{envelopeRunID: envelope.RunID, applyRunID: records[0].runID, bundle: *official.Bundle, resolution: cloneLiveConfigResolution(wire.ConfigResolutions[0]), restoreItems: cloneLiveRestoreItems(wire.RestoreItems)}, nil
}

func validateLiveRevert(output liveCommandOutput, definition LiveDefinition, expected map[string]struct{}, phase string, runtimeRestore *liveRuntimeRestoreAuthority) (commands.RevertData, *Failure) {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "revert")
	if failure != nil {
		return commands.RevertData{}, failure
	}
	if failure := decodeLiveEvents(output.Stderr, "revert", envelope.RunID); failure != nil {
		return commands.RevertData{}, failure
	}
	data, err := liveObject(envelope.Data, "journalUsed", "results")
	if err != nil {
		return commands.RevertData{}, liveDecodeFailure(phase, "data")
	}
	var official commands.RevertData
	if json.Unmarshal(envelope.Data, &official) != nil {
		return commands.RevertData{}, liveDecodeFailure(phase, "data")
	}
	var journal string
	if liveString(data["journalUsed"], &journal) != nil || journal == "" || !filepath.IsAbs(journal) || filepath.Clean(journal) != journal || official.JournalUsed != journal {
		return commands.RevertData{}, liveDecodeFailure(phase, "journal")
	}
	inventory, ok := liveRestoreInventory(definition)
	if !ok || !liveActiveRestoreSubset(expected, inventory) {
		return commands.RevertData{}, liveDecodeFailure(phase, "results")
	}
	expectedTargets := liveActiveRestoreTargets(expected, inventory)
	if runtimeRestore != nil {
		expectedTargets = liveActiveRuntimeRestoreTargets(expected, inventory, runtimeRestore)
		if len(expectedTargets) != len(expected) {
			return commands.RevertData{}, liveDecodeFailure(phase, "results")
		}
	}
	results, err := liveArray(data["results"])
	if err != nil || len(results) != len(expectedTargets) {
		return commands.RevertData{}, liveDecodeFailure(phase, "results")
	}
	seen := make(map[string]struct{}, len(results))
	for _, raw := range results {
		item, err := liveObjectAllowed(raw, []string{"target", "action"}, []string{"target", "action", "backupUsed"})
		if err != nil {
			return commands.RevertData{}, liveDecodeFailure(phase, "results")
		}
		var target, action string
		if liveString(item["target"], &target) != nil || liveString(item["action"], &action) != nil || action != "deleted" || item["backupUsed"] != nil {
			return commands.RevertData{}, liveDecodeFailure(phase, "results")
		}
		if _, ok := expectedTargets[target]; !ok {
			return commands.RevertData{}, liveDecodeFailure(phase, "results")
		}
		if _, duplicate := seen[target]; duplicate {
			return commands.RevertData{}, liveDecodeFailure(phase, "results")
		}
		seen[target] = struct{}{}
	}
	if !liveSameStringSet(seen, expectedTargets) {
		return commands.RevertData{}, liveDecodeFailure(phase, "results")
	}
	return cloneLiveRevertData(official), nil
}

func validateLiveRebuildBundle(data map[string]json.RawMessage, official *commands.RebuildBundleInfo) bool {
	raw, ok := data["bundle"]
	if !ok || official == nil {
		return false
	}
	fields, err := liveObject(raw, "extracted", "schemaVersion", "capturedAt", "machineName", "endstateVersion", "configModulesIncluded")
	if err != nil {
		return false
	}
	var bundle commands.RebuildBundleInfo
	if json.Unmarshal(raw, &bundle) != nil || !reflect.DeepEqual(bundle, *official) || !bundle.Extracted || liveString(fields["schemaVersion"], &bundle.SchemaVersion) != nil || liveString(fields["machineName"], &bundle.MachineName) != nil || liveString(fields["endstateVersion"], &bundle.EndstateVersion) != nil {
		return false
	}
	if _, err := time.Parse(time.RFC3339, bundle.CapturedAt); err != nil {
		return false
	}
	if !liveExactStrings(fields["configModulesIncluded"], []string{"notepad-plus-plus"}) {
		return false
	}
	return true
}

func cloneLiveConfigResolution(value planner.ConfigResolution) planner.ConfigResolution {
	raw, err := json.Marshal(value)
	if err != nil {
		return planner.ConfigResolution{}
	}
	var copy planner.ConfigResolution
	if json.Unmarshal(raw, &copy) != nil {
		return planner.ConfigResolution{}
	}
	return copy
}

func cloneLiveRestoreItems(values []restore.RestoreResult) []restore.RestoreResult {
	copy := append([]restore.RestoreResult(nil), values...)
	for index := range copy {
		copy[index].Warnings = append([]string(nil), values[index].Warnings...)
	}
	return copy
}

func cloneLiveRevertData(value commands.RevertData) commands.RevertData {
	value.Results = append([]restore.RevertResult(nil), value.Results...)
	return value
}

func validateLiveConfigFields(data map[string]json.RawMessage, definition LiveDefinition, expected map[string]struct{}, converged bool, runtimeRestore *liveRuntimeRestoreAuthority) bool {
	if _, err := liveObject(data["configResolutionSummary"], "total", "direct", "migrate", "incompatible", "unknown", "legacyUnverified", "selected", "skipped", "failed"); err != nil {
		return false
	}
	var summary planner.ConfigResolutionSummary
	if json.Unmarshal(data["configResolutionSummary"], &summary) != nil || summary.Total != 1 || summary.Direct != 0 || summary.Migrate != 0 || summary.Incompatible != 0 || summary.Unknown != 0 || summary.LegacyUnverified != 1 || summary.Failed != 0 ||
		(!converged && (summary.Selected != 1 || summary.Skipped != 0)) || (converged && (summary.Selected != 0 || summary.Skipped != 1)) {
		return false
	}
	resolutions, err := liveArray(data["configResolutions"])
	if err != nil || len(resolutions) != 1 {
		return false
	}
	resolution, err := liveObjectAllowed(resolutions[0], []string{"captureId", "moduleId", "configSetId", "targetCandidates", "resolution", "reason", "migrationPath", "resolvedTargets", "status", "label", "message", "remediation"}, []string{"captureId", "moduleId", "configSetId", "sourceInstance", "sourceInstanceId", "targetInstanceId", "targetCandidates", "sourceGeneration", "sourceGenerationFingerprint", "targetGeneration", "resolution", "reason", "migrationPath", "captureModuleRevision", "restoreModuleRevision", "resolvedTargets", "label", "message", "remediation", "status"})
	if err != nil || !validateLiveResolutionSubobjects(resolution) {
		return false
	}
	var moduleID, status, kind string
	if liveString(resolution["moduleId"], &moduleID) != nil || liveString(resolution["status"], &status) != nil || liveString(resolution["resolution"], &kind) != nil || moduleID != definition.ModuleID || kind != "legacy_unverified" || !liveEmptyArray(resolution["resolvedTargets"]) ||
		(!converged && (status != "restored" || !liveNull(resolution["reason"]))) || (converged && (status != "skipped" || !liveStringEquals(resolution["reason"], "already_up_to_date"))) {
		return false
	}
	inventory, ok := liveRestoreInventory(definition)
	if !ok || !liveActiveRestoreSubset(expected, inventory) {
		return false
	}
	items, err := liveArray(data["restoreItems"])
	if err != nil || len(items) != len(inventory) {
		return false
	}
	var runtimeOrder []string
	if runtimeRestore != nil {
		runtimeOrder, ok = liveRuntimeRestoreOrder(definition, inventory)
		if !ok {
			return false
		}
	}
	seen := make(map[string]struct{}, len(items))
	extractionRoot := ""
	for index, raw := range items {
		item, err := liveObjectAllowed(raw, []string{"id", "source", "target", "status", "backupCreated", "targetExistedBefore"}, []string{"id", "source", "target", "status", "backupPath", "backupCreated", "targetExistedBefore", "restoreType", "warnings", "error", "captureId", "configSetId", "targetInstanceId", "sourceGeneration", "targetGeneration"})
		if err != nil {
			return false
		}
		var id, source, target, itemStatus, restoreType, backupPath string
		var backup, existed bool
		if liveString(item["id"], &id) != nil || liveString(item["source"], &source) != nil || liveString(item["target"], &target) != nil || liveString(item["status"], &itemStatus) != nil || json.Unmarshal(item["backupCreated"], &backup) != nil || json.Unmarshal(item["targetExistedBefore"], &existed) != nil || id == "" {
			return false
		}
		if rawBackupPath, exists := item["backupPath"]; exists && liveOptionalString(rawBackupPath, &backupPath) != nil {
			return false
		}
		semanticSource := source
		expectedItem, found := inventory[source]
		wantTarget := expectedItem.target
		if runtimeRestore != nil {
			var root string
			semanticSource, expectedItem, root, found = liveRuntimeRestoreSource(inventory, source)
			wantTarget = runtimeRestore.targets[expectedItem.identity]
			if !found || wantTarget == "" || runtimeOrder[index] != semanticSource || (extractionRoot != "" && !strings.EqualFold(extractionRoot, root)) {
				return false
			}
			extractionRoot = root
		}
		_, active := expected[expectedItem.identity]
		wantStatus := "skipped_missing_source"
		wantExisted := false
		if active {
			wantStatus = "restored"
			if converged {
				wantStatus = "skipped_up_to_date"
				wantExisted = true
			}
		}
		if !found || target != wantTarget || itemStatus != wantStatus || backup || backupPath != "" || existed != wantExisted {
			return false
		}
		if rawType, ok := item["restoreType"]; ok && (liveString(rawType, &restoreType) != nil || restoreType != "copy") {
			return false
		}
		if _, duplicate := seen[semanticSource]; duplicate {
			return false
		}
		seen[semanticSource] = struct{}{}
	}
	if runtimeRestore != nil {
		return extractionRoot != "" && liveSameStringSet(seen, liveRestoreSourceSet(inventory))
	}
	return liveSameStringSet(seen, liveRestoreSourceSet(inventory))
}

func decodeLiveEnvelope(stdout []byte, command string) (liveEnvelope, *Failure) {
	if len(stdout) == 0 || len(stdout) > maxLiveDecodeOutputBytes || rejectDuplicateJSONKeys(stdout) != nil {
		return liveEnvelope{}, liveDecodeFailure(command, "stdout")
	}
	_, err := liveObject(stdout, "schemaVersion", "cliVersion", "command", "runId", "timestampUtc", "success", "data", "error")
	if err != nil {
		return liveEnvelope{}, liveDecodeFailure(command, "stdout")
	}
	var envelope liveEnvelope
	if err := json.Unmarshal(stdout, &envelope); err != nil || envelope.SchemaVersion == "" || envelope.CLIVersion == "" || envelope.Command != command || !validLiveObserverValue(envelope.RunID) || !envelope.Success || len(envelope.Data) == 0 || !liveNull(envelope.Error) {
		return liveEnvelope{}, liveDecodeFailure(command, "envelope")
	}
	if _, err := time.Parse(time.RFC3339, envelope.TimestampUTC); err != nil {
		return liveEnvelope{}, liveDecodeFailure(command, "timestamp")
	}
	return envelope, nil
}

func decodeLiveEvents(stderr []byte, command, runID string) *Failure {
	_, failure := decodeLiveEventRecords(stderr, command, runID)
	return failure
}

func decodeLiveEventRecords(stderr []byte, command, runID string) ([]liveEventRecord, *Failure) {
	if len(stderr) == 0 || len(stderr) > maxLiveDecodeOutputBytes || !bytes.HasSuffix(stderr, []byte("\n")) {
		return nil, liveDecodeFailure(command, "events")
	}
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	scanner.Buffer(make([]byte, 1024), 64*1024)
	count := 0
	var records []liveEventRecord
	for scanner.Scan() {
		line := scanner.Bytes()
		count++
		if count > maxLiveDecodeEvents || len(line) == 0 || rejectDuplicateJSONKeys(line) != nil {
			return nil, liveDecodeFailure(command, "events")
		}
		record, ok := decodeLiveEvent(line)
		if !ok {
			return nil, liveDecodeFailure(command, "events")
		}
		records = append(records, record)
	}
	if scanner.Err() != nil || !liveEventTopology(records, command, runID) {
		return nil, liveDecodeFailure(command, "events")
	}
	return records, nil
}

// projectLiveCaptureClaims derives the transient path-bearing artifact claims
// from the existing official capture envelope/event wire and sealed receipt.
// It does not consume the receipt; final proof still consumes it atomically.
func projectLiveCaptureClaims(issuer *liveReceiptIssuer, definition LiveDefinition, module *modules.Module, receipt *liveExecutionReceipt, sequence uint64, nonce [32]byte, hostname, osName string) (liveCaptureArtifactClaims, *Failure) {
	if module == nil || module.ID != definition.ModuleID || module.Revision != definition.ModuleRevision || !validLiveObserverValue(hostname) || !validLiveObserverValue(osName) {
		return liveCaptureArtifactClaims{}, fail(CodeArtifactContract, "capture", "claims", "capture projection identity is invalid")
	}
	receipt, ok := projectLiveCaptureReceipt(issuer, receipt, sequence, nonce)
	if !ok {
		return liveCaptureArtifactClaims{}, fail(CodeEnvelopeContract, "capture", "receipt", "capture receipt projection is unavailable")
	}
	envelope, failure := decodeLiveEnvelope(receipt.stdout, "capture")
	if failure != nil {
		return liveCaptureArtifactClaims{}, failure
	}
	var captured commands.CaptureResult
	if json.Unmarshal(envelope.Data, &captured) != nil || captured.OutputPath == "" || captured.Manifest.Path == "" || !sameLiveArtifactPath(captured.OutputPath, captured.Manifest.Path) {
		return liveCaptureArtifactClaims{}, liveDecodeFailure("capture", "data")
	}
	expectedMappings, mappingsOK := liveExpectedMappings(definition.Comparator.Mappings)
	_, captureFailure := validateLiveCapture(liveCommandOutput{Stdout: receipt.stdout, Stderr: receipt.stderr}, definition, expectedMappings)
	if !mappingsOK || captureFailure != nil {
		return liveCaptureArtifactClaims{}, liveDecodeFailure("capture", "data")
	}
	records, failure := decodeLiveEventRecords(receipt.stderr, "capture", envelope.RunID)
	if failure != nil {
		return liveCaptureArtifactClaims{}, failure
	}
	var artifact *events.ArtifactEvent
	for _, record := range records {
		if record.artifact == nil {
			continue
		}
		if artifact != nil {
			return liveCaptureArtifactClaims{}, fail(CodeArtifactContract, "capture", "event", "capture emitted more than one artifact event")
		}
		artifact = record.artifact
	}
	out, ok := liveCaptureOutputArgument(receipt.args)
	if !ok || artifact == nil || !sameLiveArtifactPathClaims(captured.OutputPath, liveCaptureArtifactClaims{OutputPath: captured.OutputPath, EventPath: artifact.Path, Receipt: liveReceiptArtifactPathClaim{Path: out}}) {
		return liveCaptureArtifactClaims{}, fail(CodeArtifactContract, "capture", "artifact", "capture output claims disagree")
	}
	if !liveCaptureTimestampWithinReceipt(envelope.TimestampUTC, receipt.created, receipt.finished) {
		return liveCaptureArtifactClaims{}, fail(CodeArtifactContract, "capture", "timestamp", "capture envelope timestamp is outside the sealed receipt")
	}
	return liveCaptureArtifactClaims{OutputPath: captured.OutputPath, EventPath: artifact.Path, Receipt: liveReceiptArtifactPathClaim{Path: out}, ModuleRevision: module.Revision, MachineName: hostname, ReceiptCreated: receipt.created, ReceiptFinished: receipt.finished, EndstateVersion: envelope.CLIVersion, OS: osName, RestoreProjection: cloneLiveRestoreProjection(module.Restore), VerifyProjection: append([]modules.VerifyDef(nil), module.Verify...)}, nil
}

func liveCaptureTimestampWithinReceipt(raw string, created, finished time.Time) bool {
	timestamp, err := time.Parse(time.RFC3339, raw)
	if err != nil || finished.Before(created) {
		return false
	}
	if strings.Contains(raw, ".") {
		return !timestamp.Before(created) && !timestamp.After(finished)
	}
	return !finished.Before(timestamp) && created.Before(timestamp.Add(time.Second))
}

func cloneLiveRestoreProjection(source []modules.RestoreDef) []modules.RestoreDef {
	result := append([]modules.RestoreDef(nil), source...)
	for index := range result {
		result[index].Exclude = append([]string(nil), source[index].Exclude...)
	}
	return result
}

func liveCaptureOutputArgument(args []string) (string, bool) {
	var output string
	for index := 0; index < len(args); index++ {
		if args[index] != "--out" {
			continue
		}
		if index+1 >= len(args) || output != "" {
			return "", false
		}
		output = args[index+1]
	}
	_, ok := canonicalLiveArtifactPath(output)
	return output, ok
}

type liveEventRecord struct {
	runID    string
	event    string
	phase    string
	artifact *events.ArtifactEvent
}

func decodeLiveEvent(raw []byte) (liveEventRecord, bool) {
	fields, err := liveOpenObject(raw)
	if err != nil {
		return liveEventRecord{}, false
	}
	var version int
	var runID, timestamp, event string
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 || liveString(fields["runId"], &runID) != nil || liveString(fields["timestamp"], &timestamp) != nil || liveString(fields["event"], &event) != nil {
		return liveEventRecord{}, false
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return liveEventRecord{}, false
	}
	record := liveEventRecord{runID: runID, event: event}
	base := []string{"version", "runId", "timestamp", "event"}
	switch event {
	case "phase":
		if !liveExactKeySet(fields, append(base, "phase")...) || liveString(fields["phase"], &record.phase) != nil {
			return liveEventRecord{}, false
		}
		var official events.PhaseEvent
		if json.Unmarshal(raw, &official) != nil {
			return liveEventRecord{}, false
		}
	case "summary":
		if !liveExactKeySet(fields, append(base, "phase", "total", "success", "skipped", "failed")...) || liveString(fields["phase"], &record.phase) != nil {
			return liveEventRecord{}, false
		}
		var summary struct{ Total, Success, Skipped, Failed int }
		if json.Unmarshal(raw, &summary) != nil || summary.Total < 0 || summary.Success < 0 || summary.Skipped < 0 || summary.Failed < 0 || summary.Total != summary.Success+summary.Skipped+summary.Failed {
			return liveEventRecord{}, false
		}
		var official events.SummaryEvent
		if json.Unmarshal(raw, &official) != nil {
			return liveEventRecord{}, false
		}
	case "progress":
		if !liveExactKeySet(fields, append(base, "phase", "stage")...) || liveString(fields["phase"], &record.phase) != nil || liveString(fields["stage"], new(string)) != nil {
			return liveEventRecord{}, false
		}
		var official events.ProgressEvent
		if json.Unmarshal(raw, &official) != nil {
			return liveEventRecord{}, false
		}
	case "item":
		if !liveEventKeys(fields, append(base, "id", "driver", "status", "reason"), append(base, "id", "driver", "name", "status", "reason", "message", "rebootRequired")) || liveString(fields["id"], new(string)) != nil || liveString(fields["driver"], new(string)) != nil || liveString(fields["status"], new(string)) != nil || liveOptionalMapString(fields, "reason", new(string)) != nil {
			return liveEventRecord{}, false
		}
		var official events.ItemEvent
		if json.Unmarshal(raw, &official) != nil {
			return liveEventRecord{}, false
		}
	case "artifact":
		if !liveExactKeySet(fields, append(base, "phase", "kind", "path")...) || liveString(fields["phase"], &record.phase) != nil || liveString(fields["kind"], new(string)) != nil || liveOptionalString(fields["path"], new(string)) != nil {
			return liveEventRecord{}, false
		}
		var official events.ArtifactEvent
		if json.Unmarshal(raw, &official) != nil {
			return liveEventRecord{}, false
		}
		if official.Phase == "capture" && official.Kind == "manifest" && official.Path != "" {
			record.artifact = &official
		}
	case "error":
		if !liveEventKeys(fields, append(base, "scope", "message"), append(base, "scope", "message", "id")) || liveString(fields["scope"], new(string)) != nil || liveOptionalString(fields["message"], new(string)) != nil {
			return liveEventRecord{}, false
		}
		var official events.ErrorEvent
		if json.Unmarshal(raw, &official) != nil {
			return liveEventRecord{}, false
		}
	case "config-resolution":
		if !liveEventKeys(fields, append(base, "captureId", "moduleId", "configSetId", "targetCandidates", "resolution", "reason", "migrationPath", "label", "message", "remediation"), append(base, "captureId", "moduleId", "configSetId", "sourceInstance", "sourceInstanceId", "targetInstanceId", "targetCandidates", "sourceGeneration", "sourceGenerationFingerprint", "targetGeneration", "resolution", "reason", "migrationPath", "captureModuleRevision", "restoreModuleRevision", "label", "message", "remediation")) {
			return liveEventRecord{}, false
		}
		if !validateLiveResolutionSubobjects(fields) {
			return liveEventRecord{}, false
		}
		var official events.ConfigResolutionEvent
		if json.Unmarshal(raw, &official) != nil || !liveResolutionValue(official.Resolution) || !liveResolutionReason(official.Reason) {
			return liveEventRecord{}, false
		}
	case "config-migration":
		if !liveEventKeys(fields, append(base, "captureId", "configSetId", "stage", "status", "reason", "message", "remediation"), append(base, "captureId", "configSetId", "stage", "fromGeneration", "toGeneration", "status", "reason", "message", "remediation")) {
			return liveEventRecord{}, false
		}
		if liveString(fields["captureId"], new(string)) != nil || liveString(fields["configSetId"], new(string)) != nil {
			return liveEventRecord{}, false
		}
		var official events.ConfigMigrationEvent
		if json.Unmarshal(raw, &official) != nil || !liveMigrationStage(official.Stage) || !liveMigrationStatus(official.Status) || !liveNullableNonempty(fields["reason"]) {
			return liveEventRecord{}, false
		}
	case "restore-item":
		if !liveEventKeys(fields, append(base, "id", "module", "restorer", "source", "target", "status", "reason", "backupPath", "targetExisted", "message"), append(base, "id", "module", "restorer", "source", "target", "status", "reason", "backupPath", "targetExisted", "message", "captureId", "configSetId", "targetInstanceId", "sourceGeneration", "targetGeneration")) {
			return liveEventRecord{}, false
		}
		for _, key := range []string{"id", "module", "restorer", "source", "target", "status", "message"} {
			if liveString(fields[key], new(string)) != nil {
				return liveEventRecord{}, false
			}
		}
		var official events.RestoreItemEvent
		if json.Unmarshal(raw, &official) != nil || !liveRestoreItemStatus(official.Status) || !liveNullableNonempty(fields["reason"]) {
			return liveEventRecord{}, false
		}
	default:
		return liveEventRecord{}, false
	}
	return record, true
}

func liveResolutionValue(value planner.Resolution) bool {
	return value == planner.ResolutionDirect || value == planner.ResolutionMigrate || value == planner.ResolutionIncompatible || value == planner.ResolutionUnknown || value == planner.ResolutionLegacyUnverified
}
func liveMigrationStage(value events.ConfigMigrationStage) bool {
	return value == events.ConfigMigrationStaging || value == events.ConfigMigrationEdge || value == events.ConfigMigrationValidation || value == events.ConfigMigrationCommit || value == events.ConfigMigrationRollback
}
func liveMigrationStatus(value events.ConfigProgressStatus) bool {
	return value == events.ConfigProgressStarted || value == events.ConfigProgressCompleted || value == events.ConfigProgressFailed
}
func liveRestoreItemStatus(value events.RestoreItemStatus) bool {
	return value == events.RestoreItemRestoring || value == events.RestoreItemRestored || value == events.RestoreItemSkippedUpToDate || value == events.RestoreItemSkippedMissingSource || value == events.RestoreItemFailed
}

func liveResolutionReason(value *planner.ResolutionReason) bool {
	if value == nil {
		return true
	}
	switch *value {
	case planner.ReasonUnknownGeneration, planner.ReasonAmbiguousGeneration, planner.ReasonDowngradeUnsupported,
		planner.ReasonMigrationPathMissing, planner.ReasonAmbiguousTargetInstance, planner.ReasonTargetNotDetected,
		planner.ReasonMappedTargetNotDetected, planner.ReasonMappedTargetIncompatible, planner.ReasonTargetCollision,
		planner.ReasonPayloadIntegrityFailed, planner.ReasonUnsupportedModuleSchema, planner.ReasonCatalogModuleMissing,
		planner.ReasonConfigSetMissing, planner.ReasonSourceGenerationUnknown, planner.ReasonSourceGenerationDefinitionChanged,
		planner.ReasonAppRunning, planner.ReasonRecoveryRequired, planner.ReasonRestoreFiltered, planner.ReasonRestoreNotEnabled,
		planner.ReasonTargetDetectionFailed, planner.ReasonStagingValidationFailed, planner.ReasonBackupFailed,
		planner.ReasonJournalIntentFailed, planner.ReasonCommitFailed, planner.ReasonTargetValidationFailed,
		planner.ReasonJournalCompletionFailed, planner.ReasonAlreadyUpToDate:
		return true
	default:
		return false
	}
}

func validateLiveResolutionSubobjects(fields map[string]json.RawMessage) bool {
	for _, key := range []string{"captureId", "moduleId", "configSetId"} {
		if liveString(fields[key], new(string)) != nil {
			return false
		}
	}
	if raw, ok := fields["sourceInstance"]; ok && !liveNull(raw) && !validateLiveSourceInstance(raw) {
		return false
	}
	targets, err := liveArray(fields["targetCandidates"])
	if err != nil {
		return false
	}
	for _, raw := range targets {
		if !validateLiveTargetInstance(raw) {
			return false
		}
	}
	return true
}

func validateLiveSourceInstance(raw json.RawMessage) bool {
	fields, err := liveObject(raw, "id", "detectorId", "rawVersion", "normalizedVersion", "evidence")
	if err != nil {
		return false
	}
	for _, key := range []string{"id", "detectorId", "rawVersion", "normalizedVersion"} {
		if liveString(fields[key], new(string)) != nil {
			return false
		}
	}
	return validateLiveInstanceEvidence(fields["evidence"])
}

func validateLiveTargetInstance(raw json.RawMessage) bool {
	fields, err := liveObjectAllowed(raw, []string{"id", "moduleId", "detectorId", "rawVersion", "normalizedVersion", "evidence", "restoreModuleRevision"}, []string{"id", "moduleId", "detectorId", "rawVersion", "normalizedVersion", "evidence", "targetGeneration", "targetGenerationFingerprint", "restoreModuleRevision"})
	if err != nil {
		return false
	}
	for _, key := range []string{"id", "moduleId", "detectorId", "rawVersion", "normalizedVersion", "restoreModuleRevision"} {
		if liveString(fields[key], new(string)) != nil {
			return false
		}
	}
	return validateLiveInstanceEvidence(fields["evidence"])
}

func validateLiveInstanceEvidence(raw json.RawMessage) bool {
	fields, err := liveObjectAllowed(raw, []string{"type"}, []string{"type", "appId", "backend", "platform", "ref", "driver"})
	if err != nil || liveString(fields["type"], new(string)) != nil {
		return false
	}
	for _, key := range []string{"appId", "backend", "platform", "ref", "driver"} {
		if value, ok := fields[key]; ok && liveString(value, new(string)) != nil {
			return false
		}
	}
	return true
}

func liveNullableNonempty(raw json.RawMessage) bool {
	return liveNull(raw) || liveString(raw, new(string)) == nil
}

func liveEventKeys(fields map[string]json.RawMessage, required, allowed []string) bool {
	if len(fields) < len(required) {
		return false
	}
	permitted := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		permitted[key] = struct{}{}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	for key := range fields {
		if _, ok := permitted[key]; !ok {
			return false
		}
	}
	return true
}

func liveEventTopology(records []liveEventRecord, command, envelopeRunID string) bool {
	if len(records) == 0 {
		return false
	}
	segments := make([][]liveEventRecord, 0, 2)
	for _, record := range records {
		if len(segments) == 0 || segments[len(segments)-1][0].runID != record.runID {
			segments = append(segments, []liveEventRecord{record})
		} else {
			segments[len(segments)-1] = append(segments[len(segments)-1], record)
		}
	}
	expected := map[string][][]string{"apply": {{"plan", "apply", "verify"}}, "verify": {{"verify"}}, "capture": {{"capture"}}, "revert": {{"restore"}}, "rebuild": {{"plan", "apply", "restore", "verify"}, {"verify"}}}[command]
	if len(expected) == 0 || len(segments) != len(expected) {
		return false
	}
	for index, segment := range segments {
		if command == "rebuild" {
			if segment[0].runID == envelopeRunID || (index == 0 && !strings.HasPrefix(segment[0].runID, "apply-")) || (index == 1 && !strings.HasPrefix(segment[0].runID, "verify-")) {
				return false
			}
		} else if !strings.HasPrefix(segment[0].runID, command+"-") {
			return false
		}
		if !liveEventSegment(segment, expected[index]) {
			return false
		}
	}
	return true
}

func liveEventSegment(records []liveEventRecord, phases []string) bool {
	phaseIndex, open := 0, ""
	for _, record := range records {
		switch record.event {
		case "phase":
			if open != "" || phaseIndex >= len(phases) || record.phase != phases[phaseIndex] {
				return false
			}
			open = record.phase
		case "summary":
			if open == "" || record.phase != open {
				return false
			}
			open = ""
			phaseIndex++
		default:
			if open == "" || (record.phase != "" && record.phase != open) {
				return false
			}
		}
	}
	return open == "" && phaseIndex == len(phases)
}

func validateLiveApplyData(raw json.RawMessage, definition LiveDefinition, converged, includePruned bool) *Failure {
	var data map[string]json.RawMessage
	var err error
	data, err = liveObjectAllowed(raw, []string{"dryRun", "manifest", "summary", "actions"}, []string{"dryRun", "manifest", "summary", "actions", "configModuleMap", "packageModuleMap", "warnings", "restoreModulesAvailable", "pruned", "configResolutions", "configResolutionSummary", "restoreItems"})
	if err != nil {
		return liveDecodeFailure("rebuild", "apply")
	}
	if includePruned && !liveEmptyArray(data["pruned"]) {
		return liveDecodeFailure("rebuild", "apply")
	}
	if _, err := liveObject(data["manifest"], "path", "name", "hash"); err != nil {
		return liveDecodeFailure("rebuild", "apply")
	}
	var dryRun bool
	if json.Unmarshal(data["dryRun"], &dryRun) != nil || dryRun || !liveApplySummary(data["summary"], converged) {
		return liveDecodeFailure("rebuild", "apply")
	}
	actions, err := liveArray(data["actions"])
	if err != nil || len(actions) != 1 {
		return liveDecodeFailure("rebuild", "apply")
	}
	action, err := liveObjectAllowed(actions[0], []string{"id", "ref", "driver", "status", "manual"}, []string{"id", "ref", "driver", "status", "manual", "source", "name", "reason", "message", "version", "rebootRequired"})
	if err != nil {
		return liveDecodeFailure("rebuild", "apply")
	}
	var id, ref, driver, status, reason string
	if liveString(action["id"], &id) != nil || liveString(action["ref"], &ref) != nil || liveString(action["driver"], &driver) != nil || liveString(action["status"], &status) != nil || liveOptionalMapString(action, "reason", &reason) != nil || !liveNull(action["manual"]) || id != liveManifestAppID(definition.WingetRef) || ref != definition.WingetRef || !strings.EqualFold(driver, "winget") ||
		(converged && (status != "present" || reason != "already_installed")) || (!converged && (status != "installed" || reason != "")) {
		return liveDecodeFailure("rebuild", "apply")
	}
	return nil
}

func liveApplySummary(raw json.RawMessage, converged bool) bool {
	if _, err := liveObject(raw, "total", "success", "skipped", "failed"); err != nil {
		return false
	}
	var summary struct{ Total, Success, Skipped, Failed int }
	return json.Unmarshal(raw, &summary) == nil && summary.Total == 1 && summary.Failed == 0 && ((converged && summary.Success == 0 && summary.Skipped == 1) || (!converged && summary.Success == 1 && summary.Skipped == 0))
}
func liveExpectedMappings(mappings []ComparatorMapping) (map[string]struct{}, bool) {
	values := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		if mapping.Identity == "" || !validLiveObserverValue(mapping.Identity) {
			return nil, false
		}
		if _, duplicate := values[mapping.Identity]; duplicate {
			return nil, false
		}
		values[mapping.Identity] = struct{}{}
	}
	return values, true
}
func liveCapturedMappings(raw json.RawMessage, expected map[string]struct{}) (map[string]struct{}, bool) {
	values, err := liveArray(raw)
	if err != nil || len(values) == 0 {
		return nil, false
	}
	productionPaths := make(map[string]string, len(expected))
	for identity := range expected {
		if !strings.HasPrefix(identity, "apps/") || !validLiveIdentity(identity) {
			return nil, false
		}
		path := "configs/" + strings.TrimPrefix(identity, "apps/")
		if _, duplicate := productionPaths[path]; duplicate {
			return nil, false
		}
		productionPaths[path] = identity
	}
	captured := make(map[string]struct{}, len(values))
	for _, value := range values {
		var path string
		if liveString(value, &path) != nil {
			return nil, false
		}
		identity, ok := productionPaths[path]
		if !ok {
			return nil, false
		}
		if _, duplicate := captured[identity]; duplicate {
			return nil, false
		}
		captured[identity] = struct{}{}
	}
	return captured, true
}

type liveRestoreItemExpectation struct {
	identity string
	target   string
}

func newLiveRuntimeRestoreAuthority(definition LiveDefinition, targets map[string]string) (*liveRuntimeRestoreAuthority, bool) {
	if targets == nil {
		return nil, true
	}
	inventory, ok := liveRestoreInventory(definition)
	if !ok || len(inventory) != 6 || len(targets) != len(inventory) {
		return nil, false
	}
	copy := make(map[string]string, len(targets))
	for _, item := range inventory {
		target, exists := targets[item.identity]
		if !exists || !liveCanonicalWindowsPath(target) {
			return nil, false
		}
		copy[item.identity] = target
	}
	return &liveRuntimeRestoreAuthority{targets: copy}, true
}

func liveCanonicalWindowsPath(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value && cleanLiveWindowsPath(value)
}

func liveRestoreInventory(definition LiveDefinition) (map[string]liveRestoreItemExpectation, bool) {
	directory := liveAppID(definition.ModuleID)
	if directory == "" {
		return nil, false
	}
	inventory := make(map[string]liveRestoreItemExpectation, len(definition.Comparator.Mappings)+1)
	for _, mapping := range definition.Comparator.Mappings {
		prefix := "apps/" + directory + "/"
		if !strings.HasPrefix(mapping.Identity, prefix) || strings.Contains(strings.TrimPrefix(mapping.Identity, prefix), "/") || mapping.RestoreTemplate == "" {
			return nil, false
		}
		source := "configs/" + directory + "/" + strings.TrimPrefix(mapping.Identity, prefix)
		if _, duplicate := inventory[source]; duplicate {
			return nil, false
		}
		inventory[source] = liveRestoreItemExpectation{identity: mapping.Identity, target: mapping.RestoreTemplate}
	}
	// The production module also restores this directory, which is intentionally
	// absent from the exact-file comparator because it is not a single file.
	if definition.ModuleID == "apps.notepad-plus-plus" && len(definition.Comparator.Mappings) == 5 {
		source := "configs/notepad-plus-plus/userDefineLangs"
		inventory[source] = liveRestoreItemExpectation{identity: "apps/notepad-plus-plus/userDefineLangs", target: `%APPDATA%\Notepad++\userDefineLangs`}
	}
	return inventory, len(inventory) > 0
}

func liveRuntimeRestoreOrder(definition LiveDefinition, inventory map[string]liveRestoreItemExpectation) ([]string, bool) {
	if definition.production == nil || len(definition.production.Restore) != len(inventory) {
		return nil, false
	}
	order := make([]string, 0, len(inventory))
	seen := make(map[string]struct{}, len(inventory))
	for _, restore := range definition.production.Restore {
		identity, err := liveRestoreIdentity(restore.Source)
		if err != nil {
			return nil, false
		}
		for source, item := range inventory {
			if item.identity == identity {
				if _, duplicate := seen[source]; duplicate {
					return nil, false
				}
				seen[source] = struct{}{}
				order = append(order, source)
				break
			}
		}
	}
	return order, len(order) == len(inventory) && liveSameStringSet(seen, liveRestoreSourceSet(inventory))
}

func liveRuntimeRestoreSource(inventory map[string]liveRestoreItemExpectation, source string) (string, liveRestoreItemExpectation, string, bool) {
	if !liveCanonicalWindowsPath(source) {
		return "", liveRestoreItemExpectation{}, "", false
	}
	for semantic, item := range inventory {
		suffix := `\` + strings.ReplaceAll(semantic, "/", `\`)
		if len(source) <= len(suffix) || !strings.EqualFold(source[len(source)-len(suffix):], suffix) {
			continue
		}
		root := source[:len(source)-len(suffix)]
		if !liveCanonicalWindowsPath(root) {
			return "", liveRestoreItemExpectation{}, "", false
		}
		return semantic, item, root, true
	}
	return "", liveRestoreItemExpectation{}, "", false
}

func liveActiveRestoreSubset(active map[string]struct{}, inventory map[string]liveRestoreItemExpectation) bool {
	available := make(map[string]struct{}, len(inventory))
	for _, item := range inventory {
		available[item.identity] = struct{}{}
	}
	for identity := range active {
		if _, ok := available[identity]; !ok {
			return false
		}
	}
	return len(active) > 0
}

func liveRestoreSourceSet(inventory map[string]liveRestoreItemExpectation) map[string]struct{} {
	values := make(map[string]struct{}, len(inventory))
	for source := range inventory {
		values[source] = struct{}{}
	}
	return values
}
func liveActiveRestoreTargets(active map[string]struct{}, inventory map[string]liveRestoreItemExpectation) map[string]struct{} {
	values := make(map[string]struct{}, len(active))
	for _, item := range inventory {
		if _, ok := active[item.identity]; ok {
			values[item.target] = struct{}{}
		}
	}
	return values
}

func liveActiveRuntimeRestoreTargets(active map[string]struct{}, inventory map[string]liveRestoreItemExpectation, authority *liveRuntimeRestoreAuthority) map[string]struct{} {
	values := make(map[string]struct{}, len(active))
	if authority == nil {
		return values
	}
	for _, item := range inventory {
		if _, ok := active[item.identity]; ok {
			target, exists := authority.targets[item.identity]
			if !exists {
				return map[string]struct{}{}
			}
			values[target] = struct{}{}
		}
	}
	return values
}
func liveManifestAppID(wingetRef string) string {
	return strings.ToLower(strings.ReplaceAll(wingetRef, ".", "-"))
}
func liveAppID(moduleID string) string { return strings.TrimPrefix(moduleID, "apps.") }
func liveDecodeFailure(phase, coordinate string) *Failure {
	return fail(CodeEnvelopeContract, phase, coordinate, "live output violates its closed contract")
}
func liveNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func liveStringEquals(raw json.RawMessage, expected string) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value == expected
}
func liveString(raw json.RawMessage, target *string) error {
	if len(raw) == 0 || json.Unmarshal(raw, target) != nil || !validLiveObserverValue(*target) {
		return fmt.Errorf("invalid string")
	}
	return nil
}

func liveOptionalString(raw json.RawMessage, target *string) error {
	if len(raw) == 0 || json.Unmarshal(raw, target) != nil || len(*target) > maxLiveStringBytes || strings.TrimSpace(*target) != *target || !validLiveObserverText(*target) {
		return fmt.Errorf("invalid string")
	}
	return nil
}

func liveOptionalMapString(values map[string]json.RawMessage, key string, target *string) error {
	raw, ok := values[key]
	if !ok {
		*target = ""
		return nil
	}
	return liveOptionalString(raw, target)
}
func liveRawString(raw json.RawMessage) (string, bool) {
	var value string
	return value, json.Unmarshal(raw, &value) == nil
}
func liveArray(raw json.RawMessage) ([]json.RawMessage, error) {
	var values []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return nil, fmt.Errorf("invalid array")
	}
	return values, nil
}
func liveEmptyArray(raw json.RawMessage) bool {
	values, err := liveArray(raw)
	return err == nil && len(values) == 0
}
func liveExactStrings(raw json.RawMessage, expected []string) bool {
	values, err := liveArray(raw)
	if err != nil || len(values) != len(expected) {
		return false
	}
	for index := range values {
		var value string
		if liveString(values[index], &value) != nil || value != expected[index] {
			return false
		}
	}
	return true
}
func liveExactStringSet(raw json.RawMessage, expected map[string]struct{}) bool {
	values, err := liveArray(raw)
	if err != nil || len(values) != len(expected) {
		return false
	}
	found := map[string]struct{}{}
	for _, raw := range values {
		var value string
		if liveString(raw, &value) != nil {
			return false
		}
		if _, ok := expected[value]; !ok {
			return false
		}
		if _, duplicate := found[value]; duplicate {
			return false
		}
		found[value] = struct{}{}
	}
	return true
}
func liveSameStringSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if _, ok := right[value]; !ok {
			return false
		}
	}
	return true
}
func liveExactKeySet(values map[string]json.RawMessage, expected ...string) bool {
	if len(values) != len(expected) {
		return false
	}
	for _, key := range expected {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}
func liveObject(raw []byte, required ...string) (map[string]json.RawMessage, error) {
	fields, err := liveOpenObject(raw)
	if err != nil || !liveExactKeySet(fields, required...) {
		return nil, fmt.Errorf("invalid object")
	}
	return fields, nil
}

func liveObjectAllowed(raw []byte, required, allowed []string) (map[string]json.RawMessage, error) {
	fields, err := liveOpenObject(raw)
	if err != nil {
		return nil, err
	}
	permitted := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		permitted[key] = struct{}{}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("missing key")
		}
	}
	for key := range fields {
		if _, ok := permitted[key]; !ok {
			return nil, fmt.Errorf("unexpected key")
		}
	}
	return fields, nil
}
func liveOpenObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || rejectDuplicateJSONKeys(raw) != nil {
		return nil, fmt.Errorf("invalid object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var fields map[string]json.RawMessage
	if decoder.Decode(&fields) != nil || fields == nil {
		return nil, fmt.Errorf("invalid object")
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return nil, fmt.Errorf("trailing object")
	}
	return fields, nil
}
