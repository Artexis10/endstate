// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package configvalidate applies the engine-owned validation primitives to a
// staged or committed config-set root.
package configvalidate

import (
	"errors"
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type Code string

const (
	CodeUnsupportedValidation Code = "unsupported_validation"
	CodeUnsafeRoot            Code = "unsafe_root"
	CodeUnsafePath            Code = "unsafe_path"
	CodeLinkUnsupported       Code = "link_unsupported"
	CodeSourceChanged         Code = "source_changed"
	CodePathNotFound          Code = "path_not_found"
	CodeUnsupportedFileType   Code = "unsupported_file_type"
	CodeMalformedJSON         Code = "malformed_json"
	CodeInvalidJSONPath       Code = "invalid_json_path"
	CodeJSONPathMissing       Code = "json_path_missing"
	CodeMalformedINI          Code = "malformed_ini"
	CodeInvalidINIAddress     Code = "invalid_ini_address"
	CodeINIKeyMissing         Code = "ini_key_missing"
	CodeIO                    Code = "io_error"
)

// Error identifies the exact validation definition that failed. Index is
// zero-based, or -1 when the validation root itself is invalid.
type Error struct {
	Code       Code
	Index      int
	Validation modules.ValidationDef
	HostPath   string
	Err        error
}

func (e *Error) Error() string {
	if e.Index < 0 {
		return fmt.Sprintf("config validation root failed (%s): %v", e.Code, e.Err)
	}
	return fmt.Sprintf(
		"config validation[%d] %s for %q at %q failed (%s): %v",
		e.Index,
		e.Validation.Type,
		e.Validation.Path,
		e.HostPath,
		e.Code,
		e.Err,
	)
}

func (e *Error) Unwrap() error { return e.Err }

func CodeOf(err error) Code {
	var validationError *Error
	if errors.As(err, &validationError) {
		return validationError.Code
	}
	return ""
}

type localError struct {
	code Code
	err  error
}

func (e *localError) Error() string { return e.err.Error() }
func (e *localError) Unwrap() error { return e.err }

func fail(code Code, err error) error {
	return &localError{code: code, err: err}
}
