// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type v2TransitionTestRig struct {
	transition                            *v2VersionTransition
	descriptorPath, statePath, bundlePath string
	oldDescriptor, oldState, newState     []byte
}

func TestCompileV2VersionTransitionRejectsSelfConsistentForeignDescriptorIdentity(t *testing.T) {
	root := t.TempDir()
	repo, mod, scenario := trackedV2Fixture(t, "apps.owncloud", "migration-preferences-g1-to-g2")
	compiled, failure := compileV2FixtureAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	inventory := validationInventory(mod)
	inventory.Version = compiled.Definition.SourceVersion
	nonce := "transition-authority"
	descriptor := validationmode.Descriptor{SchemaVersion: 1, ScenarioID: scenario.ID, Nonce: nonce, ModuleID: mod.ID, Inventory: inventory}
	tests := []struct {
		name   string
		mutate func(*validationmode.Descriptor)
	}{
		{name: "module id", mutate: func(value *validationmode.Descriptor) { value.ModuleID = "apps.foreign" }},
		{name: "app id", mutate: func(value *validationmode.Descriptor) { value.Inventory.AppID = "foreign" }},
		{name: "driver", mutate: func(value *validationmode.Descriptor) { value.Inventory.Driver = "chocolatey" }},
		{name: "ref", mutate: func(value *validationmode.Descriptor) { value.Inventory.Ref = "Foreign.Ref" }},
		{name: "source", mutate: func(value *validationmode.Descriptor) { value.Inventory.Source = "foreign" }},
		{name: "display name", mutate: func(value *validationmode.Descriptor) { value.Inventory.DisplayName = "Foreign" }},
		{name: "nonce", mutate: func(value *validationmode.Descriptor) { value.Nonce = "foreign-nonce" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := descriptor
			test.mutate(&candidate)
			candidateBytes, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, failure := compileV2VersionTransition(root, scenario, compiled, mod, inventory, nonce, candidate, candidateBytes); failure == nil || failure.Code != CodeMigrationContract || failure.Coordinate != "descriptor" {
				t.Fatalf("self-consistent foreign descriptor accepted: %+v", failure)
			}
		})
	}
	t.Run("foreign supplied module authority", func(t *testing.T) {
		foreignModule := *mod
		foreignModule.ID = "apps.foreign"
		foreignInventory := validationInventory(&foreignModule)
		foreignInventory.Version = compiled.Definition.SourceVersion
		candidate := descriptor
		candidate.ModuleID = foreignModule.ID
		candidate.Inventory = foreignInventory
		candidateBytes, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, failure := compileV2VersionTransition(root, scenario, compiled, &foreignModule, foreignInventory, nonce, candidate, candidateBytes); failure == nil || failure.Code != CodeMigrationContract || failure.Coordinate != "descriptor" {
			t.Fatalf("foreign module authority accepted: %+v", failure)
		}
	})
	t.Run("foreign supplied inventory authority", func(t *testing.T) {
		foreignInventory := inventory
		foreignInventory.AppID = "foreign"
		candidate := descriptor
		candidate.Inventory = foreignInventory
		candidateBytes, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, failure := compileV2VersionTransition(root, scenario, compiled, mod, foreignInventory, nonce, candidate, candidateBytes); failure == nil || failure.Code != CodeMigrationContract || failure.Coordinate != "descriptor" {
			t.Fatalf("foreign inventory authority accepted: %+v", failure)
		}
	})
}

