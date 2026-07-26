// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

const (
	maxStoreInspectionEntries  = 4096
	maxStoreInspectionDepth    = 12
	maxStoreInspectionFile     = 8 << 20
	maxStoreInspectionBytes    = 64 << 20
	maxStoreInspectionJournals = 256
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
	actions               []StoreActionInspection
}

func (m StoreMemberInspection) Actions() []StoreActionInspection {
	return append([]StoreActionInspection(nil), m.actions...)
}

// StoreActionInspection records only the durable facts available in a
// generation journal. Filesystem identities are opaque digests, never paths.
type StoreActionInspection struct {
	Index          int
	Kind           ActionKind
	Status         StoreActionStatus
	Strategy       string
	SourceIdentity string
	SourceDigest   string
	TargetIdentity string
	PriorDigest    string
	DesiredDigest  string
	PriorKind      StateKind
	DesiredKind    StateKind
	Backup         StoreBackupInspection
}

// StoreActionStatus is a closed legacy restore outcome vocabulary.
type StoreActionStatus string

const (
	StoreActionStatusFailed               StoreActionStatus = "failed"
	StoreActionStatusInstalled            StoreActionStatus = "installed"
	StoreActionStatusRestored             StoreActionStatus = "restored"
	StoreActionStatusSkipped              StoreActionStatus = "skipped"
	StoreActionStatusSkippedMissingSource StoreActionStatus = "skipped_missing_source"
	StoreActionStatusSkippedUpToDate      StoreActionStatus = "skipped_up_to_date"
)

type StoreBackupInspection struct {
	Exists   bool
	Identity string
	Digest   string
	Kind     StateKind
	Mode     uint32
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

type storeInspectionFile interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}

// storeInspectionFS deliberately has no mutation operations. Tests may
// replace this private read-only surface to audit every inspector operation.
type storeInspectionFS interface {
	Lstat(string) (os.FileInfo, error)
	ReadDir(string) ([]os.DirEntry, error)
	Open(string) (storeInspectionFile, error)
}

type osStoreInspectionFS struct{}

func (osStoreInspectionFS) Lstat(path string) (os.FileInfo, error)        { return os.Lstat(path) }
func (osStoreInspectionFS) ReadDir(path string) ([]os.DirEntry, error)    { return os.ReadDir(path) }
func (osStoreInspectionFS) Open(path string) (storeInspectionFile, error) { return os.Open(path) }

var storeInspectionFilesystem storeInspectionFS = osStoreInspectionFS{}

// InspectStore validates the existing config-restore/v1 store without writing
// to it. exclusiveLease is an explicit caller precondition: a concurrent
// writer makes the evidence invalid, and passing false is always rejected.
func InspectStore(root string, exclusiveLease bool) (*StoreInspection, error) {
	return InspectStoreWithBoundary(root, exclusiveLease, nil)
}

// InspectStoreWithBoundary resolves projected legacy journal identities under
// the caller's already-authorized host boundary without acquiring any lock.
func InspectStoreWithBoundary(root string, exclusiveLease bool, boundary HostBoundary) (*StoreInspection, error) {
	if !exclusiveLease {
		return nil, fmt.Errorf("config restore inspection requires an exclusive no-concurrent-writer lease")
	}
	fs := storeInspectionFilesystem
	if err := validateInspectionRoot(fs, root); err != nil {
		return nil, err
	}
	before, err := scanStoreInspectionTree(fs, root)
	if err != nil {
		return nil, err
	}
	if storeInspectionAfterFirstScan != nil {
		storeInspectionAfterFirstScan()
	}
	inspection, err := inspectClosedStore(fs, root, boundary)
	if err != nil {
		return nil, err
	}
	after, err := scanStoreInspectionTree(fs, root)
	if err != nil {
		return nil, err
	}
	if !before.equal(after) {
		return nil, fmt.Errorf("config restore store changed during read-only inspection")
	}
	return inspection, nil
}

