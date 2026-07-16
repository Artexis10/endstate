// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
)

type preparedInternal struct {
	record              PreparedAction
	priorFS             filesystemState
	priorRegistry       registrySnapshot
	temporaryBackupPath string
}

// PrepareSnapshots uses a default preparer. Tests that need deterministic
// failure checkpoints use SnapshotPreparer.Prepare directly.
func PrepareSnapshots(ctx context.Context, request SnapshotRequest) (*PreparedSet, error) {
	return NewSnapshotPreparer().Prepare(ctx, request)
}

// Prepare snapshots and verifies every prior/desired state before publishing
// one immutable PreparedSet. It never mutates a restore target.
func (p *SnapshotPreparer) Prepare(ctx context.Context, request SnapshotRequest) (result *PreparedSet, resultErr error) {
	set, transactionRoot, err := validateSnapshotRequest(ctx, request)
	if err != nil {
		return nil, backupError(-1, request.TransactionRoot, err)
	}
	finalRoot := filepath.Join(transactionRoot, "snapshots")
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		if err == nil {
			err = fmt.Errorf("snapshot publication path already exists")
		}
		return nil, backupError(-1, finalRoot, err)
	}
	if len(set.Actions) == 0 {
		return &PreparedSet{
			actions: []PreparedAction{}, validations: append([]configvalidate.ResolvedValidation(nil), set.Validations...),
		}, nil
	}

	temporaryRoot, err := os.MkdirTemp(transactionRoot, ".snapshots-preparing-")
	if err != nil {
		return nil, backupError(-1, transactionRoot, err)
	}
	published := false
	defer func() {
		if published {
			return
		}
		if cleanupErr := os.RemoveAll(temporaryRoot); cleanupErr != nil {
			if resultErr == nil {
				resultErr = backupError(-1, temporaryRoot, cleanupErr)
			} else {
				resultErr = errors.Join(resultErr, cleanupErr)
			}
			result = nil
		}
	}()
	if err := os.Chmod(temporaryRoot, 0o700); err != nil {
		return nil, backupError(-1, temporaryRoot, err)
	}
	if err := rejectExistingTargetLinks(temporaryRoot); err != nil {
		return nil, backupError(-1, temporaryRoot, err)
	}

	prepared := make([]preparedInternal, 0, len(set.Actions))
	for index, original := range set.Actions {
		action := cloneAction(original)
		if err := p.runCheckpoint(ctx, phaseBeforeAction, index); err != nil {
			return nil, backupError(index, action.Target, err)
		}
		actionDirectory := filepath.Join(temporaryRoot, formatActionIndex(index))
		if err := os.Mkdir(actionDirectory, 0o700); err != nil {
			return nil, backupError(index, action.Target, err)
		}
		internal, err := prepareSnapshotAction(ctx, request.RegistryReader, action, index, actionDirectory, finalRoot)
		if err != nil {
			return nil, backupError(index, action.Target, err)
		}
		prepared = append(prepared, internal)
		if err := p.runCheckpoint(ctx, phaseAfterAction, index); err != nil {
			return nil, backupError(index, action.Target, err)
		}
	}
	assignMissingParentOwnership(prepared)
	if err := p.runCheckpoint(ctx, phaseBeforeFinalVerify, -1); err != nil {
		return nil, backupError(-1, finalRoot, err)
	}
	for index := range prepared {
		if err := verifyPreparedAction(ctx, request.RegistryReader, prepared[index]); err != nil {
			return nil, backupError(index, prepared[index].record.action.Target, err)
		}
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, backupError(-1, finalRoot, err)
	}
	if err := rejectExistingTargetLinks(transactionRoot); err != nil {
		return nil, backupError(-1, transactionRoot, err)
	}
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		if err == nil {
			err = fmt.Errorf("snapshot publication path appeared during preparation")
		}
		return nil, backupError(-1, finalRoot, err)
	}
	if err := os.Rename(temporaryRoot, finalRoot); err != nil {
		return nil, backupError(-1, finalRoot, err)
	}
	published = true

	records := make([]PreparedAction, len(prepared))
	for index := range prepared {
		records[index] = clonePreparedAction(prepared[index].record)
	}
	return &PreparedSet{
		snapshotRoot: finalRoot,
		actions:      records,
		validations:  append([]configvalidate.ResolvedValidation(nil), set.Validations...),
	}, nil
}

