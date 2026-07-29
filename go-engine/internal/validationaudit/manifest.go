// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package validationaudit owns strict, bounded audit manifest contracts.
package validationaudit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const (
	// SchemaVersion is the only supported audit manifest schema version.
	SchemaVersion = 1
	// MaxManifestSize bounds candidate and frozen manifest input bytes.
	MaxManifestSize = 256 * 1024
)

var (
	ErrInputTooLarge     = errors.New("validation audit input too large")
	ErrDuplicateJSONKey  = errors.New("validation audit duplicate JSON key")
	ErrUnknownField      = errors.New("validation audit unknown JSON field")
	ErrTrailingJSONValue = errors.New("validation audit trailing JSON value")
	ErrUnsupportedSchema = errors.New("validation audit unsupported schema")
	ErrInvalidManifest   = errors.New("validation audit invalid manifest")
	sha1Pattern          = regexp.MustCompile(`^[a-f0-9]{40}$`)
	sha256Pattern        = regexp.MustCompile(`^[a-f0-9]{64}$`)
	identifierPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	pathSegmentPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	stableValuePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]{0,127}$`)
	canonicalCategories  = []string{"module-data", "engine-lifecycle", "artifact-config", "critical-safety"}
	categoryQuota        = []int{10, 8, 6, 6}
	legacyLaneOrder      = []legacyLaneIdentity{
		{ID: "windows-go", Runner: "windows-latest"},
		{ID: "ubuntu-go", Runner: "ubuntu-latest"},
		{ID: "macos-go", Runner: "macos-latest"},
		{ID: "windows-integration", Runner: "windows-latest"},
		{ID: "ubuntu-nix", Runner: "ubuntu-latest"},
		{ID: "macos-nix", Runner: "macos-latest"},
	}
	windowsDeviceNames = map[string]struct{}{
		"con": {}, "prn": {}, "aux": {}, "nul": {}, "clock$": {},
		"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
		"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
	}
)

type legacyLaneIdentity struct {
	ID     string
	Runner string
}

// ReferenceIdentity binds a manifest to one exact Git commit and tree.
type ReferenceIdentity struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

// DetectorContract identifies an independently declared detector contract.
type DetectorContract struct {
	ID             string `json:"id"`
	ContractSHA256 string `json:"contractSha256"`
}

// LaneContract binds one legacy control lane to its fixed runner and command.
type LaneContract struct {
	ID             string `json:"id"`
	Runner         string `json:"runner"`
	CommandSHA256  string `json:"commandSha256"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// ExpectedFailure fixes the stable failure that a candidate must produce.
type ExpectedFailure struct {
	Class      string `json:"class"`
	Phase      string `json:"phase"`
	Coordinate string `json:"coordinate"`
}

// Candidate is one reviewed, ordered, pre-qualification mutation candidate.
type Candidate struct {
	ID               string            `json:"id"`
	Reference        ReferenceIdentity `json:"reference"`
	PatchPath        string            `json:"patchPath"`
	PatchSHA256      string            `json:"patchSha256"`
	TouchedPaths     []string          `json:"touchedPaths"`
	Rationale        string            `json:"rationale"`
	ViolatedBehavior string            `json:"violatedBehavior"`
	Critical         bool              `json:"critical"`
	Detector         string            `json:"detector"`
	ExpectedFailure  ExpectedFailure   `json:"expectedFailure"`
}

// CandidateQueue preserves the reviewed ordering for one audit category.
type CandidateQueue struct {
	Category   string      `json:"category"`
	Candidates []Candidate `json:"candidates"`
}

// CandidateSet is the versioned input to legacy-only qualification.
type CandidateSet struct {
	SchemaVersion int                `json:"schemaVersion"`
	Reference     ReferenceIdentity  `json:"reference"`
	Detectors     []DetectorContract `json:"detectors"`
	LegacyLanes   *[]LaneContract    `json:"legacyLanes"`
	Queues        []CandidateQueue   `json:"queues"`
}

// QualificationIdentity binds a frozen item to the evidence produced before freeze.
type QualificationIdentity struct {
	RunID          string `json:"runId"`
	EvidenceSHA256 string `json:"evidenceSha256"`
}

