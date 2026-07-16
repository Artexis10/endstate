// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// PreflightAppClosure observes process basenames and rejects a declared match.
// It never starts, stops, signals, or otherwise mutates a process.
func PreflightAppClosure(
	ctx context.Context,
	required bool,
	patterns []string,
	observer ProcessObserver,
) error {
	if !required {
		return nil
	}
	if ctx == nil {
		return newError(CodeInvalidRequest, -1, "", fmt.Errorf("context is nil"))
	}
	if len(patterns) == 0 {
		return newError(CodeAppClosureConfig, -1, "", fmt.Errorf("application closure requires a trusted process basename or glob"))
	}
	if observer == nil {
		return newError(CodeAppClosureConfig, -1, "", fmt.Errorf("application closure requires a process observer"))
	}

	normalized := make([]string, len(patterns))
	for index, pattern := range patterns {
		if err := validateProcessPattern(pattern); err != nil {
			return newError(CodeAppClosureConfig, -1, "", fmt.Errorf("process pattern[%d]: %w", index, err))
		}
		normalized[index] = strings.ToLower(pattern)
	}

	names, err := observer.RunningProcessBasenames(ctx)
	if err != nil {
		return newError(CodeProcessObservation, -1, "", err)
	}
	for _, observed := range names {
		basename := portableBasename(observed)
		if basename == "" || basename == "." {
			continue
		}
		folded := strings.ToLower(basename)
		for index, pattern := range normalized {
			matched, matchErr := path.Match(pattern, folded)
			if matchErr != nil {
				return newError(CodeAppClosureConfig, -1, "", matchErr)
			}
			if matched {
				return &Error{
					Code: CodeAppRunning, ActionIndex: -1, ValidationIndex: -1, MappingCount: -1,
					ProcessBasename: basename, ProcessPattern: patterns[index],
					Err: fmt.Errorf("declared application process is active"),
				}
			}
		}
	}
	return nil
}

func validateProcessPattern(pattern string) error {
	if pattern == "" || pattern != strings.TrimSpace(pattern) || strings.ContainsAny(pattern, `/\:`) || strings.ContainsRune(pattern, '\x00') {
		return fmt.Errorf("pattern %q must be a basename glob without path syntax", pattern)
	}
	for _, character := range pattern {
		if character < 0x20 {
			return fmt.Errorf("pattern %q contains a control character", pattern)
		}
	}
	if _, err := path.Match(strings.ToLower(pattern), "process.exe"); err != nil {
		return fmt.Errorf("invalid basename glob %q: %w", pattern, err)
	}
	return nil
}

func portableBasename(value string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	return path.Base(normalized)
}
