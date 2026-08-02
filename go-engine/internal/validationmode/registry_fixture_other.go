//go:build !windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import "fmt"

type RegistryFixture struct{}

func NewRegistryFixture(*Context) (*RegistryFixture, error) {
	return nil, fmt.Errorf("%w: registry fixtures are unsupported", ErrUnsafeRegistry)
}

func (*RegistryFixture) Materialize(string) error {
	return fmt.Errorf("%w: registry fixtures are unsupported", ErrUnsafeRegistry)
}

func (*RegistryFixture) Replace(string, RegistryState) error {
	return fmt.Errorf("%w: registry fixtures are unsupported", ErrUnsafeRegistry)
}

func (*RegistryFixture) Snapshot(string) (RegistryState, error) {
	return RegistryState{}, fmt.Errorf("%w: registry fixtures are unsupported", ErrUnsafeRegistry)
}

func (*RegistryFixture) Remove(string) error {
	return fmt.Errorf("%w: registry fixtures are unsupported", ErrUnsafeRegistry)
}

func (*RegistryFixture) ProveAbsent(string) error {
	return fmt.Errorf("%w: registry fixtures are unsupported", ErrUnsafeRegistry)
}

func (*RegistryFixture) Cleanup() error { return nil }
