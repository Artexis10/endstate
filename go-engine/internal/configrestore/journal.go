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
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// PersistJournalIntent durably publishes one immutable pending intent before
// any target mutation. An identical existing intent is returned idempotently.
func PersistJournalIntent(ctx context.Context, request JournalIntentRequest) (*JournalIntent, error) {
	return NewJournalWriter().PersistIntent(ctx, request)
}

// PersistIntent is PersistJournalIntent with private failure checkpoints.
func (w *JournalWriter) PersistIntent(ctx context.Context, request JournalIntentRequest) (*JournalIntent, error) {
	root, lineage, actions, validations, err := validateJournalIntentRequest(ctx, request)
	if err != nil {
		return nil, journalIntentError(request.TransactionRoot, err)
	}
	if err := validateAndSyncPreparedSnapshots(ctx, w, root, request.Prepared, actions); err != nil {
		return nil, journalIntentError(request.Prepared.SnapshotRoot(), err)
	}
	disk, encoded, err := newJournalIntentDisk(lineage, actions, validations)
	if err != nil {
		return nil, journalIntentError(root, err)
	}
	journalDirectory := filepath.Join(root, "journal")
	created, err := ensureJournalDirectory(journalDirectory)
	if err != nil {
		return nil, journalIntentError(journalDirectory, err)
	}
	if created {
		if err := syncDurableDirectory(root); err != nil {
			return nil, journalIntentError(root, err)
		}
	}
	intentPath := filepath.Join(journalDirectory, "intent.json")
	if existing, err := readJournalIntentFile(ctx, root, intentPath); err == nil {
		existingBytes, readErr := os.ReadFile(intentPath)
		if readErr != nil {
			return nil, journalIntentError(intentPath, readErr)
		}
		if existing.digest != disk.IntentDigest || !bytes.Equal(existingBytes, encoded) {
			return nil, journalIntentError(intentPath, fmt.Errorf("conflicting journal intent already exists"))
		}
		reconciled, reconcileErr := w.reconcileIntent(ctx, root, intentPath, encoded, disk.IntentDigest)
		if reconcileErr != nil {
			return nil, journalIntentError(intentPath, errors.Join(ErrPublicationAmbiguous, reconcileErr))
		}
		return reconciled, nil
	} else if !os.IsNotExist(err) {
		return nil, journalIntentError(intentPath, err)
	}

	temporary, err := os.CreateTemp(journalDirectory, ".intent-*.tmp")
	if err != nil {
		return nil, journalIntentError(journalDirectory, err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return nil, journalIntentError(temporaryPath, err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		return nil, journalIntentError(temporaryPath, err)
	}
	if err := temporary.Sync(); err != nil {
		return nil, journalIntentError(temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return nil, journalIntentError(temporaryPath, err)
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, journalIntentError(intentPath, err)
	}
	if err := w.runCheckpoint(ctx, journalPhaseBeforeIntentPublish, intentPath); err != nil {
		return nil, journalIntentError(intentPath, err)
	}
	publication, publishErr := w.publishNoReplace(temporaryPath, intentPath)
	if publishErr != nil || publication != publicationDurable {
		reconciled, reconcileErr := w.reconcileIntent(ctx, root, intentPath, encoded, disk.IntentDigest)
		if reconcileErr == nil {
			return reconciled, nil
		}
		if publishErr == nil {
			publishErr = fmt.Errorf("publisher returned non-durable success state %d", publication)
		}
		return nil, journalIntentError(intentPath, publicationFailure(publication, publishErr, reconcileErr))
	}
	if err := w.runCheckpoint(ctx, journalPhaseAfterIntentPublish, intentPath); err != nil {
		return nil, journalIntentError(intentPath, err)
	}
	if err := syncDurableDirectory(journalDirectory); err != nil {
		return nil, journalIntentError(journalDirectory, err)
	}
	verified, err := readJournalIntentFile(ctx, root, intentPath)
	if err != nil {
		return nil, journalIntentError(intentPath, err)
	}
	if verified.digest != disk.IntentDigest {
		return nil, journalIntentError(intentPath, fmt.Errorf("journal intent readback identity changed"))
	}
	return verified, nil
}

func (w *JournalWriter) reconcileIntent(
	ctx context.Context,
	root string,
	path string,
	expectedBytes []byte,
	expectedDigest string,
) (*JournalIntent, error) {
	reconcileContext := context.WithoutCancel(ctx)
	check := func() (*JournalIntent, error) {
		existing, err := readJournalIntentFile(reconcileContext, root, path)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if existing.digest != expectedDigest || !bytes.Equal(data, expectedBytes) {
			return nil, fmt.Errorf("published journal intent conflicts with requested content")
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

// ReadJournalIntent reads and verifies the canonical pending intent beneath a
// safe transaction root.
func ReadJournalIntent(ctx context.Context, transactionRoot string) (*JournalIntent, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, journalIntentError(transactionRoot, err)
	}
	root, err := validateJournalRoot(transactionRoot)
	if err != nil {
		return nil, journalIntentError(transactionRoot, err)
	}
	path := filepath.Join(root, "journal", "intent.json")
	intent, err := readJournalIntentFile(ctx, root, path)
	if err != nil {
		return nil, journalIntentError(path, err)
	}
	return intent, nil
}

func readJournalIntentFile(ctx context.Context, root, path string) (*JournalIntent, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, err
	}
	if err := rejectExistingTargetLinks(filepath.Dir(path)); err != nil {
		return nil, err
	}
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return nil, err
	}
	disk, err := decodeJournalIntent(data)
	if err != nil {
		return nil, err
	}
	if err := validateJournalLineage(disk.Lineage); err != nil {
		return nil, err
	}
	if err := validateJournalActions(root, disk.Actions); err != nil {
		return nil, err
	}
	if err := validateJournalValidations(root, disk.Actions, disk.Validations); err != nil {
		return nil, err
	}
	if _, _, err := verifySnapshotArtifacts(ctx, filepath.Join(root, "snapshots"), disk.Actions); err != nil {
		return nil, err
	}
	return intentFromDisk(root, path, disk), nil
}

func validateJournalIntentRequest(
	ctx context.Context,
	request JournalIntentRequest,
) (string, JournalLineage, []JournalAction, []JournalValidation, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return "", JournalLineage{}, nil, nil, err
	}
	root, err := validateJournalRoot(request.TransactionRoot)
	if err != nil {
		return "", JournalLineage{}, nil, nil, err
	}
	if request.Prepared == nil || request.Prepared.SnapshotRoot() != filepath.Join(root, "snapshots") {
		return "", JournalLineage{}, nil, nil, fmt.Errorf("verified prepared set from the transaction root is required")
	}
	preparedActions := request.Prepared.Actions()
	if len(preparedActions) == 0 {
		return "", JournalLineage{}, nil, nil, fmt.Errorf("journal intent requires at least one prepared action")
	}
	lineage := cloneJournalLineage(request.Lineage)
	if err := validateJournalLineage(lineage); err != nil {
		return "", JournalLineage{}, nil, nil, err
	}
	actions := make([]JournalAction, len(preparedActions))
	for index, prepared := range preparedActions {
		action := prepared.Action()
		prior := prepared.Prior()
		desired := prepared.Desired()
		actions[index] = JournalAction{
			Index: index, Kind: action.Kind, Strategy: action.Strategy, Target: action.Target,
			MissingParents: prepared.MissingParents(),
			Prior:          journalActionState(prior), Desired: journalActionState(desired), SourceDigest: prepared.SourceDigest(),
		}
		if action.RegistryValue != nil {
			actions[index].RegistryKey = action.RegistryValue.Key
			actions[index].RegistryValueName = action.RegistryValue.ValueName
		}
	}
	resolved := request.Prepared.Validations()
	validations := make([]JournalValidation, len(resolved))
	for index, validation := range resolved {
		validations[index] = JournalValidation{
			Type: validation.Definition.Type, Path: validation.Definition.Path, JSONPath: validation.Definition.JSONPath,
			Section: validation.Definition.Section, Key: validation.Definition.Key, HostPath: validation.HostPath,
		}
	}
	if err := validateJournalActions(root, actions); err != nil {
		return "", JournalLineage{}, nil, nil, err
	}
	if err := validateJournalValidations(root, actions, validations); err != nil {
		return "", JournalLineage{}, nil, nil, err
	}
	return root, lineage, actions, validations, nil
}

func validateJournalRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", fmt.Errorf("transaction root must be a clean absolute path")
	}
	if err := rejectExistingTargetLinks(root); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return "", fmt.Errorf("transaction root must be an existing safe directory")
	}
	return root, nil
}

