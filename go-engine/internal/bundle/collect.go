// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package bundle provides zip-based profile bundle creation and extraction for
// the Endstate Go engine. A capture bundle is a self-contained zip containing
// manifest.jsonc, metadata.json, and configs/<module-id>/ directories with
// collected config files.
package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

var runRegistryExport = func(key, destination string) error {
	return exec.Command("reg", "export", key, destination, "/y").Run()
}

var runRegistryQuery = func(key, valueName string) ([]byte, error) {
	return exec.Command("reg", "query", key, "/v", valueName).Output()
}

// CollectConfigFiles copies config files from system paths to stagingDir
// according to the module's capture.files mapping. Environment variables in
// source paths are expanded. Optional files that don't exist are skipped.
// ExcludeGlobs filter out matching paths during collection.
//
// Files matching the module's secrets.files patterns are NEVER bundled — this
// enforces the secrets-exclusion invariant (INV-BUNDLE-2) at capture time,
// independent of excludeGlobs.
//
// Returns the list of relative paths (under stagingDir) that were collected and
// the number of files excluded because they matched the module's secrets.files.
func CollectConfigFiles(module *modules.Module, stagingDir string) ([]string, int, error) {
	return CollectConfigFilesWithValidation(module, stagingDir, nil)
}

// CollectConfigFilesWithValidation preserves the legacy collector contract and
// virtualizes only host reads and portable staging writes when context is set.
func CollectConfigFilesWithValidation(module *modules.Module, stagingDir string, context *validationmode.Context) ([]string, int, error) {
	if module.Capture == nil || len(module.Capture.Files) == 0 {
		return nil, 0, nil
	}

	// Determine the module directory name (strip "apps." prefix if present).
	moduleDirName := module.ID
	if strings.HasPrefix(moduleDirName, "apps.") {
		moduleDirName = moduleDirName[5:]
	}

	excludeGlobs := module.Capture.ExcludeGlobs
	var secretsFiles []string
	if module.Secrets != nil {
		secretsFiles = module.Secrets.Files
	}
	resolvedSecrets, err := resolveCaptureSecretPatterns(context, module.ID, secretsFiles, validationmode.HostPathPolicy{})
	if err != nil {
		return nil, 0, err
	}
	var collected []string
	secretsExcluded := 0

	for index, fileEntry := range module.Capture.Files {
		sourcePath := expandPath(fileEntry.Source)
		if context != nil {
			var err error
			sourcePath, err = resolveCaptureHost(context, module.ID, fmt.Sprintf("capture.files[%d].source", index), fileEntry.Source, validationmode.HostPathPolicy{})
			if err != nil {
				return collected, secretsExcluded, err
			}
		}

		destFileName := filepath.Base(fileEntry.Dest)
		relativePath := filepath.ToSlash(filepath.Join("configs", moduleDirName, destFileName))
		destPath := filepath.Join(stagingDir, filepath.FromSlash(relativePath))
		if context != nil {
			var err error
			destPath, err = resolveCapturePortable(context, module.ID, fmt.Sprintf("capture.files[%d].dest", index), stagingDir, relativePath)
			if err != nil {
				return collected, secretsExcluded, err
			}
		}

		// Secret files declared in the module's secrets block are never bundled.
		if captureMatchesSecrets(sourcePath, secretsFiles, resolvedSecrets, context != nil) {
			secretsExcluded++
			continue
		}

		// Check if source matches exclude globs.
		// Exclusions describe content within this declared capture entry. Never
		// match them against unrelated absolute ancestors (for example the OS
		// Temp directory that happens to contain a validation sandbox).
		if matchesExcludeGlobs(filepath.Base(sourcePath), excludeGlobs) {
			continue
		}

		// Check if source exists.
		info, err := os.Stat(sourcePath)
		if err != nil {
			if fileEntry.Optional {
				continue
			}
			if context != nil {
				if os.IsNotExist(err) {
					return collected, secretsExcluded, fmt.Errorf("missing required file at capture.files[%d].source (module: %s)", index, module.ID)
				}
				return collected, secretsExcluded, captureIsolation(module.ID, fmt.Sprintf("capture.files[%d].source", index), "path", fileEntry.Source, validationmode.ErrUnsafePath)
			}
			return collected, secretsExcluded, fmt.Errorf("missing required file: %s (module: %s)", sourcePath, module.ID)
		}

		// Defense-in-depth: skip an oversized installer/executable even if the
		// module explicitly declared it (a config file is never a 10 MiB binary).
		if !info.IsDir() && isOversizedInstaller(sourcePath, info.Size()) {
			continue
		}

		// Ensure destination directory exists.
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			if context != nil {
				return collected, secretsExcluded, captureIsolation(module.ID, fmt.Sprintf("capture.files[%d].dest", index), "portable", fileEntry.Dest, validationmode.ErrUnsafePath)
			}
			return collected, secretsExcluded, fmt.Errorf("failed to create directory %s: %w", destDir, err)
		}

		if info.IsDir() {
			// Copy directory recursively, excluding secret files within.
			n, err := copyDirWithValidation(sourcePath, destPath, excludeGlobs, secretsFiles, resolvedSecrets, context, module.ID, fmt.Sprintf("capture.files[%d].source", index), fileEntry.Source)
			if err != nil {
				if context != nil {
					var isolation *CaptureIsolationError
					if errors.As(err, &isolation) {
						return collected, secretsExcluded, err
					}
					return collected, secretsExcluded, captureIsolation(module.ID, fmt.Sprintf("capture.files[%d].source", index), "path", fileEntry.Source, validationmode.ErrUnsafePath)
				}
				return collected, secretsExcluded, fmt.Errorf("failed to copy directory %s: %w", sourcePath, err)
			}
			secretsExcluded += n
		} else {
			// Copy single file.
			if err := copyFile(sourcePath, destPath); err != nil {
				if context != nil {
					return collected, secretsExcluded, captureIsolation(module.ID, fmt.Sprintf("capture.files[%d].source", index), "path", fileEntry.Source, validationmode.ErrUnsafePath)
				}
				return collected, secretsExcluded, fmt.Errorf("failed to copy file %s: %w", sourcePath, err)
			}
		}

		collected = append(collected, relativePath)
	}

	return collected, secretsExcluded, nil
}

