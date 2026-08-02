// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const fixturePayloadName = "endstate-validation-fixture.txt"

type FixturePlan struct {
	context         *validationmode.Context
	registryFixture registryFixture
	Targets         []FixtureTarget
	RegistryTargets []RegistryFixtureTarget
}

type registryFixture interface {
	Replace(string, validationmode.RegistryState) error
	Snapshot(string) (validationmode.RegistryState, error)
	Remove(string) error
	ProveAbsent(string) error
}

type FixtureTarget struct {
	Coordinate                  string
	Authored                    string
	Destination                 string
	Resolved                    string
	PayloadPath                 string
	Strategy                    string
	Captured                    string
	Mutated                     string
	Restored                    string
	Directory                   bool
	Optional                    bool
	CaptureExcluded             []FixtureExcluded
	RestoreExcluded             []FixtureExcluded
	OverlappingExcluded         []FixtureExcluded
	RetainedCaptureAncestorDirs []string
}

type RegistryFixtureTarget struct {
	Coordinate  string
	Authored    string
	Destination string
	Source      string
	Target      string
	Strategy    string
	Optional    bool
	Captured    validationmode.RegistryState
	Mutated     validationmode.RegistryState
	Restored    validationmode.RegistryState
}

// FixtureRestoreTarget is the shared, deterministic projection consumed by
// every production-backed roundtrip assertion. Registry payload text proves
// only its grammar and scope; typed fixture snapshots prove its content.
type FixtureRestoreTarget struct {
	Coordinate  string
	Authored    string
	Destination string
	Strategy    string
	Optional    bool
	Registry    bool
}

type FixtureExcluded struct {
	Relative        string
	Path            string
	Captured        string
	Mutated         string
	CapturePatterns []string
	RestorePatterns []string
}

func compileFixturePlan(context *validationmode.Context, mod *modules.Module, scenario validationmatrix.Scenario, definitions fixtureDefinitions) (*FixturePlan, *Failure) {
	return compileFixturePlanWithEmptyOption(context, mod, scenario, definitions, false)
}

