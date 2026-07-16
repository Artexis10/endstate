// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package configrestore turns one successful generation staging result into a
// deterministic, transaction-ready set of concrete target actions. It does
// not create backups, journals, or mutate targets.
package configrestore

import (
	"errors"
	"fmt"
)

// Code is a stable machine-readable materialization or preflight failure.
type Code string

const (
	CodeInvalidRequest        Code = "invalid_request"
	CodeUnsupportedRestore    Code = "unsupported_restore"
	CodeUnsafePath            Code = "unsafe_path"
	CodeSourceMissing         Code = "source_missing"
	CodeUnsupportedFileType   Code = "unsupported_file_type"
	CodeTargetOverlap         Code = "target_overlap"
	CodeValidationMapping     Code = "validation_mapping"
	CodeInvalidRegistryTarget Code = "invalid_registry_target"
	CodeInvalidRegistryValue  Code = "invalid_registry_value"
	CodeMalformedJSON         Code = "malformed_json"
	CodeMaterialization       Code = "materialization_failed"
	CodeAppClosureConfig      Code = "app_closure_config"
	CodeProcessObservation    Code = "process_observation_failed"
	CodeAppRunning            Code = "app_running"
)

// Error identifies the exact declarative item that failed. Indices are
// zero-based and remain -1 when a failure is not tied to one declaration.
type Error struct {
	Code            Code
	ActionIndex     int
	ValidationIndex int
	MappingCount    int
	Target          string
	ProcessBasename string
	ProcessPattern  string
	Err             error
}

func (e *Error) Error() string {
	switch e.Code {
	case CodeAppRunning:
		return fmt.Sprintf("config restore preflight failed (%s): process %q matches %q", e.Code, e.ProcessBasename, e.ProcessPattern)
	case CodeValidationMapping:
		return fmt.Sprintf("config restore validation[%d] has %d source-to-target mappings (%s): %v", e.ValidationIndex, e.MappingCount, e.Code, e.Err)
	default:
		return fmt.Sprintf("config restore action[%d] target %q failed (%s): %v", e.ActionIndex, e.Target, e.Code, e.Err)
	}
}

func (e *Error) Unwrap() error { return e.Err }

// CodeOf extracts a configrestore Code from err.
func CodeOf(err error) Code {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

func newError(code Code, actionIndex int, target string, err error) *Error {
	return &Error{Code: code, ActionIndex: actionIndex, ValidationIndex: -1, MappingCount: -1, Target: target, Err: err}
}
