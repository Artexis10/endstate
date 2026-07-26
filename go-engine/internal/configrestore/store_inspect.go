// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxStoreInspectionEntries = 4096
	maxStoreInspectionDepth   = 12
	maxStoreInspectionFile    = 8 << 20
	maxStoreInspectionBytes   = 64 << 20
)

// StoreInspection is an immutable, value-only view of one closed config
// restore store. Callers must hold an exclusive writer lease for the whole
// inspection; the inspector never opens, creates, or acquires mutation.lock.
type StoreInspection struct {
	transactionCount int
	memberCount      int
	runs             []StoreRunInspection
}

func (s StoreInspection) TransactionCount() int { return s.transactionCount }
func (s StoreInspection) MemberCount() int      { return s.memberCount }
func (s StoreInspection) Runs() []StoreRunInspection {
	return cloneStoreRunInspections(s.runs)
}

// StoreRunInspection is one restore run reconstructed from immutable store
// records. It deliberately contains no store or host filesystem path.
type StoreRunInspection struct {
	ID           string
	RunID        string
	StartedAtUTC string
	members      []StoreMemberInspection
}

func (r StoreRunInspection) Members() []StoreMemberInspection {
	return cloneStoreMemberInspections(r.members)
}

// StoreMemberInspection binds a stored generation transaction or a registered
// legacy journal to its closed state. Legacy members do not invent lineage
// that their schema did not record.
type StoreMemberInspection struct {
	Kind                  StoreMemberKind
	ID                    string
	Ordinal               uint64
	CaptureID             string
	DescriptorDigest      string
	MemberDigest          string
	IntentDigest          string
	TerminalDigest        string
	TerminalState         JournalState
	ValidationStatus      ValidationStatus
	RollbackOutcome       RollbackOutcome
	Reverted              bool
	RevertDigest          string
	HasLineage            bool
	Lineage               StoreLineageInspection
	LegacyJournalIdentity string
	LegacyJournalDigest   string
}

// StoreLineageInspection contains only schema-recorded identity facts.
type StoreLineageInspection struct {
	RunID                       string
	CaptureID                   string
	ModuleID                    string
	ConfigSetID                 string
	TargetInstanceID            string
	SourceGeneration            string
	TargetGeneration            string
	SourceGenerationFingerprint string
	CaptureModuleRevision       string
	RestoreModuleRevision       string
	migrationPath               []string
}

func (l StoreLineageInspection) MigrationPath() []string {
	return append([]string(nil), l.migrationPath...)
}

// storeInspectionAfterFirstScan is a test seam for proving that inspection
// fails closed when any byte or metadata changes between scans.
var storeInspectionAfterFirstScan func()

// InspectStore validates the existing config-restore/v1 store without writing
// to it. exclusiveLease is an explicit caller precondition: a concurrent
// writer makes the evidence invalid, and passing false is always rejected.
func InspectStore(root string, exclusiveLease bool) (*StoreInspection, error) {
	if !exclusiveLease {
		return nil, fmt.Errorf("config restore inspection requires an exclusive no-concurrent-writer lease")
	}
	if err := validateInspectionRoot(root); err != nil {
		return nil, err
	}
	before, err := scanStoreInspectionTree(root)
	if err != nil {
		return nil, err
	}
	if storeInspectionAfterFirstScan != nil {
		storeInspectionAfterFirstScan()
	}
	inspection, err := inspectClosedStore(root)
	if err != nil {
		return nil, err
	}
	after, err := scanStoreInspectionTree(root)
	if err != nil {
		return nil, err
	}
	if !before.equal(after) {
		return nil, fmt.Errorf("config restore store changed during read-only inspection")
	}
	return inspection, nil
}

func validateInspectionRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root ||
		filepath.Base(root) != "v1" || filepath.Base(filepath.Dir(root)) != "config-restore" {
		return fmt.Errorf("config restore inspector requires an existing clean config-restore/v1 root")
	}
	if err := rejectExistingTargetLinks(root); err != nil {
		return fmt.Errorf("inspect config restore root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return fmt.Errorf("config restore inspector requires an existing safe store root")
	}
	return nil
}

