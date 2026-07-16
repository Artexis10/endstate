// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeUnsupportedOperation  ErrorCode = "unsupported_operation"
	CodeUnsafeRoot            ErrorCode = "unsafe_root"
	CodeUnsafePath            ErrorCode = "unsafe_path"
	CodeLinkUnsupported       ErrorCode = "link_unsupported"
	CodePathNotFound          ErrorCode = "path_not_found"
	CodeUnsupportedFileType   ErrorCode = "unsupported_file_type"
	CodeDestinationExists     ErrorCode = "destination_exists"
	CodeSourceDescendant      ErrorCode = "source_descendant"
	CodeSourceChanged         ErrorCode = "source_changed"
	CodeMalformedJSON         ErrorCode = "malformed_json"
	CodeInvalidJSONPath       ErrorCode = "invalid_json_path"
	CodeInvalidJSONValue      ErrorCode = "invalid_json_value"
	CodeJSONParentMissing     ErrorCode = "json_parent_missing"
	CodeJSONSourceMissing     ErrorCode = "json_source_missing"
	CodeJSONDestinationExists ErrorCode = "json_destination_exists"
	CodeMalformedINI          ErrorCode = "malformed_ini"
	CodeInvalidINIValue       ErrorCode = "invalid_ini_value"
	CodeINISourceMissing      ErrorCode = "ini_source_missing"
	CodeINIDestinationExists  ErrorCode = "ini_destination_exists"
	CodeIO                    ErrorCode = "io_error"
)

type Error struct {
	Code      ErrorCode
	Operation string
	Index     int
	Err       error
}

func (e *Error) Error() string {
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
	return &Error{Code: code, Operation: operation, Index: index, Err: err}
}
