// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type decodedEnvelope struct {
	SchemaVersion string           `json:"schemaVersion"`
	CLIVersion    string           `json:"cliVersion"`
	Command       string           `json:"command"`
	RunID         string           `json:"runId"`
	TimestampUTC  string           `json:"timestampUtc"`
	Success       bool             `json:"success"`
	TestMode      testModeIdentity `json:"testMode"`
	Data          json.RawMessage  `json:"data"`
	Error         json.RawMessage  `json:"error"`
}

type testModeIdentity struct {
	Active     bool   `json:"active"`
	ScenarioID string `json:"scenarioId"`
	ModuleID   string `json:"moduleId"`
}

func decodeEnvelope(stdout []byte, command, moduleID, scenarioID string, forbidden ...string) (decodedEnvelope, *Failure) {
	if leaked(stdout, forbidden...) {
		return decodedEnvelope{}, fail(CodeIsolationFailure, command, "stdout", "command output leaked validation authority")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	decoder.DisallowUnknownFields()
	var envelope decodedEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "stdout", "malformed JSON envelope")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "stdout", "stdout must contain exactly one JSON envelope")
	}
	if envelope.Command != command {
		return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "command", "envelope command does not match invocation")
	}
	if !envelope.Success {
		var engineError struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		detail := "engine envelope reported failure"
		if err := json.Unmarshal(envelope.Error, &engineError); err == nil && engineError.Code != "" {
			detail = engineError.Code
			if engineError.Message != "" {
				detail += ": " + engineError.Message
			}
		}
		return decodedEnvelope{}, fail(CodeExecutionFailure, command, "success", detail)
	}
	if envelope.SchemaVersion == "" || envelope.CLIVersion == "" || envelope.RunID == "" || len(envelope.Data) == 0 || string(envelope.Error) != "null" {
		return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "envelope", "stable envelope fields are absent or inconsistent")
	}
	if _, err := time.Parse(time.RFC3339, envelope.TimestampUTC); err != nil {
		return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "timestampUtc", "envelope timestamp is invalid")
	}
	if !envelope.TestMode.Active || envelope.TestMode.ModuleID != moduleID || envelope.TestMode.ScenarioID != scenarioID {
		return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "testMode", "test-mode identity is absent or mismatched")
	}
	if command == "rebuild" {
		var nested struct {
			Apply struct {
				Summary struct {
					Total   int `json:"total"`
					Success int `json:"success"`
					Skipped int `json:"skipped"`
					Failed  int `json:"failed"`
				} `json:"summary"`
			} `json:"apply"`
			ConfigResolutionSummary struct {
				Total    int `json:"total"`
				Selected int `json:"selected"`
				Skipped  int `json:"skipped"`
				Failed   int `json:"failed"`
			} `json:"configResolutionSummary"`
			ConfigResolutions []struct {
				Status string `json:"status"`
			} `json:"configResolutions"`
			RestoreItems []struct {
				Status string `json:"status"`
			} `json:"restoreItems"`
			Verify struct {
				Summary struct {
					Pass int `json:"pass"`
					Fail int `json:"fail"`
				} `json:"summary"`
			} `json:"verify"`
		}
		if err := json.Unmarshal(envelope.Data, &nested); err != nil ||
			nested.Apply.Summary.Total <= 0 || nested.Apply.Summary.Success < 0 || nested.Apply.Summary.Skipped < 0 || nested.Apply.Summary.Failed != 0 ||
			nested.Apply.Summary.Success+nested.Apply.Summary.Skipped != nested.Apply.Summary.Total ||
			nested.ConfigResolutionSummary.Total <= 0 || nested.ConfigResolutionSummary.Selected < 0 || nested.ConfigResolutionSummary.Skipped < 0 || nested.ConfigResolutionSummary.Failed != 0 ||
			nested.ConfigResolutionSummary.Selected+nested.ConfigResolutionSummary.Skipped != nested.ConfigResolutionSummary.Total ||
			len(nested.ConfigResolutions) != nested.ConfigResolutionSummary.Total || len(nested.RestoreItems) == 0 ||
			nested.Verify.Summary.Fail != 0 || nested.Verify.Summary.Pass <= 0 {
			return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "data", "nested rebuild result failed or was vacuous")
		}
		for _, resolution := range nested.ConfigResolutions {
			if resolution.Status != "restored" && resolution.Status != "skipped" {
				return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "configResolutions", "config resolution has a failed or malformed terminal status")
			}
		}
		for _, item := range nested.RestoreItems {
			if item.Status != "restored" && item.Status != "skipped_up_to_date" {
				return decodedEnvelope{}, fail(CodeEnvelopeContract, command, "restoreItems", "restore item failed or has an unknown terminal status")
			}
		}
	}
	return envelope, nil
}

