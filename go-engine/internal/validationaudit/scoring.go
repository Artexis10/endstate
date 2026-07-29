// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
)

const (
	QualificationControlSurvivor       = "control-survivor"
	QualificationAlreadyCovered        = "already-covered"
	QualificationInvalidBeforeFreeze   = "invalid-before-freeze"
	QualificationInfrastructureFailure = "infrastructure-failure"

	ClassificationCorrectNewOnlyKill = "correct-new-only-kill"
	ClassificationWrongKill          = "wrong-kill"
	ClassificationSurvivor           = "survivor"
	ClassificationInfrastructure     = "infrastructure-failure"
	ClassificationFlake              = "flake"

	DecisionProceed         = "proceed"
	DecisionStopAndRepair   = "stop-and-repair"
	DecisionRejectDirection = "reject-direction"
)

var (
	ErrInvalidQualification = errors.New("validation audit invalid qualification")
	ErrWholeCorpusInvalid   = errors.New("validation audit whole corpus invalid")
	ErrInvalidAggregate     = errors.New("validation audit invalid aggregate")
)

// AuditIdentity fixes the audit and corpus versions that evidence must bind.
type AuditIdentity struct {
	AuditVersion  string
	AuditSHA256   string
	CorpusVersion string
	CorpusSHA256  string
}

// QualificationResult is one legacy-only inventory result in candidate order.
type QualificationResult struct {
	CandidateID   string                `json:"candidateId"`
	Category      string                `json:"category"`
	Reference     ReferenceIdentity     `json:"reference"`
	PatchSHA256   string                `json:"patchSha256"`
	State         string                `json:"state"`
	Qualification QualificationIdentity `json:"qualification"`
}

// DetectorCommand fixes the reviewed command identity for one detector.
type DetectorCommand struct {
	ID            string
	CommandSHA256 string
}

// Classification is the closed scored result for one frozen item.
type Classification struct {
	CandidateID string `json:"candidateId"`
	Category    string `json:"category"`
	State       string `json:"state"`
}

// Aggregate contains a denominator-preserving mechanical audit decision.
type Aggregate struct {
	SchemaVersion   int              `json:"schemaVersion"`
	Decision        string           `json:"decision"`
	CorrectKills    int              `json:"correctKills"`
	WrongKills      int              `json:"wrongKills"`
	Survivors       int              `json:"survivors"`
	Infrastructure  int              `json:"infrastructureFailures"`
	Flakes          int              `json:"flakes"`
	Classifications []Classification `json:"classifications"`
}

// QualifyControls inventories only the declared legacy lanes. Detector evidence
// is invalid input, not evidence that can be ignored.
func QualifyControls(set CandidateSet, identity AuditIdentity, runID string, evidence []AttemptEvidence) ([]QualificationResult, error) {
	if !validCandidateSetForScoring(set) || !validAuditIdentity(identity) || !validAuditIdentifier(runID) {
		return nil, ErrInvalidQualification
	}
	candidates := orderedCandidates(set)
	byCandidate := make(map[string]map[string]AttemptEvidence, len(candidates))
	known := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		known[candidate.ID] = candidate
		byCandidate[candidate.ID] = make(map[string]AttemptEvidence, len(*set.LegacyLanes))
	}
	for _, record := range evidence {
		if record.Kind != EvidenceKindControl || !validAttemptEvidence(record) || !matchesAuditIdentity(record, identity) || record.Reference != set.Reference {
			return nil, ErrInvalidQualification
		}
		candidate, exists := known[record.CandidateID]
		if !exists || record.PatchSHA256 != candidate.PatchSHA256 {
			return nil, ErrInvalidQualification
		}
		lane, exists := laneForID(*set.LegacyLanes, record.Lane)
		if !exists || record.CommandSHA256 != lane.CommandSHA256 {
			return nil, ErrInvalidQualification
		}
		if _, exists := byCandidate[candidate.ID][lane.ID]; exists {
			return nil, ErrInvalidQualification
		}
		byCandidate[candidate.ID][lane.ID] = record
	}
	results := make([]QualificationResult, 0, len(candidates))
	for _, candidate := range candidates {
		inventory := byCandidate[candidate.ID]
		state := QualificationControlSurvivor
		if len(inventory) != len(*set.LegacyLanes) {
			state = QualificationInfrastructureFailure
		} else {
			for _, lane := range *set.LegacyLanes {
				record := inventory[lane.ID]
				if record.ExitClass == ExitClassInfrastructure || record.ExitClass == ExitClassCanceled || record.ExitClass == ExitClassTimeout {
					state = QualificationInfrastructureFailure
					break
				}
				if record.ExitClass == ExitClassRejected {
					state = QualificationAlreadyCovered
				}
			}
		}
		digest, ok := controlEvidenceDigest(*set.LegacyLanes, inventory)
		if !ok {
			return nil, ErrInvalidQualification
		}
		results = append(results, QualificationResult{CandidateID: candidate.ID, Category: categoryForCandidate(set, candidate.ID), Reference: candidate.Reference, PatchSHA256: candidate.PatchSHA256, State: state, Qualification: QualificationIdentity{RunID: runID, EvidenceSHA256: digest}})
	}
	return results, nil
}

