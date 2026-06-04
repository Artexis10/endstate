// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

	// 8. Validate app-level constraints (manual.verifyPath required when manual present).
	if hasApps {
		var apps []App
		if err := json.Unmarshal(appsRaw, &apps); err == nil {
			for _, app := range apps {
				if app.Manual != nil && app.Manual.VerifyPath == "" {
					res.Errors = append(res.Errors, ValidationError{
						Code:    "MANUAL_MISSING_VERIFY_PATH",
						Message: fmt.Sprintf(`app %q: "manual.verifyPath" is required when "manual" is present`, app.ID),
					})
				}
			}
		}
	}

	res.Valid = len(res.Errors) == 0
	return res
}

// ValidateManifestApps checks app-level and home-manager constraints on a parsed
// manifest. Returns validation errors for apps with manual blocks missing
// verifyPath, and for a homeManager block that declares both inputs.
func ValidateManifestApps(m *Manifest) []ValidationError {
	var errs []ValidationError
	for _, app := range m.Apps {
		if app.Manual != nil && app.Manual.VerifyPath == "" {
			errs = append(errs, ValidationError{
				Code:    "MANUAL_MISSING_VERIFY_PATH",
				Message: fmt.Sprintf(`app %q: "manual.verifyPath" is required when "manual" is present`, app.ID),
			})
		}
		errs = append(errs, validateBrewApp(app)...)
	}
	// home-manager: the three inputs — settings (a declarative catalog the engine
	// compiles), config (a home.nix the engine wraps), and flake (a direct
	// flakeref) — are mutually exclusive; exactly one home-manager input.
	if m.HomeManager != nil {
		n := 0
		if m.HomeManager.Settings != nil {
			n++
		}
		if m.HomeManager.Config != "" {
			n++
		}
		if m.HomeManager.Flake != "" {
			n++
		}
		if n > 1 {
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_INPUT_CONFLICT",
				Message: `homeManager: "settings", "config", and "flake" are mutually exclusive — set exactly one`,
			})
		}
		errs = append(errs, validateHomeManagerSecrets(m.HomeManager)...)
	}
	return errs
}

// brewCaskPrefix marks a darwin ref as a Homebrew Cask (a GUI app). It mirrors
// the brew driver's caskPrefix; kept local so the manifest package has no
// dependency on the driver package.
const brewCaskPrefix = "cask:"

// validateBrewApp enforces the brew driver routing rules (host-independent, so a
// manifest authored on any OS is rejected the same way):
//
//   - a darwin ref marked as a Cask (cask: prefix) WITHOUT driver:"brew" →
//     CASK_REF_REQUIRES_BREW_DRIVER. The Nix realizer must never receive a cask
//     ref; declaring the brew driver is the only valid home for one.
//   - driver:"brew" WITHOUT a darwin ref → BREW_DRIVER_REQUIRES_DARWIN_REF. The
//     brew driver only installs on darwin, so a brew app needs a darwin package
//     to install (a bare formula ref or a cask: ref).
//
// Driver matching is case-insensitive (EqualFold), matching the apply-lane
// partition. The windows-brew-ref conflict check is deferred (out of scope).
func validateBrewApp(app App) []ValidationError {
	var errs []ValidationError
	isBrew := strings.EqualFold(app.Driver, "brew")
	darwinRef := app.Refs["darwin"]
	isCask := strings.HasPrefix(strings.ToLower(strings.TrimSpace(darwinRef)), brewCaskPrefix)

	if isCask && !isBrew {
		errs = append(errs, ValidationError{
			Code:    "CASK_REF_REQUIRES_BREW_DRIVER",
			Message: fmt.Sprintf(`app %q: a "cask:" darwin ref requires driver:"brew" (the Nix realizer cannot install a Cask)`, app.ID),
		})
	}
	if isBrew && darwinRef == "" {
		errs = append(errs, ValidationError{
			Code:    "BREW_DRIVER_REQUIRES_DARWIN_REF",
			Message: fmt.Sprintf(`app %q: driver:"brew" requires a "darwin" ref (a formula name or a "cask:" ref); brew only installs on macOS`, app.ID),
		})
	}
	return errs
}

// validateHomeManagerSecrets checks the Phase-1 documented-boundary secrets list.
// Secrets compose with the engine-generated modes (settings/config) but are
// rejected alongside a pure flake input (the user's external flake owns its own
// secrets — the engine generates nothing to inject reference sinks into). Each
// entry must name exactly one reference (path XOR env), a non-empty unique name,
// and a supported backend ("" defaults to / is equivalent to "boundary").
func validateHomeManagerSecrets(hm *HomeManagerConfig) []ValidationError {
	if len(hm.Secrets) == 0 {
		return nil
	}
	var errs []ValidationError
	if hm.Flake != "" {
		errs = append(errs, ValidationError{
			Code:    "HOMEMANAGER_SECRETS_FLAKE_UNSUPPORTED",
			Message: `homeManager.secrets is not supported with "flake" — an external flake owns its own secrets; use "settings" or "config"`,
		})
	}
	seen := make(map[string]bool, len(hm.Secrets))
	for _, s := range hm.Secrets {
		if s.Name == "" {
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_EMPTY_NAME",
				Message: `homeManager.secrets: each entry requires a non-empty "name"`,
			})
		} else if seen[s.Name] {
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_DUPLICATE_NAME",
				Message: fmt.Sprintf("homeManager.secrets: duplicate secret name %q", s.Name),
			})
		}
		seen[s.Name] = true

		// Phase 1 is PATH-ONLY. An env-exposed secret is deferred: in the
		// documented-boundary model the engine never holds a secret's value, so it
		// cannot meaningfully set an env var — that needs its own design (a future
		// phase). Reject env now with a clear message rather than emit dead config.
		hasPath := s.Path != ""
		hasEnv := s.Env != ""
		switch {
		case hasEnv:
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_ENV_UNSUPPORTED",
				Message: fmt.Sprintf("homeManager.secrets[%q]: env-exposed secrets are not yet supported; declare the secret as a \"path\" reference", s.Name),
			})
		case !hasPath:
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_MISSING_REF",
				Message: fmt.Sprintf("homeManager.secrets[%q]: requires a \"path\" reference", s.Name),
			})
		}

		if s.Backend != "" && s.Backend != "boundary" {
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_UNSUPPORTED_BACKEND",
				Message: fmt.Sprintf("homeManager.secrets[%q]: unsupported backend %q (Phase 1 supports only \"boundary\")", s.Name, s.Backend),
			})
		}
	}
	return errs
}