func compileFixturePlanWithEmptyOption(context *validationmode.Context, mod *modules.Module, scenario validationmatrix.Scenario, definitions fixtureDefinitions, allowEmpty bool) (*FixturePlan, *Failure) {
	if context == nil || mod == nil || context.Descriptor().ModuleID != mod.ID || context.Descriptor().ScenarioID != scenario.ID {
		return nil, fail(CodeIsolationFailure, "fixture", "testMode", "fixture authority does not match module and scenario")
	}
	globalPatterns := []string(nil)
	firstDirectory := -1
	for index, definition := range definitions.Entries {
		if index == 0 {
			globalPatterns = append(globalPatterns, definition.GlobalExclude...)
		} else if !exactStrings(definition.GlobalExclude, globalPatterns) {
			return nil, fail(CodeUnsupportedFixture, "fixture", definition.Coordinate, "global exclude contract changed between capture coordinates")
		}
		if definition.Kind == fixtureKindDirectory && firstDirectory < 0 {
			firstDirectory = index
		}
	}
	for _, definition := range definitions.Entries {
		if definition.Kind != fixtureKindFile {
			continue
		}
		basename := path.Base(catalogPath(definition.Source))
		for patternIndex, pattern := range globalPatterns {
			matched, err := bundle.ConfigPathMatchesExcludeGlob(basename, pattern)
			if err != nil {
				return nil, fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("capture.excludeGlobs[%d]", patternIndex), "global exclude glob is malformed")
			}
			if matched {
				return nil, fail(CodeUnsupportedFixture, "fixture", definition.Coordinate, "global exclude glob applies to a direct capture filename")
			}
		}
	}
	globalWitnesses := []string(nil)
	if firstDirectory >= 0 {
		var globalOK bool
		globalWitnesses, globalOK = excludedFixtureRelatives(globalPatterns)
		if !globalOK {
			return nil, fail(CodeUnsupportedFixture, "fixture", "capture.excludeGlobs", "every authored exclude glob requires one deterministic directory witness")
		}
	}
	plan := &FixturePlan{context: context}
	for definitionIndex, definition := range definitions.Entries {
		resolved, err := context.ResolveHostPath(definition.Target, validationmode.HostPathPolicy{})
		if err != nil {
			return nil, fail(CodeUnsupportedFixture, "fixture", definition.Coordinate, "target cannot be resolved through validation mode")
		}
		for _, existing := range plan.Targets {
			if fixtureTargetRootsOverlap(existing.Resolved, resolved) {
				return nil, fail(CodeUnsupportedFixture, "fixture", definition.Coordinate, "capture target overlaps another resolved fixture target")
			}
		}
		directory := definition.Kind == fixtureKindDirectory
		payloadPath := resolved
		if directory {
			payloadPath = filepath.Join(resolved, fixturePayloadName)
		}
		captured, mutated, restored := fixtureStates(mod.ID, scenario.ID, definition.Coordinate, definition.Strategy)
		target := FixtureTarget{
			Coordinate: definition.Coordinate, Authored: definition.Target, Resolved: resolved,
			Destination: definition.Destination,
			PayloadPath: payloadPath, Directory: directory, Optional: definition.Optional, Strategy: definition.Strategy,
			Captured: captured, Mutated: mutated, Restored: restored,
		}
		if directory {
			restoreWitnesses, ok := excludedFixtureRelatives(definition.TargetExclude)
			if !ok {
				return nil, fail(CodeUnsupportedFixture, "fixture", definition.Coordinate, "every authored exclude glob must have one deterministic witness")
			}
			captureWitnesses := []string(nil)
			capturePatterns := []string(nil)
			if definitionIndex == firstDirectory {
				captureWitnesses = append(captureWitnesses, globalWitnesses...)
				capturePatterns = append(capturePatterns, globalPatterns...)
			}
			captureByRelative := witnessPatterns(captureWitnesses, capturePatterns)
			restoreByRelative := witnessPatterns(restoreWitnesses, definition.TargetExclude)
			for _, relative := range restoreWitnesses {
				key := strings.ToLower(catalogPath(relative))
				for _, pattern := range globalPatterns {
					matched, err := bundle.ConfigPathMatchesExcludeGlob(relative, pattern)
					if err != nil || !matched {
						continue
					}
					alreadyAssigned := false
					for _, assigned := range captureByRelative[key] {
						if strings.EqualFold(catalogPath(assigned), catalogPath(pattern)) {
							alreadyAssigned = true
							break
						}
					}
					if !alreadyAssigned {
						captureByRelative[key] = append(captureByRelative[key], pattern)
					}
				}
			}
			ordered := append(append([]string(nil), captureWitnesses...), restoreWitnesses...)
			seen := map[string]struct{}{}
			for _, relative := range ordered {
				key := strings.ToLower(catalogPath(relative))
				if _, duplicate := seen[key]; duplicate {
					continue
				}
				seen[key] = struct{}{}
				captureRole := captureByRelative[key]
				restoreRole := restoreByRelative[key]
				witness := FixtureExcluded{
					Relative:        relative,
					Path:            filepath.Join(resolved, filepath.FromSlash(relative)),
					Captured:        fixtureSentinel(mod.ID, scenario.ID, definition.Coordinate+"/"+relative, "excluded-captured"),
					Mutated:         fixtureSentinel(mod.ID, scenario.ID, definition.Coordinate+"/"+relative, "excluded-mutated"),
					CapturePatterns: append([]string(nil), captureRole...), RestorePatterns: append([]string(nil), restoreRole...),
				}
				switch {
				case len(captureRole) > 0 && len(restoreRole) > 0:
					target.OverlappingExcluded = append(target.OverlappingExcluded, witness)
				case len(captureRole) > 0:
					target.CaptureExcluded = append(target.CaptureExcluded, witness)
				case len(restoreRole) > 0:
					target.RestoreExcluded = append(target.RestoreExcluded, witness)
				}
			}
			target.RetainedCaptureAncestorDirs = retainedCaptureAncestorDirs(target)
		} else if len(definition.TargetExclude) != 0 {
			return nil, fail(CodeUnsupportedFixture, "fixture", definition.Coordinate, "target-specific exclude glob requires a directory capture")
		}
		plan.Targets = append(plan.Targets, target)
	}
	if !allowEmpty && len(plan.Targets) == 0 {
		return nil, fail(CodeUnsupportedFixture, "fixture", "operations", "fixture plan is empty")
	}
	if failure := plan.bindNestedFileVerifierPayloads(mod, definitions); failure != nil {
		return nil, failure
	}
	return plan, nil
}

func retainedCaptureAncestorDirs(target FixtureTarget) []string {
	if !target.Directory {
		return nil
	}
	witnesses := append(append([]FixtureExcluded(nil), target.CaptureExcluded...), target.OverlappingExcluded...)
	patterns := make([]string, 0)
	for _, witness := range witnesses {
		patterns = append(patterns, witness.CapturePatterns...)
	}
	seen := make(map[string]struct{})
	var result []string
	for _, witness := range witnesses {
		for parent := path.Dir(catalogPath(witness.Relative)); parent != "." && parent != "/"; parent = path.Dir(parent) {
			if fixtureRelativeMatchesPatterns(parent, patterns) {
				continue
			}
			key := strings.ToLower(catalogPath(parent))
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, parent)
		}
	}
	sort.Slice(result, func(left, right int) bool { return strings.ToLower(result[left]) < strings.ToLower(result[right]) })
	return result
}

func compileCompositeFixturePlanAt(repoRoot string, context *validationmode.Context, mod *modules.Module, scenario validationmatrix.Scenario, fixture registryFixture) (*FixturePlan, *Failure) {
	definitions, failure := compileFilesystemFixtureDefinitionsAt(repoRoot, mod, scenario, true)
	if failure != nil {
		return nil, failure
	}
	registries := registryDefinitions{}
	if moduleHasRegistryFixtureContract(mod) {
		registries, failure = compileRegistryDefinitions(mod, scenario)
		if failure != nil {
			return nil, failure
		}
	}
	return compileCompositeFixturePlanWithRegistryDefinitions(context, mod, scenario, definitions, registries, fixture)
}

