// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
)

func commitRegistryAction(
	ctx context.Context,
	prepared Action,
	journal JournalAction,
	registry RegistryMutator,
	touch func(),
) error {
	if registry == nil || prepared.RegistryValue == nil {
		return fmt.Errorf("registry mutator and prepared desired value are required")
	}
	desired, err := desiredRegistrySnapshot(prepared.RegistryValue)
	if err != nil {
		return err
	}
	if desired.Key != journal.RegistryKey || desired.ValueName != journal.RegistryValueName ||
		digestRegistrySnapshot(desired) != journal.Desired.Digest {
		return fmt.Errorf("prepared registry value differs from journal desired state")
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if touch != nil {
		touch()
	}
	return registry.SetValue(
		ctx, desired.Key, desired.ValueName, desired.ValueType, append([]byte(nil), desired.Data...),
	)
}

func rollbackRegistryAction(ctx context.Context, action JournalAction, registry RegistryMutator) error {
	if registry == nil {
		return fmt.Errorf("registry mutator is required")
	}
	prior, err := loadRegistrySnapshot(action.Prior.BackupPath)
	if err != nil {
		return err
	}
	if prior.Key != action.RegistryKey || prior.ValueName != action.RegistryValueName ||
		digestRegistrySnapshot(prior) != action.Prior.Digest {
		return fmt.Errorf("registry prior backup differs from journal")
	}
	classification, err := classifyRegistryRollbackState(ctx, action, registry)
	if err != nil {
		return err
	}
	if classification == rollbackStatePrior {
		return nil
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if !prior.Exists {
		return registry.DeleteValue(ctx, prior.Key, prior.ValueName)
	}
	return registry.SetValue(
		ctx, prior.Key, prior.ValueName, prior.ValueType, append([]byte(nil), prior.Data...),
	)
}
