// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const (
	V1SchemaVersion   = 1
	V1MaxDocumentSize = 64 * 1024

	V1KindComparator       = "comparator"
	V1KindDetector         = "detector"
	V1LaneWindowsGo        = "windows-go"
	V1LaneUbuntuGo         = "ubuntu-go"
	V1LaneMacOSGo          = "macos-go"
	V1AdmissionAdmitted    = "admitted"
	V1AdmissionRejected    = "rejected"
	V1StatusPassed         = "passed"
	V1StatusRejected       = "rejected"
	V1StatusInfrastructure = "infrastructure"
	DecisionInconclusive   = "inconclusive"
)

var (
	v1ToolchainPattern  = regexp.MustCompile(`^go1\.26\.[0-9]+$`)
	v1SHA1Pattern       = regexp.MustCompile(`^[a-f0-9]{40}$`)
	v1IdentifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// V1Reference identifies one reviewed commit and tree authority.
type V1Reference struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

// V1Authorities binds the four independently reviewed v1 authorities.
type V1Authorities struct {
	Evaluated V1Reference `json:"evaluated"`
	Freeze    V1Reference `json:"freeze"`
	Corpus    V1Reference `json:"corpus"`
	Dispatch  V1Reference `json:"dispatch"`
}

// V1Fingerprint prevents a held-out candidate from reusing v0's operator or invariant.
type V1Fingerprint struct {
	OperatorFingerprint  string `json:"operatorFingerprint"`
	InvariantFingerprint string `json:"invariantFingerprint"`
}

// V1Failure is the exact bounded causal detector result.
type V1Failure struct {
	Class       string `json:"class"`
	Phase       string `json:"phase"`
	Coordinate  string `json:"coordinate"`
	ChildReason string `json:"childReason,omitempty"`
}

// V1Candidate is one preregistered held-out production mutation.
type V1Candidate struct {
	ID                   string    `json:"id"`
	Family               string    `json:"family"`
	PatchSHA256          string    `json:"patchSha256"`
	MutatedTree          string    `json:"mutatedTree"`
	OperatorFingerprint  string    `json:"operatorFingerprint"`
	InvariantFingerprint string    `json:"invariantFingerprint"`
	Expected             V1Failure `json:"expected"`
}

// V1Manifest defines the closed six-item v1 preflight denominator.
type V1Manifest struct {
	SchemaVersion            int             `json:"schemaVersion"`
	Authorities              V1Authorities   `json:"authorities"`
	Toolchain                string          `json:"toolchain"`
	ComparatorContractSHA256 string          `json:"comparatorContractSha256"`
	DetectorContractSHA256   string          `json:"detectorContractSha256"`
	Calibration              []V1Fingerprint `json:"calibration"`
	Candidates               []V1Candidate   `json:"candidates"`
}

// V1Attempt preserves only typed, bounded v1 proof data. The engine digest is
// diagnostic and deliberately excluded from repeatability identity.
type V1Attempt struct {
	CandidateID              string        `json:"candidateId"`
	Kind                     string        `json:"kind"`
	Lane                     string        `json:"lane,omitempty"`
	Repetition               int           `json:"repetition"`
	Authorities              V1Authorities `json:"authorities"`
	PatchSHA256              string        `json:"patchSha256"`
	MutatedTree              string        `json:"mutatedTree"`
	Toolchain                string        `json:"toolchain"`
	ComparatorContractSHA256 string        `json:"comparatorContractSha256,omitempty"`
	DetectorContractSHA256   string        `json:"detectorContractSha256,omitempty"`
	Admission                string        `json:"admission,omitempty"`
	Status                   string        `json:"status"`
	Failure                  *V1Failure    `json:"failure,omitempty"`
	DiagnosticEngineSHA256   string        `json:"diagnosticEngineSha256,omitempty"`
}

// V1Evidence is the complete typed proof inventory.
type V1Evidence struct {
	SchemaVersion int         `json:"schemaVersion"`
	Attempts      []V1Attempt `json:"attempts"`
}

type V1AggregateRow struct {
	ID             string `json:"id"`
	Classification string `json:"classification"`
}

// V1Aggregate is the six-item v1 decision output.
type V1Aggregate struct {
	SchemaVersion          int              `json:"schemaVersion"`
	Rows                   []V1AggregateRow `json:"rows"`
	Decision               string           `json:"decision"`
	CorrectKills           int              `json:"correctKills"`
	WrongKills             int              `json:"wrongKills"`
	Survivors              int              `json:"survivors"`
	InfrastructureFailures int              `json:"infrastructureFailures"`
	Flakes                 int              `json:"flakes"`
}

func DecodeV1Manifest(raw []byte) (V1Manifest, error) {
	var manifest V1Manifest
	if err := decodeV1(raw, &manifest); err != nil || !validV1Manifest(manifest) {
		return V1Manifest{}, errors.New("invalid v1 manifest")
	}
	return manifest, nil
}

func EncodeV1Manifest(manifest V1Manifest) ([]byte, string, error) {
	if !validV1Manifest(manifest) {
		return nil, "", errors.New("invalid v1 manifest")
	}
	return encodeV1(manifest)
}

func DecodeV1Evidence(raw []byte) (V1Evidence, error) {
	var evidence V1Evidence
	if err := decodeV1(raw, &evidence); err != nil || !validV1Evidence(evidence) {
		return V1Evidence{}, errors.New("invalid v1 evidence")
	}
	return evidence, nil
}

func EncodeV1Evidence(evidence V1Evidence) ([]byte, string, error) {
	if !validV1Evidence(evidence) {
		return nil, "", errors.New("invalid v1 evidence")
	}
	return encodeV1(evidence)
}

// DecodeV1Aggregate strictly reads one bounded v1 decision artifact.
func DecodeV1Aggregate(raw []byte) (V1Aggregate, error) {
	var aggregate V1Aggregate
	if err := decodeV1(raw, &aggregate); err != nil || !validV1Aggregate(aggregate) {
		return V1Aggregate{}, errors.New("invalid v1 aggregate")
	}
	return aggregate, nil
}

// EncodeV1Aggregate deterministically encodes one complete v1 decision.
func EncodeV1Aggregate(aggregate V1Aggregate) ([]byte, string, error) {
	if !validV1Aggregate(aggregate) {
		return nil, "", errors.New("invalid v1 aggregate")
	}
	return encodeV1(aggregate)
}

func ClassifyV1(manifest V1Manifest, evidence V1Evidence) (V1Aggregate, error) {
	if !validV1Manifest(manifest) || !validV1Evidence(evidence) {
		return V1Aggregate{}, errors.New("v1 proof is malformed")
	}
	byCandidate := make(map[string][]V1Attempt, len(manifest.Candidates))
	known := make(map[string]V1Candidate, len(manifest.Candidates))
	for _, candidate := range manifest.Candidates {
		known[candidate.ID] = candidate
	}
	for _, attempt := range evidence.Attempts {
		if _, found := known[attempt.CandidateID]; !found {
			return V1Aggregate{}, errors.New("v1 evidence contains a foreign candidate")
		}
		byCandidate[attempt.CandidateID] = append(byCandidate[attempt.CandidateID], attempt)
	}
	aggregate := V1Aggregate{SchemaVersion: V1SchemaVersion, Rows: make([]V1AggregateRow, 0, len(manifest.Candidates)), Decision: DecisionInsufficientSignal}
	moduleKills, catalogKills := 0, 0
	for _, candidate := range manifest.Candidates {
		classification, err := classifyV1Candidate(manifest, candidate, byCandidate[candidate.ID])
		if err != nil {
			return V1Aggregate{}, err
		}
		aggregate.Rows = append(aggregate.Rows, V1AggregateRow{ID: candidate.ID, Classification: classification})
		switch classification {
		case ClassificationCorrectNewOnlyKill:
			aggregate.CorrectKills++
			if candidate.Family == "module" {
				moduleKills++
			} else {
				catalogKills++
			}
		case ClassificationWrongKill:
			aggregate.WrongKills++
		case ClassificationSurvivor:
			aggregate.Survivors++
		case ClassificationInfrastructureFailure:
			aggregate.InfrastructureFailures++
		case ClassificationFlake:
			aggregate.Flakes++
		}
	}
	if aggregate.CorrectKills == 6 && moduleKills == 3 && catalogKills == 3 && aggregate.WrongKills == 0 && aggregate.Survivors == 0 && aggregate.InfrastructureFailures == 0 && aggregate.Flakes == 0 {
		aggregate.Decision = DecisionMeaningfulSignal
	}
	return aggregate, nil
}

func classifyV1Candidate(manifest V1Manifest, candidate V1Candidate, attempts []V1Attempt) (string, error) {
	comparators := map[string]V1Attempt{}
	detectors := map[int]V1Attempt{}
	for _, attempt := range attempts {
		if !matchesV1Candidate(attempt, candidate) {
			return "", errors.New("v1 evidence candidate identity differs")
		}
		switch attempt.Kind {
		case V1KindComparator:
			if _, duplicate := comparators[attempt.Lane]; duplicate {
				return "", errors.New("v1 evidence duplicates a comparator lane")
			}
			comparators[attempt.Lane] = attempt
		case V1KindDetector:
			if _, duplicate := detectors[attempt.Repetition]; duplicate {
				return "", errors.New("v1 evidence duplicates a detector attempt")
			}
			detectors[attempt.Repetition] = attempt
		default:
			return "", errors.New("v1 evidence has an unknown attempt kind")
		}
	}
	if len(comparators) != 3 || len(detectors) != 2 {
		return "", errors.New("v1 evidence inventory is incomplete")
	}
	for _, lane := range []string{V1LaneWindowsGo, V1LaneUbuntuGo, V1LaneMacOSGo} {
		attempt, found := comparators[lane]
		if !found || attempt.Repetition != 1 || attempt.Toolchain != manifest.Toolchain || attempt.ComparatorContractSHA256 != manifest.ComparatorContractSHA256 || attempt.DetectorContractSHA256 != "" || attempt.Admission != "" || attempt.Status == V1StatusInfrastructure {
			return "", errors.New("v1 comparator evidence is foreign or malformed")
		}
		if !sameV1Authorities(attempt.Authorities, manifest.Authorities) {
			return ClassificationFlake, nil
		}
		if attempt.Status != V1StatusPassed {
			return ClassificationWrongKill, nil
		}
	}
	first, firstFound := detectors[1]
	second, secondFound := detectors[2]
	if !firstFound || !secondFound || first.Lane != "" || second.Lane != "" || first.Toolchain != manifest.Toolchain || second.Toolchain != manifest.Toolchain || first.DetectorContractSHA256 != manifest.DetectorContractSHA256 || second.DetectorContractSHA256 != manifest.DetectorContractSHA256 || first.ComparatorContractSHA256 != "" || second.ComparatorContractSHA256 != "" {
		return "", errors.New("v1 detector evidence is foreign or malformed")
	}
	if first.Status == V1StatusInfrastructure || second.Status == V1StatusInfrastructure {
		return ClassificationInfrastructureFailure, nil
	}
	if !sameV1Authorities(first.Authorities, manifest.Authorities) || !sameV1Authorities(second.Authorities, manifest.Authorities) || !sameV1AttemptIdentity(first, second) {
		return ClassificationFlake, nil
	}
	if first.Status != second.Status || !sameV1Failure(first.Failure, second.Failure) {
		return ClassificationFlake, nil
	}
	if first.Admission != V1AdmissionAdmitted || second.Admission != V1AdmissionAdmitted || shallowV1Failure(first.Failure) {
		return ClassificationWrongKill, nil
	}
	if first.Status == V1StatusPassed {
		return ClassificationSurvivor, nil
	}
	if first.Status != V1StatusRejected || first.Failure == nil || *first.Failure != candidate.Expected {
		return ClassificationWrongKill, nil
	}
	return ClassificationCorrectNewOnlyKill, nil
}

func matchesV1Candidate(attempt V1Attempt, candidate V1Candidate) bool {
	return attempt.PatchSHA256 == candidate.PatchSHA256 && attempt.MutatedTree == candidate.MutatedTree
}
func sameV1AttemptIdentity(first, second V1Attempt) bool {
	return first.CandidateID == second.CandidateID && first.Kind == second.Kind && first.Authorities == second.Authorities && first.PatchSHA256 == second.PatchSHA256 && first.MutatedTree == second.MutatedTree && first.Toolchain == second.Toolchain && first.ComparatorContractSHA256 == second.ComparatorContractSHA256 && first.DetectorContractSHA256 == second.DetectorContractSHA256 && first.Admission == second.Admission
}
func sameV1Authorities(first, second V1Authorities) bool { return first == second }
func sameV1Failure(first, second *V1Failure) bool {
	return (first == nil && second == nil) || (first != nil && second != nil && *first == *second)
}
func shallowV1Failure(failure *V1Failure) bool {
	return failure == nil || failure.Class == "envelope_contract" || failure.Class == "scenario_selection" || failure.Class == "assertion_contract" && failure.Phase == "aggregate"
}

func validV1Manifest(manifest V1Manifest) bool {
	if manifest.SchemaVersion != V1SchemaVersion || !validV1Authorities(manifest.Authorities) || !v1ToolchainPattern.MatchString(manifest.Toolchain) || !sha256Pattern.MatchString(manifest.ComparatorContractSHA256) || !sha256Pattern.MatchString(manifest.DetectorContractSHA256) || len(manifest.Candidates) != 6 {
		return false
	}
	operators, invariants, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}
	calibrationOperators, calibrationInvariants := map[string]bool{}, map[string]bool{}
	for _, fingerprint := range manifest.Calibration {
		if !validV1Fingerprint(fingerprint) || calibrationOperators[fingerprint.OperatorFingerprint] || calibrationInvariants[fingerprint.InvariantFingerprint] {
			return false
		}
		calibrationOperators[fingerprint.OperatorFingerprint] = true
		calibrationInvariants[fingerprint.InvariantFingerprint] = true
	}
	modules, catalogs := 0, 0
	for _, candidate := range manifest.Candidates {
		if !validV1Candidate(candidate) || ids[candidate.ID] || operators[candidate.OperatorFingerprint] || invariants[candidate.InvariantFingerprint] || calibrationOperators[candidate.OperatorFingerprint] || calibrationInvariants[candidate.InvariantFingerprint] {
			return false
		}
		ids[candidate.ID], operators[candidate.OperatorFingerprint], invariants[candidate.InvariantFingerprint] = true, true, true
		if candidate.Family == "module" {
			modules++
		} else {
			catalogs++
		}
	}
	return modules == 3 && catalogs == 3
}

