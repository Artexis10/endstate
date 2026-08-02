//go:build windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRegistryFixtureSnapshotsRootAndNestedTypedValues(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-live")
	fixture, err := NewRegistryFixture(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fixture.Cleanup(); err != nil {
			t.Error(err)
		}
	})
	state, err := NewRegistryState([]RegistryKey{
		{Path: "", Values: []RegistryValue{{Name: "", Type: RegistryTypeString, Data: utf16RegistryString("root")}}},
		{Path: `Child`, Values: []RegistryValue{{Name: "Flag", Type: RegistryTypeDWORD, Data: []byte{1, 0, 0, 0}}, {Name: "Blob", Type: RegistryTypeBinary, Data: []byte{1, 2, 3}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Replace(`HKCU\Software\Fixture`, state); err != nil {
		t.Fatal(err)
	}
	got, err := fixture.Snapshot(`HKCU\Software\Fixture`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(state) {
		t.Fatalf("Snapshot() = %#v, want %#v", got, state)
	}
}

func TestRegistryFixtureReplacesAndSnapshotsTypedState(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-state")
	operations := &fakeRegistryFixtureOperations{}
	fixture := newRegistryFixtureWithOperations(context, operations)
	state, err := NewRegistryState([]RegistryKey{
		{Path: "", Values: []RegistryValue{{Name: "", Type: RegistryTypeString, Data: utf16RegistryString("root")}}},
		{Path: `Child`, Values: []RegistryValue{{Name: "Flag", Type: RegistryTypeDWORD, Data: []byte{1, 0, 0, 0}}, {Name: "Blob", Type: RegistryTypeBinary, Data: []byte{1, 2, 3}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Replace(`HKCU\Software\Fixture`, state); err != nil {
		t.Fatal(err)
	}
	if operations.replaceCalls != 1 || !operations.replaced.Equal(state) {
		t.Fatalf("Replace() calls/state = %d/%#v", operations.replaceCalls, operations.replaced)
	}
	operations.snapshot = state
	got, err := fixture.Snapshot(`HKCU\Software\Fixture`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(state) {
		t.Fatalf("Snapshot() = %#v, want %#v", got, state)
	}
}

func TestRegistryFixtureReplaceRemovesPreexistingExtraState(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-replace-exact")
	desired, err := NewRegistryState([]RegistryKey{{Path: "", Values: []RegistryValue{{Name: "Setting", Type: RegistryTypeDWORD, Data: []byte{1, 0, 0, 0}}}}})
	if err != nil {
		t.Fatal(err)
	}
	extra, err := NewRegistryState([]RegistryKey{
		{Path: "", Values: []RegistryValue{{Name: "Setting", Type: RegistryTypeDWORD, Data: []byte{9, 0, 0, 0}}, {Name: "Unexpected", Type: RegistryTypeBinary, Data: []byte{1}}}},
		{Path: "Legacy", Values: []RegistryValue{{Name: "Value", Type: RegistryTypeBinary, Data: []byte{2}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	operations := &fakeRegistryFixtureOperations{snapshot: extra}
	fixture := newRegistryFixtureWithOperations(context, operations)
	if err := fixture.Replace(`HKCU\Software\Fixture`, desired); err != nil {
		t.Fatal(err)
	}
	got, err := fixture.Snapshot(`HKCU\Software\Fixture`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(desired) || got.Equal(extra) {
		t.Fatalf("replacement snapshot = %#v, want exact desired state %#v", got, desired)
	}
}

func TestRegistryFixtureRejectsForeignIdentityBeforeBackendCalls(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-reject")
	operations := &fakeRegistryFixtureOperations{}
	fixture := newRegistryFixtureWithOperations(context, operations)
	state, err := NewRegistryState([]RegistryKey{{}})
	if err != nil {
		t.Fatal(err)
	}
	foreign := strings.Replace(context.RegistryNamespace(), context.Descriptor().Nonce, "foreign", 1) + `\Software\Fixture`
	for _, authored := range []string{`HKLM\Software\Fixture`, foreign} {
		if err := fixture.Replace(authored, state); !errors.Is(err, ErrUnsafeRegistry) {
			t.Fatalf("Replace(%q) error = %v, want ErrUnsafeRegistry", authored, err)
		}
	}
	if operations.calls() != 0 {
		t.Fatalf("backend calls = %d, want 0", operations.calls())
	}
}

func TestRegistryFixtureCleanupAlwaysProbesAbsence(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-cleanup")
	deleteErr := errors.New("delete failed")
	operations := &fakeRegistryFixtureOperations{removeErr: deleteErr}
	fixture := newRegistryFixtureWithOperations(context, operations)
	if err := fixture.Cleanup(); !errors.Is(err, deleteErr) {
		t.Fatalf("Cleanup() error = %v, want delete error", err)
	}
	if operations.existsCalls != 1 {
		t.Fatalf("Cleanup() probes = %d, want 1", operations.existsCalls)
	}

	probeErr := errors.New("probe failed")
	operations = &fakeRegistryFixtureOperations{existsErr: probeErr}
	fixture = newRegistryFixtureWithOperations(context, operations)
	if err := fixture.Cleanup(); !errors.Is(err, probeErr) {
		t.Fatalf("Cleanup() error = %v, want probe error", err)
	}

	operations = &fakeRegistryFixtureOperations{fixtureExists: true}
	fixture = newRegistryFixtureWithOperations(context, operations)
	if err := fixture.Cleanup(); err == nil {
		t.Fatal("Cleanup() succeeded while namespace remains")
	}

	operations = &fakeRegistryFixtureOperations{}
	fixture = newRegistryFixtureWithOperations(context, operations)
	if err := fixture.Cleanup(); err != nil {
		t.Fatalf("idempotent Cleanup(): %v", err)
	}
}

func TestRegistryFixtureRemovesAndProvesAuthoredKeyAbsent(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-absence")
	operations := &fakeRegistryFixtureOperations{}
	fixture := newRegistryFixtureWithOperations(context, operations)
	if err := fixture.Remove(`HKCU\Software\Fixture`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.ProveAbsent(`HKCU\Software\Fixture`); err != nil {
		t.Fatal(err)
	}
	if operations.removeCalls != 1 || operations.existsCalls != 1 {
		t.Fatalf("remove/probe calls = %d/%d, want 1/1", operations.removeCalls, operations.existsCalls)
	}
	operations.fixtureExists = true
	if err := fixture.ProveAbsent(`HKCU\Software\Fixture`); err == nil {
		t.Fatal("ProveAbsent() succeeded while key remains")
	}
}

func TestRegistryFixturePropagatesAccessDeniedWithoutSkip(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-denied")
	state, err := NewRegistryState([]RegistryKey{{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		operations *fakeRegistryFixtureOperations
		call       func(*RegistryFixture) error
	}{
		{"materialize", &fakeRegistryFixtureOperations{ensureErr: windows.ERROR_ACCESS_DENIED}, func(fixture *RegistryFixture) error { return fixture.Materialize(`HKCU\Software\Fixture`) }},
		{"replace", &fakeRegistryFixtureOperations{replaceErr: windows.ERROR_ACCESS_DENIED}, func(fixture *RegistryFixture) error { return fixture.Replace(`HKCU\Software\Fixture`, state) }},
		{"snapshot", &fakeRegistryFixtureOperations{snapshotErr: windows.ERROR_ACCESS_DENIED}, func(fixture *RegistryFixture) error { _, err := fixture.Snapshot(`HKCU\Software\Fixture`); return err }},
		{"remove", &fakeRegistryFixtureOperations{removeErr: windows.ERROR_ACCESS_DENIED}, func(fixture *RegistryFixture) error { return fixture.Remove(`HKCU\Software\Fixture`) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRegistryFixtureWithOperations(context, test.operations)
			if err := test.call(fixture); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Fatalf("operation error = %v, want access denied", err)
			}
		})
	}
}

func TestReadRegistryValuesSkipsEnumeratedDefault(t *testing.T) {
	values, err := collectRegistryValues([]string{"", "Named"}, func(name string) (RegistryValue, bool, error) {
		return RegistryValue{Name: name, Type: RegistryTypeBinary, Data: []byte(name)}, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].Name != "" || values[1].Name != "Named" {
		t.Fatalf("values = %#v, want one default and one named value", values)
	}
}

type fakeRegistryFixtureOperations struct {
	ensureErr, replaceErr, snapshotErr, removeErr, existsErr error
	fixtureExists                                            bool
	snapshot, replaced                                       RegistryState
	ensureCalls, replaceCalls, snapshotCalls                 int
	removeCalls, existsCalls                                 int
}

func (operations *fakeRegistryFixtureOperations) ensure(string) error {
	operations.ensureCalls++
	return operations.ensureErr
}

func (operations *fakeRegistryFixtureOperations) replace(_ string, state RegistryState) error {
	operations.replaceCalls++
	if operations.replaceErr == nil {
		operations.replaced = state
		operations.snapshot = state
	}
	return operations.replaceErr
}

func (operations *fakeRegistryFixtureOperations) read(string) (RegistryState, error) {
	operations.snapshotCalls++
	return operations.snapshot, operations.snapshotErr
}

func (operations *fakeRegistryFixtureOperations) remove(string) error {
	operations.removeCalls++
	return operations.removeErr
}

func (operations *fakeRegistryFixtureOperations) exists(string) (bool, error) {
	operations.existsCalls++
	return operations.fixtureExists, operations.existsErr
}

func (operations *fakeRegistryFixtureOperations) calls() int {
	return operations.ensureCalls + operations.replaceCalls + operations.snapshotCalls + operations.removeCalls + operations.existsCalls
}
