// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

const maxKnownFailureLedgerSize = 512 * 1024

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var stableFailurePartPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var knownFailureIdentityPartPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// KnownFailureLedger is the protected-main authority for reviewed synthetic
// validation debt. It intentionally has no dependency on validationharness.
type KnownFailureLedger struct {
	SchemaVersion      int                   `json:"schemaVersion"`
	Inventory          KnownFailureInventory `json:"inventory"`
	KnownFailures      []KnownFailure        `json:"knownFailures"`
	FailureTransitions []FailureTransition   `json:"failureTransitions"`
	InventoryRemovals  []InventoryRemoval    `json:"inventoryRemovals"`
}

type KnownFailureInventory struct {
	ModuleCount   int                    `json:"moduleCount"`
	ScenarioCount int                    `json:"scenarioCount"`
	SHA256        string                 `json:"sha256"`
	Rows          []KnownFailureIdentity `json:"rows"`
}

// KnownFailureIdentity deliberately excludes mutable row evidence.
type KnownFailureIdentity struct {
	ModuleID   string `json:"moduleId"`
	ScenarioID string `json:"scenarioId"`
	Kind       string `json:"kind"`
}

type KnownFailure struct {
	Identity         KnownFailureIdentity `json:"identity"`
	PreviousEvidence PreviousEvidence     `json:"previousEvidence"`
	Failure          FailureFingerprint   `json:"failure"`
	Detail           string               `json:"detail"`
}

type PreviousEvidence struct {
	Commit         string      `json:"commit"`
	EngineSHA256   string      `json:"engineSha256"`
	RepositoryHash string      `json:"repositoryHash"`
	Row            LedgerRowID `json:"row"`
}

type LedgerRowID struct {
	ModuleID       string `json:"moduleId"`
	ModuleRevision string `json:"moduleRevision"`
	ScenarioID     string `json:"scenarioId"`
	ScenarioKind   string `json:"scenarioKind"`
	ScenarioDigest string `json:"scenarioDigest"`
	Shard          int    `json:"shard"`
	RowSHA256      string `json:"rowSha256"`
}

// FailureFingerprint is intentionally limited to stable failure coordinates.
type FailureFingerprint struct {
	SchemaVersion int    `json:"schemaVersion"`
	Code          string `json:"code"`
	Phase         string `json:"phase"`
	Coordinate    string `json:"coordinate"`
	SHA256        string `json:"sha256"`
}

type FailureTransition struct {
	Identity KnownFailureIdentity `json:"identity"`
	From     FailureFingerprint   `json:"from"`
	To       FailureFingerprint   `json:"to"`
	Reason   string               `json:"reason"`
}

type InventoryRemoval struct {
	Identity KnownFailureIdentity `json:"identity"`
	Reason   string               `json:"reason"`
}

// KnownFailureRow is the evaluator's minimal projection of independently
// validated synthetic evidence.
type KnownFailureRow struct {
	Identity KnownFailureIdentity
	Passed   bool
	Failure  FailureFingerprint
}

type KnownFailureComparison struct {
	Base     *KnownFailureLedger
	Head     *KnownFailureLedger
	HeadRows []KnownFailureRow
}

// KnownFailureComparisonResult keeps proof and debt accounting separate.
type KnownFailureComparisonResult struct {
	Failure            string
	Passed             []KnownFailureIdentity
	KnownDebt          []KnownFailureIdentity
	ResolvedDebt       []KnownFailureIdentity
	NewFailures        []KnownFailureIdentity
	ChangedFailures    []KnownFailureIdentity
	MissingDebt        []KnownFailureIdentity
	StaleDebt          []KnownFailureIdentity
	InventoryAdditions []KnownFailureIdentity
	InventoryRemovals  []KnownFailureIdentity
}

func (result KnownFailureComparisonResult) Clean() bool { return result.Failure == "" }

