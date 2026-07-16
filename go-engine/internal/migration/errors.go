// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeUnsupportedOperation ErrorCode = "unsupported_operation"
	CodeUnsafeRoot           ErrorCode = "unsafe_root"
	CodeUnsafePath           ErrorCode = "unsafe_path"
	CodeLinkUnsupported      ErrorCode = "link_unsupported"
	CodePathNotFound         ErrorCode = "path_not_found"
	CodeUnsupportedFileType  ErrorCode = "unsupported_file_type"
	CodeDestinationExists    ErrorCode = "destination_exists"
	CodeSourceDescendant     ErrorCode = "source_descendant"
	CodeIO                   ErrorCode = "io_error"
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
