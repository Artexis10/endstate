// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrValidation distinguishes a syntactically valid manifest that violates
// schema or app constraints from malformed JSON. Command envelopes use this to
// preserve MANIFEST_VALIDATION_ERROR instead of collapsing both cases into a
// parse error.
var ErrValidation = errors.New("manifest validation error")

// LoadManifest reads the file at path, strips JSONC comments, unmarshals the
// JSON into a Manifest, and recursively resolves any includes. The returned
// Manifest has all included Apps merged in (included apps are appended after
// the declaring manifest's own apps, mirroring PowerShell behaviour).
func LoadManifest(path string) (*Manifest, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: cannot resolve path %q: %w", path, err)
	}

	visited := make(map[string]bool)
	return loadManifestInternal(absPath, visited, 0, true, nil, false)
}

// LoadManifestForValidationCapture admits the private validation driver only
// for one exact descriptor-bound intermediate capture manifest. Authored
// manifests continue to use LoadManifest and reject that synthetic driver.
func LoadManifestForValidationCapture(path string, expected App) (*Manifest, error) {
	return loadManifestWithValidationCapture(path, expected, false)
}

// LoadProjectedManifestForValidationCapture admits the same exact private
// validation app identity after capture has added its strictly validated
// restore/config projection. It remains unavailable to authored manifests.
func LoadProjectedManifestForValidationCapture(path string, expected App) (*Manifest, error) {
	return loadManifestWithValidationCapture(path, expected, true)
}

func loadManifestWithValidationCapture(path string, expected App, allowProjection bool) (*Manifest, error) {
	if !strings.EqualFold(strings.TrimSpace(expected.Driver), "validation") || expected.ID == "" || expected.Refs["windows"] == "" {
		return nil, fmt.Errorf("manifest: validation capture identity is invalid")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: cannot resolve path %q: %w", path, err)
	}
	return loadManifestInternal(absPath, make(map[string]bool), 0, true, &expected, allowProjection)
}

// loadManifestInternal is the recursive implementation used by LoadManifest
// and resolveIncludes. visited tracks absolute paths to detect cycles. Includes
// inherit their parent's version when they omit an explicit version.
func loadManifestInternal(absPath string, visited map[string]bool, inheritedVersion int, root bool, validationCapture *App, allowValidationProjection bool) (*Manifest, error) {
	// Circular include detection
	if visited[absPath] {
		return nil, fmt.Errorf("manifest: circular include detected at %q", absPath)
	}
	visited[absPath] = true
	defer func() { delete(visited, absPath) }()

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("manifest: cannot read %q: %w", absPath, err)
	}

	clean := StripJsoncComments(data)

	// Dispatch on the raw version before unmarshalling into the typed Manifest.
	// This prevents malformed or future manifests from silently taking a legacy
	// path merely because their v2-only fields are unknown to an older shape.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(clean, &raw); err != nil {
		return nil, fmt.Errorf("manifest: JSON parse error in %q: %w", absPath, err)
	}
	version, err := parseManifestVersion(raw, absPath, inheritedVersion)
	if err != nil {
		return nil, err
	}
	if err := validateRawConfigCaptures(raw, version, absPath); err != nil {
		return nil, err
	}
	if err := validateRawLegacyAssociations(raw, version, absPath); err != nil {
		return nil, err
	}

	var m Manifest
	if err := json.Unmarshal(clean, &m); err != nil {
		return nil, fmt.Errorf("manifest: JSON parse error in %q: %w", absPath, err)
	}
	m.Version = version
	if root && validationCapture != nil && len(m.Includes) > 0 {
		return nil, fmt.Errorf("manifest: validation error in %q: %w: private validation capture manifests must not declare includes", absPath, ErrValidation)
	}
	if version == 2 {
		if err := validateConfigCaptures(m.ConfigCaptures, absPath, false); err != nil {
			return nil, err
		}
		if err := validateLegacyConfigLanes(&m, absPath); err != nil {
			return nil, err
		}
	}

	// Validate app-level constraints (e.g. manual.verifyPath required).
	validationTarget := validationCapture
	if !root {
		validationTarget = nil
	}
	if errs := validateLoadedManifestApps(&m, validationTarget, allowValidationProjection); len(errs) > 0 {
		return nil, fmt.Errorf("manifest: validation error in %q: %w: %s", absPath, ErrValidation, errs[0].Message)
	}

	if len(m.Includes) > 0 {
		baseDir := filepath.Dir(absPath)
		if err := resolveIncludes(&m, baseDir, visited, version); err != nil {
			return nil, err
		}
	}

	if version == 2 {
		if err := validateConfigCaptures(m.ConfigCaptures, absPath, root); err != nil {
			return nil, err
		}
		if err := validateNoGenerationLegacyFallback(&m, absPath); err != nil {
			return nil, err
		}
	}
	if root && validationCapture != nil {
		if errs := validateLoadedManifestApps(&m, validationCapture, allowValidationProjection); len(errs) > 0 {
			return nil, fmt.Errorf("manifest: validation error in %q: %w: %s", absPath, ErrValidation, errs[0].Message)
		}
	}

	return &m, nil
}

