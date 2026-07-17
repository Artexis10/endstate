// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package envelope

// ErrorCode is a stable, machine-readable error code used in JSON envelope errors.
// Codes follow SCREAMING_SNAKE_CASE convention as defined in the CLI JSON Contract v1.0.
type ErrorCode string

const (
	ErrManifestNotFound        ErrorCode = "MANIFEST_NOT_FOUND"
	ErrManifestParseError      ErrorCode = "MANIFEST_PARSE_ERROR"
	ErrManifestValidationError ErrorCode = "MANIFEST_VALIDATION_ERROR"
	ErrManifestWriteFailed     ErrorCode = "MANIFEST_WRITE_FAILED"
	ErrPlanNotFound            ErrorCode = "PLAN_NOT_FOUND"
	ErrPlanParseError          ErrorCode = "PLAN_PARSE_ERROR"
	ErrWingetNotAvailable      ErrorCode = "WINGET_NOT_AVAILABLE"
	ErrRealizerUnavailable     ErrorCode = "REALIZER_UNAVAILABLE"
	ErrEngineCLINotFound       ErrorCode = "ENGINE_CLI_NOT_FOUND"
	ErrCaptureFailed           ErrorCode = "CAPTURE_FAILED"
	ErrCaptureBlocked          ErrorCode = "CAPTURE_BLOCKED"
	ErrInstallFailed           ErrorCode = "INSTALL_FAILED"
	ErrRestoreFailed           ErrorCode = "RESTORE_FAILED"
	ErrInvalidRestoreTarget    ErrorCode = "INVALID_RESTORE_TARGET"
	ErrVerifyFailed            ErrorCode = "VERIFY_FAILED"
	ErrPermissionDenied        ErrorCode = "PERMISSION_DENIED"
	ErrInternalError           ErrorCode = "INTERNAL_ERROR"
	ErrSchemaIncompatible      ErrorCode = "SCHEMA_INCOMPATIBLE"

	// Provisioning-generation rollback codes (nix-native-rollback). The package
	// `rollback` command surfaces these; they are distinct from install/restore
	// codes because rollback is its own pipeline verb.
	ErrRollbackUnsupported ErrorCode = "ROLLBACK_UNSUPPORTED"
	ErrGenerationNotFound  ErrorCode = "GENERATION_NOT_FOUND"
	ErrRollbackFailed      ErrorCode = "ROLLBACK_FAILED"

	// Convergence (apply --prune) error code. Returned when a backend that does
	// not support whole-set removal (e.g. the winget driver) is asked to prune
	// installed-but-undeclared packages.
	ErrConvergenceUnsupported ErrorCode = "CONVERGENCE_UNSUPPORTED"

	// ErrConfirmationRequired is returned by `rebuild` when a live run (neither
	// --dry-run nor --no-restore) is requested without --confirm. It is raised
	// before any mutation so the refusal has zero side effects. Additive in
	// schema 1.x.
	ErrConfirmationRequired ErrorCode = "CONFIRMATION_REQUIRED"

	// Schedule error codes. Additive in schema 1.x.
	// ErrNotSupported is returned by schedule enable/disable/run on non-Windows platforms.
	ErrNotSupported ErrorCode = "NOT_SUPPORTED"
	// ErrTaskRegistrationFailed is returned when schtasks.exe fails to register the task.
	ErrTaskRegistrationFailed ErrorCode = "TASK_REGISTRATION_FAILED"
	// ErrScheduleDisabled is returned when schedule run is invoked but no schedule is
	// enabled; the caller should run 'schedule enable --manifest <path>' first.
	ErrScheduleDisabled ErrorCode = "SCHEDULE_DISABLED"

	// Hosted-backup error codes. Mapped from substrate backend HTTP responses
	// per docs/contracts/hosted-backup-contract.md and cli-json-contract.md.
	ErrAuthRequired         ErrorCode = "AUTH_REQUIRED"
	ErrSubscriptionRequired ErrorCode = "SUBSCRIPTION_REQUIRED"
	ErrNotFound             ErrorCode = "NOT_FOUND"
	ErrRateLimited          ErrorCode = "RATE_LIMITED"
	ErrBackendError         ErrorCode = "BACKEND_ERROR"
	ErrBackendUnreachable   ErrorCode = "BACKEND_UNREACHABLE"
	ErrBackendIncompatible  ErrorCode = "BACKEND_INCOMPATIBLE"
	ErrStorageQuotaExceeded ErrorCode = "STORAGE_QUOTA_EXCEEDED"

	// Substrate claim-flow domain codes. Surfaced verbatim by
	// `client.parseAPIError` when substrate returns them on the
	// response body of `/api/auth/claim`. NOT declared as constants
	// because the engine is a transparent pipe for the substrate
	// claim namespace — the GUI's `friendlyAuthError` map owns the
	// user-facing copy. Valid `ErrorCode` values produced by this
	// engine include (cast from the substrate response):
	//
	//   "CLAIM_TOKEN_INVALID"   (HTTP 401)
	//   "CLAIM_TOKEN_EXPIRED"   (HTTP 401)
	//   "CLAIM_TOKEN_CONSUMED"  (HTTP 409)
	//   "KDF_TOO_WEAK"          (HTTP 400)
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
