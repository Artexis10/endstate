// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package dconf

import (
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
)

// Reason says why the dconf boundary refused. Each value appears in error text
// so an operator can act on it; callers branch on the desktopsettings sentinels.
type Reason string

const (
	ReasonRuntimeUnavailable  Reason = "desktop_runtime_unavailable"
	ReasonSchemaUnavailable   Reason = "desktop_schema_unavailable"
	ReasonProfileUnsupported  Reason = "desktop_profile_unsupported"
	ReasonScopeUnproven       Reason = "desktop_scope_unproven"
	ReasonDatabaseUnavailable Reason = "desktop_database_unavailable"
	ReasonValueUnsupported    Reason = "desktop_value_unsupported"
)

// Error is a refusal that never mutated the desktop. It matches
// desktopsettings.ErrUnsupported for a stored value outside the contract and
// desktopsettings.ErrUnavailable for every other reason.
type Error struct {
	Reason      Reason
	Detail      string
	Remediation string
	Err         error
}

func (e *Error) Error() string {
	text := string(e.Reason) + ": " + e.Detail
	if e.Remediation != "" {
		text += " (" + e.Remediation + ")"
	}
	return text
}
func (e *Error) Unwrap() error { return e.Err }
func (e *Error) Is(target error) bool {
	if e.Reason == ReasonValueUnsupported {
		return target == desktopsettings.ErrUnsupported
	}
	return target == desktopsettings.ErrUnavailable
}

func refuse(reason Reason, detail string) *Error { return &Error{Reason: reason, Detail: detail} }
func (e *Error) because(err error) *Error {
	e.Err = err
	return e
}
func (e *Error) fix(remediation string) *Error {
	e.Remediation = remediation
	return e
}