func decodeEvents(stderr []byte, command, envelopeRunID string, forbidden ...string) ([]map[string]any, *Failure) {
	if leaked(stderr, forbidden...) {
		return nil, fail(CodeIsolationFailure, "events", "stderr", "event output leaked validation authority")
	}
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var events []map[string]any
	var eventRunIDAuthority string
	seenRunIDs := make(map[string]struct{})
	if envelopeRunID == "" {
		return nil, fail(CodeEventContract, "events", "runId", "authoritative envelope runId is absent")
	}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&event); err != nil {
			return nil, fail(CodeEventContract, "events", "stderr", "malformed JSONL event")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fail(CodeEventContract, "events", "stderr", "event line contains multiple JSON values")
		}
		version, versionOK := event["version"].(json.Number)
		eventRunID, runOK := event["runId"].(string)
		timestamp, timeOK := event["timestamp"].(string)
		eventType, typeOK := event["event"].(string)
		if !versionOK || version.String() != "1" || !runOK || eventRunID == "" || !timeOK || !typeOK || !knownEventType(eventType) {
			return nil, fail(CodeEventContract, "events", "base", "event base fields are absent, mismatched, or unknown")
		}
		if failure := validateEventShape(eventType, event); failure != nil {
			return nil, failure
		}
		if !exactEventFields(eventType, event) {
			return nil, fail(CodeEventContract, "events", "fields", "event contains a field outside its versioned wire shape")
		}
		if eventRunIDAuthority == "" {
			if eventType != "phase" {
				return nil, fail(CodeEventContract, "events", "segment", "event stream segment must begin with a phase event")
			}
			eventRunIDAuthority = eventRunID
			seenRunIDs[eventRunID] = struct{}{}
		} else if eventRunID != eventRunIDAuthority {
			_, seen := seenRunIDs[eventRunID]
			phase, isPhase := event["phase"].(string)
			if command != "rebuild" || seen || len(seenRunIDs) != 1 || eventType != "phase" || !isPhase || phase != "verify" {
				return nil, fail(CodeEventContract, "events", "runId", "event stream contains inconsistent run ID segments")
			}
			if len(events) == 0 || events[len(events)-1]["event"] != "summary" {
				return nil, fail(CodeEventContract, "events", "segment", "event stream segment must end with a summary event")
			}
			eventRunIDAuthority = eventRunID
			seenRunIDs[eventRunID] = struct{}{}
		}
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			return nil, fail(CodeEventContract, "events", "timestamp", "event timestamp is invalid")
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fail(CodeEventContract, "events", "stderr", fmt.Sprintf("read JSONL events: %v", err))
	}
	if len(events) == 0 {
		return nil, fail(CodeEventContract, "events", "stderr", "requested event stream is empty")
	}
	if events[len(events)-1]["event"] != "summary" {
		return nil, fail(CodeEventContract, "events", "segment", "event stream segment must end with a summary event")
	}
	if failure := validateCommandEventStream(command, events); failure != nil {
		return nil, failure
	}
	return events, nil
}

type eventSegmentState struct {
	runID  string
	events []map[string]any
}

func validateCommandEventStream(command string, events []map[string]any) *Failure {
	segments := []eventSegmentState{}
	seenRunIDs := map[string]struct{}{}
	for _, event := range events {
		runID := event["runId"].(string)
		if len(segments) == 0 || segments[len(segments)-1].runID != runID {
			if _, duplicate := seenRunIDs[runID]; duplicate {
				return fail(CodeEventContract, "events", "runId", "event run ID segment reappeared after closing")
			}
			seenRunIDs[runID] = struct{}{}
			segments = append(segments, eventSegmentState{runID: runID})
		}
		segments[len(segments)-1].events = append(segments[len(segments)-1].events, event)
	}
	type segmentContract struct {
		prefix string
		phases []string
	}
	var contracts []segmentContract
	switch command {
	case "capture":
		contracts = []segmentContract{{prefix: "capture-", phases: []string{"capture"}}}
	case "rebuild":
		contracts = []segmentContract{
			{prefix: "apply-", phases: []string{"plan", "apply", "restore", "verify"}},
			{prefix: "verify-", phases: []string{"verify"}},
		}
	case "revert":
		contracts = []segmentContract{{prefix: "revert-", phases: []string{"restore"}}}
	case "verify":
		contracts = []segmentContract{{prefix: "verify-", phases: []string{"verify"}}}
	default:
		return fail(CodeEventContract, "events", "command", "command has no validation event contract")
	}
	if len(segments) != len(contracts) {
		return fail(CodeEventContract, "events", "segments", "event stream has an unexpected command segment count")
	}
	for index := range contracts {
		if !strings.HasPrefix(segments[index].runID, contracts[index].prefix) {
			return fail(CodeEventContract, "events", "runId", "event run ID has the wrong command prefix")
		}
		if failure := validateEventSegment(command, segments[index].events, contracts[index].phases); failure != nil {
			return failure
		}
	}
	return nil
}

