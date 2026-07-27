// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
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
}

type fixtureDefinitions struct {
	Entries []fixtureDefinition
}

func compileFixtureDefinitions(mod *modules.Module, scenario validationmatrix.Scenario) (fixtureDefinitions, *Failure) {
	if mod == nil || mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioConfigRoundtripV1 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "schema", "fixture requires a schema-v1 roundtrip module")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureAuto && scenario.Fixture.Type != validationmatrix.FixtureDeclarative {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "fixture.type", "fixture type is unsupported")
	}
	for index, restore := range mod.Restore {
		if restore.Type != "copy" || restore.Key != "" {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "only schema-v1 copy restores are supported")
		}
		if !restore.Backup {
			return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("restore[%d]", index), "roundtrip restore must produce revertable backup evidence")
		}
	}
	if mod.Capture == nil || len(mod.Capture.Files) == 0 || len(mod.Restore) == 0 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "operations", "roundtrip fixture has no executable file operations")
	}
	if len(mod.Capture.RegistryKeys) > 0 || len(mod.Capture.RegistryValues) > 0 {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "capture.registry", "Task 7A does not support registry capture fixtures")
	}
	result := fixtureDefinitions{}
	consumedRestore := make(map[int]struct{}, len(mod.Restore))
	for captureIndex, capture := range mod.Capture.Files {
		var matches []int
		for restoreIndex, restore := range mod.Restore {
			if capture.Source == restore.Target && payloadDestination(restore.Source) == filepath.ToSlash(capture.Dest) {
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
		if scenario.Fixture.Type == validationmatrix.FixtureAuto && filepath.Ext(filepath.Base(filepath.ToSlash(capture.Dest))) == "" {
			kind = fixtureKindDirectory
		}
		result.Entries = append(result.Entries, fixtureDefinition{
			Coordinate: fmt.Sprintf("capture.files[%d]", captureIndex), Source: capture.Source,
			Destination: filepath.ToSlash(capture.Dest), Target: restore.Target,
			Optional:      capture.Optional || restore.Optional,
			GlobalExclude: append([]string(nil), mod.Capture.ExcludeGlobs...),
			TargetExclude: append([]string(nil), restore.Exclude...),
			Kind:          kind,
		})
	}
	if len(consumedRestore) != len(mod.Restore) {
		return fixtureDefinitions{}, fail(CodeUnsupportedFixture, "fixture", "restore", "every copy restore must be consumed exactly once")
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
	normalized := filepath.ToSlash(strings.TrimPrefix(source, "./"))
	const marker = "payload/"
	if index := strings.Index(strings.ToLower(normalized), marker); index >= 0 {
		return normalized[index+len(marker):]
	}
	return normalized
}