func validV1Evidence(evidence V1Evidence) bool {
	if evidence.SchemaVersion != V1SchemaVersion || len(evidence.Attempts) == 0 || len(evidence.Attempts) > 30 {
		return false
	}
	for _, attempt := range evidence.Attempts {
		if !validV1Attempt(attempt) {
			return false
		}
	}
	return true
}

func validV1Aggregate(aggregate V1Aggregate) bool {
	if aggregate.SchemaVersion != V1SchemaVersion || len(aggregate.Rows) != 6 || (aggregate.Decision != DecisionMeaningfulSignal && aggregate.Decision != DecisionInsufficientSignal && aggregate.Decision != DecisionInconclusive) {
		return false
	}
	seen := map[string]bool{}
	correct, wrong, survivors, infrastructure, flakes := 0, 0, 0, 0, 0
	for _, row := range aggregate.Rows {
		if !validV1Value(row.ID) || seen[row.ID] {
			return false
		}
		seen[row.ID] = true
		switch row.Classification {
		case ClassificationCorrectNewOnlyKill:
			correct++
		case ClassificationWrongKill:
			wrong++
		case ClassificationSurvivor:
			survivors++
		case ClassificationInfrastructureFailure:
			infrastructure++
		case ClassificationFlake:
			flakes++
		default:
			return false
		}
	}
	if aggregate.CorrectKills != correct || aggregate.WrongKills != wrong || aggregate.Survivors != survivors || aggregate.InfrastructureFailures != infrastructure || aggregate.Flakes != flakes {
		return false
	}
	return aggregate.Decision != DecisionMeaningfulSignal || correct == 6 && wrong == 0 && survivors == 0 && infrastructure == 0 && flakes == 0
}

