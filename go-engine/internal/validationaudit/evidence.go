// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const (
	// MaxEvidenceSize bounds one compact attempt evidence document.
	MaxEvidenceSize = 64 * 1024

	EvidenceKindControl          = "control"
	EvidenceKindDetectorBaseline = "detector-baseline"
	EvidenceKindDetectorMutation = "detector-mutation"

	ExitClassPassed         = "passed"
	ExitClassRejected       = "rejected"
	ExitClassTimeout        = "timeout"
	ExitClassCanceled       = "canceled"
	ExitClassInfrastructure = "infrastructure"
)

var (
	ErrInvalidEvidence       = errors.New("validation audit invalid evidence")
	ErrUnsafeEvidencePath    = errors.New("validation audit unsafe result authority")
	ErrEvidenceAlreadyExists = errors.New("validation audit evidence already exists")
	ErrEvidencePublication   = errors.New("validation audit evidence publication failure")
)

// DetectorEvidence binds one detector to its reviewed contract bytes.
type DetectorEvidence struct {
	ID             string `json:"id"`
	ContractSHA256 string `json:"contractSha256"`
}

// RunnerIdentity binds the disposable runner used for the attempt.
type RunnerIdentity struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Image        string `json:"image"`
}

// ToolchainIdentity binds the exact Go toolchain identity.
type ToolchainIdentity struct {
	Go string `json:"go"`
}

// StableFailure preserves only an expected bounded behavioral result.
type StableFailure struct {
	Class      string `json:"class"`
	Phase      string `json:"phase"`
	Coordinate string `json:"coordinate"`
}

// AttemptEvidence is one closed, reproducible audit attempt record.
type AttemptEvidence struct {
	SchemaVersion  int               `json:"schemaVersion"`
	AuditVersion   string            `json:"auditVersion"`
	AuditSHA256    string            `json:"auditSha256"`
	CorpusVersion  string            `json:"corpusVersion"`
	CorpusSHA256   string            `json:"corpusSha256"`
	Reference      ReferenceIdentity `json:"reference"`
	Kind           string            `json:"kind"`
	AttemptID      string            `json:"attemptId"`
	Repetition     int               `json:"repetition"`
	CandidateID    string            `json:"candidateId,omitempty"`
	PatchSHA256    string            `json:"patchSha256,omitempty"`
	MutatedTree    string            `json:"mutatedTree,omitempty"`
	Lane           string            `json:"lane,omitempty"`
	Detector       DetectorEvidence  `json:"detector,omitempty"`
	Runner         RunnerIdentity    `json:"runner"`
	Toolchain      ToolchainIdentity `json:"toolchain"`
	CommandSHA256  string            `json:"commandSha256"`
	StartedAt      string            `json:"startedAt"`
	EndedAt        string            `json:"endedAt"`
	DurationMillis int64             `json:"durationMillis"`
	ExitClass      string            `json:"exitClass"`
	Failure        *StableFailure    `json:"failure,omitempty"`
}

type attemptEvidenceWire struct {
	SchemaVersion  int               `json:"schemaVersion"`
	AuditVersion   string            `json:"auditVersion"`
	AuditSHA256    string            `json:"auditSha256"`
	CorpusVersion  string            `json:"corpusVersion"`
	CorpusSHA256   string            `json:"corpusSha256"`
	Reference      ReferenceIdentity `json:"reference"`
	Kind           string            `json:"kind"`
	AttemptID      string            `json:"attemptId"`
	Repetition     int               `json:"repetition"`
	CandidateID    *string           `json:"candidateId,omitempty"`
	PatchSHA256    *string           `json:"patchSha256,omitempty"`
	MutatedTree    *string           `json:"mutatedTree,omitempty"`
	Lane           *string           `json:"lane,omitempty"`
	Detector       *DetectorEvidence `json:"detector,omitempty"`
	Runner         RunnerIdentity    `json:"runner"`
	Toolchain      ToolchainIdentity `json:"toolchain"`
	CommandSHA256  string            `json:"commandSha256"`
	StartedAt      string            `json:"startedAt"`
	EndedAt        string            `json:"endedAt"`
	DurationMillis int64             `json:"durationMillis"`
	ExitClass      string            `json:"exitClass"`
	Failure        *StableFailure    `json:"failure,omitempty"`
}

// PublishedEvidence describes an exact, verified create-new publication.
type PublishedEvidence struct {
	SHA256 string
	Size   int64
}