// CollectRegistryKeys exports Windows registry keys defined in module.Capture.RegistryKeys
// to stagingDir using reg.exe. Each key is exported as a .reg file.
// Returns the list of relative paths (under stagingDir) that were collected.
// On non-Windows platforms this is a no-op that returns nil.
func CollectRegistryKeys(module *modules.Module, stagingDir string) ([]string, error) {
	return CollectRegistryKeysWithValidation(module, stagingDir, nil)
}

// CollectRegistryKeysWithValidation maps only the native reg export argument,
// then rewrites the disposable physical namespace back to semantic HKCU bytes
// before publication. A nil context is the established production path.
func CollectRegistryKeysWithValidation(module *modules.Module, stagingDir string, context *validationmode.Context) ([]string, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	if module.Capture == nil || len(module.Capture.RegistryKeys) == 0 {
		return nil, nil
	}

	// Determine the module directory name (strip "apps." prefix if present).
	moduleDirName := module.ID
	if strings.HasPrefix(moduleDirName, "apps.") {
		moduleDirName = moduleDirName[5:]
	}

	var collected []string

	for index, keyEntry := range module.Capture.RegistryKeys {
		exportKey := keyEntry.Key
		semanticKey := keyEntry.Key
		if context != nil {
			var err error
			semanticKey, err = validationmode.NormalizeHKCU(keyEntry.Key)
			if err != nil {
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].key", index), "registry", keyEntry.Key, err)
			}
			exportKey, err = context.MapHKCU(semanticKey)
			if err != nil {
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].key", index), "registry", keyEntry.Key, err)
			}
		}
		// Dest is relative to the configs/<module>/ staging area.
		destFileName := filepath.Base(keyEntry.Dest)
		relativePath := filepath.ToSlash(filepath.Join("configs", moduleDirName, destFileName))
		destPath := filepath.Join(stagingDir, filepath.FromSlash(relativePath))
		if context != nil {
			var err error
			destPath, err = resolveCapturePortable(context, module.ID, fmt.Sprintf("capture.registryKeys[%d].dest", index), stagingDir, relativePath)
			if err != nil {
				return collected, err
			}
		}

		// Ensure destination directory exists.
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			if context != nil {
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].dest", index), "portable", keyEntry.Dest, validationmode.ErrUnsafePath)
			}
			return collected, fmt.Errorf("failed to create directory %s: %w", destDir, err)
		}

		exportDestination := destPath
		cleanup := func() {}
		if context != nil {
			temporary, err := os.CreateTemp(destDir, ".registry-export-*")
			if err != nil {
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].dest", index), "portable", keyEntry.Dest, validationmode.ErrUnsafePath)
			}
			exportDestination = temporary.Name()
			if err := temporary.Close(); err != nil {
				_ = os.Remove(exportDestination)
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].dest", index), "portable", keyEntry.Dest, validationmode.ErrUnsafePath)
			}
			cleanup = func() { _ = os.Remove(exportDestination) }
		}

		// Export via reg.exe only after semantic mapping and destination preflight.
		if err := runRegistryExport(exportKey, exportDestination); err != nil {
			cleanup()
			if keyEntry.Optional {
				continue
			}
			if context != nil {
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].key", index), "registry", keyEntry.Key, validationmode.ErrUnsafeRegistry)
			}
			return collected, fmt.Errorf("reg export failed for key %s: %w", keyEntry.Key, err)
		}
		if context != nil {
			raw, _, err := safepath.ReadRegularFile(exportDestination)
			if err != nil {
				cleanup()
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].key", index), "registry", keyEntry.Key, validationmode.ErrUnsafeRegistry)
			}
			rewritten, err := registryfile.RewriteSubtree(raw, exportKey, semanticKey)
			cleanup()
			if err != nil {
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].key", index), "registry", keyEntry.Key, validationmode.ErrUnsafeRegistry)
			}
			if err := os.WriteFile(destPath, rewritten, 0o644); err != nil {
				return collected, captureIsolation(module.ID, fmt.Sprintf("capture.registryKeys[%d].dest", index), "portable", keyEntry.Dest, validationmode.ErrUnsafePath)
			}
		}

		collected = append(collected, relativePath)
	}

	return collected, nil
}

