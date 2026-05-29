// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package bundle provides zip-based profile bundle creation and extraction for
// the Endstate Go engine. A capture bundle is a self-contained zip containing
// manifest.jsonc, metadata.json, and configs/<module-id>/ directories with
// collected config files.
package bundle

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

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
	var collected []string
	secretsExcluded := 0

	for _, fileEntry := range module.Capture.Files {
		sourcePath := expandPath(fileEntry.Source)

		destFileName := filepath.Base(fileEntry.Dest)
		destPath := filepath.Join(stagingDir, "configs", moduleDirName, destFileName)
		relativePath := filepath.ToSlash(filepath.Join("configs", moduleDirName, destFileName))

		// Secret files declared in the module's secrets block are never bundled.
		if matchesSecrets(sourcePath, secretsFiles) {
			secretsExcluded++
			continue
		}

		// Check if source matches exclude globs.
		if matchesExcludeGlobs(sourcePath, excludeGlobs) {
			continue
		}

		// Check if source exists.
		info, err := os.Stat(sourcePath)
		if err != nil {
			if fileEntry.Optional {
				continue
			}
			return collected, secretsExcluded, fmt.Errorf("missing required file: %s (module: %s)", sourcePath, module.ID)
		}

		// Ensure destination directory exists.
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return collected, secretsExcluded, fmt.Errorf("failed to create directory %s: %w", destDir, err)
		}

		if info.IsDir() {
			// Copy directory recursively, excluding secret files within.
			n, err := copyDir(sourcePath, destPath, excludeGlobs, secretsFiles)
			if err != nil {
				return collected, secretsExcluded, fmt.Errorf("failed to copy directory %s: %w", sourcePath, err)
			}
			secretsExcluded += n
		} else {
			// Copy single file.
			if err := copyFile(sourcePath, destPath); err != nil {
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

	for _, keyEntry := range module.Capture.RegistryKeys {
		// Dest is relative to the configs/<module>/ staging area.
		destFileName := filepath.Base(keyEntry.Dest)
		destPath := filepath.Join(stagingDir, "configs", moduleDirName, destFileName)
		relativePath := filepath.ToSlash(filepath.Join("configs", moduleDirName, destFileName))

		// Ensure destination directory exists.
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return collected, fmt.Errorf("failed to create directory %s: %w", destDir, err)
		}

		// Export via reg.exe.
		cmd := exec.Command("reg", "export", keyEntry.Key, destPath, "/y")
		if err := cmd.Run(); err != nil {
			if keyEntry.Optional {
				continue
			}
			return collected, fmt.Errorf("reg export failed for key %s: %w", keyEntry.Key, err)
		}

		collected = append(collected, relativePath)
	}

	return collected, nil
}

// matchesExcludeGlobs checks if a path matches any of the exclude glob patterns.
func matchesExcludeGlobs(path string, excludeGlobs []string) bool {
	if len(excludeGlobs) == 0 {
		return false
	}

	normalizedPath := filepath.ToSlash(path)

	for _, glob := range excludeGlobs {
		pattern := filepath.ToSlash(glob)

		// Strip leading **/ — means "anywhere in tree".
		stripped := strings.TrimPrefix(pattern, "**/")

		if strings.HasSuffix(stripped, "/**") {
			// Pattern like "**/Cache/**" — exclude if directory segment appears in path.
			dirName := strings.TrimSuffix(stripped, "/**")
			if strings.Contains(normalizedPath, "/"+dirName+"/") {
				return true
			}
		} else {
			// Pattern like "*.log" or "state.vscdb*" — match against any path segment.
			segments := strings.Split(normalizedPath, "/")
			for _, seg := range segments {
				matched, _ := filepath.Match(stripped, seg)
				if matched {
					return true
				}
			}
		}
	}

	return false
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

		// Secret files/dirs declared in the module's secrets block are never bundled.
		if matchesSecrets(path, secretsFiles) {
			secretsExcluded++
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Check exclude globs.
		if matchesExcludeGlobs(path, excludeGlobs) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
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
