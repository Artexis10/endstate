// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
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

// ValidateRegistryImportScope proves that every key section in a registry
// import stays at or below the action's declared HKCU target. This makes the
// target usable as the complete collision and rollback scope for the import.
func ValidateRegistryImportScope(source, target string) error {
	if err := ValidateRegistryTarget(target); err != nil {
		return err
	}
	data, _, err := safepath.ReadRegularFile(source)
	if err != nil {
		return fmt.Errorf("read registry import safely: %w", err)
	}
	content, err := decodeRegistryImport(data)
	if err != nil {
		return err
	}
	declared := normalizeRegistryImportKey(target)
	sections := 0
	for lineNumber, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "[") {
			continue
		}
		if !strings.HasSuffix(line, "]") || strings.Count(line, "[") != 1 || strings.Count(line, "]") != 1 {
			return fmt.Errorf("malformed registry key section on line %d", lineNumber+1)
		}
		key := strings.TrimSpace(line[1 : len(line)-1])
		key = strings.TrimSpace(strings.TrimPrefix(key, "-"))
		if err := ValidateRegistryTarget(key); err != nil {
			return err
		}
		normalized := normalizeRegistryImportKey(key)
		if normalized != declared && !strings.HasPrefix(normalized, declared+`\`) {
			return fmt.Errorf("registry key %q is outside declared target %q", key, target)
		}
		sections++
	}
	if sections == 0 {
		return fmt.Errorf("registry import contains no registry key sections")
	}
	return nil
}

func decodeRegistryImport(data []byte) (string, error) {
	switch {
	case len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe:
		payload := data[2:]
		if len(payload)%2 != 0 {
			return "", fmt.Errorf("registry import has malformed UTF-16LE content")
		}
		words := make([]uint16, len(payload)/2)
		for index := range words {
			words[index] = binary.LittleEndian.Uint16(payload[index*2:])
		}
		return string(utf16.Decode(words)), nil
	case len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff:
		return "", fmt.Errorf("registry import UTF-16BE encoding is unsupported")
	case len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf:
		data = data[3:]
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("registry import is not valid UTF-8 or UTF-16LE")
	}
	return string(data), nil
}

func normalizeRegistryImportKey(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "/", `\`))
	value = strings.TrimPrefix(value, `hkey_current_user\`)
	value = strings.TrimPrefix(value, `hkcu\`)
	return strings.Trim(value, `\`)
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
	if err := ValidateRegistryImportScope(source, entry.Target); err != nil {
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

	// Probe whether the target key exists (used for TargetExistedBefore and backup).
	queryCmd := exec.Command("reg", "query", entry.Target)
	keyExists := queryCmd.Run() == nil
	result.TargetExistedBefore = keyExists

	// Backup the existing registry key when requested.
	if entry.Backup {
		if keyExists {
			backupDir := opts.BackupDir
			if backupDir == "" {
				backupDir = defaultBackupDir(opts.RunID)
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
