// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package restore implements restore strategies (copy, merge-json, merge-ini,
// append, delete-glob), backup creation, journaling, and revert for the
// Endstate Go engine.
package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// RestoreAction describes a single restore operation to execute.
type RestoreAction struct {
	Type       string   `json:"type"`
	Source     string   `json:"source"`
	Target     string   `json:"target"`
	Pattern    string   `json:"pattern,omitempty"`
	Reason     string   `json:"reason,omitempty"`
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
	RestoreType         string   `json:"restoreType,omitempty"`
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
	if t == "delete-glob" {
		return fmt.Sprintf("delete-glob:%s/%s", filepath.ToSlash(action.Target), action.Pattern)
	}
	return fmt.Sprintf("%s:%s->%s", t, filepath.ToSlash(action.Source), filepath.ToSlash(action.Target))
}

// RunRestore iterates over restore entries, dispatches each to the correct
// strategy by Type field, and collects RestoreResult structs. emitter is
// optional (nil is a no-op); when provided, item events are emitted for each
// restore action as it completes.
func RunRestore(entries []RestoreAction, opts RestoreOptions, emitter *events.Emitter) ([]RestoreResult, error) {
	var results []RestoreResult

	for _, entry := range entries {
		id := generateID(entry)

		// delete-glob: no source path, may produce multiple results.
		if entry.Type == "delete-glob" {
			target := resolveTarget(entry.Target)

			// Check if target directory exists.
			if _, err := os.Stat(target); os.IsNotExist(err) {
				if entry.Optional {
					r := RestoreResult{
						ID:     id,
						Target: target,
						Status: "skipped_missing_source",
					}
					emitRestoreItemEvent(emitter, entry, r)
					results = append(results, r)
					continue
				}
				r := RestoreResult{
					ID:     id,
					Target: target,
					Status: "failed",
					Error:  fmt.Sprintf("target directory not found: %s", target),
				}
				emitRestoreItemEvent(emitter, entry, r)
				results = append(results, r)
				continue
			}

			deleteResults, err := RestoreDeleteGlob(entry, target, opts)
			if err != nil {
				r := RestoreResult{
					ID:     id,
					Target: target,
					Status: "failed",
					Error:  err.Error(),
				}
				emitRestoreItemEvent(emitter, entry, r)
				results = append(results, r)
				continue
			}

			for i := range deleteResults {
				deleteResults[i].ID = id
				emitRestoreItemEvent(emitter, entry, deleteResults[i])
			}
			results = append(results, deleteResults...)
			continue
		}

		// registry-import: target is a registry key path, not a file path.
		// Dispatch early to avoid the generic file-based source/target resolution.
		if entry.Type == "registry-import" {
			source := resolveSource(entry.Source, opts)
			result, err := RestoreRegistryImport(entry, source, opts)
			if err != nil {
				r := RestoreResult{
					ID:          id,
					Source:      source,
					Target:      entry.Target,
					Status:      "failed",
					Error:       err.Error(),
					RestoreType: "registry-import",
				}
				emitRestoreItemEvent(emitter, entry, r)
				results = append(results, r)
				continue
			}
			result.ID = id
			if result.Source == "" {
				result.Source = source
			}
			if result.Target == "" {
				result.Target = entry.Target
			}
			emitRestoreItemEvent(emitter, entry, *result)
			results = append(results, *result)
			continue
		}

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
			r := RestoreResult{
				ID:     id,
				Source: source,
				Target: target,
				Status: "skipped_missing_source",
			}
			emitRestoreItemEvent(emitter, entry, r)
			results = append(results, r)
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

		emitRestoreItemEvent(emitter, entry, *result)
		results = append(results, *result)
	}

	return results, nil
}

// emitRestoreItemEvent emits an item event for a completed restore action when
// emitter is non-nil. Status mapping:
//   - "restored"              → item status "installed", reason ""
//   - "skipped_up_to_date"   → item status "skipped",   reason "already_installed"
//   - "skipped_missing_source"→ item status "skipped",   reason "missing"
//   - "failed"               → item status "failed",    reason "restore_failed"
//
// The event id is the restore entry target (or source when target is empty).
// The driver field is the restore strategy type (e.g. "copy", "merge-json").
// The name field is FromModule when set.
func emitRestoreItemEvent(emitter *events.Emitter, entry RestoreAction, result RestoreResult) {
	if emitter == nil {
		return
	}

	// Determine event id: prefer target, fall back to source.
	eventID := result.Target
	if eventID == "" {
		eventID = result.Source
	}

	// Determine driver (restore strategy type).
	driverName := entry.Type
	if driverName == "" {
		driverName = "copy"
	}

	// Determine name (module that owns this entry).
	name := entry.FromModule

	var itemStatus, reason string
	switch result.Status {
	case "restored":
		itemStatus = "installed"
		reason = ""
	case "skipped_up_to_date":
		itemStatus = "skipped"
		reason = "already_installed"
	case "skipped_missing_source":
		itemStatus = "skipped"
		reason = "missing"
	default: // "failed" or any unknown status
		itemStatus = "failed"
		reason = "restore_failed"
	}

	emitter.EmitItem(eventID, driverName, itemStatus, reason, result.Error, name)
}