// FrozenItem is one denominator member selected before detector execution.
type FrozenItem struct {
	ID               string                `json:"id"`
	Category         string                `json:"category"`
	Reference        ReferenceIdentity     `json:"reference"`
	PatchSHA256      string                `json:"patchSha256"`
	Critical         bool                  `json:"critical"`
	Detector         string                `json:"detector"`
	ExpectedFailure  ExpectedFailure       `json:"expectedFailure"`
	ViolatedBehavior string                `json:"violatedBehavior"`
	Qualification    QualificationIdentity `json:"qualification"`
}

// FrozenManifest is the exact, quota-filled manifest used by detector execution.
type FrozenManifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	Reference     ReferenceIdentity  `json:"reference"`
	Detectors     []DetectorContract `json:"detectors"`
	Items         []FrozenItem       `json:"items"`
}

// DecodeCandidateSet strictly decodes one bounded candidate-set JSON document.
func DecodeCandidateSet(raw []byte) (CandidateSet, error) {
	var set CandidateSet
	if err := decodeStrict(raw, &set); err != nil {
		return CandidateSet{}, err
	}
	if set.SchemaVersion != SchemaVersion {
		return CandidateSet{}, ErrUnsupportedSchema
	}
	if !validReference(set.Reference) || !validDetectors(set.Detectors) || set.LegacyLanes != nil && !validLegacyLanes(*set.LegacyLanes) || len(set.Queues) != len(canonicalCategories) {
		return CandidateSet{}, ErrInvalidManifest
	}
	detectors := detectorIDs(set.Detectors)
	ids, patchPaths, patchDigests := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	total := 0
	for index, queue := range set.Queues {
		if queue.Category != canonicalCategories[index] || len(queue.Candidates) < categoryQuota[index] {
			return CandidateSet{}, ErrInvalidManifest
		}
		for _, candidate := range queue.Candidates {
			total++
			if !validCandidate(candidate, queue.Category, set.Reference, detectors) || seen(ids, candidate.ID) || seen(patchPaths, candidate.PatchPath) || seen(patchDigests, candidate.PatchSHA256) {
				return CandidateSet{}, ErrInvalidManifest
			}
		}
	}
	if total > 45 {
		return CandidateSet{}, ErrInvalidManifest
	}
	return set, nil
}

// DecodeFrozenManifest strictly decodes one bounded frozen-manifest JSON document.
func DecodeFrozenManifest(raw []byte) (FrozenManifest, error) {
	var manifest FrozenManifest
	if err := decodeStrict(raw, &manifest); err != nil {
		return FrozenManifest{}, err
	}
	if manifest.SchemaVersion != SchemaVersion {
		return FrozenManifest{}, ErrUnsupportedSchema
	}
	if !validReference(manifest.Reference) || !validDetectors(manifest.Detectors) || len(manifest.Items) != 30 {
		return FrozenManifest{}, ErrInvalidManifest
	}
	detectors := detectorIDs(manifest.Detectors)
	ids, patchDigests := map[string]struct{}{}, map[string]struct{}{}
	position := 0
	for categoryIndex, category := range canonicalCategories {
		for count := 0; count < categoryQuota[categoryIndex]; count++ {
			item := manifest.Items[position]
			position++
			if !validFrozenItem(item, category, manifest.Reference, detectors) || seen(ids, item.ID) || seen(patchDigests, item.PatchSHA256) {
				return FrozenManifest{}, ErrInvalidManifest
			}
		}
	}
	return manifest, nil
}

func decodeStrict(raw []byte, destination any) error {
	if len(raw) > MaxManifestSize {
		return ErrInputTooLarge
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		if errors.Is(err, ErrDuplicateJSONKey) {
			return ErrDuplicateJSONKey
		}
		if errors.Is(err, ErrTrailingJSONValue) {
			return ErrTrailingJSONValue
		}
		return ErrInvalidManifest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return ErrUnknownField
		}
		return ErrInvalidManifest
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrTrailingJSONValue
	}
	return nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
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
			keys := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return ErrInvalidManifest
				}
				if _, exists := keys[key]; exists {
					return ErrDuplicateJSONKey
				}
				keys[key] = struct{}{}
				if err := consume(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return ErrInvalidManifest
			}
		case '[':
			for decoder.More() {
				if err := consume(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return ErrInvalidManifest
			}
		default:
			return ErrInvalidManifest
		}
		return nil
	}
	if err := consume(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrTrailingJSONValue
	}
	return nil
}