// DecodeAttemptEvidence strictly decodes one bounded evidence document.
func DecodeAttemptEvidence(raw []byte) (AttemptEvidence, error) {
	if len(raw) == 0 {
		return AttemptEvidence{}, ErrInvalidEvidence
	}
	if len(raw) > MaxEvidenceSize {
		return AttemptEvidence{}, ErrInputTooLarge
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		if errors.Is(err, ErrDuplicateJSONKey) {
			return AttemptEvidence{}, ErrDuplicateJSONKey
		}
		if errors.Is(err, ErrTrailingJSONValue) {
			return AttemptEvidence{}, ErrTrailingJSONValue
		}
		return AttemptEvidence{}, ErrInvalidEvidence
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire attemptEvidenceWire
	if err := decoder.Decode(&wire); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return AttemptEvidence{}, ErrUnknownField
		}
		return AttemptEvidence{}, ErrInvalidEvidence
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return AttemptEvidence{}, ErrTrailingJSONValue
	}
	if wire.SchemaVersion != SchemaVersion {
		return AttemptEvidence{}, ErrUnsupportedSchema
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return AttemptEvidence{}, ErrInvalidEvidence
	}
	evidence := evidenceFromWire(wire)
	if !validAttemptEvidence(evidence) || !validAttemptWire(wire, fields) {
		return AttemptEvidence{}, ErrInvalidEvidence
	}
	return evidence, nil
}

// EncodeAttemptEvidence validates and deterministically encodes one closed
// evidence record. The returned digest covers the exact newline-terminated bytes.
func EncodeAttemptEvidence(evidence AttemptEvidence) ([]byte, string, error) {
	if !validAttemptEvidence(evidence) {
		return nil, "", ErrInvalidEvidence
	}
	raw, err := json.Marshal(evidenceWire(evidence))
	if err != nil || len(raw)+1 > MaxEvidenceSize {
		return nil, "", ErrInvalidEvidence
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	return raw, hexDigest(sum), nil
}

func validAttemptEvidence(evidence AttemptEvidence) bool {
	if evidence.SchemaVersion != SchemaVersion || !validAuditIdentifier(evidence.AuditVersion) || !sha256Pattern.MatchString(evidence.AuditSHA256) || !validAuditIdentifier(evidence.CorpusVersion) || !sha256Pattern.MatchString(evidence.CorpusSHA256) || !validReference(evidence.Reference) || !validAuditIdentifier(evidence.AttemptID) || !validRunner(evidence.Runner) || !validToolchain(evidence.Toolchain) || !sha256Pattern.MatchString(evidence.CommandSHA256) || !validAttemptTiming(evidence) || !validExitClass(evidence.ExitClass) || !validEvidenceFailure(evidence.ExitClass, evidence.Failure) {
		return false
	}
	switch evidence.Kind {
	case EvidenceKindControl:
		return evidence.Repetition == 1 && validMutationIdentity(evidence) && validLegacyLaneID(evidence.Lane) && evidence.Detector == (DetectorEvidence{})
	case EvidenceKindDetectorBaseline:
		return validRepetition(evidence.Repetition) && evidence.CandidateID == "" && evidence.PatchSHA256 == "" && evidence.MutatedTree == "" && evidence.Lane == "" && validDetectorEvidence(evidence.Detector)
	case EvidenceKindDetectorMutation:
		return validRepetition(evidence.Repetition) && validMutationIdentity(evidence) && evidence.Lane == "" && validDetectorEvidence(evidence.Detector)
	default:
		return false
	}
}

func validMutationIdentity(evidence AttemptEvidence) bool {
	return validAuditIdentifier(evidence.CandidateID) && sha256Pattern.MatchString(evidence.PatchSHA256) && sha1Pattern.MatchString(evidence.MutatedTree)
}

func validRepetition(value int) bool {
	return value == 1 || value == 2
}

func validLegacyLaneID(value string) bool {
	for _, lane := range legacyLaneOrder {
		if value == lane.ID {
			return true
		}
	}
	return false
}

func validDetectorEvidence(detector DetectorEvidence) bool {
	return validAuditIdentifier(detector.ID) && sha256Pattern.MatchString(detector.ContractSHA256)
}

func validRunner(runner RunnerIdentity) bool {
	if runner.OS != "windows" && runner.OS != "linux" && runner.OS != "darwin" {
		return false
	}
	return (runner.Architecture == "amd64" || runner.Architecture == "arm64") && validAuditIdentifier(runner.Image)
}

func validToolchain(toolchain ToolchainIdentity) bool {
	return validAuditIdentifier(toolchain.Go)
}

func validAttemptTiming(evidence AttemptEvidence) bool {
	if evidence.DurationMillis < 0 {
		return false
	}
	start, startOK := canonicalUTCTime(evidence.StartedAt)
	end, endOK := canonicalUTCTime(evidence.EndedAt)
	if !startOK || !endOK || end.Before(start) {
		return false
	}
	elapsed := end.Sub(start)
	if elapsed < 0 || elapsed%time.Millisecond != 0 || evidence.DurationMillis > int64(^uint64(0)>>1)/int64(time.Millisecond) {
		return false
	}
	return elapsed == time.Duration(evidence.DurationMillis)*time.Millisecond
}

func canonicalUTCTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC {
		return time.Time{}, false
	}
	return parsed, parsed.Format(time.RFC3339Nano) == value
}