// NewFailureFingerprint returns the only failure representation used for debt
// matching. Detail and complete row evidence are deliberately excluded.
func NewFailureFingerprint(code, phase, coordinate string) (FailureFingerprint, error) {
	fingerprint := FailureFingerprint{SchemaVersion: SchemaVersion, Code: code, Phase: phase, Coordinate: coordinate}
	if err := validateFailureFingerprint(fingerprint, false); err != nil {
		return FailureFingerprint{}, err
	}
	fingerprint.SHA256 = failureFingerprintSHA256(fingerprint)
	return fingerprint, nil
}

func (fingerprint FailureFingerprint) CanonicalJSON() string {
	return fmt.Sprintf(`{"schemaVersion":1,"code":%s,"phase":%s,"coordinate":%s}`, jsonString(fingerprint.Code), jsonString(fingerprint.Phase), jsonString(fingerprint.Coordinate))
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func failureFingerprintSHA256(fingerprint FailureFingerprint) string {
	sum := sha256.Sum256([]byte(fingerprint.CanonicalJSON()))
	return hex.EncodeToString(sum[:])
}

// ParseKnownFailureLedger applies the ledger's separate, bounded strict JSON
// boundary. It never falls back to a candidate or constructs an empty ledger.
func ParseKnownFailureLedger(data []byte) (KnownFailureLedger, error) {
	if len(data) == 0 || len(data) > maxKnownFailureLedgerSize {
		return KnownFailureLedger{}, errors.New("known-failure ledger is missing or oversized")
	}
	if err := rejectKnownFailureLedgerDuplicateJSONKeys(data); err != nil {
		return KnownFailureLedger{}, err
	}
	if err := rejectKnownFailureLedgerNulls(data); err != nil {
		return KnownFailureLedger{}, err
	}
	if err := requireKnownFailureLedgerFields(data); err != nil {
		return KnownFailureLedger{}, err
	}
	var ledger KnownFailureLedger
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return KnownFailureLedger{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return KnownFailureLedger{}, errors.New("known-failure ledger contains multiple JSON values")
		}
		return KnownFailureLedger{}, err
	}
	if err := ValidateKnownFailureLedger(ledger); err != nil {
		return KnownFailureLedger{}, err
	}
	return ledger, nil
}

func rejectKnownFailureLedgerDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := walkKnownFailureLedgerDuplicateKeys(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return err
	}
	return nil
}

func walkKnownFailureLedgerDuplicateKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := []string{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("known-failure ledger object key is not a string")
			}
			for _, prior := range seen {
				if strings.EqualFold(prior, name) {
					return fmt.Errorf("duplicate known-failure ledger object key %q", name)
				}
			}
			seen = append(seen, name)
			if err := walkKnownFailureLedgerDuplicateKeys(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkKnownFailureLedgerDuplicateKeys(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return errors.New("unexpected known-failure ledger JSON delimiter")
	}
}

func requireKnownFailureLedgerFields(data []byte) error {
	root, err := knownFailureLedgerObject(data, "ledger", "schemaVersion", "inventory", "knownFailures", "failureTransitions", "inventoryRemovals")
	if err != nil {
		return err
	}
	if err := requireKnownFailureInventory(root["inventory"]); err != nil {
		return err
	}
	if err := requireKnownFailureArray(root["knownFailures"], requireKnownFailure); err != nil {
		return err
	}
	if err := requireKnownFailureArray(root["failureTransitions"], requireFailureTransition); err != nil {
		return err
	}
	return requireKnownFailureArray(root["inventoryRemovals"], requireInventoryRemoval)
}

func requireKnownFailureInventory(data json.RawMessage) error {
	object, err := knownFailureLedgerObject(data, "inventory", "moduleCount", "scenarioCount", "sha256", "rows")
	if err != nil {
		return err
	}
	return requireKnownFailureArray(object["rows"], requireKnownFailureIdentity)
}