type storeInspectionTree struct {
	entries map[string]storeInspectionTreeEntry
}
type storeInspectionTreeEntry struct {
	mode       os.FileMode
	size       int64
	modifiedNS int64
	digest     string
}

func (tree storeInspectionTree) equal(other storeInspectionTree) bool {
	if len(tree.entries) != len(other.entries) {
		return false
	}
	for path, entry := range tree.entries {
		if other.entries[path] != entry {
			return false
		}
	}
	return true
}

func scanStoreInspectionTree(root string) (storeInspectionTree, error) {
	tree := storeInspectionTree{entries: make(map[string]storeInspectionTreeEntry)}
	var total int64
	var visit func(string, int) error
	visit = func(current string, depth int) error {
		if depth > maxStoreInspectionDepth {
			return fmt.Errorf("config restore store exceeds inspection depth limit")
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("config restore store contains link or reparse point")
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("config restore store entry escapes inspection root")
		}
		portable := filepath.ToSlash(relative)
		record := storeInspectionTreeEntry{mode: info.Mode(), size: info.Size(), modifiedNS: info.ModTime().UnixNano()}
		switch {
		case info.IsDir():
			entries, err := os.ReadDir(current)
			if err != nil {
				return err
			}
			tree.entries[portable] = record
			if len(tree.entries) > maxStoreInspectionEntries {
				return fmt.Errorf("config restore store exceeds inspection entry limit")
			}
			for _, entry := range entries {
				if entry.Name() == "." || entry.Name() == ".." || filepath.Base(entry.Name()) != entry.Name() {
					return fmt.Errorf("config restore store contains unsafe entry name")
				}
				if err := visit(filepath.Join(current, entry.Name()), depth+1); err != nil {
					return err
				}
			}
		case info.Mode().IsRegular():
			if info.Size() < 0 || info.Size() > maxStoreInspectionFile || total > maxStoreInspectionBytes-info.Size() {
				return fmt.Errorf("config restore store exceeds inspection byte limit")
			}
			file, err := os.Open(current)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, copyErr := io.Copy(hash, io.LimitReader(file, maxStoreInspectionFile+1))
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				return fmt.Errorf("read config restore store record: %w", firstInspectionError(copyErr, closeErr))
			}
			after, err := os.Lstat(current)
			if err != nil || after.Mode() != info.Mode() || after.Size() != info.Size() || after.ModTime() != info.ModTime() || isLinkOrReparse(after) {
				return fmt.Errorf("config restore store record changed while scanning")
			}
			record.digest = hex.EncodeToString(hash.Sum(nil))
			total += info.Size()
			tree.entries[portable] = record
			if len(tree.entries) > maxStoreInspectionEntries {
				return fmt.Errorf("config restore store exceeds inspection entry limit")
			}
		default:
			return fmt.Errorf("config restore store contains unsupported special file")
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return storeInspectionTree{}, err
	}
	return tree, nil
}

