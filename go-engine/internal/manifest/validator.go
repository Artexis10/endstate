// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"os"
)

// ValidationResult is returned by ValidateProfile and mirrors the shape of
// Test-ProfileManifest in engine/manifest.ps1.
type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
}

// ValidationError carries one validation failure with a machine-readable Code
// and a human-readable Message, matching the error codes in profile-contract.md.
type ValidationError struct {
	Code    string
	Message string
}

// ValidateProfile checks path against the profile contract rules defined in
// docs/contracts/profile-contract.md and returns a ValidationResult.
//
// Validation order (same as the PS reference implementation):
//  1. FILE_NOT_FOUND  - file must exist
//  2. PARSE_ERROR     - content must be valid JSON/JSONC
//  3. MISSING_VERSION - "version" key must be present
//  4. INVALID_VERSION_TYPE - "version" must be a JSON number
//  5. UNSUPPORTED_VERSION  - "version" must equal 1
//  6. MISSING_APPS    - "apps" key must be present
//  7. INVALID_APPS_TYPE    - "apps" must be a JSON array
func ValidateProfile(path string) *ValidationResult {
	res := &ValidationResult{Valid: false}

	// 1. File must exist.
	data, err := os.ReadFile(path)
	if err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Code:    "FILE_NOT_FOUND",
			Message: "file not found: " + path,
		})
		return res
	}

	// 2. Content must be parseable JSON/JSONC.
	clean := StripJsoncComments(data)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(clean, &raw); err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Code:    "PARSE_ERROR",
			Message: "invalid JSON/JSONC: " + err.Error(),
		})
		return res
	}

	// 3. "version" key must be present.
	versionRaw, hasVersion := raw["version"]
	if !hasVersion {
		res.Errors = append(res.Errors, ValidationError{
			Code:    "MISSING_VERSION",
			Message: `"version" field is required`,
		})
	}

	// 4 & 5. Validate version type and value only when the key exists.
	if hasVersion {
		// Attempt to unmarshal as float64 (all JSON numbers decode to float64).
		var versionNum float64
		if err := json.Unmarshal(versionRaw, &versionNum); err != nil {
			// Could be a string, object, array, or boolean - all are wrong type.
			res.Errors = append(res.Errors, ValidationError{
				Code:    "INVALID_VERSION_TYPE",
				Message: `"version" must be a number`,
			})
		} else {
			// Type is correct; check the value.
			if versionNum != 1 {
				res.Errors = append(res.Errors, ValidationError{
					Code:    "UNSUPPORTED_VERSION",
					Message: `"version" must be 1`,
				})
			}
		}
	}

	// 6. "apps" key must be present.
	appsRaw, hasApps := raw["apps"]
	if !hasApps {
		res.Errors = append(res.Errors, ValidationError{
			Code:    "MISSING_APPS",
			Message: `"apps" field is required`,
		})
	}

	// 7. "apps" must be a JSON array.
	if hasApps {
		var appsArr []json.RawMessage
		if err := json.Unmarshal(appsRaw, &appsArr); err != nil {
			res.Errors = append(res.Errors, ValidationError{
				Code:    "INVALID_APPS_TYPE",
				Message: `"apps" must be an array`,
			})
		}
	}

	res.Valid = len(res.Errors) == 0
	return res
}
