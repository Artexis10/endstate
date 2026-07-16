// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

func executeTransactionAction(
	ctx context.Context,
	prepared PreparedAction,
	journal JournalAction,
	registry RegistryMutator,
	touch func(),
	afterMutation func() error,
) error {
	action := prepared.Action()
	if action.Kind != ActionRegistrySet {
		if err := createMissingTransactionParents(ctx, journal.MissingParents, touch, afterMutation); err != nil {
			return err
		}
	}
	switch action.Kind {
	case ActionCopy:
		return commitCopyAction(ctx, action, journal, touch, afterMutation)
	case ActionWriteFile:
		desired, err := journalFilesystemState(journal.Desired)
		if err != nil {
			return err
		}
		prior, err := journalFilesystemState(journal.Prior)
		if err != nil {
			return err
		}
		computed := desiredWriteState(prior, action.DesiredContent)
		if !statesEqual(computed, desired) {
			return fmt.Errorf("prepared write content differs from journal desired state")
		}
		return atomicWriteTransactionFile(ctx, action.Target, action.DesiredContent, desired.Mode, touch, afterMutation)
	case ActionDeleteFile:
		return removeTransactionFile(ctx, action.Target, touch, afterMutation)
	case ActionRegistrySet:
		return commitRegistryAction(ctx, action, journal, registry, touch, afterMutation)
	default:
		return fmt.Errorf("unsupported transaction action %q", action.Kind)
	}
}

func commitCopyAction(
	ctx context.Context,
	action Action,
	journal JournalAction,
	touch func(),
	afterMutation func() error,
) error {
	source, err := scanFilesystemState(ctx, action.Source)
	if err != nil {
		return err
	}
	if source.Digest != journal.SourceDigest {
		return fmt.Errorf("copy source changed after journal intent")
	}
	desired, err := journalFilesystemState(journal.Desired)
	if err != nil {
		return err
	}
	switch source.Kind {
	case StateFile:
		data, _, err := safepath.ReadRegularFile(action.Source)
		if err != nil {
			return err
		}
		if err := verifyFilesystemEntryData(source.Entries["."], data); err != nil {
			return err
		}
		if err := prepareTransactionFileDestination(ctx, action.Target, touch, afterMutation); err != nil {
			return err
		}
		if err := atomicWriteTransactionFile(ctx, action.Target, data, desired.Mode, touch, afterMutation); err != nil {
			return err
		}
	case StateDirectory:
		if err := commitDirectoryCopy(ctx, action, source, desired, touch, afterMutation); err != nil {
			return err
		}
	default:
		return fmt.Errorf("copy source has unsupported state %q", source.Kind)
	}
	currentSource, err := scanFilesystemState(ctx, action.Source)
	if err != nil {
		return err
	}
	if currentSource.Digest != journal.SourceDigest {
		return fmt.Errorf("copy source changed during transaction action")
	}
	return nil
}

func commitDirectoryCopy(
	ctx context.Context,
	action Action,
	source filesystemState,
	desired filesystemState,
	touch func(),
	afterMutation func() error,
) error {
	rootEntry, exists := desired.Entries["."]
	if !exists || rootEntry.Kind != StateDirectory {
		return fmt.Errorf("directory copy desired state has no root directory")
	}
	if err := ensureTransactionDirectory(ctx, action.Target, rootEntry.Mode, touch, afterMutation); err != nil {
		return err
	}
	paths := sortedFilesystemPaths(source.Entries)
	excludedDirectories := make([]string, 0)
	for _, relative := range paths {
		if relative == "." || hasExcludedAncestor(relative, excludedDirectories) {
			continue
		}
		sourceEntry := source.Entries[relative]
		if copyPathExcluded(relative, action.Exclude) {
			if sourceEntry.Kind == StateDirectory {
				excludedDirectories = append(excludedDirectories, relative)
			}
			continue
		}
		desiredEntry, exists := desired.Entries[relative]
		if !exists || desiredEntry.Kind != sourceEntry.Kind {
			return fmt.Errorf("copy path %q differs from journal desired manifest", relative)
		}
		sourcePath := filepath.Join(action.Source, filepath.FromSlash(relative))
		targetPath := filepath.Join(action.Target, filepath.FromSlash(relative))
		switch sourceEntry.Kind {
		case StateDirectory:
			if err := ensureTransactionDirectory(ctx, targetPath, desiredEntry.Mode, touch, afterMutation); err != nil {
				return err
			}
		case StateFile:
			data, _, err := safepath.ReadRegularFile(sourcePath)
			if err != nil {
				return err
			}
			if err := verifyFilesystemEntryData(sourceEntry, data); err != nil {
				return fmt.Errorf("copy source %q: %w", relative, err)
			}
			if err := prepareTransactionFileDestination(ctx, targetPath, touch, afterMutation); err != nil {
				return err
			}
			if err := atomicWriteTransactionFile(ctx, targetPath, data, desiredEntry.Mode, touch, afterMutation); err != nil {
				return err
			}
		default:
			return fmt.Errorf("copy source path %q has unsupported kind %q", relative, sourceEntry.Kind)
		}
	}
	return nil
}