func validateEventSegment(command string, events []map[string]any, expectedPhases []string) *Failure {
	phaseIndex := 0
	currentPhase := ""
	captureStages := []string{}
	captureArtifacts := 0
	for _, event := range events {
		eventType := event["event"].(string)
		switch eventType {
		case "phase":
			phase := event["phase"].(string)
			if currentPhase != "" || phaseIndex >= len(expectedPhases) || phase != expectedPhases[phaseIndex] {
				return fail(CodeEventContract, "events", "phase", "event phases differ from the exact command sequence")
			}
			currentPhase = phase
		case "summary":
			phase := event["phase"].(string)
			if currentPhase == "" || phase != currentPhase {
				return fail(CodeEventContract, "events", "summary.phase", "summary is not bound to the open command phase")
			}
			currentPhase = ""
			phaseIndex++
		default:
			if currentPhase == "" {
				return fail(CodeEventContract, "events", "phase", "event occurred outside an open command phase")
			}
			if phase, exists := event["phase"].(string); exists && phase != currentPhase {
				return fail(CodeEventContract, "events", "phase", "event phase differs from the open command phase")
			}
			if !eventTypeAllowed(command, currentPhase, eventType) {
				return fail(CodeEventContract, "events", "event", "event type is not allowed in the open command phase")
			}
		}
		if eventType == "progress" && command == "capture" {
			captureStages = append(captureStages, event["stage"].(string))
		}
		if eventType == "artifact" && command == "capture" {
			if event["kind"] != "manifest" {
				return fail(CodeEventContract, "events", "artifact.kind", "capture artifact kind is outside the closed vocabulary")
			}
			captureArtifacts++
		}
		if eventType == "item" {
			if failure := validateItemEventVocabulary(command, currentPhase, event); failure != nil {
				return failure
			}
		}
		if eventType == "config-resolution" {
			if failure := validateConfigResolutionEventVocabulary(event); failure != nil {
				return failure
			}
		}
		if eventType == "config-migration" {
			if !oneOf(event["stage"], "staging", "edge", "validation", "commit", "rollback") || !oneOf(event["status"], "started", "completed") {
				return fail(CodeEventContract, "events", "config-migration", "config migration stage or passing status is outside the closed vocabulary")
			}
		}
		if eventType == "restore-item" {
			if failure := validateRestoreItemEventVocabulary(event); failure != nil {
				return failure
			}
		}
	}
	if currentPhase != "" || phaseIndex != len(expectedPhases) {
		return fail(CodeEventContract, "events", "phase", "event stream omitted an exact command phase or summary")
	}
	if command == "capture" && (!exactStrings(captureStages, []string{"inventory", "settings", "packaging"}) || captureArtifacts != 1) {
		return fail(CodeEventContract, "events", "capture", "capture progress stages or artifact evidence differ from the exact command sequence")
	}
	return nil
}

func eventTypeAllowed(command, phase, eventType string) bool {
	switch command + "/" + phase {
	case "capture/capture":
		return oneOf(eventType, "progress", "item", "artifact")
	case "rebuild/plan", "rebuild/apply", "rebuild/verify", "verify/verify", "revert/restore":
		return eventType == "item"
	case "rebuild/restore":
		return oneOf(eventType, "config-resolution", "config-migration", "restore-item")
	default:
		return false
	}
}

