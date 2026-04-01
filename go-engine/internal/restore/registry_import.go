// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ValidateRegistryTarget validates that target is an HKCU (current user) registry
// key path. Returns a non-nil error if the key is not under HKCU or
// HKEY_CURRENT_USER. This function is exported so that it can be tested on any
// platform independently of the GOOS guard in RestoreRegistryImport.
func ValidateRegistryTarget(target string) error {
	if !isHKCUKey(target) {
		return fmt.Errorf("registry-import only supports HKCU keys: %s", target)
	}
	return nil
}

// isHKCUKey returns true when target begins with HKCU\ or HKEY_CURRENT_USER\
// (case-insensitive).
func isHKCUKey(target string) bool {
	upper := strings.ToUpper(target)
	return strings.HasPrefix(upper, `HKCU\`) ||
		strings.HasPrefix(upper, `HKEY_CURRENT_USER\`)
}

// RestoreRegistryImport implements the registry-import restore strategy.
// source is the resolved path to a .reg file on disk.
// entry.Target is the raw Windows registry key path (e.g. HKCU\Software\...).
//
// The function validates that the target is an HKCU key, optionally exports
// the existing key as a backup, and then imports the .reg file via reg.exe.
func RestoreRegistryImport(entry RestoreAction, source string, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Source:      source,
		Target:      entry.Target,
		RestoreType: "registry-import",
	}

	// Validate target key (cross-platform — no OS dependency).
	if err := ValidateRegistryTarget(entry.Target); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result, nil
	}

	// Registry operations are Windows-only.
	if runtime.GOOS != "windows" {
		result.Status = "failed"
		result.Error = "registry-import is only supported on Windows"
		return result, nil
	}

	// Check whether the source .reg file exists.
	if _, err := os.Stat(source); os.IsNotExist(err) {
		if entry.Optional {
			result.Status = "skipped_missing_source"
			return result, nil
		}
		result.Status = "failed"
		result.Error = fmt.Sprintf("source not found: %s", source)
		return result, nil
	}

	// Probe whether the target key exists (used for TargetExistedBefore and backup).
	queryCmd := exec.Command("reg", "query", entry.Target)
	keyExists := queryCmd.Run() == nil
	result.TargetExistedBefore = keyExists

	// Backup the existing registry key when requested.
	if entry.Backup {
		if keyExists {
			backupDir := opts.BackupDir
			if backupDir == "" {
				backupDir = filepath.Join("state", "backups", opts.RunID)
			}
			if mkErr := os.MkdirAll(backupDir, 0755); mkErr != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("backup: cannot create backup directory: %v", mkErr)
				return result, nil
			}
			// Use a sanitized filename derived from the registry key.
			safeKey := strings.NewReplacer(`\`, "_", ` `, "_").Replace(entry.Target)
			backupPath := filepath.Join(backupDir, safeKey+".reg")
			exportCmd := exec.Command("reg", "export", entry.Target, backupPath, "/y")
			if exportErr := exportCmd.Run(); exportErr != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("backup: reg export failed: %v", exportErr)
				return result, nil
			}
			result.BackupPath = backupPath
			result.BackupCreated = true
		}
	}

	// Dry-run: report what would happen without touching the registry.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}

	// Import the .reg file.
	importCmd := exec.Command("reg", "import", source)
	if err := importCmd.Run(); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("reg import failed: %v", err)
		return result, nil
	}

	result.Status = "restored"
	return result, nil
}
