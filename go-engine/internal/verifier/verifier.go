// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package verifier provides state assertion checkers for the Endstate verify
// pipeline. Each checker validates a single aspect of machine state (file
// existence, command availability, registry key presence) and returns a
// VerifyResult indicating pass or fail.
package verifier

import (
	"errors"
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

// VerifyResult holds the outcome of a single verify check.
type VerifyResult struct {
	Type      string `json:"type"`
	Path      string `json:"path,omitempty"`
	Command   string `json:"command,omitempty"`
	ValueName string `json:"valueName,omitempty"`
	Pass      bool   `json:"pass"`
	Message   string `json:"message"`
}

// RunVerify dispatches each entry to the correct checker based on the Type
// field and returns a result slice. Unknown types produce a fail result with
// a descriptive message.
func RunVerify(entries []manifest.VerifyEntry) []VerifyResult {
	results, _ := RunVerifyWithValidation(entries, nil)
	return results
}

// RunVerifyWithValidation dispatches verification through an optional
// disposable-host authority. Assertion failures remain ordinary results;
// isolation failures are returned separately so the command session can record
// them and the CLI entrypoint remains the sole public error-code mapper.
func RunVerifyWithValidation(entries []manifest.VerifyEntry, context *validationmode.Context) ([]VerifyResult, error) {
	results := make([]VerifyResult, 0, len(entries))
	var isolationErrors []error
	for _, entry := range entries {
		var r VerifyResult
		var err error
		switch entry.Type {
		case "file-exists":
			if context == nil {
				r = CheckFileExists(entry)
			} else {
				r, err = checkFileExistsWithValidation(entry, context)
			}
		case "command-exists":
			r = CheckCommandExists(entry)
		case "registry-key-exists":
			if context == nil {
				r = CheckRegistryKeyExists(entry)
			} else {
				r, err = checkRegistryKeyExistsWithValidation(entry, context)
			}
		case "registry-value-equals":
			if context == nil {
				r = CheckRegistryValueEquals(entry)
			} else {
				r, err = checkRegistryValueEqualsWithValidation(entry, context)
			}
		default:
			r = VerifyResult{
				Type:    entry.Type,
				Pass:    false,
				Message: fmt.Sprintf("Unknown verify type: %s", entry.Type),
			}
		}
		results = append(results, r)
		if err != nil {
			isolationErrors = append(isolationErrors, err)
		}
	}
	return results, errors.Join(isolationErrors...)
}