func validateInspectionRoot(fs storeInspectionFS, root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root ||
		filepath.Base(root) != "v1" || filepath.Base(filepath.Dir(root)) != "config-restore" {
		return fmt.Errorf("config restore inspector requires an existing clean config-restore/v1 root")
	}
	if err := validateInspectionPathNoLinks(fs, root); err != nil {
		return fmt.Errorf("inspect config restore root: %w", err)
	}
	info, err := fs.Lstat(root)
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

func scanStoreInspectionTree(fs storeInspectionFS, root string) (storeInspectionTree, error) {
	tree := storeInspectionTree{entries: make(map[string]storeInspectionTreeEntry)}
	var total int64
	var visit func(string, int) error
	visit = func(current string, depth int) error {
		if depth > maxStoreInspectionDepth {
			return fmt.Errorf("config restore store exceeds inspection depth limit")
		}
		info, err := fs.Lstat(current)
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
			entries, err := fs.ReadDir(current)
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
			read, err := readInspectionBoundedFile(fs, current)
			if err != nil {
				return err
			}
			record.digest = read.digest
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

type inspectionRead struct {
	data   []byte
	info   os.FileInfo
	digest string
}

func readInspectionBoundedFile(fs storeInspectionFS, path string) (inspectionRead, error) {
	if err := validateInspectionPathNoLinks(fs, path); err != nil {
		return inspectionRead{}, err
	}
	before, err := fs.Lstat(path)
	if err != nil {
		return inspectionRead{}, err
	}
	if !before.Mode().IsRegular() || isLinkOrReparse(before) || before.Size() < 0 || before.Size() > maxStoreInspectionFile {
		return inspectionRead{}, fmt.Errorf("config restore record is not a bounded safe regular file")
	}
	file, err := fs.Open(path)
	if err != nil {
		return inspectionRead{}, err
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || isLinkOrReparse(opened) || !os.SameFile(before, opened) || opened.Size() != before.Size() {
		_ = file.Close()
		return inspectionRead{}, fmt.Errorf("config restore record changed before bounded read")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxStoreInspectionFile+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) != before.Size() || len(data) > maxStoreInspectionFile {
		return inspectionRead{}, fmt.Errorf("read bounded config restore record: %w", firstInspectionError(readErr, closeErr))
	}
	after, err := fs.Lstat(path)
	if err != nil || !after.Mode().IsRegular() || isLinkOrReparse(after) || !os.SameFile(before, after) ||
		after.Size() != before.Size() || after.Mode() != before.Mode() || after.ModTime() != before.ModTime() {
		return inspectionRead{}, fmt.Errorf("config restore record changed during bounded read")
	}
	digest := sha256.Sum256(data)
	return inspectionRead{data: data, info: after, digest: hex.EncodeToString(digest[:])}, nil
}

func validateInspectionPathNoLinks(fs storeInspectionFS, path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("inspection path is not clean and absolute")
	}
	volume := filepath.VolumeName(path)
	current := volume + string(filepath.Separator)
	relative, err := filepath.Rel(current, path)
	if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("inspection path escapes volume root")
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := fs.Lstat(current)
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("inspection path contains link or reparse point")
		}
	}
	return nil
}

func firstInspectionError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func inspectClosedStore(fs storeInspectionFS, root string, boundary HostBoundary) (*StoreInspection, error) {
	transactions := filepath.Join(root, "transactions")
	legacyMembers := filepath.Join(root, "legacy-members")
	legacyReverts := filepath.Join(root, "legacy-reverts")
	legacyWork := filepath.Join(root, "legacy-revert-work")
	if err := requireInspectionDirectory(fs, root, "transactions", transactions); err != nil {
		return nil, err
	}
	if err := requireInspectionDirectory(fs, root, "legacy-members", legacyMembers); err != nil {
		return nil, err
	}
	if err := requireInspectionDirectory(fs, root, "legacy-reverts", legacyReverts); err != nil {
		return nil, err
	}
	if err := requireInspectionDirectory(fs, root, "legacy-revert-work", legacyWork); err != nil {
		return nil, err
	}
	if err := requireEmptyInspectionDirectory(fs, legacyWork); err != nil {
		return nil, fmt.Errorf("legacy revert work is incomplete or ambiguous: %w", err)
	}
	entries, err := fs.ReadDir(root)
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

	transactionCount, err := inspectGenerationTransactions(fs, transactions, add)
	if err != nil {
		return nil, err
	}
	legacyCount, err := inspectLegacyMembers(fs, legacyMembers, legacyReverts, boundary, add)
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

func requireInspectionDirectory(fs storeInspectionFS, root, name, directory string) error {
	if filepath.Dir(directory) != root || filepath.Base(directory) != name {
		return fmt.Errorf("invalid inspection directory")
	}
	info, err := fs.Lstat(directory)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return fmt.Errorf("config restore store %q is not a safe directory", name)
	}
	return nil
}

func requireEmptyInspectionDirectory(fs storeInspectionFS, directory string) error {
	entries, err := fs.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("directory is not empty")
	}
	return nil
}