func ensureJournalDirectory(path string) (bool, error) {
	created := false
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return false, err
	} else if err == nil {
		created = true
	}
	if err := rejectExistingTargetLinks(path); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return false, fmt.Errorf("journal path must be a safe directory")
	}
	return created, nil
}

func validateJournalLineage(lineage JournalLineage) error {
	identities := []struct {
		name  string
		value string
	}{
		{"run ID", lineage.RunID}, {"capture ID", lineage.CaptureID}, {"module ID", lineage.ModuleID},
		{"config-set ID", lineage.ConfigSetID}, {"target-instance ID", lineage.TargetInstanceID},
		{"source generation", lineage.SourceGeneration}, {"target generation", lineage.TargetGeneration},
	}
	for _, identity := range identities {
		if identity.value == "" || identity.value != strings.TrimSpace(identity.value) || containsControl(identity.value) {
			return fmt.Errorf("%s is required", identity.name)
		}
	}
	for name, digest := range map[string]string{
		"source-generation fingerprint": lineage.SourceGenerationFingerprint,
		"capture module revision":       lineage.CaptureModuleRevision,
		"restore module revision":       lineage.RestoreModuleRevision,
	} {
		if !isLowerHexDigest(digest) {
			return fmt.Errorf("%s must be a lowercase SHA-256 digest", name)
		}
	}
	if lineage.SourceGeneration == lineage.TargetGeneration {
		if len(lineage.MigrationPath) != 0 {
			return fmt.Errorf("direct lineage migration path must be empty")
		}
	} else if len(lineage.MigrationPath) < 2 || lineage.MigrationPath[0] != lineage.SourceGeneration ||
		lineage.MigrationPath[len(lineage.MigrationPath)-1] != lineage.TargetGeneration {
		return fmt.Errorf("migration path must start at source and end at target generation")
	}
	for _, generation := range lineage.MigrationPath {
		if generation == "" || generation != strings.TrimSpace(generation) || containsControl(generation) {
			return fmt.Errorf("migration path contains an empty generation")
		}
	}
	return nil
}

func isLowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func journalActionState(state StateRecord) JournalActionState {
	stateEntries := state.Entries()
	entries := make([]JournalFilesystemEntry, len(stateEntries))
	for index, entry := range stateEntries {
		entries[index] = JournalFilesystemEntry{
			Path: entry.Path, Kind: entry.Kind, Mode: uint32(entry.Mode.Perm()), Size: entry.Size, ContentHash: entry.ContentHash,
		}
	}
	return JournalActionState{
		Kind: state.Kind, Digest: state.Digest, Mode: uint32(state.Mode.Perm()), BackupPath: state.BackupPath,
		Entries: entries,
	}
}

func journalIntentError(target string, err error) *Error {
	return newError(CodeJournalIntentFailed, -1, target, err)
}

func (w *JournalWriter) runCheckpoint(ctx context.Context, phase journalPhase, path string) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if w != nil && w.checkpoint != nil {
		if err := w.checkpoint(ctx, phase, path); err != nil {
			return err
		}
	}
	return checkSnapshotContext(ctx)
}