func validateItemEventVocabulary(command, phase string, event map[string]any) *Failure {
	status := event["status"].(string)
	reason := event["reason"].(string)
	valid := false
	switch command + "/" + phase {
	case "capture/capture":
		valid = status == "present" && reason == "detected"
	case "rebuild/plan":
		valid = status == "present" && reason == "already_installed"
	case "rebuild/apply":
		valid = (status == "present" || status == "installed" || status == "skipped") && oneOf(reason, "", "already_installed", "filtered")
	case "rebuild/verify", "verify/verify":
		valid = status == "present" && reason == ""
	case "revert/restore":
		valid = (status == "installed" || status == "skipped") && reason == ""
	}
	if !valid {
		return fail(CodeEventContract, "events", "item", "item status and reason differ from the passing command vocabulary")
	}
	return nil
}

func validateConfigResolutionEventVocabulary(event map[string]any) *Failure {
	resolution := event["resolution"].(string)
	if !oneOf(resolution, "direct", "migrate", "legacy_unverified") {
		return fail(CodeEventContract, "events", "config-resolution.resolution", "config resolution is not a passing closed value")
	}
	if reason := event["reason"]; reason != nil {
		value, ok := reason.(string)
		if !ok || !oneOf(value, "already_up_to_date") {
			return fail(CodeEventContract, "events", "config-resolution.reason", "config resolution reason is outside the passing closed vocabulary")
		}
	}
	return nil
}

func validateRestoreItemEventVocabulary(event map[string]any) *Failure {
	status := event["status"].(string)
	if !oneOf(status, "restoring", "restored", "skipped_up_to_date") {
		return fail(CodeEventContract, "events", "restore-item.status", "restore item status is outside the passing closed vocabulary")
	}
	reason := event["reason"]
	if status == "skipped_up_to_date" {
		if reason != "already_up_to_date" {
			return fail(CodeEventContract, "events", "restore-item.reason", "up-to-date restore item lacks its exact reason")
		}
	} else if reason != nil {
		return fail(CodeEventContract, "events", "restore-item.reason", "restoring or restored item has an unexpected reason")
	}
	return nil
}

func oneOf(value any, allowed ...string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if text == candidate {
			return true
		}
	}
	return false
}

func exactEventFields(eventType string, event map[string]any) bool {
	base := []string{"version", "runId", "timestamp", "event"}
	fields := map[string][]string{
		"phase":             {"phase"},
		"progress":          {"phase", "stage"},
		"item":              {"id", "driver", "name", "status", "reason", "message", "rebootRequired"},
		"summary":           {"phase", "total", "success", "skipped", "failed"},
		"error":             {"scope", "message", "id"},
		"artifact":          {"phase", "kind", "path"},
		"config-resolution": {"captureId", "moduleId", "configSetId", "sourceInstance", "sourceInstanceId", "targetInstanceId", "targetCandidates", "sourceGeneration", "sourceGenerationFingerprint", "targetGeneration", "resolution", "reason", "migrationPath", "captureModuleRevision", "restoreModuleRevision", "label", "message", "remediation"},
		"config-migration":  {"captureId", "configSetId", "stage", "fromGeneration", "toGeneration", "status", "reason", "message", "remediation"},
		"restore-item":      {"id", "module", "restorer", "source", "target", "status", "reason", "backupPath", "targetExisted", "message", "captureId", "configSetId", "targetInstanceId", "sourceGeneration", "targetGeneration"},
		"consent":           {"backends", "message", "details"},
		"backup-chunk":      {"chunkIndex", "totalChunks", "encryptedSize", "status", "message", "attempt", "maxAttempts", "current", "total"},
	}
	allowed := map[string]struct{}{}
	for _, name := range append(base, fields[eventType]...) {
		allowed[name] = struct{}{}
	}
	for name := range event {
		if _, ok := allowed[name]; !ok {
			return false
		}
	}
	return true
}

