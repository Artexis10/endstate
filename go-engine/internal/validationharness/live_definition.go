// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

const (
	LiveDefinitionSchemaVersion = 1
	LiveResultSchemaVersion     = 1
	maxLiveMappings             = 64
	maxLiveAttempts             = 3
	maxLiveStringBytes          = 512
)

type LiveStatus string

const (
	LiveStatusPending LiveStatus = "pending"
	LiveStatusPassed  LiveStatus = "passed"
	LiveStatusFailed  LiveStatus = "failed"
)

type LivePhase string

const (
	LivePhasePreparation LivePhase = "preparation"
	LivePhasePackage     LivePhase = "package"
	LivePhaseSeed        LivePhase = "seed"
	LivePhaseCapture     LivePhase = "capture"
	LivePhaseRestore     LivePhase = "restore"
	LivePhaseCompare     LivePhase = "compare"
)

type LiveFailureCategory string

const (
	LiveFailureNone        LiveFailureCategory = ""
	LiveFailureDefinition  LiveFailureCategory = "definition"
	LiveFailureSeed        LiveFailureCategory = "seed"
	LiveFailurePackage     LiveFailureCategory = "package"
	LiveFailureCapture     LiveFailureCategory = "capture"
	LiveFailureRestore     LiveFailureCategory = "restore"
	LiveFailureComparison  LiveFailureCategory = "comparison"
	LiveFailureEnvironment LiveFailureCategory = "environment"
)

// LiveRequest contains only a compiled, non-authorizing diagnostic definition.
type LiveRequest struct {
	SchemaVersion int            `json:"schemaVersion"`
	Definition    LiveDefinition `json:"definition"`
	MaxAttempts   int            `json:"maxAttempts"`
}

// LiveDefinition is preparation data only. It cannot authorize package or
// configuration mutation.
type LiveDefinition struct {
	SchemaVersion           int                         `json:"schemaVersion"`
	ModuleID                string                      `json:"moduleId"`
	ModuleRevision          string                      `json:"moduleRevision"`
	ValidationSourceSHA256  string                      `json:"validationSourceSha256"`
	Policy                  validationmatrix.LivePolicy `json:"policy"`
	WingetRef               string                      `json:"wingetRef"`
	SeedRepositoryPath      string                      `json:"seedRepositoryPath"`
	SeedSHA256              string                      `json:"seedSha256"`
	RunnerLabel             string                      `json:"runnerLabel"`
	PRTimeoutMinutes        int                         `json:"prTimeoutMinutes"`
	ScheduledTimeoutMinutes int                         `json:"scheduledTimeoutMinutes"`
	Comparator              ExactBytesComparator        `json:"comparator"`
	NonAuthorizing          bool                        `json:"nonAuthorizing"`
	MutationAuthorized      bool                        `json:"mutationAuthorized"`
}

// ExactBytesComparator describes the stable mappings a later runner may
// snapshot and compare. It deliberately contains templates, never resolved
// host paths or bytes.
type ExactBytesComparator struct {
	Mappings                []ComparatorMapping `json:"mappings"`
	MinimumExistingMappings int                 `json:"minimumExistingMappings"`
}

type ComparatorMapping struct {
	Identity        string `json:"identity"`
	CaptureTemplate string `json:"captureTemplate"`
	RestoreTemplate string `json:"restoreTemplate"`
	Optional        bool   `json:"optional"`
}

type PackageObservation struct {
	Ref     string `json:"ref"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status"`
}

type ComparatorOutcome struct {
	Identity string `json:"identity"`
	Status   string `json:"status"`
}

type LiveAttempt struct {
	Number          int                 `json:"number"`
	Phase           LivePhase           `json:"phase"`
	Status          LiveStatus          `json:"status"`
	Package         PackageObservation  `json:"package"`
	Comparator      []ComparatorOutcome `json:"comparator"`
	FailureCategory LiveFailureCategory `json:"failureCategory,omitempty"`
}

// LiveResult has no fields for config bytes, paths, environment state, command
// output, or secret-derived material. This slice never grants public evidence.
type LiveResult struct {
	SchemaVersion          int                           `json:"schemaVersion"`
	ModuleID               string                        `json:"moduleId"`
	ModuleRevision         string                        `json:"moduleRevision"`
	ValidationSourceSHA256 string                        `json:"validationSourceSha256"`
	DefinitionSHA256       string                        `json:"definitionSha256"`
	Status                 LiveStatus                    `json:"status"`
	PublicEvidenceEligible bool                          `json:"publicEvidenceEligible"`
	ProvenProofLevels      []validationmatrix.ProofLevel `json:"provenProofLevels"`
	Attempts               []LiveAttempt                 `json:"attempts"`
	FailureCategory        LiveFailureCategory           `json:"failureCategory,omitempty"`
}

