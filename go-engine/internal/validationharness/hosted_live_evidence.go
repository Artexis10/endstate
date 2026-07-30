// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	hostedLiveEvidenceSchemaVersion = 1
	maxHostedLiveEvidenceBytes      = 64 * 1024
	hostedLiveEvidenceFilename      = "hosted-live-evidence.json"
)

// hostedLiveEvidence is the fixed, public-safe projection of one candidate
// hosted attempt. It intentionally contains no host paths, captured bytes,
// process output, archive contents, or authority material.
type hostedLiveEvidence struct {
	Campaign               string                    `json:"-"`
	Run                    hostedLiveEvidenceRun     `json:"-"`
	Engine                 hostedLiveEvidenceEngine  `json:"-"`
	Inputs                 hostedLiveEvidenceInputs  `json:"-"`
	Capture                hostedLiveEvidenceCapture `json:"-"`
	Runner                 hostedLiveEvidenceRunner  `json:"-"`
	Package                hostedLiveEvidencePackage `json:"-"`
	Phases                 []hostedLiveEvidencePhase `json:"-"`
	Status                 string                    `json:"-"`
	Candidate              bool                      `json:"-"`
	PublicEvidenceEligible bool                      `json:"-"`
	Failure                hostedLiveEvidenceFailure `json:"-"`
	Cleanup                hostedLiveEvidenceCleanup `json:"-"`
}

type hostedLiveEvidenceRun struct {
	ID            uint64 `json:"id"`
	Attempt       int    `json:"attempt"`
	Event         string `json:"event"`
	Ref           string `json:"ref"`
	TrustedCommit string `json:"trustedCommit"`
}

type hostedLiveEvidenceEngine struct {
	Commit          string `json:"commit"`
	Version         string `json:"version"`
	SHA256          string `json:"sha256"`
	ValidatorSHA256 string `json:"validatorSha256"`
}

type hostedLiveEvidenceInputs struct {
	DefinitionSHA256       string `json:"definitionSha256"`
	ModuleSHA256           string `json:"moduleSha256"`
	ValidationSourceSHA256 string `json:"validationSourceSha256"`
	SeedSHA256             string `json:"seedSha256"`
	ComparatorSHA256       string `json:"comparatorSha256"`
	TargetsSHA256          string `json:"targetsSha256"`
	ObserverSHA256         string `json:"observerSha256"`
	WorkflowSHA256         string `json:"workflowSha256"`
}

type hostedLiveEvidenceCapture struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type hostedLiveEvidenceRunner struct {
	OS    string `json:"os"`
	Image string `json:"image"`
}

type hostedLiveEvidencePackage struct {
	Driver  string `json:"driver"`
	Source  string `json:"source"`
	Ref     string `json:"ref"`
	Version string `json:"version"`
}

type hostedLiveEvidencePhase struct {
	Index                int    `json:"index"`
	Name                 string `json:"name"`
	Status               string `json:"status"`
	DurationMilliseconds int64  `json:"durationMilliseconds"`
	Assertions           int    `json:"assertions"`
}

type hostedLiveEvidenceFailure struct {
	Code       string `json:"code"`
	Phase      string `json:"phase"`
	PhaseIndex int    `json:"phaseIndex"`
}

type hostedLiveEvidenceCleanup struct {
	Status      string `json:"status"`
	FailureCode string `json:"failureCode"`
}

type hostedLiveEvidenceWire struct {
	SchemaVersion          int                       `json:"schemaVersion"`
	Campaign               string                    `json:"campaign"`
	Run                    hostedLiveEvidenceRun     `json:"run"`
	Engine                 hostedLiveEvidenceEngine  `json:"engine"`
	Inputs                 hostedLiveEvidenceInputs  `json:"inputs"`
	Capture                hostedLiveEvidenceCapture `json:"capture"`
	Runner                 hostedLiveEvidenceRunner  `json:"runner"`
	Package                hostedLiveEvidencePackage `json:"package"`
	Phases                 []hostedLiveEvidencePhase `json:"phases"`
	Status                 string                    `json:"status"`
	Candidate              bool                      `json:"candidate"`
	PublicEvidenceEligible bool                      `json:"publicEvidenceEligible"`
	Failure                hostedLiveEvidenceFailure `json:"failure"`
	Cleanup                hostedLiveEvidenceCleanup `json:"cleanup"`
}

