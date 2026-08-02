// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

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
	case hasCapture:
		want = ScenarioCaptureContract
	case hasRestore:
		want = ScenarioRestoreContract
	default:
		if !hasProductionAppReference(mod) || len(mod.Verify) == 0 {
			return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "install-only module requires an app reference and production verifier")
		}
	}

	if len(record.Synthetic.Scenarios) != 1 || record.Synthetic.Scenarios[0].Mode != want {
		return validationError(CodeInvalidClassification, record.ModuleID, record.FilePath, "schema-v1 module requires exactly one %q scenario", want)
	}
	return nil
}

func hasProductionAppReference(mod *modules.Module) bool {
	if mod == nil {
		return false
	}
	referenceFamilies := [][]string{
		mod.Matches.Winget,
		mod.Matches.Chocolatey,
		mod.Matches.Exe,
		mod.Matches.UninstallDisplayName,
		mod.Matches.PathExists,
	}
	for _, references := range referenceFamilies {
		for _, reference := range references {
			if strings.TrimSpace(reference) != "" {
				return true
			}
		}
	}
	return false
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
