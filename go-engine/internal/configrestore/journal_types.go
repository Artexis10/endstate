// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import "context"

// JournalState is the closed durable transaction-state vocabulary. An
// incomplete rollback leaves the state pending for recovery.
type JournalState string

const (
	JournalPending    JournalState = "pending"
	JournalCommitted  JournalState = "committed"
	JournalRolledBack JournalState = "rolled_back"
)

// ValidationStatus records the final-target validation position associated
// with an immutable journal record.
type ValidationStatus string

const (
	ValidationPending ValidationStatus = "pending"
	ValidationNotRun  ValidationStatus = "not_run"
	ValidationPassed  ValidationStatus = "passed"
	ValidationFailed  ValidationStatus = "failed"
)

// RollbackOutcome records whether rollback was needed and proven. Failed
// rollback has no terminal marker and therefore leaves the intent pending.
type RollbackOutcome string

const (
	RollbackNotAttempted RollbackOutcome = "not_attempted"
	RollbackNotRequired  RollbackOutcome = "not_required"
	RollbackSucceeded    RollbackOutcome = "succeeded"
)

// JournalLineage binds one transaction to immutable capture and trusted
// restore-catalog identities.
type JournalLineage struct {
	RunID                       string   `json:"runId"`
	CaptureID                   string   `json:"captureId"`
	ModuleID                    string   `json:"moduleId"`
	ConfigSetID                 string   `json:"configSetId"`
	TargetInstanceID            string   `json:"targetInstanceId"`
	SourceGeneration            string   `json:"sourceGeneration"`
	TargetGeneration            string   `json:"targetGeneration"`
	MigrationPath               []string `json:"migrationPath"`
	SourceGenerationFingerprint string   `json:"sourceGenerationFingerprint"`
	CaptureModuleRevision       string   `json:"captureModuleRevision"`
	RestoreModuleRevision       string   `json:"restoreModuleRevision"`
}

// JournalActionState is the canonical prior or desired state recorded for an
// action. Mode contains portable permission bits as an unsigned integer.
type JournalActionState struct {
	Kind       StateKind                `json:"kind"`
	Digest     string                   `json:"digest"`
	Mode       uint32                   `json:"mode"`
	BackupPath string                   `json:"backupPath"`
	Entries    []JournalFilesystemEntry `json:"entries"`
}

// JournalFilesystemEntry is a stable, per-entry filesystem identity used by
// recovery to distinguish prior, desired, and partially applied tree states.
type JournalFilesystemEntry struct {
	Path        string    `json:"path"`
	Kind        StateKind `json:"kind"`
	Mode        uint32    `json:"mode"`
	Size        int64     `json:"size"`
	ContentHash string    `json:"contentHash"`
}

// JournalAction is one ordered concrete transaction action without staged
// bytes or a mutable source path.
type JournalAction struct {
	Index             int                `json:"index"`
	Kind              ActionKind         `json:"kind"`
	Strategy          string             `json:"strategy"`
	Target            string             `json:"target"`
	RegistryKey       string             `json:"registryKey"`
	RegistryValueName string             `json:"registryValueName"`
	MissingParents    []string           `json:"missingParents"`
	Prior             JournalActionState `json:"prior"`
	Desired           JournalActionState `json:"desired"`
	SourceDigest      string             `json:"sourceDigest"`
}

// JournalValidation is one resolved final-target validation in declaration
// order. Empty primitive-specific fields remain explicit in canonical JSON.
type JournalValidation struct {
	Type     string `json:"type"`
	Path     string `json:"path"`
	JSONPath string `json:"jsonPath"`
	Section  string `json:"section"`
	Key      string `json:"key"`
	HostPath string `json:"hostPath"`
}

// JournalIntentRequest persists one already-verified PreparedSet beneath the
// same caller-owned transaction root used for its snapshots.
type JournalIntentRequest struct {
	Prepared        *PreparedSet
	TransactionRoot string
	Lineage         JournalLineage
}

// JournalIntent is an immutable, verified view of a pending intent. The
// transaction root is private so terminal writers cannot be redirected.
type JournalIntent struct {
	transactionRoot string
	path            string
	digest          string
	lineage         JournalLineage
	actions         []JournalAction
	validations     []JournalValidation
}

type journalPhase string

const (
	journalPhaseBeforeSnapshotFileSync      journalPhase = "before_snapshot_file_sync"
	journalPhaseBeforeSnapshotDirectorySync journalPhase = "before_snapshot_directory_sync"
	journalPhaseBeforeTransactionRootSync   journalPhase = "before_transaction_root_sync"
	journalPhaseBeforeIntentPublish         journalPhase = "before_intent_publish"
	journalPhaseAfterIntentPublish          journalPhase = "after_intent_publish"
	journalPhaseBeforeMarkerPublish         journalPhase = "before_marker_publish"
)

