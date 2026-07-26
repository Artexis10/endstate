// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
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
	Rebuild            liveCommandOutput
	Revert             liveCommandOutput
	Recovery           liveCommandOutput
	PackageAfterRevert PackageObservation
	FinalApply         liveCommandOutput
}

// liveJourneyProjection is deliberately summary-only: host paths, setting
// content, journal names, raw command output, and credentials stay internal.
type liveJourneyProjection struct {
	ModuleID                  string
	Ref                       string
	CapturedMappings          int
	RestoredMappings          int
	PackagePresentAfterRevert bool
	ConvergedWithoutMutation  bool
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
	if failure := validateLiveCapture(inputs.Capture, definition, expectedMappings); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if failure := validateLiveRebuild(inputs.Rebuild, definition, expectedMappings); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if failure := validateLiveRevert(inputs.Revert, definition, len(expectedMappings), "revert"); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if failure := validateLiveRevert(inputs.Recovery, definition, len(expectedMappings), "recovery"); failure != nil {
		return liveJourneyProjection{}, failure
	}
	if inputs.PackageAfterRevert.Ref != definition.WingetRef || inputs.PackageAfterRevert.Status != "present" || !validLiveObserverValue(inputs.PackageAfterRevert.Version) && inputs.PackageAfterRevert.Version != "" {
		return liveJourneyProjection{}, fail(CodeEnvelopeContract, "revert", "package", "configuration revert lacks a present package observation")
	}
	if failure := validateLiveApply(inputs.FinalApply, definition, true); failure != nil {
		return liveJourneyProjection{}, failure
	}
	return liveJourneyProjection{
		ModuleID: definition.ModuleID, Ref: definition.WingetRef, CapturedMappings: len(expectedMappings), RestoredMappings: len(expectedMappings),
		PackagePresentAfterRevert: true, ConvergedWithoutMutation: true,
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
	var data map[string]json.RawMessage
	var err error
	if converged {
		data, err = liveObject(envelope.Data, "dryRun", "summary", "actions", "pruned")
	} else {
		data, err = liveObject(envelope.Data, "dryRun", "summary", "actions")
	}
	if err != nil {
		return liveDecodeFailure("apply", "data")
	}
	if converged {
		if _, err := liveArray(data["pruned"]); err != nil {
			return liveDecodeFailure("apply", "convergence")
		}
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
	action, err := liveObject(actions[0], "id", "ref", "driver", "status", "reason")
	if err != nil {
		return liveDecodeFailure("apply", "actions")
	}
	var id, ref, driver, status, reason string
	if liveString(action["id"], &id) != nil || liveString(action["ref"], &ref) != nil || liveString(action["driver"], &driver) != nil || liveString(action["status"], &status) != nil || liveOptionalString(action["reason"], &reason) != nil ||
		id != liveAppID(definition.ModuleID) || ref != definition.WingetRef || !strings.EqualFold(driver, "winget") {
		return liveDecodeFailure("apply", "actions")
	}
	if converged && (status != "present" || reason != "already_installed") || !converged && (status != "installed" || reason != "") {
		return liveDecodeFailure("apply", "actions")
	}
	return nil
}

func validateLiveVerify(output liveCommandOutput, definition LiveDefinition, mappingCount int) *Failure {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "verify")
	if failure != nil {
		return failure
	}
	if failure := decodeLiveEvents(output.Stderr, "verify", envelope.RunID); failure != nil {
		return failure
	}
	return validateLiveVerifyData(envelope.Data, definition, mappingCount)
}

func validateLiveVerifyData(raw json.RawMessage, definition LiveDefinition, mappingCount int) *Failure {
	data, err := liveObject(raw, "summary", "results")
	if err != nil {
		return liveDecodeFailure("verify", "data")
	}
	if _, err := liveObject(data["summary"], "total", "pass", "fail", "skipped"); err != nil {
		return liveDecodeFailure("verify", "summary")
	}
	var summary struct{ Total, Pass, Fail, Skipped int }
	if json.Unmarshal(data["summary"], &summary) != nil || summary.Total != mappingCount+1 || summary.Pass != summary.Total || summary.Fail != 0 || summary.Skipped != 0 {
		return liveDecodeFailure("verify", "summary")
	}
	results, err := liveArray(data["results"])
	if err != nil || len(results) != summary.Total {
		return liveDecodeFailure("verify", "results")
	}
	apps, settings := 0, 0
	for _, raw := range results {
		item, err := liveOpenObject(raw)
		if err != nil {
			return liveDecodeFailure("verify", "results")
		}
		var kind, status, reason string
		if liveString(item["type"], &kind) != nil || liveString(item["status"], &status) != nil || liveOptionalString(item["reason"], &reason) != nil || status != "pass" || reason != "" {
			return liveDecodeFailure("verify", "results")
		}
		if kind == "app" {
			var id, ref, driver string
			if !liveExactKeySet(item, "type", "id", "ref", "driver", "status", "reason") || liveString(item["id"], &id) != nil || liveString(item["ref"], &ref) != nil || liveString(item["driver"], &driver) != nil || id != liveAppID(definition.ModuleID) || ref != definition.WingetRef || !strings.EqualFold(driver, "winget") {
				return liveDecodeFailure("verify", "app")
			}
			apps++
		} else if kind == "file-exists" && liveExactKeySet(item, "type", "status", "reason") {
			settings++
		} else {
			return liveDecodeFailure("verify", "results")
		}
	}
	if apps != 1 || settings != mappingCount {
		return liveDecodeFailure("verify", "results")
	}
	return nil
}

func validateLiveCapture(output liveCommandOutput, definition LiveDefinition, expected map[string]struct{}) *Failure {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "capture")
	if failure != nil {
		return failure
	}
	if failure := decodeLiveEvents(output.Stderr, "capture", envelope.RunID); failure != nil {
		return failure
	}
	data, err := liveObject(envelope.Data, "appsIncluded", "configModules", "counts", "configsIncluded", "configsSkipped", "configsCaptureErrors")
	if err != nil {
		return liveDecodeFailure("capture", "data")
	}
	apps, err := liveArray(data["appsIncluded"])
	modules, modulesErr := liveArray(data["configModules"])
	if err != nil || modulesErr != nil || len(apps) != 1 || len(modules) != 1 {
		return liveDecodeFailure("capture", "modules")
	}
	app, err := liveObject(apps[0], "id", "manifestId")
	if err != nil {
		return liveDecodeFailure("capture", "apps")
	}
	var appRef, appID string
	if liveString(app["id"], &appRef) != nil || liveString(app["manifestId"], &appID) != nil || appRef != definition.WingetRef || appID != liveAppID(definition.ModuleID) {
		return liveDecodeFailure("capture", "apps")
	}
	module, err := liveObject(modules[0], "id", "wingetRefs", "filesCaptured", "status", "paths")
	if err != nil {
		return liveDecodeFailure("capture", "modules")
	}
	var moduleID, status string
	var captured int
	if liveString(module["id"], &moduleID) != nil || liveString(module["status"], &status) != nil || json.Unmarshal(module["filesCaptured"], &captured) != nil || moduleID != definition.ModuleID || status != "captured" || captured != len(expected) || captured == 0 || !liveExactStrings(module["wingetRefs"], []string{definition.WingetRef}) || !liveExactStringSet(module["paths"], expected) {
		return liveDecodeFailure("capture", "modules")
	}
	if _, err := liveObject(data["counts"], "included", "skipped", "totalFound"); err != nil {
		return liveDecodeFailure("capture", "counts")
	}
	var counts struct{ Included, Skipped, TotalFound int }
	if json.Unmarshal(data["counts"], &counts) != nil || counts.Included != 1 || counts.Skipped != 0 || counts.TotalFound < 1 || !liveExactStrings(data["configsIncluded"], []string{definition.ModuleID}) || !liveEmptyArray(data["configsSkipped"]) || !liveEmptyArray(data["configsCaptureErrors"]) {
		return liveDecodeFailure("capture", "counts")
	}
	return nil
}