func validateEventShape(eventType string, event map[string]any) *Failure {
	bad := func(coordinate, detail string) *Failure { return fail(CodeEventContract, "events", coordinate, detail) }
	switch eventType {
	case "phase":
		if !eventString(event, "phase", true) {
			return bad("phase", "phase event lacks a nonempty phase")
		}
	case "progress":
		if !eventString(event, "phase", true) || !eventString(event, "stage", true) {
			return bad("progress", "progress event lacks phase or stage")
		}
	case "item":
		if !eventString(event, "id", true) || !eventString(event, "driver", true) || !eventString(event, "status", true) || !eventNullableString(event, "reason") {
			return bad("item", "item event lacks id, driver, status, or reason")
		}
		if event["status"] == "failed" {
			return bad("item.status", "item event reported failed status")
		}
	case "summary":
		if !eventString(event, "phase", true) {
			return bad("summary.phase", "summary lacks phase")
		}
		total, totalOK := eventNonnegativeInt(event, "total")
		success, successOK := eventNonnegativeInt(event, "success")
		skipped, skippedOK := eventNonnegativeInt(event, "skipped")
		failed, failedOK := eventNonnegativeInt(event, "failed")
		if !totalOK || !successOK || !skippedOK || !failedOK || total != success+skipped+failed || failed != 0 {
			return bad("summary", "summary counts are negative, inconsistent, or failed")
		}
	case "error":
		return bad("error", "error event is incompatible with passing proof")
	case "artifact":
		if !eventString(event, "phase", true) || !eventString(event, "kind", true) || !eventString(event, "path", true) {
			return bad("artifact", "artifact event lacks phase, kind, or path")
		}
	case "config-resolution":
		for _, key := range []string{"captureId", "moduleId", "configSetId", "resolution", "label", "message"} {
			if !eventString(event, key, key != "label" && key != "message") {
				return bad("config-resolution", "config-resolution event lacks required string fields")
			}
		}
		if _, ok := event["targetCandidates"].([]any); !ok || !eventNullableValue(event, "reason") || !eventNullableString(event, "remediation") {
			return bad("config-resolution", "config-resolution event lacks required collection or nullable fields")
		}
		if _, ok := event["migrationPath"].([]any); !ok {
			return bad("config-resolution", "config-resolution migrationPath is absent")
		}
	case "config-migration":
		for _, key := range []string{"captureId", "configSetId", "stage", "status", "message"} {
			if !eventString(event, key, key != "message") {
				return bad("config-migration", "config-migration event lacks required string fields")
			}
		}
		if !eventNullableString(event, "reason") || !eventNullableString(event, "remediation") || event["status"] == "failed" {
			return bad("config-migration.status", "config-migration event is malformed or failed")
		}
	case "restore-item":
		for _, key := range []string{"id", "module", "restorer", "source", "target", "status", "message"} {
			if !eventString(event, key, key != "message") {
				return bad("restore-item", "restore-item event lacks required string fields")
			}
		}
		if !eventNullableString(event, "reason") || !eventNullableString(event, "backupPath") {
			return bad("restore-item", "restore-item event lacks nullable reason or backupPath")
		}
		if _, ok := event["targetExisted"].(bool); !ok || event["status"] == "failed" {
			return bad("restore-item.status", "restore-item event is malformed or failed")
		}
	case "consent":
		if !eventString(event, "message", true) || !eventStringArray(event, "backends", true) {
			return bad("consent", "consent event lacks message or backends")
		}
	case "backup-chunk":
		if !eventString(event, "status", true) || event["status"] == "failed" {
			return bad("backup-chunk.status", "backup-chunk event is malformed or failed")
		}
		for _, key := range []string{"chunkIndex", "totalChunks", "encryptedSize", "current", "total"} {
			if _, ok := eventInteger(event, key); !ok {
				return bad("backup-chunk", "backup-chunk event lacks integer fields")
			}
		}
	}
	return nil
}

func eventString(event map[string]any, key string, nonempty bool) bool {
	value, ok := event[key].(string)
	return ok && (!nonempty || strings.TrimSpace(value) != "")
}

func eventNullableString(event map[string]any, key string) bool {
	value, exists := event[key]
	if !exists || value == nil {
		return exists
	}
	_, ok := value.(string)
	return ok
}

func eventNullableValue(event map[string]any, key string) bool {
	_, exists := event[key]
	return exists
}

func eventInteger(event map[string]any, key string) (int, bool) {
	value, ok := event[key].(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.Atoi(value.String())
	return parsed, err == nil
}

func eventNonnegativeInt(event map[string]any, key string) (int, bool) {
	value, ok := eventInteger(event, key)
	return value, ok && value >= 0
}

func eventStringArray(event map[string]any, key string, nonempty bool) bool {
	values, ok := event[key].([]any)
	if !ok || nonempty && len(values) == 0 {
		return false
	}
	for _, value := range values {
		if text, ok := value.(string); !ok || strings.TrimSpace(text) == "" {
			return false
		}
	}
	return true
}

func knownEventType(value string) bool {
	switch value {
	case "phase", "progress", "item", "summary", "error", "artifact", "config-resolution", "config-migration", "restore-item", "consent", "backup-chunk":
		return true
	default:
		return false
	}
}

func leaked(value []byte, forbidden ...string) bool {
	text := strings.ToLower(string(value))
	for _, item := range forbidden {
		if item != "" && strings.Contains(text, strings.ToLower(item)) {
			return true
		}
	}
	return false
}