func firstInspectionError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func inspectClosedStore(root string) (*StoreInspection, error) {
	transactions := filepath.Join(root, "transactions")
	legacyMembers := filepath.Join(root, "legacy-members")
	legacyReverts := filepath.Join(root, "legacy-reverts")
	legacyWork := filepath.Join(root, "legacy-revert-work")
	if err := requireInspectionDirectory(root, "transactions", transactions); err != nil {
		return nil, err
	}
	if err := requireInspectionDirectory(root, "legacy-members", legacyMembers); err != nil {
		return nil, err
	}
	if err := requireInspectionDirectory(root, "legacy-reverts", legacyReverts); err != nil {
		return nil, err
	}
	if err := requireInspectionDirectory(root, "legacy-revert-work", legacyWork); err != nil {
		return nil, err
	}
	if err := requireEmptyInspectionDirectory(legacyWork); err != nil {
		return nil, fmt.Errorf("legacy revert work is incomplete or ambiguous: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	if len(entries) != 4 {
		return nil, fmt.Errorf("config restore store contains unknown root entries")
	}

	runs := map[string]*StoreRunInspection{}
	ordinals := map[string]string{}
	memberIDs := map[string]struct{}{}
	add := func(restoreRunID, runID, started string, member StoreMemberInspection) error {
		if _, exists := memberIDs[member.ID]; exists {
			return fmt.Errorf("config restore store duplicates member identity")
		}
		memberIDs[member.ID] = struct{}{}
		key := fmt.Sprintf("%s/%020d", restoreRunID, member.Ordinal)
		if existing, exists := ordinals[key]; exists {
			return fmt.Errorf("config restore store duplicates ordinal with member %q", existing)
		}
		ordinals[key] = member.ID
		run := runs[restoreRunID]
		if run == nil {
			run = &StoreRunInspection{ID: restoreRunID, RunID: runID, StartedAtUTC: started}
			runs[restoreRunID] = run
		} else if run.RunID != runID || run.StartedAtUTC != started {
			return fmt.Errorf("config restore store has conflicting restore run identity")
		}
		run.members = append(run.members, member)
		return nil
	}

	transactionCount, err := inspectGenerationTransactions(transactions, add)
	if err != nil {
		return nil, err
	}
	legacyCount, err := inspectLegacyMembers(legacyMembers, legacyReverts, add)
	if err != nil {
		return nil, err
	}
	result := &StoreInspection{transactionCount: transactionCount, memberCount: transactionCount + legacyCount}
	for _, run := range runs {
		sort.Slice(run.members, func(left, right int) bool { return run.members[left].Ordinal < run.members[right].Ordinal })
		result.runs = append(result.runs, *run)
	}
	sort.Slice(result.runs, func(left, right int) bool {
		if result.runs[left].StartedAtUTC != result.runs[right].StartedAtUTC {
			return result.runs[left].StartedAtUTC > result.runs[right].StartedAtUTC
		}
		return result.runs[left].ID > result.runs[right].ID
	})
	return result, nil
}

func requireInspectionDirectory(root, name, directory string) error {
	if filepath.Dir(directory) != root || filepath.Base(directory) != name {
		return fmt.Errorf("invalid inspection directory")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return fmt.Errorf("config restore store %q is not a safe directory", name)
	}
	return nil
}

func requireEmptyInspectionDirectory(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("directory is not empty")
	}
	return nil
}