func validateLiveRebuild(output liveCommandOutput, definition LiveDefinition, expected map[string]struct{}) *Failure {
	envelope, failure := decodeLiveEnvelope(output.Stdout, "rebuild")
	if failure != nil {
		return failure
	}
	if failure := decodeLiveEvents(output.Stderr, "rebuild", envelope.RunID); failure != nil {
		return failure
	}
	data, err := liveObject(envelope.Data, "dryRun", "restore", "apply", "configResolutionSummary", "configResolutions", "restoreItems", "verify")
	if err != nil {
		return liveDecodeFailure("rebuild", "data")
	}
	var dryRun bool
	var restore string
	if json.Unmarshal(data["dryRun"], &dryRun) != nil || dryRun || liveString(data["restore"], &restore) != nil || restore != "enabled" {
		return liveDecodeFailure("rebuild", "restore")
	}
	if failure := validateLiveApplyData(data["apply"], definition, true, false); failure != nil {
		return failure
	}
	if _, err := liveObject(data["configResolutionSummary"], "total", "selected", "skipped", "failed"); err != nil {
		return liveDecodeFailure("rebuild", "config")
	}
	var summary struct{ Total, Selected, Skipped, Failed int }
	if json.Unmarshal(data["configResolutionSummary"], &summary) != nil || summary.Total != 1 || summary.Selected != 1 || summary.Skipped != 0 || summary.Failed != 0 {
		return liveDecodeFailure("rebuild", "config")
	}
	resolutions, err := liveArray(data["configResolutions"])
	if err != nil || len(resolutions) != 1 {
		return liveDecodeFailure("rebuild", "config")
	}
	resolution, err := liveObject(resolutions[0], "status", "resolution", "reason")
	if err != nil {
		return liveDecodeFailure("rebuild", "config")
	}
	var status, kind string
	if liveString(resolution["status"], &status) != nil || liveString(resolution["resolution"], &kind) != nil || status != "restored" || kind != "legacy_unverified" || !liveNull(resolution["reason"]) {
		return liveDecodeFailure("rebuild", "config")
	}
	items, err := liveArray(data["restoreItems"])
	if err != nil || len(items) != len(expected) {
		return liveDecodeFailure("rebuild", "restoreItems")
	}
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		item, err := liveObject(raw, "source", "target", "status", "backupCreated", "targetExistedBefore", "restoreType")
		if err != nil {
			return liveDecodeFailure("rebuild", "restoreItems")
		}
		var source, target, itemStatus, restoreType string
		var backup, existed bool
		if liveString(item["source"], &source) != nil || liveString(item["target"], &target) != nil || liveString(item["status"], &itemStatus) != nil || liveString(item["restoreType"], &restoreType) != nil || json.Unmarshal(item["backupCreated"], &backup) != nil || json.Unmarshal(item["targetExistedBefore"], &existed) != nil || itemStatus != "restored" || restoreType != "copy" || !backup || !existed || !liveExpectedTarget(definition.Comparator.Mappings, source, target) {
			return liveDecodeFailure("rebuild", "restoreItems")
		}
		if _, duplicate := seen[source]; duplicate {
			return liveDecodeFailure("rebuild", "restoreItems")
		}
		seen[source] = struct{}{}
	}
	if !liveSameStringSet(seen, expected) {
		return liveDecodeFailure("rebuild", "restoreItems")
	}
	if failure := validateLiveVerifyData(data["verify"], definition, len(expected)); failure != nil {
		return failure
	}
	return nil
}

