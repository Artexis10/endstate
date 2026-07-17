// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
	"path"
	"sort"
)

type rollbackStateClass uint8

const (
	rollbackStatePrior rollbackStateClass = iota
	rollbackStateDesired
	rollbackStateProvablePartial
)

func classifyFilesystemRollbackState(
	ctx context.Context,
	action JournalAction,
) (rollbackStateClass, filesystemState, error) {
	prior, err := journalFilesystemState(action.Prior)
	if err != nil {
		return 0, filesystemState{}, err
	}
	desired, err := journalFilesystemState(action.Desired)
	if err != nil {
		return 0, filesystemState{}, err
	}
	actual, err := scanFilesystemState(ctx, action.Target)
	if err != nil {
		return 0, filesystemState{}, err
	}
	if statesEqual(actual, prior) {
		return rollbackStatePrior, actual, nil
	}
	if statesEqual(actual, desired) {
		return rollbackStateDesired, actual, nil
	}
	if isJournalProvableFilesystemPartial(actual, prior, desired) {
		return rollbackStateProvablePartial, actual, nil
	}
	return 0, filesystemState{}, fmt.Errorf(
		"rollback target differs from recorded prior, desired, and journal-provable partial state",
	)
}

func isJournalProvableFilesystemPartial(actual, prior, desired filesystemState) bool {
	entries := cloneFilesystemEntries(prior.Entries)
	matches := func() bool {
		return statesEqual(actual, filesystemStateFromTransactionEntries(entries))
	}
	remove := func(relative string) bool {
		for _, removed := range transactionRemovalOrder(entries, relative) {
			delete(entries, removed)
			if matches() {
				return true
			}
		}
		return false
	}
	ensureDirectory := func(relative string, entry filesystemEntry) bool {
		if existing, exists := entries[relative]; exists {
			switch existing.Kind {
			case StateFile:
				if remove(relative) {
					return true
				}
			case StateDirectory:
				if filesystemEntriesEqual(existing, entry) {
					return false
				}
				// An existing directory is changed directly to its desired mode;
				// the 0700 creation state below is never produced on this path.
				entries[relative] = entry
				return matches()
			}
		}
		created := entry
		created.Mode = 0o700
		entries[relative] = created
		if matches() {
			return true
		}
		entries[relative] = entry
		return matches()
	}

	switch desired.Kind {
	case StateAbsent:
		// Delete-file removes one regular-file target in a single target mutation.
		return false
	case StateFile:
		if prior.Kind == StateDirectory && remove(".") {
			return true
		}
		entries = cloneFilesystemEntries(desired.Entries)
		return matches()
	case StateDirectory:
		desiredRoot := desired.Entries["."]
		if prior.Kind == StateFile && remove(".") {
			return true
		}
		if prior.Kind != StateDirectory && ensureDirectory(".", desiredRoot) {
			return true
		}
	default:
		return false
	}

	for _, relative := range sortedFilesystemPaths(desired.Entries) {
		if relative == "." {
			continue
		}
		desiredEntry := desired.Entries[relative]
		currentEntry, exists := entries[relative]
		if exists && filesystemEntriesEqual(currentEntry, desiredEntry) {
			continue
		}
		switch desiredEntry.Kind {
		case StateDirectory:
			if ensureDirectory(relative, desiredEntry) {
				return true
			}
		case StateFile:
			if exists && currentEntry.Kind == StateDirectory && remove(relative) {
				return true
			}
			entries[relative] = desiredEntry
			if matches() {
				return true
			}
		default:
			return false
		}
	}
	return false
}

func filesystemEntriesEqual(left, right filesystemEntry) bool {
	return left.Path == right.Path && left.Kind == right.Kind && left.Mode.Perm() == right.Mode.Perm() &&
		left.Size == right.Size && left.ContentHash == right.ContentHash
}

func filesystemStateFromTransactionEntries(entries map[string]filesystemEntry) filesystemState {
	root, exists := entries["."]
	if !exists {
		return absentFilesystemState()
	}
	state := filesystemState{
		Kind: root.Kind, Mode: root.Mode.Perm(), Entries: cloneFilesystemEntries(entries),
	}
	state.Digest = digestFilesystemState(state)
	return state
}

func transactionRemovalOrder(entries map[string]filesystemEntry, root string) []string {
	children := make(map[string][]string)
	for relative := range entries {
		if relative == root || !portablePathWithin(root, relative) {
			continue
		}
		parent := path.Dir(relative)
		children[parent] = append(children[parent], relative)
	}
	for parent := range children {
		sort.Strings(children[parent])
	}
	var order []string
	var visit func(string)
	visit = func(relative string) {
		for _, child := range children[relative] {
			visit(child)
		}
		order = append(order, relative)
	}
	visit(root)
	return order
}

func portablePathWithin(root, candidate string) bool {
	if root == "." {
		return candidate != "."
	}
	return candidate != root && len(candidate) > len(root) && candidate[:len(root)+1] == root+"/"
}

func classifyRegistryRollbackState(
	ctx context.Context,
	action JournalAction,
	registry RegistryMutator,
) (rollbackStateClass, error) {
	if registry == nil {
		return 0, fmt.Errorf("registry mutator is required")
	}
	actual, err := readTransactionRegistrySnapshot(ctx, registry, action.RegistryKey, action.RegistryValueName)
	if err != nil {
		return 0, err
	}
	digest := digestRegistrySnapshot(actual)
	if digest == action.Prior.Digest && actual.Exists == (action.Prior.Kind == StateRegistryValue) {
		return rollbackStatePrior, nil
	}
	if digest == action.Desired.Digest && actual.Exists == (action.Desired.Kind == StateRegistryValue) {
		return rollbackStateDesired, nil
	}
	return 0, fmt.Errorf("rollback registry value differs from recorded prior and desired state")
}