// CapturedRegistryValue is a single named value captured at value-level (its
// key, name, REG_* type, and string-form data) — the value-scoped analogue of a
// whole-key .reg export. Used to snapshot Windows OS-settings preferences.
type CapturedRegistryValue struct {
	Key       string `json:"key"`
	ValueName string `json:"valueName"`
	ValueType string `json:"valueType,omitempty"`
	Data      string `json:"data,omitempty"`
	// Existed reports whether the value was present at capture time.
	Existed bool `json:"existed"`
}

// CollectRegistryValues reads the specific named values declared in
// module.Capture.RegistryValues — value-level, NOT a whole-key export — and
// writes them as a single JSON snapshot (registry-values.json) under
// configs/<module>/. This is the capture half of the value-level Windows
// OS-settings tier; it never reads or rewrites co-resident unrelated values.
// Returns the list of relative paths (under stagingDir) that were written.
// On non-Windows platforms this is a no-op that returns nil.
func CollectRegistryValues(module *modules.Module, stagingDir string) ([]string, error) {
	return CollectRegistryValuesWithValidation(module, stagingDir, nil)
}

// CollectRegistryValuesWithValidation reads the mapped physical key but keeps
// the module-authored semantic key in registry-values.json.
func CollectRegistryValuesWithValidation(module *modules.Module, stagingDir string, context *validationmode.Context) ([]string, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	if module.Capture == nil || len(module.Capture.RegistryValues) == 0 {
		return nil, nil
	}

	moduleDirName := module.ID
	if strings.HasPrefix(moduleDirName, "apps.") {
		moduleDirName = moduleDirName[5:]
	}

	var captured []CapturedRegistryValue
	for index, ve := range module.Capture.RegistryValues {
		readKey := ve.Key
		if context != nil {
			semantic, err := validationmode.NormalizeHKCU(ve.Key)
			if err != nil {
				return nil, captureIsolation(module.ID, fmt.Sprintf("capture.registryValues[%d].key", index), "registry", ve.Key, err)
			}
			readKey, err = context.MapHKCU(semantic)
			if err != nil {
				return nil, captureIsolation(module.ID, fmt.Sprintf("capture.registryValues[%d].key", index), "registry", ve.Key, err)
			}
		}
		valType, data, ok := readRegistryNamedValue(readKey, ve.ValueName)
		if !ok {
			if ve.Optional {
				captured = append(captured, CapturedRegistryValue{
					Key:       ve.Key,
					ValueName: ve.ValueName,
					Existed:   false,
				})
				continue
			}
			return nil, fmt.Errorf("registry value not found: %s\\%s (module: %s)", ve.Key, ve.ValueName, module.ID)
		}
		captured = append(captured, CapturedRegistryValue{
			Key:       ve.Key,
			ValueName: ve.ValueName,
			ValueType: valType,
			Data:      data,
			Existed:   true,
		})
	}

	if len(captured) == 0 {
		return nil, nil
	}

	destPath := filepath.Join(stagingDir, "configs", moduleDirName, "registry-values.json")
	relativePath := filepath.ToSlash(filepath.Join("configs", moduleDirName, "registry-values.json"))
	if context != nil {
		var err error
		destPath, err = resolveCapturePortable(context, module.ID, "capture.registryValues", stagingDir, relativePath)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		if context != nil {
			return nil, captureIsolation(module.ID, "capture.registryValues", "portable", relativePath, validationmode.ErrUnsafePath)
		}
		return nil, fmt.Errorf("failed to create directory %s: %w", filepath.Dir(destPath), err)
	}
	data, err := json.MarshalIndent(captured, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal captured registry values: %w", err)
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		if context != nil {
			return nil, captureIsolation(module.ID, "capture.registryValues", "portable", relativePath, validationmode.ErrUnsafePath)
		}
		return nil, fmt.Errorf("failed to write %s: %w", destPath, err)
	}

	return []string{relativePath}, nil
}