func validReference(reference ReferenceIdentity) bool {
	return sha1Pattern.MatchString(reference.Commit) && sha1Pattern.MatchString(reference.Tree)
}

func validDetectors(detectors []DetectorContract) bool {
	if len(detectors) == 0 {
		return false
	}
	seenIDs := map[string]struct{}{}
	for _, detector := range detectors {
		if !identifierPattern.MatchString(detector.ID) || !sha256Pattern.MatchString(detector.ContractSHA256) || seen(seenIDs, detector.ID) {
			return false
		}
	}
	return true
}

func validLegacyLanes(lanes []LaneContract) bool {
	if len(lanes) != len(legacyLaneOrder) {
		return false
	}
	for index, lane := range lanes {
		expected := legacyLaneOrder[index]
		if lane.ID != expected.ID || lane.Runner != expected.Runner || !sha256Pattern.MatchString(lane.CommandSHA256) || lane.TimeoutSeconds < 1 || lane.TimeoutSeconds > 14_400 {
			return false
		}
	}
	return true
}

func detectorIDs(detectors []DetectorContract) map[string]struct{} {
	result := make(map[string]struct{}, len(detectors))
	for _, detector := range detectors {
		result[detector.ID] = struct{}{}
	}
	return result
}

func validCandidate(candidate Candidate, category string, reference ReferenceIdentity, detectors map[string]struct{}) bool {
	_, knownDetector := detectors[candidate.Detector]
	return identifierPattern.MatchString(candidate.ID) && candidate.Reference == reference && validRepositoryPath(candidate.PatchPath) && sha256Pattern.MatchString(candidate.PatchSHA256) && validPaths(candidate.TouchedPaths) && validText(candidate.Rationale) && validText(candidate.ViolatedBehavior) && candidate.Critical == (category == "critical-safety") && knownDetector && validExpectedFailure(candidate.ExpectedFailure)
}

func validFrozenItem(item FrozenItem, category string, reference ReferenceIdentity, detectors map[string]struct{}) bool {
	_, knownDetector := detectors[item.Detector]
	return item.Category == category && identifierPattern.MatchString(item.ID) && item.Reference == reference && sha256Pattern.MatchString(item.PatchSHA256) && item.Critical == (category == "critical-safety") && knownDetector && validExpectedFailure(item.ExpectedFailure) && validText(item.ViolatedBehavior) && identifierPattern.MatchString(item.Qualification.RunID) && sha256Pattern.MatchString(item.Qualification.EvidenceSHA256)
}

func validPaths(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	seenPaths := map[string]struct{}{}
	for _, path := range paths {
		if !validRepositoryPath(path) || seen(seenPaths, path) {
			return false
		}
	}
	return true
}

func validRepositoryPath(path string) bool {
	if path == "" || len(path) > 256 || strings.Contains(path, `\`) || strings.HasPrefix(path, "/") || strings.Contains(path, "//") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if !pathSegmentPattern.MatchString(segment) || segment == "." || segment == ".." || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || windowsReservedDeviceName(segment) {
			return false
		}
	}
	return true
}

func windowsReservedDeviceName(segment string) bool {
	base := strings.ToLower(strings.SplitN(segment, ".", 2)[0])
	_, reserved := windowsDeviceNames[base]
	return reserved
}

func validText(value string) bool {
	return len(value) > 0 && len(value) <= 512 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\t")
}

func validExpectedFailure(expected ExpectedFailure) bool {
	return stableValuePattern.MatchString(expected.Class) && stableValuePattern.MatchString(expected.Phase) && stableValuePattern.MatchString(expected.Coordinate)
}

func seen(values map[string]struct{}, value string) bool {
	if _, exists := values[value]; exists {
		return true
	}
	values[value] = struct{}{}
	return false
}
