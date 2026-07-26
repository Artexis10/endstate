// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/restore"
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
	ctx = withHostBoundary(ctx, g.boundary)
	if err := g.reconcileLegacyRevertWork(ctx); err != nil {
		return err
	}
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

// reconcileLegacyRevertWork retires only legacy work roots backed by an exact
// durable consumption marker. Unmarked registered roots are resumable; every
// other shape is recovery-required rather than ignored.
func (g *Guard) reconcileLegacyRevertWork(ctx context.Context) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := validateHostIO(ctx, g.legacyRevertWork); err != nil {
		return &RecoveryError{TransactionID: "legacy-revert-work", Err: err}
	}
	if err := rejectExistingTargetLinks(g.legacyRevertWork); err != nil {
		return &RecoveryError{TransactionID: "legacy-revert-work", Err: err}
	}
	entries, err := os.ReadDir(g.legacyRevertWork)
	if err != nil {
		return &RecoveryError{TransactionID: "legacy-revert-work", Err: fmt.Errorf("read legacy revert work store: %w", err)}
	}
	for _, entry := range entries {
		if err := checkSnapshotContext(ctx); err != nil {
			return err
		}
		memberID := entry.Name()
		root := filepath.Join(g.legacyRevertWork, memberID)
		if err := validateHostIO(ctx, root); err != nil {
			return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: err}
		}
		if !isOpaqueStoreID(memberID) || !entry.IsDir() {
			return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: fmt.Errorf("unexpected legacy revert work entry")}
		}
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || isLinkOrReparse(info) {
			if err == nil {
				err = fmt.Errorf("legacy revert work root is not a safe directory")
			}
			return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: err}
		}
		disk, err := g.loadLegacyMemberByID(memberID)
		if err != nil {
			return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: fmt.Errorf("read matching legacy member: %w", err)}
		}
		journal, err := g.readLegacyRevertJournal(disk)
		if err != nil {
			return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: fmt.Errorf("read registered legacy journal: %w", err)}
		}
		if err := validateLegacyRevertWorkTree(ctx, root, journal); err != nil {
			return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: err}
		}
		reverted, err := memberReverted(
			filepath.Join(g.legacyReverts, memberID+".json"), StoreMemberLegacy, memberID, disk.MemberDigest, g.boundary,
		)
		if err != nil {
			return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: fmt.Errorf("read matching legacy revert marker: %w", err)}
		}
		if reverted {
			if err := g.retireLegacyMemberRevertWork(ctx, memberID); err != nil {
				return &RecoveryError{TransactionID: "legacy-revert-work/" + memberID, Err: err}
			}
		}
	}
	return nil
}

func (g *Guard) readLegacyRevertJournal(disk legacyMemberDisk) (*restore.Journal, error) {
	path, err := resolveLegacyJournalIdentity(disk.JournalPath, g.boundary)
	if err != nil {
		return nil, err
	}
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != disk.JournalDigest {
		return nil, fmt.Errorf("registered legacy journal changed")
	}
	return restore.ParseJournal(data)
}

func validateLegacyRevertWorkTree(ctx context.Context, root string, journal *restore.Journal) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := validateHostIO(ctx, root); err != nil {
		return err
	}
	if err := rejectExistingTargetLinks(root); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		if err == nil {
			err = fmt.Errorf("legacy revert work path is not a safe directory")
		}
		return err
	}
	if journal == nil {
		return fmt.Errorf("legacy revert work journal is required")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if err := validateHostIO(ctx, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("legacy revert work entry %q is a link or reparse point", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("legacy revert work entry %q has unsupported special type", path)
		}
		if err := validateLegacyRevertWorkRecord(path, entry.Name(), journal); err != nil {
			return err
		}
	}
	return nil
}

type legacyRevertWorkState struct {
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
}

type legacyRevertWorkPrepared struct {
	Version       int                   `json:"version"`
	EntryIndex    int                   `json:"entryIndex"`
	EntryDigest   string                `json:"entryDigest"`
	Target        string                `json:"target"`
	Before        legacyRevertWorkState `json:"before"`
	Desired       legacyRevertWorkState `json:"desired"`
	DesiredSource string                `json:"desiredSource,omitempty"`
	StagePath     string                `json:"stagePath,omitempty"`
	HeldPath      string                `json:"heldPath,omitempty"`
}

type legacyRevertWorkCompleted struct {
	Version     int    `json:"version"`
	EntryIndex  int    `json:"entryIndex"`
	EntryDigest string `json:"entryDigest"`
	Action      string `json:"action"`
}

