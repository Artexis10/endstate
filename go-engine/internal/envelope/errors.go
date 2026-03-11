// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package envelope

// ErrorCode is a stable, machine-readable error code used in JSON envelope errors.
// Codes follow SCREAMING_SNAKE_CASE convention as defined in the CLI JSON Contract v1.0.
type ErrorCode string

const (
	ErrManifestNotFound       ErrorCode = "MANIFEST_NOT_FOUND"
	ErrManifestParseError     ErrorCode = "MANIFEST_PARSE_ERROR"
	ErrManifestValidationError ErrorCode = "MANIFEST_VALIDATION_ERROR"
	ErrManifestWriteFailed    ErrorCode = "MANIFEST_WRITE_FAILED"
	ErrPlanNotFound           ErrorCode = "PLAN_NOT_FOUND"
	ErrPlanParseError         ErrorCode = "PLAN_PARSE_ERROR"
	ErrWingetNotAvailable     ErrorCode = "WINGET_NOT_AVAILABLE"
	ErrEngineCLINotFound      ErrorCode = "ENGINE_CLI_NOT_FOUND"
	ErrCaptureFailed          ErrorCode = "CAPTURE_FAILED"
	ErrCaptureBlocked         ErrorCode = "CAPTURE_BLOCKED"
	ErrInstallFailed          ErrorCode = "INSTALL_FAILED"
	ErrRestoreFailed          ErrorCode = "RESTORE_FAILED"
	ErrVerifyFailed           ErrorCode = "VERIFY_FAILED"
	ErrPermissionDenied       ErrorCode = "PERMISSION_DENIED"
	ErrInternalError          ErrorCode = "INTERNAL_ERROR"
	ErrSchemaIncompatible     ErrorCode = "SCHEMA_INCOMPATIBLE"
)

// Error is the structured error object included in the JSON envelope when success is false.
type Error struct {
	Code        ErrorCode   `json:"code"`
	Message     string      `json:"message"`
	Detail      interface{} `json:"detail,omitempty"`
	Remediation string      `json:"remediation,omitempty"`
	DocsKey     string      `json:"docsKey,omitempty"`
}

// NewError constructs an Error with the given code and message.
func NewError(code ErrorCode, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// WithDetail attaches structured detail context to the error and returns the receiver
// for chaining.
func (e *Error) WithDetail(detail interface{}) *Error {
	e.Detail = detail
	return e
}

// WithRemediation attaches a remediation hint and returns the receiver for chaining.
func (e *Error) WithRemediation(remediation string) *Error {
	e.Remediation = remediation
	return e
}

// WithDocsKey attaches a documentation reference key and returns the receiver for
// chaining.
func (e *Error) WithDocsKey(docsKey string) *Error {
	e.DocsKey = docsKey
	return e
}
