// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"fmt"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

// CaptureIsolationError carries safe declaration coordinates back to the
// command-scoped isolation recorder. Error intentionally omits physical paths,
// nonce namespaces, and registry data.
type CaptureIsolationError struct {
	ModuleID   string
	Coordinate string
	TargetKind string
	Authored   string
	Cause      error
}

func (err *CaptureIsolationError) Error() string {
	if err == nil {
		return "validation capture isolation violation"
	}
	return fmt.Sprintf("validation capture isolation violation: module=%s coordinate=%s", err.ModuleID, err.Coordinate)
}

func (err *CaptureIsolationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func captureIsolation(moduleID, coordinate, targetKind, authored string, cause error) error {
	return &CaptureIsolationError{ModuleID: moduleID, Coordinate: coordinate, TargetKind: targetKind, Authored: authored, Cause: cause}
}

func resolveCapturePortable(context *validationmode.Context, moduleID, coordinate, root, portable string) (string, error) {
	if context == nil {
		return containedHostPath(root, portable)
	}
	if err := context.ValidateSandboxPath(filepath.Clean(root)); err != nil {
		return "", captureIsolation(moduleID, coordinate, "portable", portable, err)
	}
	resolved, err := validationmode.ResolvePortablePath(root, portable)
	if err != nil {
		return "", captureIsolation(moduleID, coordinate, "portable", portable, err)
	}
	return resolved, nil
}

func resolveCaptureHost(context *validationmode.Context, moduleID, coordinate, authored string, policy validationmode.HostPathPolicy) (string, error) {
	if context == nil {
		return "", nil
	}
	resolved, err := context.ResolveHostPath(authored, policy)
	if err != nil {
		return "", captureIsolation(moduleID, coordinate, "path", authored, err)
	}
	return resolved, nil
}

func resolveCaptureSecretPatterns(context *validationmode.Context, moduleID string, patterns []string, policy validationmode.HostPathPolicy) ([]string, error) {
	if context == nil {
		return patterns, nil
	}
	resolved := make([]string, 0, len(patterns))
	for index, authored := range patterns {
		pattern, err := context.ResolveHostPattern(authored, policy)
		if err != nil {
			return nil, captureIsolation(moduleID, fmt.Sprintf("secrets.files[%d]", index), "path", authored, err)
		}
		resolved = append(resolved, pattern)
	}
	return resolved, nil
}
