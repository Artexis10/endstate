// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// ErrRecoveryRequired marks a durable pending transaction that could not be
// proven restored. Callers must not begin new config mutation after this error.
var ErrRecoveryRequired = errors.New("config restore recovery required")

// RecoveryError identifies the transaction that remains pending.
type RecoveryError struct {
	TransactionID string
	Err           error
}

func (e *RecoveryError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%v for transaction %q: %v", ErrRecoveryRequired, e.TransactionID, e.Err)
}

func (e *RecoveryError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return []error{ErrRecoveryRequired, e.Err}
}

type storedPendingTransaction struct {
	descriptor transactionDescriptorDisk
	started    time.Time
	intent     *JournalIntent
}

func (g *Guard) recoverPending(ctx context.Context) error {
	pending, err := g.scanPending(ctx)
	if err != nil {
		return err
	}
	for _, transaction := range pending {
		if err := g.recoverOne(context.WithoutCancel(ctx), transaction); err != nil {
			return &RecoveryError{TransactionID: transaction.descriptor.TransactionID, Err: err}
		}
	}
	return nil
}

func (g *Guard) scanPending(ctx context.Context) ([]storedPendingTransaction, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(g.transactions)
	if err != nil {
		return nil, &RecoveryError{Err: fmt.Errorf("read transaction store: %w", err)}
	}
	pending := make([]storedPendingTransaction, 0)
	ordinals := make(map[string]string)
	for _, entry := range entries {
		if err := checkSnapshotContext(ctx); err != nil {
			return nil, err
		}
		root := filepath.Join(g.transactions, entry.Name())
		if !entry.IsDir() || !isOpaqueStoreID(entry.Name()) {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: fmt.Errorf("unexpected transaction-store entry")}
		}
		if err := rejectExistingTargetLinks(root); err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		intentPath := filepath.Join(root, "journal", "intent.json")
		if _, err := os.Lstat(intentPath); os.IsNotExist(err) {
			// No durable intent means no mutation was authorized. The root may be
			// a normal unused preallocation or a crash residue from descriptor/
			// snapshot preparation; under the global lease it is safe to reap.
			if err := removeSafeTransactionPath(context.WithoutCancel(ctx), root); err != nil {
				return nil, &RecoveryError{TransactionID: entry.Name(), Err: fmt.Errorf("remove no-intent transaction: %w", err)}
			}
			continue
		} else if err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		descriptor, started, err := readStoredTransactionDescriptor(root)
		if err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		intent, err := readJournalIntentMetadataFile(ctx, root, intentPath)
		if err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		lineage := intent.Lineage()
		if descriptor.TransactionID != entry.Name() || descriptor.RunID != lineage.RunID ||
			descriptor.CaptureID != lineage.CaptureID {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: fmt.Errorf("descriptor differs from journal lineage")}
		}
		ordinalKey := fmt.Sprintf("%s/%020d", descriptor.RestoreRunID, descriptor.MutationOrdinal)
		if owner, exists := ordinals[ordinalKey]; exists {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: fmt.Errorf("mutation ordinal duplicates transaction %q", owner)}
		}
		ordinals[ordinalKey] = entry.Name()
		terminalPath := journalMarkerPath(filepath.Join(root, "journal"), JournalCommitted, intent.Digest())
		marker, markerErr := readJournalMarkerFile(root, terminalPath, intent)
		if markerErr == nil {
			if marker.State() != JournalCommitted && marker.State() != JournalRolledBack {
				return nil, &RecoveryError{TransactionID: entry.Name(), Err: fmt.Errorf("unsupported terminal state %q", marker.State())}
			}
			continue
		}
		if !os.IsNotExist(markerErr) {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: fmt.Errorf("invalid terminal record: %w", markerErr)}
		}
		intent, err = ReadJournalIntent(ctx, root)
		if err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		pending = append(pending, storedPendingTransaction{descriptor: descriptor, started: started, intent: intent})
	}
	sort.Slice(pending, func(left, right int) bool {
		if !pending[left].started.Equal(pending[right].started) {
			return pending[left].started.After(pending[right].started)
		}
		if pending[left].descriptor.RestoreRunID != pending[right].descriptor.RestoreRunID {
			return pending[left].descriptor.RestoreRunID > pending[right].descriptor.RestoreRunID
		}
		return pending[left].descriptor.MutationOrdinal > pending[right].descriptor.MutationOrdinal
	})
	return pending, nil
}

func readStoredTransactionDescriptor(root string) (transactionDescriptorDisk, time.Time, error) {
	path := filepath.Join(root, "transaction.json")
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return transactionDescriptorDisk{}, time.Time{}, err
	}
	descriptor, _, err := decodeTransactionDescriptor(data)
	if err != nil {
		return transactionDescriptorDisk{}, time.Time{}, err
	}
	if descriptor.TransactionID != filepath.Base(root) {
		return transactionDescriptorDisk{}, time.Time{}, fmt.Errorf("transaction descriptor path identity differs")
	}
	started, err := time.Parse(time.RFC3339Nano, descriptor.RunStartedAtUTC)
	if err != nil {
		return transactionDescriptorDisk{}, time.Time{}, err
	}
	return descriptor, started, nil
}

func (g *Guard) recoverOne(ctx context.Context, transaction storedPendingTransaction) error {
	actions := transaction.intent.Actions()
	var rollbackErrors []error
	for index := len(actions) - 1; index >= 0; index-- {
		if err := rollbackTransactionAction(ctx, actions[index], g.registry); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback action[%d]: %w", index, err))
		}
	}
	if err := verifyAllTransactionStates(ctx, actions, g.registry, false); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("verify complete recovery: %w", err))
	}
	if err := errors.Join(rollbackErrors...); err != nil {
		return err
	}
	_, err := PersistRolledBackMarker(ctx, transaction.intent, ValidationNotRun)
	if err != nil {
		return fmt.Errorf("persist recovered terminal record: %w", err)
	}
	return nil
}