func prepareTransactionFileDestination(
	ctx context.Context,
	target string,
	touch func(),
	afterMutation func() error,
) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := rejectExistingTargetLinks(target); err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() && !isLinkOrReparse(info) {
		return nil
	}
	if !info.IsDir() || isLinkOrReparse(info) {
		return fmt.Errorf("transaction file target has unsupported type")
	}
	if touch != nil {
		touch()
	}
	if err := removeSafeTransactionPath(ctx, target); err != nil {
		return err
	}
	return runAfterTransactionMutation(afterMutation)
}

func verifyFilesystemEntryData(entry filesystemEntry, data []byte) error {
	sum := sha256.Sum256(data)
	if entry.Size != int64(len(data)) || entry.ContentHash != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("file bytes differ from verified source manifest")
	}
	return nil
}

func atomicWriteTransactionFile(
	ctx context.Context,
	destination string,
	data []byte,
	mode os.FileMode,
	touch func(),
	afterMutation func() error,
) (resultErr error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	parent := filepath.Dir(destination)
	if err := rejectExistingTargetLinks(parent); err != nil {
		return err
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || isLinkOrReparse(parentInfo) {
		return fmt.Errorf("transaction target parent is not a safe existing directory")
	}
	temporary, err := os.CreateTemp(parent, ".endstate-transaction-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := rejectExistingTargetLinks(destination); err != nil {
		return err
	}
	if touch != nil {
		touch()
	}
	if err := replaceTransactionFile(temporaryPath, destination); err != nil {
		return err
	}
	return runAfterTransactionMutation(afterMutation)
}

func ensureTransactionDirectory(
	ctx context.Context,
	path string,
	mode os.FileMode,
	touch func(),
	afterMutation func() error,
) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := rejectExistingTargetLinks(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		parent := filepath.Dir(path)
		if parentInfo, parentErr := os.Lstat(parent); parentErr != nil || !parentInfo.IsDir() || isLinkOrReparse(parentInfo) {
			return fmt.Errorf("transaction directory parent is not safe")
		}
		if touch != nil {
			touch()
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(path, mode.Perm()); err != nil {
			return err
		}
		if err := syncDurableDirectory(path); err != nil {
			return err
		}
		if err := syncDurableDirectory(parent); err != nil {
			return err
		}
		return runAfterTransactionMutation(afterMutation)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || isLinkOrReparse(info) {
		if !info.Mode().IsRegular() || isLinkOrReparse(info) {
			return fmt.Errorf("transaction directory path has an incompatible type")
		}
		if touch != nil {
			touch()
		}
		if err := removeSafeTransactionPath(ctx, path); err != nil {
			return err
		}
		if err := runAfterTransactionMutation(afterMutation); err != nil {
			return err
		}
		return ensureTransactionDirectory(ctx, path, mode, touch, afterMutation)
	}
	if info.Mode().Perm() != mode.Perm() {
		if err := rejectExistingTargetLinks(path); err != nil {
			return err
		}
		if touch != nil {
			touch()
		}
		if err := os.Chmod(path, mode.Perm()); err != nil {
			return err
		}
		if err := syncDurableDirectory(path); err != nil {
			return err
		}
		return runAfterTransactionMutation(afterMutation)
	}
	return nil
}

func removeTransactionFile(ctx context.Context, target string, touch func(), afterMutation func() error) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := rejectExistingTargetLinks(target); err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || isLinkOrReparse(info) {
		return fmt.Errorf("delete target is not a safe regular file")
	}
	if touch != nil {
		touch()
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	if err := syncDurableDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	return runAfterTransactionMutation(afterMutation)
}

func runAfterTransactionMutation(afterMutation func() error) error {
	if afterMutation == nil {
		return nil
	}
	return afterMutation()
}

func rollbackTransactionAction(ctx context.Context, action JournalAction, registry RegistryMutator) error {
	if action.Kind == ActionRegistrySet {
		return rollbackRegistryAction(ctx, action, registry)
	}
	classification, current, err := classifyFilesystemRollbackState(ctx, action)
	if err != nil {
		return err
	}
	prior, err := journalFilesystemState(action.Prior)
	if err != nil {
		return err
	}
	if classification != rollbackStatePrior {
		if err := restoreFilesystemPrior(ctx, action.Target, action.Prior.BackupPath, prior, current); err != nil {
			return err
		}
	}
	return removeRecordedTransactionParents(ctx, action.MissingParents)
}

func restoreFilesystemPrior(
	ctx context.Context,
	target string,
	backupPath string,
	prior filesystemState,
	current filesystemState,
) error {
	switch prior.Kind {
	case StateAbsent:
		return removeSafeTransactionPath(ctx, target)
	case StateFile:
		backup, err := scanFilesystemState(ctx, backupPath)
		if err != nil {
			return fmt.Errorf("read prior file backup: %w", err)
		}
		if !statesEqual(backup, prior) {
			return fmt.Errorf("prior file backup is not exact")
		}
		if current.Kind == StateDirectory {
			if err := removeSafeTransactionPath(ctx, target); err != nil {
				return err
			}
		}
		data, _, err := safepath.ReadRegularFile(backupPath)
		if err != nil {
			return err
		}
		if err := verifyFilesystemEntryData(prior.Entries["."], data); err != nil {
			return err
		}
		return atomicWriteTransactionFile(ctx, target, data, prior.Mode, nil, nil)
	case StateDirectory:
		backup, err := scanFilesystemState(ctx, backupPath)
		if err != nil {
			return fmt.Errorf("read prior directory backup: %w", err)
		}
		if !statesEqual(backup, prior) {
			return fmt.Errorf("prior directory backup is not exact")
		}
		if err := removeDesiredOnlyFilesystemEntries(ctx, target, current, prior); err != nil {
			return err
		}
		return restoreRecordedDirectory(ctx, backupPath, target, prior)
	default:
		return fmt.Errorf("unsupported prior filesystem state %q", prior.Kind)
	}
}

func removeDesiredOnlyFilesystemEntries(
	ctx context.Context,
	target string,
	current filesystemState,
	prior filesystemState,
) error {
	if current.Kind != StateDirectory {
		if current.Kind != StateAbsent {
			if err := removeSafeTransactionPath(ctx, target); err != nil {
				return err
			}
		}
		return nil
	}
	for _, relative := range sortedFilesystemPaths(current.Entries) {
		if relative == "." {
			continue
		}
		if _, retained := prior.Entries[relative]; retained {
			continue
		}
		path := filepath.Join(target, filepath.FromSlash(relative))
		if err := removeSafeTransactionPath(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func restoreRecordedDirectory(
	ctx context.Context,
	backupRoot string,
	targetRoot string,
	prior filesystemState,
) error {
	paths := sortedFilesystemPaths(prior.Entries)
	for _, relative := range paths {
		entry := prior.Entries[relative]
		target := targetRoot
		backup := backupRoot
		if relative != "." {
			target = filepath.Join(targetRoot, filepath.FromSlash(relative))
			backup = filepath.Join(backupRoot, filepath.FromSlash(relative))
		}
		switch entry.Kind {
		case StateDirectory:
			if err := ensureTransactionDirectory(ctx, target, entry.Mode, nil, nil); err != nil {
				return err
			}
		case StateFile:
			data, _, err := safepath.ReadRegularFile(backup)
			if err != nil {
				return err
			}
			if err := verifyFilesystemEntryData(entry, data); err != nil {
				return err
			}
			if err := prepareTransactionFileDestination(ctx, target, nil, nil); err != nil {
				return err
			}
			if err := atomicWriteTransactionFile(ctx, target, data, entry.Mode, nil, nil); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported prior entry kind %q", entry.Kind)
		}
	}
	return nil
}

func removeSafeTransactionPath(ctx context.Context, target string) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := rejectExistingTargetLinks(target); err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if isLinkOrReparse(info) {
		return fmt.Errorf("rollback target is a link or reparse point")
	}
	if info.IsDir() {
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := removeSafeTransactionPath(ctx, filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("rollback target has unsupported special type")
	}
	if err := rejectExistingTargetLinks(target); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	return syncDurableDirectory(filepath.Dir(target))
}
