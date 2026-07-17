// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RestoreDeleteGlob implements the delete-glob restore strategy. It deletes
// files matching a glob pattern inside the target directory. Each deleted file
// is backed up first (when BackupDir is set) so that revert can undo the
// deletions. In dry-run mode, matching files are reported but not deleted.
func RestoreDeleteGlob(entry RestoreAction, target string, opts RestoreOptions) ([]RestoreResult, error) {
	if err := ValidateDeleteGlobPattern(entry.Pattern); err != nil {
		return nil, err
	}
	if err := ValidateFilesystemTarget(target); err != nil {
		return nil, err
	}
	pattern := strings.ReplaceAll(entry.Pattern, `\`, "/")
	matches, err := expandSafeDeleteGlob(target, strings.Split(pattern, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", entry.Pattern, err)
	}
	sort.Slice(matches, func(left, right int) bool {
		return strings.ToLower(filepath.ToSlash(matches[left])) < strings.ToLower(filepath.ToSlash(matches[right]))
	})

	// Filter to files only (skip directories).
	var files []string
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil || info.IsDir() {
			continue
		}
		files = append(files, m)
	}

	if len(files) == 0 {
		return []RestoreResult{{
			Target: target,
			Status: "skipped_up_to_date",
		}}, nil
	}

	var results []RestoreResult

	for _, f := range files {
		r := RestoreResult{
			Target:              f,
			TargetExistedBefore: true,
		}

		if opts.DryRun {
			r.Status = "restored"
			results = append(results, r)
			continue
		}

		// Back up before deleting so revert can restore the file.
		if opts.BackupDir != "" {
			backupPath, backupErr := CreateBackup(f, opts.BackupDir)
			if backupErr != nil {
				r.Status = "failed"
				r.Error = fmt.Sprintf("backup before delete failed: %v", backupErr)
				results = append(results, r)
				continue
			}
			r.BackupCreated = true
			r.BackupPath = backupPath
		}

		if err := os.Remove(f); err != nil {
			r.Status = "failed"
			r.Error = fmt.Sprintf("delete failed: %v", err)
			results = append(results, r)
			continue
		}

		r.Status = "restored"
		results = append(results, r)
	}

	return results, nil
}

// ValidateDeleteGlobPattern rejects patterns that can escape the declared
// target directory or whose host interpretation is not portable.
func ValidateDeleteGlobPattern(pattern string) error {
	if pattern == "" || pattern != strings.TrimSpace(pattern) || filepath.IsAbs(pattern) || filepath.VolumeName(pattern) != "" ||
		strings.ContainsAny(pattern, "$%~\x00") {
		return fmt.Errorf("delete-glob pattern must be a portable relative glob")
	}
	normalized := strings.ReplaceAll(pattern, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return fmt.Errorf("delete-glob pattern must be a portable relative glob")
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == "" || component == "." || component == ".." || component != strings.TrimSpace(component) {
			return fmt.Errorf("delete-glob pattern must be a portable relative glob")
		}
		if _, err := filepath.Match(component, "probe"); err != nil {
			return fmt.Errorf("invalid delete-glob pattern %q: %w", pattern, err)
		}
	}
	return nil
}

func expandSafeDeleteGlob(root string, components []string) ([]string, error) {
	var matches []string
	var visit func(string, int) error
	visit = func(current string, componentIndex int) error {
		entries, err := os.ReadDir(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			matched, err := filepath.Match(components[componentIndex], entry.Name())
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
			candidate := filepath.Join(current, entry.Name())
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if isLinkOrReparse(info) {
				return fmt.Errorf("delete-glob path component %q is a link or reparse point", candidate)
			}
			if componentIndex == len(components)-1 {
				if info.Mode().IsRegular() {
					matches = append(matches, candidate)
				}
				continue
			}
			if info.IsDir() {
				if err := visit(candidate, componentIndex+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(filepath.Clean(root), 0); err != nil {
		return nil, err
	}
	return matches, nil
}