func validateLegacyRevertWorkRecord(path, name string, journal *restore.Journal) error {
	index, completed, ok := parseLegacyRevertWorkRecordName(name)
	if !ok || index < 0 || index >= len(journal.Entries) {
		return fmt.Errorf("legacy revert work entry %q is not a valid durable record name", path)
	}
	entry := journal.Entries[index]
	if entry.Action != "restored" {
		return fmt.Errorf("legacy revert work entry %q binds a non-restored journal entry", path)
	}
	digest, err := legacyRevertJournalEntryDigest(entry)
	if err != nil {
		return err
	}
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return err
	}
	if completed {
		var record legacyRevertWorkCompleted
		if err := decodeCanonicalLegacyRevertWorkRecord(data, &record); err != nil {
			return err
		}
		if record.Version != 1 || record.EntryIndex != index || record.EntryDigest != digest ||
			(record.Action != "reverted" && record.Action != "deleted") {
			return fmt.Errorf("legacy revert completion record %q differs from its journal entry", path)
		}
		return nil
	}
	var record legacyRevertWorkPrepared
	if err := decodeCanonicalLegacyRevertWorkRecord(data, &record); err != nil {
		return err
	}
	if record.Version != 1 || record.EntryIndex != index || record.EntryDigest != digest ||
		!validLegacyRevertWorkState(record.Before) || !validLegacyRevertWorkState(record.Desired) {
		return fmt.Errorf("legacy revert prepared record %q is invalid", path)
	}
	expectedTarget := entry.TargetPath
	if entry.RestoreType != "registry-import" && entry.RestoreType != "registry-set" {
		expectedTarget = filepath.Clean(expectedTarget)
	}
	if record.Target != expectedTarget {
		return fmt.Errorf("legacy revert prepared record %q target differs from its journal entry", path)
	}
	expectedSource := entry.BackupPath
	if entry.RestoreType != "registry-import" && entry.RestoreType != "registry-set" {
		if entry.BackupCreated && entry.BackupPath != "" {
			expectedSource = filepath.Clean(entry.BackupPath)
		} else {
			expectedSource = ""
		}
	}
	if record.DesiredSource != expectedSource {
		return fmt.Errorf("legacy revert prepared record %q desired source differs from its journal entry", path)
	}
	if entry.RestoreType != "registry-import" && entry.RestoreType != "registry-set" {
		suffix := digest[:16]
		if record.StagePath != legacyRevertFilesystemScratch(entry.TargetPath, suffix, "stage") ||
			record.HeldPath != legacyRevertFilesystemScratch(entry.TargetPath, suffix, "held") {
			return fmt.Errorf("legacy revert prepared record %q scratch paths differ from its journal entry", path)
		}
	}
	return nil
}

func legacyRevertFilesystemScratch(target, suffix, kind string) string {
	clean := filepath.Clean(target)
	return filepath.Join(filepath.Dir(clean), "."+filepath.Base(clean)+".endstate-revert-"+suffix+"-"+kind)
}

func parseLegacyRevertWorkRecordName(name string) (int, bool, bool) {
	const prefix = "entry-"
	const preparedSuffix = ".json"
	const completedSuffix = "-completed.json"
	if !strings.HasPrefix(name, prefix) {
		return 0, false, false
	}
	rest := strings.TrimPrefix(name, prefix)
	completed := strings.HasSuffix(rest, completedSuffix)
	suffix := preparedSuffix
	if completed {
		suffix = completedSuffix
	}
	if !strings.HasSuffix(rest, suffix) {
		return 0, false, false
	}
	digits := strings.TrimSuffix(rest, suffix)
	if len(digits) != 6 {
		return 0, false, false
	}
	for _, character := range digits {
		if character < '0' || character > '9' {
			return 0, false, false
		}
	}
	index, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false, false
	}
	return index, completed, true
}

func decodeCanonicalLegacyRevertWorkRecord(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(data, canonical) {
		return fmt.Errorf("legacy revert work record is not canonical")
	}
	return nil
}

func validLegacyRevertWorkState(state legacyRevertWorkState) bool {
	if !isLowerHexDigest(state.Digest) {
		return false
	}
	switch state.Kind {
	case "file", "directory", "absent", "registry-key", "registry-value":
		return true
	default:
		return false
	}
}

func legacyRevertJournalEntryDigest(entry restore.JournalEntry) (string, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (g *Guard) scanPending(ctx context.Context) ([]storedPendingTransaction, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, err
	}
	if err := validateHostIO(ctx, g.transactions); err != nil {
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
		if err := validateHostIO(ctx, root); err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		if !entry.IsDir() || !isOpaqueStoreID(entry.Name()) {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: fmt.Errorf("unexpected transaction-store entry")}
		}
		if err := rejectExistingTargetLinks(root); err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		intentPath := filepath.Join(root, "journal", "intent.json")
		if err := validateHostIO(ctx, intentPath); err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
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
		descriptor, started, err := readStoredTransactionDescriptorWithBoundary(root, g.boundary)
		if err != nil {
			return nil, &RecoveryError{TransactionID: entry.Name(), Err: err}
		}
		intent, err := readJournalIntentMetadataFileWithBoundary(ctx, root, intentPath, g.boundary)
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
		intent, err = ReadJournalIntentWithBoundary(ctx, root, g.boundary)
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
	return readStoredTransactionDescriptorWithBoundary(root, nil)
}

func readStoredTransactionDescriptorWithBoundary(root string, boundary HostBoundary) (transactionDescriptorDisk, time.Time, error) {
	path := filepath.Join(root, "transaction.json")
	if err := validateBoundaryHostIO(boundary, path); err != nil {
		return transactionDescriptorDisk{}, time.Time{}, err
	}
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
	ctx = withHostBoundary(ctx, g.boundary)
	actions, err := resolveJournalActionsForHostIO(transaction.intent.Actions(), g.boundary)
	if err != nil {
		return err
	}
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
	_, err = PersistRolledBackMarker(ctx, transaction.intent, ValidationNotRun)
	if err != nil {
		return fmt.Errorf("persist recovered terminal record: %w", err)
	}
	return nil
}