func requireKnownFailure(data json.RawMessage) error {
	object, err := knownFailureLedgerObject(data, "known failure", "identity", "previousEvidence", "failure", "detail")
	if err != nil {
		return err
	}
	if err := requireKnownFailureIdentity(object["identity"]); err != nil {
		return err
	}
	if err := requirePreviousEvidence(object["previousEvidence"]); err != nil {
		return err
	}
	return requireFailureFingerprint(object["failure"])
}

func requirePreviousEvidence(data json.RawMessage) error {
	object, err := knownFailureLedgerObject(data, "previous evidence", "commit", "engineSha256", "repositoryHash", "row")
	if err != nil {
		return err
	}
	_, err = knownFailureLedgerObject(object["row"], "previous evidence row", "moduleId", "moduleRevision", "scenarioId", "scenarioKind", "scenarioDigest", "shard", "rowSha256")
	return err
}

func requireFailureFingerprint(data json.RawMessage) error {
	_, err := knownFailureLedgerObject(data, "failure fingerprint", "schemaVersion", "code", "phase", "coordinate", "sha256")
	return err
}

func requireFailureTransition(data json.RawMessage) error {
	object, err := knownFailureLedgerObject(data, "failure transition", "identity", "from", "to", "reason")
	if err != nil {
		return err
	}
	if err := requireKnownFailureIdentity(object["identity"]); err != nil {
		return err
	}
	if err := requireFailureFingerprint(object["from"]); err != nil {
		return err
	}
	return requireFailureFingerprint(object["to"])
}

func requireInventoryRemoval(data json.RawMessage) error {
	object, err := knownFailureLedgerObject(data, "inventory removal", "identity", "reason")
	if err != nil {
		return err
	}
	return requireKnownFailureIdentity(object["identity"])
}

func requireKnownFailureIdentity(data json.RawMessage) error {
	_, err := knownFailureLedgerObject(data, "known-failure identity", "moduleId", "scenarioId", "kind")
	return err
}

func knownFailureLedgerObject(data []byte, name string, required ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("invalid %s object", name)
	}
	for _, field := range required {
		if _, found := object[field]; !found {
			return nil, fmt.Errorf("known-failure ledger %s is missing %q", name, field)
		}
	}
	return object, nil
}

func requireKnownFailureArray(data []byte, requireElement func(json.RawMessage) error) error {
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil || values == nil {
		return errors.New("known-failure ledger has invalid array")
	}
	for _, value := range values {
		if err := requireElement(value); err != nil {
			return err
		}
	}
	return nil
}

func rejectKnownFailureLedgerNulls(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := walkKnownFailureLedgerValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return err
	}
	return nil
}

func walkKnownFailureLedgerValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return errors.New("known-failure ledger contains null")
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := walkKnownFailureLedgerValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkKnownFailureLedgerValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return errors.New("unexpected known-failure ledger JSON delimiter")
	}
}