func encodeHostedLiveEvidence(evidence hostedLiveEvidence) ([]byte, error) {
	return encodeHostedLiveEvidenceWithLimit(evidence, maxHostedLiveEvidenceBytes)
}

func encodeHostedLiveEvidenceWithLimit(evidence hostedLiveEvidence, limit int) ([]byte, error) {
	if limit < 1 || limit > maxHostedLiveEvidenceBytes {
		return nil, fmt.Errorf("hosted live evidence limit is invalid")
	}
	if err := validateHostedLiveEvidence(evidence); err != nil {
		return nil, err
	}
	wire := hostedLiveEvidenceWire{
		SchemaVersion: hostedLiveEvidenceSchemaVersion,
		Campaign:      evidence.Campaign, Run: evidence.Run, Engine: evidence.Engine,
		Inputs: evidence.Inputs, Capture: evidence.Capture, Runner: evidence.Runner,
		Package: evidence.Package, Phases: append([]hostedLiveEvidencePhase(nil), evidence.Phases...),
		Status: evidence.Status, Candidate: true, PublicEvidenceEligible: false,
		Failure: evidence.Failure, Cleanup: evidence.Cleanup,
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode hosted live evidence: %w", err)
	}
	if len(encoded) > limit {
		return nil, fmt.Errorf("hosted live evidence exceeds the compact result limit")
	}
	return encoded, nil
}

func validateHostedLiveEvidence(evidence hostedLiveEvidence) error {
	if !lowerSHA256(evidence.Campaign) || evidence.Run.ID == 0 || evidence.Run.Attempt != 1 ||
		(evidence.Run.Event != "schedule" && evidence.Run.Event != "workflow_dispatch") || evidence.Run.Ref != "refs/heads/main" || !liveCommitSHA(evidence.Run.TrustedCommit) ||
		!liveCommitSHA(evidence.Engine.Commit) || !validHostedLiveEvidenceValue(evidence.Engine.Version) || !lowerSHA256(evidence.Engine.SHA256) || !lowerSHA256(evidence.Engine.ValidatorSHA256) ||
		!hostedLiveEvidenceHashes(evidence.Inputs) || evidence.Runner.OS != "windows" || !validHostedLiveEvidenceValue(evidence.Runner.Image) ||
		evidence.Package.Driver != "winget" || evidence.Package.Source != "winget" || evidence.Package.Ref != "Notepad++.Notepad++" || (evidence.Package.Version != "" && !validHostedLiveEvidenceValue(evidence.Package.Version)) ||
		!evidence.Candidate || evidence.PublicEvidenceEligible || (evidence.Status != "passed" && evidence.Status != "failed") ||
		(evidence.Capture.Size < 0 || (evidence.Capture.SHA256 != "" && !lowerSHA256(evidence.Capture.SHA256)) || (evidence.Capture.SHA256 == "" && evidence.Capture.Size != 0)) {
		return fmt.Errorf("hosted live evidence identity is invalid")
	}
	if evidence.Status == "passed" && (evidence.Capture.Size == 0 || !lowerSHA256(evidence.Capture.SHA256) || evidence.Package.Version == "" || evidence.Cleanup.Status != "passed" || evidence.Failure != (hostedLiveEvidenceFailure{})) {
		return fmt.Errorf("hosted live passed evidence is invalid")
	}
	if !validHostedLiveEvidenceCode(evidence.Failure.Code) || !validHostedLiveEvidenceCode(evidence.Cleanup.FailureCode) || (evidence.Cleanup.Status != "passed" && evidence.Cleanup.Status != "failed") ||
		(evidence.Cleanup.Status == "passed" && evidence.Cleanup.FailureCode != "") || (evidence.Cleanup.Status == "failed" && evidence.Cleanup.FailureCode == "") {
		return fmt.Errorf("hosted live failure evidence is invalid")
	}
	if err := validateHostedLiveEvidencePhases(evidence); err != nil {
		return err
	}
	return nil
}

func hostedLiveEvidenceHashes(inputs hostedLiveEvidenceInputs) bool {
	for _, value := range []string{inputs.DefinitionSHA256, inputs.ModuleSHA256, inputs.ValidationSourceSHA256, inputs.SeedSHA256, inputs.ComparatorSHA256, inputs.TargetsSHA256, inputs.ObserverSHA256, inputs.WorkflowSHA256} {
		if !lowerSHA256(value) {
			return false
		}
	}
	return true
}

