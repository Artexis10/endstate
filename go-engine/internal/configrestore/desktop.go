// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

const maxDesktopSnapshotBytes = 32 << 10

// Private snapshots bind portable state to the actual runtime namespace.
type desktopSnapshot struct {
	Target desktopsettings.Target `json:"target"`
	Scope  string                 `json:"scope"`
	Value  desktopsettings.Value  `json:"value"`
}

func (s desktopSnapshot) validate() error {
	if err := s.Target.Validate(); err != nil {
		return err
	}
	return (desktopsettings.Observation{Scope: s.Scope, User: s.Value}).Validate()
}
func (s desktopSnapshot) digest() string {
	data, _ := json.Marshal(s)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func desktopState(s desktopSnapshot, backup string) StateRecord {
	kind := StateAbsent
	if s.Value.Present {
		kind = StateDesktopValue
	}
	return StateRecord{Kind: kind, Digest: s.digest(), BackupPath: backup, desktop: &s}
}
func readDesktopSnapshot(ctx context.Context, target desktopsettings.Target) (desktopSnapshot, error) {
	observation, err := desktopsettings.Read(ctx, desktopsettings.Backend(ctx), target)
	if err != nil {
		return desktopSnapshot{}, err
	}
	return desktopSnapshot{Target: target, Scope: observation.Scope, Value: observation.User}, nil
}
func decodeDesktopSnapshot(data []byte) (desktopSnapshot, error) {
	if len(data) > maxDesktopSnapshotBytes {
		return desktopSnapshot{}, fmt.Errorf("desktop snapshot exceeds byte limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var s desktopSnapshot
	if err := decoder.Decode(&s); err != nil {
		return s, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return s, err
	}
	if err := s.validate(); err != nil {
		return s, err
	}
	canonical, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	if !bytes.Equal(data, append(canonical, '\n')) {
		return s, fmt.Errorf("desktop snapshot is not canonical")
	}
	return s, nil
}
func loadDesktopSnapshot(path string) (desktopSnapshot, error) {
	data, _, err := safepath.ReadRegularFileBounded(path, maxDesktopSnapshotBytes)
	if err != nil {
		return desktopSnapshot{}, err
	}
	return decodeDesktopSnapshot(data)
}
func prepareDesktopSnapshot(ctx context.Context, result preparedInternal, actionDirectory, finalDirectory string, boundary HostBoundary) (preparedInternal, error) {
	action := result.record.action
	if action.DesktopTarget == nil || action.DesktopValue == nil || action.Target != action.DesktopTarget.Identity() {
		return result, fmt.Errorf("desktop action lacks trusted target or desired state")
	}
	if err := action.DesktopValue.Validate(); err != nil {
		return result, err
	}
	prior, err := readDesktopSnapshot(ctx, *action.DesktopTarget)
	if err != nil {
		return result, err
	}
	desired := prior
	desired.Value = action.DesktopValue.Clone()
	temporary := filepath.Join(actionDirectory, "prior.desktop")
	if err := validateHostIO(ctx, temporary); err != nil {
		return result, err
	}
	data, err := json.Marshal(prior)
	if err != nil {
		return result, err
	}
	if err := os.WriteFile(temporary, append(data, '\n'), 0600); err != nil {
		return result, err
	}
	backup, err := projectFilesystemIdentity(boundary, filepath.Join(finalDirectory, "prior.desktop"))
	if err != nil {
		return result, err
	}
	result.temporaryBackupPath = temporary
	result.record.prior = desktopState(prior, backup)
	result.record.desired = desktopState(desired, "")
	if err := verifyPreparedDesktop(ctx, result); err != nil {
		return result, err
	}
	return result, nil
}
func verifyPreparedDesktop(ctx context.Context, prepared preparedInternal) error {
	prior := prepared.record.prior.desktop
	if prior == nil {
		return fmt.Errorf("desktop snapshot is missing")
	}
	if err := validateHostIO(ctx, prepared.temporaryBackupPath); err != nil {
		return err
	}
	backup, err := loadDesktopSnapshot(prepared.temporaryBackupPath)
	if err != nil {
		return err
	}
	current, err := readDesktopSnapshot(ctx, prior.Target)
	if err != nil {
		return err
	}
	if backup.digest() != prior.digest() || current.digest() != prior.digest() {
		return fmt.Errorf("desktop value or scope changed during snapshot")
	}
	return nil
}
func journalDesktopSnapshot(action JournalAction, state JournalActionState) (desktopSnapshot, error) {
	if action.DesktopTarget == nil || state.Desktop == nil {
		return desktopSnapshot{}, fmt.Errorf("desktop journal target or state is missing")
	}
	snapshot := desktopSnapshot{Target: *action.DesktopTarget, Scope: action.DesktopScope, Value: state.Desktop.Clone()}
	if err := snapshot.validate(); err != nil {
		return snapshot, err
	}
	kind := StateAbsent
	if snapshot.Value.Present {
		kind = StateDesktopValue
	}
	if state.Kind != kind || state.Digest != snapshot.digest() || state.Mode != 0 || len(state.Entries) != 0 {
		return snapshot, fmt.Errorf("desktop journal state differs from its typed digest")
	}
	return snapshot, nil
}
func validateDesktopJournalAction(action JournalAction, actionRoot string) error {
	if action.Strategy != "desktop-value" || action.DesktopTarget == nil || action.Target != action.DesktopTarget.Identity() || action.RegistryKey != "" || action.RegistryValueName != "" || action.SourceDigest != "" || len(action.MissingParents) != 0 || len(action.TransferredPaths) != 0 || action.Prior.BackupPath != filepath.Join(actionRoot, "prior.desktop") || action.Desired.BackupPath != "" {
		return fmt.Errorf("desktop journal identity is inconsistent")
	}
	if _, err := journalDesktopSnapshot(action, action.Prior); err != nil {
		return err
	}
	_, err := journalDesktopSnapshot(action, action.Desired)
	return err
}
func verifyDesktopBackup(action JournalAction, data []byte) error {
	backup, err := decodeDesktopSnapshot(data)
	if err != nil {
		return err
	}
	prior, err := journalDesktopSnapshot(action, action.Prior)
	if err != nil {
		return err
	}
	if backup.digest() != prior.digest() {
		return fmt.Errorf("desktop prior backup differs from journal")
	}
	return nil
}
func classifyDesktopState(ctx context.Context, action JournalAction) (rollbackStateClass, error) {
	prior, err := journalDesktopSnapshot(action, action.Prior)
	if err != nil {
		return 0, err
	}
	desired, err := journalDesktopSnapshot(action, action.Desired)
	if err != nil {
		return 0, err
	}
	actual, err := readDesktopSnapshot(ctx, prior.Target)
	if err != nil {
		return 0, err
	}
	// Scope and third-value conflicts preserve another owner's state. They keep
	// the existing pending mutation stop until recovery can prove ownership.
	if actual.Scope != prior.Scope {
		return 0, fmt.Errorf("%w: runtime scope changed", desktopsettings.ErrConflict)
	}
	if actual.digest() == prior.digest() {
		return rollbackStatePrior, nil
	}
	if actual.digest() == desired.digest() {
		return rollbackStateDesired, nil
	}
	return 0, fmt.Errorf("%w: value differs from prior and desired", desktopsettings.ErrConflict)
}
func writeDesktopState(ctx context.Context, action JournalAction, state JournalActionState) error {
	snapshot, err := journalDesktopSnapshot(action, state)
	if err != nil {
		return err
	}
	backend := desktopsettings.Backend(ctx)
	if backend == nil {
		return desktopsettings.ErrUnavailable
	}
	// Recheck immediately before writing, even when the caller already checked.
	if _, err := classifyDesktopState(ctx, action); err != nil {
		return err
	}
	if err := backend.Write(ctx, snapshot.Target, snapshot.Scope, snapshot.Value.Clone()); err != nil {
		return err
	}
	actual, err := readDesktopSnapshot(ctx, snapshot.Target)
	if err != nil {
		return err
	}
	if actual.digest() != snapshot.digest() {
		return fmt.Errorf("desktop mutation failed fresh-read verification")
	}
	return nil
}
func commitDesktopAction(ctx context.Context, prepared Action, action JournalAction, touch func(), afterMutation func() error) error {
	desired, err := journalDesktopSnapshot(action, action.Desired)
	if err != nil {
		return err
	}
	if prepared.DesktopTarget == nil || *prepared.DesktopTarget != desired.Target || prepared.DesktopValue == nil || !prepared.DesktopValue.Equal(desired.Value) {
		return fmt.Errorf("prepared desktop desired state differs from journal")
	}
	if _, err := classifyDesktopState(ctx, action); err != nil {
		return err
	}
	if touch != nil {
		touch()
	}
	if err := writeDesktopState(ctx, action, action.Desired); err != nil {
		return err
	}
	return runAfterTransactionMutation(afterMutation)
}
func rollbackDesktopAction(ctx context.Context, action JournalAction) error {
	if err := validateHostIO(ctx, action.Prior.BackupPath); err != nil {
		return err
	}
	data, _, err := safepath.ReadRegularFileBounded(action.Prior.BackupPath, maxDesktopSnapshotBytes)
	if err != nil {
		return err
	}
	if err := verifyDesktopBackup(action, data); err != nil {
		return err
	}
	state, err := classifyDesktopState(ctx, action)
	if err != nil {
		return err
	}
	if state == rollbackStatePrior {
		return nil
	}
	return writeDesktopState(ctx, action, action.Prior)
}