func validateSnapshotRequest(ctx context.Context, request SnapshotRequest) (*MaterializedSet, string, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, "", err
	}
	if request.Set == nil || request.Set.Actions == nil {
		return nil, "", fmt.Errorf("materialized set with a non-nil action list is required")
	}
	root := request.TransactionRoot
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, "", fmt.Errorf("transaction root must be a clean absolute path")
	}
	if err := rejectExistingTargetLinks(root); err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return nil, "", fmt.Errorf("transaction root must be an existing safe directory")
	}
	copy := &MaterializedSet{
		Actions:     make([]Action, len(request.Set.Actions)),
		Validations: append([]configvalidate.ResolvedValidation(nil), request.Set.Validations...),
	}
	for index, action := range request.Set.Actions {
		if !action.SnapshotRequired {
			return nil, "", fmt.Errorf("action[%d] is not marked snapshot-required", index)
		}
		copy.Actions[index] = cloneAction(action)
		if action.Kind != ActionRegistrySet {
			if err := validateSnapshotPathSeparation(root, action.Target); err != nil {
				return nil, "", fmt.Errorf("action[%d] target: %w", index, err)
			}
			if action.Kind == ActionCopy {
				if err := validateSnapshotPathSeparation(root, action.Source); err != nil {
					return nil, "", fmt.Errorf("action[%d] source: %w", index, err)
				}
			}
		}
	}
	return copy, root, nil
}

func validateSnapshotPathSeparation(transactionRoot, path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("filesystem action path must be clean and absolute")
	}
	if filesystemTargetsOverlap(canonicalFilesystemTarget(transactionRoot), canonicalFilesystemTarget(path)) {
		return fmt.Errorf("filesystem action path overlaps transaction root")
	}
	return nil
}

func prepareSnapshotAction(
	ctx context.Context,
	registryReader RegistryReader,
	action Action,
	index int,
	actionDirectory string,
	finalRoot string,
) (preparedInternal, error) {
	finalDirectory := filepath.Join(finalRoot, formatActionIndex(index))
	record := PreparedAction{action: cloneAction(action)}
	result := preparedInternal{record: record}
	switch action.Kind {
	case ActionCopy, ActionWriteFile, ActionDeleteFile:
		missingParents, err := findMissingTransactionParents(action.Target)
		if err != nil {
			return result, err
		}
		result.record.missingParents = missingParents
		prior, err := scanFilesystemState(ctx, action.Target)
		if err != nil {
			return result, err
		}
		if (action.Kind == ActionWriteFile || action.Kind == ActionDeleteFile) && prior.Kind == StateDirectory {
			return result, fmt.Errorf("%s target changed to a directory", action.Kind)
		}
		backupPath := ""
		if prior.Kind != StateAbsent {
			temporaryBackup := filepath.Join(actionDirectory, "prior")
			if err := copyFilesystemSnapshot(ctx, action.Target, temporaryBackup, prior); err != nil {
				return result, err
			}
			backup, err := scanFilesystemState(ctx, temporaryBackup)
			if err != nil {
				return result, err
			}
			if !statesEqual(prior, backup) {
				return result, fmt.Errorf("filesystem backup verification mismatch")
			}
			backupPath = filepath.Join(finalDirectory, "prior")
			result.temporaryBackupPath = temporaryBackup
		}
		current, err := scanFilesystemState(ctx, action.Target)
		if err != nil {
			return result, err
		}
		if !statesEqual(prior, current) {
			return result, fmt.Errorf("filesystem target changed during snapshot")
		}
		result.priorFS = prior
		result.record.prior = stateRecord(prior, backupPath)
		switch action.Kind {
		case ActionCopy:
			source, err := scanFilesystemState(ctx, action.Source)
			if err != nil {
				return result, err
			}
			if source.Kind == StateAbsent || action.SourceIsDirectory != (source.Kind == StateDirectory) {
				return result, fmt.Errorf("copy source kind differs from materialized action")
			}
			desired, err := desiredCopyState(prior, source, action.Exclude)
			if err != nil {
				return result, err
			}
			result.record.sourceDigest = source.Digest
			result.record.desired = stateRecord(desired, "")
		case ActionWriteFile:
			desired := desiredWriteState(prior, action.DesiredContent)
			result.record.desired = stateRecord(desired, "")
		case ActionDeleteFile:
			desired := absentFilesystemState()
			result.record.desired = stateRecord(desired, "")
		}
		return result, nil
	case ActionRegistrySet:
		prior, err := readRegistrySnapshot(ctx, registryReader, action.RegistryValue)
		if err != nil {
			return result, err
		}
		temporaryBackup := filepath.Join(actionDirectory, "prior.registry")
		if err := persistRegistrySnapshot(temporaryBackup, prior); err != nil {
			return result, err
		}
		current, err := readRegistrySnapshot(ctx, registryReader, action.RegistryValue)
		if err != nil {
			return result, err
		}
		if !registrySnapshotsEqual(prior, current) {
			return result, fmt.Errorf("registry value changed during snapshot")
		}
		desired, err := desiredRegistrySnapshot(action.RegistryValue)
		if err != nil {
			return result, err
		}
		backupPath := filepath.Join(finalDirectory, "prior.registry")
		result.priorRegistry = prior
		result.temporaryBackupPath = temporaryBackup
		result.record.prior = registryStateRecord(prior, backupPath)
		result.record.desired = registryStateRecord(desired, "")
		return result, nil
	default:
		return result, fmt.Errorf("unsupported materialized action kind %q", action.Kind)
	}
}