func validateHostedLiveEvidencePhases(evidence hostedLiveEvidence) error {
	expected := append(append([]string(nil), hostedLiveLifecycle...), hostedLiveCleanup...)
	if len(evidence.Phases) != len(expected) {
		return fmt.Errorf("hosted live evidence phases are incomplete")
	}
	firstFailure, firstCleanupFailure := -1, -1
	for index, phase := range evidence.Phases {
		if phase.Index != index+1 || phase.Name != expected[index] || (phase.Status != "passed" && phase.Status != "failed" && phase.Status != "skipped") || phase.DurationMilliseconds < 0 || phase.DurationMilliseconds > 45*60*1000 || phase.Assertions < 0 || phase.Assertions > 1_000_000 {
			return fmt.Errorf("hosted live evidence phase is invalid")
		}
		if (phase.Status == "skipped" && (phase.Assertions != 0 || phase.DurationMilliseconds != 0)) || (phase.Status != "skipped" && phase.Assertions != 1) {
			return fmt.Errorf("hosted live evidence phase assertions are invalid")
		}
		if index < len(hostedLiveLifecycle) {
			if firstFailure < 0 && phase.Status == "skipped" {
				return fmt.Errorf("hosted live evidence skipped before failure")
			}
			if firstFailure >= 0 && phase.Status != "skipped" {
				return fmt.Errorf("hosted live evidence continued after lifecycle failure")
			}
		}
		if phase.Status == "failed" && firstFailure < 0 {
			firstFailure = index
		}
		if index >= len(hostedLiveLifecycle) && phase.Status == "skipped" {
			return fmt.Errorf("hosted live evidence skipped cleanup")
		}
		if index >= len(hostedLiveLifecycle) && phase.Status == "failed" && firstCleanupFailure < 0 {
			firstCleanupFailure = index
		}
	}
	if evidence.Status == "passed" && firstFailure >= 0 {
		return fmt.Errorf("hosted live passed evidence has a failed phase")
	}
	if evidence.Status == "passed" {
		for _, phase := range evidence.Phases {
			if phase.Status != "passed" {
				return fmt.Errorf("hosted live passed evidence has a non-passing phase")
			}
		}
	}
	if evidence.Status == "failed" && (firstFailure < 0 || evidence.Failure.Code != hostedLiveEvidenceFailureCode(evidence.Phases[firstFailure].Name) || evidence.Failure.Phase != evidence.Phases[firstFailure].Name || evidence.Failure.PhaseIndex != firstFailure+1) {
		return fmt.Errorf("hosted live failed evidence does not bind its failed phase")
	}
	if evidence.Status == "failed" {
		if firstFailure > hostedLiveLifecyclePhaseIndex("observe-present") && !validHostedLiveEvidenceValue(evidence.Package.Version) {
			return fmt.Errorf("hosted live failed evidence omitted observed package version")
		}
		if firstFailure > hostedLiveLifecyclePhaseIndex("inspect-capture") && (evidence.Capture.Size == 0 || !lowerSHA256(evidence.Capture.SHA256)) {
			return fmt.Errorf("hosted live failed evidence omitted observed capture")
		}
	}
	cleanupFailed := firstCleanupFailure >= 0
	if (evidence.Cleanup.Status == "failed") != cleanupFailed || (cleanupFailed && evidence.Cleanup.FailureCode == "") {
		return fmt.Errorf("hosted live cleanup failure is not bound")
	}
	if cleanupFailed && evidence.Cleanup.FailureCode != hostedLiveEvidenceFailureCode(evidence.Phases[firstCleanupFailure].Name) {
		return fmt.Errorf("hosted live cleanup failure code is not bound")
	}
	if cleanupFailed && firstFailure >= len(hostedLiveLifecycle) && evidence.Failure.Code != evidence.Cleanup.FailureCode {
		return fmt.Errorf("hosted live cleanup failure code is not bound")
	}
	return nil
}

func hostedLiveLifecyclePhaseIndex(name string) int {
	for index, phase := range hostedLiveLifecycle {
		if phase == name {
			return index
		}
	}
	return len(hostedLiveLifecycle)
}

