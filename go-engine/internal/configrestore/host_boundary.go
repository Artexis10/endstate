// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type hostBoundaryContextKey struct{}

func withHostBoundary(ctx context.Context, boundary HostBoundary) context.Context {
	if ctx == nil || boundary == nil {
		return ctx
	}
	return context.WithValue(ctx, hostBoundaryContextKey{}, boundary)
}

func hostBoundaryFromContext(ctx context.Context) HostBoundary {
	if ctx == nil {
		return nil
	}
	boundary, _ := ctx.Value(hostBoundaryContextKey{}).(HostBoundary)
	return boundary
}

func validateHostIO(ctx context.Context, absolute string) error {
	boundary := hostBoundaryFromContext(ctx)
	return validateBoundaryHostIO(boundary, absolute)
}

func validateBoundaryHostIO(boundary HostBoundary, absolute string) error {
	if boundary == nil {
		return nil
	}
	if err := boundary.ValidateFilesystemTarget(absolute); err != nil {
		return fmt.Errorf("validate host I/O boundary for %q: %w", absolute, err)
	}
	return nil
}

func resolveActionForHostIO(action Action, boundary HostBoundary, instance modules.ConfigInstance) (Action, error) {
	resolved := cloneAction(action)
	if boundary == nil {
		return resolved, nil
	}
	var err error
	if action.Kind != ActionRegistrySet {
		resolved.Target = action.resolvedTarget
		if resolved.Target == "" {
			resolved.Target, err = boundary.ResolveFilesystemIdentity(action.Target)
			if err != nil {
				return Action{}, fmt.Errorf("resolve target identity: %w", err)
			}
		}
		if err := boundary.ValidateFilesystemTarget(resolved.Target); err != nil {
			return Action{}, fmt.Errorf("validate target boundary: %w", err)
		}
	}
	if action.Kind == ActionCopy || action.Kind == ActionWriteFile {
		resolved.Source = action.resolvedSource
		if resolved.Source == "" {
			resolved.Source, err = boundary.ResolveFilesystemIdentity(action.Source)
			if err != nil {
				return Action{}, fmt.Errorf("resolve source identity: %w", err)
			}
		}
		if err := boundary.ValidateFilesystemTarget(resolved.Source); err != nil {
			return Action{}, fmt.Errorf("validate source boundary: %w", err)
		}
	}
	return resolved, nil
}

func resolveJournalActionForHostIO(action JournalAction, boundary HostBoundary) (JournalAction, error) {
	resolved := action
	resolved.MissingParents = append([]string(nil), action.MissingParents...)
	resolved.Prior.Entries = append([]JournalFilesystemEntry(nil), action.Prior.Entries...)
	resolved.Desired.Entries = append([]JournalFilesystemEntry(nil), action.Desired.Entries...)
	if boundary == nil {
		return resolved, nil
	}
	var err error
	if action.Kind != ActionRegistrySet {
		resolved.Target, err = boundary.ResolveFilesystemIdentity(action.Target)
		if err != nil {
			return JournalAction{}, fmt.Errorf("resolve journal target: %w", err)
		}
		if err := boundary.ValidateFilesystemTarget(resolved.Target); err != nil {
			return JournalAction{}, fmt.Errorf("validate journal target: %w", err)
		}
	}
	if action.Prior.BackupPath != "" {
		resolved.Prior.BackupPath, err = boundary.ResolveFilesystemIdentity(action.Prior.BackupPath)
		if err != nil {
			return JournalAction{}, fmt.Errorf("resolve prior backup: %w", err)
		}
		if err := boundary.ValidateFilesystemTarget(resolved.Prior.BackupPath); err != nil {
			return JournalAction{}, fmt.Errorf("validate prior backup: %w", err)
		}
	}
	for index, parent := range action.MissingParents {
		resolved.MissingParents[index], err = boundary.ResolveFilesystemIdentity(parent)
		if err != nil {
			return JournalAction{}, fmt.Errorf("resolve missing parent[%d]: %w", index, err)
		}
		if err := boundary.ValidateFilesystemTarget(resolved.MissingParents[index]); err != nil {
			return JournalAction{}, fmt.Errorf("validate missing parent[%d]: %w", index, err)
		}
	}
	return resolved, nil
}

func resolveJournalActionsForHostIO(actions []JournalAction, boundary HostBoundary) ([]JournalAction, error) {
	resolved := make([]JournalAction, len(actions))
	for index, action := range actions {
		var err error
		resolved[index], err = resolveJournalActionForHostIO(action, boundary)
		if err != nil {
			return nil, fmt.Errorf("action[%d]: %w", index, err)
		}
	}
	return resolved, nil
}

func projectFilesystemIdentity(boundary HostBoundary, absolute string) (string, error) {
	if boundary == nil || absolute == "" {
		return absolute, nil
	}
	if err := boundary.ValidateFilesystemTarget(absolute); err != nil {
		return "", err
	}
	return boundary.ProjectFilesystemIdentity(absolute)
}
