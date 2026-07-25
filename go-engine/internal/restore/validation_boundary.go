// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

var (
	legacyRestoreIOCheckpoint      = func(string, string) {}
	legacyRestoreStatNative        = os.Stat
	legacyRestoreLstatNative       = os.Lstat
	legacyRestoreReadFileNative    = os.ReadFile
	legacyRestoreReadDirNative     = os.ReadDir
	legacyRestoreReadRegularNative = safepath.ReadRegularFile
	legacyRestoreMkdirAllNative    = os.MkdirAll
	legacyRestoreMkdirNative       = os.Mkdir
	legacyRestoreRemoveNative      = os.Remove
	legacyRestoreWriteFileNative   = os.WriteFile
	legacyRestoreRenameNative      = os.Rename
	legacyRestoreCreateTempNative  = os.CreateTemp
	legacyRestoreAtomicWriteNative = safepath.AtomicWriteFile
	legacyRestoreAtomicCopyNative  = safepath.AtomicCopyFile
)

type legacyValidationBoundary struct {
	context   *validationmode.Context
	backupDir string
}

func (boundary legacyValidationBoundary) authorizeIO(operation, path string) error {
	if boundary.context == nil {
		return nil
	}
	legacyRestoreIOCheckpoint(operation, path)
	clean := filepath.Clean(path)
	if err := boundary.context.ValidateSandboxPath(clean); err != nil {
		return fmt.Errorf("validation restore %s is outside disposable storage: %w", operation, validationmode.ErrUnsafePath)
	}
	if err := ValidateFilesystemTarget(clean); err != nil {
		return fmt.Errorf("validation restore %s is unsafe: %w", operation, validationmode.ErrUnsafePath)
	}
	return nil
}

func (boundary legacyValidationBoundary) authorizeBackupDir(path string) error {
	return boundary.authorizeIO("backup-directory", path)
}

func restoreBackupDirectory(opts RestoreOptions) string {
	if opts.BackupDir != "" {
		return opts.BackupDir
	}
	return defaultBackupDir(opts.RunID)
}

func (boundary legacyValidationBoundary) stat(operation, path string) (os.FileInfo, error) {
	if boundary.context == nil {
		return os.Stat(path)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return nil, err
	}
	return legacyRestoreStatNative(path)
}

func (boundary legacyValidationBoundary) lstat(operation, path string) (os.FileInfo, error) {
	if boundary.context == nil {
		return os.Lstat(path)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return nil, err
	}
	return legacyRestoreLstatNative(path)
}

func (boundary legacyValidationBoundary) readFile(operation, path string) ([]byte, error) {
	if boundary.context == nil {
		return os.ReadFile(path)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return nil, err
	}
	return legacyRestoreReadFileNative(path)
}

func (boundary legacyValidationBoundary) readDir(operation, path string) ([]os.DirEntry, error) {
	if boundary.context == nil {
		return os.ReadDir(path)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return nil, err
	}
	return legacyRestoreReadDirNative(path)
}

func (boundary legacyValidationBoundary) readRegularFile(operation, path string) ([]byte, os.FileMode, error) {
	if boundary.context == nil {
		return safepath.ReadRegularFile(path)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return nil, 0, err
	}
	return legacyRestoreReadRegularNative(path)
}

func (boundary legacyValidationBoundary) mkdirAll(operation, path string, mode os.FileMode) error {
	if boundary.context == nil {
		return os.MkdirAll(path, mode)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return err
	}
	return legacyRestoreMkdirAllNative(path, mode)
}

func (boundary legacyValidationBoundary) mkdir(operation, path string, mode os.FileMode) error {
	if boundary.context == nil {
		return os.Mkdir(path, mode)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return err
	}
	return legacyRestoreMkdirNative(path, mode)
}

func (boundary legacyValidationBoundary) remove(operation, path string) error {
	if boundary.context == nil {
		return os.Remove(path)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return err
	}
	return legacyRestoreRemoveNative(path)
}

func (boundary legacyValidationBoundary) writeFile(operation, path string, data []byte, mode os.FileMode) error {
	if boundary.context == nil {
		return os.WriteFile(path, data, mode)
	}
	if err := boundary.authorizeIO(operation, path); err != nil {
		return err
	}
	return legacyRestoreWriteFileNative(path, data, mode)
}

func (boundary legacyValidationBoundary) rename(operation, source, destination string) error {
	if boundary.context == nil {
		return os.Rename(source, destination)
	}
	if err := boundary.authorizeIO(operation+"-source", source); err != nil {
		return err
	}
	if err := boundary.authorizeIO(operation+"-destination", destination); err != nil {
		return err
	}
	return legacyRestoreRenameNative(source, destination)
}

func (boundary legacyValidationBoundary) createTemp(operation, directory, pattern string) (*os.File, error) {
	if boundary.context == nil {
		return os.CreateTemp(directory, pattern)
	}
	if err := boundary.authorizeIO(operation+"-directory", directory); err != nil {
		return nil, err
	}
	return legacyRestoreCreateTempNative(directory, pattern)
}

func (boundary legacyValidationBoundary) atomicWrite(operation, target string, data []byte, mode os.FileMode) error {
	if boundary.context == nil {
		return atomicRestoreWrite(target, data, mode)
	}
	if err := boundary.mkdirAll(operation+"-parent-mkdir", filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := boundary.authorizeIO(operation, target); err != nil {
		return err
	}
	if err := legacyRestoreAtomicWriteNative(target, data, mode); err != nil {
		return err
	}
	return boundary.authorizeIO(operation+"-result", target)
}

func (boundary legacyValidationBoundary) atomicCopy(operation, source, target string) error {
	if boundary.context == nil {
		return atomicRestoreCopy(source, target)
	}
	if err := boundary.mkdirAll(operation+"-parent-mkdir", filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := boundary.authorizeIO(operation, target); err != nil {
		return err
	}
	info, err := boundary.lstat("source-atomic-copy", source)
	if err != nil {
		return err
	}
	if isLinkOrReparse(info) || !info.Mode().IsRegular() {
		return fmt.Errorf("validation restore source is not a safe regular file")
	}
	if err := legacyRestoreAtomicCopyNative(source, target, info.Mode()); err != nil {
		return err
	}
	return boundary.authorizeIO(operation+"-result", target)
}

func walkTreeWithBoundary(
	root string,
	boundary legacyValidationBoundary,
	operation string,
	walkFn filepath.WalkFunc,
) error {
	if boundary.context == nil {
		return filepath.Walk(root, walkFn)
	}
	var visit func(string) (bool, error)
	visit = func(path string) (bool, error) {
		info, err := boundary.lstat(operation+"-member-lstat", path)
		if err != nil {
			return false, walkFn(path, nil, err)
		}
		if err := walkFn(path, info, nil); err != nil {
			if err == filepath.SkipDir && info.IsDir() {
				return true, nil
			}
			return false, err
		}
		if !info.IsDir() {
			return false, nil
		}
		entries, err := boundary.readDir(operation+"-directory-read", path)
		if err != nil {
			return false, walkFn(path, info, err)
		}
		for _, entry := range entries {
			_, err := visit(filepath.Join(path, entry.Name()))
			if err != nil {
				return false, err
			}
		}
		return false, nil
	}
	_, err := visit(filepath.Clean(root))
	return err
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
			boundary := legacyValidationBoundary{context: opts.ValidationContext}
			if _, err := boundary.stat("source-authority-stat", resolved); err == nil {
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