// FreezeCandidates picks the first legacy survivors in each reviewed queue.
func FreezeCandidates(set CandidateSet, results []QualificationResult) (FrozenManifest, error) {
	if !validCandidateSetForScoring(set) || len(results) != len(orderedCandidates(set)) {
		return FrozenManifest{}, ErrWholeCorpusInvalid
	}
	items := make([]FrozenItem, 0, 30)
	position := 0
	for queueIndex, queue := range set.Queues {
		selected := 0
		for _, candidate := range queue.Candidates {
			result := results[position]
			position++
			if !matchesQualificationResult(candidate, queue.Category, result) {
				return FrozenManifest{}, ErrWholeCorpusInvalid
			}
			if result.State == QualificationControlSurvivor && selected < categoryQuota[queueIndex] {
				items = append(items, FrozenItem{ID: candidate.ID, Category: queue.Category, Reference: candidate.Reference, PatchSHA256: candidate.PatchSHA256, Critical: candidate.Critical, Detector: candidate.Detector, ExpectedFailure: candidate.ExpectedFailure, ViolatedBehavior: candidate.ViolatedBehavior, Qualification: result.Qualification})
				selected++
			}
		}
		if selected != categoryQuota[queueIndex] {
			return FrozenManifest{}, ErrWholeCorpusInvalid
		}
	}
	manifest := FrozenManifest{SchemaVersion: SchemaVersion, Reference: set.Reference, Detectors: append([]DetectorContract(nil), set.Detectors...), Items: items}
	if err := ValidateFrozenManifest(set, manifest); err != nil {
		return FrozenManifest{}, err
	}
	return manifest, nil
}

