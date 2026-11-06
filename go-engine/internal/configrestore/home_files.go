// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
	"os"
)

// HomeFile preserves both trusted placement intent and the resolved host target.
// Source stays inside the successful stage until the Home Manager consumer ends.
type HomeFile struct {
	Source           string
	Target           string
	TargetExpression string
}

// ResolveHomeFiles shares staged source and target validation without creating
// direct actions. Home Manager may replace a symlink leaf, never its referent.
func ResolveHomeFiles(ctx context.Context, request Request) ([]HomeFile, error) {
	target, generation, err := validateRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := PreflightAppClosure(ctx, generation.RequiresAppClosed, request.ProcessPatterns, request.ProcessObserver); err != nil {
		return nil, err
	}
	instance := targetConfigInstance(target)
	resolved := []resolvedRestore{}
	files := []HomeFile{}
	for index, definition := range generation.Restore {
		// This first bridge supports the schema's regular-file copy operation.
		if definition.Type != "copy" || len(definition.Exclude) != 0 {
			return nil, newError(CodeUnsupportedRestore, index, definition.Target, fmt.Errorf("Home Manager placement requires a regular-file copy"))
		}
		entry, err := resolveDefinition(request.Stage.Root, definition, instance, request.Boundary, index, true)
		if err != nil {
			return nil, err
		}
		if entry.sourceMissing {
			continue
		}
		if !entry.sourceInfo.Mode().IsRegular() {
			return nil, newError(CodeUnsupportedRestore, index, entry.target, fmt.Errorf("Home Manager placement requires a regular source file"))
		}
		if info, err := os.Lstat(entry.target); err == nil && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return nil, newError(CodeTargetStateUnsupported, index, entry.target, fmt.Errorf("Home Manager target is not a regular file or symlink leaf"))
		} else if err != nil && !os.IsNotExist(err) {
			return nil, newError(CodeMaterialization, index, entry.target, err)
		}
		resolved = append(resolved, entry)
		files = append(files, HomeFile{Source: entry.source, Target: entry.target, TargetExpression: definition.Target})
	}
	if err := rejectOverlappingTargets(resolved); err != nil {
		return nil, err
	}
	return files, nil
}