// CompileLiveDefinition loads current repository authority and prepares one
// candidate policy for diagnosis only. It never authorizes mutation or proof.
func CompileLiveDefinition(repoRoot, moduleID string) (LiveDefinition, error) {
	if repoRoot == "" || !filepath.IsAbs(repoRoot) || filepath.Clean(repoRoot) != repoRoot {
		return LiveDefinition{}, fmt.Errorf("live definition repository root must be canonical and absolute")
	}
	if err := safepath.ValidateRoot(repoRoot); err != nil {
		return LiveDefinition{}, fmt.Errorf("live definition repository root is unsafe: %w", err)
	}
	catalog, err := validationmatrix.LoadCatalog(repoRoot, time.Now().UTC())
	if err != nil {
		return LiveDefinition{}, fmt.Errorf("load validation catalog: %w", err)
	}
	record, ok := catalog.Records[moduleID]
	if !ok {
		return LiveDefinition{}, fmt.Errorf("live definition module %q is absent", moduleID)
	}
	mod := catalog.Modules[moduleID]
	if mod == nil {
		return LiveDefinition{}, fmt.Errorf("live definition module %q has no production definition", moduleID)
	}
	return compileLiveDefinitionAt(repoRoot, record, mod)
}

func compileLiveDefinition(record validationmatrix.ValidationRecord, module *modules.Module) (LiveDefinition, error) {
	return compileLiveDefinitionAt("", record, module)
}

func compileLiveDefinitionAt(repoRoot string, record validationmatrix.ValidationRecord, module *modules.Module) (LiveDefinition, error) {
	if module == nil || record.ModuleID == "" || record.ModuleID != module.ID || record.Live.Mode != validationmatrix.LiveCandidate {
		return LiveDefinition{}, fmt.Errorf("live definition requires a candidate policy for its current module")
	}
	if module.EffectiveSchemaVersion() != 1 {
		return LiveDefinition{}, fmt.Errorf("live definition supports schema-v1 modules only")
	}
	if err := rejectDuplicateJSONKeys(manifest.StripJsoncComments(record.SourceSnapshot())); err != nil {
		return LiveDefinition{}, fmt.Errorf("live definition validation source has duplicate object keys")
	}
	if _, err := validationmatrix.ReadHashBoundSeed(record); err != nil {
		return LiveDefinition{}, fmt.Errorf("live definition seed is stale or unsafe: %w", err)
	}
	definitions, err := compileLiveFixtureDefinitions(repoRoot, record, module)
	if err != nil {
		return LiveDefinition{}, err
	}
	comparator, err := deriveExactBytesComparator(module, definitions)
	if err != nil {
		return LiveDefinition{}, err
	}
	source := record.SourceSnapshot()
	digest := sha256.Sum256(source)
	policy := cloneLivePolicy(record.Live)
	if policy.Trust == nil {
		return LiveDefinition{}, fmt.Errorf("live definition seed trust is absent")
	}
	definition := LiveDefinition{
		SchemaVersion: LiveDefinitionSchemaVersion, ModuleID: record.ModuleID, ModuleRevision: module.Revision,
		ValidationSourceSHA256: hex.EncodeToString(digest[:]), Policy: policy, WingetRef: policy.Ref,
		SeedRepositoryPath: policy.Seed, SeedSHA256: policy.Trust.SeedSHA256, RunnerLabel: policy.RunnerLabel,
		PRTimeoutMinutes: policy.PRTimeoutMinutes, ScheduledTimeoutMinutes: policy.ScheduledTimeoutMinutes,
		Comparator: comparator, NonAuthorizing: true, MutationAuthorized: false,
	}
	if err := validateLiveDefinition(definition); err != nil {
		return LiveDefinition{}, err
	}
	return definition, nil
}

func cloneLivePolicy(policy validationmatrix.LivePolicy) validationmatrix.LivePolicy {
	copy := policy
	if policy.Trust != nil {
		trust := *policy.Trust
		copy.Trust = &trust
	}
	return copy
}

