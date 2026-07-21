// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// ValidateFilesystemTarget rejects existing link/reparse components and
// non-directory parents so a lexical restore target cannot alias elsewhere.
func ValidateFilesystemTarget(target string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("restore target is empty")
	}
	clean, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return err
	}
	clean, err = safepath.CanonicalizePlatformRootAlias(clean)
	if err != nil {
		return err
	}
	chain := make([]string, 0, 8)
	for current := clean; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for index := len(chain) - 1; index >= 0; index-- {
		current := chain[index]
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("restore target component %q is a link or reparse point", current)
		}
		if index > 0 && !info.IsDir() {
			return fmt.Errorf("restore target parent %q is not a directory", current)
		}
	}
	return nil
}

// ConcreteFilesystemTarget returns a stable lexical identity using the actual
// names of existing filesystem entries and the host volume's case semantics
// for a missing suffix. Link/reparse aliases are rejected by validation.
func ConcreteFilesystemTarget(target string) (string, error) {
	if err := ValidateFilesystemTarget(target); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	abs, err = safepath.CanonicalizePlatformRootAlias(abs)
	if err != nil {
		return "", err
	}
	volume := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs, volume)
	rest = strings.TrimLeftFunc(rest, func(r rune) bool { return os.IsPathSeparator(uint8(r)) })
	root := string(filepath.Separator)
	if volume != "" {
		root = volume + string(filepath.Separator)
	}
	components := strings.FieldsFunc(rest, func(r rune) bool { return os.IsPathSeparator(uint8(r)) })
	current := filepath.Clean(root)
	caseInsensitive := runtime.GOOS == "windows"
	caseKnown := caseInsensitive
	for index, component := range components {
		candidate := filepath.Join(current, component)
		info, statErr := os.Lstat(candidate)
		if statErr == nil {
			actual, err := actualFilesystemEntryName(current, component, info)
			if err != nil {
				return "", err
			}
			current = filepath.Join(current, actual)
			continue
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
		if !caseKnown {
			caseInsensitive = filesystemCaseInsensitive(current)
			caseKnown = true
		}
		for _, missing := range components[index:] {
			if caseInsensitive {
				missing = strings.ToLower(missing)
			}
			current = filepath.Join(current, missing)
		}
		break
	}
	if runtime.GOOS == "windows" {
		current = strings.ToLower(current)
	}
	return filepath.Clean(current), nil
}

func actualFilesystemEntryName(parent, requested string, requestedInfo os.FileInfo) (string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Name() == requested {
			return requested, nil
		}
		info, err := entry.Info()
		if err != nil {
			// A sibling entry can vanish between ReadDir and Info() when the
			// parent is a shared, high-churn directory — e.g. the system temp
			// root while `go test ./...` runs package binaries concurrently, on
			// platforms where Info() lstats lazily (Unix) rather than returning
			// cached readdir data (Windows). Such an entry is not the component
			// being resolved, so skip it rather than failing target resolution
			// on an unrelated race.
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if os.SameFile(info, requestedInfo) {
			return entry.Name(), nil
		}
	}
	return "", fmt.Errorf("restore target component %q changed during validation", filepath.Join(parent, requested))
}

func filesystemCaseInsensitive(directory string) bool {
	for current := directory; ; current = filepath.Dir(current) {
		entries, err := os.ReadDir(current)
		if err == nil {
			for _, entry := range entries {
				alternate, ok := toggledCase(entry.Name())
				if !ok {
					continue
				}
				actualInfo, infoErr := entry.Info()
				alternateInfo, alternateErr := os.Lstat(filepath.Join(current, alternate))
				return infoErr == nil && alternateErr == nil && os.SameFile(actualInfo, alternateInfo)
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

func toggledCase(value string) (string, bool) {
	for index, r := range value {
		var replacement rune
		switch {
		case r >= 'a' && r <= 'z':
			replacement = r - ('a' - 'A')
		case r >= 'A' && r <= 'Z':
			replacement = r + ('a' - 'A')
		default:
			continue
		}
		return value[:index] + string(replacement) + value[index+1:], true
	}
	return value, false
}

func atomicRestoreWrite(target string, data []byte, mode os.FileMode) error {
	if err := ValidateFilesystemTarget(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := ValidateFilesystemTarget(target); err != nil {
		return err
	}
	if err := safepath.AtomicWriteFile(target, data, mode); err != nil {
		return err
	}
	return ValidateFilesystemTarget(target)
}

func atomicRestoreCopy(source, target string) error {
	if err := ValidateFilesystemTarget(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := ValidateFilesystemTarget(target); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if isLinkOrReparse(info) || !info.Mode().IsRegular() {
		return fmt.Errorf("restore source %q is not a safe regular file", source)
	}
	if err := safepath.AtomicCopyFile(source, target, info.Mode()); err != nil {
		return err
	}
	return ValidateFilesystemTarget(target)
}
