// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/events"
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
	ScenarioID         string
	InitialApply       liveCommandOutput
	Verify             liveCommandOutput
	Capture            liveCommandOutput
	RestoreRebuild     liveCommandOutput
	Revert             liveCommandOutput
	RestoreJournalID   string
	RecoveryRebuild    liveCommandOutput
	PackageAfterRevert PackageObservation
	ConvergenceRebuild liveCommandOutput
}

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
	if !validLiveModuleID(definition.ModuleID) || !validLiveObserverValue(definition.WingetRef) || len(definition.Comparator.Mappings) == 0 || len(definition.Comparator.Mappings) > maxLiveMappings || inputs.ScenarioID != liveConfigRoundtripScenarioID {
		return liveJourneyProjection{}, fail(CodeEnvelopeContract, "live", "identity", "live journey identity is invalid")
	}
	expectedMappings, ok := liveExpectedMappings(definition.Comparator.Mappings)
	if !ok {
		return liveJourneyProjection{}, fail(CodeEnvelopeContract, "live", "mappings", "live comparison mappings are invalid")
	}
	if failure := validateLiveApply(inputs.InitialApply, definition, false); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if failure := validateLiveVerify(inputs.Verify, definition, len(expectedMappings)); failure != nil {
		return liveJourneyProjection{}, failure
	}
	capturedMappings, failure := validateLiveCapture(inputs.Capture, definition, expectedMappings)
	if failure != nil {
		return liveJourneyProjection{}, failure
	}
	if failure := validateLiveRebuild(inputs.RestoreRebuild, definition, capturedMappings, true, false); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if failure := validateLiveRevert(inputs.Revert, definition, capturedMappings, inputs.RestoreJournalID, "revert"); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if failure := validateLiveRebuild(inputs.RecoveryRebuild, definition, capturedMappings, false, false); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if inputs.PackageAfterRevert.Ref != definition.WingetRef || inputs.PackageAfterRevert.Status != "present" || !validLiveObserverValue(inputs.PackageAfterRevert.Version) && inputs.PackageAfterRevert.Version != "" {
		return liveJourneyProjection{}, fail(CodeEnvelopeContract, "revert", "package", "configuration revert lacks a present package observation")
	}
	if failure := validateLiveRebuild(inputs.ConvergenceRebuild, definition, capturedMappings, false, true); failure != nil {
		return liveJourneyProjection{}, failure
	}
	return liveJourneyProjection{
		ModuleID: definition.ModuleID, Ref: definition.WingetRef, CapturedMappings: len(capturedMappings), RestoredMappings: len(capturedMappings),
		PackagePresentAfterRevert: true, ConvergenceEnvelopeObserved: true,
	}, nil
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
	item, err := liveObjectAllowed(results[0], []string{"type", "status"}, []string{"type", "status", "reason", "message"})
	if err != nil {
		return liveDecodeFailure("verify", "results")
	}
	var kind, status, reason string
	if liveString(item["type"], &kind) != nil || liveString(item["status"], &status) != nil || liveOptionalMapString(item, "reason", &reason) != nil || kind != "command-exists" || status != "pass" || reason != "" {
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
	capturedMappings, ok := liveSubsetStrings(module["paths"], expected)
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

func validateLiveRebuild(output liveCommandOutput, definition LiveDefinition, expected map[string]struct{}, requireInstall, converged bool) *Failure {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "rebuild")
	if failure != nil {
		return failure
	}
	if failure := decodeLiveEvents(output.Stderr, "rebuild", envelope.RunID); failure != nil {
		return failure
	}
	data, err := liveObjectAllowed(envelope.Data, []string{"from", "dryRun", "restore", "apply", "configResolutionSummary", "configResolutions", "restoreItems", "verify"}, []string{"from", "bundle", "dryRun", "restore", "apply", "configResolutionSummary", "configResolutions", "restoreItems", "verify"})
	if err != nil {
		return liveDecodeFailure("rebuild", "data")
	}
	var wire liveRebuildWire
	var officialApply commands.ApplyResult
	var officialVerify commands.VerifyResult
	if json.Unmarshal(envelope.Data, &wire) != nil || json.Unmarshal(wire.Apply, &officialApply) != nil || json.Unmarshal(wire.Verify, &officialVerify) != nil {
		return liveDecodeFailure("rebuild", "data")
	}
	nested, err := liveObjectAllowed(wire.Apply, []string{"dryRun", "manifest", "summary", "actions", "configResolutions", "configResolutionSummary", "restoreItems"}, []string{"dryRun", "manifest", "summary", "actions", "configModuleMap", "packageModuleMap", "warnings", "restoreModulesAvailable", "pruned", "configResolutions", "configResolutionSummary", "restoreItems"})
	if err != nil || officialApply.ConfigResultFields == nil || !reflect.DeepEqual(commands.ConfigResultFields{
		ConfigResolutions: wire.ConfigResolutions, ConfigResolutionSummary: wire.ConfigResolutionSummary, RestoreItems: wire.RestoreItems,
	}, *officialApply.ConfigResultFields) {
		return liveDecodeFailure("rebuild", "config")
	}
	var dryRun bool
	var restore string
	if json.Unmarshal(data["dryRun"], &dryRun) != nil || dryRun || liveString(data["restore"], &restore) != nil || restore != "enabled" {
		return liveDecodeFailure("rebuild", "restore")
	}
	if failure := validateLiveApplyData(data["apply"], definition, !requireInstall, false); failure != nil {
		return failure
	}
	if !validateLiveConfigFields(data, definition, expected, converged) || !validateLiveConfigFields(nested, definition, expected, converged) {
		return liveDecodeFailure("rebuild", "config")
	}
	if failure := validateLiveVerifyData(data["verify"], definition, len(expected)); failure != nil {
		return failure
	}
	return nil
}