func ValidateKnownFailureLedger(ledger KnownFailureLedger) error {
	if ledger.SchemaVersion != SchemaVersion || ledger.KnownFailures == nil || ledger.FailureTransitions == nil || ledger.InventoryRemovals == nil || ledger.Inventory.Rows == nil {
		return errors.New("known-failure ledger has missing required fields")
	}
	if err := validateInventory(ledger.Inventory); err != nil {
		return err
	}
	debts := make(map[string]KnownFailure, len(ledger.KnownFailures))
	previous := ""
	for _, debt := range ledger.KnownFailures {
		key, err := identityKey(debt.Identity)
		if err != nil {
			return err
		}
		if previous >= key {
			return errors.New("known failures are not canonically sorted and unique")
		}
		previous = key
		if !containsInventory(ledger.Inventory.Rows, key) {
			return errors.New("known failure is outside inventory")
		}
		if strings.TrimSpace(debt.Detail) == "" {
			return errors.New("known failure has missing detail")
		}
		if err := validatePreviousEvidence(debt.PreviousEvidence, debt.Identity); err != nil {
			return err
		}
		if err := validateFailureFingerprint(debt.Failure, true); err != nil {
			return err
		}
		debts[key] = debt
	}
	previous = ""
	for _, transition := range ledger.FailureTransitions {
		key, err := identityKey(transition.Identity)
		if err != nil {
			return err
		}
		if previous >= key {
			return errors.New("failure transitions are not canonically sorted and unique")
		}
		previous = key
		debt, found := debts[key]
		if !found || !sameFingerprint(transition.From, debt.Failure) || sameFingerprint(transition.From, transition.To) || strings.TrimSpace(transition.Reason) == "" {
			return errors.New("invalid failure transition")
		}
		if err := validateFailureFingerprint(transition.From, true); err != nil {
			return err
		}
		if err := validateFailureFingerprint(transition.To, true); err != nil {
			return err
		}
	}
	previous = ""
	for _, removal := range ledger.InventoryRemovals {
		key, err := identityKey(removal.Identity)
		if err != nil {
			return err
		}
		if previous >= key || strings.TrimSpace(removal.Reason) == "" || !containsInventory(ledger.Inventory.Rows, key) {
			return errors.New("inventory removals are not canonically sorted and unique")
		}
		previous = key
	}
	return nil
}

func validateInventory(inventory KnownFailureInventory) error {
	if inventory.ModuleCount < 0 || inventory.ScenarioCount < 0 || !sha256Pattern.MatchString(inventory.SHA256) {
		return errors.New("invalid known-failure inventory")
	}
	modules := map[string]struct{}{}
	previous := ""
	for _, row := range inventory.Rows {
		key, err := identityKey(row)
		if err != nil {
			return err
		}
		if previous >= key {
			return errors.New("inventory rows are not canonically sorted and unique")
		}
		previous = key
		modules[row.ModuleID] = struct{}{}
	}
	if inventory.ModuleCount != len(modules) || inventory.ScenarioCount != len(inventory.Rows) || inventory.SHA256 != inventorySHA256(inventory) {
		return errors.New("known-failure inventory count or digest mismatch")
	}
	return nil
}

