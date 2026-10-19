// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type fixtureKind string

const (
	fixtureKindFile      fixtureKind = "file"
	fixtureKindDirectory fixtureKind = "directory"
)

type fixtureDefinition struct {
	Coordinate    string
	Source        string
	Destination   string
	Target        string
	Optional      bool
	GlobalExclude []string
	TargetExclude []string
	Kind          fixtureKind
	Strategy      string
}

type fixtureDefinitions struct {
	Entries []fixtureDefinition
}

type registryDefinition struct {
	Coordinate  string
	Authored    string
	Key         string
	Destination string
	Source      string
	Target      string
}

// registryValueDefinition is one capture.registryValues entry and the
// registry-set restore that writes its captured type and data back.
type registryValueDefinition struct {
	Coordinate string
	Key        string
	Authored   string
	ValueName  string
	Target     string
	// Absent leaves the value unset at capture, so the fixture proves that
	// capture records the absence and restore never creates the value.
	Absent bool
}

type registryDefinitions struct {
	Entries []registryDefinition
	Values  []registryValueDefinition
}

// registryValuesPayloadName is the payload where capture records a module's
// named registry values.
const registryValuesPayloadName = "registry-values.json"

// registryUnrelatedValueName names the value that the fixture seeds beside the
// captured values. Capture must leave it out and restore must leave it alone.
const registryUnrelatedValueName = "EndstateValidationUnrelated"

func rejectFlattenedPayloadCollisions(mod *modules.Module, files fixtureDefinitions, registries registryDefinitions) *Failure {
	if mod == nil {
		return nil
	}
	seenPayloads := make(map[string]struct{}, len(files.Entries)+len(registries.Entries))
	for _, entry := range files.Entries {
		payload := strings.ToLower(v1ArtifactPayloadPath(mod.ID, entry.Destination))
		if _, duplicate := seenPayloads[payload]; duplicate {
			return fail(CodeUnsupportedFixture, "fixture", entry.Coordinate+".dest", "capture destinations collide after the production flattened payload rewrite")
		}
		seenPayloads[payload] = struct{}{}
	}
	for _, entry := range registries.Entries {
		payload := strings.ToLower(v1ArtifactPayloadPath(mod.ID, entry.Destination))
		if _, duplicate := seenPayloads[payload]; duplicate {
			return fail(CodeUnsupportedFixture, "fixture", entry.Coordinate+".dest", "capture destinations collide after the production flattened payload rewrite")
		}
		seenPayloads[payload] = struct{}{}
	}
	if len(registries.Values) != 0 {
		payload := strings.ToLower(v1ArtifactPayloadPath(mod.ID, registryValuesPayloadName))
		if _, duplicate := seenPayloads[payload]; duplicate {
			return fail(CodeUnsupportedFixture, "fixture", registries.Values[0].Coordinate, "capture destinations collide after the production flattened payload rewrite")
		}
	}
	return nil
}