func TestV2VersionTransitionRepinsExactDescriptorAndReinitializedState(t *testing.T) {
	root := t.TempDir()
	_, mod, scenario := trackedV2Fixture(t, "apps.owncloud", "migration-preferences-g1-to-g2")
	repo, _, _ := trackedV2Fixture(t, "apps.owncloud", "migration-preferences-g1-to-g2")
	compiled, failure := compileV2FixtureAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	inventory := validationInventory(mod)
	inventory.Version = compiled.Definition.SourceVersion
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: scenario.ID, Nonce: "transition-test", ModuleID: mod.ID, Inventory: inventory,
	}
	descriptorBytes, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	descriptorPath := filepath.Join(root, ".endstate", "validation-mode.json")
	statePath := filepath.Join(root, ".endstate", "validation-package-state.json")
	writeV2TestFile(t, descriptorPath, descriptorBytes)
	writeV2TestFile(t, statePath, v2TestPackageStateBytes(t, inventory))
	bundlePath := filepath.Join(root, "manifests", "captured.zip")
	bundleBytes := []byte("immutable captured bundle")
	writeV2TestFile(t, bundlePath, bundleBytes)

	transition, failure := compileV2VersionTransition(root, scenario, compiled, mod, inventory, descriptor.Nonce, descriptor, descriptorBytes)
	if failure != nil {
		t.Fatal(failure)
	}
	if failure := transition.Apply(bundlePath); failure != nil {
		t.Fatal(failure)
	}
	newDescriptorBytes, err := os.ReadFile(descriptorPath)
	if err != nil {
		t.Fatal(err)
	}
	var newDescriptor validationmode.Descriptor
	if err := strictV2JSON(newDescriptorBytes, &newDescriptor); err != nil {
		t.Fatal(err)
	}
	wantDescriptor := descriptor
	wantDescriptor.Inventory.Version = compiled.Definition.TargetVersion
	if !reflect.DeepEqual(newDescriptor, wantDescriptor) || bytes.Equal(newDescriptorBytes, descriptorBytes) {
		t.Fatalf("transition descriptor = %+v", newDescriptor)
	}
	if _, err := os.Lstat(statePath); !os.IsNotExist(err) {
		t.Fatalf("old package state still exists: %v", err)
	}
	writeV2TestFile(t, statePath, v2TestPackageStateBytes(t, wantDescriptor.Inventory))
	if failure := transition.ValidateReinitialized(bundlePath); failure != nil {
		t.Fatal(failure)
	}
	if after, err := os.ReadFile(bundlePath); err != nil || !bytes.Equal(after, bundleBytes) {
		t.Fatalf("bundle changed across transition: %q, %v", after, err)
	}
}