func validateLiveRevert(output liveCommandOutput, definition LiveDefinition, mappingCount int, phase string) *Failure {
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
	var journal string
	if liveString(data["journalUsed"], &journal) != nil || journal == "" {
		return liveDecodeFailure(phase, "journal")
	}
	results, err := liveArray(data["results"])
	if err != nil || len(results) != mappingCount {
		return liveDecodeFailure(phase, "results")
	}
	for _, raw := range results {
		item, err := liveObject(raw, "action")
		if err != nil {
			return liveDecodeFailure(phase, "results")
		}
		var action string
		if liveString(item["action"], &action) != nil || action != "reverted" {
			return liveDecodeFailure(phase, "results")
		}
	}
	return nil
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
	phase := map[string]string{"apply": "plan", "verify": "verify", "capture": "capture", "rebuild": "restore", "revert": "restore"}[command]
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	scanner.Buffer(make([]byte, 1024), 64*1024)
	count, sawPhase, sawSummary := 0, false, false
	for scanner.Scan() {
		line := scanner.Bytes()
		count++
		if count > maxLiveDecodeEvents || len(line) == 0 || rejectDuplicateJSONKeys(line) != nil {
			return liveDecodeFailure(command, "events")
		}
		fields, err := liveOpenObject(line)
		if err != nil {
			return liveDecodeFailure(command, "events")
		}
		var eventRun, timestamp, event, eventPhase string
		if liveString(fields["runId"], &eventRun) != nil || liveString(fields["timestamp"], &timestamp) != nil || liveString(fields["event"], &event) != nil || liveString(fields["phase"], &eventPhase) != nil || eventRun != runID || eventPhase != phase {
			return liveDecodeFailure(command, "events")
		}
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			return liveDecodeFailure(command, "events")
		}
		var version int
		if json.Unmarshal(fields["version"], &version) != nil || version != 1 || sawSummary {
			return liveDecodeFailure(command, "events")
		}
		if event == "phase" && !sawPhase && liveExactKeySet(fields, "version", "runId", "timestamp", "event", "phase") {
			sawPhase = true
			continue
		}
		if event != "summary" || !sawPhase || !liveExactKeySet(fields, "version", "runId", "timestamp", "event", "phase", "total", "success", "skipped", "failed") {
			return liveDecodeFailure(command, "events")
		}
		var summary struct{ Total, Success, Skipped, Failed int }
		if json.Unmarshal(line, &summary) != nil || summary.Total != summary.Success+summary.Skipped+summary.Failed || summary.Failed != 0 {
			return liveDecodeFailure(command, "events")
		}
		sawSummary = true
	}
	if scanner.Err() != nil || !sawPhase || !sawSummary {
		return liveDecodeFailure(command, "events")
	}
	return nil
}

