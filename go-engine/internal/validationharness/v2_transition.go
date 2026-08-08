// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type v2ValidationPackageState struct {
	SchemaVersion int    `json:"schemaVersion"`
	Driver        string `json:"driver"`
	Ref           string `json:"ref"`
	Source        string `json:"source,omitempty"`
	Present       bool   `json:"present"`
	Version       string `json:"version,omitempty"`
}

type v2PinnedJSON[T any] struct {
	value  T
	bytes  []byte
	digest [sha256.Size]byte
}

type v2VersionTransition struct {
	root            string
	descriptorPath  string
	statePath       string
	oldDescriptor   v2PinnedJSON[validationmode.Descriptor]
	newDescriptor   v2PinnedJSON[validationmode.Descriptor]
	oldState        v2PinnedJSON[v2ValidationPackageState]
	newState        v2PinnedJSON[v2ValidationPackageState]
	bundlePath      string
	bundle          boundaryEntry
	applied         bool
	writeDescriptor func(string, []byte, os.FileMode) error
	removeState     func(string) error
	guardBundle     func(string, boundaryEntry) *Failure
}

func compileV2VersionTransition(
	root string,
	scenario validationmatrix.Scenario,
	compiled v2CompiledFixture,
	mod *modules.Module,
	expectedInventory validationmode.Inventory,
	expectedNonce string,
	descriptor validationmode.Descriptor,
	descriptorBytes []byte,
) (*v2VersionTransition, *Failure) {
	bad := func(coordinate, detail string) (*v2VersionTransition, *Failure) {
		return nil, fail(CodeMigrationContract, "transition", coordinate, detail)
	}
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || safepath.ValidateRoot(root) != nil {
		return nil, fail(CodeIsolationFailure, "transition", "root", "migration transition root is outside validation authority")
	}
	if scenario.Mode != validationmatrix.ScenarioConfigMigrationV2 || scenario.Expected == nil || compiled.Migration == nil ||
		compiled.Definition.SourceVersion == "" || compiled.Definition.TargetVersion == "" ||
		compiled.Definition.SourceVersion == compiled.Definition.TargetVersion ||
		scenario.Expected.MigrationFrom != compiled.Generation.ID || scenario.Expected.MigrationTo != compiled.TargetGeneration.ID ||
		compiled.Migration.From != compiled.Generation.ID || compiled.Migration.To != compiled.TargetGeneration.ID ||
		compiled.Generation.Order >= compiled.TargetGeneration.Order {
		return bad("scenario", "transition does not bind the authored forward migration scenario")
	}
	if mod == nil || mod.ID == "" || expectedNonce == "" || mod.ID != compiled.ModuleID || mod.Revision != compiled.ModuleRevision {
		return bad("descriptor", "source module, inventory, or nonce authority differs from production selection")
	}
	productionInventory := validationInventory(mod)
	productionInventory.Version = compiled.Definition.SourceVersion
	if !reflect.DeepEqual(expectedInventory, productionInventory) {
		return bad("descriptor", "source module, inventory, or nonce authority differs from production selection")
	}
	expectedDescriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: scenario.ID, Nonce: expectedNonce, ModuleID: mod.ID, Inventory: expectedInventory,
	}
	if !reflect.DeepEqual(descriptor, expectedDescriptor) {
		return bad("descriptor", "source descriptor identity, presence, or version differs from migration authority")
	}
	wantOldDescriptorBytes, err := json.Marshal(expectedDescriptor)
	if err != nil || !bytes.Equal(descriptorBytes, wantOldDescriptorBytes) || sha256.Sum256(descriptorBytes) != sha256.Sum256(wantOldDescriptorBytes) {
		return bad("descriptor", "source descriptor bytes are not the exact prepared authority")
	}
	newDescriptor := expectedDescriptor
	newDescriptor.Inventory.Version = compiled.Definition.TargetVersion
	newDescriptorBytes, err := json.Marshal(newDescriptor)
	if err != nil || bytes.Equal(newDescriptorBytes, descriptorBytes) {
		return bad("descriptor", "target descriptor bytes do not encode one exact version transition")
	}
	oldState := v2PackageStateForInventory(expectedInventory)
	newState := v2PackageStateForInventory(newDescriptor.Inventory)
	oldStateBytes, err := marshalV2PackageState(oldState)
	if err != nil {
		return bad("packageState", "source package state cannot be pinned")
	}
	newStateBytes, err := marshalV2PackageState(newState)
	if err != nil || bytes.Equal(oldStateBytes, newStateBytes) {
		return bad("packageState", "target package state does not encode one exact version transition")
	}
	descriptorPath, err := safepath.Resolve(root, ".endstate/validation-mode.json")
	if err != nil {
		return nil, fail(CodeIsolationFailure, "transition", "descriptor", "descriptor path left validation authority")
	}
	statePath, err := safepath.Resolve(root, ".endstate/validation-package-state.json")
	if err != nil || !fixtureContained(root, descriptorPath) || !fixtureContained(root, statePath) {
		return nil, fail(CodeIsolationFailure, "transition", "packageState", "package-state path left validation authority")
	}
	return &v2VersionTransition{
		root: root, descriptorPath: descriptorPath, statePath: statePath,
		oldDescriptor:   pinV2JSON(expectedDescriptor, wantOldDescriptorBytes),
		newDescriptor:   pinV2JSON(newDescriptor, newDescriptorBytes),
		oldState:        pinV2JSON(oldState, oldStateBytes),
		newState:        pinV2JSON(newState, newStateBytes),
		writeDescriptor: safepath.AtomicWriteFile,
		removeState:     os.Remove,
		guardBundle:     validateV2BundleBoundary,
	}, nil
}