func hostedLiveEvidenceFromRun(evidence hostedLiveEvidence, result hostedLiveRunResult) (hostedLiveEvidence, error) {
	expected := append(append([]string(nil), hostedLiveLifecycle...), hostedLiveCleanup...)
	if len(result.phases) != len(expected) {
		return hostedLiveEvidence{}, fmt.Errorf("hosted live phase record is incomplete")
	}
	evidence.Phases = make([]hostedLiveEvidencePhase, len(result.phases))
	firstFailure, firstCleanupFailure := -1, -1
	for index, record := range result.phases {
		if record.name != expected[index] || (record.status != "passed" && record.status != "failed" && record.status != "skipped") || record.durationMilliseconds < 0 || record.assertions < 0 {
			return hostedLiveEvidence{}, fmt.Errorf("hosted live phase record is invalid")
		}
		evidence.Phases[index] = hostedLiveEvidencePhase{Index: index + 1, Name: record.name, Status: record.status, DurationMilliseconds: record.durationMilliseconds, Assertions: record.assertions}
		if record.status == "failed" && firstFailure < 0 {
			firstFailure = index
		}
		if index >= len(hostedLiveLifecycle) && record.status == "failed" && firstCleanupFailure < 0 {
			firstCleanupFailure = index
		}
	}
	evidence.Candidate, evidence.PublicEvidenceEligible = true, false
	if firstFailure < 0 {
		evidence.Status, evidence.Failure, evidence.Cleanup = "passed", hostedLiveEvidenceFailure{}, hostedLiveEvidenceCleanup{Status: "passed"}
	} else {
		failed := evidence.Phases[firstFailure]
		evidence.Status = "failed"
		evidence.Failure = hostedLiveEvidenceFailure{Code: hostedLiveEvidenceFailureCode(failed.Name), Phase: failed.Name, PhaseIndex: failed.Index}
		evidence.Cleanup = hostedLiveEvidenceCleanup{Status: "passed"}
		if firstCleanupFailure >= 0 {
			evidence.Cleanup = hostedLiveEvidenceCleanup{Status: "failed", FailureCode: hostedLiveEvidenceFailureCode(evidence.Phases[firstCleanupFailure].Name)}
		}
	}
	if err := validateHostedLiveEvidence(evidence); err != nil {
		return hostedLiveEvidence{}, err
	}
	return evidence, nil
}

func hostedLiveEvidenceFailureCode(phase string) string {
	return strings.ReplaceAll(phase, "-", "_") + "_failed"
}

func validHostedLiveEvidenceCode(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 64 || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func validHostedLiveEvidenceValue(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune(".-+_", character)) {
			return false
		}
	}
	return true
}

func hostedLiveEvidenceResultRootName(campaign string, runID uint64, attempt int) string {
	return fmt.Sprintf("hosted-live-%s-%d-%d", campaign, runID, attempt)
}

func hostedLiveEvidenceMatchesCampaign(evidence hostedLiveEvidence, campaign LiveCampaign, campaignID string) bool {
	return evidence.Campaign == campaignID && evidence.Run.ID == campaign.RunID && evidence.Run.Attempt == campaign.RunAttempt && evidence.Run.Event == campaign.Event && evidence.Run.Ref == campaign.Ref && evidence.Run.TrustedCommit == campaign.ControllerCommit &&
		evidence.Engine.Commit == campaign.TestedCheckoutCommit && evidence.Engine.SHA256 == campaign.EngineSHA256 && evidence.Engine.ValidatorSHA256 == campaign.ValidatorSHA256 &&
		evidence.Inputs.ModuleSHA256 == campaign.ModuleRevision && evidence.Inputs.ValidationSourceSHA256 == campaign.ValidationSourceSHA256 && evidence.Inputs.SeedSHA256 == campaign.SeedSHA256 && evidence.Inputs.ComparatorSHA256 == campaign.ComparatorSHA256 && evidence.Inputs.TargetsSHA256 == campaign.TargetsSHA256 && evidence.Inputs.ObserverSHA256 == campaign.ObserverSHA256 && evidence.Inputs.WorkflowSHA256 == campaign.WorkflowPolicySHA256 &&
		evidence.Package.Driver == campaign.PackageDriver && evidence.Package.Ref == campaign.PackageRef
}