func inspectGenerationTransactions(
	fs storeInspectionFS,
	directory string,
	add func(string, string, string, StoreMemberInspection) error,
) (int, error) {
	entries, err := fs.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isOpaqueStoreID(entry.Name()) {
			return 0, fmt.Errorf("config restore store has unknown transaction entry %q", entry.Name())
		}
		root := filepath.Join(directory, entry.Name())
		info, err := fs.Lstat(root)
		if err != nil || isLinkOrReparse(info) {
			return 0, fmt.Errorf("config restore transaction %q is unsafe", entry.Name())
		}
		descriptor, started, err := readInspectionTransactionDescriptor(fs, root)
		if err != nil {
			return 0, fmt.Errorf("inspect transaction %q descriptor: %w", entry.Name(), err)
		}
		intent, err := readInspectionJournalIntent(fs, root)
		if err != nil {
			return 0, fmt.Errorf("inspect transaction %q intent: %w", entry.Name(), err)
		}
		lineage := intent.Lineage()
		if descriptor.RunID != lineage.RunID || descriptor.CaptureID != lineage.CaptureID || descriptor.RestoreRunID == "" {
			return 0, fmt.Errorf("config restore transaction %q descriptor differs from intent lineage", entry.Name())
		}
		terminalPath := journalMarkerPath(filepath.Join(root, "journal"), JournalCommitted, intent.Digest())
		terminal, err := readInspectionJournalMarker(fs, root, terminalPath, intent)
		if err != nil {
			return 0, fmt.Errorf("config restore transaction %q is pending or has invalid terminal marker: %w", entry.Name(), err)
		}
		if terminal.State() != JournalCommitted {
			return 0, fmt.Errorf("config restore transaction %q is rolled back or ambiguous", entry.Name())
		}
		if err := requireCanonicalTransactionLayout(fs, root, intent.Digest()); err != nil {
			return 0, fmt.Errorf("config restore transaction %q layout: %w", entry.Name(), err)
		}
		reverted, revertDigest, err := inspectMemberRevert(
			fs, filepath.Join(root, "reverted.json"), StoreMemberGeneration, descriptor.TransactionID, terminal.Digest(),
		)
		if err != nil {
			return 0, fmt.Errorf("config restore transaction %q revert: %w", entry.Name(), err)
		}
		actions, err := inspectionJournalActions(fs, root, intent.Actions())
		if err != nil {
			return 0, fmt.Errorf("config restore transaction %q actions: %w", entry.Name(), err)
		}
		member := StoreMemberInspection{
			Kind: StoreMemberGeneration, ID: descriptor.TransactionID, Ordinal: descriptor.MutationOrdinal,
			CaptureID: descriptor.CaptureID, DescriptorDigest: descriptor.DescriptorDigest, MemberDigest: terminal.Digest(),
			IntentDigest: intent.Digest(), TerminalDigest: terminal.Digest(), TerminalState: terminal.State(),
			ValidationStatus: terminal.ValidationStatus(), RollbackOutcome: terminal.RollbackOutcome(),
			Reverted: reverted, RevertDigest: revertDigest, HasLineage: true,
			Lineage: inspectionLineage(lineage),
			actions: actions,
		}
		if err := add(descriptor.RestoreRunID, descriptor.RunID, started.UTC().Format(time.RFC3339Nano), member); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

func requireCanonicalTransactionLayout(fs storeInspectionFS, root, intentDigest string) error {
	entries, err := fs.ReadDir(root)
	if err != nil {
		return err
	}
	expected := map[string]bool{"transaction.json": true, "journal": true, "snapshots": true, "reverted.json": true}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			return fmt.Errorf("unknown entry %q", entry.Name())
		}
		path := filepath.Join(root, entry.Name())
		info, err := fs.Lstat(path)
		if err != nil || isLinkOrReparse(info) {
			return fmt.Errorf("unsafe entry %q", entry.Name())
		}
		if (entry.Name() == "journal" || entry.Name() == "snapshots") != info.IsDir() {
			return fmt.Errorf("entry %q has wrong file type", entry.Name())
		}
	}
	if err := requireCanonicalJournalLayout(fs, filepath.Join(root, "journal"), intentDigest); err != nil {
		return err
	}
	return nil
}

