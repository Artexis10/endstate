// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package configdoc provides strict deterministic JSON and INI document edits.
package configdoc

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeMalformedJSON         Code = "malformed_json"
	CodeInvalidJSONPath       Code = "invalid_json_path"
	CodeInvalidJSONValue      Code = "invalid_json_value"
	CodeJSONParentMissing     Code = "json_parent_missing"
	CodeJSONSourceMissing     Code = "json_source_missing"
	CodeJSONDestinationExists Code = "json_destination_exists"
	CodeMalformedINI          Code = "malformed_ini"
	CodeInvalidINIValue       Code = "invalid_ini_value"
	CodeINISourceMissing      Code = "ini_source_missing"
	CodeINIDestinationExists  Code = "ini_destination_exists"
)

type Error struct {
	Code Code
	Path string
	Err  error
}

func (e *Error) Error() string {
	return fmt.Sprintf("config document %s at %q: %v", e.Code, e.Path, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func CodeOf(err error) Code {
	var documentError *Error
	if errors.As(err, &documentError) {
		return documentError.Code
	}
	return ""
}

func documentError(code Code, path string, err error) error {
	return &Error{Code: code, Path: path, Err: err}
}
