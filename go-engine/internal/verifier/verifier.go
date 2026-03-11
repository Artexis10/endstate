// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package verifier provides state assertion checkers for the Endstate verify
// pipeline. Each checker validates a single aspect of machine state (file
// existence, command availability, registry key presence) and returns a
// VerifyResult indicating pass or fail.
package verifier

import (
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
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
	results := make([]VerifyResult, 0, len(entries))
	for _, entry := range entries {
		var r VerifyResult
		switch entry.Type {
		case "file-exists":
			r = CheckFileExists(entry)
		case "command-exists":
			r = CheckCommandExists(entry)
		case "registry-key-exists":
			r = CheckRegistryKeyExists(entry)
		default:
			r = VerifyResult{
				Type:    entry.Type,
				Pass:    false,
				Message: fmt.Sprintf("Unknown verify type: %s", entry.Type),
			}
		}
		results = append(results, r)
	}
	return results
}
