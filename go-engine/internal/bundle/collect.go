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
// Returns the list of relative paths (under stagingDir) that were collected.
func CollectConfigFiles(module *modules.Module, stagingDir string) ([]string, error) {
	if module.Capture == nil || len(module.Capture.Files) == 0 {
		return nil, nil
	}

	// Determine the module directory name (strip "apps." prefix if present).
	moduleDirName := module.ID
	if strings.HasPrefix(moduleDirName, "apps.") {
		moduleDirName = moduleDirName[5:]
	}

	excludeGlobs := module.Capture.ExcludeGlobs
	var collected []string

	for _, fileEntry := range module.Capture.Files {
		sourcePath := config.ExpandWindowsEnvVars(fileEntry.Source)
		sourcePath = os.ExpandEnv(sourcePath)

		// Expand ~ to home directory.
		if strings.HasPrefix(sourcePath, "~/") || strings.HasPrefix(sourcePath, "~\\") {
			home, err := os.UserHomeDir()
			if err == nil {
				sourcePath = filepath.Join(home, sourcePath[2:])
			}
		}

		destFileName := filepath.Base(fileEntry.Dest)
		destPath := filepath.Join(stagingDir, "configs", moduleDirName, destFileName)
		relativePath := filepath.ToSlash(filepath.Join("configs", moduleDirName, destFileName))

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
			return collected, fmt.Errorf("missing required file: %s (module: %s)", sourcePath, module.ID)
		}

		// Ensure destination directory exists.
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return collected, fmt.Errorf("failed to create directory %s: %w", destDir, err)
		}

		if info.IsDir() {
			// Copy directory recursively.
			if err := copyDir(sourcePath, destPath, excludeGlobs); err != nil {
				return collected, fmt.Errorf("failed to copy directory %s: %w", sourcePath, err)
			}
		} else {
			// Copy single file.
			if err := copyFile(sourcePath, destPath); err != nil {
				return collected, fmt.Errorf("failed to copy file %s: %w", sourcePath, err)
			}
		}

		collected = append(collected, relativePath)
	}

	return collected, nil
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

// copyDir recursively copies a directory, respecting excludeGlobs.
func copyDir(src, dst string, excludeGlobs []string) error {
	// Remove existing destination to prevent nesting (matches PS behaviour).
	if _, err := os.Stat(dst); err == nil {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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
}
