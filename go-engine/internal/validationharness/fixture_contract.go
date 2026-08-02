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
	Key         string
	Destination string
	Source      string
	Target      string
}

type registryDefinitions struct {
	Entries []registryDefinition
}

func compileRegistryDefinitions(mod *modules.Module, scenario validationmatrix.Scenario) (registryDefinitions, *Failure) {
	if mod == nil || mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioConfigRoundtripV1 {
		return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "schema", "registry fixture requires a schema-v1 roundtrip module")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureAuto && scenario.Fixture.Type != validationmatrix.FixtureDeclarative {
		return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.type", "fixture type is unsupported")
	}
	if mod.Capture == nil || len(mod.Capture.RegistryKeys) == 0 {
		return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "capture.registry", "roundtrip fixture has no whole-key registry capture")
	}
	if len(mod.Capture.RegistryValues) != 0 {
		return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "capture.registryValues[0]", "named registry values are unsupported")
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
		if mod.Secrets != nil {
			for _, secret := range mod.Secrets.Files {
				secretKey, err := validationmode.NormalizeHKCU(secret)
				if err == nil && registryKeyContains(key, secretKey) {
					return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", coordinate+".key", "registry capture contains a declared secret")
				}
			}
		}
	}

	registryImports := make(map[int]struct{})
	for index, restore := range mod.Restore {
		switch restore.Type {
		case "registry-set":
			return registryDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "registry-set restores are unsupported")
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
			Coordinate: coordinate, Key: canonicalKeys[captureIndex], Destination: destinations[captureIndex], Source: restore.Source, Target: canonicalKeys[captureIndex],
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
	return result, nil
}

func portableRegistryDestination(value string) (string, bool) {
	if value == "" || value != strings.TrimSpace(value) || strings.Contains(value, ":") {
		return "", false
	}
	normalized := catalogPath(value)
	if strings.HasPrefix(normalized, "/") || !strings.HasSuffix(strings.ToLower(normalized), ".reg") {
		return "", false
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == "" || component == "." || component == ".." {
			return "", false
		}
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
	if mod == nil || mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioConfigRoundtripV1 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "schema", "fixture requires a schema-v1 roundtrip module")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureAuto && scenario.Fixture.Type != validationmatrix.FixtureDeclarative {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.type", "fixture type is unsupported")
	}
	if mod.Capture == nil || len(mod.Capture.RegistryKeys) > 0 || len(mod.Capture.RegistryValues) > 0 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "capture.registry", "schema-v1 merge fixtures do not support registry capture")
	}
	for index, restore := range mod.Restore {
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
	}
	if len(mod.Capture.Files) == 0 || len(mod.Restore) == 0 {
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
	if len(consumedRestore) != len(mod.Restore) {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "restore", "every copy restore must be consumed exactly once")
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
	definitions, failure := compileFixtureDefinitions(mod, scenario)
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
	var consume func() error
	consume = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, nested := token.(json.Delim)
		if !nested {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = struct{}{}
				if err := consume(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return fmt.Errorf("object is not closed")
			}
		case '[':
			for decoder.More() {
				if err := consume(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return fmt.Errorf("array is not closed")
			}
		default:
			return fmt.Errorf("unexpected delimiter %q", delimiter)
		}
		return nil
	}
	if err := consume(); err != nil {
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
