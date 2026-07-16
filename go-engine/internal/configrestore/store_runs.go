// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// StoreMemberKind distinguishes an engine-owned generation transaction from
// a registered legacy journal in one restore run.
type StoreMemberKind string

const (
	StoreMemberGeneration StoreMemberKind = "generation"
	StoreMemberLegacy     StoreMemberKind = "legacy"
)

// ErrStoreMemberReverted reports that a generation member has already been
// durably consumed and must not mutate its targets again.
var ErrStoreMemberReverted = errors.New("config restore store member already reverted")

// StoreMember is an opaque, read-only capability returned by ActiveStoreRuns.
type StoreMember struct {
	kind         StoreMemberKind
	ordinal      uint64
	captureID    string
	legacyPath   string
	storeRoot    string
	restoreRun   string
	memberID     string
	sourceDigest string
}

func (m *StoreMember) Kind() StoreMemberKind {
	if m == nil {
		return ""
	}
	return m.kind
}
func (m *StoreMember) Ordinal() uint64 {
	if m == nil {
		return 0
	}
	return m.ordinal
}
func (m *StoreMember) CaptureID() string {
	if m == nil {
		return ""
	}
	return m.captureID
}
func (m *StoreMember) LegacyJournalPath() string {
	if m == nil {
		return ""
	}
	return m.legacyPath
}

// StoreRun groups active members produced beneath one BeginLive restore run.
type StoreRun struct {
	id      string
	runID   string
	started time.Time
	members []*StoreMember
}

func (r *StoreRun) ID() string {
	if r == nil {
		return ""
	}
	return r.id
}
func (r *StoreRun) RunID() string {
	if r == nil {
		return ""
	}
	return r.runID
}
func (r *StoreRun) StartedAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.started
}
func (r *StoreRun) Members() []*StoreMember {
	if r == nil {
		return nil
	}
	result := make([]*StoreMember, len(r.members))
	for index, member := range r.members {
		clone := *member
		result[index] = &clone
	}
	return result
}

// GenerationRevertAction reports one concrete action restored to its prior
// state. Actions are ordered exactly as the reverse execution occurred.
type GenerationRevertAction struct {
	Index      int
	Kind       ActionKind
	Target     string
	BackupUsed bool
}

// GenerationRevertResult reports the concrete actions restored for one member.
type GenerationRevertResult struct {
	ActionCount int
	Actions     []GenerationRevertAction
}

// RegisterLegacyJournal binds an existing legacy journal to the current
// restore run at the next mutation ordinal.
func (g *Guard) RegisterLegacyJournal(path string) (*StoreMember, error) {
	if g == nil {
		return nil, fmt.Errorf("live config restore guard is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.requireLeaseLocked(); err != nil {
		return nil, err
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("legacy journal path must be a clean absolute path")
	}
	if err := rejectExistingTargetLinks(path); err != nil {
		return nil, fmt.Errorf("validate legacy journal path: %w", err)
	}
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return nil, fmt.Errorf("read legacy journal: %w", err)
	}
	journalHash := sha256.Sum256(data)
	journalDigest := hex.EncodeToString(journalHash[:])
	memberID, err := newOpaqueStoreID()
	if err != nil {
		return nil, fmt.Errorf("create legacy member identity: %w", err)
	}
	disk, encoded, err := newLegacyMember(
		memberID, g.restoreRunID, g.runID, g.runStartedAt, g.nextOrdinal, path, journalDigest,
	)
	if err != nil {
		return nil, err
	}
	if err := publishImmutableStoreRecord(g.legacyMembers, memberID+".json", encoded); err != nil {
		return nil, fmt.Errorf("publish legacy member: %w", err)
	}
	g.nextOrdinal++
	return legacyHandle(g.storeRoot, disk), nil
}

