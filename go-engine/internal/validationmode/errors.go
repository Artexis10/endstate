// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package validationmode provides the fail-closed, disposable host boundary
// used by production-engine validation scenarios.
package validationmode

import "errors"

var (
	ErrInvalidActivation = errors.New("invalid validation-mode activation")
	ErrUnsafeRoot        = errors.New("unsafe validation root")
	ErrInvalidDescriptor = errors.New("invalid validation-mode descriptor")
	ErrUnsafePath        = errors.New("unsafe validation path")
	ErrUnsafeRegistry    = errors.New("unsafe validation registry key")
	ErrPackageIdentity   = errors.New("package identity is outside validation inventory")
	ErrInvalidState      = errors.New("invalid validation package state")
	ErrGuardOverlap      = errors.New("validation and protected paths overlap")
	ErrUnsafeGuardPath   = errors.New("unsafe filesystem guard path")
)