func compileRegistryDefinitions(mod *modules.Module, scenario validationmatrix.Scenario) (registryDefinitions, *Failure) {
	if mod == nil || mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioConfigRoundtripV1 {
		return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "schema", "registry fixture requires a schema-v1 roundtrip module")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureAuto && scenario.Fixture.Type != validationmatrix.FixtureDeclarative {
		return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.type", "fixture type is unsupported")
	}
	scope, conflicts := modules.ResolveRegistrySecretScope(mod)
	conflicting := make(map[string]struct{}, len(conflicts))
	for _, conflict := range conflicts {
		conflicting[conflict.Coordinate] = struct{}{}
	}
	registryImports := make(map[int]struct{})
	for index, restore := range mod.Restore {
		switch restore.Type {
		case "registry-set":
			if restore.Source != "" || restore.Target != "" || restore.Pattern != "" || len(restore.Exclude) != 0 || restore.ValueType != "" || restore.Data != "" {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "registry-set restore must take its type and data from capture")
			}
			if !mod.Capture.CapturesRegistryValue(restore.Key, restore.ValueName) {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "registry-set restore names no captured registry value")
			}
		case "registry-import":
			if restore.Pattern != "" || restore.Reason != "" || len(restore.Exclude) != 0 || restore.Key != "" || restore.ValueName != "" || restore.ValueType != "" || restore.Data != "" {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "registry import includes fields from another restore strategy")
			}
			if !restore.Optional || !restore.Backup {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "registry import must be optional and backup-enabled")
			}
			if strings.ContainsAny(restore.Target, "*?[") {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d].target", index), "authored registry identity does not support wildcard paths")
			}
			if _, err := validationmode.NormalizeHKCU(restore.Target); err != nil {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d].target", index), "registry import requires a canonical HKCU target")
			}
			if !strings.HasPrefix(restore.Source, "./payload/") {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d].source", index), "registry import source must use the payload prefix")
			}
			if _, ok := portableRegistryDestination(strings.TrimPrefix(restore.Source, "./payload/")); !ok {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d].source", index), "registry import source is not portable")
			}
			registryImports[index] = struct{}{}
		}
	}
	if mod.Capture == nil || len(mod.Capture.RegistryKeys) == 0 && len(mod.Capture.RegistryValues) == 0 {
		for index := range mod.Restore {
			if _, registryImport := registryImports[index]; registryImport {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "registry import has no whole-key registry capture")
			}
		}
		return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "capture.registry", "roundtrip fixture has no registry capture")
	}

	canonicalKeys := make([]string, len(mod.Capture.RegistryKeys))
	destinations := make([]string, len(mod.Capture.RegistryKeys))
	for index, capture := range mod.Capture.RegistryKeys {
		coordinate := fmt.Sprintf("capture.registryKeys[%d]", index)
		if !capture.Optional {
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate, "registry capture must be optional")
		}
		if strings.ContainsAny(capture.Key, "*?[") {
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "authored registry identity does not support wildcard paths")
		}
		key, err := validationmode.NormalizeHKCU(capture.Key)
		if err != nil {
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "registry capture requires a canonical HKCU key")
		}
		destination, ok := portableRegistryDestination(capture.Dest)
		if !ok {
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate+".dest", "registry capture destination is not portable")
		}
		canonicalKeys[index], destinations[index] = key, destination
		for previous := range canonicalKeys[:index] {
			if registryRootsOverlap(key, canonicalKeys[previous]) {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "registry capture roots overlap")
			}
		}
		// Production capture filters secrets out of an overlapping export,
		// so the exported key no longer equals the seeded fixture state.
		// The exact-state roundtrip cannot prove that key.
		if _, conflict := conflicting[coordinate+".key"]; conflict || scope.Intersects(capture.Key) {
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "registry capture overlaps a declared secret")
		}
	}

	result := registryDefinitions{}
	consumed := make(map[int]struct{}, len(registryImports))
	for captureIndex := range mod.Capture.RegistryKeys {
		var matches []int
		for restoreIndex, restore := range mod.Restore {
			if _, ok := registryImports[restoreIndex]; !ok {
				continue
			}
			target, _ := validationmode.NormalizeHKCU(restore.Target)
			if !strings.EqualFold(target, canonicalKeys[captureIndex]) {
				continue
			}
			source, _ := portableRegistryDestination(strings.TrimPrefix(restore.Source, "./payload/"))
			if source == destinations[captureIndex] {
				matches = append(matches, restoreIndex)
			}
		}
		coordinate := fmt.Sprintf("capture.registryKeys[%d]", captureIndex)
		if len(matches) != 1 {
			if len(matches) == 0 {
				return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "registry capture must map to exactly one registry import")
			}
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", matches[1]), "registry import is matched more than once")
		}
		if _, duplicate := consumed[matches[0]]; duplicate {
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate, "registry import is consumed by more than one capture")
		}
		consumed[matches[0]] = struct{}{}
		restore := mod.Restore[matches[0]]
		result.Entries = append(result.Entries, registryDefinition{
			Coordinate: coordinate, Authored: mod.Capture.RegistryKeys[captureIndex].Key, Key: canonicalKeys[captureIndex],
			Destination: destinations[captureIndex], Source: restore.Source, Target: restore.Target,
		})
	}
	for restoreIndex := range mod.Restore {
		if _, registryImport := registryImports[restoreIndex]; !registryImport {
			continue
		}
		if _, ok := consumed[restoreIndex]; !ok {
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", restoreIndex), "registry import is not consumed by a capture")
		}
	}
	values, failure := compileRegistryValueDefinitions(mod, canonicalKeys, conflicting)
	if failure != nil {
		return registryDefinitions{}, failure
	}
	result.Values = values
	if failure := rejectFlattenedPayloadCollisions(mod, fixtureDefinitions{}, result); failure != nil {
		return registryDefinitions{}, failure
	}
	return result, nil
}

