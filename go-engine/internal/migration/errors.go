// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"errors"
	"fmt"
)

type ErrorCode string

type StagePhase string

const (
	CodeUnsupportedOperation   ErrorCode = "unsupported_operation"
	CodeInvalidStageRequest    ErrorCode = "invalid_stage_request"
	CodePayloadIntegrityFailed ErrorCode = "payload_integrity_failed"
	CodeInvalidMigrationPath   ErrorCode = "invalid_migration_path"
	CodeValidationFailed       ErrorCode = "validation_failed"
	CodeCanceled               ErrorCode = "canceled"
	CodeUnsafeRoot             ErrorCode = "unsafe_root"
	CodeUnsafePath             ErrorCode = "unsafe_path"
	CodeLinkUnsupported        ErrorCode = "link_unsupported"
	CodePathNotFound           ErrorCode = "path_not_found"
	CodeUnsupportedFileType    ErrorCode = "unsupported_file_type"
	CodeDestinationExists      ErrorCode = "destination_exists"
	CodeSourceDescendant       ErrorCode = "source_descendant"
	CodeSourceChanged          ErrorCode = "source_changed"
	CodeMalformedJSON          ErrorCode = "malformed_json"
	CodeInvalidJSONPath        ErrorCode = "invalid_json_path"
	CodeInvalidJSONValue       ErrorCode = "invalid_json_value"
	CodeJSONParentMissing      ErrorCode = "json_parent_missing"
	CodeJSONSourceMissing      ErrorCode = "json_source_missing"
	CodeJSONDestinationExists  ErrorCode = "json_destination_exists"
	CodeMalformedINI           ErrorCode = "malformed_ini"
	CodeInvalidINIValue        ErrorCode = "invalid_ini_value"
	CodeINISourceMissing       ErrorCode = "ini_source_missing"
	CodeINIDestinationExists   ErrorCode = "ini_destination_exists"
	CodeIO                     ErrorCode = "io_error"
)

const (
	PhaseRequestValidation StagePhase = "request_validation"
	PhaseSourceIntegrity   StagePhase = "source_integrity"
	PhaseStageCreate       StagePhase = "stage_create"
	PhaseStageCopy         StagePhase = "stage_copy"
	PhaseStagedIntegrity   StagePhase = "staged_integrity"
	PhasePathValidation    StagePhase = "path_validation"
	PhaseEdgeOperation     StagePhase = "edge_operation"
	PhaseEdgeValidation    StagePhase = "edge_validation"
	PhaseTargetValidation  StagePhase = "target_validation"
	PhaseCleanup           StagePhase = "cleanup"
)

type Error struct {
	Code            ErrorCode
	Operation       string
	Index           int
	CaptureID       string
	Phase           StagePhase
	EdgeIndex       int
	ValidationIndex int
	IntegrityCode   string
	ValidationCode  string
	Err             error
}

func (e *Error) Error() string {
	if e.Phase != "" {
		return fmt.Sprintf(
			"migration stage %q phase %q edge %d failed (%s): %v",
			e.CaptureID,
			e.Phase,
			e.EdgeIndex,
			e.Code,
			e.Err,
		)
	}
	return fmt.Sprintf("migration operation %d %q failed (%s): %v", e.Index, e.Operation, e.Code, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func CodeOf(err error) ErrorCode {
	var migrationError *Error
	if errors.As(err, &migrationError) {
		return migrationError.Code
	}
	return ""
}

func operationError(code ErrorCode, operation string, index int, err error) error {
	return &Error{Code: code, Operation: operation, Index: index, EdgeIndex: -1, ValidationIndex: -1, Err: err}
}
