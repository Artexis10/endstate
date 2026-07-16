// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	HistoryDiagnosticReadFailed                       = "generation_history_read_failed"
	HistoryDiagnosticInvalidJSON                      = "generation_history_invalid_json"
	HistoryDiagnosticUnsupportedSchema                = "generation_history_unsupported_schema"
	HistoryDiagnosticMissingGenerations               = "generation_history_missing_generations"
	HistoryDiagnosticInvalidIdentity                  = "generation_history_invalid_identity"
	HistoryDiagnosticDuplicateIdentity                = "generation_history_duplicate_identity"
	HistoryDiagnosticUnsortedIdentity                 = "generation_history_unsorted_identity"
	HistoryDiagnosticMissingFingerprint               = "generation_history_missing_fingerprint"
	HistoryDiagnosticInvalidFingerprint               = "generation_history_invalid_fingerprint"
	HistoryDiagnosticDuplicateFingerprint             = "generation_history_duplicate_fingerprint"
	HistoryDiagnosticUnsortedFingerprint              = "generation_history_unsorted_fingerprint"
	HistoryDiagnosticCurrentIdentityMissing           = "generation_history_current_identity_missing"
	HistoryDiagnosticCurrentFingerprintInvalid        = "generation_history_current_fingerprint_invalid"
	HistoryDiagnosticCurrentFingerprintMissing        = "generation_history_current_fingerprint_missing"
	HistoryDiagnosticInvalidAcceptedFingerprint       = "generation_history_accepted_fingerprint_invalid"
	HistoryDiagnosticDuplicateAcceptedFingerprint     = "generation_history_accepted_fingerprint_duplicate"
	HistoryDiagnosticCurrentFingerprintAccepted       = "generation_history_current_fingerprint_accepted"
	HistoryDiagnosticAcceptedFingerprintNotRecorded   = "generation_history_accepted_fingerprint_not_recorded"
	HistoryDiagnosticHistoricalFingerprintNotAccepted = "generation_history_historical_fingerprint_not_accepted"
)

var generationFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// GenerationHistory is governed product data recording every released
// fingerprint for each stable module/config-set/generation identity.
type GenerationHistory struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Generations   []GenerationHistoryEntry `json:"generations"`
}

// GenerationHistoryEntry records the retained fingerprints for one stable
// generation identity. Repository policy keeps entries and fingerprints in
// strict lexical order so changes remain reviewable.
type GenerationHistoryEntry struct {
	ModuleID     string   `json:"moduleId"`
	ConfigSetID  string   `json:"configSetId"`
	GenerationID string   `json:"generationId"`
	Fingerprints []string `json:"fingerprints"`
}

// Identity returns the fully scoped stable generation identity.
func (e GenerationHistoryEntry) Identity() string {
	return e.ModuleID + "/" + e.ConfigSetID + "/" + e.GenerationID
}

// GenerationHistoryValidationError is a stable, machine-readable failure
// suitable for repository CI diagnostics.
type GenerationHistoryValidationError struct {
	Code     string `json:"code"`
	Identity string `json:"identity,omitempty"`
	Field    string `json:"field,omitempty"`
	Detail   string `json:"detail"`
}

func (e *GenerationHistoryValidationError) Error() string {
	if e.Identity != "" {
		return fmt.Sprintf("invalid generation history %s: %s", e.Identity, e.Detail)
	}
	return "invalid generation history: " + e.Detail
}

// GenerationHistoryDiagnosticCode extracts the stable CI diagnostic code.
func GenerationHistoryDiagnosticCode(err error) string {
	var validationError *GenerationHistoryValidationError
	if errors.As(err, &validationError) {
		return validationError.Code
	}
	return ""
}

