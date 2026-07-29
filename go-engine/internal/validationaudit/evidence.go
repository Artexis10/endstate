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
	var evidence AttemptEvidence
	if err := decoder.Decode(&evidence); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return AttemptEvidence{}, ErrUnknownField
		}
		return AttemptEvidence{}, ErrInvalidEvidence
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return AttemptEvidence{}, ErrTrailingJSONValue
	}
	if evidence.SchemaVersion != SchemaVersion {
		return AttemptEvidence{}, ErrUnsupportedSchema
	}
	if !validAttemptEvidence(evidence) {
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
	raw, err := json.Marshal(evidence)
	if err != nil || len(raw)+1 > MaxEvidenceSize {
		return nil, "", ErrInvalidEvidence
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	return raw, hexDigest(sum), nil
}

func validAttemptEvidence(evidence AttemptEvidence) bool {
	if evidence.SchemaVersion != SchemaVersion || !validCorpusVersion(evidence.AuditVersion) || !sha256Pattern.MatchString(evidence.AuditSHA256) || !validCorpusVersion(evidence.CorpusVersion) || !sha256Pattern.MatchString(evidence.CorpusSHA256) || !validReference(evidence.Reference) || !identifierPattern.MatchString(evidence.AttemptID) || !validRunner(evidence.Runner) || !validToolchain(evidence.Toolchain) || !sha256Pattern.MatchString(evidence.CommandSHA256) || !validAttemptTiming(evidence) || !validExitClass(evidence.ExitClass) || !validEvidenceFailure(evidence.ExitClass, evidence.Failure) {
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
	return identifierPattern.MatchString(evidence.CandidateID) && sha256Pattern.MatchString(evidence.PatchSHA256) && sha1Pattern.MatchString(evidence.MutatedTree)
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
	return identifierPattern.MatchString(detector.ID) && sha256Pattern.MatchString(detector.ContractSHA256)
}

func validRunner(runner RunnerIdentity) bool {
	if runner.OS != "windows" && runner.OS != "linux" && runner.OS != "darwin" {
		return false
	}
	return (runner.Architecture == "amd64" || runner.Architecture == "arm64") && identifierPattern.MatchString(runner.Image)
}

func validToolchain(toolchain ToolchainIdentity) bool {
	return identifierPattern.MatchString(toolchain.Go)
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
	startMillis := start.Unix()*1000 + int64(start.Nanosecond()/int(time.Millisecond))
	endMillis := end.Unix()*1000 + int64(end.Nanosecond()/int(time.Millisecond))
	return endMillis >= startMillis && evidence.DurationMillis == endMillis-startMillis
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
	return (exitClass == ExitClassRejected || exitClass == ExitClassTimeout) && stableValuePattern.MatchString(failure.Class) && stableValuePattern.MatchString(failure.Phase) && stableValuePattern.MatchString(failure.Coordinate)
}