func v2PackageStateForInventory(inventory validationmode.Inventory) v2ValidationPackageState {
	return v2ValidationPackageState{
		SchemaVersion: 1, Driver: inventory.Driver, Ref: inventory.Ref, Source: inventory.Source,
		Present: inventory.InitialState == "present", Version: inventory.Version,
	}
}

func marshalV2PackageState(state v2ValidationPackageState) ([]byte, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func pinV2JSON[T any](value T, data []byte) v2PinnedJSON[T] {
	copyOfBytes := append([]byte(nil), data...)
	return v2PinnedJSON[T]{value: value, bytes: copyOfBytes, digest: sha256.Sum256(copyOfBytes)}
}

func (transition *v2VersionTransition) Apply(bundlePath string) *Failure {
	if transition == nil || transition.applied {
		return fail(CodeMigrationContract, "transition", "state", "migration transition is absent or repeated")
	}
	bundle, failure := transition.bindBundle(bundlePath)
	if failure != nil {
		return failure
	}
	if failure := validatePinnedV2JSON(transition.descriptorPath, transition.oldDescriptor, "descriptor"); failure != nil {
		return failure
	}
	if failure := validatePinnedV2JSON(transition.statePath, transition.oldState, "packageState"); failure != nil {
		return failure
	}
	writeDescriptor := transition.writeDescriptor
	if writeDescriptor == nil {
		writeDescriptor = safepath.AtomicWriteFile
	}
	if err := writeDescriptor(transition.descriptorPath, transition.newDescriptor.bytes, 0o600); err != nil {
		return transition.rollback(fail(CodeMigrationContract, "transition", "descriptor", "target descriptor could not be atomically installed"))
	}
	if failure := validatePinnedV2JSON(transition.descriptorPath, transition.newDescriptor, "descriptor"); failure != nil {
		return transition.rollback(failure)
	}
	// Re-read immediately before the only deletion so a swapped package-state
	// leaf cannot be removed under an earlier identity check.
	if failure := validatePinnedV2JSON(transition.statePath, transition.oldState, "packageState"); failure != nil {
		return transition.rollback(failure)
	}
	removeState := transition.removeState
	if removeState == nil {
		removeState = os.Remove
	}
	if err := removeState(transition.statePath); err != nil {
		return transition.rollback(fail(CodeMigrationContract, "transition", "packageState", "exact source package state could not be safely removed"))
	}
	if _, err := os.Lstat(transition.statePath); !os.IsNotExist(err) {
		return transition.rollback(fail(CodeMigrationContract, "transition", "packageState", "source package state still exists after exact removal"))
	}
	guardBundle := transition.guardBundle
	if guardBundle == nil {
		guardBundle = validateV2BundleBoundary
	}
	if failure := guardBundle(bundlePath, bundle); failure != nil {
		return transition.rollback(failure)
	}
	transition.applied = true
	return nil
}

func (transition *v2VersionTransition) bindBundle(bundlePath string) (boundaryEntry, *Failure) {
	if transition.bundlePath == "" {
		bundle, failure := transition.snapshotBundle(bundlePath)
		if failure != nil {
			return boundaryEntry{}, failure
		}
		transition.bundlePath, transition.bundle = bundlePath, bundle
		return bundle, nil
	}
	if filepath.Clean(bundlePath) != transition.bundlePath {
		return boundaryEntry{}, fail(CodeMigrationContract, "transition", "bundle", "migration bundle differs from the first exact transition attempt")
	}
	if failure := validateV2BundleBoundary(bundlePath, transition.bundle); failure != nil {
		return boundaryEntry{}, failure
	}
	return transition.bundle, nil
}

func (transition *v2VersionTransition) rollback(primary *Failure) *Failure {
	descriptorErr := safepath.AtomicWriteFile(transition.descriptorPath, transition.oldDescriptor.bytes, 0o600)
	stateErr := safepath.AtomicWriteFile(transition.statePath, transition.oldState.bytes, 0o600)
	descriptorFailure := validatePinnedV2JSON(transition.descriptorPath, transition.oldDescriptor, "descriptor")
	stateFailure := validatePinnedV2JSON(transition.statePath, transition.oldState, "packageState")
	if descriptorErr != nil || stateErr != nil || descriptorFailure != nil || stateFailure != nil {
		return fail(CodeMigrationContract, "transition", "state", "failed transition could not restore the exact source descriptor and package state")
	}
	return primary
}

func (transition *v2VersionTransition) ValidateReinitialized(bundlePath string) *Failure {
	if transition == nil || !transition.applied || filepath.Clean(bundlePath) != transition.bundlePath {
		return fail(CodeMigrationContract, "transition", "state", "migration transition was not applied exactly once for this bundle")
	}
	if failure := validatePinnedV2JSON(transition.descriptorPath, transition.newDescriptor, "descriptor"); failure != nil {
		return failure
	}
	if failure := validatePinnedV2JSON(transition.statePath, transition.newState, "packageState"); failure != nil {
		return failure
	}
	return validateV2BundleBoundary(bundlePath, transition.bundle)
}

func (transition *v2VersionTransition) ValidateBundle(bundlePath string) *Failure {
	if transition == nil || !transition.applied || filepath.Clean(bundlePath) != transition.bundlePath {
		return fail(CodeMigrationContract, "transition", "bundle", "migration bundle boundary is not initialized")
	}
	return validateV2BundleBoundary(bundlePath, transition.bundle)
}

func (transition *v2VersionTransition) snapshotBundle(bundlePath string) (boundaryEntry, *Failure) {
	if bundlePath == "" || !filepath.IsAbs(bundlePath) || filepath.Clean(bundlePath) != bundlePath || !fixtureContained(transition.root, bundlePath) {
		return boundaryEntry{}, fail(CodeIsolationFailure, "transition", "bundle", "captured bundle left validation authority")
	}
	entry, err := snapshotBoundaryFile(bundlePath)
	if err != nil {
		return boundaryEntry{}, fail(CodeMigrationContract, "transition", "bundle", "captured bundle is absent or unsafe")
	}
	return entry, nil
}

func validateV2BundleBoundary(path string, expected boundaryEntry) *Failure {
	actual, err := snapshotBoundaryFile(path)
	if err != nil || boundaryEntryDifference(expected, actual) != "" {
		return fail(CodeMigrationContract, "transition", "bundle", "captured bundle changed across the disposable host transition")
	}
	return nil
}

func validatePinnedV2JSON[T any](path string, expected v2PinnedJSON[T], coordinate string) *Failure {
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil || !bytes.Equal(data, expected.bytes) || sha256.Sum256(data) != expected.digest || rejectDuplicateJSONFields(data) != nil {
		return fail(CodeMigrationContract, "transition", coordinate, "pinned JSON bytes or hash differ from exact transition authority")
	}
	var decoded T
	if strictV2JSON(data, &decoded) != nil || !reflect.DeepEqual(decoded, expected.value) {
		return fail(CodeMigrationContract, "transition", coordinate, "pinned JSON identity or fields differ from exact transition authority")
	}
	return nil
}
