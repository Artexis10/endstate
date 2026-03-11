// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package restore implements the four restore strategies (copy, merge-json,
// merge-ini, append), backup creation, journaling, and revert for the
// Endstate Go engine. It mirrors the behaviour of the PowerShell restorers/
// directory and engine/restore.ps1.
package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
)

// RestoreAction describes a single restore operation to execute.
type RestoreAction struct {
	Type       string   `json:"type"`
	Source     string   `json:"source"`
	Target     string   `json:"target"`
	Backup     bool     `json:"backup"`
	Optional   bool     `json:"optional"`
	Exclude    []string `json:"exclude,omitempty"`
	ID         string   `json:"id"`
	FromModule string   `json:"fromModule,omitempty"`
}

// RestoreResult records the outcome of a single restore action.
type RestoreResult struct {
	ID                  string   `json:"id"`
	Source              string   `json:"source"`
	Target              string   `json:"target"`
	Status              string   `json:"status"` // restored, skipped_up_to_date, skipped_missing_source, failed
	BackupPath          string   `json:"backupPath,omitempty"`
	BackupCreated       bool     `json:"backupCreated"`
	TargetExistedBefore bool     `json:"targetExistedBefore"`
	Error               string   `json:"error,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

// RestoreOptions holds configuration for a restore run.
type RestoreOptions struct {
	DryRun      bool
	BackupDir   string
	ManifestDir string
	ExportRoot  string
	RunID       string
}

// sensitiveSegments are path segments that trigger a warning when detected in
// restore target paths. Matches the PowerShell $script:SensitivePathSegments.
var sensitiveSegments = []string{
	".ssh",
	".aws",
	".azure",
	".gnupg",
	".gpg",
	"credentials",
	"secrets",
	"tokens",
	".kube",
	".docker",
	"id_rsa",
	"id_ed25519",
	"id_ecdsa",
}

// CheckSensitivePath returns warnings if path contains any sensitive segments.
func CheckSensitivePath(path string) []string {
	normalized := strings.ToLower(filepath.ToSlash(path))
	var warnings []string
	for _, seg := range sensitiveSegments {
		if strings.Contains(normalized, strings.ToLower(seg)) {
			warnings = append(warnings, fmt.Sprintf("Path contains sensitive segment '%s': %s", seg, path))
		}
	}
	return warnings
}

// expandPath expands environment variables in a path using both Windows-style
// %VAR% expansion (via config.ExpandWindowsEnvVars) and Go-style $VAR
// expansion (via os.ExpandEnv).
func expandPath(p string) string {
	expanded := config.ExpandWindowsEnvVars(p)
	expanded = os.ExpandEnv(expanded)
	// Handle ~ for home directory
	if strings.HasPrefix(expanded, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			expanded = home + expanded[1:]
		}
	}
	return expanded
}

// resolveSource resolves the source path, trying ExportRoot first (Model B),
// then falling back to ManifestDir.
func resolveSource(source string, opts RestoreOptions) string {
	expanded := expandPath(source)

	// If the expanded path is already absolute, use it directly.
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded)
	}

	// Model B: try ExportRoot first when set.
	if opts.ExportRoot != "" {
		candidate := filepath.Join(opts.ExportRoot, expanded)
		candidate = filepath.Clean(candidate)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Fallback to ManifestDir.
	if opts.ManifestDir != "" {
		candidate := filepath.Join(opts.ManifestDir, expanded)
		return filepath.Clean(candidate)
	}

	return filepath.Clean(expanded)
}

// resolveTarget resolves the target path by expanding environment variables.
// If the result is not absolute, it is resolved relative to the current
// working directory.
func resolveTarget(target string) string {
	expanded := expandPath(target)
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded)
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return filepath.Clean(expanded)
	}
	return abs
}

// generateID creates a deterministic ID for a restore action when one is not
// provided, matching the PowerShell Get-RestoreActionId pattern.
func generateID(action RestoreAction) string {
	if action.ID != "" {
		return action.ID
	}
	t := action.Type
	if t == "" {
		t = "copy"
	}
	return fmt.Sprintf("%s:%s->%s", t, filepath.ToSlash(action.Source), filepath.ToSlash(action.Target))
}

// RunRestore iterates over restore entries, dispatches each to the correct
// strategy by Type field, and collects RestoreResult structs.
func RunRestore(entries []RestoreAction, opts RestoreOptions) ([]RestoreResult, error) {
	var results []RestoreResult

	for _, entry := range entries {
		id := generateID(entry)

		// Resolve source and target paths.
		source := resolveSource(entry.Source, opts)
		target := resolveTarget(entry.Target)

		// Check if source exists.
		sourceExists := true
		if _, err := os.Stat(source); os.IsNotExist(err) {
			sourceExists = false
		}

		// Optional entry handling: skip if source does not exist.
		if !sourceExists && entry.Optional {
			results = append(results, RestoreResult{
				ID:     id,
				Source: source,
				Target: target,
				Status: "skipped_missing_source",
			})
			continue
		}

		// Check for sensitive paths.
		var warnings []string
		warnings = append(warnings, CheckSensitivePath(source)...)
		warnings = append(warnings, CheckSensitivePath(target)...)

		// Track whether target existed before.
		_, targetErr := os.Stat(target)
		targetExisted := targetErr == nil

		// Build per-entry options.
		entryOpts := opts

		var result *RestoreResult
		var err error

		strategyType := entry.Type
		if strategyType == "" {
			strategyType = "copy"
		}

		switch strategyType {
		case "copy":
			result, err = RestoreCopy(entry, source, target, entryOpts)
		case "merge-json":
			result, err = RestoreMergeJson(entry, source, target, entryOpts)
		case "merge-ini":
			result, err = RestoreMergeIni(entry, source, target, entryOpts)
		case "append":
			result, err = RestoreAppend(entry, source, target, entryOpts)
		default:
			result = &RestoreResult{
				ID:     id,
				Source: source,
				Target: target,
				Status: "failed",
				Error:  fmt.Sprintf("unsupported restore type: %s", strategyType),
			}
		}

		if err != nil {
			result = &RestoreResult{
				ID:     id,
				Source: source,
				Target: target,
				Status: "failed",
				Error:  err.Error(),
			}
		}

		// Ensure ID and path fields are set.
		result.ID = id
		if result.Source == "" {
			result.Source = source
		}
		if result.Target == "" {
			result.Target = target
		}
		result.TargetExistedBefore = targetExisted

		// Merge warnings.
		result.Warnings = append(warnings, result.Warnings...)

		results = append(results, *result)
	}

	return results, nil
}
