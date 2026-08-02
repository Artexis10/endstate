//go:build !windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"testing"
)

func TestRegistryFixtureIsFailClosedOffWindows(t *testing.T) {
	fixture := &RegistryFixture{}
	state, err := NewRegistryState(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		fixture.Materialize(`HKCU\Software\Fixture`),
		fixture.Replace(`HKCU\Software\Fixture`, state),
		fixture.Remove(`HKCU\Software\Fixture`),
		fixture.ProveAbsent(`HKCU\Software\Fixture`),
	} {
		if !errors.Is(err, ErrUnsafeRegistry) {
			t.Fatalf("operation error = %v, want ErrUnsafeRegistry", err)
		}
	}
	if _, err := fixture.Snapshot(`HKCU\Software\Fixture`); !errors.Is(err, ErrUnsafeRegistry) {
		t.Fatalf("Snapshot() error = %v, want ErrUnsafeRegistry", err)
	}
}