func validateLiveRevert(output liveCommandOutput, definition LiveDefinition, expected map[string]struct{}, journalID, phase string) *Failure {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "revert")
	if failure != nil {
		return failure
	}
	if failure := decodeLiveEvents(output.Stderr, "revert", envelope.RunID); failure != nil {
		return failure
	}
	data, err := liveObject(envelope.Data, "journalUsed", "results")
	if err != nil {
		return liveDecodeFailure(phase, "data")
	}
	var official commands.RevertData
	if json.Unmarshal(envelope.Data, &official) != nil {
		return liveDecodeFailure(phase, "data")
	}
	var journal string
	if liveString(data["journalUsed"], &journal) != nil || journal == "" || journal != journalID {
		return liveDecodeFailure(phase, "journal")
	}
	inventory, ok := liveRestoreInventory(definition)
	if !ok || !liveActiveRestoreSubset(expected, inventory) {
		return liveDecodeFailure(phase, "results")
	}
	expectedTargets := liveActiveRestoreTargets(expected, inventory)
	results, err := liveArray(data["results"])
	if err != nil || len(results) != len(expectedTargets) {
		return liveDecodeFailure(phase, "results")
	}
	seen := make(map[string]struct{}, len(results))
	for _, raw := range results {
		item, err := liveObjectAllowed(raw, []string{"target", "action"}, []string{"target", "action", "backupUsed"})
		if err != nil {
			return liveDecodeFailure(phase, "results")
		}
		var target, action string
		if liveString(item["target"], &target) != nil || liveString(item["action"], &action) != nil || action != "deleted" || item["backupUsed"] != nil {
			return liveDecodeFailure(phase, "results")
		}
		if _, ok := expectedTargets[target]; !ok {
			return liveDecodeFailure(phase, "results")
		}
		if _, duplicate := seen[target]; duplicate {
			return liveDecodeFailure(phase, "results")
		}
		seen[target] = struct{}{}
	}
	if !liveSameStringSet(seen, expectedTargets) {
		return liveDecodeFailure(phase, "results")
	}
	return nil
}

func validateLiveConfigFields(data map[string]json.RawMessage, definition LiveDefinition, expected map[string]struct{}, converged bool) bool {
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
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		item, err := liveObjectAllowed(raw, []string{"id", "source", "target", "status", "backupCreated", "targetExistedBefore"}, []string{"id", "source", "target", "status", "backupPath", "backupCreated", "targetExistedBefore", "restoreType", "warnings", "error", "captureId", "configSetId", "targetInstanceId", "sourceGeneration", "targetGeneration"})
		if err != nil {
			return false
		}
		var id, source, target, itemStatus, restoreType string
		var backup, existed bool
		if liveString(item["id"], &id) != nil || liveString(item["source"], &source) != nil || liveString(item["target"], &target) != nil || liveString(item["status"], &itemStatus) != nil || json.Unmarshal(item["backupCreated"], &backup) != nil || json.Unmarshal(item["targetExistedBefore"], &existed) != nil || id == "" {
			return false
		}
		expectedItem, found := inventory[source]
		_, active := expected[expectedItem.identity]
		wantStatus := "skipped_missing_source"
		if active {
			wantStatus = "restored"
			if converged {
				wantStatus = "skipped_up_to_date"
			}
		}
		if !found || target != expectedItem.target || itemStatus != wantStatus || backup || existed {
			return false
		}
		if rawType, ok := item["restoreType"]; ok && (liveString(rawType, &restoreType) != nil || restoreType != "copy") {
			return false
		}
		if _, duplicate := seen[source]; duplicate {
			return false
		}
		seen[source] = struct{}{}
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
	if len(stderr) == 0 || len(stderr) > maxLiveDecodeOutputBytes || !bytes.HasSuffix(stderr, []byte("\n")) {
		return liveDecodeFailure(command, "events")
	}
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	scanner.Buffer(make([]byte, 1024), 64*1024)
	count := 0
	var records []liveEventRecord
	for scanner.Scan() {
		line := scanner.Bytes()
		count++
		if count > maxLiveDecodeEvents || len(line) == 0 || rejectDuplicateJSONKeys(line) != nil {
			return liveDecodeFailure(command, "events")
		}
		record, ok := decodeLiveEvent(line)
		if !ok {
			return liveDecodeFailure(command, "events")
		}
		records = append(records, record)
	}
	if scanner.Err() != nil || !liveEventTopology(records, command, runID) {
		return liveDecodeFailure(command, "events")
	}
	return nil
}

type liveEventRecord struct {
	runID string
	event string
	phase string
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
func liveSubsetStrings(raw json.RawMessage, allowed map[string]struct{}) (map[string]struct{}, bool) {
	values, err := liveArray(raw)
	if err != nil || len(values) == 0 {
		return nil, false
	}
	observed := make(map[string]struct{}, len(values))
	for _, value := range values {
		var identity string
		if liveString(value, &identity) != nil {
			return nil, false
		}
		if _, ok := allowed[identity]; !ok {
			return nil, false
		}
		if _, duplicate := observed[identity]; duplicate {
			return nil, false
		}
		observed[identity] = struct{}{}
	}
	return observed, true
}

type liveRestoreItemExpectation struct {
	identity string
	target   string
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