// ActiveStoreRuns returns newest-first runs whose members have not been
// reverted. Members within each run are ordered by ascending mutation ordinal.
func (g *Guard) ActiveStoreRuns(ctx context.Context) ([]*StoreRun, error) {
	if g == nil {
		return nil, fmt.Errorf("live config restore guard is nil")
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.requireLeaseLocked(); err != nil {
		return nil, err
	}
	return g.activeStoreRunsLocked(ctx)
}

// RevertGenerationMember restores one committed generation transaction to its
// exact prior state and only then publishes its immutable consumption record.
func (g *Guard) RevertGenerationMember(ctx context.Context, member *StoreMember) (*GenerationRevertResult, error) {
	if g == nil {
		return nil, fmt.Errorf("live config restore guard is nil")
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.requireLeaseLocked(); err != nil {
		return nil, err
	}
	intent, err := g.loadActiveGenerationMember(ctx, member)
	if err != nil {
		return nil, err
	}
	actions := intent.Actions()
	if err := preflightGenerationRevert(ctx, actions, g.registry); err != nil {
		return nil, err
	}
	result := &GenerationRevertResult{Actions: make([]GenerationRevertAction, 0, len(actions))}
	workContext := context.WithoutCancel(ctx)
	var revertErrors []error
	for index := len(actions) - 1; index >= 0; index-- {
		action := actions[index]
		if err := rollbackTransactionAction(workContext, action, g.registry); err != nil {
			revertErrors = append(revertErrors, fmt.Errorf("revert action[%d]: %w", index, err))
			continue
		}
		result.Actions = append(result.Actions, GenerationRevertAction{
			Index: action.Index, Kind: action.Kind, Target: generationActionTarget(action),
			BackupUsed: action.Prior.Kind != StateAbsent,
		})
	}
	if err := verifyAllTransactionStates(workContext, actions, g.registry, false); err != nil {
		revertErrors = append(revertErrors, fmt.Errorf("verify complete generation revert: %w", err))
	}
	if err := errors.Join(revertErrors...); err != nil {
		return result, err
	}
	_, encoded, err := newMemberRevert(StoreMemberGeneration, member.memberID, member.sourceDigest)
	if err != nil {
		return result, err
	}
	root := filepath.Join(g.transactions, member.memberID)
	if err := publishImmutableStoreRecord(root, "reverted.json", encoded); err != nil {
		return result, fmt.Errorf("publish generation revert record: %w", err)
	}
	result.ActionCount = len(result.Actions)
	return result, nil
}

// MarkLegacyMemberReverted durably consumes a registered legacy member after
// the command layer has successfully reverted its verified journal.
func (g *Guard) MarkLegacyMemberReverted(ctx context.Context, member *StoreMember) error {
	if g == nil {
		return fmt.Errorf("live config restore guard is nil")
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.requireLeaseLocked(); err != nil {
		return err
	}
	disk, err := g.loadLegacyMember(member)
	if err != nil {
		return err
	}
	revertPath := filepath.Join(g.legacyReverts, disk.MemberID+".json")
	if reverted, err := memberReverted(revertPath, StoreMemberLegacy, disk.MemberID, disk.MemberDigest); err != nil {
		return err
	} else if reverted {
		return nil
	}
	journalData, _, err := safepath.ReadRegularFile(disk.JournalPath)
	if err != nil {
		return fmt.Errorf("read registered legacy journal: %w", err)
	}
	journalHash := sha256.Sum256(journalData)
	if hex.EncodeToString(journalHash[:]) != disk.JournalDigest {
		return fmt.Errorf("registered legacy journal changed")
	}
	_, encoded, err := newMemberRevert(StoreMemberLegacy, disk.MemberID, disk.MemberDigest)
	if err != nil {
		return err
	}
	if err := publishImmutableStoreRecord(g.legacyReverts, disk.MemberID+".json", encoded); err != nil {
		return fmt.Errorf("publish legacy revert record: %w", err)
	}
	return nil
}

func (g *Guard) requireLeaseLocked() error {
	if g.closed || g.lock == nil || !g.lock.Locked() {
		return fmt.Errorf("live config restore guard is closed")
	}
	return nil
}

func legacyHandle(storeRoot string, disk legacyMemberDisk) *StoreMember {
	return &StoreMember{
		kind: StoreMemberLegacy, ordinal: disk.MutationOrdinal, legacyPath: disk.JournalPath,
		storeRoot: storeRoot, restoreRun: disk.RestoreRunID, memberID: disk.MemberID, sourceDigest: disk.MemberDigest,
	}
}

func publishImmutableStoreRecord(directory, name string, data []byte) error {
	publishErr := publishStoreRecord(directory, name, data)
	if publishErr == nil {
		return nil
	}
	path := filepath.Join(directory, name)
	existing, _, readErr := safepath.ReadRegularFile(path)
	if readErr != nil || !bytes.Equal(existing, data) {
		return errors.Join(publishErr, readErr)
	}
	if syncErr := syncDurableFile(path); syncErr != nil {
		return errors.Join(publishErr, syncErr)
	}
	if syncErr := syncDurableDirectory(directory); syncErr != nil {
		return errors.Join(publishErr, syncErr)
	}
	existing, _, readErr = safepath.ReadRegularFile(path)
	if readErr != nil || !bytes.Equal(existing, data) {
		return errors.Join(publishErr, readErr, fmt.Errorf("published store record changed during reconciliation"))
	}
	return nil
}

func (g *Guard) activeStoreRunsLocked(ctx context.Context) ([]*StoreRun, error) {
	runs := make(map[string]*StoreRun)
	ordinals := make(map[string]string)
	add := func(restoreRunID, runID string, started time.Time, member *StoreMember) error {
		run := runs[restoreRunID]
		if run == nil {
			run = &StoreRun{id: restoreRunID, runID: runID, started: started, members: []*StoreMember{}}
			runs[restoreRunID] = run
		} else if run.runID != runID || !run.started.Equal(started) {
			return fmt.Errorf("restore run %q has conflicting identity metadata", restoreRunID)
		}
		ordinalKey := fmt.Sprintf("%s/%020d", restoreRunID, member.ordinal)
		if existing, ok := ordinals[ordinalKey]; ok {
			return fmt.Errorf("restore run ordinal duplicates member %q", existing)
		}
		ordinals[ordinalKey] = member.memberID
		run.members = append(run.members, member)
		return nil
	}

	transactionEntries, err := os.ReadDir(g.transactions)
	if err != nil {
		return nil, fmt.Errorf("read generation transaction store: %w", err)
	}
	for _, entry := range transactionEntries {
		if err := checkSnapshotContext(ctx); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !isOpaqueStoreID(entry.Name()) {
			return nil, fmt.Errorf("unexpected generation transaction entry %q", entry.Name())
		}
		root := filepath.Join(g.transactions, entry.Name())
		intentPath := filepath.Join(root, "journal", "intent.json")
		if _, err := os.Lstat(intentPath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		descriptor, started, err := readStoredTransactionDescriptor(root)
		if err != nil {
			return nil, fmt.Errorf("read transaction %q descriptor: %w", entry.Name(), err)
		}
		intent, err := readJournalIntentMetadataFile(ctx, root, intentPath)
		if err != nil {
			return nil, fmt.Errorf("read transaction %q intent: %w", entry.Name(), err)
		}
		lineage := intent.Lineage()
		if descriptor.RunID != lineage.RunID || descriptor.CaptureID != lineage.CaptureID {
			return nil, fmt.Errorf("transaction %q descriptor differs from journal lineage", entry.Name())
		}
		terminalPath := journalMarkerPath(filepath.Join(root, "journal"), JournalCommitted, intent.Digest())
		terminal, err := readJournalMarkerFile(root, terminalPath, intent)
		if err != nil {
			return nil, fmt.Errorf("read transaction %q terminal: %w", entry.Name(), err)
		}
		if terminal.State() == JournalRolledBack {
			continue
		}
		if terminal.State() != JournalCommitted {
			return nil, fmt.Errorf("transaction %q has unsupported terminal state %q", entry.Name(), terminal.State())
		}
		reverted, err := memberReverted(
			filepath.Join(root, "reverted.json"), StoreMemberGeneration, descriptor.TransactionID, terminal.Digest(),
		)
		if err != nil {
			return nil, fmt.Errorf("read transaction %q revert record: %w", entry.Name(), err)
		}
		if reverted {
			continue
		}
		member := &StoreMember{
			kind: StoreMemberGeneration, ordinal: descriptor.MutationOrdinal, captureID: descriptor.CaptureID,
			storeRoot: g.storeRoot, restoreRun: descriptor.RestoreRunID,
			memberID: descriptor.TransactionID, sourceDigest: terminal.Digest(),
		}
		if err := add(descriptor.RestoreRunID, descriptor.RunID, started, member); err != nil {
			return nil, err
		}
	}

	legacyEntries, err := os.ReadDir(g.legacyMembers)
	if err != nil {
		return nil, fmt.Errorf("read registered legacy member store: %w", err)
	}
	for _, entry := range legacyEntries {
		if err := checkSnapshotContext(ctx); err != nil {
			return nil, err
		}
		name := entry.Name()
		memberID := name
		if filepath.Ext(name) == ".json" {
			memberID = name[:len(name)-len(".json")]
		}
		if entry.IsDir() || !isOpaqueStoreID(memberID) || name != memberID+".json" {
			return nil, fmt.Errorf("unexpected registered legacy entry %q", name)
		}
		data, _, err := safepath.ReadRegularFile(filepath.Join(g.legacyMembers, name))
		if err != nil {
			return nil, err
		}
		disk, started, err := decodeLegacyMember(data)
		if err != nil {
			return nil, fmt.Errorf("read registered legacy member %q: %w", memberID, err)
		}
		if disk.MemberID != memberID {
			return nil, fmt.Errorf("registered legacy member %q path identity differs", memberID)
		}
		reverted, err := memberReverted(
			filepath.Join(g.legacyReverts, memberID+".json"), StoreMemberLegacy, memberID, disk.MemberDigest,
		)
		if err != nil {
			return nil, fmt.Errorf("read registered legacy revert %q: %w", memberID, err)
		}
		if reverted {
			continue
		}
		journalData, _, err := safepath.ReadRegularFile(disk.JournalPath)
		if err != nil {
			return nil, fmt.Errorf("read registered legacy journal %q: %w", disk.JournalPath, err)
		}
		journalHash := sha256.Sum256(journalData)
		if hex.EncodeToString(journalHash[:]) != disk.JournalDigest {
			return nil, fmt.Errorf("registered legacy journal %q changed", disk.JournalPath)
		}
		if err := add(disk.RestoreRunID, disk.RunID, started, legacyHandle(g.storeRoot, disk)); err != nil {
			return nil, err
		}
	}

	result := make([]*StoreRun, 0, len(runs))
	for _, run := range runs {
		sort.Slice(run.members, func(left, right int) bool {
			return run.members[left].ordinal < run.members[right].ordinal
		})
		result = append(result, run)
	}
	sort.Slice(result, func(left, right int) bool {
		if !result[left].started.Equal(result[right].started) {
			return result[left].started.After(result[right].started)
		}
		return result[left].id > result[right].id
	})
	return result, nil
}

func memberReverted(path string, kind StoreMemberKind, memberID, sourceDigest string) (bool, error) {
	data, _, err := safepath.ReadRegularFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	disk, _, err := decodeMemberRevert(data)
	if err != nil {
		return false, err
	}
	if disk.Kind != kind || disk.MemberID != memberID || disk.SourceDigest != sourceDigest {
		return false, fmt.Errorf("member revert record identifies a different member")
	}
	return true, nil
}

func (g *Guard) loadActiveGenerationMember(ctx context.Context, member *StoreMember) (*JournalIntent, error) {
	if member == nil || member.kind != StoreMemberGeneration || member.storeRoot != g.storeRoot ||
		!isOpaqueStoreID(member.memberID) || !isOpaqueStoreID(member.restoreRun) || !isLowerHexDigest(member.sourceDigest) {
		return nil, fmt.Errorf("valid generation store member is required")
	}
	root := filepath.Join(g.transactions, member.memberID)
	descriptor, _, err := readStoredTransactionDescriptor(root)
	if err != nil {
		return nil, err
	}
	intentPath := filepath.Join(root, "journal", "intent.json")
	metadata, err := readJournalIntentMetadataFile(ctx, root, intentPath)
	if err != nil {
		return nil, err
	}
	lineage := metadata.Lineage()
	if descriptor.RunID != lineage.RunID || descriptor.CaptureID != lineage.CaptureID ||
		descriptor.TransactionID != member.memberID || descriptor.RestoreRunID != member.restoreRun ||
		descriptor.MutationOrdinal != member.ordinal || descriptor.CaptureID != member.captureID {
		return nil, fmt.Errorf("generation member differs from its immutable transaction")
	}
	terminalPath := journalMarkerPath(filepath.Join(root, "journal"), JournalCommitted, metadata.Digest())
	terminal, err := readJournalMarkerFile(root, terminalPath, metadata)
	if err != nil {
		return nil, err
	}
	if terminal.State() != JournalCommitted || terminal.Digest() != member.sourceDigest {
		return nil, fmt.Errorf("generation member is not the expected committed transaction")
	}
	if reverted, err := memberReverted(
		filepath.Join(root, "reverted.json"), StoreMemberGeneration, member.memberID, member.sourceDigest,
	); err != nil {
		return nil, err
	} else if reverted {
		return nil, ErrStoreMemberReverted
	}
	return ReadJournalIntent(ctx, root)
}

func (g *Guard) loadLegacyMember(member *StoreMember) (legacyMemberDisk, error) {
	if member == nil || member.kind != StoreMemberLegacy || member.storeRoot != g.storeRoot ||
		!isOpaqueStoreID(member.memberID) || !isOpaqueStoreID(member.restoreRun) || !isLowerHexDigest(member.sourceDigest) {
		return legacyMemberDisk{}, fmt.Errorf("valid legacy store member is required")
	}
	path := filepath.Join(g.legacyMembers, member.memberID+".json")
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return legacyMemberDisk{}, err
	}
	disk, _, err := decodeLegacyMember(data)
	if err != nil {
		return legacyMemberDisk{}, err
	}
	if disk.MemberID != member.memberID || disk.RestoreRunID != member.restoreRun ||
		disk.MutationOrdinal != member.ordinal || disk.MemberDigest != member.sourceDigest ||
		disk.JournalPath != member.legacyPath {
		return legacyMemberDisk{}, fmt.Errorf("legacy member differs from its immutable record")
	}
	return disk, nil
}

func generationActionTarget(action JournalAction) string {
	if action.Kind != ActionRegistrySet {
		return action.Target
	}
	return action.RegistryKey + "::" + action.RegistryValueName
}

func preflightGenerationRevert(ctx context.Context, actions []JournalAction, registry RegistryMutator) error {
	for index := len(actions) - 1; index >= 0; index-- {
		action := actions[index]
		var err error
		if action.Kind == ActionRegistrySet {
			_, err = classifyRegistryRollbackState(ctx, action, registry)
		} else {
			_, _, err = classifyFilesystemRollbackState(ctx, action)
		}
		if err != nil {
			return fmt.Errorf("preflight revert action[%d]: %w", index, err)
		}
	}
	return nil
}