func verifyPreparedAction(ctx context.Context, registryReader RegistryReader, prepared preparedInternal) error {
	action := prepared.record.action
	switch action.Kind {
	case ActionCopy, ActionWriteFile, ActionDeleteFile:
		if err := verifyMissingTransactionParents(prepared.record.missingParents); err != nil {
			return err
		}
		if prepared.priorFS.Kind != StateAbsent {
			backup, err := scanFilesystemState(ctx, prepared.temporaryBackupPath)
			if err != nil {
				return fmt.Errorf("verify filesystem backup: %w", err)
			}
			if !statesEqual(prepared.priorFS, backup) {
				return fmt.Errorf("filesystem backup changed after verification")
			}
		}
		current, err := scanFilesystemState(ctx, action.Target)
		if err != nil {
			return err
		}
		if !statesEqual(prepared.priorFS, current) {
			return fmt.Errorf("filesystem target changed after snapshot")
		}
		if action.Kind == ActionCopy {
			source, err := scanFilesystemState(ctx, action.Source)
			if err != nil {
				return err
			}
			if source.Digest != prepared.record.sourceDigest {
				return fmt.Errorf("copy source changed after desired digest calculation")
			}
		}
		return nil
	case ActionRegistrySet:
		backup, err := loadRegistrySnapshot(prepared.temporaryBackupPath)
		if err != nil {
			return fmt.Errorf("verify registry backup: %w", err)
		}
		if !registrySnapshotsEqual(prepared.priorRegistry, backup) {
			return fmt.Errorf("registry backup changed after verification")
		}
		current, err := readRegistrySnapshot(ctx, registryReader, action.RegistryValue)
		if err != nil {
			return err
		}
		if !registrySnapshotsEqual(prepared.priorRegistry, current) {
			return fmt.Errorf("registry value changed after snapshot")
		}
		return nil
	default:
		return fmt.Errorf("unsupported prepared action kind %q", action.Kind)
	}
}

func (p *SnapshotPreparer) runCheckpoint(ctx context.Context, phase snapshotPhase, index int) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if p != nil && p.checkpoint != nil {
		return p.checkpoint(ctx, phase, index)
	}
	return nil
}

func backupError(index int, target string, err error) *Error {
	return newError(CodeBackupFailed, index, target, err)
}