func validExitClass(value string) bool {
	switch value {
	case ExitClassPassed, ExitClassRejected, ExitClassTimeout, ExitClassCanceled, ExitClassInfrastructure:
		return true
	default:
		return false
	}
}

func validEvidenceFailure(exitClass string, failure *StableFailure) bool {
	if failure == nil {
		return true
	}
	return (exitClass == ExitClassRejected || exitClass == ExitClassTimeout) && validStableAuditValue(failure.Class) && validStableAuditValue(failure.Phase) && validStableAuditValue(failure.Coordinate)
}

func validAuditIdentifier(value string) bool {
	return identifierPattern.MatchString(value) && !unsafeAuditText(value)
}

func validStableAuditValue(value string) bool {
	return stableValuePattern.MatchString(value) && !unsafeAuditText(value)
}

func unsafeAuditText(value string) bool {
	lower := strings.ToLower(value)
	if strings.ContainsAny(value, `/\\`) || len(value) >= 2 && value[1] == ':' && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) {
		return true
	}
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "akia", "asia", "sk-", "sk-proj-", "glpat-", "npm_", "pypi-", "xox", "bearer", "eyj"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func evidenceFromWire(wire attemptEvidenceWire) AttemptEvidence {
	evidence := AttemptEvidence{SchemaVersion: wire.SchemaVersion, AuditVersion: wire.AuditVersion, AuditSHA256: wire.AuditSHA256, CorpusVersion: wire.CorpusVersion, CorpusSHA256: wire.CorpusSHA256, Reference: wire.Reference, Kind: wire.Kind, AttemptID: wire.AttemptID, Repetition: wire.Repetition, Runner: wire.Runner, Toolchain: wire.Toolchain, CommandSHA256: wire.CommandSHA256, StartedAt: wire.StartedAt, EndedAt: wire.EndedAt, DurationMillis: wire.DurationMillis, ExitClass: wire.ExitClass, Failure: wire.Failure}
	if wire.CandidateID != nil {
		evidence.CandidateID = *wire.CandidateID
	}
	if wire.PatchSHA256 != nil {
		evidence.PatchSHA256 = *wire.PatchSHA256
	}
	if wire.MutatedTree != nil {
		evidence.MutatedTree = *wire.MutatedTree
	}
	if wire.Lane != nil {
		evidence.Lane = *wire.Lane
	}
	if wire.Detector != nil {
		evidence.Detector = *wire.Detector
	}
	return evidence
}

func evidenceWire(evidence AttemptEvidence) attemptEvidenceWire {
	wire := attemptEvidenceWire{SchemaVersion: evidence.SchemaVersion, AuditVersion: evidence.AuditVersion, AuditSHA256: evidence.AuditSHA256, CorpusVersion: evidence.CorpusVersion, CorpusSHA256: evidence.CorpusSHA256, Reference: evidence.Reference, Kind: evidence.Kind, AttemptID: evidence.AttemptID, Repetition: evidence.Repetition, Runner: evidence.Runner, Toolchain: evidence.Toolchain, CommandSHA256: evidence.CommandSHA256, StartedAt: evidence.StartedAt, EndedAt: evidence.EndedAt, DurationMillis: evidence.DurationMillis, ExitClass: evidence.ExitClass, Failure: evidence.Failure}
	switch evidence.Kind {
	case EvidenceKindControl:
		wire.CandidateID, wire.PatchSHA256, wire.MutatedTree, wire.Lane = &evidence.CandidateID, &evidence.PatchSHA256, &evidence.MutatedTree, &evidence.Lane
	case EvidenceKindDetectorBaseline:
		wire.Detector = &evidence.Detector
	case EvidenceKindDetectorMutation:
		wire.CandidateID, wire.PatchSHA256, wire.MutatedTree, wire.Detector = &evidence.CandidateID, &evidence.PatchSHA256, &evidence.MutatedTree, &evidence.Detector
	}
	return wire
}

func validAttemptWire(wire attemptEvidenceWire, fields map[string]json.RawMessage) bool {
	switch wire.Kind {
	case EvidenceKindControl:
		return wire.CandidateID != nil && wire.PatchSHA256 != nil && wire.MutatedTree != nil && wire.Lane != nil && wire.Detector == nil && !wireHasField(fields, "detector")
	case EvidenceKindDetectorBaseline:
		return wire.CandidateID == nil && wire.PatchSHA256 == nil && wire.MutatedTree == nil && wire.Lane == nil && wire.Detector != nil && !wireHasField(fields, "candidateId") && !wireHasField(fields, "patchSha256") && !wireHasField(fields, "mutatedTree") && !wireHasField(fields, "lane")
	case EvidenceKindDetectorMutation:
		return wire.CandidateID != nil && wire.PatchSHA256 != nil && wire.MutatedTree != nil && wire.Lane == nil && wire.Detector != nil && !wireHasField(fields, "lane")
	default:
		return false
	}
}

func wireHasField(fields map[string]json.RawMessage, name string) bool {
	_, present := fields[name]
	return present
}