func compileCompositeFixturePlanWithRegistryDefinitions(context *validationmode.Context, mod *modules.Module, scenario validationmatrix.Scenario, definitions fixtureDefinitions, registries registryDefinitions, fixture registryFixture) (*FixturePlan, *Failure) {
	plan, failure := compileFixturePlanWithEmptyOption(context, mod, scenario, definitions, true)
	if failure != nil {
		return nil, failure
	}
	if len(registries.Entries) != 0 && fixture == nil {
		return nil, fail(CodeIsolationFailure, "fixture", registries.Entries[0].Coordinate, "registry fixture authority is absent")
	}
	plan.registryFixture = fixture
	for _, definition := range registries.Entries {
		captured, mutated, restored, err := registryFixtureStates(mod.ID, scenario.ID, definition.Coordinate, definition.Key)
		if err != nil {
			return nil, fail(CodeUnsupportedFixture, "fixture", definition.Coordinate, "registry fixture state is invalid")
		}
		plan.RegistryTargets = append(plan.RegistryTargets, RegistryFixtureTarget{
			Coordinate: definition.Coordinate, Authored: definition.Authored, Destination: definition.Destination,
			Source: definition.Source, Target: definition.Target, Strategy: "registry-import", Optional: true,
			Captured: captured, Mutated: mutated, Restored: restored,
		})
	}
	if failure := plan.bindCoveredRegistryKeyVerifiers(mod); failure != nil {
		return nil, failure
	}
	if plan.OperationCount() == 0 {
		return nil, fail(CodeUnsupportedFixture, "fixture", "operations", "fixture plan is empty")
	}
	return plan, nil
}