func inventorySHA256(inventory KnownFailureInventory) string {
	projection := struct {
		SchemaVersion int                    `json:"schemaVersion"`
		ModuleCount   int                    `json:"moduleCount"`
		ScenarioCount int                    `json:"scenarioCount"`
		Rows          []KnownFailureIdentity `json:"rows"`
	}{SchemaVersion: SchemaVersion, ModuleCount: inventory.ModuleCount, ScenarioCount: inventory.ScenarioCount, Rows: inventory.Rows}
	data, _ := json.Marshal(projection)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validatePreviousEvidence(evidence PreviousEvidence, identity KnownFailureIdentity) error {
	if !commitPattern.MatchString(evidence.Commit) || !sha256Pattern.MatchString(evidence.EngineSHA256) || !sha256Pattern.MatchString(evidence.RepositoryHash) ||
		!sha256Pattern.MatchString(evidence.Row.ModuleRevision) || !sha256Pattern.MatchString(evidence.Row.ScenarioDigest) || !sha256Pattern.MatchString(evidence.Row.RowSHA256) || evidence.Row.Shard < 0 ||
		evidence.Row.ModuleID != identity.ModuleID || evidence.Row.ScenarioID != identity.ScenarioID || evidence.Row.ScenarioKind != identity.Kind {
		return errors.New("known failure previous evidence does not match identity")
	}
	return nil
}

func validateFailureFingerprint(fingerprint FailureFingerprint, requireHash bool) error {
	if fingerprint.SchemaVersion != SchemaVersion || !stableFailurePartPattern.MatchString(fingerprint.Code) || strings.TrimSpace(fingerprint.Phase) == "" || strings.TrimSpace(fingerprint.Coordinate) == "" {
		return errors.New("invalid failure fingerprint")
	}
	if requireHash && (!sha256Pattern.MatchString(fingerprint.SHA256) || fingerprint.SHA256 != failureFingerprintSHA256(fingerprint)) {
		return errors.New("failure fingerprint digest mismatch")
	}
	return nil
}

func identityKey(identity KnownFailureIdentity) (string, error) {
	if !knownFailureIdentityPartPattern.MatchString(identity.ModuleID) || !knownFailureIdentityPartPattern.MatchString(identity.ScenarioID) || !knownFailureIdentityPartPattern.MatchString(identity.Kind) {
		return "", errors.New("invalid known-failure identity")
	}
	return identity.ModuleID + "\x00" + identity.ScenarioID + "\x00" + identity.Kind, nil
}

func containsInventory(rows []KnownFailureIdentity, key string) bool {
	index := sort.Search(len(rows), func(index int) bool { candidate, _ := identityKey(rows[index]); return candidate >= key })
	return index < len(rows) && func() bool { candidate, _ := identityKey(rows[index]); return candidate == key }()
}

func sameFingerprint(left, right FailureFingerprint) bool {
	return left.SchemaVersion == right.SchemaVersion && left.Code == right.Code && left.Phase == right.Phase && left.Coordinate == right.Coordinate && left.SHA256 == right.SHA256
}

// EvaluateKnownFailureLedger compares protected-main authority with a candidate
// and independently validated head-row projection. It never creates authority
// from candidate state.
func EvaluateKnownFailureLedger(comparison KnownFailureComparison) KnownFailureComparisonResult {
	result := KnownFailureComparisonResult{}
	if comparison.Base == nil {
		result.Failure = "missing known-failure authority"
		return result
	}
	if comparison.Head == nil {
		result.Failure = "missing known-failure candidate"
		return result
	}
	if err := ValidateKnownFailureLedger(*comparison.Base); err != nil {
		result.Failure = "malformed known-failure authority"
		return result
	}
	if err := ValidateKnownFailureLedger(*comparison.Head); err != nil {
		result.Failure = "malformed known-failure candidate"
		return result
	}
	baseInventory := inventoryMap(comparison.Base.Inventory.Rows)
	headInventory := inventoryMap(comparison.Head.Inventory.Rows)
	baseDebts := debtMap(comparison.Base.KnownFailures)
	headDebts := debtMap(comparison.Head.KnownFailures)
	baseTransitions := transitionMap(comparison.Base.FailureTransitions)
	headTransitions := transitionMap(comparison.Head.FailureTransitions)
	baseRemovals := removalMap(comparison.Base.InventoryRemovals)
	headRemovals := removalMap(comparison.Head.InventoryRemovals)
	headRows := map[string]KnownFailureRow{}
	for _, row := range comparison.HeadRows {
		key, err := identityKey(row.Identity)
		if err != nil || !containsIdentity(headInventory, key) || containsRow(headRows, key) || (!row.Passed && validateFailureFingerprint(row.Failure, true) != nil) {
			result.Failure = "missing or malformed head evidence"
			return result
		}
		headRows[key] = row
	}
	if len(headRows) != len(headInventory) {
		result.Failure = "missing or malformed head evidence"
		return result
	}
	for _, identity := range comparison.Base.Inventory.Rows {
		key, _ := identityKey(identity)
		if containsIdentity(headInventory, key) {
			continue
		}
		if _, authorized := baseRemovals[key]; authorized {
			result.InventoryRemovals = append(result.InventoryRemovals, identity)
			if _, retained := headRemovals[key]; retained {
				result.fail("authorized row removal not consumed")
			}
			continue
		}
		result.InventoryRemovals = append(result.InventoryRemovals, identity)
		result.fail("unauthorized row removal")
	}
	for _, identity := range comparison.Head.Inventory.Rows {
		key, _ := identityKey(identity)
		if !containsIdentity(baseInventory, key) {
			result.InventoryAdditions = append(result.InventoryAdditions, identity)
		}
	}
	for _, identity := range comparison.Head.Inventory.Rows {
		key, _ := identityKey(identity)
		row := headRows[key]
		baseDebt, hadDebt := baseDebts[key]
		headDebt, keepsDebt := headDebts[key]
		if row.Passed {
			if keepsDebt {
				result.StaleDebt = append(result.StaleDebt, identity)
				result.fail("stale known-failure debt")
				continue
			}
			result.Passed = append(result.Passed, identity)
			if hadDebt {
				result.ResolvedDebt = append(result.ResolvedDebt, identity)
			}
			continue
		}
		if !hadDebt {
			result.NewFailures = append(result.NewFailures, identity)
			result.fail("new failed row")
			continue
		}
		if !keepsDebt {
			result.MissingDebt = append(result.MissingDebt, identity)
			result.fail("removed known-failure debt")
			continue
		}
		if sameFingerprint(row.Failure, baseDebt.Failure) {
			if transition, added := headTransitions[key]; added {
				if _, wasBaseAuthority := baseTransitions[key]; !wasBaseAuthority && !sameFingerprint(transition.From, headDebt.Failure) {
					result.ChangedFailures = append(result.ChangedFailures, identity)
					result.fail("invalid head-added failure transition")
					continue
				}
			}
			if !sameFingerprint(headDebt.Failure, baseDebt.Failure) {
				result.ChangedFailures = append(result.ChangedFailures, identity)
				result.fail("unreviewed failure fingerprint transition")
				continue
			}
			result.KnownDebt = append(result.KnownDebt, identity)
			continue
		}
		transition, authorized := baseTransitions[key]
		if !authorized || !sameFingerprint(transition.From, baseDebt.Failure) || !sameFingerprint(transition.To, row.Failure) {
			result.ChangedFailures = append(result.ChangedFailures, identity)
			result.fail("unreviewed failure fingerprint transition")
			continue
		}
		if _, retained := headTransitions[key]; retained {
			result.ChangedFailures = append(result.ChangedFailures, identity)
			result.fail("authorized transition not consumed")
			continue
		}
		if !sameFingerprint(headDebt.Failure, row.Failure) {
			result.ChangedFailures = append(result.ChangedFailures, identity)
			result.fail("unreviewed failure fingerprint transition")
			continue
		}
		result.KnownDebt = append(result.KnownDebt, identity)
	}
	return result
}

func (result *KnownFailureComparisonResult) fail(classification string) {
	if result.Failure == "" {
		result.Failure = classification
	}
}

func inventoryMap(rows []KnownFailureIdentity) map[string]KnownFailureIdentity {
	result := make(map[string]KnownFailureIdentity, len(rows))
	for _, row := range rows {
		key, _ := identityKey(row)
		result[key] = row
	}
	return result
}
func debtMap(rows []KnownFailure) map[string]KnownFailure {
	result := make(map[string]KnownFailure, len(rows))
	for _, row := range rows {
		key, _ := identityKey(row.Identity)
		result[key] = row
	}
	return result
}
func transitionMap(rows []FailureTransition) map[string]FailureTransition {
	result := make(map[string]FailureTransition, len(rows))
	for _, row := range rows {
		key, _ := identityKey(row.Identity)
		result[key] = row
	}
	return result
}
func removalMap(rows []InventoryRemoval) map[string]InventoryRemoval {
	result := make(map[string]InventoryRemoval, len(rows))
	for _, row := range rows {
		key, _ := identityKey(row.Identity)
		result[key] = row
	}
	return result
}
func containsIdentity(values map[string]KnownFailureIdentity, key string) bool {
	_, found := values[key]
	return found
}
func containsRow(values map[string]KnownFailureRow, key string) bool {
	_, found := values[key]
	return found
}
