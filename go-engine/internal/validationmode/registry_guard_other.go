//go:build !windows

// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import "fmt"

type RegistryGuard struct{}

func NewRegistryGuard(*Context) *RegistryGuard { return &RegistryGuard{} }
func (*RegistryGuard) Protect(values []ProtectedRegistry) error {
	if len(values) == 0 {
		return nil
	}
	return fmt.Errorf("%w: registry guard is unsupported", ErrUnsafeRegistry)
}
func (*RegistryGuard) Seal()                            {}
func (*RegistryGuard) Check() ([]RegistryChange, error) { return []RegistryChange{}, nil }