func (plan *FixturePlan) bindCoveredRegistryKeyVerifiers(mod *modules.Module) *Failure {
	if plan == nil || mod == nil {
		return nil
	}
	for verifierIndex, verifier := range mod.Verify {
		if verifier.Type != "registry-key-exists" {
			continue
		}
		verifierKey, err := validationmode.NormalizeHKCU(verifier.Path)
		if err != nil {
			return fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("verify[%d].path", verifierIndex), "registry verifier key is invalid")
		}
		for targetIndex := range plan.RegistryTargets {
			target := &plan.RegistryTargets[targetIndex]
			root, err := validationmode.NormalizeHKCU(target.Authored)
			if err != nil || !registryKeyContains(root, verifierKey) {
				continue
			}
			relative := strings.TrimPrefix(verifierKey[len(root):], `\`)
			captured, err := bindRegistryKeyExists(target.Captured, relative)
			if err != nil {
				return fail(CodeUnsupportedFixture, "fixture", target.Coordinate, "registry verifier state is invalid")
			}
			mutated, err := bindRegistryKeyExists(target.Mutated, relative)
			if err != nil {
				return fail(CodeUnsupportedFixture, "fixture", target.Coordinate, "registry verifier state is invalid")
			}
			restored, err := bindRegistryKeyExists(target.Restored, relative)
			if err != nil {
				return fail(CodeUnsupportedFixture, "fixture", target.Coordinate, "registry verifier state is invalid")
			}
			target.Captured, target.Mutated, target.Restored = captured, mutated, restored
		}
	}
	return nil
}

func bindRegistryKeyExists(state validationmode.RegistryState, relative string) (validationmode.RegistryState, error) {
	keys := state.Keys()
	for current := relative; ; {
		found := false
		for _, key := range keys {
			if strings.EqualFold(key.Path, current) {
				found = true
				break
			}
		}
		if !found {
			keys = append(keys, validationmode.RegistryKey{Path: current})
		}
		if current == "" {
			break
		}
		separator := strings.LastIndex(current, `\`)
		if separator < 0 {
			current = ""
		} else {
			current = current[:separator]
		}
	}
	return validationmode.NewRegistryState(keys)
}

func moduleHasRegistryFixtureContract(mod *modules.Module) bool {
	if mod == nil {
		return false
	}
	if mod.Capture != nil && (len(mod.Capture.RegistryKeys) != 0 || len(mod.Capture.RegistryValues) != 0) {
		return true
	}
	for _, restore := range mod.Restore {
		if restore.Type == "registry-import" || restore.Type == "registry-set" {
			return true
		}
	}
	return false
}

func registryFixtureStates(moduleID, scenarioID, coordinate, key string) (validationmode.RegistryState, validationmode.RegistryState, validationmode.RegistryState, error) {
	value := func(state, identity string) string {
		return fixtureSentinel(moduleID, scenarioID, coordinate+"/"+key+"/"+identity, state)
	}
	captured, err := validationmode.NewRegistryState([]validationmode.RegistryKey{
		{Values: []validationmode.RegistryValue{
			{Name: "", Type: validationmode.RegistryTypeString, Data: registryStringBytes(value("captured", "default"))},
			{Name: "mode", Type: validationmode.RegistryTypeDWORD, Data: registryDWORDBytes(value("captured", "mode"))},
		}},
		{Path: "Child", Values: []validationmode.RegistryValue{{Name: "binary", Type: validationmode.RegistryTypeBinary, Data: registryBinaryBytes(value("captured", "binary"))}}},
	})
	if err != nil {
		return validationmode.RegistryState{}, validationmode.RegistryState{}, validationmode.RegistryState{}, err
	}
	mutated, err := validationmode.NewRegistryState([]validationmode.RegistryKey{
		{Values: []validationmode.RegistryValue{
			{Name: "", Type: validationmode.RegistryTypeDWORD, Data: registryDWORDBytes(value("mutated", "default"))},
			{Name: "mode", Type: validationmode.RegistryTypeString, Data: registryStringBytes(value("mutated", "mode"))},
		}},
		{Path: "Child", Values: []validationmode.RegistryValue{{Name: "binary", Type: validationmode.RegistryTypeBinary, Data: registryBinaryBytes(value("mutated", "binary"))}}},
	})
	if err != nil {
		return validationmode.RegistryState{}, validationmode.RegistryState{}, validationmode.RegistryState{}, err
	}
	return captured, mutated, captured, nil
}

func registryStringBytes(value string) []byte {
	units := append(utf16.Encode([]rune(value)), 0)
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	return data
}

func registryDWORDBytes(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, binary.LittleEndian.Uint32(digest[:4]))
	return data
}

func registryBinaryBytes(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return append([]byte(nil), digest[:8]...)
}

func fixtureStates(moduleID, scenarioID, coordinate, strategy string) (captured, mutated, restored string) {
	sourceValue, targetValue := "", ""
	if strategy == "merge-json" || strategy == "merge-ini" {
		target := fixtureSentinel(moduleID, scenarioID, coordinate, "merge-target")
		sourceValue, targetValue = "source-"+target, "target-"+target
	}
	switch strategy {
	case "merge-json":
		return fmt.Sprintf(`{"array":["%s"],"nested":{"sourceOnly":"%s","shared":"%s"},"sourceOnly":"%s"}`, sourceValue, sourceValue, sourceValue, sourceValue),
			fmt.Sprintf(`{"array":["%s"],"nested":{"shared":"%s","targetOnly":"%s"},"targetOnly":"%s"}`, targetValue, targetValue, targetValue, targetValue),
			fmt.Sprintf("{\n  \"array\": [\n    \"%s\"\n  ],\n  \"nested\": {\n    \"shared\": \"%s\",\n    \"sourceOnly\": \"%s\",\n    \"targetOnly\": \"%s\"\n  },\n  \"sourceOnly\": \"%s\",\n  \"targetOnly\": \"%s\"\n}\n", sourceValue, sourceValue, sourceValue, targetValue, sourceValue, targetValue)
	case "merge-ini":
		return fmt.Sprintf("global=%s\n\n[shared]\nsourceOnly=%s\nshared=%s\n\n[source]\nkey=%s", sourceValue, sourceValue, sourceValue, sourceValue),
			fmt.Sprintf("global=%s\ntargetGlobal=%s\n\n[shared]\nshared=%s\ntargetOnly=%s\n\n[target]\nkey=%s", targetValue, targetValue, targetValue, targetValue, targetValue),
			fmt.Sprintf("global=%s\ntargetGlobal=%s\n\n[shared]\nshared=%s\nsourceOnly=%s\ntargetOnly=%s\n\n[source]\nkey=%s\n\n[target]\nkey=%s", sourceValue, targetValue, sourceValue, sourceValue, targetValue, sourceValue, targetValue)
	default:
		captured = fixtureSentinel(moduleID, scenarioID, coordinate, "captured")
		return captured, fixtureSentinel(moduleID, scenarioID, coordinate, "mutated"), captured
	}
}

func fixtureStrategy(target FixtureTarget) string {
	if target.Strategy == "" {
		return "copy"
	}
	return target.Strategy
}

func fixtureTargetRootsOverlap(first, second string) bool {
	first = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(first)), "/")
	second = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(second)), "/")
	if first == "" || second == "" {
		return false
	}
	first = strings.ToLower(first)
	second = strings.ToLower(second)
	return first == second || strings.HasPrefix(first, second+"/") || strings.HasPrefix(second, first+"/")
}

func (plan *FixturePlan) bindNestedFileVerifierPayloads(mod *modules.Module, definitions fixtureDefinitions) *Failure {
	if plan == nil || plan.context == nil || mod == nil || len(plan.Targets) != len(definitions.Entries) {
		return fail(CodeUnsupportedFixture, "fixture", "verify", "nested verifier payload authority is absent")
	}
	for targetIndex := range plan.Targets {
		target, definition := &plan.Targets[targetIndex], definitions.Entries[targetIndex]
		if !target.Directory {
			continue
		}
		candidates := map[string]string{}
		for verifierIndex, verifier := range mod.Verify {
			if verifier.Type != "file-exists" {
				continue
			}
			path, err := plan.context.ResolveHostPath(verifier.Path, validationmode.HostPathPolicy{})
			if err != nil {
				return fail(CodeUnsupportedFixture, "fixture", fmt.Sprintf("verify[%d]", verifierIndex), "file verifier cannot be resolved through validation mode")
			}
			if !pathIsEqualOrAncestor(target.Resolved, path) || strings.EqualFold(filepath.Clean(target.Resolved), filepath.Clean(path)) {
				continue
			}
			relative, ok := targetPayloadRelativeForPath(*target, path)
			if !ok {
				return fail(CodeUnsupportedFixture, "fixture", target.Coordinate, "nested file verifier left the directory fixture")
			}
			if fixtureRelativeMatchesPatterns(relative, append(append([]string(nil), definition.GlobalExclude...), definition.TargetExclude...)) {
				return fail(CodeUnsupportedFixture, "fixture", target.Coordinate, "nested file verifier is excluded from capture or restore")
			}
			candidates[strings.ToLower(filepath.Clean(path))] = path
		}
		switch len(candidates) {
		case 0:
		case 1:
			for _, path := range candidates {
				target.PayloadPath = path
			}
		default:
			return fail(CodeUnsupportedFixture, "fixture", target.Coordinate, "directory capture has ambiguous nested file verifiers")
		}
	}
	return nil
}

func fixtureRelativeMatchesPatterns(relative string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := bundle.ConfigPathMatchesExcludeGlob(relative, pattern)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func targetPayloadRelative(target FixtureTarget) (string, bool) {
	return targetPayloadRelativeForPath(target, target.PayloadPath)
}

func targetPayloadRelativeForPath(target FixtureTarget, path string) (string, bool) {
	if !target.Directory {
		return ".", strings.EqualFold(filepath.Clean(path), filepath.Clean(target.Resolved))
	}
	relative, err := filepath.Rel(target.Resolved, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || !fixtureContained(target.Resolved, path) {
		return "", false
	}
	return catalogPath(relative), true
}

func witnessPatterns(relatives, patterns []string) map[string][]string {
	result := make(map[string][]string, len(relatives))
	for index, relative := range relatives {
		key := strings.ToLower(catalogPath(relative))
		result[key] = append(result[key], patterns[index])
	}
	return result
}

func fixtureSentinel(moduleID, scenarioID, coordinate, state string) string {
	digest := sha256.Sum256([]byte(moduleID + "\x00" + scenarioID + "\x00" + coordinate + "\x00" + state))
	return "endstate-validation-v1:" + hex.EncodeToString(digest[:])
}

func excludedFixtureRelatives(patterns []string) ([]string, bool) {
	seen := map[string]string{}
	var result []string
	for _, raw := range patterns {
		pattern := catalogPath(raw)
		stripped := strings.TrimPrefix(pattern, "**/")
		var relative string
		if strings.HasSuffix(stripped, "/**") {
			directory := strings.Trim(strings.TrimSuffix(stripped, "/**"), "/")
			if directory != "" {
				components := strings.Split(directory, "/")
				witness := make([]string, 0, len(components)+1)
				for _, component := range components {
					if component == "" || component == "." || component == ".." || strings.Contains(component, "**") || strings.ContainsAny(component, "?[") {
						return nil, false
					}
					witness = append(witness, strings.ReplaceAll(component, "*", "fixture"))
				}
				witness = append(witness, fixturePayloadName)
				relative = strings.Join(witness, "/")
			}
		} else if !strings.Contains(stripped, "/") {
			switch {
			case strings.HasPrefix(stripped, "*."):
				relative = "excluded" + strings.TrimPrefix(stripped, "*")
			case strings.ContainsAny(stripped, "?["):
				return nil, false
			case strings.Contains(stripped, "*"):
				relative = strings.ReplaceAll(stripped, "*", "fixture")
			case stripped != "." && stripped != "..":
				relative = stripped
			}
		}
		if relative == "" {
			return nil, false
		}
		matched, err := bundle.ConfigPathMatchesExcludeGlob(relative, raw)
		if err != nil || !matched {
			return nil, false
		}
		key := strings.ToLower(relative)
		normalizedPattern := strings.ToLower(catalogPath(raw))
		if previous, exists := seen[key]; exists {
			if previous != normalizedPattern {
				return nil, false
			}
			result = append(result, relative)
			continue
		}
		seen[key] = normalizedPattern
		result = append(result, relative)
	}
	return result, true
}

func (plan *FixturePlan) MaterializeCaptured() *Failure {
	for index := range plan.Targets {
		if failure := plan.materialize(&plan.Targets[index], plan.Targets[index].Captured, true); failure != nil {
			return failure
		}
	}
	for index := range plan.RegistryTargets {
		if failure := plan.replaceRegistry(&plan.RegistryTargets[index], plan.RegistryTargets[index].Captured); failure != nil {
			return failure
		}
	}
	return nil
}

func (plan *FixturePlan) MaterializeRestored() *Failure {
	for index := range plan.Targets {
		if failure := plan.materialize(&plan.Targets[index], plan.Targets[index].Restored, false); failure != nil {
			return failure
		}
		if failure := plan.materializeRetainedCaptureAncestorDirs(&plan.Targets[index]); failure != nil {
			return failure
		}
	}
	for index := range plan.RegistryTargets {
		if failure := plan.replaceRegistry(&plan.RegistryTargets[index], plan.RegistryTargets[index].Restored); failure != nil {
			return failure
		}
	}
	return nil
}

func (plan *FixturePlan) materializeRetainedCaptureAncestorDirs(target *FixtureTarget) *Failure {
	for _, relative := range target.RetainedCaptureAncestorDirs {
		path := filepath.Join(target.Resolved, filepath.FromSlash(relative))
		if failure := prepareRetainedCaptureAncestorDirectory(plan.context, path, target.Coordinate); failure != nil {
			return failure
		}
	}
	return nil
}

func (plan *FixturePlan) HasOptionalTargets() bool {
	for _, target := range plan.Targets {
		if target.Optional {
			return true
		}
	}
	for _, target := range plan.RegistryTargets {
		if target.Optional {
			return true
		}
	}
	return false
}

func (plan *FixturePlan) MaterializeOptionalAbsent() *Failure {
	for index := range plan.Targets {
		if plan.Targets[index].Optional {
			if failure := plan.removeTarget(&plan.Targets[index]); failure != nil {
				return failure
			}
		}
	}
	for index := range plan.RegistryTargets {
		target := &plan.RegistryTargets[index]
		if !target.Optional {
			continue
		}
		if plan.registryFixture == nil || plan.registryFixture.Remove(target.Authored) != nil || plan.registryFixture.ProveAbsent(target.Authored) != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "registry fixture operation failed")
		}
	}
	return nil
}

func (plan *FixturePlan) Mutate() *Failure {
	for index := range plan.Targets {
		if failure := plan.materialize(&plan.Targets[index], plan.Targets[index].Mutated, false); failure != nil {
			return failure
		}
	}
	for index := range plan.RegistryTargets {
		if failure := plan.replaceRegistry(&plan.RegistryTargets[index], plan.RegistryTargets[index].Mutated); failure != nil {
			return failure
		}
	}
	return nil
}

func (plan *FixturePlan) OperationCount() int {
	if plan == nil {
		return 0
	}
	return len(plan.Targets) + len(plan.RegistryTargets)
}

func (plan *FixturePlan) RestoreTargets() []FixtureRestoreTarget {
	if plan == nil {
		return nil
	}
	targets := make([]FixtureRestoreTarget, 0, plan.OperationCount())
	for _, target := range plan.Targets {
		targets = append(targets, FixtureRestoreTarget{
			Coordinate: target.Coordinate, Authored: target.Authored, Destination: target.Destination,
			Strategy: fixtureStrategy(target), Optional: target.Optional,
		})
	}
	for _, target := range plan.RegistryTargets {
		targets = append(targets, FixtureRestoreTarget{
			Coordinate: target.Coordinate, Authored: target.Target, Destination: target.Destination,
			Strategy: target.Strategy, Optional: target.Optional, Registry: true,
		})
	}
	return targets
}

func (plan *FixturePlan) replaceRegistry(target *RegistryFixtureTarget, state validationmode.RegistryState) *Failure {
	if plan.registryFixture == nil || plan.registryFixture.Replace(target.Authored, state) != nil {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "registry fixture operation failed")
	}
	return nil
}

func (plan *FixturePlan) materialize(target *FixtureTarget, content string, includeExcluded bool) *Failure {
	if failure := plan.removeTarget(target); failure != nil {
		return failure
	}
	if failure := prepareFixtureFile(plan.context, target.PayloadPath, target.Coordinate); failure != nil {
		return failure
	}
	if err := os.MkdirAll(filepath.Dir(target.PayloadPath), 0o700); err != nil {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "create fixture parent")
	}
	if failure := prepareFixtureFile(plan.context, target.PayloadPath, target.Coordinate); failure != nil {
		return failure
	}
	if err := os.WriteFile(target.PayloadPath, []byte(content), 0o600); err != nil {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "write fixture payload")
	}
	if includeExcluded {
		all := append(append(append([]FixtureExcluded(nil), target.CaptureExcluded...), target.RestoreExcluded...), target.OverlappingExcluded...)
		for _, excluded := range all {
			if failure := prepareFixtureFile(plan.context, excluded.Path, target.Coordinate); failure != nil {
				return failure
			}
			if err := os.MkdirAll(filepath.Dir(excluded.Path), 0o700); err != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "create excluded fixture parent")
			}
			if failure := prepareFixtureFile(plan.context, excluded.Path, target.Coordinate); failure != nil {
				return failure
			}
			if err := os.WriteFile(excluded.Path, []byte(excluded.Captured), 0o600); err != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "write excluded fixture")
			}
		}
	} else {
		for _, excluded := range target.RestoreExcluded {
			if failure := prepareFixtureFile(plan.context, excluded.Path, target.Coordinate); failure != nil {
				return failure
			}
			if err := os.MkdirAll(filepath.Dir(excluded.Path), 0o700); err != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "create restore-excluded fixture parent")
			}
			if err := os.WriteFile(excluded.Path, []byte(excluded.Mutated), 0o600); err != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "write restore-excluded fixture")
			}
		}
	}
	return nil
}

func (plan *FixturePlan) CompareCaptureSeed() *Failure {
	if failure := plan.compare("captured", true); failure != nil {
		return failure
	}
	return plan.compareRegistry("captured")
}

func (plan *FixturePlan) CompareOptionalAbsent() *Failure {
	if plan == nil || plan.context == nil {
		return fail(CodeIsolationFailure, "fixture", "optional", "fixture authority is absent")
	}
	for _, target := range plan.Targets {
		if target.Optional {
			if err := plan.context.ValidateSandboxPath(target.Resolved); err != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "optional fixture target left validation authority")
			}
			info, err := os.Lstat(target.Resolved)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() && !info.IsDir() {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "optional fixture target cannot be checked safely")
			}
			return fail(CodeContentMismatch, "fixture", target.Coordinate, "optional fixture target is present after optional-absence capture")
		}
	}
	for _, target := range plan.RegistryTargets {
		if target.Optional {
			if plan.registryFixture == nil || plan.registryFixture.ProveAbsent(target.Authored) != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "registry fixture operation failed")
			}
		}
	}
	retained := *plan
	retained.Targets = make([]FixtureTarget, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		if !target.Optional {
			retained.Targets = append(retained.Targets, target)
		}
	}
	retained.RegistryTargets = make([]RegistryFixtureTarget, 0, len(plan.RegistryTargets))
	for _, target := range plan.RegistryTargets {
		if !target.Optional {
			retained.RegistryTargets = append(retained.RegistryTargets, target)
		}
	}
	return retained.CompareCaptureSeed()
}

func (plan *FixturePlan) CompareCaptured() *Failure {
	if failure := plan.compare("captured", false); failure != nil {
		return failure
	}
	return plan.compareRegistry("captured")
}

func (plan *FixturePlan) CompareRestored() *Failure {
	if failure := plan.compare("restored", false); failure != nil {
		return failure
	}
	return plan.compareRegistry("restored")
}

func (plan *FixturePlan) CompareMutated() *Failure {
	if failure := plan.compare("mutated", false); failure != nil {
		return failure
	}
	return plan.compareRegistry("mutated")
}

func (plan *FixturePlan) compareRegistry(state string) *Failure {
	for index := range plan.RegistryTargets {
		target := &plan.RegistryTargets[index]
		if plan.registryFixture == nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "registry fixture authority is absent")
		}
		actual, err := plan.registryFixture.Snapshot(target.Authored)
		if err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "registry fixture operation failed")
		}
		expected := target.Mutated
		if state == "captured" {
			expected = target.Captured
		} else if state == "restored" {
			expected = target.Restored
		}
		if !actual.Equal(expected) {
			return fail(CodeContentMismatch, "fixture", target.Coordinate, "registry fixture state differs from the exact deterministic state")
		}
	}
	return nil
}

type expectedFixtureEntry struct {
	Directory bool
	Content   string
}

func (plan *FixturePlan) compare(state string, includeExcluded bool) *Failure {
	for _, target := range plan.Targets {
		if err := plan.context.ValidateSandboxPath(target.Resolved); err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture target left validation authority before comparison")
		}
		rootInfo, err := os.Lstat(target.Resolved)
		if err != nil || safepath.IsLinkOrReparse(rootInfo) || rootInfo.IsDir() != target.Directory || !rootInfo.Mode().IsRegular() && !rootInfo.IsDir() {
			return fail(CodeContentMismatch, "fixture", target.Coordinate, "fixture root changed type before comparison")
		}
		want := target.Mutated
		if state == "captured" {
			want = target.Captured
		} else if state == "restored" {
			want = target.Restored
		}
		expected := map[string]expectedFixtureEntry{".": {Directory: target.Directory}}
		if target.Directory {
			relative, ok := targetPayloadRelative(target)
			if !ok {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "directory payload left fixture authority")
			}
			expected[filepath.FromSlash(relative)] = expectedFixtureEntry{Content: want}
			for parent := filepath.Dir(filepath.FromSlash(relative)); parent != "."; parent = filepath.Dir(parent) {
				expected[parent] = expectedFixtureEntry{Directory: true}
			}
		} else {
			expected["."] = expectedFixtureEntry{Content: want}
		}
		if includeExcluded {
			all := append(append(append([]FixtureExcluded(nil), target.CaptureExcluded...), target.RestoreExcluded...), target.OverlappingExcluded...)
			for _, excluded := range all {
				relative, err := filepath.Rel(target.Resolved, excluded.Path)
				if err != nil || relative == "." || !fixtureContained(target.Resolved, excluded.Path) {
					return fail(CodeIsolationFailure, "fixture", target.Coordinate, "excluded witness left fixture authority")
				}
				expected[relative] = expectedFixtureEntry{Content: excluded.Captured}
				for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
					expected[parent] = expectedFixtureEntry{Directory: true}
				}
			}
		} else {
			for _, excluded := range target.RestoreExcluded {
				relative, err := filepath.Rel(target.Resolved, excluded.Path)
				if err != nil || relative == "." || !fixtureContained(target.Resolved, excluded.Path) {
					return fail(CodeIsolationFailure, "fixture", target.Coordinate, "restore-excluded witness left fixture authority")
				}
				expected[relative] = expectedFixtureEntry{Content: excluded.Mutated}
				for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
					expected[parent] = expectedFixtureEntry{Directory: true}
				}
			}
		}
		if state == "restored" {
			for _, retained := range target.RetainedCaptureAncestorDirs {
				expected[filepath.FromSlash(retained)] = expectedFixtureEntry{Directory: true}
			}
		}
		seen := make(map[string]struct{}, len(expected))
		err = filepath.Walk(target.Resolved, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := plan.context.ValidateSandboxPath(path); err != nil || !fixtureContained(target.Resolved, path) {
				return fmt.Errorf("fixture member left authority")
			}
			if safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() && !info.IsDir() {
				return fmt.Errorf("fixture member is linked or special")
			}
			relative, err := filepath.Rel(target.Resolved, path)
			if err != nil {
				return err
			}
			entry, exists := expected[relative]
			if !exists || entry.Directory != info.IsDir() {
				return fmt.Errorf("fixture tree differs")
			}
			if !info.IsDir() {
				data, _, err := safepath.ReadRegularFile(path)
				if err != nil || string(data) != entry.Content {
					return fmt.Errorf("fixture content differs")
				}
			}
			seen[relative] = struct{}{}
			return nil
		})
		if err != nil || len(seen) != len(expected) {
			return fail(CodeContentMismatch, "fixture", target.Coordinate, "fixture tree, type, or content differs from the exact deterministic state")
		}
	}
	return nil
}

func (plan *FixturePlan) removeTarget(target *FixtureTarget) *Failure {
	if err := plan.context.ValidateSandboxPath(target.Resolved); err != nil {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture target left validation authority")
	}
	paths, err := fixtureTreePostorder(target.Resolved)
	if err != nil {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture target contains a linked or special member")
	}
	return plan.removeFixtureEntries(target, paths)
}

type fixtureRemovalEntry struct {
	Path      string
	Directory bool
}

func (plan *FixturePlan) removeFixtureEntries(target *FixtureTarget, entries []fixtureRemovalEntry) *Failure {
	for _, entry := range entries {
		if err := plan.context.ValidateSandboxPath(entry.Path); err != nil || !fixtureContained(target.Resolved, entry.Path) {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture member left validation authority")
		}
		info, err := os.Lstat(entry.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || safepath.IsLinkOrReparse(info) || info.IsDir() != entry.Directory || !info.Mode().IsRegular() && !info.IsDir() {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture member changed type before removal")
		}
		if err := os.Remove(entry.Path); err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "remove guarded fixture member")
		}
	}
	return nil
}

func fixtureTreePostorder(root string) ([]fixtureRemovalEntry, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() && !info.IsDir() {
		return nil, fmt.Errorf("unsafe fixture root")
	}
	if !info.IsDir() {
		return []fixtureRemovalEntry{{Path: root}}, nil
	}
	var paths []fixtureRemovalEntry
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("unsafe fixture member")
		}
		paths = append(paths, fixtureRemovalEntry{Path: path, Directory: info.IsDir()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(paths)-1; left < right; left, right = left+1, right-1 {
		paths[left], paths[right] = paths[right], paths[left]
	}
	return paths, nil
}

func fixtureContained(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func prepareFixtureFile(context *validationmode.Context, path, coordinate string) *Failure {
	if err := context.ValidateSandboxPath(path); err != nil {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture file left validation authority")
	}
	parent := filepath.Dir(path)
	if err := context.ValidateSandboxPath(parent); err != nil {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture parent left validation authority")
	}
	if info, err := os.Lstat(parent); err == nil && (safepath.IsLinkOrReparse(info) || !info.IsDir()) {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture parent is linked or not a directory")
	} else if err != nil && !os.IsNotExist(err) {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture parent cannot be inspected")
	}
	if info, err := os.Lstat(path); err == nil && (safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular()) {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture file is linked or not regular")
	} else if err != nil && !os.IsNotExist(err) {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture file cannot be inspected")
	}
	return nil
}

func prepareRetainedCaptureAncestorDirectory(context *validationmode.Context, path, coordinate string) *Failure {
	if err := context.ValidateSandboxPath(path); err != nil {
		return fail(CodeIsolationFailure, "fixture", coordinate, "retained capture ancestor left validation authority")
	}
	parent := filepath.Dir(path)
	if err := context.ValidateSandboxPath(parent); err != nil {
		return fail(CodeIsolationFailure, "fixture", coordinate, "retained capture ancestor parent left validation authority")
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || safepath.IsLinkOrReparse(parentInfo) || !parentInfo.IsDir() {
		return fail(CodeIsolationFailure, "fixture", coordinate, "retained capture ancestor parent is linked or not a directory")
	}
	info, err := os.Lstat(path)
	if err == nil {
		if safepath.IsLinkOrReparse(info) || !info.IsDir() {
			return fail(CodeIsolationFailure, "fixture", coordinate, "retained capture ancestor is linked or not a directory")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fail(CodeIsolationFailure, "fixture", coordinate, "retained capture ancestor cannot be inspected")
	}
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return fail(CodeIsolationFailure, "fixture", coordinate, "create retained capture ancestor")
	}
	info, err = os.Lstat(path)
	if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
		return fail(CodeIsolationFailure, "fixture", coordinate, "retained capture ancestor changed type during creation")
	}
	return nil
}
