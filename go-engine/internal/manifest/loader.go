// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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
	return loadManifestInternal(absPath, visited)
}

// loadManifestInternal is the recursive implementation used by LoadManifest
// and resolveIncludes. visited tracks absolute paths to detect cycles.
func loadManifestInternal(absPath string, visited map[string]bool) (*Manifest, error) {
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

	var m Manifest
	if err := json.Unmarshal(clean, &m); err != nil {
		return nil, fmt.Errorf("manifest: JSON parse error in %q: %w", absPath, err)
	}

	// Validate app-level constraints (e.g. manual.verifyPath required).
	if errs := ValidateManifestApps(&m); len(errs) > 0 {
		return nil, fmt.Errorf("manifest: validation error in %q: %s", absPath, errs[0].Message)
	}

	if len(m.Includes) > 0 {
		baseDir := filepath.Dir(absPath)
		if err := resolveIncludes(&m, baseDir, visited); err != nil {
			return nil, err
		}
	}

	return &m, nil
}

// resolveIncludes iterates over manifest.Includes, loads each included file
// (detecting cycles via visited), and merges the included Apps into m.Apps.
// The includes slice on m is cleared after processing, matching the PS engine.
func resolveIncludes(m *Manifest, basePath string, visited map[string]bool) error {
	includes := m.Includes
	// Clear includes on the parent so it is not serialised again.
	m.Includes = nil

	for _, inc := range includes {
		inclPath := inc
		if !filepath.IsAbs(inclPath) {
			inclPath = filepath.Join(basePath, inclPath)
		}

		inclPath = filepath.Clean(inclPath)

		included, err := loadManifestInternal(inclPath, visited)
		if err != nil {
			return fmt.Errorf("manifest: failed to load include %q: %w", inc, err)
		}

		// Merge: included apps are appended after the parent's own apps.
		m.Apps = append(m.Apps, included.Apps...)

		// Merge restore and verify entries as well (matching PS Resolve-ManifestIncludes).
		m.Restore = append(m.Restore, included.Restore...)
		m.Verify = append(m.Verify, included.Verify...)
	}

	return nil
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
//     - '//' starts a single-line comment: skip until '\n' or '\r', then
//       emit the line ending so JSON line numbers remain stable.
//     - '/*' starts a block comment: skip until '*/'.
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
