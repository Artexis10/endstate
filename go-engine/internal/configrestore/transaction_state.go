// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
	"os"
)

func verifyAllTransactionStates(
	ctx context.Context,
	actions []JournalAction,
	registry RegistryMutator,
	desired bool,
) error {
	for index, action := range actions {
		if err := verifyTransactionActionState(ctx, action, registry, desired); err != nil {
			return fmt.Errorf("action[%d] target %q: %w", index, action.Target, err)
		}
	}
	return nil
}

func verifyTransactionActionState(
	ctx context.Context,
	action JournalAction,
	registry RegistryMutator,
	desired bool,
) error {
	if action.Kind != ActionRegistrySet {
		if err := verifyTransactionParentState(action.MissingParents, desired); err != nil {
			return err
		}
	}
	want := action.Prior
	if desired {
		want = action.Desired
	}
	if action.Kind == ActionRegistrySet {
		if registry == nil {
			return fmt.Errorf("registry mutator is required")
		}
		actual, err := readTransactionRegistrySnapshot(ctx, registry, action.RegistryKey, action.RegistryValueName)
		if err != nil {
			return err
		}
		actualKind := StateAbsent
		if actual.Exists {
			actualKind = StateRegistryValue
		}
		if actualKind != want.Kind || digestRegistrySnapshot(actual) != want.Digest {
			return fmt.Errorf("registry state differs from recorded %s state", statePosition(desired))
		}
		return nil
	}
	actual, err := scanFilesystemState(ctx, action.Target)
	if err != nil {
		return err
	}
	expected, err := journalFilesystemState(want)
	if err != nil {
		return err
	}
	if !statesEqual(actual, expected) {
		return fmt.Errorf("filesystem state differs from recorded %s state", statePosition(desired))
	}
	return nil
}

func verifyTransactionParentState(parents []string, desired bool) error {
	if !desired {
		return verifyMissingTransactionParents(parents)
	}
	for _, parent := range parents {
		if err := rejectExistingTargetLinks(parent); err != nil {
			return err
		}
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || isLinkOrReparse(info) {
			return fmt.Errorf("recorded transaction parent %q is not a safe directory", parent)
		}
	}
	return nil
}

func journalFilesystemState(state JournalActionState) (filesystemState, error) {
	record := StateRecord{
		Kind: state.Kind, Digest: state.Digest, Mode: os.FileMode(state.Mode), BackupPath: state.BackupPath,
		entries: stateEntriesFromJournal(state.Entries),
	}
	result, err := filesystemStateFromRecord(record)
	if err != nil {
		return filesystemState{}, err
	}
	if result.Digest != state.Digest {
		return filesystemState{}, fmt.Errorf("journal filesystem state digest mismatch")
	}
	return result, nil
}

func statePosition(desired bool) string {
	if desired {
		return "desired"
	}
	return "prior"
}

func readTransactionRegistrySnapshot(
	ctx context.Context,
	reader RegistryReader,
	key string,
	valueName string,
) (registrySnapshot, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return registrySnapshot{}, err
	}
	result, err := reader.ReadValue(ctx, key, valueName)
	if err != nil {
		return registrySnapshot{}, err
	}
	if !result.Exists && (result.ValueType != 0 || len(result.Data) != 0) {
		return registrySnapshot{}, fmt.Errorf("absent registry value returned type or data")
	}
	return registrySnapshot{
		Key: key, ValueName: valueName, Exists: result.Exists,
		ValueType: result.ValueType, Data: append([]byte(nil), result.Data...),
	}, nil
}