func TestV2VersionTransitionRejectsDescriptorAndPackageStateDriftBeforeMutation(t *testing.T) {
	tests := []struct {
		name       string
		coordinate string
		mutate     func(*testing.T, v2TransitionTestRig)
	}{
		{name: "descriptor bytes", coordinate: "descriptor", mutate: func(t *testing.T, rig v2TransitionTestRig) {
			writeV2TestFile(t, rig.descriptorPath, append(append([]byte(nil), rig.oldDescriptor...), ' '))
		}},
		{name: "missing package state", coordinate: "packageState", mutate: func(t *testing.T, rig v2TransitionTestRig) {
			if err := os.Remove(rig.statePath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "package state schema", coordinate: "packageState", mutate: mutateV2TransitionState("schemaVersion", float64(2))},
		{name: "package state driver", coordinate: "packageState", mutate: mutateV2TransitionState("driver", "foreign")},
		{name: "package state ref", coordinate: "packageState", mutate: mutateV2TransitionState("ref", "foreign")},
		{name: "package state source", coordinate: "packageState", mutate: mutateV2TransitionState("source", "foreign")},
		{name: "package state absent", coordinate: "packageState", mutate: mutateV2TransitionState("present", false)},
		{name: "package state version", coordinate: "packageState", mutate: mutateV2TransitionState("version", "2.5")},
		{name: "duplicate package state field", coordinate: "packageState", mutate: func(t *testing.T, rig v2TransitionTestRig) {
			writeV2TestFile(t, rig.statePath, []byte(strings.Replace(string(rig.oldState), `"version":"2.4"`, `"version":"2.4","version":"2.4"`, 1)))
		}},
		{name: "trailing package state JSON", coordinate: "packageState", mutate: func(t *testing.T, rig v2TransitionTestRig) {
			writeV2TestFile(t, rig.statePath, append(append([]byte(nil), rig.oldState...), []byte(`{}`)...))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rig := newV2TransitionTestRig(t)
			test.mutate(t, rig)
			if failure := rig.transition.Apply(rig.bundlePath); failure == nil || failure.Code != CodeMigrationContract || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v", failure)
			}
			descriptor, err := os.ReadFile(rig.descriptorPath)
			if err != nil {
				t.Fatal(err)
			}
			if test.coordinate != "descriptor" && !bytes.Equal(descriptor, rig.oldDescriptor) {
				t.Fatal("descriptor changed after rejected transition")
			}
		})
	}
}

func TestV2VersionTransitionRejectsRepeatUnsafeBundleMutationAndWrongReinit(t *testing.T) {
	t.Run("repeat", func(t *testing.T) {
		rig := newV2TransitionTestRig(t)
		if failure := rig.transition.Apply(rig.bundlePath); failure != nil {
			t.Fatal(failure)
		}
		writeV2TestFile(t, rig.statePath, rig.newState)
		if failure := rig.transition.Apply(rig.bundlePath); failure == nil || failure.Code != CodeMigrationContract {
			t.Fatalf("repeat failure = %+v", failure)
		}
	})
	t.Run("out of authority bundle", func(t *testing.T) {
		rig := newV2TransitionTestRig(t)
		foreign := filepath.Join(t.TempDir(), "captured.zip")
		writeV2TestFile(t, foreign, []byte("foreign"))
		if failure := rig.transition.Apply(foreign); failure == nil || failure.Code != CodeIsolationFailure || failure.Coordinate != "bundle" {
			t.Fatalf("foreign bundle failure = %+v", failure)
		}
	})
	t.Run("bundle mutation", func(t *testing.T) {
		rig := newV2TransitionTestRig(t)
		if failure := rig.transition.Apply(rig.bundlePath); failure != nil {
			t.Fatal(failure)
		}
		writeV2TestFile(t, rig.bundlePath, []byte("mutated bundle"))
		if failure := rig.transition.ValidateBundle(rig.bundlePath); failure == nil || failure.Code != CodeMigrationContract || failure.Coordinate != "bundle" {
			t.Fatalf("bundle mutation failure = %+v", failure)
		}
	})
	t.Run("wrong reinitialized version", func(t *testing.T) {
		rig := newV2TransitionTestRig(t)
		if failure := rig.transition.Apply(rig.bundlePath); failure != nil {
			t.Fatal(failure)
		}
		writeV2TestFile(t, rig.statePath, rig.oldState)
		if failure := rig.transition.ValidateReinitialized(rig.bundlePath); failure == nil || failure.Code != CodeMigrationContract || failure.Coordinate != "packageState" {
			t.Fatalf("wrong reinit failure = %+v", failure)
		}
	})
}

func TestV2VersionTransitionFailureRollsBackExactOldStateAndLaunchesNoFurtherChild(t *testing.T) {
	tests := []struct {
		name   string
		inject func(*testing.T, v2TransitionTestRig)
	}{
		{name: "descriptor write", inject: func(t *testing.T, rig v2TransitionTestRig) {
			rig.transition.writeDescriptor = func(path string, data []byte, mode os.FileMode) error {
				if err := safepath.AtomicWriteFile(path, []byte(`{"partial":true}`), mode); err != nil {
					t.Fatal(err)
				}
				return errors.New("injected descriptor write failure")
			}
		}},
		{name: "state removal", inject: func(t *testing.T, rig v2TransitionTestRig) {
			rig.transition.removeState = func(path string) error {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				return errors.New("injected state removal failure")
			}
		}},
		{name: "post-transition bundle guard", inject: func(t *testing.T, rig v2TransitionTestRig) {
			rig.transition.guardBundle = func(path string, expected boundaryEntry) *Failure {
				writeV2TestFile(t, path, []byte("mutated captured bundle"))
				return validateV2BundleBoundary(path, expected)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rig := newV2TransitionTestRig(t)
			test.inject(t, rig)
			executor := &v2JourneyExecutorFixture{
				captureEvidence: captureEvidence{ArtifactPath: rig.bundlePath},
				transition: func(evidence captureEvidence) *Failure {
					return rig.transition.Apply(evidence.ArtifactPath)
				},
			}
			runtime := &scenarioRuntime{
				Module:   &modules.Module{ID: "apps.owncloud"},
				Scenario: validationmatrix.Scenario{ID: "migration-preferences-g1-to-g2", Mode: validationmatrix.ScenarioConfigMigrationV2},
				V2Plan:   &V2FixturePlan{},
			}
			result := executeV2Journey(context.Background(), runtime, executor)
			if result.Failure == nil || result.Failure.Code != CodeMigrationContract || !reflect.DeepEqual(executor.calls, []string{"capture", "transition"}) {
				t.Fatalf("failure cutoff = result:%+v calls:%v", result, executor.calls)
			}
			assertV2TransitionOldState(t, rig)
			if test.name == "post-transition bundle guard" {
				if failure := rig.transition.Apply(rig.bundlePath); failure == nil || failure.Coordinate != "bundle" {
					t.Fatalf("corrupted bundle retry accepted: %+v", failure)
				}
				assertV2TransitionOldState(t, rig)
			}
		})
	}
}

func assertV2TransitionOldState(t *testing.T, rig v2TransitionTestRig) {
	t.Helper()
	descriptor, descriptorErr := os.ReadFile(rig.descriptorPath)
	state, stateErr := os.ReadFile(rig.statePath)
	if descriptorErr != nil || stateErr != nil || !bytes.Equal(descriptor, rig.oldDescriptor) || !bytes.Equal(state, rig.oldState) || rig.transition.applied {
		t.Fatalf("partial transition remained: descriptor=%q/%v state=%q/%v applied=%t", descriptor, descriptorErr, state, stateErr, rig.transition.applied)
	}
}

func newV2TransitionTestRig(t *testing.T) v2TransitionTestRig {
	t.Helper()
	root := t.TempDir()
	repo, mod, scenario := trackedV2Fixture(t, "apps.owncloud", "migration-preferences-g1-to-g2")
	compiled, failure := compileV2FixtureAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	inventory := validationInventory(mod)
	inventory.Version = compiled.Definition.SourceVersion
	descriptor := validationmode.Descriptor{SchemaVersion: 1, ScenarioID: scenario.ID, Nonce: "transition-adversarial", ModuleID: mod.ID, Inventory: inventory}
	descriptorBytes, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	descriptorPath := filepath.Join(root, ".endstate", "validation-mode.json")
	statePath := filepath.Join(root, ".endstate", "validation-package-state.json")
	oldState := v2TestPackageStateBytes(t, inventory)
	newInventory := inventory
	newInventory.Version = compiled.Definition.TargetVersion
	newState := v2TestPackageStateBytes(t, newInventory)
	bundlePath := filepath.Join(root, "manifests", "captured.zip")
	writeV2TestFile(t, descriptorPath, descriptorBytes)
	writeV2TestFile(t, statePath, oldState)
	writeV2TestFile(t, bundlePath, []byte("immutable captured bundle"))
	transition, failure := compileV2VersionTransition(root, scenario, compiled, mod, inventory, descriptor.Nonce, descriptor, descriptorBytes)
	if failure != nil {
		t.Fatal(failure)
	}
	return v2TransitionTestRig{transition: transition, descriptorPath: descriptorPath, statePath: statePath, bundlePath: bundlePath, oldDescriptor: descriptorBytes, oldState: oldState, newState: newState}
}

func mutateV2TransitionState(field string, value any) func(*testing.T, v2TransitionTestRig) {
	return func(t *testing.T, rig v2TransitionTestRig) {
		t.Helper()
		var state map[string]any
		if err := json.Unmarshal(rig.oldState, &state); err != nil {
			t.Fatal(err)
		}
		state[field] = value
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		writeV2TestFile(t, rig.statePath, append(data, '\n'))
	}
}

func v2TestPackageStateBytes(t *testing.T, inventory validationmode.Inventory) []byte {
	t.Helper()
	data, err := json.Marshal(v2ValidationPackageState{
		SchemaVersion: 1, Driver: inventory.Driver, Ref: inventory.Ref, Source: inventory.Source,
		Present: inventory.InitialState == "present", Version: inventory.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}