func validateLoadedManifestApps(value *Manifest, validationCapture *App, allowValidationProjection bool) []ValidationError {
	if validationCapture == nil {
		return ValidateManifestApps(value)
	}
	if value == nil || len(value.Apps) != 1 || !exactValidationCaptureApp(value.Apps[0], *validationCapture) || len(value.Includes) != 0 ||
		!allowValidationProjection && (len(value.Restore) != 0 || len(value.ConfigModules) != 0 || len(value.LegacyConfigLanes) != 0) {
		return []ValidationError{{Code: "VALIDATION_CAPTURE_IDENTITY_MISMATCH", Message: "validation capture app differs from descriptor identity"}}
	}
	clone := *value
	clone.Apps = append([]App(nil), value.Apps...)
	clone.Apps[0].Driver = ""
	return ValidateManifestApps(&clone)
}

func exactValidationCaptureApp(actual, expected App) bool {
	return actual.ID == expected.ID && strings.EqualFold(actual.Driver, "validation") && strings.EqualFold(expected.Driver, "validation") &&
		actual.Refs["windows"] == expected.Refs["windows"] && actual.Source == expected.Source && actual.Version == expected.Version &&
		actual.DisplayName == expected.DisplayName && actual.Manual == nil && len(actual.Refs) == 1
}

// resolveIncludes iterates over manifest.Includes, loads each included file
// (detecting cycles via visited), and merges the included Apps into m.Apps.
// The includes slice on m is cleared after processing, matching the PS engine.
func resolveIncludes(m *Manifest, basePath string, visited map[string]bool, parentVersion int) error {
	includes := m.Includes
	// Clear includes on the parent so it is not serialised again.
	m.Includes = nil

	for _, inc := range includes {
		inclPath := inc
		if !filepath.IsAbs(inclPath) {
			inclPath = filepath.Join(basePath, inclPath)
		}

		inclPath = filepath.Clean(inclPath)

		included, err := loadManifestInternal(inclPath, visited, parentVersion, false, nil, false)
		if err != nil {
			return fmt.Errorf("manifest: failed to load include %q: %w", inc, err)
		}

		// Merge: included apps are appended after the parent's own apps.
		m.Apps = append(m.Apps, included.Apps...)

		// Merge restore and verify entries as well (matching PS Resolve-ManifestIncludes).
		m.Restore = append(m.Restore, included.Restore...)
		m.Verify = append(m.Verify, included.Verify...)
		m.ConfigCaptures = append(m.ConfigCaptures, included.ConfigCaptures...)
		m.LegacyConfigLanes = append(m.LegacyConfigLanes, included.LegacyConfigLanes...)
		if parentVersion == 2 {
			m.ConfigModules = append(m.ConfigModules, included.ConfigModules...)
		}
	}

	return nil
}

// HashManifest computes the hex-encoded SHA256 hash of the manifest file at
// path. The hash is computed over the raw file content after normalizing CRLF
// to LF, ensuring cross-platform consistency with the PowerShell engine which
// applies the same normalization before hashing.
func HashManifest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("manifest: cannot read %q for hashing: %w", path, err)
	}

	// Normalize CRLF → LF for cross-platform hash consistency.
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")

	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:]), nil
}

// StripJsoncComments removes single-line (//) and block (/* ... */) comments
// from src without touching content inside JSON string literals. It implements
// the same state-machine approach as Remove-JsoncComments in engine/manifest.ps1.
//
// Rules:
//   - Track whether the cursor is inside a JSON string by watching for
//     unescaped '"' characters.
//   - While inside a string, copy bytes verbatim (no comment stripping).
//   - While outside a string:
//   - '//' starts a single-line comment: skip until '\n' or '\r', then
//     emit the line ending so JSON line numbers remain stable.
//   - '/*' starts a block comment: skip until '*/'.
//   - All other bytes are copied verbatim.
func StripJsoncComments(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inString := false
	escaped := false
	i := 0

	for i < len(src) {
		ch := src[i]

		// Inside a string: handle escape sequences
		if inString {
			if escaped {
				out = append(out, ch)
				escaped = false
				i++
				continue
			}
			if ch == '\\' {
				out = append(out, ch)
				escaped = true
				i++
				continue
			}
			if ch == '"' {
				inString = false
				out = append(out, ch)
				i++
				continue
			}
			out = append(out, ch)
			i++
			continue
		}

		// Outside a string: check for comment starters or string opener
		if ch == '"' {
			inString = true
			out = append(out, ch)
			i++
			continue
		}

		// Single-line comment: // ...
		if ch == '/' && i+1 < len(src) && src[i+1] == '/' {
			i += 2
			// Advance past the comment body, stopping before the line ending.
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				i++
			}
			// Emit the line ending(s) so line numbers are preserved.
			if i < len(src) {
				if src[i] == '\r' {
					out = append(out, src[i])
					i++
				}
				if i < len(src) && src[i] == '\n' {
					out = append(out, src[i])
					i++
				}
			}
			continue
		}

		// Block comment: /* ... */
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}

		out = append(out, ch)
		i++
	}

	return out
}