func compileLiveFixtureDefinitions(repoRoot string, record validationmatrix.ValidationRecord, module *modules.Module) (fixtureDefinitions, error) {
	if repoRoot == "" {
		return fixtureDefinitions{}, fmt.Errorf("live exact-bytes comparator requires repository fixture authority")
	}
	var matches []validationmatrix.Scenario
	for _, scenario := range record.Synthetic.Scenarios {
		if scenario.Mode == validationmatrix.ScenarioConfigRoundtripV1 {
			matches = append(matches, scenario)
		}
	}
	if len(matches) != 1 {
		return fixtureDefinitions{}, fmt.Errorf("live exact-bytes comparator requires exactly one schema-v1 config-roundtrip scenario")
	}
	if matches[0].Fixture.Type != validationmatrix.FixtureDeclarative {
		return fixtureDefinitions{}, fmt.Errorf("live exact-bytes comparator requires a hash-bound declarative fixture")
	}
	definitions, failure := compileFixtureDefinitionsAt(repoRoot, module, matches[0])
	if failure != nil {
		return fixtureDefinitions{}, fmt.Errorf("live exact-bytes comparator fixture is invalid: %s", failure)
	}
	return definitions, nil
}

func deriveExactBytesComparator(module *modules.Module, definitions fixtureDefinitions) (ExactBytesComparator, error) {
	if module.Capture == nil || len(module.Capture.Files) == 0 || len(module.Capture.RegistryKeys) != 0 || len(module.Capture.RegistryValues) != 0 {
		return ExactBytesComparator{}, fmt.Errorf("live exact-bytes comparator requires file-only capture mappings")
	}
	if len(module.Restore) == 0 || len(definitions.Entries) != len(module.Capture.Files) {
		return ExactBytesComparator{}, fmt.Errorf("live exact-bytes comparator requires copy restore mappings")
	}

	mappings := make([]ComparatorMapping, 0, len(module.Capture.Files))
	seen := make(map[string]struct{}, len(module.Capture.Files))
	for index, capture := range module.Capture.Files {
		definition := definitions.Entries[index]
		if definition.Coordinate != fmt.Sprintf("capture.files[%d]", index) {
			return ExactBytesComparator{}, fmt.Errorf("live capture.files[%d] lacks matching fixture classification", index)
		}
		if definition.Kind == fixtureKindDirectory {
			continue
		}
		if definition.Kind != fixtureKindFile {
			return ExactBytesComparator{}, fmt.Errorf("live capture.files[%d] has unsupported fixture kind", index)
		}
		identity, err := liveCaptureIdentity(capture.Dest)
		if err != nil {
			return ExactBytesComparator{}, fmt.Errorf("live capture.files[%d]: %w", index, err)
		}
		if !validLiveUserTemplate(capture.Source) {
			return ExactBytesComparator{}, fmt.Errorf("live capture.files[%d] source must be a static user-scoped template", index)
		}
		if _, duplicate := seen[identity]; duplicate {
			return ExactBytesComparator{}, fmt.Errorf("live capture.files[%d] duplicates identity %q", index, identity)
		}
		seen[identity] = struct{}{}
		var restores []modules.RestoreDef
		for _, restore := range module.Restore {
			if capture.Source == restore.Target && payloadDestination(restore.Source) == definition.Destination {
				restores = append(restores, restore)
			}
		}
		if len(restores) != 1 || restores[0].Type != "copy" || restores[0].Pattern != "" || len(restores[0].Exclude) != 0 {
			return ExactBytesComparator{}, fmt.Errorf("live exact-bytes mapping %q must have one plain file restore", identity)
		}
		if definition.Destination != filepath.ToSlash(capture.Dest) || !sameLiveTemplate(capture.Source, definition.Source) || !sameLiveTemplate(capture.Source, definition.Target) {
			return ExactBytesComparator{}, fmt.Errorf("live exact-bytes mapping %q is asymmetric", identity)
		}
		mappings = append(mappings, ComparatorMapping{Identity: identity, CaptureTemplate: capture.Source, RestoreTemplate: definition.Target, Optional: definition.Optional})
	}
	sort.Slice(mappings, func(left, right int) bool { return mappings[left].Identity < mappings[right].Identity })
	if len(mappings) == 0 {
		return ExactBytesComparator{}, fmt.Errorf("live exact-bytes comparator has no comparable mappings")
	}
	return ExactBytesComparator{Mappings: mappings, MinimumExistingMappings: 1}, nil
}