func validateLiveApplyData(raw json.RawMessage, definition LiveDefinition, converged, includePruned bool) *Failure {
	var data map[string]json.RawMessage
	var err error
	if includePruned {
		data, err = liveObject(raw, "dryRun", "summary", "actions", "pruned")
	} else {
		data, err = liveObject(raw, "dryRun", "summary", "actions")
	}
	if err != nil {
		return liveDecodeFailure("rebuild", "apply")
	}
	if includePruned && !liveEmptyArray(data["pruned"]) {
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
	action, err := liveObject(actions[0], "id", "ref", "driver", "status", "reason")
	if err != nil {
		return liveDecodeFailure("rebuild", "apply")
	}
	var id, ref, driver, status, reason string
	if liveString(action["id"], &id) != nil || liveString(action["ref"], &ref) != nil || liveString(action["driver"], &driver) != nil || liveString(action["status"], &status) != nil || liveOptionalString(action["reason"], &reason) != nil || id != liveAppID(definition.ModuleID) || ref != definition.WingetRef || !strings.EqualFold(driver, "winget") || status != "present" || reason != "already_installed" {
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
func liveExpectedTarget(mappings []ComparatorMapping, source, target string) bool {
	for _, mapping := range mappings {
		if mapping.Identity == source && mapping.RestoreTemplate == target {
			return true
		}
	}
	return false
}
func liveAppID(moduleID string) string { return strings.TrimPrefix(moduleID, "apps.") }
func liveDecodeFailure(phase, coordinate string) *Failure {
	return fail(CodeEnvelopeContract, phase, coordinate, "live output violates its closed contract")
}
func liveNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
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
