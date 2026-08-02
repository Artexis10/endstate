// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"regexp"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

var (
	lowerSHA256Pattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	windowsDrivePattern = regexp.MustCompile(`^[A-Za-z]:`)
)

var knownAssertionNames = map[string]struct{}{
	AssertionCaptured: {}, AssertionPayload: {}, AssertionProvenance: {},
	AssertionRewrittenRestore: {}, AssertionContent: {}, AssertionRebuild: {},
	AssertionVerify: {}, AssertionNestedSummary: {}, AssertionRevert: {},
	AssertionGeneration: {}, AssertionValidation: {}, AssertionMigration: {},
	AssertionRestored: {}, AssertionAppReferences: {},
}

func validateFixture(record *ValidationRecord, scenario *Scenario) error {
	switch scenario.Fixture.Type {
	case FixtureAuto:
		if scenario.Fixture.Path != "" || scenario.Fixture.SHA256 != "" {
			return validationError(CodeInvalidFixture, record.ModuleID, record.FilePath, "auto fixture for scenario %q forbids path and sha256", scenario.ID)
		}
	case FixtureDeclarative:
		if !isPortableRepositoryRelativePath(scenario.Fixture.Path) {
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

// isPortableRepositoryRelativePath validates repository paths independently
// of the host GOOS so Windows roots cannot be accepted on Unix, or vice versa.
func isPortableRepositoryRelativePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || windowsDrivePattern.MatchString(trimmed) || strings.HasPrefix(trimmed, `/`) || strings.HasPrefix(trimmed, `\`) {
		return false
	}
	normalized := strings.ReplaceAll(trimmed, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
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
	if (scenario.Mode == ScenarioConfigGenerationV2 || scenario.Mode == ScenarioConfigMigrationV2) && len(mod.Verify) == 0 && scenario.MinimumAssertions[AssertionVerify] > 0 {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q cannot require app-verifier assertions when the production module declares no top-level verifier", scenario.ID)
	}
	if err := validateSchemaV2ProductionValidation(record, mod, scenario); err != nil {
		return err
	}
	required := requiredAssertions(scenario.Mode, len(mod.Verify) > 0)
	requiredSet := make(map[string]struct{}, len(required))
	for _, name := range required {
		requiredSet[name] = struct{}{}
		if scenario.MinimumAssertions[name] <= 0 {
			return validationError(CodeMissingAssertionMinimum, record.ModuleID, record.FilePath, "scenario %q requires non-zero %s assertions", scenario.ID, name)
		}
	}
	for name := range scenario.MinimumAssertions {
		if _, required := requiredSet[name]; !required {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q assertion %q is not required for this scenario", scenario.ID, name)
		}
	}
	return nil
}

func requiredAssertions(kind ScenarioKind, hasProductionVerifier bool) []string {
	roundtrip := []string{
		AssertionCaptured, AssertionPayload, AssertionProvenance, AssertionRewrittenRestore,
		AssertionContent, AssertionRebuild, AssertionNestedSummary, AssertionRevert,
	}
	switch kind {
	case ScenarioConfigRoundtripV1:
		return append(roundtrip, AssertionVerify)
	case ScenarioConfigGenerationV2:
		required := append(roundtrip, AssertionGeneration, AssertionValidation)
		if hasProductionVerifier {
			required = append(required, AssertionVerify)
		}
		return required
	case ScenarioConfigMigrationV2:
		required := append(roundtrip, AssertionGeneration, AssertionValidation, AssertionMigration)
		if hasProductionVerifier {
			required = append(required, AssertionVerify)
		}
		return required
	case ScenarioCaptureContract:
		return []string{AssertionCaptured, AssertionPayload, AssertionProvenance, AssertionContent}
	case ScenarioRestoreContract:
		required := []string{AssertionRestored, AssertionContent, AssertionNestedSummary, AssertionRevert}
		if hasProductionVerifier {
			required = append(required, AssertionVerify)
		}
		return required
	case ScenarioInstallContract:
		return []string{AssertionAppReferences, AssertionVerify}
	default:
		return nil
	}
}

func validateSchemaV2ProductionValidation(record *ValidationRecord, mod *modules.Module, scenario *Scenario) error {
	if (scenario.Mode != ScenarioConfigGenerationV2 && scenario.Mode != ScenarioConfigMigrationV2) || scenario.Expected == nil || mod == nil || mod.Config == nil {
		return nil
	}
	expected := scenario.Expected
	for setIndex := range mod.Config.Sets {
		set := &mod.Config.Sets[setIndex]
		if set.ID != expected.ConfigSetID {
			continue
		}
		generationID := expected.GenerationID
		if scenario.Mode == ScenarioConfigMigrationV2 {
			generationID = expected.MigrationTo
		}
		for generationIndex := range set.Generations {
			generation := &set.Generations[generationIndex]
			if generation.ID == generationID && len(generation.Validate) == 0 {
				return validationError(CodeMissingProductionValidation, record.ModuleID, record.FilePath, "scenario %q target generation %q has no production validation", scenario.ID, generationID)
			}
		}
		if scenario.Mode == ScenarioConfigMigrationV2 {
			for edgeIndex := range set.Migrations {
				edge := &set.Migrations[edgeIndex]
				if edge.From == expected.MigrationFrom && edge.To == expected.MigrationTo && len(edge.Validate) == 0 {
					return validationError(CodeMissingProductionValidation, record.ModuleID, record.FilePath, "scenario %q migration %q -> %q has no production edge validation", scenario.ID, edge.From, edge.To)
				}
			}
		}
		return nil
	}
	return nil
}

func validateExpected(record *ValidationRecord, mod *modules.Module, scenario *Scenario) error {
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
	requiredFields := []struct {
		name  string
		value string
	}{
		{name: "configSetId", value: expected.ConfigSetID},
		{name: "generationId", value: expected.GenerationID},
	}
	for _, field := range requiredFields {
		if strings.TrimSpace(field.value) == "" {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q expected.%s is required", scenario.ID, field.name)
		}
	}
	if err := validateExpectedIdentity(record, mod, scenario); err != nil {
		return err
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

func validateExpectedIdentity(record *ValidationRecord, mod *modules.Module, scenario *Scenario) error {
	expected := scenario.Expected
	switch expected.IdentityMode {
	case IdentityLiteral:
		if strings.TrimSpace(expected.CaptureID) == "" || strings.TrimSpace(expected.InstanceID) == "" {
			return validationError(CodeInvalidSchemaV2Identity, record.ModuleID, record.FilePath, "scenario %q literal identity requires captureId and instanceId", scenario.ID)
		}
		if expected.detectorIDSet {
			return validationError(CodeInvalidSchemaV2Identity, record.ModuleID, record.FilePath, "scenario %q literal identity forbids detectorId", scenario.ID)
		}
	case IdentityDerivedFromFixture:
		if strings.TrimSpace(expected.DetectorID) == "" {
			return validationError(CodeInvalidSchemaV2Identity, record.ModuleID, record.FilePath, "scenario %q derived identity requires detectorId", scenario.ID)
		}
		if expected.captureIDSet || expected.instanceIDSet {
			return validationError(CodeInvalidSchemaV2Identity, record.ModuleID, record.FilePath, "scenario %q derived identity forbids captureId and instanceId", scenario.ID)
		}
		if !moduleDeclaresDetector(mod, expected.DetectorID) {
			return validationError(CodeInvalidSchemaV2Identity, record.ModuleID, record.FilePath, "scenario %q detectorId %q is not declared by the production module", scenario.ID, expected.DetectorID)
		}
	default:
		return validationError(CodeInvalidSchemaV2Identity, record.ModuleID, record.FilePath, "scenario %q requires identityMode literal or derived-from-fixture", scenario.ID)
	}
	return nil
}

func moduleDeclaresDetector(mod *modules.Module, detectorID string) bool {
	if mod == nil || mod.Config == nil {
		return false
	}
	for _, detector := range mod.Config.InstanceDetectors {
		if detector.ID == detectorID {
			return true
		}
	}
	return false
}
