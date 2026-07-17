// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
)

// envNamePattern restricts a secret's env-variable name to a valid shell/Nix
// identifier. Because the compiler emits an env secret as a BARE Nix attribute
// (home.sessionVariables.<Env> = …), this load-time check is also the guard that
// blocks Nix-attr injection via a crafted name (e.g. `x = "evil"; y`).
const envNamePattern = "^[A-Za-z_][A-Za-z0-9_]*$"

var envNameRe = regexp.MustCompile(envNamePattern)

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
//  5. UNSUPPORTED_VERSION  - "version" must be integer 1 or 2
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
	supportedVersion := 0
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
			if math.Trunc(versionNum) != versionNum || versionNum < 1 || versionNum > 2 {
				res.Errors = append(res.Errors, ValidationError{
					Code:    "UNSUPPORTED_VERSION",
					Message: `"version" must be integer 1 or 2`,
				})
			} else {
				supportedVersion = int(versionNum)
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
			res.Errors = append(res.Errors, ValidateManifestApps(&Manifest{Apps: apps})...)
		}
	}

	// The signature checks above preserve their established diagnostic ordering.
	// Once they pass, use the same version-dispatched loader as execution so v2
	// provenance, include compatibility, and structural isolation cannot validate
	// differently in profile discovery than they do at apply time.
	if len(res.Errors) == 0 && supportedVersion != 0 {
		if _, err := LoadManifest(path); err != nil {
			code := ManifestDiagnosticCode(err)
			if code == "" {
				code = "MANIFEST_VALIDATION_ERROR"
			}
			res.Errors = append(res.Errors, ValidationError{Code: code, Message: err.Error()})
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
	for i := range m.Apps {
		app := m.Apps[i]
		canonicalDriver, known := normalizeAppDriver(app.Driver)
		if !known {
			errs = append(errs, ValidationError{
				Code:    "UNSUPPORTED_APP_DRIVER",
				Message: fmt.Sprintf("app %q: unsupported driver %q", app.ID, app.Driver),
			})
		} else {
			m.Apps[i].Driver = canonicalDriver
			app.Driver = canonicalDriver
		}
		if app.Manual != nil && app.Manual.VerifyPath == "" {
			errs = append(errs, ValidationError{
				Code:    "MANUAL_MISSING_VERIFY_PATH",
				Message: fmt.Sprintf(`app %q: "manual.verifyPath" is required when "manual" is present`, app.ID),
			})
		}
		errs = append(errs, validateBrewApp(app)...)
		errs = append(errs, validateChocolateyApp(app)...)
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

// normalizeAppDriver canonicalizes the stable package-backend names while
// keeping an omitted driver empty so platform defaults remain backward
// compatible. Recognition is global and host-independent; platform support is
// enforced later by the command-layer backend registry.
func normalizeAppDriver(name string) (string, bool) {
	canonical := strings.ToLower(strings.TrimSpace(name))
	switch canonical {
	case "", "winget", "chocolatey", "brew":
		return canonical, true
	default:
		return canonical, false
	}
}

func validateChocolateyApp(app App) []ValidationError {
	if app.Driver == "chocolatey" && app.Refs["windows"] == "" {
		return []ValidationError{{
			Code:    "CHOCOLATEY_DRIVER_REQUIRES_WINDOWS_REF",
			Message: fmt.Sprintf(`app %q: driver:"chocolatey" requires a "windows" ref`, app.ID),
		}}
	}
	return nil
}

// validateBrewApp enforces the brew driver routing rules (host-independent, so a
// manifest authored on any OS is checked the same way):
//
//   - driver:"brew" WITHOUT a darwin ref → BREW_DRIVER_REQUIRES_DARWIN_REF. The
//     brew driver only installs on darwin, so a brew app needs a darwin package
//     to install (a bare formula ref or a cask: ref).
//
// A darwin ref marked as a Cask (cask: prefix) WITHOUT driver:"brew" is NO LONGER
// rejected: the cask: prefix routes the app to the brew lane BY DEFAULT
// (partitionBrewLane), so the Nix realizer still never receives a cask ref — the
// invariant is now upheld by routing, not by a manifest rejection
// (brew-default-for-apps). Driver matching is case-insensitive (EqualFold),
// matching the apply-lane partition.
func validateBrewApp(app App) []ValidationError {
	var errs []ValidationError
	isBrew := strings.EqualFold(app.Driver, "brew")
	darwinRef := app.Refs["darwin"]

	if isBrew && darwinRef == "" {
		errs = append(errs, ValidationError{
			Code:    "BREW_DRIVER_REQUIRES_DARWIN_REF",
			Message: fmt.Sprintf(`app %q: driver:"brew" requires a "darwin" ref (a formula name or a "cask:" ref); brew only installs on macOS`, app.ID),
		})
	}
	return errs
}

// validateHomeManagerSecrets checks the documented-boundary secrets list. Secrets
// compose with the engine-generated modes (settings/config) but are rejected
// alongside a pure flake input (the user's external flake owns its own secrets —
// the engine generates nothing to inject reference sinks into). Each entry requires
// a "path" reference (the file the user provisions out-of-band) and a non-empty
// unique name; it MAY also carry an "env" var name, in which case the engine emits
// a sessionVariable referencing that PATH (never the value — the *_FILE
// path-reference convention). An "env" without a "path" is rejected, as is an env
// name that is not a valid identifier (which would otherwise allow Nix-attr
// injection). The backend ("" defaults to / is equivalent to "boundary").
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

		// A secret always references a file PATH (the user provisions it out-of-band).
		// It MAY additionally carry an "env" var name: the engine then emits
		// home.sessionVariables.<env> = "<path>"; — referencing the path, never the
		// value (the *_FILE path-reference convention). So: path-only ✓, env+path ✓,
		// env-only ✗ (no file to reference), neither ✗. The env name must be a valid
		// identifier (the env-requires-path check runs FIRST so the loader surfaces it
		// before the name check; the name check also blocks Nix-attr injection because
		// the compiler emits env as a bare attribute).
		hasPath := s.Path != ""
		hasEnv := s.Env != ""
		switch {
		case hasEnv && !hasPath:
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_ENV_REQUIRES_PATH",
				Message: fmt.Sprintf("homeManager.secrets[%q]: an \"env\" secret must also declare the file via \"path\" (the engine references the file path, never the value)", s.Name),
			})
		case !hasPath && !hasEnv:
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_MISSING_REF",
				Message: fmt.Sprintf("homeManager.secrets[%q]: requires a \"path\" reference (optionally with \"env\")", s.Name),
			})
		}
		if hasEnv && !envNameRe.MatchString(s.Env) {
			errs = append(errs, ValidationError{
				Code:    "HOMEMANAGER_SECRET_INVALID_ENV_NAME",
				Message: fmt.Sprintf("homeManager.secrets[%q]: invalid env name %q (must match %s)", s.Name, s.Env, envNamePattern),
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
