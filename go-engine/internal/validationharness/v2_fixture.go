// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Artexis10/endstate/go-engine/internal/configdoc"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type v2FixtureFormat string

const (
	v2FormatJSON v2FixtureFormat = "json"
	v2FormatINI  v2FixtureFormat = "ini"
	v2FormatFile v2FixtureFormat = "file"
	v2FormatTree v2FixtureFormat = "tree"
)

type v2FixtureDefinition struct {
	SchemaVersion   int              `json:"schemaVersion"`
	SourceVersion   string           `json:"sourceVersion"`
	TargetVersion   string           `json:"targetVersion,omitempty"`
	InstanceLocator string           `json:"instanceLocator,omitempty"`
	Entries         []v2FixtureEntry `json:"entries"`
}

type v2FixtureEntry struct {
	CaptureCoordinate string            `json:"captureCoordinate"`
	RestoreCoordinate string            `json:"restoreCoordinate"`
	Kind              fixtureKind       `json:"kind"`
	Format            v2FixtureFormat   `json:"format"`
	Members           []v2FixtureMember `json:"members,omitempty"`
}

type v2FixtureMember struct {
	Path   string          `json:"path"`
	Format v2FixtureFormat `json:"format"`
}

type v2CompiledFixture struct {
	Definition v2FixtureDefinition
	Detector   modules.InstanceDetectorDef
	Set        modules.ConfigSetDef
	Generation modules.GenerationDef
	Entries    []v2CompiledEntry
}

type v2CompiledEntry struct {
	Shape       v2FixtureEntry
	Capture     modules.CaptureFile
	Restore     modules.RestoreDef
	Validations []modules.ValidationDef
	CaptureOnly []string
	RestoreOnly []string
	Overlapping []string
}

