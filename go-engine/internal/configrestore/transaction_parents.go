// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func findMissingTransactionParents(target string) ([]string, error) {
	if err := rejectExistingTargetLinks(target); err != nil {
		return nil, err
	}
	var nearestFirst []string
	for current := filepath.Dir(target); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || isLinkOrReparse(info) {
				return nil, fmt.Errorf("transaction target parent %q is not a safe directory", current)
			}
			break
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		nearestFirst = append(nearestFirst, current)
		parent := filepath.Dir(current)
		if parent == current {
			return nil, fmt.Errorf("transaction target has no existing directory ancestor")
		}
	}
	parents := make([]string, len(nearestFirst))
	for index := range nearestFirst {
		parents[len(nearestFirst)-1-index] = nearestFirst[index]
	}
	return parents, nil
}

func assignMissingParentOwnership(prepared []preparedInternal) {
	claimed := make(map[string]struct{})
	for index := range prepared {
		owned := make([]string, 0, len(prepared[index].record.missingParents))
		for _, parent := range prepared[index].record.missingParents {
			canonical := canonicalFilesystemTarget(parent)
			if _, exists := claimed[canonical]; exists {
				continue
			}
			claimed[canonical] = struct{}{}
			owned = append(owned, parent)
		}
		prepared[index].record.missingParents = owned
	}
}

func verifyMissingTransactionParents(parents []string) error {
	for _, parent := range parents {
		if err := rejectExistingTargetLinks(parent); err != nil {
			return err
		}
		if _, err := os.Lstat(parent); !os.IsNotExist(err) {
			if err == nil {
				return fmt.Errorf("recorded missing parent %q now exists", parent)
			}
			return err
		}
	}
	return nil
}

func createMissingTransactionParents(
	ctx context.Context,
	parents []string,
	touch func(),
) error {
	if err := verifyMissingTransactionParents(parents); err != nil {
		return err
	}
	for _, parent := range parents {
		if err := checkSnapshotContext(ctx); err != nil {
			return err
		}
		if err := rejectExistingTargetLinks(parent); err != nil {
			return err
		}
		container := filepath.Dir(parent)
		info, err := os.Lstat(container)
		if err != nil || !info.IsDir() || isLinkOrReparse(info) {
			return fmt.Errorf("recorded parent container %q is not safe", container)
		}
		if touch != nil {
			touch()
		}
		if err := os.Mkdir(parent, 0o755); err != nil {
			return err
		}
		if err := os.Chmod(parent, 0o755); err != nil {
			return err
		}
		if err := syncDurableDirectory(parent); err != nil {
			return err
		}
		if err := syncDurableDirectory(container); err != nil {
			return err
		}
	}
	return nil
}

func removeRecordedTransactionParents(ctx context.Context, parents []string) error {
	for index := len(parents) - 1; index >= 0; index-- {
		parent := parents[index]
		if err := checkSnapshotContext(ctx); err != nil {
			return err
		}
		if err := rejectExistingTargetLinks(parent); err != nil {
			return err
		}
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() || isLinkOrReparse(info) {
			return fmt.Errorf("recorded transaction parent %q has unsafe type", parent)
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("recorded transaction parent %q contains unrelated entries", parent)
		}
		if err := os.Remove(parent); err != nil {
			return err
		}
		if err := syncDurableDirectory(filepath.Dir(parent)); err != nil {
			return err
		}
	}
	return nil
}