// EncodeFrozenManifest produces the compact, newline-terminated frozen bytes.
func EncodeFrozenManifest(manifest FrozenManifest) ([]byte, string, error) {
	if !validFrozenManifestStructure(manifest) {
		return nil, "", ErrWholeCorpusInvalid
	}
	raw, err := json.Marshal(manifest)
	if err != nil || len(raw)+1 > MaxManifestSize {
		return nil, "", ErrWholeCorpusInvalid
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	return raw, hexDigest(sum), nil
}

// ValidateFrozenManifest rejects any denominator substitution against its source set.
func ValidateFrozenManifest(set CandidateSet, manifest FrozenManifest) error {
	if !validCandidateSetForScoring(set) || !validFrozenManifestStructure(manifest) || manifest.Reference != set.Reference || !sameDetectors(manifest.Detectors, set.Detectors) {
		return ErrWholeCorpusInvalid
	}
	itemPosition := 0
	for queueIndex, queue := range set.Queues {
		lastCandidate := -1
		for selected := 0; selected < categoryQuota[queueIndex]; selected++ {
			item := manifest.Items[itemPosition]
			itemPosition++
			candidateIndex, candidate, found := candidateByID(queue.Candidates, item.ID)
			if !found || candidateIndex <= lastCandidate || !sameFrozenCandidate(item, queue.Category, candidate) {
				return ErrWholeCorpusInvalid
			}
			lastCandidate = candidateIndex
		}
	}
	return nil
}

// ScoreFrozenManifest validates baselines and mutation pairs before producing a decision.
func ScoreFrozenManifest(set CandidateSet, manifest FrozenManifest, identity AuditIdentity, commands []DetectorCommand, evidence []AttemptEvidence) (Aggregate, error) {
	if err := ValidateFrozenManifest(set, manifest); err != nil || !validAuditIdentity(identity) {
		return Aggregate{}, ErrInvalidAggregate
	}
	referenced := referencedDetectors(manifest)
	commandByDetector, ok := validDetectorCommands(referenced, commands)
	if !ok {
		return Aggregate{}, ErrInvalidAggregate
	}
	baselines := make(map[string]map[int]AttemptEvidence, len(referenced))
	mutations := make(map[string]map[int]AttemptEvidence, len(manifest.Items))
	items := make(map[string]FrozenItem, len(manifest.Items))
	for _, item := range manifest.Items {
		items[item.ID] = item
	}
	for _, record := range evidence {
		if !validAttemptEvidence(record) || !matchesAuditIdentity(record, identity) || record.Reference != manifest.Reference {
			return Aggregate{}, ErrInvalidAggregate
		}
		switch record.Kind {
		case EvidenceKindDetectorBaseline:
			contract, found := detectorByID(referenced, record.Detector.ID)
			if !found || record.Detector.ContractSHA256 != contract.ContractSHA256 || record.CommandSHA256 != commandByDetector[record.Detector.ID] || record.ExitClass != ExitClassPassed {
				return Aggregate{}, ErrInvalidAggregate
			}
			if baselines[record.Detector.ID] == nil {
				baselines[record.Detector.ID] = map[int]AttemptEvidence{}
			}
			if _, exists := baselines[record.Detector.ID][record.Repetition]; exists {
				return Aggregate{}, ErrInvalidAggregate
			}
			baselines[record.Detector.ID][record.Repetition] = record
		case EvidenceKindDetectorMutation:
			item, found := items[record.CandidateID]
			if !found || record.PatchSHA256 != item.PatchSHA256 || record.Detector.ID != item.Detector || record.Detector.ContractSHA256 != detectorContractSHA(manifest.Detectors, item.Detector) || record.CommandSHA256 != commandByDetector[item.Detector] {
				return Aggregate{}, ErrInvalidAggregate
			}
			if mutations[item.ID] == nil {
				mutations[item.ID] = map[int]AttemptEvidence{}
			}
			if _, exists := mutations[item.ID][record.Repetition]; exists {
				return Aggregate{}, ErrInvalidAggregate
			}
			mutations[item.ID][record.Repetition] = record
		default:
			return Aggregate{}, ErrInvalidAggregate
		}
	}
	for _, detector := range referenced {
		pair := baselines[detector.ID]
		if len(pair) != 2 || pair[1].AttemptID == pair[2].AttemptID || !sameProofIdentity(pair[1], pair[2]) {
			return Aggregate{}, ErrInvalidAggregate
		}
	}
	classifications := make([]Classification, 0, len(manifest.Items))
	for _, item := range manifest.Items {
		pair := mutations[item.ID]
		if len(pair) != 2 || pair[1].AttemptID == pair[2].AttemptID || !sameExecutionIdentity(pair[1], pair[2]) {
			return Aggregate{}, ErrInvalidAggregate
		}
		classifications = append(classifications, Classification{CandidateID: item.ID, Category: item.Category, State: classifyMutationPair(item.ExpectedFailure, pair[1], pair[2])})
	}
	return EvaluateClassifications(manifest, classifications)
}

// EvaluateClassifications applies the fixed threshold to a complete frozen corpus.
func EvaluateClassifications(manifest FrozenManifest, classifications []Classification) (Aggregate, error) {
	if !validFrozenManifestStructure(manifest) || len(classifications) != len(manifest.Items) {
		return Aggregate{}, ErrInvalidAggregate
	}
	aggregate := Aggregate{SchemaVersion: SchemaVersion, Classifications: append([]Classification(nil), classifications...)}
	criticalCorrect := 0
	for index, classification := range classifications {
		item := manifest.Items[index]
		if classification.CandidateID != item.ID || classification.Category != item.Category || !validClassificationState(classification.State) {
			return Aggregate{}, ErrInvalidAggregate
		}
		switch classification.State {
		case ClassificationCorrectNewOnlyKill:
			aggregate.CorrectKills++
			if item.Critical {
				criticalCorrect++
			}
		case ClassificationWrongKill:
			aggregate.WrongKills++
		case ClassificationSurvivor:
			aggregate.Survivors++
		case ClassificationInfrastructure:
			aggregate.Infrastructure++
		case ClassificationFlake:
			aggregate.Flakes++
		}
	}
	if aggregate.WrongKills != 0 || aggregate.Flakes != 0 || aggregate.Infrastructure != 0 || criticalCorrect != 6 {
		aggregate.Decision = DecisionStopAndRepair
	} else if aggregate.CorrectKills >= 27 {
		aggregate.Decision = DecisionProceed
	} else {
		aggregate.Decision = DecisionRejectDirection
	}
	return aggregate, nil
}

// EncodeAggregate produces compact, newline-terminated decision bytes.
func EncodeAggregate(aggregate Aggregate) ([]byte, string, error) {
	if !validAggregate(aggregate) {
		return nil, "", ErrInvalidAggregate
	}
	raw, err := json.Marshal(aggregate)
	if err != nil {
		return nil, "", ErrInvalidAggregate
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	return raw, hexDigest(sum), nil
}

func validAggregate(aggregate Aggregate) bool {
	if aggregate.SchemaVersion != SchemaVersion || (aggregate.Decision != DecisionProceed && aggregate.Decision != DecisionStopAndRepair && aggregate.Decision != DecisionRejectDirection) || len(aggregate.Classifications) != 30 {
		return false
	}
	correct, wrong, survivors, infrastructure, flakes := 0, 0, 0, 0, 0
	for _, classification := range aggregate.Classifications {
		switch classification.State {
		case ClassificationCorrectNewOnlyKill:
			correct++
		case ClassificationWrongKill:
			wrong++
		case ClassificationSurvivor:
			survivors++
		case ClassificationInfrastructure:
			infrastructure++
		case ClassificationFlake:
			flakes++
		default:
			return false
		}
	}
	return aggregate.CorrectKills == correct && aggregate.WrongKills == wrong && aggregate.Survivors == survivors && aggregate.Infrastructure == infrastructure && aggregate.Flakes == flakes
}

func validCandidateSetForScoring(set CandidateSet) bool {
	if set.SchemaVersion != SchemaVersion || set.LegacyLanes == nil || !validReference(set.Reference) || !validDetectors(set.Detectors) || !validLegacyLanes(*set.LegacyLanes) || len(set.Queues) != len(canonicalCategories) {
		return false
	}
	detectors := detectorIDs(set.Detectors)
	ids, paths, digests := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	total := 0
	for index, queue := range set.Queues {
		if queue.Category != canonicalCategories[index] || len(queue.Candidates) < categoryQuota[index] {
			return false
		}
		for _, candidate := range queue.Candidates {
			total++
			if !validCandidate(candidate, queue.Category, set.Reference, detectors) || seen(ids, candidate.ID) || seen(paths, candidate.PatchPath) || seen(digests, candidate.PatchSHA256) {
				return false
			}
		}
	}
	return total <= 45
}

func validFrozenManifestStructure(manifest FrozenManifest) bool {
	if manifest.SchemaVersion != SchemaVersion || !validReference(manifest.Reference) || !validDetectors(manifest.Detectors) || len(manifest.Items) != 30 {
		return false
	}
	detectors := detectorIDs(manifest.Detectors)
	ids, digests := map[string]struct{}{}, map[string]struct{}{}
	position := 0
	for categoryIndex, category := range canonicalCategories {
		for count := 0; count < categoryQuota[categoryIndex]; count++ {
			item := manifest.Items[position]
			position++
			if !validFrozenItem(item, category, manifest.Reference, detectors) || seen(ids, item.ID) || seen(digests, item.PatchSHA256) {
				return false
			}
		}
	}
	return true
}

func validAuditIdentity(identity AuditIdentity) bool {
	return validAuditIdentifier(identity.AuditVersion) && sha256Pattern.MatchString(identity.AuditSHA256) && validAuditIdentifier(identity.CorpusVersion) && sha256Pattern.MatchString(identity.CorpusSHA256)
}

func matchesAuditIdentity(record AttemptEvidence, identity AuditIdentity) bool {
	return record.AuditVersion == identity.AuditVersion && record.AuditSHA256 == identity.AuditSHA256 && record.CorpusVersion == identity.CorpusVersion && record.CorpusSHA256 == identity.CorpusSHA256
}

func controlEvidenceDigest(lanes []LaneContract, inventory map[string]AttemptEvidence) (string, bool) {
	bytes := make([]byte, 0, len(lanes)*512)
	for _, lane := range lanes {
		record, exists := inventory[lane.ID]
		if !exists {
			continue
		}
		raw, _, err := EncodeAttemptEvidence(record)
		if err != nil {
			return "", false
		}
		bytes = append(bytes, raw...)
	}
	sum := sha256.Sum256(bytes)
	return hexDigest(sum), true
}

func laneForID(lanes []LaneContract, id string) (LaneContract, bool) {
	for _, lane := range lanes {
		if lane.ID == id {
			return lane, true
		}
	}
	return LaneContract{}, false
}

func orderedCandidates(set CandidateSet) []Candidate {
	result := make([]Candidate, 0)
	for _, queue := range set.Queues {
		result = append(result, queue.Candidates...)
	}
	return result
}

func categoryForCandidate(set CandidateSet, id string) string {
	for _, queue := range set.Queues {
		for _, candidate := range queue.Candidates {
			if candidate.ID == id {
				return queue.Category
			}
		}
	}
	return ""
}

func matchesQualificationResult(candidate Candidate, category string, result QualificationResult) bool {
	return result.CandidateID == candidate.ID && result.Category == category && result.Reference == candidate.Reference && result.PatchSHA256 == candidate.PatchSHA256 && validQualificationState(result.State) && validAuditIdentifier(result.Qualification.RunID) && sha256Pattern.MatchString(result.Qualification.EvidenceSHA256)
}

func validQualificationState(state string) bool {
	return state == QualificationControlSurvivor || state == QualificationAlreadyCovered || state == QualificationInvalidBeforeFreeze || state == QualificationInfrastructureFailure
}

func sameDetectors(first, second []DetectorContract) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func candidateByID(candidates []Candidate, id string) (int, Candidate, bool) {
	for index, candidate := range candidates {
		if candidate.ID == id {
			return index, candidate, true
		}
	}
	return 0, Candidate{}, false
}

func sameFrozenCandidate(item FrozenItem, category string, candidate Candidate) bool {
	return item.Category == category && item.Reference == candidate.Reference && item.PatchSHA256 == candidate.PatchSHA256 && item.Critical == candidate.Critical && item.Detector == candidate.Detector && item.ExpectedFailure == candidate.ExpectedFailure && item.ViolatedBehavior == candidate.ViolatedBehavior
}

func validDetectorCommands(detectors []DetectorContract, commands []DetectorCommand) (map[string]string, bool) {
	if len(detectors) != len(commands) {
		return nil, false
	}
	result := make(map[string]string, len(commands))
	for _, command := range commands {
		if !validAuditIdentifier(command.ID) || !sha256Pattern.MatchString(command.CommandSHA256) {
			return nil, false
		}
		if _, exists := result[command.ID]; exists {
			return nil, false
		}
		if _, found := detectorByID(detectors, command.ID); !found {
			return nil, false
		}
		result[command.ID] = command.CommandSHA256
	}
	return result, true
}

func detectorByID(detectors []DetectorContract, id string) (DetectorContract, bool) {
	for _, detector := range detectors {
		if detector.ID == id {
			return detector, true
		}
	}
	return DetectorContract{}, false
}

func detectorContractSHA(detectors []DetectorContract, id string) string {
	detector, found := detectorByID(detectors, id)
	if !found {
		return ""
	}
	return detector.ContractSHA256
}

func referencedDetectors(manifest FrozenManifest) []DetectorContract {
	result := make([]DetectorContract, 0, len(manifest.Detectors))
	for _, detector := range manifest.Detectors {
		for _, item := range manifest.Items {
			if item.Detector == detector.ID {
				result = append(result, detector)
				break
			}
		}
	}
	return result
}

func sameProofIdentity(first, second AttemptEvidence) bool {
	return sameExecutionIdentity(first, second) && first.ExitClass == second.ExitClass && sameFailure(first.Failure, second.Failure)
}

func sameExecutionIdentity(first, second AttemptEvidence) bool {
	return first.SchemaVersion == second.SchemaVersion && first.AuditVersion == second.AuditVersion && first.AuditSHA256 == second.AuditSHA256 && first.CorpusVersion == second.CorpusVersion && first.CorpusSHA256 == second.CorpusSHA256 && first.Reference == second.Reference && first.Kind == second.Kind && first.CandidateID == second.CandidateID && first.PatchSHA256 == second.PatchSHA256 && first.MutatedTree == second.MutatedTree && first.Lane == second.Lane && first.Detector == second.Detector && first.Runner == second.Runner && first.Toolchain == second.Toolchain && first.CommandSHA256 == second.CommandSHA256
}

func classifyMutationPair(expected ExpectedFailure, first, second AttemptEvidence) string {
	if first.ExitClass == ExitClassInfrastructure || second.ExitClass == ExitClassInfrastructure || first.ExitClass == ExitClassCanceled || second.ExitClass == ExitClassCanceled {
		return ClassificationInfrastructure
	}
	expectedTimeout := expected.Class == ExitClassTimeout
	if (first.ExitClass == ExitClassTimeout || second.ExitClass == ExitClassTimeout) && !expectedTimeout {
		return ClassificationInfrastructure
	}
	if first.ExitClass != second.ExitClass || !sameFailure(first.Failure, second.Failure) {
		return ClassificationFlake
	}
	if first.ExitClass == ExitClassPassed {
		return ClassificationSurvivor
	}
	if (first.ExitClass == ExitClassRejected || first.ExitClass == ExitClassTimeout) && first.Failure != nil && first.Failure.Class == expected.Class && first.Failure.Phase == expected.Phase && first.Failure.Coordinate == expected.Coordinate {
		return ClassificationCorrectNewOnlyKill
	}
	return ClassificationWrongKill
}

func sameFailure(first, second *StableFailure) bool {
	if first == nil || second == nil {
		return first == second
	}
	return *first == *second
}

func validClassificationState(state string) bool {
	return state == ClassificationCorrectNewOnlyKill || state == ClassificationWrongKill || state == ClassificationSurvivor || state == ClassificationInfrastructure || state == ClassificationFlake
}