func decodeV2Fixture(raw []byte) (v2FixtureDefinition, error) {
	jsonc := manifest.StripJsoncComments(raw)
	if err := rejectDuplicateJSONFields(jsonc); err != nil {
		return v2FixtureDefinition{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(jsonc))
	decoder.DisallowUnknownFields()
	var fixture v2FixtureDefinition
	if err := decoder.Decode(&fixture); err != nil {
		return v2FixtureDefinition{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return v2FixtureDefinition{}, err
	}
	if fixture.SchemaVersion != 1 || strings.TrimSpace(fixture.SourceVersion) == "" || len(fixture.Entries) == 0 {
		return v2FixtureDefinition{}, fmt.Errorf("v2 fixture requires schema 1, sourceVersion, and entries")
	}
	if fixture.SourceVersion != strings.TrimSpace(fixture.SourceVersion) || strings.IndexFunc(fixture.SourceVersion, unicode.IsControl) >= 0 {
		return v2FixtureDefinition{}, fmt.Errorf("v2 fixture sourceVersion is malformed")
	}
	if _, err := modules.NormalizeNumericVersion(fixture.SourceVersion); err != nil {
		return v2FixtureDefinition{}, fmt.Errorf("v2 fixture sourceVersion is invalid: %w", err)
	}
	if fixture.TargetVersion != "" {
		if fixture.TargetVersion != strings.TrimSpace(fixture.TargetVersion) || strings.IndexFunc(fixture.TargetVersion, unicode.IsControl) >= 0 {
			return v2FixtureDefinition{}, fmt.Errorf("v2 fixture targetVersion is malformed")
		}
		if _, err := modules.NormalizeNumericVersion(fixture.TargetVersion); err != nil {
			return v2FixtureDefinition{}, fmt.Errorf("v2 fixture targetVersion is invalid: %w", err)
		}
	}
	seenCapture := map[string]struct{}{}
	seenRestore := map[string]struct{}{}
	for index, entry := range fixture.Entries {
		if strings.TrimSpace(entry.CaptureCoordinate) == "" || strings.TrimSpace(entry.RestoreCoordinate) == "" {
			return v2FixtureDefinition{}, fmt.Errorf("entry %d lacks exact coordinates", index)
		}
		if _, duplicate := seenCapture[entry.CaptureCoordinate]; duplicate {
			return v2FixtureDefinition{}, fmt.Errorf("entry %d duplicates capture coordinate", index)
		}
		if _, duplicate := seenRestore[entry.RestoreCoordinate]; duplicate {
			return v2FixtureDefinition{}, fmt.Errorf("entry %d duplicates restore coordinate", index)
		}
		seenCapture[entry.CaptureCoordinate] = struct{}{}
		seenRestore[entry.RestoreCoordinate] = struct{}{}
		switch entry.Kind {
		case fixtureKindFile:
			if entry.Format != v2FormatJSON && entry.Format != v2FormatINI && entry.Format != v2FormatFile || len(entry.Members) != 0 {
				return v2FixtureDefinition{}, fmt.Errorf("entry %d has invalid file format or members", index)
			}
		case fixtureKindDirectory:
			if entry.Format != v2FormatTree || len(entry.Members) == 0 {
				return v2FixtureDefinition{}, fmt.Errorf("entry %d has invalid directory shape", index)
			}
			seenMembers := map[string]struct{}{}
			for memberIndex, member := range entry.Members {
				path := strings.ReplaceAll(member.Path, `\`, "/")
				if !safeArtifactName(path) || strings.HasSuffix(path, "/") || member.Format != v2FormatJSON && member.Format != v2FormatINI && member.Format != v2FormatFile {
					return v2FixtureDefinition{}, fmt.Errorf("entry %d member %d is unsafe or unsupported", index, memberIndex)
				}
				key := strings.ToLower(path)
				if _, duplicate := seenMembers[key]; duplicate {
					return v2FixtureDefinition{}, fmt.Errorf("entry %d member %d is duplicated", index, memberIndex)
				}
				seenMembers[key] = struct{}{}
			}
		default:
			return v2FixtureDefinition{}, fmt.Errorf("entry %d kind is unsupported", index)
		}
	}
	return fixture, nil
}

func compileV2FixtureAt(repoRoot string, mod *modules.Module, scenario validationmatrix.Scenario) (v2CompiledFixture, *Failure) {
	unsupported := func(coordinate, detail string) (v2CompiledFixture, *Failure) {
		return v2CompiledFixture{}, fail(CodeUnsupportedFixture, "fixture", coordinate, detail)
	}
	generationFailure := func(coordinate, detail string) (v2CompiledFixture, *Failure) {
		return v2CompiledFixture{}, fail(CodeGenerationContract, "fixture", coordinate, detail)
	}
	if mod == nil || mod.EffectiveSchemaVersion() != 2 || mod.Config == nil || scenario.Mode != validationmatrix.ScenarioConfigGenerationV2 || scenario.Expected == nil {
		return unsupported("schema", "direct generation fixture requires one schema-v2 generation scenario")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureDeclarative || repoRoot == "" || !filepath.IsAbs(repoRoot) || filepath.Clean(repoRoot) != repoRoot {
		return unsupported("fixture", "schema-v2 generation fixture must be declarative and repository-contained")
	}
	fixturePath, err := safepath.Resolve(repoRoot, filepath.FromSlash(scenario.Fixture.Path))
	if err != nil {
		return unsupported("fixture.path", "fixture path escaped repository authority")
	}
	raw, _, err := safepath.ReadRegularFile(fixturePath)
	if err != nil {
		return unsupported("fixture.path", "fixture is absent or not a regular file")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != scenario.Fixture.SHA256 {
		return unsupported("fixture.sha256", "fixture bytes differ from the sidecar hash")
	}
	definition, err := decodeV2Fixture(raw)
	if err != nil {
		return unsupported("fixture.path", "fixture JSONC is malformed or unsupported")
	}
	if definition.TargetVersion != "" && definition.TargetVersion != definition.SourceVersion {
		return unsupported("targetVersion", "direct generation targetVersion must equal sourceVersion")
	}

	var detectors []modules.InstanceDetectorDef
	for _, detector := range mod.Config.InstanceDetectors {
		if detector.ID == scenario.Expected.DetectorID {
			detectors = append(detectors, detector)
		}
	}
	if len(detectors) != 1 || detectors[0].Type != "package" && detectors[0].Type != "path" {
		return generationFailure("expected.detectorId", "expected detector does not resolve exactly one supported production detector")
	}
	detector := detectors[0]
	if detector.Type == "package" && definition.InstanceLocator != "" || detector.Type == "path" && definition.InstanceLocator == "" {
		return unsupported("instanceLocator", "fixture locator does not match the production detector type")
	}

	var sets []modules.ConfigSetDef
	for _, set := range mod.Config.Sets {
		if set.ID == scenario.Expected.ConfigSetID {
			sets = append(sets, set)
		}
	}
	if len(sets) != 1 {
		return generationFailure("expected.configSetId", "expected config set does not resolve exactly one production set")
	}
	set := sets[0]
	generation, err := modules.SelectGeneration(&set, modules.NewVersionEvidence(definition.SourceVersion))
	if err != nil || generation == nil || generation.ID != scenario.Expected.GenerationID {
		return generationFailure("sourceVersion", "sourceVersion does not select the expected production generation")
	}
	if generation.Fingerprint != scenario.Expected.Fingerprint {
		return generationFailure("expected.fingerprint", "scenario fingerprint is not the current production generation fingerprint")
	}
	if generation.Capture == nil || len(generation.Capture.Files) == 0 || len(generation.Restore) == 0 {
		return unsupported("operations", "selected production generation has no roundtrip operations")
	}
	if len(generation.Capture.RegistryKeys) != 0 || len(generation.Capture.RegistryValues) != 0 {
		return unsupported("operations", "direct generation fixture supports file capture operations only")
	}

	compiled := v2CompiledFixture{Definition: definition, Detector: detector, Set: set, Generation: *generation}
	usedCapture := map[int]struct{}{}
	usedRestore := map[int]struct{}{}
	for entryIndex, shape := range definition.Entries {
		captureIndex := coordinateIndex(shape.CaptureCoordinate, "config.sets[%d].generations[%d].capture.files[%d]", mod, set.ID, generation.ID)
		restoreIndex := coordinateIndex(shape.RestoreCoordinate, "config.sets[%d].generations[%d].restore[%d]", mod, set.ID, generation.ID)
		if captureIndex < 0 || captureIndex >= len(generation.Capture.Files) || restoreIndex < 0 || restoreIndex >= len(generation.Restore) {
			return unsupported(fmt.Sprintf("entries[%d]", entryIndex), "fixture coordinate does not select the current production generation")
		}
		if _, duplicate := usedCapture[captureIndex]; duplicate {
			return unsupported(shape.CaptureCoordinate, "capture coordinate is duplicated")
		}
		if _, duplicate := usedRestore[restoreIndex]; duplicate {
			return unsupported(shape.RestoreCoordinate, "restore coordinate is duplicated")
		}
		usedCapture[captureIndex] = struct{}{}
		usedRestore[restoreIndex] = struct{}{}
		capture, restore := generation.Capture.Files[captureIndex], generation.Restore[restoreIndex]
		if restore.Type != "copy" || !restore.Backup || restore.Source != capture.Dest || restore.Target != capture.Source {
			return unsupported(shape.RestoreCoordinate, "capture and restore are not one exact backup-enabled copy pair")
		}
		if shape.Kind == fixtureKindFile && (strings.Contains(capture.Source, "${instance.root}") || strings.Contains(restore.Target, "${instance.root}")) {
			return unsupported(shape.CaptureCoordinate, "instance-root capture must declare a directory tree")
		}
		if shape.Kind == fixtureKindDirectory && capture.Source != "${instance.root}" {
			return unsupported(shape.CaptureCoordinate, "directory tree fixture must be anchored by the production instance root")
		}
		entry := v2CompiledEntry{Shape: shape, Capture: capture, Restore: restore}
		captureSet := stringSet(generation.Capture.ExcludeGlobs)
		restoreSet := stringSet(restore.Exclude)
		for _, pattern := range generation.Capture.ExcludeGlobs {
			if _, overlap := restoreSet[strings.ToLower(filepath.ToSlash(pattern))]; overlap {
				entry.Overlapping = append(entry.Overlapping, pattern)
			} else {
				entry.CaptureOnly = append(entry.CaptureOnly, pattern)
			}
		}
		for _, pattern := range restore.Exclude {
			if _, overlap := captureSet[strings.ToLower(filepath.ToSlash(pattern))]; !overlap {
				entry.RestoreOnly = append(entry.RestoreOnly, pattern)
			}
		}
		compiled.Entries = append(compiled.Entries, entry)
	}
	if len(usedCapture) != len(generation.Capture.Files) || len(usedRestore) != len(generation.Restore) {
		return unsupported("fixture.entries", "fixture must cover every selected capture and restore coordinate exactly once")
	}
	for validationIndex, validation := range generation.Validate {
		matched := -1
		for entryIndex := range compiled.Entries {
			if v2ValidationWitnessed(compiled.Entries[entryIndex].Shape, compiled.Entries[entryIndex].Capture, validation) {
				if matched >= 0 {
					return unsupported(fmt.Sprintf("validate[%d]", validationIndex), "production validation is ambiguously witnessed")
				}
				matched = entryIndex
			}
		}
		if matched < 0 {
			return unsupported(fmt.Sprintf("validate[%d]", validationIndex), "production validation has no syntactically valid fixture witness")
		}
		compiled.Entries[matched].Validations = append(compiled.Entries[matched].Validations, validation)
	}
	if len(generation.Validate) == 0 {
		return unsupported("validate", "selected production generation has no validation primitive")
	}
	_ = os.PathSeparator // retain platform-aware filepath semantics in this compiler
	return compiled, nil
}

func coordinateIndex(coordinate, format string, mod *modules.Module, setID, generationID string) int {
	setIndex, generationIndex := -1, -1
	for index, set := range mod.Config.Sets {
		if set.ID == setID {
			setIndex = index
			for nested, generation := range set.Generations {
				if generation.ID == generationID {
					generationIndex = nested
				}
			}
		}
	}
	for index := 0; index < 4096; index++ {
		if coordinate == fmt.Sprintf(format, setIndex, generationIndex, index) {
			return index
		}
	}
	return -1
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(filepath.ToSlash(value))] = struct{}{}
	}
	return result
}

func v2ValidationWitnessed(shape v2FixtureEntry, capture modules.CaptureFile, validation modules.ValidationDef) bool {
	path := filepath.ToSlash(validation.Path)
	destination := filepath.ToSlash(capture.Dest)
	if shape.Kind == fixtureKindFile {
		if path != destination {
			return false
		}
		return validation.Type == "json-parse" && shape.Format == v2FormatJSON || validation.Type == "ini-parse" && shape.Format == v2FormatINI || validation.Type == "file-exists" && shape.Format == v2FormatFile
	}
	for _, member := range shape.Members {
		if path != strings.TrimSuffix(destination, "/")+"/"+filepath.ToSlash(member.Path) {
			continue
		}
		return validation.Type == "json-parse" && member.Format == v2FormatJSON || validation.Type == "ini-parse" && member.Format == v2FormatINI || validation.Type == "file-exists" && member.Format == v2FormatFile
	}
	return false
}

func v2FixtureContents(moduleID, scenarioID, coordinate string, format v2FixtureFormat) ([]byte, []byte, error) {
	content := func(state string) ([]byte, error) {
		digest := sha256.Sum256([]byte(moduleID + "\x00" + scenarioID + "\x00" + coordinate + "\x00" + state))
		value := hex.EncodeToString(digest[:])
		var data []byte
		switch format {
		case v2FormatJSON:
			data = []byte(fmt.Sprintf("{\n  \"endstateValidation\": %q\n}\n", value))
		case v2FormatINI:
			data = []byte("[endstate-validation]\nvalue=" + value + "\n")
		case v2FormatFile:
			data = []byte("endstate-validation-v2:" + value + "\n")
		default:
			return nil, fmt.Errorf("unsupported content format %q", format)
		}
		if err := validateV2FixtureContent(format, data); err != nil {
			return nil, err
		}
		return data, nil
	}
	captured, err := content("captured")
	if err != nil {
		return nil, nil, err
	}
	mutated, err := content("mutated")
	return captured, mutated, err
}

func validateV2FixtureContent(format v2FixtureFormat, data []byte) error {
	switch format {
	case v2FormatJSON:
		_, err := configdoc.ParseJSON(data)
		return err
	case v2FormatINI:
		_, err := configdoc.ParseINI(data)
		return err
	case v2FormatFile:
		if len(data) == 0 || !strings.HasPrefix(string(data), "endstate-validation-v2:") {
			return fmt.Errorf("regular fixture content is empty or malformed")
		}
		return nil
	default:
		return fmt.Errorf("unsupported content format %q", format)
	}
}