func validV1Authorities(authorities V1Authorities) bool {
	return validV1Reference(authorities.Evaluated) && validV1Reference(authorities.Freeze) && validV1Reference(authorities.Corpus) && validV1Reference(authorities.Dispatch) && authorities.Evaluated.Commit != authorities.Freeze.Commit && authorities.Evaluated.Commit != authorities.Corpus.Commit && authorities.Evaluated.Commit != authorities.Dispatch.Commit && authorities.Freeze.Commit != authorities.Corpus.Commit && authorities.Freeze.Commit != authorities.Dispatch.Commit && authorities.Corpus.Commit != authorities.Dispatch.Commit
}
func validV1Reference(reference V1Reference) bool {
	return v1SHA1Pattern.MatchString(reference.Commit) && v1SHA1Pattern.MatchString(reference.Tree)
}
func validV1Fingerprint(fingerprint V1Fingerprint) bool {
	return validV1Value(fingerprint.OperatorFingerprint) && validV1Value(fingerprint.InvariantFingerprint)
}
func validV1Candidate(candidate V1Candidate) bool {
	return validV1Value(candidate.ID) && (candidate.Family == "catalog" || candidate.Family == "module") && sha256Pattern.MatchString(candidate.PatchSHA256) && v1SHA1Pattern.MatchString(candidate.MutatedTree) && validV1Fingerprint(V1Fingerprint{candidate.OperatorFingerprint, candidate.InvariantFingerprint}) && validV1Failure(candidate.Expected)
}
func validV1Attempt(attempt V1Attempt) bool {
	if !validV1Value(attempt.CandidateID) || !validV1Authorities(attempt.Authorities) || !sha256Pattern.MatchString(attempt.PatchSHA256) || !v1SHA1Pattern.MatchString(attempt.MutatedTree) || !v1ToolchainPattern.MatchString(attempt.Toolchain) || !validV1Status(attempt.Status) || attempt.DiagnosticEngineSHA256 != "" && !sha256Pattern.MatchString(attempt.DiagnosticEngineSHA256) || attempt.Failure != nil && !validV1Failure(*attempt.Failure) {
		return false
	}
	switch attempt.Kind {
	case V1KindComparator:
		return (attempt.Lane == V1LaneWindowsGo || attempt.Lane == V1LaneUbuntuGo || attempt.Lane == V1LaneMacOSGo) && attempt.Repetition == 1 && sha256Pattern.MatchString(attempt.ComparatorContractSHA256) && attempt.DetectorContractSHA256 == "" && attempt.Admission == "" && (attempt.Status == V1StatusPassed || attempt.Status == V1StatusRejected) && (attempt.Status == V1StatusRejected) == (attempt.Failure != nil)
	case V1KindDetector:
		return attempt.Lane == "" && (attempt.Repetition == 1 || attempt.Repetition == 2) && sha256Pattern.MatchString(attempt.DetectorContractSHA256) && attempt.ComparatorContractSHA256 == "" && (attempt.Admission == V1AdmissionAdmitted || attempt.Admission == V1AdmissionRejected) && ((attempt.Status == V1StatusRejected) == (attempt.Failure != nil))
	default:
		return false
	}
}
func validV1Status(status string) bool {
	return status == V1StatusPassed || status == V1StatusRejected || status == V1StatusInfrastructure
}
func validV1Failure(failure V1Failure) bool {
	return validV1Value(failure.Class) && validV1Value(failure.Phase) && validV1Value(failure.Coordinate) && (failure.ChildReason == "" || validV1Value(failure.ChildReason))
}
func validV1Value(value string) bool {
	return v1IdentifierPattern.MatchString(value) && !strings.ContainsAny(value, `/\\`)
}

func decodeV1(raw []byte, destination any) error {
	if len(raw) == 0 || len(raw) > V1MaxDocumentSize {
		return errors.New("v1 document size is invalid")
	}
	if err := rejectV1DuplicateJSONKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("v1 document has trailing JSON")
	}
	return nil
}

func encodeV1(value any) ([]byte, string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw)+1 > V1MaxDocumentSize {
		return nil, "", errors.New("v1 document encoding is invalid")
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func rejectV1DuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var consume func() error
	consume = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || keys[key] {
					return errors.New("v1 document has duplicate key")
				}
				keys[key] = true
				if err := consume(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return errors.New("v1 document is malformed")
			}
		case '[':
			for decoder.More() {
				if err := consume(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return errors.New("v1 document is malformed")
			}
		}
		return nil
	}
	if err := consume(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("v1 document has trailing JSON")
	}
	return nil
}
