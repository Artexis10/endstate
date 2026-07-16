// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// PersistCommittedMarker durably records a validated completed transaction.
func PersistCommittedMarker(ctx context.Context, intent *JournalIntent) (*JournalMarker, error) {
	return NewJournalWriter().PersistCommitted(ctx, intent)
}

// PersistRolledBackMarker durably records a completely proven rollback. An
// incomplete rollback must not call this function and leaves the intent pending.
func PersistRolledBackMarker(
	ctx context.Context,
	intent *JournalIntent,
	validation ValidationStatus,
) (*JournalMarker, error) {
	return NewJournalWriter().PersistRolledBack(ctx, intent, validation)
}

// PersistAbortedMarker durably closes a pending intent when the engine proves
// that it performed no target mutation. The journal uses the rolled_back
// terminal state with rollbackOutcome=not_required; envelope status remains
// failed rather than rolled_back.
func PersistAbortedMarker(ctx context.Context, intent *JournalIntent) (*JournalMarker, error) {
	return NewJournalWriter().PersistAborted(ctx, intent)
}

func (w *JournalWriter) PersistCommitted(ctx context.Context, intent *JournalIntent) (*JournalMarker, error) {
	return w.persistMarker(ctx, intent, JournalCommitted, ValidationPassed, RollbackNotRequired)
}

func (w *JournalWriter) PersistRolledBack(
	ctx context.Context,
	intent *JournalIntent,
	validation ValidationStatus,
) (*JournalMarker, error) {
	return w.persistMarker(ctx, intent, JournalRolledBack, validation, RollbackSucceeded)
}

func (w *JournalWriter) PersistAborted(ctx context.Context, intent *JournalIntent) (*JournalMarker, error) {
	return w.persistMarker(ctx, intent, JournalRolledBack, ValidationNotRun, RollbackNotRequired)
}