func inspectGenerationTransactions(
	directory string,
	add func(string, string, string, StoreMemberInspection) error,
) (int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isOpaqueStoreID(entry.Name()) {
			return 0, fmt.Errorf("config restore store has unknown transaction entry %q", entry.Name())
		}
		root := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(root)
		if err != nil || isLinkOrReparse(info) {
			return 0, fmt.Errorf("config restore transaction %q is unsafe", entry.Name())
		}
		descriptor, started, err := readStoredTransactionDescriptor(root)
		if err != nil {
			return 0, fmt.Errorf("inspect transaction %q descriptor: %w", entry.Name(), err)
		}
		intent, err := ReadJournalIntent(context.Background(), root)
		if err != nil {
			return 0, fmt.Errorf("inspect transaction %q intent: %w", entry.Name(), err)
		}
		lineage := intent.Lineage()
		if descriptor.RunID != lineage.RunID || descriptor.CaptureID != lineage.CaptureID || descriptor.RestoreRunID == "" {
			return 0, fmt.Errorf("config restore transaction %q descriptor differs from intent lineage", entry.Name())
		}
		terminalPath := journalMarkerPath(filepath.Join(root, "journal"), JournalCommitted, intent.Digest())
		terminal, err := readJournalMarkerFile(root, terminalPath, intent)
		if err != nil {
			return 0, fmt.Errorf("config restore transaction %q is pending or has invalid terminal marker: %w", entry.Name(), err)
		}
		if terminal.State() != JournalCommitted {
			return 0, fmt.Errorf("config restore transaction %q is rolled back or ambiguous", entry.Name())
		}
		if err := requireCanonicalTransactionLayout(root, intent.Digest()); err != nil {
			return 0, fmt.Errorf("config restore transaction %q layout: %w", entry.Name(), err)
		}
		reverted, revertDigest, err := inspectMemberRevert(
			filepath.Join(root, "reverted.json"), StoreMemberGeneration, descriptor.TransactionID, terminal.Digest(),
		)
		if err != nil {
			return 0, fmt.Errorf("config restore transaction %q revert: %w", entry.Name(), err)
		}
		member := StoreMemberInspection{
			Kind: StoreMemberGeneration, ID: descriptor.TransactionID, Ordinal: descriptor.MutationOrdinal,
			CaptureID: descriptor.CaptureID, DescriptorDigest: descriptor.DescriptorDigest, MemberDigest: terminal.Digest(),
			IntentDigest: intent.Digest(), TerminalDigest: terminal.Digest(), TerminalState: terminal.State(),
			ValidationStatus: terminal.ValidationStatus(), RollbackOutcome: terminal.RollbackOutcome(),
			Reverted: reverted, RevertDigest: revertDigest, HasLineage: true,
			Lineage: inspectionLineage(lineage),
		}
		if err := add(descriptor.RestoreRunID, descriptor.RunID, started.UTC().Format(time.RFC3339Nano), member); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

func requireCanonicalTransactionLayout(root, intentDigest string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	expected := map[string]bool{"transaction.json": true, "journal": true, "snapshots": true, "reverted.json": true}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			return fmt.Errorf("unknown entry %q", entry.Name())
		}
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || isLinkOrReparse(info) {
			return fmt.Errorf("unsafe entry %q", entry.Name())
		}
		if (entry.Name() == "journal" || entry.Name() == "snapshots") != info.IsDir() {
			return fmt.Errorf("entry %q has wrong file type", entry.Name())
		}
	}
	if err := requireCanonicalJournalLayout(filepath.Join(root, "journal"), intentDigest); err != nil {
		return err
	}
	return nil
}

func requireCanonicalJournalLayout(directory, intentDigest string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	expectedTerminal := "terminal-" + intentDigest + ".json"
	if len(entries) != 2 {
		return fmt.Errorf("journal contains unexpected entries")
	}
	for _, entry := range entries {
		if entry.Name() != "intent.json" && entry.Name() != expectedTerminal {
			return fmt.Errorf("journal contains unknown entry %q", entry.Name())
		}
		info, err := os.Lstat(filepath.Join(directory, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || isLinkOrReparse(info) {
			return fmt.Errorf("journal entry %q is not a safe regular file", entry.Name())
		}
	}
	return nil
}

func inspectLegacyMembers(
	membersDirectory, revertsDirectory string,
	add func(string, string, string, StoreMemberInspection) error,
) (int, error) {
	entries, err := os.ReadDir(membersDirectory)
	if err != nil {
		return 0, err
	}
	memberByID := make(map[string]legacyMemberDisk, len(entries))
	journalIdentities := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id, ok := inspectionRecordID(entry.Name())
		if !ok || entry.IsDir() {
			return 0, fmt.Errorf("config restore store has unknown legacy member entry %q", entry.Name())
		}
		path := filepath.Join(membersDirectory, entry.Name())
		data, _, err := readInspectionRegularFile(path)
		if err != nil {
			return 0, err
		}
		disk, started, err := decodeLegacyMember(data)
		if err != nil || disk.MemberID != id {
			return 0, fmt.Errorf("config restore legacy member %q is invalid", id)
		}
		if !strings.HasPrefix(disk.JournalPath, "$ENDSTATE_ROOT/logs/") {
			return 0, fmt.Errorf("config restore legacy member %q has no portable journal identity", id)
		}
		identity := disk.JournalPath + "\x00" + disk.JournalDigest
		if _, duplicate := journalIdentities[identity]; duplicate {
			return 0, fmt.Errorf("config restore store duplicates registered legacy journal")
		}
		journalIdentities[identity] = struct{}{}
		memberByID[id] = disk
		reverted, revertDigest, err := inspectMemberRevert(
			filepath.Join(revertsDirectory, id+".json"), StoreMemberLegacy, id, disk.MemberDigest,
		)
		if err != nil {
			return 0, fmt.Errorf("config restore legacy member %q revert: %w", id, err)
		}
		member := StoreMemberInspection{
			Kind: StoreMemberLegacy, ID: id, Ordinal: disk.MutationOrdinal, MemberDigest: disk.MemberDigest,
			Reverted: reverted, RevertDigest: revertDigest,
			LegacyJournalIdentity: disk.JournalPath, LegacyJournalDigest: disk.JournalDigest,
		}
		if err := add(disk.RestoreRunID, disk.RunID, started.UTC().Format(time.RFC3339Nano), member); err != nil {
			return 0, err
		}
	}
	if err := requireCanonicalLegacyReverts(revertsDirectory, memberByID); err != nil {
		return 0, err
	}
	return len(entries), nil
}

func inspectionRecordID(name string) (string, bool) {
	if !strings.HasSuffix(name, ".json") {
		return "", false
	}
	id := strings.TrimSuffix(name, ".json")
	return id, isOpaqueStoreID(id)
}

func requireCanonicalLegacyReverts(directory string, members map[string]legacyMemberDisk) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id, ok := inspectionRecordID(entry.Name())
		if !ok || entry.IsDir() {
			return fmt.Errorf("config restore store has unknown legacy revert entry %q", entry.Name())
		}
		member, exists := members[id]
		if !exists {
			return fmt.Errorf("config restore store has orphan legacy revert %q", entry.Name())
		}
		if _, _, err := inspectMemberRevert(filepath.Join(directory, entry.Name()), StoreMemberLegacy, id, member.MemberDigest); err != nil {
			return err
		}
	}
	return nil
}

