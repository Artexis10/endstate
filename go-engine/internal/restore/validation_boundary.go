// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type legacyValidationBoundary struct {
	context   *validationmode.Context
	backupDir string
}

func (boundary legacyValidationBoundary) resolveHost(authored string) (string, error) {
	if boundary.context == nil {
		return resolveTarget(authored), nil
	}
	resolved, err := boundary.context.ResolveHostPath(authored, validationmode.HostPathPolicy{})
	if err != nil {
		return "", fmt.Errorf("validate restore target %q: %w", authored, err)
	}
	return resolved, nil
}

func (boundary legacyValidationBoundary) validateConcrete(path string) error {
	if boundary.context != nil {
		if err := boundary.context.ValidateSandboxPath(filepath.Clean(path)); err != nil {
			return err
		}
	}
	return ValidateFilesystemTarget(path)
}

func (boundary legacyValidationBoundary) resolveBackup(identity string) (string, error) {
	if boundary.context == nil {
		return filepath.Clean(identity), nil
	}
	if identity == "" || filepath.IsAbs(identity) || containsFold(identity, boundary.context.Root()) {
		return "", fmt.Errorf("validation backup identity is not semantic")
	}
	resolved := expandPath(identity)
	if !filepath.IsAbs(resolved) && boundary.backupDir != "" {
		resolved = filepath.Join(boundary.backupDir, filepath.FromSlash(identity))
	}
	resolved = filepath.Clean(resolved)
	if err := boundary.context.ValidateSandboxPath(resolved); err != nil {
		return "", fmt.Errorf("validate restore backup identity: %w", err)
	}
	if err := ValidateFilesystemTarget(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func resolveRestoreSource(source string, opts RestoreOptions) (string, error) {
	if opts.ValidationContext == nil {
		return resolveSource(source, opts), nil
	}
	portable := strings.ReplaceAll(source, `\`, "/")
	portable = strings.TrimPrefix(portable, "./")
	if portable == "" || filepath.IsAbs(expandPath(source)) {
		return "", fmt.Errorf("validate restore source %q: %w", source, validationmode.ErrUnsafePath)
	}
	roots := []string{opts.ExportRoot, opts.ManifestDir}
	for index, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if err := opts.ValidationContext.ValidateSandboxPath(root); err != nil {
			return "", fmt.Errorf("validate restore source authority: %w", err)
		}
		resolved, err := validationmode.ResolvePortablePath(root, portable)
		if err != nil {
			return "", fmt.Errorf("validate restore source %q: %w", source, err)
		}
		if index == 0 && opts.ExportRoot != "" {
			if _, err := os.Stat(resolved); err == nil {
				return resolved, nil
			} else if !os.IsNotExist(err) {
				return "", fmt.Errorf("inspect restore source %q: %w", source, err)
			}
			continue
		}
		return resolved, nil
	}
	return "", fmt.Errorf("validate restore source %q: no portable source authority", source)
}

func projectValidationRestoreResult(result *RestoreResult, entry RestoreAction, opts RestoreOptions) *RestoreResult {
	if result == nil || opts.ValidationContext == nil {
		return result
	}
	physicalSource, physicalTarget, physicalBackup := result.Source, result.Target, result.BackupPath
	if entry.Source != "" {
		result.Source = entry.Source
	}
	if entry.Type == "delete-glob" && physicalTarget != "" {
		semantic, err := opts.ValidationContext.DisplayPath(filepath.Clean(physicalTarget))
		if err != nil {
			result.Status = "failed"
			result.Error = "delete-glob target escaped validation-owned storage"
		} else {
			result.Target = semantic
		}
	} else if entry.Type == "registry-set" {
		result.Target = registrySetTarget(entry)
	} else if entry.Target != "" {
		result.Target = entry.Target
	}
	if result.BackupPath != "" {
		if !isValidationDisplayPath(result.BackupPath) {
			semantic, err := opts.ValidationContext.DisplayPath(filepath.Clean(result.BackupPath))
			if err != nil {
				result.Status = "failed"
				result.Error = "backup identity escaped validation-owned storage"
				result.BackupPath = ""
				result.BackupCreated = false
			} else {
				result.BackupPath = semantic
			}
		}
	}
	replacements := [][2]string{
		{physicalSource, result.Source},
		{physicalTarget, result.Target},
		{physicalBackup, result.BackupPath},
		{opts.ValidationContext.Root(), "$ENDSTATE_ROOT"},
		{opts.ValidationContext.RegistryNamespace(), "HKCU"},
		{opts.ValidationContext.Descriptor().Nonce, "validation"},
	}
	result.Error = replaceFoldMany(result.Error, replacements)
	for index := range result.Warnings {
		result.Warnings[index] = replaceFoldMany(result.Warnings[index], replacements)
	}
	return result
}

func isValidationDisplayPath(value string) bool {
	return strings.HasPrefix(value, "$ENDSTATE_ROOT/") ||
		(strings.HasPrefix(value, "%") && strings.Contains(value[1:], "%"))
}

func semanticFilesystemScratch(target, suffix, kind string) string {
	clean := filepath.Clean(target)
	return filepath.Join(filepath.Dir(clean), "."+filepath.Base(clean)+".endstate-revert-"+suffix+"-"+kind)
}

func replaceFoldMany(value string, replacements [][2]string) string {
	for _, replacement := range replacements {
		if replacement[0] == "" || replacement[0] == replacement[1] {
			continue
		}
		value = replaceFold(value, replacement[0], replacement[1])
	}
	return value
}

func replaceFold(value, old, replacement string) string {
	for {
		index := strings.Index(strings.ToLower(value), strings.ToLower(old))
		if index < 0 {
			return value
		}
		value = value[:index] + replacement + value[index+len(old):]
	}
}

func containsFold(value, fragment string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(fragment))
}