// readRegistryNamedValue reads a single named value via `reg query <key> /v
// <name>` and returns its REG_* type string and string-form data. ok is false
// when the key or value is missing. Output format from reg.exe is:
//
//	<ValueName>    <REG_TYPE>    <data>
//
// (whitespace-separated, with the data being the remainder of the line).
func readRegistryNamedValue(key, valueName string) (regType, data string, ok bool) {
	out, err := runRegistryQuery(key, valueName)
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Locate the REG_* token; everything before it is the (possibly
		// space-containing) value name, everything after is the data.
		for i, f := range fields {
			if strings.HasPrefix(f, "REG_") {
				if i == 0 {
					break // no name before the type — not a value line
				}
				name := strings.Join(fields[:i], " ")
				if !strings.EqualFold(name, valueName) {
					break
				}
				regType = f
				if i+1 < len(fields) {
					data = strings.Join(fields[i+1:], " ")
				}
				// Normalize DWORD hex (0x...) to decimal for round-trip parity
				// with registry-set's stored decimal form.
				if regType == "REG_DWORD" {
					if n, perr := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(data)), "0x"), 16, 64); perr == nil {
						data = strconv.FormatUint(n, 10)
					}
				}
				return regType, data, true
			}
		}
	}
	return "", "", false
}