func liveCaptureIdentity(destination string) (string, error) {
	if !validLiveIdentity(destination) {
		return "", fmt.Errorf("destination must be a stable module-relative identity")
	}
	return destination, nil
}

func liveRestoreIdentity(source string) (string, error) {
	const prefix = "./payload/"
	if !strings.HasPrefix(source, prefix) {
		return "", fmt.Errorf("source must resolve from ./payload/")
	}
	identity := strings.TrimPrefix(source, prefix)
	if !validLiveIdentity(identity) {
		return "", fmt.Errorf("source must resolve to a stable module-relative identity")
	}
	return identity, nil
}

func validLiveIdentity(identity string) bool {
	if identity == "" || len(identity) > maxLiveStringBytes || strings.ContainsAny(identity, `\\:*?[]{}$%`) || strings.HasPrefix(identity, "/") || strings.HasSuffix(identity, "/") {
		return false
	}
	for _, segment := range strings.Split(identity, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validLiveUserTemplate(template string) bool {
	if template == "" || len(template) > maxLiveStringBytes || strings.ContainsAny(template, `*?[]{}$`) || strings.Contains(template, "..") || strings.HasSuffix(template, `\\`) || strings.Contains(template, "/") {
		return false
	}
	upper := strings.ToUpper(template)
	for _, root := range []string{"%APPDATA%\\", "%LOCALAPPDATA%\\", "%USERPROFILE%\\"} {
		if strings.HasPrefix(upper, root) && len(template) > len(root) {
			return true
		}
	}
	return false
}

func sameLiveTemplate(left, right string) bool {
	return strings.EqualFold(left, right)
}

// ValidateLiveResult enforces the deliberately narrow result contract before a
// later runner serializes or persists it.
func ValidateLiveResult(result LiveResult) error {
	if result.SchemaVersion != LiveResultSchemaVersion || !validLiveModuleID(result.ModuleID) || !lowerSHA256(result.ModuleRevision) || !lowerSHA256(result.ValidationSourceSHA256) || !lowerSHA256(result.DefinitionSHA256) || !knownLiveStatus(result.Status) {
		return fmt.Errorf("live result identity is invalid")
	}
	if result.PublicEvidenceEligible || len(result.ProvenProofLevels) != 0 {
		return fmt.Errorf("live result cannot claim public evidence or proof")
	}
	if len(result.Attempts) > maxLiveAttempts || !knownLiveFailureCategory(result.FailureCategory) {
		return fmt.Errorf("live result bounds are invalid")
	}
	if result.Status == LiveStatusPassed && len(result.Attempts) == 0 {
		return fmt.Errorf("live result pass is vacuous")
	}
	for index, attempt := range result.Attempts {
		if attempt.Number != index+1 || !knownLivePhase(attempt.Phase) || !knownLiveStatus(attempt.Status) || !knownLiveFailureCategory(attempt.FailureCategory) {
			return fmt.Errorf("live result attempt %d is invalid", index)
		}
		if !validLiveValue(attempt.Package.Ref) || !validLiveValue(attempt.Package.Version) || !knownObservationStatus(attempt.Package.Status) || len(attempt.Comparator) > maxLiveMappings {
			return fmt.Errorf("live result attempt %d package observation is invalid", index)
		}
		for _, outcome := range attempt.Comparator {
			if !validLiveIdentity(outcome.Identity) || !knownObservationStatus(outcome.Status) {
				return fmt.Errorf("live result comparator outcome is invalid")
			}
		}
	}
	return validateLiveResultState(result)
}

// CanonicalLiveDefinitionSHA256 returns the deterministic digest that binds a
// result to all non-authorizing preparation inputs.
func CanonicalLiveDefinitionSHA256(definition LiveDefinition) (string, error) {
	if err := validateLiveDefinition(definition); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ValidateLiveResultForDefinition binds a result to the exact compiled
// definition and the package/comparator identities it authorizes for
// observation only.
func ValidateLiveResultForDefinition(result LiveResult, definition LiveDefinition) error {
	if err := ValidateLiveResult(result); err != nil {
		return err
	}
	digest, err := CanonicalLiveDefinitionSHA256(definition)
	if err != nil {
		return err
	}
	if result.ModuleID != definition.ModuleID || result.ModuleRevision != definition.ModuleRevision || result.ValidationSourceSHA256 != definition.ValidationSourceSHA256 || result.DefinitionSHA256 != digest {
		return fmt.Errorf("live result definition binding is stale")
	}
	identities := make(map[string]struct{}, len(definition.Comparator.Mappings))
	for _, mapping := range definition.Comparator.Mappings {
		identities[mapping.Identity] = struct{}{}
	}
	for _, attempt := range result.Attempts {
		if attempt.Package.Ref != "" && attempt.Package.Ref != definition.WingetRef {
			return fmt.Errorf("live result package reference is not bound to definition")
		}
		seen := make(map[string]struct{}, len(attempt.Comparator))
		for _, outcome := range attempt.Comparator {
			if _, ok := identities[outcome.Identity]; !ok {
				return fmt.Errorf("live result comparator identity is not bound to definition")
			}
			if _, duplicate := seen[outcome.Identity]; duplicate {
				return fmt.Errorf("live result comparator identity is duplicated")
			}
			seen[outcome.Identity] = struct{}{}
		}
		if attempt.Status == LiveStatusPassed && len(seen) != len(identities) {
			return fmt.Errorf("passed live result comparator identities do not match definition")
		}
	}
	return nil
}

func validateLiveResultState(result LiveResult) error {
	switch result.Status {
	case LiveStatusPending:
		if len(result.Attempts) != 0 || result.FailureCategory != LiveFailureNone {
			return fmt.Errorf("pending live result must have no attempts or failure")
		}
		return nil
	case LiveStatusPassed:
		if len(result.Attempts) == 0 || result.FailureCategory != LiveFailureNone {
			return fmt.Errorf("passed live result must have attempts and no top-level failure")
		}
		for index, attempt := range result.Attempts {
			if index == len(result.Attempts)-1 {
				if attempt.Status != LiveStatusPassed || attempt.FailureCategory != LiveFailureNone {
					return fmt.Errorf("passed live result final attempt must pass without failure")
				}
				if err := validatePassedLiveAttempt(attempt); err != nil {
					return err
				}
				continue
			}
			if attempt.Status != LiveStatusFailed || attempt.FailureCategory == LiveFailureNone {
				return fmt.Errorf("passed live result retry attempt %d must fail with a category", index)
			}
		}
		return nil
	case LiveStatusFailed:
		if len(result.Attempts) == 0 || result.FailureCategory == LiveFailureNone {
			return fmt.Errorf("failed live result requires attempts and a top-level failure")
		}
		for index, attempt := range result.Attempts {
			if attempt.Status != LiveStatusFailed || attempt.FailureCategory == LiveFailureNone {
				return fmt.Errorf("failed live result attempt %d must fail with a category", index)
			}
		}
		final := result.Attempts[len(result.Attempts)-1]
		if final.FailureCategory != result.FailureCategory {
			return fmt.Errorf("failed live result final failure must match top-level failure")
		}
		return nil
	default:
		return fmt.Errorf("live result status is invalid")
	}
}

func validatePassedLiveAttempt(attempt LiveAttempt) error {
	if attempt.Package.Ref == "" || attempt.Package.Status != "passed" || len(attempt.Comparator) == 0 {
		return fmt.Errorf("passed live result attempt must have passed package and comparator observations")
	}
	seen := make(map[string]struct{}, len(attempt.Comparator))
	for _, outcome := range attempt.Comparator {
		if outcome.Status != "passed" {
			return fmt.Errorf("passed live result comparator outcome must pass")
		}
		if _, duplicate := seen[outcome.Identity]; duplicate {
			return fmt.Errorf("passed live result comparator outcomes must be unique")
		}
		seen[outcome.Identity] = struct{}{}
	}
	return nil
}

// ValidateLiveRequest rejects malformed or authorizing work before a runner can
// observe it. Compilation returns a definition that already satisfies it.
func ValidateLiveRequest(request LiveRequest) error {
	if request.SchemaVersion != LiveDefinitionSchemaVersion || request.MaxAttempts < 1 || request.MaxAttempts > maxLiveAttempts {
		return fmt.Errorf("live request bounds are invalid")
	}
	return validateLiveDefinition(request.Definition)
}

func validateLiveDefinition(definition LiveDefinition) error {
	if definition.SchemaVersion != LiveDefinitionSchemaVersion || !validLiveModuleID(definition.ModuleID) || !lowerSHA256(definition.ModuleRevision) || !lowerSHA256(definition.ValidationSourceSHA256) || !definition.NonAuthorizing || definition.MutationAuthorized {
		return fmt.Errorf("live definition identity or authority is invalid")
	}
	policy := definition.Policy
	if policy.Mode != validationmatrix.LiveCandidate || policy.Driver != "winget" || !validLiveValue(policy.Ref) || !validLiveRepositoryPath(policy.Seed) || policy.Comparator != validationmatrix.ComparatorExactBytes || !validLiveValue(policy.RunnerLabel) || policy.Trust == nil || !lowerSHA256(policy.Trust.SeedSHA256) || policy.Trust.ComparatorSHA256 != "" || !boundedLivePolicy(policy) {
		return fmt.Errorf("live definition policy is invalid")
	}
	if definition.WingetRef != policy.Ref || definition.SeedRepositoryPath != policy.Seed || definition.SeedSHA256 != policy.Trust.SeedSHA256 || definition.RunnerLabel != policy.RunnerLabel || definition.PRTimeoutMinutes != policy.PRTimeoutMinutes || definition.ScheduledTimeoutMinutes != policy.ScheduledTimeoutMinutes {
		return fmt.Errorf("live definition policy binding is invalid")
	}
	if policy.PRTimeoutMinutes < 1 || policy.PRTimeoutMinutes > 25 || policy.ScheduledTimeoutMinutes < 1 || policy.ScheduledTimeoutMinutes > 45 || len(definition.Comparator.Mappings) == 0 || len(definition.Comparator.Mappings) > maxLiveMappings || definition.Comparator.MinimumExistingMappings < 1 || definition.Comparator.MinimumExistingMappings > len(definition.Comparator.Mappings) {
		return fmt.Errorf("live definition comparator bounds are invalid")
	}
	seen := make(map[string]struct{}, len(definition.Comparator.Mappings))
	for _, mapping := range definition.Comparator.Mappings {
		if !validLiveIdentity(mapping.Identity) || !validLiveUserTemplate(mapping.CaptureTemplate) || !validLiveUserTemplate(mapping.RestoreTemplate) || !sameLiveTemplate(mapping.CaptureTemplate, mapping.RestoreTemplate) {
			return fmt.Errorf("live definition comparator mapping is invalid")
		}
		if _, duplicate := seen[mapping.Identity]; duplicate {
			return fmt.Errorf("live definition comparator mapping is duplicated")
		}
		seen[mapping.Identity] = struct{}{}
	}
	return nil
}

func boundedLivePolicy(policy validationmatrix.LivePolicy) bool {
	return len(policy.Driver) <= maxLiveStringBytes && len(policy.Ref) <= maxLiveStringBytes && len(policy.Seed) <= maxLiveStringBytes && len(policy.Comparator) <= maxLiveStringBytes && len(policy.RunnerLabel) <= maxLiveStringBytes && len(policy.ReasonCode) <= maxLiveStringBytes && len(policy.Explanation) <= maxLiveStringBytes
}

func validLiveModuleID(value string) bool {
	return validLiveValue(value) && strings.HasPrefix(value, "apps.")
}

func validLiveValue(value string) bool {
	return len(value) <= maxLiveStringBytes && !strings.ContainsAny(value, "\\/:=%\r\n") && strings.TrimSpace(value) == value
}

func validLiveRepositoryPath(value string) bool {
	return value != "" && len(value) <= maxLiveStringBytes && !strings.ContainsAny(value, `\\:*?[]{}$%`) && !strings.Contains(value, "/") && filepath.Base(value) == value && value != "." && value != ".."
}

func lowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func knownLiveStatus(status LiveStatus) bool {
	switch status {
	case LiveStatusPending, LiveStatusPassed, LiveStatusFailed:
		return true
	default:
		return false
	}
}

func knownLivePhase(phase LivePhase) bool {
	switch phase {
	case LivePhasePreparation, LivePhasePackage, LivePhaseSeed, LivePhaseCapture, LivePhaseRestore, LivePhaseCompare:
		return true
	default:
		return false
	}
}

func knownLiveFailureCategory(category LiveFailureCategory) bool {
	switch category {
	case LiveFailureNone, LiveFailureDefinition, LiveFailureSeed, LiveFailurePackage, LiveFailureCapture, LiveFailureRestore, LiveFailureComparison, LiveFailureEnvironment:
		return true
	default:
		return false
	}
}

func knownObservationStatus(status string) bool {
	switch status {
	case "", "pending", "passed", "failed", "skipped", "not-observed":
		return true
	default:
		return false
	}
}