type journalCheckpointFunc func(context.Context, journalPhase, string) error

// JournalWriter owns private durability checkpoints used by failure tests.
type JournalWriter struct {
	checkpoint    journalCheckpointFunc
	publish       func(string, string) (publicationState, error)
	syncFile      func(string) error
	syncDirectory func(string) error
}

func NewJournalWriter() *JournalWriter { return &JournalWriter{} }

func (w *JournalWriter) publishNoReplace(temporary, destination string) (publicationState, error) {
	if w != nil && w.publish != nil {
		return w.publish(temporary, destination)
	}
	return publishFileNoReplace(temporary, destination)
}

func (w *JournalWriter) syncReconciledFile(path string) error {
	if w != nil && w.syncFile != nil {
		return w.syncFile(path)
	}
	return syncDurableFile(path)
}

func (w *JournalWriter) syncReconciledDirectory(path string) error {
	if w != nil && w.syncDirectory != nil {
		return w.syncDirectory(path)
	}
	return syncDurableDirectory(path)
}

func (i *JournalIntent) Path() string {
	if i == nil {
		return ""
	}
	return i.path
}

func (i *JournalIntent) Digest() string {
	if i == nil {
		return ""
	}
	return i.digest
}

func (i *JournalIntent) State() JournalState {
	if i == nil {
		return ""
	}
	return JournalPending
}

func (i *JournalIntent) Lineage() JournalLineage {
	if i == nil {
		return JournalLineage{}
	}
	return cloneJournalLineage(i.lineage)
}

func (i *JournalIntent) Actions() []JournalAction {
	if i == nil {
		return nil
	}
	return cloneJournalActions(i.actions)
}

func (i *JournalIntent) Validations() []JournalValidation {
	if i == nil {
		return nil
	}
	return append([]JournalValidation(nil), i.validations...)
}

func cloneJournalLineage(lineage JournalLineage) JournalLineage {
	lineage.MigrationPath = append([]string{}, lineage.MigrationPath...)
	return lineage
}

func cloneJournalActions(actions []JournalAction) []JournalAction {
	result := make([]JournalAction, len(actions))
	for index, action := range actions {
		result[index] = action
		result[index].MissingParents = append([]string{}, action.MissingParents...)
		result[index].Prior.Entries = append([]JournalFilesystemEntry{}, action.Prior.Entries...)
		result[index].Desired.Entries = append([]JournalFilesystemEntry{}, action.Desired.Entries...)
	}
	return result
}

// JournalMarker is an immutable, verified terminal marker bound to its
// verified pending intent and private transaction root.
type JournalMarker struct {
	transactionRoot  string
	path             string
	digest           string
	intentDigest     string
	state            JournalState
	validationStatus ValidationStatus
	rollbackOutcome  RollbackOutcome
	intent           *JournalIntent
}

func (m *JournalMarker) Path() string {
	if m == nil {
		return ""
	}
	return m.path
}

func (m *JournalMarker) Digest() string {
	if m == nil {
		return ""
	}
	return m.digest
}

func (m *JournalMarker) IntentDigest() string {
	if m == nil {
		return ""
	}
	return m.intentDigest
}

func (m *JournalMarker) State() JournalState {
	if m == nil {
		return ""
	}
	return m.state
}

func (m *JournalMarker) ValidationStatus() ValidationStatus {
	if m == nil {
		return ""
	}
	return m.validationStatus
}

func (m *JournalMarker) RollbackOutcome() RollbackOutcome {
	if m == nil {
		return ""
	}
	return m.rollbackOutcome
}

func (m *JournalMarker) Lineage() JournalLineage {
	if m == nil || m.intent == nil {
		return JournalLineage{}
	}
	return m.intent.Lineage()
}

func (m *JournalMarker) Actions() []JournalAction {
	if m == nil || m.intent == nil {
		return nil
	}
	return m.intent.Actions()
}

func (m *JournalMarker) Validations() []JournalValidation {
	if m == nil || m.intent == nil {
		return nil
	}
	return m.intent.Validations()
}

func cloneJournalIntent(intent *JournalIntent) *JournalIntent {
	if intent == nil {
		return nil
	}
	return &JournalIntent{
		transactionRoot: intent.transactionRoot, path: intent.path, digest: intent.digest,
		lineage: cloneJournalLineage(intent.lineage), actions: cloneJournalActions(intent.actions),
		validations: append([]JournalValidation{}, intent.validations...),
	}
}