// matchesExcludeGlobs checks if a path matches any of the exclude glob patterns.
func matchesExcludeGlobs(path string, excludeGlobs []string) bool {
	if len(excludeGlobs) == 0 {
		return false
	}

	normalizedPath := filepath.ToSlash(path)

	for _, glob := range excludeGlobs {
		matched, err := ConfigPathMatchesExcludeGlob(normalizedPath, glob)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// ConfigPathMatchesExcludeGlob exposes the exact production capture matcher
// with strict malformed-pattern reporting for validation fixture proof. The
// collector preserves authored compatibility by treating malformed patterns
// as non-matches through matchesExcludeGlobs.
func ConfigPathMatchesExcludeGlob(value, glob string) (bool, error) {
	normalizedPath := filepath.ToSlash(value)
	pattern := filepath.ToSlash(glob)
	stripped := strings.TrimPrefix(pattern, "**/")
	if strings.HasSuffix(stripped, "/**") {
		dirName := strings.TrimSuffix(stripped, "/**")
		if dirName == "" {
			return false, fmt.Errorf("exclude glob %q has no directory segment", glob)
		}
		boundedPath := "/" + strings.Trim(normalizedPath, "/") + "/"
		return strings.Contains(boundedPath, "/"+dirName+"/"), nil
	}
	for _, segment := range strings.Split(normalizedPath, "/") {
		matched, err := filepath.Match(stripped, segment)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// captureBloatDirs are directory names (matched case-insensitively, any depth)
// that are never legitimate config — downloaded-update/installer staging,
// caches, and crash dumps. Every module inherits these as a defense-in-depth
// baseline (on top of its own excludeGlobs) so a single misconfigured module
// can't silently bloat the backup and burn the user's quota (cf. the PowerToys
// `Updates/` installer leak that made a capture ~283× larger than it should be).
var captureBloatDirs = map[string]bool{
	"updates":       true,
	"installer":     true,
	"setup":         true,
	"cache":         true,
	"gpucache":      true,
	"code cache":    true,
	"shadercache":   true,
	"crashpad":      true,
	"crash reports": true,
	"temp":          true,
	"logs":          true,
}

// captureBloatBinaryExts are installer/executable extensions that are never
// config. Files with these extensions are skipped above captureBloatBinaryMaxBytes.
var captureBloatBinaryExts = map[string]bool{
	".exe":  true,
	".msi":  true,
	".msix": true,
	".appx": true,
	".dmg":  true,
	".pkg":  true,
}

// captureBloatBinaryMaxBytes is the size over which an installer/executable is
// treated as bloat and skipped. A small bundled helper binary still rides along.
const captureBloatBinaryMaxBytes = 10 * 1024 * 1024 // 10 MiB

// isBloatDirSegment reports whether any segment of relPath is a known bloat
// directory (case-insensitive). relPath is relative to the captured source root,
// so only dirs WITHIN a recursively-captured subtree are matched — never a
// coincidentally-named ancestor of the source itself.
func isBloatDirSegment(relPath string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if captureBloatDirs[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// isOversizedInstaller reports whether path is an installer/executable larger
// than captureBloatBinaryMaxBytes — defense-in-depth against a bloated binary
// that escapes the directory baseline (e.g. a stray installer in an app root).
func isOversizedInstaller(path string, size int64) bool {
	return captureBloatBinaryExts[strings.ToLower(filepath.Ext(path))] &&
		size > captureBloatBinaryMaxBytes
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}

// copyDir recursively copies a directory, respecting excludeGlobs and the
// module's secrets.files patterns. Returns the number of entries skipped
// because they matched a secrets pattern.
func copyDir(src, dst string, excludeGlobs, secretsFiles []string) (int, error) {
	return copyDirWithValidation(src, dst, excludeGlobs, secretsFiles, secretsFiles, nil, "", "", "")
}

func copyDirWithValidation(src, dst string, excludeGlobs, secretsFiles, resolvedSecrets []string, context *validationmode.Context, moduleID, coordinate, authored string) (int, error) {
	// Remove existing destination to prevent nesting (matches PS behaviour).
	if _, err := os.Stat(dst); err == nil {
		if err := os.RemoveAll(dst); err != nil {
			return 0, err
		}
	}

	secretsExcluded := 0
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if context != nil {
			if validationErr := context.ValidateSandboxPath(filepath.Clean(path)); validationErr != nil {
				return captureIsolation(moduleID, coordinate, "path", authored, validationErr)
			}
			if isLinkOrReparse(info) {
				return captureIsolation(moduleID, coordinate, "path", authored, validationmode.ErrUnsafePath)
			}
		}

		// Secret files/dirs declared in the module's secrets block are never bundled.
		if captureMatchesSecrets(path, secretsFiles, resolvedSecrets, context != nil) {
			secretsExcluded++
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Exclusions are scoped to descendants of the declared directory, not
		// absolute host ancestors outside that capture entry.
		if matchesExcludeGlobs(relPath, excludeGlobs) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Defense-in-depth: never bundle known bloat dirs (caches, update/installer
		// staging, crash dumps) or oversized installers, regardless of module config.
		if isBloatDirSegment(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() && isOversizedInstaller(path, info.Size()) {
			return nil
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		return copyFile(path, destPath)
	})
	return secretsExcluded, err
}

// expandPath expands %VAR% / $VAR environment variables and a leading ~ in a path.
func expandPath(p string) string {
	s := config.ExpandEnvVars(p)
	s = os.ExpandEnv(s)
	if strings.HasPrefix(s, "~/") || strings.HasPrefix(s, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, s[2:])
		}
	}
	return s
}

// matchesSecrets reports whether path matches any of the module's secrets.files
// patterns. Patterns may be absolute (env- or ~-rooted) file/dir paths, single-
// glob patterns (*, ?, []), or contain "**" wildcards. Matching is
// case-insensitive (Windows paths are case-insensitive).
func matchesSecrets(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	target := strings.ToLower(filepath.ToSlash(path))
	for _, raw := range patterns {
		p := strings.ToLower(filepath.ToSlash(expandPath(raw)))
		if p == "" {
			continue
		}
		switch {
		case strings.Contains(p, "**"):
			// Ordered-substring match of the literal parts around "**".
			if containsOrdered(target, strings.Split(p, "**")) {
				return true
			}
		case strings.ContainsAny(p, "*?["):
			if m, _ := filepath.Match(p, target); m {
				return true
			}
			if m, _ := filepath.Match(filepath.Base(p), filepath.Base(target)); m {
				return true
			}
		default:
			// Literal file, or directory prefix (path lives under the secret dir).
			if target == p || strings.HasPrefix(target, p+"/") {
				return true
			}
		}
	}
	return false
}

func captureMatchesSecrets(path string, authored, resolved []string, validation bool) bool {
	if validation {
		return matchesSecrets(path, resolved)
	}
	return matchesSecrets(path, authored)
}

// containsOrdered reports whether target contains each trimmed, non-empty part
// in order — used to evaluate "**"-separated secret patterns.
func containsOrdered(target string, parts []string) bool {
	idx := 0
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		j := strings.Index(target[idx:], part)
		if j < 0 {
			return false
		}
		idx += j + len(part)
	}
	return true
}
