// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

var (
	stableKebabPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	lowerSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	datePattern        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

var knownAssertionNames = map[string]struct{}{
	AssertionCaptured: {}, AssertionPayload: {}, AssertionProvenance: {},
	AssertionRewrittenRestore: {}, AssertionContent: {}, AssertionRebuild: {},
	AssertionVerify: {}, AssertionNestedSummary: {}, AssertionRevert: {},
	AssertionGeneration: {}, AssertionValidation: {}, AssertionMigration: {},
	AssertionRestored: {}, AssertionAppReferences: {},
}

func validateRecord(record *ValidationRecord, mod *modules.Module, now time.Time) error {
	if record.SchemaVersion != 1 {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "schemaVersion must be 1")
	}
	if strings.TrimSpace(record.ModuleID) == "" {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "moduleId is required")
	}
	if !lowerSHA256Pattern.MatchString(record.ModuleRevision) {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "moduleRevision must be lowercase SHA-256")
	}
	if record.ModuleRevision != mod.Revision {
		return validationError(CodeStaleSidecar, record.ModuleID, record.FilePath, "moduleRevision %q does not match current revision %q", record.ModuleRevision, mod.Revision)
	}
	if len(record.Synthetic.Scenarios) == 0 {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "synthetic.scenarios must not be empty")
	}

	seenScenarioIDs := make(map[string]struct{}, len(record.Synthetic.Scenarios))
	for index := range record.Synthetic.Scenarios {
		scenario := &record.Synthetic.Scenarios[index]
		if !stableKebabPattern.MatchString(scenario.ID) {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario[%d].id must be stable lowercase kebab-case", index)
		}
		if _, exists := seenScenarioIDs[scenario.ID]; exists {
			return validationError(CodeDuplicateScenario, record.ModuleID, record.FilePath, "duplicate scenario id %q", scenario.ID)
		}
		seenScenarioIDs[scenario.ID] = struct{}{}
		if !knownScenarioKind(scenario.Mode) {
			return validationError(CodeUnknownScenarioKind, record.ModuleID, record.FilePath, "scenario %q uses unknown mode %q", scenario.ID, scenario.Mode)
		}
		if err := validateFixture(record, scenario); err != nil {
			return err
		}
		if scenario.TimeoutSeconds <= 0 || scenario.TimeoutSeconds > 15*60 {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q timeoutSeconds must be between 1 and 900", scenario.ID)
		}
		if err := validateAssertions(record, mod, scenario); err != nil {
			return err
		}
		if err := validateExpected(record, scenario); err != nil {
			return err
		}
	}

	if err := validateClassification(record, mod); err != nil {
		return err
	}
	if err := validateLivePolicy(record); err != nil {
		return err
	}
	for index := range record.Quarantines {
		if err := validateQuarantine(record, index, &record.Quarantines[index], now); err != nil {
			return err
		}
	}
	return nil
}

func knownScenarioKind(kind ScenarioKind) bool {
	switch kind {
	case ScenarioConfigRoundtripV1, ScenarioConfigGenerationV2, ScenarioConfigMigrationV2,
		ScenarioCaptureContract, ScenarioRestoreContract, ScenarioInstallContract:
		return true
	default:
		return false
	}
}

func validateFixture(record *ValidationRecord, scenario *Scenario) error {
	switch scenario.Fixture.Type {
	case FixtureAuto:
		if scenario.Fixture.Path != "" || scenario.Fixture.SHA256 != "" {
			return validationError(CodeInvalidFixture, record.ModuleID, record.FilePath, "auto fixture for scenario %q forbids path and sha256", scenario.ID)
		}
	case FixtureDeclarative:
		if !repositoryRelativePath(scenario.Fixture.Path) {
			return validationError(CodeInvalidFixture, record.ModuleID, record.FilePath, "declarative fixture for scenario %q requires a contained repository-relative path", scenario.ID)
		}
		if !lowerSHA256Pattern.MatchString(scenario.Fixture.SHA256) {
			return validationError(CodeInvalidFixture, record.ModuleID, record.FilePath, "declarative fixture for scenario %q requires a lowercase SHA-256", scenario.ID)
		}
	default:
		return validationError(CodeInvalidFixture, record.ModuleID, record.FilePath, "scenario %q uses unknown fixture type %q", scenario.ID, scenario.Fixture.Type)
	}
	return nil
}

func repositoryRelativePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || filepath.IsAbs(trimmed) || filepath.VolumeName(trimmed) != "" || strings.HasPrefix(trimmed, `/`) || strings.HasPrefix(trimmed, `\`) {
		return false
	}
	normalized := strings.ReplaceAll(trimmed, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return true
}

func validateAssertions(record *ValidationRecord, mod *modules.Module, scenario *Scenario) error {
	for name, minimum := range scenario.MinimumAssertions {
		if _, known := knownAssertionNames[name]; !known {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q has unknown assertion minimum %q", scenario.ID, name)
		}
		if minimum < 0 {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q assertion %q cannot be negative", scenario.ID, name)
		}
	}
	required := []string{}
	switch scenario.Mode {
	case ScenarioConfigRoundtripV1:
		required = []string{AssertionCaptured, AssertionPayload, AssertionProvenance, AssertionRewrittenRestore, AssertionContent, AssertionRebuild, AssertionVerify, AssertionNestedSummary, AssertionRevert}
	case ScenarioConfigGenerationV2:
		required = []string{AssertionCaptured, AssertionPayload, AssertionProvenance, AssertionRewrittenRestore, AssertionContent, AssertionRebuild, AssertionVerify, AssertionNestedSummary, AssertionRevert, AssertionGeneration, AssertionValidation}
	case ScenarioConfigMigrationV2:
		required = []string{AssertionCaptured, AssertionPayload, AssertionProvenance, AssertionRewrittenRestore, AssertionContent, AssertionRebuild, AssertionVerify, AssertionNestedSummary, AssertionRevert, AssertionGeneration, AssertionValidation, AssertionMigration}
	case ScenarioCaptureContract:
		required = []string{AssertionCaptured, AssertionPayload, AssertionProvenance, AssertionContent}
	case ScenarioRestoreContract:
		required = []string{AssertionRestored, AssertionContent, AssertionNestedSummary, AssertionRevert}
		if len(mod.Verify) > 0 {
			required = append(required, AssertionVerify)
		}
	case ScenarioInstallContract:
		required = []string{AssertionAppReferences, AssertionVerify}
	}
	for _, name := range required {
		if scenario.MinimumAssertions[name] <= 0 {
			return validationError(CodeMissingAssertionMinimum, record.ModuleID, record.FilePath, "scenario %q requires non-zero %s assertions", scenario.ID, name)
		}
	}
	return nil
}

func validateExpected(record *ValidationRecord, scenario *Scenario) error {
	isV2 := scenario.Mode == ScenarioConfigGenerationV2 || scenario.Mode == ScenarioConfigMigrationV2
	if !isV2 {
		if scenario.Expected != nil {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q cannot declare schema-v2 expected fields", scenario.ID)
		}
		return nil
	}
	if scenario.Expected == nil {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q requires schema-v2 expected fields", scenario.ID)
	}
	expected := scenario.Expected
	for name, value := range map[string]string{
		"captureId": expected.CaptureID, "configSetId": expected.ConfigSetID,
		"instanceId": expected.InstanceID, "generationId": expected.GenerationID,
	} {
		if strings.TrimSpace(value) == "" {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q expected.%s is required", scenario.ID, name)
		}
	}
	if !lowerSHA256Pattern.MatchString(expected.Fingerprint) {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q expected.fingerprint must be lowercase SHA-256", scenario.ID)
	}
	if scenario.Mode == ScenarioConfigMigrationV2 {
		if expected.MigrationFrom == "" || expected.MigrationTo == "" || expected.MigrationFrom == expected.MigrationTo {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q requires an exact non-self migration edge", scenario.ID)
		}
	} else if expected.MigrationFrom != "" || expected.MigrationTo != "" {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "generation scenario %q cannot declare a migration edge", scenario.ID)
	}
	return nil
}

func validateClassification(record *ValidationRecord, mod *modules.Module) error {
	if mod.EffectiveSchemaVersion() == 2 {
		return validateSchemaV2Classification(record, mod)
	}
	return validateSchemaV1Classification(record, mod)
}

func validateSchemaV1Classification(record *ValidationRecord, mod *modules.Module) error {
	hasCapture := moduleHasCapture(mod)
	hasRestore := len(mod.Restore) > 0
	fullyRestorable := hasCapture && hasRestore && everyCaptureHasRestore(mod)

	want := ScenarioInstallContract
	switch {
	case fullyRestorable:
		want = ScenarioConfigRoundtripV1
		if len(mod.Verify) == 0 {
			return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "config roundtrip requires a production verifier")
		}
	case hasCapture:
		want = ScenarioCaptureContract
	case hasRestore:
		want = ScenarioRestoreContract
	default:
		if len(mod.Matches.Winget)+len(mod.Matches.Chocolatey) == 0 || len(mod.Verify) == 0 {
			return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "install-only module requires an app reference and production verifier")
		}
	}

	if len(record.Synthetic.Scenarios) != 1 || record.Synthetic.Scenarios[0].Mode != want {
		return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "schema-v1 module requires exactly one %q scenario", want)
	}
	return nil
}

func moduleHasCapture(mod *modules.Module) bool {
	return mod.Capture != nil && (len(mod.Capture.Files) > 0 || len(mod.Capture.RegistryKeys) > 0 || len(mod.Capture.RegistryValues) > 0)
}

func everyCaptureHasRestore(mod *modules.Module) bool {
	sources := make(map[string]struct{}, len(mod.Restore))
	registryValues := make(map[string]struct{}, len(mod.Restore))
	for _, restore := range mod.Restore {
		if restore.Source != "" && executableRestoreType(restore.Type) {
			sources[portableRestoreSource(restore.Source)] = struct{}{}
		}
		if restore.Type == "registry-set" {
			registryValues[strings.ToLower(restore.Key)+"\x00"+strings.ToLower(restore.ValueName)] = struct{}{}
		}
	}
	for _, capture := range mod.Capture.Files {
		if _, ok := sources[portableCaptureDestination(capture.Dest)]; !ok {
			return false
		}
	}
	for _, capture := range mod.Capture.RegistryKeys {
		if _, ok := sources[portableCaptureDestination(capture.Dest)]; !ok {
			return false
		}
	}
	for _, capture := range mod.Capture.RegistryValues {
		key := strings.ToLower(capture.Key) + "\x00" + strings.ToLower(capture.ValueName)
		if _, ok := registryValues[key]; !ok {
			return false
		}
	}
	return true
}

func executableRestoreType(restoreType string) bool {
	switch restoreType {
	case "copy", "merge-json", "merge-ini", "append", "delete-glob", "registry-import", "registry-set":
		return true
	default:
		return false
	}
}

func portableRestoreSource(source string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(source), `\`, "/")
	normalized = strings.TrimPrefix(normalized, "./")
	normalized = strings.TrimPrefix(normalized, "payload/")
	return strings.TrimPrefix(normalized, "./")
}

func portableCaptureDestination(destination string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(destination), `\`, "/")
	return strings.TrimPrefix(normalized, "./")
}

func validateSchemaV2Classification(record *ValidationRecord, mod *modules.Module) error {
	if mod.Config == nil {
		return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "schema-v2 module has no config declaration")
	}
	requiredGenerations := make(map[string]struct{})
	requiredMigrations := make(map[string]struct{})
	for _, set := range mod.Config.Sets {
		for _, generation := range set.Generations {
			fingerprints := append([]string{generation.Fingerprint}, generation.AcceptsSourceFingerprints...)
			for _, fingerprint := range fingerprints {
				requiredGenerations[v2GenerationKey(set.ID, generation.ID, fingerprint)] = struct{}{}
			}
		}
		for _, migration := range set.Migrations {
			requiredMigrations[v2MigrationKey(set.ID, migration.From, migration.To)] = struct{}{}
		}
	}

	seenGenerations := make(map[string]struct{}, len(requiredGenerations))
	seenMigrations := make(map[string]struct{}, len(requiredMigrations))
	for _, scenario := range record.Synthetic.Scenarios {
		switch scenario.Mode {
		case ScenarioConfigGenerationV2:
			key := v2GenerationKey(scenario.Expected.ConfigSetID, scenario.Expected.GenerationID, scenario.Expected.Fingerprint)
			if _, required := requiredGenerations[key]; !required {
				return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "scenario %q names an undeclared generation/fingerprint alternative", scenario.ID)
			}
			if _, duplicate := seenGenerations[key]; duplicate {
				return validationError(CodeDuplicateScenario, record.ModuleID, record.FilePath, "generation/fingerprint alternative is declared more than once")
			}
			seenGenerations[key] = struct{}{}
		case ScenarioConfigMigrationV2:
			key := v2MigrationKey(scenario.Expected.ConfigSetID, scenario.Expected.MigrationFrom, scenario.Expected.MigrationTo)
			if _, required := requiredMigrations[key]; !required {
				return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "scenario %q names an undeclared migration edge", scenario.ID)
			}
			if scenario.Expected.GenerationID != scenario.Expected.MigrationTo {
				return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "migration scenario %q generationId must identify the target generation", scenario.ID)
			}
			targetFingerprint := generationFingerprint(mod, scenario.Expected.ConfigSetID, scenario.Expected.MigrationTo)
			if scenario.Expected.Fingerprint != targetFingerprint {
				return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "migration scenario %q fingerprint must identify the target generation", scenario.ID)
			}
			if _, duplicate := seenMigrations[key]; duplicate {
				return validationError(CodeDuplicateScenario, record.ModuleID, record.FilePath, "migration edge is declared more than once")
			}
			seenMigrations[key] = struct{}{}
		default:
			return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "schema-v2 module cannot use %q", scenario.Mode)
		}
	}
	if len(seenGenerations) != len(requiredGenerations) || len(seenMigrations) != len(requiredMigrations) {
		return validationError(CodeMissingV2Scenario, record.ModuleID, record.FilePath, "schema-v2 sidecar covers %d/%d generation alternatives and %d/%d migration edges", len(seenGenerations), len(requiredGenerations), len(seenMigrations), len(requiredMigrations))
	}
	return nil
}

func generationFingerprint(mod *modules.Module, setID, generationID string) string {
	for _, set := range mod.Config.Sets {
		if set.ID != setID {
			continue
		}
		for _, generation := range set.Generations {
			if generation.ID == generationID {
				return generation.Fingerprint
			}
		}
	}
	return ""
}

func v2GenerationKey(setID, generationID, fingerprint string) string {
	return fmt.Sprintf("%s\x00%s\x00%s", setID, generationID, fingerprint)
}

func v2MigrationKey(setID, from, to string) string {
	return fmt.Sprintf("%s\x00%s\x00%s", setID, from, to)
}

func validateLivePolicy(record *ValidationRecord) error {
	live := &record.Live
	if !knownLiveMode(live.Mode) {
		return validationError(CodeUnknownLiveMode, record.ModuleID, record.FilePath, "unknown live mode %q", live.Mode)
	}
	if live.Mode != LiveHosted {
		if !stableKebabPattern.MatchString(live.ReasonCode) || strings.TrimSpace(live.Explanation) == "" {
			return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "non-hosted live mode requires a kebab-case reasonCode and explanation")
		}
		if live.Driver != "" || live.Ref != "" || live.Seed != "" || live.Comparator != "" || live.ProofMode != "" ||
			live.PRTimeoutMinutes != 0 || live.ScheduledTimeoutMinutes != 0 || live.RunnerLabel != "" || live.Trust != nil {
			return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "non-hosted live mode cannot carry trusted execution policy")
		}
		return nil
	}

	if strings.TrimSpace(live.Driver) == "" || strings.TrimSpace(live.Ref) == "" || strings.TrimSpace(live.RunnerLabel) == "" {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted live policy requires driver, ref, and runnerLabel")
	}
	if live.ProofMode != ProofLiveInstall && live.ProofMode != ProofLiveConfigRoundtrip {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted proofMode must be live-install or live-config-roundtrip")
	}
	if live.PRTimeoutMinutes <= 0 || live.PRTimeoutMinutes > 25 || live.ScheduledTimeoutMinutes <= 0 || live.ScheduledTimeoutMinutes > 45 {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted timeouts must fit PR 25-minute and scheduled 45-minute caps")
	}
	if live.ReasonCode != "" || live.Explanation != "" {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted live policy cannot carry a non-hosted reason")
	}
	if live.ProofMode == ProofLiveConfigRoundtrip && (live.Seed == "" || live.Comparator == "") {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "live-config-roundtrip requires seed and comparator")
	}
	if err := validateNamedTrustHash("seed", live.Seed, trustSeedHash(live.Trust)); err != nil {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "%v", err)
	}
	if err := validateNamedTrustHash("comparator", live.Comparator, trustComparatorHash(live.Trust)); err != nil {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "%v", err)
	}
	return nil
}

func knownLiveMode(mode LiveMode) bool {
	switch mode {
	case LiveHosted, LiveCandidate, LiveBlocked, LiveLab, LiveManual, LiveNotApplicable:
		return true
	default:
		return false
	}
}

func trustSeedHash(trust *TrustHashes) string {
	if trust == nil {
		return ""
	}
	return trust.SeedSHA256
}

func trustComparatorHash(trust *TrustHashes) string {
	if trust == nil {
		return ""
	}
	return trust.ComparatorSHA256
}

func validateNamedTrustHash(name, input, hash string) error {
	if input == "" && hash != "" {
		return fmt.Errorf("%sSha256 cannot be declared without %s", name, name)
	}
	if input != "" && !lowerSHA256Pattern.MatchString(hash) {
		return fmt.Errorf("named %s requires a lowercase SHA-256 trust hash", name)
	}
	return nil
}

func validateQuarantine(record *ValidationRecord, index int, quarantine *Quarantine, now time.Time) error {
	if !knownProofLevel(quarantine.ProofLevel) || strings.TrimSpace(quarantine.OS) == "" || strings.TrimSpace(quarantine.RunnerImage) == "" ||
		!lowerSHA256Pattern.MatchString(quarantine.FailureFingerprint) || strings.TrimSpace(quarantine.Owner) == "" ||
		!stableKebabPattern.MatchString(quarantine.ReasonCode) {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] must be scoped, owned, fingerprinted, and reason-coded", index)
	}
	issue, err := url.Parse(quarantine.IssueURL)
	if err != nil || issue.Scheme != "https" || issue.Host == "" || issue.Path == "" {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] requires an absolute HTTPS issueUrl", index)
	}
	if !datePattern.MatchString(quarantine.ExpiresOn) {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] expiresOn must be strict YYYY-MM-DD", index)
	}
	expires, err := time.Parse("2006-01-02", quarantine.ExpiresOn)
	if err != nil || !expires.After(now.UTC()) {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] is expired or malformed", index)
	}
	return nil
}

func knownProofLevel(proof ProofLevel) bool {
	switch proof {
	case ProofCatalog, ProofEngineContract, ProofConfigRoundtripV1, ProofConfigRoundtripV2, ProofLiveInstall, ProofLiveConfigRoundtrip:
		return true
	default:
		return false
	}
}