func (w *JournalWriter) persistMarker(
	ctx context.Context,
	intent *JournalIntent,
	state JournalState,
	validation ValidationStatus,
	rollback RollbackOutcome,
) (*JournalMarker, error) {
	if err := validateMarkerOutcome(state, validation, rollback); err != nil {
		return nil, journalCompletionError("", err)
	}
	verifiedIntent, err := verifyIntentForMarker(ctx, intent)
	if err != nil {
		return nil, journalCompletionError(intentPathOrEmpty(intent), err)
	}
	root := verifiedIntent.transactionRoot
	journalDirectory := filepath.Join(root, "journal")
	disk, encoded, err := newJournalMarkerDisk(verifiedIntent.digest, state, validation, rollback)
	if err != nil {
		return nil, journalCompletionError(journalDirectory, err)
	}
	path := journalMarkerPath(journalDirectory, state, verifiedIntent.digest)
	oppositePath := journalMarkerPath(journalDirectory, oppositeJournalState(state), verifiedIntent.digest)
	if exists, err := pathExistsNoLink(oppositePath); err != nil {
		return nil, journalCompletionError(oppositePath, err)
	} else if exists {
		return nil, journalCompletionError(oppositePath, fmt.Errorf("conflicting terminal journal marker exists"))
	}
	if existing, err := readJournalMarkerFile(root, path, verifiedIntent); err == nil {
		existingBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, journalCompletionError(path, readErr)
		}
		if existing.digest != disk.MarkerDigest || !bytes.Equal(existingBytes, encoded) {
			return nil, journalCompletionError(path, fmt.Errorf("conflicting terminal journal marker exists"))
		}
		reconciled, reconcileErr := w.reconcileMarker(root, path, verifiedIntent, encoded, disk.MarkerDigest)
		if reconcileErr != nil {
			return nil, journalCompletionError(path, errors.Join(ErrPublicationAmbiguous, reconcileErr))
		}
		return reconciled, nil
	} else if !os.IsNotExist(err) {
		return nil, journalCompletionError(path, err)
	}

	temporary, err := os.CreateTemp(journalDirectory, ".marker-*.tmp")
	if err != nil {
		return nil, journalCompletionError(journalDirectory, err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return nil, journalCompletionError(temporaryPath, err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		return nil, journalCompletionError(temporaryPath, err)
	}
	if err := temporary.Sync(); err != nil {
		return nil, journalCompletionError(temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return nil, journalCompletionError(temporaryPath, err)
	}
	if err := w.runCheckpoint(ctx, journalPhaseBeforeMarkerPublish, path); err != nil {
		return nil, journalCompletionError(path, err)
	}
	publication, publishErr := w.publishNoReplace(temporaryPath, path)
	if publishErr != nil || publication != publicationDurable {
		reconciled, reconcileErr := w.reconcileMarker(root, path, verifiedIntent, encoded, disk.MarkerDigest)
		if reconcileErr == nil {
			return reconciled, nil
		}
		if publishErr == nil {
			publishErr = fmt.Errorf("publisher returned non-durable success state %d", publication)
		}
		return nil, journalCompletionError(path, publicationFailure(publication, publishErr, reconcileErr))
	}

	// Publication is the terminal transition. The platform helper does not
	// return success until the destination is durable, so no fallible work may
	// follow that could tell the caller to roll back behind this marker.
	return markerFromDisk(root, path, disk, verifiedIntent), nil
}

func (w *JournalWriter) reconcileMarker(
	root string,
	path string,
	intent *JournalIntent,
	expectedBytes []byte,
	expectedDigest string,
) (*JournalMarker, error) {
	check := func() (*JournalMarker, error) {
		existing, err := readJournalMarkerFile(root, path, intent)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if existing.digest != expectedDigest || !bytes.Equal(data, expectedBytes) {
			return nil, fmt.Errorf("published terminal marker conflicts with requested content")
		}
		return existing, nil
	}
	if _, err := check(); err != nil {
		return nil, err
	}
	if err := w.syncReconciledFile(path); err != nil {
		return nil, err
	}
	if err := w.syncReconciledDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	return check()
}

func verifyIntentForMarker(ctx context.Context, intent *JournalIntent) (*JournalIntent, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, err
	}
	if intent == nil || intent.transactionRoot == "" || intent.digest == "" {
		return nil, fmt.Errorf("verified journal intent is required")
	}
	expectedPath := filepath.Join(intent.transactionRoot, "journal", "intent.json")
	if intent.path != expectedPath {
		return nil, fmt.Errorf("journal intent path differs from its verified transaction root")
	}
	verified, err := ReadJournalIntent(ctx, intent.transactionRoot)
	if err != nil {
		return nil, err
	}
	if verified.digest != intent.digest {
		return nil, fmt.Errorf("journal intent identity changed")
	}
	return verified, nil
}

func readJournalMarkerFile(root, path string, intent *JournalIntent) (*JournalMarker, error) {
	if intent == nil || intent.transactionRoot != root || intent.digest == "" {
		return nil, fmt.Errorf("verified journal intent is required")
	}
	if err := rejectExistingTargetLinks(filepath.Dir(path)); err != nil {
		return nil, err
	}
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return nil, err
	}
	disk, err := decodeJournalMarker(data)
	if err != nil {
		return nil, err
	}
	if disk.IntentDigest != intent.digest {
		return nil, fmt.Errorf("journal marker references a different intent")
	}
	if path != journalMarkerPath(filepath.Join(root, "journal"), disk.State, disk.IntentDigest) {
		return nil, fmt.Errorf("journal marker path does not match its closed state and intent")
	}
	return markerFromDisk(root, path, disk, intent), nil
}

func journalMarkerPath(directory string, state JournalState, digest string) string {
	prefix := "committed-"
	if state == JournalRolledBack {
		prefix = "rolled-back-"
	}
	return filepath.Join(directory, prefix+digest+".json")
}

func oppositeJournalState(state JournalState) JournalState {
	if state == JournalCommitted {
		return JournalRolledBack
	}
	return JournalCommitted
}

func pathExistsNoLink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if isLinkOrReparse(info) || !info.Mode().IsRegular() {
		return false, fmt.Errorf("journal marker path is not a safe regular file")
	}
	return true, nil
}

func intentPathOrEmpty(intent *JournalIntent) string {
	if intent == nil {
		return ""
	}
	return intent.path
}

func journalCompletionError(target string, err error) *Error {
	return newError(CodeJournalCompletionFailed, -1, target, err)
}