// compileRegistryValueDefinitions pairs each captured named value with the one
// registry-set restore that writes it back. The caller has checked that every
// registry-set restore names a captured value, so pairing each value with
// exactly one restore consumes every registry-set restore.
func compileRegistryValueDefinitions(mod *modules.Module, wholeKeys []string, conflicting map[string]struct{}) ([]registryValueDefinition, *Failure) {
	var result []registryValueDefinition
	seen := make(map[string]struct{}, len(mod.Capture.RegistryValues))
	for index, capture := range mod.Capture.RegistryValues {
		coordinate := fmt.Sprintf("capture.registryValues[%d]", index)
		if !capture.Optional {
			return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, "named registry value capture must be optional")
		}
		if strings.ContainsAny(capture.Key, "*?[") {
			return nil, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "authored registry identity does not support wildcard paths")
		}
		key, err := validationmode.NormalizeHKCU(capture.Key)
		if err != nil {
			return nil, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "named registry value requires a canonical HKCU key")
		}
		if capture.ValueName == "" || strings.EqualFold(capture.ValueName, registryUnrelatedValueName) {
			return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, "named registry value requires a name the fixture does not reserve")
		}
		identity := strings.ToLower(key) + "\x00" + strings.ToLower(capture.ValueName)
		if _, duplicate := seen[identity]; duplicate {
			return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, "named registry value is captured more than once")
		}
		seen[identity] = struct{}{}
		for _, root := range wholeKeys {
			if registryRootsOverlap(key, root) {
				return nil, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "named registry value overlaps a whole-key registry capture")
			}
		}
		if _, conflict := conflicting[coordinate]; conflict {
			return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, "named registry value is a declared secret")
		}
		var matches []int
		for restoreIndex, restore := range mod.Restore {
			if restore.Type != "registry-set" || !strings.EqualFold(restore.ValueName, capture.ValueName) {
				continue
			}
			if target, err := validationmode.NormalizeHKCU(restore.Key); err == nil && strings.EqualFold(target, key) {
				matches = append(matches, restoreIndex)
			}
		}
		if len(matches) != 1 {
			return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, "named registry value must map to exactly one registry-set restore")
		}
		restore := mod.Restore[matches[0]]
		result = append(result, registryValueDefinition{
			Coordinate: coordinate, Key: key, Authored: capture.Key, ValueName: capture.ValueName,
			Target: restore.Key + `\` + restore.ValueName,
		})
	}
	for index, verifier := range mod.Verify {
		if verifier.Type != "registry-value-equals" {
			continue
		}
		if verifier.ValueType != "" || verifier.Data != "" || !mod.Capture.CapturesRegistryValue(verifier.Path, verifier.ValueName) {
			return nil, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("verify[%d]", index), "registry value verifier must assert a captured value")
		}
	}
	// With two or more values, the last one stays unset at capture. One
	// value must stay present, or the module has nothing to restore.
	if len(result) >= 2 {
		result[len(result)-1].Absent = true
	}
	return result, nil
}

func portableRegistryDestination(value string) (string, bool) {
	normalized, err := safepath.NormalizePortable(value)
	if err != nil {
		return "", false
	}
	if !strings.HasSuffix(strings.ToLower(normalized), ".reg") {
		return "", false
	}
	return normalized, true
}

func registryRootsOverlap(first, second string) bool {
	return registryKeyContains(first, second) || registryKeyContains(second, first)
}

func registryKeyContains(root, key string) bool {
	root, key = strings.ToLower(root), strings.ToLower(key)
	return root == key || strings.HasPrefix(key, root+`\`)
}

func compileFixtureDefinitions(mod *modules.Module, scenario validationmatrix.Scenario) (fixtureDefinitions, *Failure) {
	return compileFilesystemFixtureDefinitions(mod, scenario, false)
}

func compileFilesystemFixtureDefinitions(mod *modules.Module, scenario validationmatrix.Scenario, allowRegistry bool) (fixtureDefinitions, *Failure) {
	if mod == nil || mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioConfigRoundtripV1 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "schema", "fixture requires a schema-v1 roundtrip module")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureAuto && scenario.Fixture.Type != validationmatrix.FixtureDeclarative {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.type", "fixture type is unsupported")
	}
	if mod.Capture == nil || !allowRegistry && (len(mod.Capture.RegistryKeys) > 0 || len(mod.Capture.RegistryValues) > 0) {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "capture.registry", "schema-v1 merge fixtures do not support registry capture")
	}
	fileRestores := 0
	for index, restore := range mod.Restore {
		if allowRegistry && (restore.Type == "registry-import" || restore.Type == "registry-set") {
			continue
		}
		if restore.Type != "copy" && restore.Type != "merge-json" && restore.Type != "merge-ini" ||
			restore.Key != "" || restore.ValueName != "" || restore.ValueType != "" || restore.Data != "" || restore.Pattern != "" || restore.Reason != "" {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "only file copy, merge-json, and merge-ini restores are supported")
		}
		if strings.ContainsAny(restore.Target, "*?[") {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d].target", index), "authored operation does not support wildcard paths")
		}
		if !restore.Backup {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "roundtrip restore must produce revertable backup evidence")
		}
		fileRestores++
	}
	if !allowRegistry && (len(mod.Capture.Files) == 0 || fileRestores == 0) {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "operations", "roundtrip fixture has no executable file operations")
	}
	result := fixtureDefinitions{}
	consumedRestore := make(map[int]struct{}, len(mod.Restore))
	for captureIndex, capture := range mod.Capture.Files {
		if strings.ContainsAny(capture.Source, "*?[") {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("capture.files[%d].source", captureIndex), "authored operation does not support wildcard paths")
		}
		var matches []int
		for restoreIndex, restore := range mod.Restore {
			if allowRegistry && (restore.Type == "registry-import" || restore.Type == "registry-set") {
				continue
			}
			if capture.Source == restore.Target && payloadDestination(restore.Source) == catalogPath(capture.Dest) {
				matches = append(matches, restoreIndex)
			}
		}
		if len(matches) != 1 {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("capture.files[%d]", captureIndex), "capture must map to exactly one copy restore")
		}
		if _, duplicate := consumedRestore[matches[0]]; duplicate {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("capture.files[%d]", captureIndex), "restore is consumed by more than one capture")
		}
		consumedRestore[matches[0]] = struct{}{}
		restore := mod.Restore[matches[0]]
		kind := fixtureKindFile
		// Auto fixtures use a deterministic synthetic shape only: a destination
		// extension selects a representative file, while no extension selects a
		// representative directory. This does not claim the production target's
		// live filesystem type; type-sensitive cases require a hash-bound
		// declarative fixture.
		if scenario.Fixture.Type == validationmatrix.FixtureAuto && restore.Type == "copy" && path.Ext(path.Base(catalogPath(capture.Dest))) == "" {
			kind = fixtureKindDirectory
		}
		result.Entries = append(result.Entries, fixtureDefinition{
			Coordinate: fmt.Sprintf("capture.files[%d]", captureIndex), Source: capture.Source,
			Destination: catalogPath(capture.Dest), Target: restore.Target,
			Optional:      capture.Optional || restore.Optional,
			GlobalExclude: append([]string(nil), mod.Capture.ExcludeGlobs...),
			TargetExclude: append([]string(nil), restore.Exclude...),
			Kind:          kind,
			Strategy:      restore.Type,
		})
	}
	if len(consumedRestore) != fileRestores {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "restore", "every copy restore must be consumed exactly once")
	}
	if failure := rejectFlattenedPayloadCollisions(mod, result, registryDefinitions{}); failure != nil {
		return fixtureDefinitions{}, failure
	}
	for index, verifier := range mod.Verify {
		if verifier.Type == "file-exists" && strings.ContainsAny(verifier.Path, "*?[") {
			// Fixture path semantics use path.Match, where '[' starts a character
			// class just like '*' and '?', so none can name a concrete fixture.
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("verify[%d].path", index), "authored operation does not support wildcard paths")
		}
	}
	return result, nil
}

type declarativeFixtureShape struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Entries       []declarativeFixtureEntry `json:"entries"`
}

type declarativeFixtureEntry struct {
	Coordinate string      `json:"coordinate"`
	Kind       fixtureKind `json:"kind"`
}

func compileFixtureDefinitionsAt(repoRoot string, mod *modules.Module, scenario validationmatrix.Scenario) (fixtureDefinitions, *Failure) {
	return compileFilesystemFixtureDefinitionsAt(repoRoot, mod, scenario, false)
}

func compileFilesystemFixtureDefinitionsAt(repoRoot string, mod *modules.Module, scenario validationmatrix.Scenario, allowRegistry bool) (fixtureDefinitions, *Failure) {
	definitions, failure := compileFilesystemFixtureDefinitions(mod, scenario, allowRegistry)
	if failure != nil || scenario.Fixture.Type == validationmatrix.FixtureAuto {
		return definitions, failure
	}
	if scenario.Fixture.Type != validationmatrix.FixtureDeclarative || repoRoot == "" || !filepath.IsAbs(repoRoot) || filepath.Clean(repoRoot) != repoRoot {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative fixture requires a canonical absolute repository root")
	}
	path, err := safepath.Resolve(repoRoot, filepath.FromSlash(scenario.Fixture.Path))
	if err != nil {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative fixture path left repository authority")
	}
	raw, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative fixture is absent or not a regular file")
	}
	digest := sha256.Sum256(raw)
	if fmt.Sprintf("%x", digest) != scenario.Fixture.SHA256 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.sha256", "declarative fixture hash differs from the sidecar")
	}
	jsonc := manifest.StripJsoncComments(raw)
	if err := rejectDuplicateJSONFields(jsonc); err != nil {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative fixture JSONC contains duplicate fields")
	}
	decoder := json.NewDecoder(bytes.NewReader(jsonc))
	decoder.DisallowUnknownFields()
	var shape declarativeFixtureShape
	if err := decoder.Decode(&shape); err != nil {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative fixture JSONC is malformed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative fixture must contain one JSON object")
	}
	if shape.SchemaVersion != 1 || len(shape.Entries) != len(definitions.Entries) {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.entries", "declarative fixture must cover every capture coordinate exactly once")
	}
	byCoordinate := make(map[string]fixtureKind, len(shape.Entries))
	for _, entry := range shape.Entries {
		if entry.Kind != fixtureKindFile && entry.Kind != fixtureKindDirectory {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", entry.Coordinate, "declarative fixture kind must be file or directory")
		}
		if _, duplicate := byCoordinate[entry.Coordinate]; duplicate {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", entry.Coordinate, "declarative fixture coordinate is duplicated")
		}
		byCoordinate[entry.Coordinate] = entry.Kind
	}
	for index := range definitions.Entries {
		kind, exists := byCoordinate[definitions.Entries[index].Coordinate]
		if !exists {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", definitions.Entries[index].Coordinate, "declarative fixture coordinate is absent")
		}
		definitions.Entries[index].Kind = kind
		if definitions.Entries[index].Strategy != "copy" && kind != fixtureKindFile {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", definitions.Entries[index].Coordinate, "merge restores require a file fixture kind")
		}
		delete(byCoordinate, definitions.Entries[index].Coordinate)
	}
	if len(byCoordinate) != 0 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.entries", "declarative fixture contains a foreign coordinate")
	}
	return definitions, nil
}

func rejectDuplicateJSONFields(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := walkUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}

func payloadDestination(source string) string {
	normalized := catalogPath(strings.TrimPrefix(source, "./"))
	const marker = "payload/"
	if index := strings.Index(strings.ToLower(normalized), marker); index >= 0 {
		return normalized[index+len(marker):]
	}
	return normalized
}

func catalogPath(value string) string {
	return strings.ReplaceAll(value, `\`, "/")
}