// LoadGenerationHistory strictly decodes and validates the governed ledger.
func LoadGenerationHistory(path string) (*GenerationHistory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, historyError(HistoryDiagnosticReadFailed, "", "", "read %s: %v", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var history GenerationHistory
	if err := decoder.Decode(&history); err != nil {
		return nil, historyError(HistoryDiagnosticInvalidJSON, "", "", "decode %s: %v", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, historyError(HistoryDiagnosticInvalidJSON, "", "", "%s contains multiple JSON values", path)
		}
		return nil, historyError(HistoryDiagnosticInvalidJSON, "", "", "decode %s: %v", path, err)
	}
	if err := ValidateGenerationHistory(&history); err != nil {
		return nil, err
	}
	return &history, nil
}

// ValidateGenerationHistory validates the ledger independently from catalog
// loading. Cross-checking against a current catalog is handled separately.
func ValidateGenerationHistory(history *GenerationHistory) error {
	if history == nil {
		return historyError(HistoryDiagnosticInvalidJSON, "", "", "history is nil")
	}
	if history.SchemaVersion != 1 {
		return historyError(HistoryDiagnosticUnsupportedSchema, "", "schemaVersion", "schemaVersion %d is not supported", history.SchemaVersion)
	}
	if history.Generations == nil {
		return historyError(HistoryDiagnosticMissingGenerations, "", "generations", "generations must be present (use an empty array when no schema-v2 generations are released)")
	}

	for index := range history.Generations {
		entry := &history.Generations[index]
		identity := entry.Identity()
		identityFields := []struct {
			name  string
			value string
		}{
			{name: "moduleId", value: entry.ModuleID},
			{name: "configSetId", value: entry.ConfigSetID},
			{name: "generationId", value: entry.GenerationID},
		}
		for _, field := range identityFields {
			if !stableIDPattern.MatchString(field.value) {
				return historyError(HistoryDiagnosticInvalidIdentity, identity, field.name, "%s %q is not a stable lowercase ID", field.name, field.value)
			}
		}
		if index > 0 {
			order := compareHistoryIdentity(history.Generations[index-1], *entry)
			switch {
			case order == 0:
				return historyError(HistoryDiagnosticDuplicateIdentity, identity, "generations", "generation identity is duplicated")
			case order > 0:
				return historyError(HistoryDiagnosticUnsortedIdentity, identity, "generations", "generation identities must be strictly sorted by moduleId, configSetId, and generationId")
			}
		}
		if len(entry.Fingerprints) == 0 {
			return historyError(HistoryDiagnosticMissingFingerprint, identity, "fingerprints", "at least one released fingerprint is required")
		}
		for fingerprintIndex, fingerprint := range entry.Fingerprints {
			if !generationFingerprintPattern.MatchString(fingerprint) {
				return historyError(HistoryDiagnosticInvalidFingerprint, identity, "fingerprints", "fingerprint %q must be lowercase 64-character SHA-256 hex", fingerprint)
			}
			if fingerprintIndex == 0 {
				continue
			}
			previous := entry.Fingerprints[fingerprintIndex-1]
			switch strings.Compare(previous, fingerprint) {
			case 0:
				return historyError(HistoryDiagnosticDuplicateFingerprint, identity, "fingerprints", "fingerprint %q is duplicated", fingerprint)
			case 1:
				return historyError(HistoryDiagnosticUnsortedFingerprint, identity, "fingerprints", "fingerprints must be strictly sorted")
			}
		}
	}
	return nil
}

// ValidateRepositoryGenerationHistory cross-checks an already loaded current
// catalog against an already loaded ledger. Keeping this separate from catalog
// loading makes released-history governance an explicit repository/CI gate,
// not runtime interpretation of untrusted source metadata.
//
// The ledger is governed product data, not a cryptographic append-only log.
// This validation prevents silent generation-ID repurposing while released
// history remains retained and reviewed in the repository.
func ValidateRepositoryGenerationHistory(catalog map[string]*Module, history *GenerationHistory) error {
	if err := ValidateGenerationHistory(history); err != nil {
		return err
	}

	historyByIdentity := make(map[string]*GenerationHistoryEntry, len(history.Generations))
	for index := range history.Generations {
		entry := &history.Generations[index]
		historyByIdentity[entry.Identity()] = entry
	}

	current := collectCurrentGenerations(catalog)
	for _, declaration := range current {
		identity := declaration.Identity()
		fingerprint := declaration.Generation.Fingerprint
		if !generationFingerprintPattern.MatchString(fingerprint) {
			return historyError(HistoryDiagnosticCurrentFingerprintInvalid, identity, "fingerprint", "current generation fingerprint %q must be lowercase 64-character SHA-256 hex", fingerprint)
		}

		entry, exists := historyByIdentity[identity]
		if !exists {
			return historyError(HistoryDiagnosticCurrentIdentityMissing, identity, "generations", "current schema-v2 generation is not registered in released history")
		}
		recorded := make(map[string]struct{}, len(entry.Fingerprints))
		for _, releasedFingerprint := range entry.Fingerprints {
			recorded[releasedFingerprint] = struct{}{}
		}
		if _, exists := recorded[fingerprint]; !exists {
			return historyError(HistoryDiagnosticCurrentFingerprintMissing, identity, "fingerprints", "current fingerprint %q is not registered; generation IDs cannot be silently repurposed", fingerprint)
		}

		accepted := make(map[string]struct{}, len(declaration.Generation.AcceptsSourceFingerprints))
		for _, acceptedFingerprint := range declaration.Generation.AcceptsSourceFingerprints {
			if !generationFingerprintPattern.MatchString(acceptedFingerprint) {
				return historyError(HistoryDiagnosticInvalidAcceptedFingerprint, identity, "acceptsSourceFingerprints", "accepted fingerprint %q must be lowercase 64-character SHA-256 hex", acceptedFingerprint)
			}
			if _, duplicate := accepted[acceptedFingerprint]; duplicate {
				return historyError(HistoryDiagnosticDuplicateAcceptedFingerprint, identity, "acceptsSourceFingerprints", "accepted fingerprint %q is duplicated", acceptedFingerprint)
			}
			accepted[acceptedFingerprint] = struct{}{}
			if acceptedFingerprint == fingerprint {
				return historyError(HistoryDiagnosticCurrentFingerprintAccepted, identity, "acceptsSourceFingerprints", "current fingerprint %q must not be listed as historical", acceptedFingerprint)
			}
			if _, exists := recorded[acceptedFingerprint]; !exists {
				return historyError(HistoryDiagnosticAcceptedFingerprintNotRecorded, identity, "acceptsSourceFingerprints", "accepted fingerprint %q is not recorded under the same generation identity", acceptedFingerprint)
			}
		}

		for _, releasedFingerprint := range entry.Fingerprints {
			if releasedFingerprint == fingerprint {
				continue
			}
			if _, exists := accepted[releasedFingerprint]; !exists {
				return historyError(HistoryDiagnosticHistoricalFingerprintNotAccepted, identity, "acceptsSourceFingerprints", "released historical fingerprint %q must be explicitly accepted", releasedFingerprint)
			}
		}
	}
	return nil
}

type currentGenerationDeclaration struct {
	ModuleID     string
	ConfigSetID  string
	GenerationID string
	Generation   *GenerationDef
}

func (d currentGenerationDeclaration) Identity() string {
	return d.ModuleID + "/" + d.ConfigSetID + "/" + d.GenerationID
}

func collectCurrentGenerations(catalog map[string]*Module) []currentGenerationDeclaration {
	var current []currentGenerationDeclaration
	for _, mod := range catalog {
		if mod == nil || mod.EffectiveSchemaVersion() != 2 || mod.Config == nil {
			continue
		}
		for setIndex := range mod.Config.Sets {
			set := &mod.Config.Sets[setIndex]
			for generationIndex := range set.Generations {
				generation := &set.Generations[generationIndex]
				current = append(current, currentGenerationDeclaration{
					ModuleID:     mod.ID,
					ConfigSetID:  set.ID,
					GenerationID: generation.ID,
					Generation:   generation,
				})
			}
		}
	}
	sort.Slice(current, func(left, right int) bool {
		return compareCurrentGenerationIdentity(current[left], current[right]) < 0
	})
	return current
}

func compareCurrentGenerationIdentity(left, right currentGenerationDeclaration) int {
	if order := strings.Compare(left.ModuleID, right.ModuleID); order != 0 {
		return order
	}
	if order := strings.Compare(left.ConfigSetID, right.ConfigSetID); order != 0 {
		return order
	}
	return strings.Compare(left.GenerationID, right.GenerationID)
}

func compareHistoryIdentity(left, right GenerationHistoryEntry) int {
	if order := strings.Compare(left.ModuleID, right.ModuleID); order != 0 {
		return order
	}
	if order := strings.Compare(left.ConfigSetID, right.ConfigSetID); order != 0 {
		return order
	}
	return strings.Compare(left.GenerationID, right.GenerationID)
}

func historyError(code, identity, field, format string, args ...any) error {
	return &GenerationHistoryValidationError{
		Code:     code,
		Identity: identity,
		Field:    field,
		Detail:   fmt.Sprintf(format, args...),
	}
}
