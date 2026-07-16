// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
	"os"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
)

// Registry value type numbers match the Windows registry API. Keeping the raw
// type and bytes allows rollback to reproduce values without lossy string
// conversion and keeps the read interface portable for tests.
const (
	RegistryTypeSZ       uint32 = 1
	RegistryTypeExpandSZ uint32 = 2
	RegistryTypeDWORD    uint32 = 4
)

// RegistryReadResult is an exact read-only snapshot of one named value.
type RegistryReadResult struct {
	Exists    bool
	ValueType uint32
	Data      []byte
}

// RegistryReader reads one HKCU named value. It intentionally exposes no
// registry mutation capability.
type RegistryReader interface {
	ReadValue(context.Context, string, string) (RegistryReadResult, error)
}

// RegistryReaderFunc adapts a function to RegistryReader.
type RegistryReaderFunc func(context.Context, string, string) (RegistryReadResult, error)

func (f RegistryReaderFunc) ReadValue(ctx context.Context, key, valueName string) (RegistryReadResult, error) {
	return f(ctx, key, valueName)
}

// SnapshotRequest identifies one materialized set and an existing,
// caller-owned transaction root beneath which snapshots are published.
type SnapshotRequest struct {
	Set             *MaterializedSet
	TransactionRoot string
	RegistryReader  RegistryReader
}

// StateKind is the canonical kind represented by a prior or desired digest.
type StateKind string

const (
	StateAbsent        StateKind = "absent"
	StateFile          StateKind = "file"
	StateDirectory     StateKind = "directory"
	StateRegistryValue StateKind = "registry-value"
)

// StateRecord is an immutable-by-value summary of one prior or desired state.
// BackupPath is empty when the prior filesystem target was absent.
type StateRecord struct {
	Kind       StateKind
	Digest     string
	Mode       os.FileMode
	BackupPath string
}

// PreparedAction is opaque so callers cannot mutate the action or state that
// the later journal and commit layers will consume.
type PreparedAction struct {
	action       Action
	prior        StateRecord
	desired      StateRecord
	sourceDigest string
}

func (a PreparedAction) Action() Action       { return cloneAction(a.action) }
func (a PreparedAction) Prior() StateRecord   { return a.prior }
func (a PreparedAction) Desired() StateRecord { return a.desired }
func (a PreparedAction) SourceDigest() string { return a.sourceDigest }

// PreparedSet is the only output accepted by later transaction phases. Its
// accessors return copies so the verified plan cannot be changed in place.
type PreparedSet struct {
	snapshotRoot string
	actions      []PreparedAction
	validations  []configvalidate.ResolvedValidation
}

func (s *PreparedSet) SnapshotRoot() string {
	if s == nil {
		return ""
	}
	return s.snapshotRoot
}

func (s *PreparedSet) Actions() []PreparedAction {
	if s == nil {
		return nil
	}
	result := make([]PreparedAction, len(s.actions))
	for index, action := range s.actions {
		result[index] = clonePreparedAction(action)
	}
	return result
}

func (s *PreparedSet) Validations() []configvalidate.ResolvedValidation {
	if s == nil {
		return nil
	}
	return append([]configvalidate.ResolvedValidation(nil), s.validations...)
}

type snapshotPhase string

const (
	phaseBeforeAction      snapshotPhase = "before_action"
	phaseAfterAction       snapshotPhase = "after_action"
	phaseBeforeFinalVerify snapshotPhase = "before_final_verify"
)

type snapshotCheckpointFunc func(context.Context, snapshotPhase, int) error

// SnapshotPreparer owns optional internal checkpoints used by failure tests.
type SnapshotPreparer struct {
	checkpoint snapshotCheckpointFunc
}

func NewSnapshotPreparer() *SnapshotPreparer { return &SnapshotPreparer{} }

func formatActionIndex(index int) string { return fmt.Sprintf("%06d", index) }

func cloneAction(action Action) Action {
	copy := action
	copy.Exclude = append([]string(nil), action.Exclude...)
	copy.DesiredContent = append([]byte(nil), action.DesiredContent...)
	if action.RegistryValue != nil {
		value := *action.RegistryValue
		copy.RegistryValue = &value
	}
	return copy
}

func clonePreparedAction(action PreparedAction) PreparedAction {
	action.action = cloneAction(action.action)
	return action
}