func requireCanonicalJournalLayout(fs storeInspectionFS, directory, intentDigest string) error {
	entries, err := fs.ReadDir(directory)
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
		info, err := fs.Lstat(filepath.Join(directory, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || isLinkOrReparse(info) {
			return fmt.Errorf("journal entry %q is not a safe regular file", entry.Name())
		}
	}
	return nil
}

func readInspectionTransactionDescriptor(fs storeInspectionFS, root string) (transactionDescriptorDisk, time.Time, error) {
	read, err := readInspectionBoundedFile(fs, filepath.Join(root, "transaction.json"))
	if err != nil {
		return transactionDescriptorDisk{}, time.Time{}, err
	}
	disk, _, err := decodeTransactionDescriptor(read.data)
	if err != nil || disk.TransactionID != filepath.Base(root) {
		return transactionDescriptorDisk{}, time.Time{}, fmt.Errorf("invalid transaction descriptor")
	}
	started, err := time.Parse(time.RFC3339Nano, disk.RunStartedAtUTC)
	return disk, started, err
}

func readInspectionJournalIntent(fs storeInspectionFS, root string) (*JournalIntent, error) {
	path := filepath.Join(root, "journal", "intent.json")
	read, err := readInspectionBoundedFile(fs, path)
	if err != nil {
		return nil, err
	}
	disk, err := decodeJournalIntent(read.data)
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
	if err := verifyInspectionSnapshotLayout(fs, root, disk.Actions); err != nil {
		return nil, err
	}
	if _, err := inspectionJournalActions(fs, root, disk.Actions); err != nil {
		return nil, err
	}
	return intentFromDisk(root, path, disk), nil
}

func verifyInspectionSnapshotLayout(fs storeInspectionFS, root string, actions []JournalAction) error {
	snapshotRoot := filepath.Join(root, "snapshots")
	entries, err := fs.ReadDir(snapshotRoot)
	if err != nil || len(entries) != len(actions) {
		return fmt.Errorf("snapshot layout is not canonical")
	}
	for index, action := range actions {
		name := formatActionIndex(index)
		if entries[index].Name() != name || !entries[index].IsDir() {
			return fmt.Errorf("snapshot action layout is not canonical")
		}
		actionRoot := filepath.Join(snapshotRoot, name)
		members, err := fs.ReadDir(actionRoot)
		if err != nil {
			return err
		}
		if action.Prior.Kind == StateAbsent {
			if len(members) != 0 {
				return fmt.Errorf("absent snapshot has artifacts")
			}
			continue
		}
		want := "prior"
		if action.Kind == ActionRegistrySet {
			want = "prior.registry"
		}
		if len(members) != 1 || members[0].Name() != want || (action.Kind == ActionRegistrySet && members[0].IsDir()) {
			return fmt.Errorf("snapshot backup layout is not canonical")
		}
	}
	return nil
}

func readInspectionJournalMarker(fs storeInspectionFS, root, path string, intent *JournalIntent) (*JournalMarker, error) {
	read, err := readInspectionBoundedFile(fs, path)
	if err != nil {
		return nil, err
	}
	disk, err := decodeJournalMarker(read.data)
	if err != nil || disk.IntentDigest != intent.Digest() || path != journalMarkerPath(filepath.Join(root, "journal"), disk.State, disk.IntentDigest) {
		return nil, fmt.Errorf("invalid journal terminal marker")
	}
	return markerFromDisk(root, path, disk, intent), nil
}

func inspectionJournalActions(fs storeInspectionFS, root string, actions []JournalAction) ([]StoreActionInspection, error) {
	result := make([]StoreActionInspection, len(actions))
	for index, action := range actions {
		item := StoreActionInspection{Index: action.Index, Kind: action.Kind, Strategy: action.Strategy, SourceDigest: action.SourceDigest,
			TargetIdentity: opaqueInspectionIdentity(action.Target), PriorDigest: action.Prior.Digest, DesiredDigest: action.Desired.Digest,
			PriorKind: action.Prior.Kind, DesiredKind: action.Desired.Kind}
		backup, err := inspectActionBackup(fs, root, action)
		if err != nil {
			return nil, fmt.Errorf("action[%d] backup: %w", index, err)
		}
		item.Backup = backup
		result[index] = item
	}
	return result, nil
}

func inspectActionBackup(fs storeInspectionFS, root string, action JournalAction) (StoreBackupInspection, error) {
	if action.Prior.Kind == StateAbsent {
		return StoreBackupInspection{}, nil
	}
	path := action.Prior.BackupPath
	if path == "" {
		return StoreBackupInspection{}, fmt.Errorf("missing prior backup path")
	}
	backup := StoreBackupInspection{Exists: true, Identity: opaqueInspectionIdentity(path), Kind: action.Prior.Kind, Mode: action.Prior.Mode}
	if action.Kind == ActionRegistrySet {
		read, err := readInspectionBoundedFile(fs, path)
		if err != nil {
			return StoreBackupInspection{}, err
		}
		var snapshot registrySnapshot
		if err := json.Unmarshal(read.data, &snapshot); err != nil || snapshot.Key != action.RegistryKey || snapshot.ValueName != action.RegistryValueName ||
			snapshot.Exists != (action.Prior.Kind == StateRegistryValue) || digestRegistrySnapshot(snapshot) != action.Prior.Digest {
			return StoreBackupInspection{}, fmt.Errorf("registry backup differs from journal")
		}
		backup.Digest = action.Prior.Digest
		return backup, nil
	}
	state, err := scanInspectionFilesystemState(fs, path)
	if err != nil || state.Digest != action.Prior.Digest || uint32(state.Mode.Perm()) != action.Prior.Mode {
		return StoreBackupInspection{}, fmt.Errorf("filesystem backup differs from journal: %w", err)
	}
	backup.Digest, backup.Kind, backup.Mode = state.Digest, state.Kind, uint32(state.Mode.Perm())
	return backup, nil
}

func scanInspectionFilesystemState(fs storeInspectionFS, root string) (filesystemState, error) {
	return scanInspectionFilesystemStateWithBudget(fs, root, nil)
}

func scanInspectionFilesystemStateWithBudget(fs storeInspectionFS, root string, budget *legacyBackupInspection) (filesystemState, error) {
	if err := validateInspectionPathNoLinks(fs, root); err != nil {
		return filesystemState{}, err
	}
	info, err := fs.Lstat(root)
	if os.IsNotExist(err) {
		return absentFilesystemState(), nil
	}
	if err != nil || isLinkOrReparse(info) {
		return filesystemState{}, fmt.Errorf("unsafe snapshot path")
	}
	entries := map[string]filesystemEntry{}
	var count, total int64
	var visit func(string, string, int) error
	visit = func(path, relative string, depth int) error {
		if depth > maxStoreInspectionDepth {
			return fmt.Errorf("snapshot exceeds inspection depth limit")
		}
		count++
		if count > maxStoreInspectionEntries {
			return fmt.Errorf("snapshot exceeds inspection entry limit")
		}
		if budget != nil {
			if budget.entries >= maxStoreInspectionEntries {
				return fmt.Errorf("legacy backups exceed inspection entry limit")
			}
			budget.entries++
		}
		entry, err := fs.Lstat(path)
		if err != nil || isLinkOrReparse(entry) {
			return fmt.Errorf("unsafe snapshot entry")
		}
		portable := filepath.ToSlash(relative)
		switch {
		case entry.Mode().IsRegular():
			if budget != nil {
				if budget.reads >= maxStoreInspectionEntries || entry.Size() < 0 || budget.bytes > maxStoreInspectionBytes-entry.Size() {
					return fmt.Errorf("legacy backups exceed inspection read limit")
				}
			}
			read, err := readInspectionBoundedFile(fs, path)
			if err != nil {
				return err
			}
			if total > maxStoreInspectionBytes-int64(len(read.data)) {
				return fmt.Errorf("snapshot exceeds inspection byte limit")
			}
			total += int64(len(read.data))
			if budget != nil {
				budget.reads++
				budget.bytes += int64(len(read.data))
			}
			entries[portable] = filesystemEntry{Path: portable, Kind: StateFile, Mode: entry.Mode().Perm(), Size: entry.Size(), ContentHash: read.digest}
		case entry.IsDir():
			entries[portable] = filesystemEntry{Path: portable, Kind: StateDirectory, Mode: entry.Mode().Perm()}
			children, err := fs.ReadDir(path)
			if err != nil {
				return err
			}
			for _, child := range children {
				if err := visit(filepath.Join(path, child.Name()), filepath.Join(relative, child.Name()), depth+1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unsupported snapshot special file")
		}
		return nil
	}
	if err := visit(root, ".", 0); err != nil {
		return filesystemState{}, err
	}
	rootEntry := entries["."]
	state := filesystemState{Kind: rootEntry.Kind, Mode: rootEntry.Mode, Entries: entries}
	state.Digest = digestFilesystemState(state)
	return state, nil
}

func opaqueInspectionIdentity(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func inspectLegacyMembers(
	fs storeInspectionFS,
	membersDirectory, revertsDirectory string,
	boundary HostBoundary,
	add func(string, string, string, StoreMemberInspection) error,
) (int, error) {
	entries, err := fs.ReadDir(membersDirectory)
	if err != nil {
		return 0, err
	}
	memberByID := make(map[string]legacyMemberDisk, len(entries))
	journalIdentities := make(map[string]struct{}, len(entries))
	journalCache := make(map[string]legacyInspectionJournal, len(entries))
	budget := legacyInspectionBudget{}
	backupInspection := newLegacyBackupInspection()
	for _, entry := range entries {
		id, ok := inspectionRecordID(entry.Name())
		if !ok || entry.IsDir() {
			return 0, fmt.Errorf("config restore store has unknown legacy member entry %q", entry.Name())
		}
		path := filepath.Join(membersDirectory, entry.Name())
		data, _, err := readInspectionRegularFile(fs, path)
		if err != nil {
			return 0, err
		}
		disk, started, err := decodeLegacyMember(data)
		if err != nil || disk.MemberID != id {
			return 0, fmt.Errorf("config restore legacy member %q is invalid", id)
		}
		journalPath, journalIdentity, err := resolveInspectionLegacyJournal(disk.JournalPath, boundary)
		if err != nil {
			return 0, fmt.Errorf("config restore legacy member %q journal identity: %w", id, err)
		}
		identity := disk.JournalPath + "\x00" + disk.JournalDigest
		if _, duplicate := journalIdentities[identity]; duplicate {
			return 0, fmt.Errorf("config restore store duplicates registered legacy journal")
		}
		journalIdentities[identity] = struct{}{}
		cached, exists := journalCache[identity]
		if !exists {
			journal, err := inspectLegacyJournal(fs, journalPath, disk.JournalDigest, &budget)
			if err != nil {
				return 0, fmt.Errorf("config restore legacy member %q journal: %w", id, err)
			}
			actions, err := inspectionLegacyJournalActions(fs, journal, boundary, backupInspection)
			if err != nil {
				return 0, fmt.Errorf("config restore legacy member %q backup: %w", id, err)
			}
			cached = legacyInspectionJournal{journal: journal, actions: actions}
			journalCache[identity] = cached
		}
		if cached.journal.RunID != disk.RunID {
			return 0, fmt.Errorf("config restore legacy member %q journal run differs", id)
		}
		memberByID[id] = disk
		reverted, revertDigest, err := inspectMemberRevert(
			fs, filepath.Join(revertsDirectory, id+".json"), StoreMemberLegacy, id, disk.MemberDigest,
		)
		if err != nil {
			return 0, fmt.Errorf("config restore legacy member %q revert: %w", id, err)
		}
		member := StoreMemberInspection{
			Kind: StoreMemberLegacy, ID: id, Ordinal: disk.MutationOrdinal, MemberDigest: disk.MemberDigest,
			Reverted: reverted, RevertDigest: revertDigest,
			LegacyJournalIdentity: journalIdentity, LegacyJournalDigest: disk.JournalDigest,
			actions: cached.actions,
		}
		if err := add(disk.RestoreRunID, disk.RunID, started.UTC().Format(time.RFC3339Nano), member); err != nil {
			return 0, err
		}
	}
	if err := requireCanonicalLegacyReverts(fs, revertsDirectory, memberByID); err != nil {
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

func requireCanonicalLegacyReverts(fs storeInspectionFS, directory string, members map[string]legacyMemberDisk) error {
	entries, err := fs.ReadDir(directory)
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
		if _, _, err := inspectMemberRevert(fs, filepath.Join(directory, entry.Name()), StoreMemberLegacy, id, member.MemberDigest); err != nil {
			return err
		}
	}
	return nil
}

func inspectMemberRevert(fs storeInspectionFS, path string, kind StoreMemberKind, id, sourceDigest string) (bool, string, error) {
	data, exists, err := readInspectionRegularFile(fs, path)
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

func readInspectionRegularFile(fs storeInspectionFS, path string) ([]byte, bool, error) {
	info, err := fs.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, err
	}
	if err != nil || !info.Mode().IsRegular() || isLinkOrReparse(info) {
		return nil, false, fmt.Errorf("config restore record is not a safe regular file")
	}
	read, err := readInspectionBoundedFile(fs, path)
	return read.data, true, err
}

type legacyInspectionBudget struct {
	journals int
	bytes    int64
	entries  int
}

type legacyInspectionJournal struct {
	journal *restore.Journal
	actions []StoreActionInspection
}

type legacyBackupInspection struct {
	entries  int
	bytes    int64
	reads    int
	evidence map[string]StoreBackupInspection
}

func newLegacyBackupInspection() *legacyBackupInspection {
	return &legacyBackupInspection{evidence: make(map[string]StoreBackupInspection)}
}

func resolveInspectionLegacyJournal(identity string, boundary HostBoundary) (string, string, error) {
	path := identity
	projectedInput := !filepath.IsAbs(path)
	if boundary == nil {
		return "", "", fmt.Errorf("legacy journal inspection requires an authorized host boundary")
	}
	if !filepath.IsAbs(path) {
		if !strings.HasPrefix(path, "$ENDSTATE_ROOT/logs/") {
			return "", "", fmt.Errorf("projected journal is outside the authorized log root")
		}
		if boundary == nil {
			return "", "", fmt.Errorf("projected journal requires a host boundary")
		}
		var err error
		path, err = boundary.ResolveFilesystemIdentity(identity)
		if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return "", "", fmt.Errorf("cannot resolve projected journal")
		}
		if err := boundary.ValidateFilesystemTarget(path); err != nil {
			return "", "", err
		}
	}
	if filepath.Clean(path) != path || containsControl(path) {
		return "", "", fmt.Errorf("journal path is not clean")
	}
	if err := boundary.ValidateFilesystemTarget(path); err != nil {
		return "", "", err
	}
	projected := ""
	if projectedInput {
		value, err := boundary.ProjectFilesystemIdentity(path)
		if err == nil && value != "" && !filepath.IsAbs(value) {
			projected = value
		}
	} else {
		projected = opaqueInspectionIdentity(path)
	}
	if projected == "" || (projectedInput && !(strings.HasPrefix(projected, "$ENDSTATE_ROOT/logs/") || strings.HasPrefix(projected, "$BOUNDARY/logs/"))) {
		return "", "", fmt.Errorf("journal is outside the authorized projected log root")
	}
	return path, projected, nil
}

func inspectLegacyJournal(fs storeInspectionFS, path, digest string, budget *legacyInspectionBudget) (*restore.Journal, error) {
	if budget.journals >= maxStoreInspectionJournals {
		return nil, fmt.Errorf("legacy journal count exceeds inspection limit")
	}
	read, err := readInspectionBoundedFile(fs, path)
	if err != nil {
		return nil, err
	}
	if budget.bytes > maxStoreInspectionBytes-int64(len(read.data)) {
		return nil, fmt.Errorf("legacy journals exceed inspection byte limit")
	}
	if read.digest != digest {
		return nil, fmt.Errorf("registered journal digest changed")
	}
	journal, err := restore.ParseJournal(read.data)
	if err != nil || journal.RunID == "" || journal.RunID != strings.TrimSpace(journal.RunID) || containsControl(journal.RunID) {
		return nil, fmt.Errorf("legacy journal is malformed")
	}
	if len(journal.Entries) > maxStoreInspectionEntries {
		return nil, fmt.Errorf("legacy journal exceeds inspection entry limit")
	}
	retained := len(journal.Entries)
	for _, entry := range journal.Entries {
		if entry.BackupCreated {
			retained++
		}
	}
	if budget.entries > maxStoreInspectionEntries-retained {
		return nil, fmt.Errorf("legacy journal actions exceed inspection limit")
	}
	for index, entry := range journal.Entries {
		if !validStoreActionStatus(entry.Action) || containsControl(entry.TargetPath) || containsControl(entry.ResolvedSourcePath) {
			return nil, fmt.Errorf("legacy journal action[%d] is malformed", index)
		}
		if entry.BackupCreated != entry.BackupRequested || (entry.BackupCreated && entry.BackupPath == "") || (!entry.BackupCreated && entry.BackupPath != "") {
			return nil, fmt.Errorf("legacy journal action[%d] has missing backup identity", index)
		}
	}
	budget.journals++
	budget.bytes += int64(len(read.data))
	budget.entries += retained
	return journal, nil
}

func validStoreActionStatus(value string) bool {
	switch StoreActionStatus(value) {
	case StoreActionStatusFailed, StoreActionStatusInstalled, StoreActionStatusRestored,
		StoreActionStatusSkipped, StoreActionStatusSkippedMissingSource, StoreActionStatusSkippedUpToDate:
		return true
	default:
		return false
	}
}

func inspectionLegacyJournalActions(
	fs storeInspectionFS,
	journal *restore.Journal,
	boundary HostBoundary,
	backupInspection *legacyBackupInspection,
) ([]StoreActionInspection, error) {
	result := make([]StoreActionInspection, len(journal.Entries))
	for index, entry := range journal.Entries {
		item := StoreActionInspection{Index: index, Status: StoreActionStatus(entry.Action), SourceIdentity: opaqueInspectionIdentity(entry.ResolvedSourcePath), TargetIdentity: opaqueInspectionIdentity(entry.TargetPath)}
		if entry.BackupCreated {
			backup, err := inspectLegacyActionBackup(fs, entry.BackupPath, boundary, backupInspection)
			if err != nil {
				return nil, fmt.Errorf("action[%d]: %w", index, err)
			}
			item.Backup = backup
		}
		result[index] = item
	}
	return result, nil
}

func inspectLegacyActionBackup(
	fs storeInspectionFS,
	identity string,
	boundary HostBoundary,
	backupInspection *legacyBackupInspection,
) (StoreBackupInspection, error) {
	path, err := resolveInspectionLegacyBackup(identity, boundary)
	if err != nil {
		return StoreBackupInspection{}, err
	}
	key := opaqueInspectionIdentity(canonicalFilesystemTarget(path))
	if _, exists := backupInspection.evidence[key]; exists {
		return StoreBackupInspection{}, fmt.Errorf("legacy backup is registered by more than one action")
	}
	state, err := scanInspectionFilesystemStateWithBudget(fs, path, backupInspection)
	if err != nil {
		return StoreBackupInspection{}, fmt.Errorf("inspect legacy backup: %w", err)
	}
	if state.Kind == StateAbsent {
		return StoreBackupInspection{}, fmt.Errorf("legacy backup is unavailable")
	}
	backup := StoreBackupInspection{
		Exists: true, Identity: opaqueInspectionIdentity(identity), Digest: state.Digest,
		Kind: state.Kind, Mode: uint32(state.Mode.Perm()),
	}
	backupInspection.evidence[key] = backup
	return backup, nil
}

func resolveInspectionLegacyBackup(identity string, boundary HostBoundary) (string, error) {
	if boundary == nil {
		return "", fmt.Errorf("legacy backup inspection requires an authorized host boundary")
	}
	path := identity
	if !filepath.IsAbs(path) {
		if !strings.HasPrefix(path, "$ENDSTATE_ROOT/state/backups/") {
			return "", fmt.Errorf("legacy backup is outside the authorized backup root")
		}
		var err error
		path, err = boundary.ResolveFilesystemIdentity(identity)
		if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return "", fmt.Errorf("cannot resolve legacy backup identity")
		}
	}
	if filepath.Clean(path) != path || containsControl(path) {
		return "", fmt.Errorf("legacy backup path is not clean")
	}
	if err := boundary.ValidateFilesystemTarget(path); err != nil {
		return "", err
	}
	projected, err := boundary.ProjectFilesystemIdentity(path)
	if err != nil || filepath.IsAbs(projected) ||
		(!strings.HasPrefix(projected, "$ENDSTATE_ROOT/state/backups/") && !strings.HasPrefix(projected, "$BOUNDARY/state/backups/")) {
		return "", fmt.Errorf("legacy backup is outside the authorized backup root")
	}
	return path, nil
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