func inspectMemberRevert(path string, kind StoreMemberKind, id, sourceDigest string) (bool, string, error) {
	data, exists, err := readInspectionRegularFile(path)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil || !exists {
		return false, "", err
	}
	disk, _, err := decodeMemberRevert(data)
	if err != nil || disk.Kind != kind || disk.MemberID != id || disk.SourceDigest != sourceDigest {
		return false, "", fmt.Errorf("member revert record is invalid")
	}
	return true, disk.RevertDigest, nil
}

func readInspectionRegularFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, err
	}
	if err != nil || !info.Mode().IsRegular() || isLinkOrReparse(info) {
		return nil, false, fmt.Errorf("config restore record is not a safe regular file")
	}
	data, err := os.ReadFile(path)
	return data, true, err
}

func inspectionLineage(lineage JournalLineage) StoreLineageInspection {
	return StoreLineageInspection{
		RunID: lineage.RunID, CaptureID: lineage.CaptureID, ModuleID: lineage.ModuleID,
		ConfigSetID: lineage.ConfigSetID, TargetInstanceID: lineage.TargetInstanceID,
		SourceGeneration: lineage.SourceGeneration, TargetGeneration: lineage.TargetGeneration,
		SourceGenerationFingerprint: lineage.SourceGenerationFingerprint,
		CaptureModuleRevision:       lineage.CaptureModuleRevision, RestoreModuleRevision: lineage.RestoreModuleRevision,
		migrationPath: append([]string(nil), lineage.MigrationPath...),
	}
}

func cloneStoreRunInspections(runs []StoreRunInspection) []StoreRunInspection {
	result := make([]StoreRunInspection, len(runs))
	for index, run := range runs {
		result[index] = run
		result[index].members = cloneStoreMemberInspections(run.members)
	}
	return result
}

func cloneStoreMemberInspections(members []StoreMemberInspection) []StoreMemberInspection {
	result := make([]StoreMemberInspection, len(members))
	for index, member := range members {
		result[index] = member
		result[index].Lineage.migrationPath = append([]string(nil), member.Lineage.migrationPath...)
	}
	return result
}
