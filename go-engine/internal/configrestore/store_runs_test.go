// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestActiveStoreRunsOrdersGenerationAndRegisteredLegacyMembers(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	guard, err := BeginLive(ctx, stateDir, "apply-mixed", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })

	firstTarget := commitStoredDelete(t, guard, "apply-mixed", "capture-first", "first")
	legacyPath := filepath.Join(t.TempDir(), "restore-legacy.json")
	if err := os.WriteFile(legacyPath, []byte("{\"runId\":\"legacy\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registered, err := guard.RegisterLegacyJournal(legacyPath)
	if err != nil {
		t.Fatalf("RegisterLegacyJournal() error = %v", err)
	}
	secondTarget := commitStoredDelete(t, guard, "apply-mixed", "capture-second", "second")

	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID() == "" || runs[0].RunID() != "apply-mixed" || runs[0].StartedAt().IsZero() {
		t.Fatalf("active runs = %+v", runs)
	}
	members := runs[0].Members()
	if len(members) != 3 {
		t.Fatalf("members = %+v, want three", members)
	}
	if members[0].Kind() != StoreMemberGeneration || members[0].Ordinal() != 0 || members[0].CaptureID() != "capture-first" ||
		members[1].Kind() != StoreMemberLegacy || members[1].Ordinal() != 1 || members[1].LegacyJournalPath() != legacyPath ||
		members[2].Kind() != StoreMemberGeneration || members[2].Ordinal() != 2 || members[2].CaptureID() != "capture-second" {
		t.Fatalf("ordered members = [%+v, %+v, %+v]", members[0], members[1], members[2])
	}
	if registered.Kind() != StoreMemberLegacy || registered.Ordinal() != members[1].Ordinal() {
		t.Fatalf("registered member = %+v", registered)
	}

	if _, err := guard.RevertGenerationMember(ctx, members[2]); err != nil {
		t.Fatalf("RevertGenerationMember(second) error = %v", err)
	}
	if data, err := os.ReadFile(secondTarget); err != nil || string(data) != "second" {
		t.Fatalf("second target after revert = %q, %v", data, err)
	}
	if _, err := guard.RevertGenerationMember(ctx, members[2]); !errors.Is(err, ErrStoreMemberReverted) {
		t.Fatalf("second revert error = %v, want ErrStoreMemberReverted", err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, members[1]); err != nil {
		t.Fatalf("MarkLegacyMemberReverted() error = %v", err)
	}

	runs, err = guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || len(runs[0].Members()) != 1 || runs[0].Members()[0].CaptureID() != "capture-first" {
		t.Fatalf("active runs after consumption = %+v", runs)
	}
	if _, err := os.Lstat(firstTarget); !os.IsNotExist(err) {
		t.Fatalf("unreverted first target should remain deleted, err=%v", err)
	}
}

func TestRegisterLegacyJournalIsIdempotentAndRevertWorkIsOneShot(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-legacy-idempotent", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	journalPath := filepath.Join(t.TempDir(), "restore-legacy.json")
	if err := os.WriteFile(journalPath, []byte("{\"runId\":\"legacy\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.Ordinal() != second.Ordinal() || first.LegacyJournalPath() != second.LegacyJournalPath() {
		t.Fatalf("duplicate registration: first=%+v second=%+v", first, second)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("active runs = %+v, %v", runs, err)
	}
	firstRoot, err := guard.LegacyMemberRevertRoot(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot, err := guard.LegacyMemberRevertRoot(ctx, second)
	if err != nil || firstRoot != secondRoot {
		t.Fatalf("revert roots = %q, %q, %v", firstRoot, secondRoot, err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(firstRoot); !os.IsNotExist(err) {
		t.Fatalf("consumed legacy revert work remains at %q: %v", firstRoot, err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, second); err != nil {
		t.Fatalf("repeat MarkLegacyMemberReverted() error = %v", err)
	}
	if _, err := guard.LegacyMemberRevertRoot(ctx, second); !errors.Is(err, ErrStoreMemberReverted) {
		t.Fatalf("consumed revert root error = %v", err)
	}
}

func TestMarkLegacyMemberRevertedLeavesWorkWhenMarkerCannotPublish(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "legacy-marker-failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	journalPath := writeLegacyRevertTestJournal(t)
	member, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := guard.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(guard.legacyReverts, member.memberID+".json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, member); err == nil {
		t.Fatal("MarkLegacyMemberReverted() accepted an unpublishable marker path")
	}
	if _, err := os.Lstat(workRoot); err != nil {
		t.Fatalf("marker failure removed legacy work: %v", err)
	}
}

func TestBeginLiveReapsMarkerBackedLegacyRevertWorkAfterCrash(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	first, err := BeginLive(ctx, stateDir, "legacy-cleanup-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := writeLegacyRevertTestJournal(t)
	member, err := first.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	populateLegacyRevertWork(t, journalPath, workRoot)
	workFiles := readLegacyRevertWorkFiles(t, workRoot)
	if err := first.MarkLegacyMemberReverted(ctx, member); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the durable marker but before work retirement.
	if err := os.Mkdir(workRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range workFiles {
		if err := os.WriteFile(filepath.Join(workRoot, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginLive(ctx, stateDir, "legacy-cleanup-recovery", nil)
	if err != nil {
		t.Fatalf("BeginLive() recovery error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := os.Lstat(workRoot); !os.IsNotExist(err) {
		t.Fatalf("marker-backed legacy work remains at %q: %v", workRoot, err)
	}
}

func TestBeginLiveRejectsForeignMarkerBackedLegacyRevertWork(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name  string
		forge func(t *testing.T, root string)
	}{
		{
			name: "foreign filename",
			forge: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "foreign.json"), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "malformed record schema",
			forge: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "entry-000000.json"), []byte(`{"version":1,"extra":true}`+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "out of range record index",
			forge: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "entry-000001.json"), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "record bound to different journal entry",
			forge: func(t *testing.T, root string) {
				t.Helper()
				record := legacyRevertWorkPrepared{
					Version: 1, EntryIndex: 0, EntryDigest: strings.Repeat("0", 64), Target: "different-target",
					Before:  legacyRevertWorkState{Kind: "absent", Digest: strings.Repeat("1", 64)},
					Desired: legacyRevertWorkState{Kind: "absent", Digest: strings.Repeat("2", 64)},
				}
				data, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "entry-000000.json"), append(data, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unbound registry-import stage",
			forge: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "entry-000000-registry-stage.reg"), []byte("foreign stage"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "near-miss record publication scratch",
			forge: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, ".legacy-revert-record-12x.tmp"), []byte("foreign scratch"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "near-miss registry export scratch",
			forge: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, ".registry-state-65x.reg"), []byte("foreign scratch"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			journalPath := writeLegacyRevertTestJournal(t)
			first, err := BeginLive(ctx, stateDir, "legacy-foreign-source", nil)
			if err != nil {
				t.Fatal(err)
			}
			member, err := first.RegisterLegacyJournal(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
			if err != nil {
				t.Fatal(err)
			}
			if err := first.MarkLegacyMemberReverted(ctx, member); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(workRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			test.forge(t, workRoot)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}

			guard, err := BeginLive(ctx, stateDir, "legacy-foreign-recovery", nil)
			if guard != nil {
				_ = guard.Close()
			}
			if !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("BeginLive() error = %v, want ErrRecoveryRequired", err)
			}
			if _, err := os.Lstat(workRoot); err != nil {
				t.Fatalf("foreign marker-backed work was removed: %v", err)
			}
		})
	}
}

func TestMarkLegacyMemberRevertedRetainsWorkWhenBoundaryRejectsCleanup(t *testing.T) {
	ctx := context.Background()
	authorityRoot := t.TempDir()
	stateDir := filepath.Join(authorityRoot, "state")
	journalPath := filepath.Join(authorityRoot, "logs", "restore-legacy.json")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	boundary := &recordingHostBoundary{root: authorityRoot, resolved: map[string]string{}, projectRootToken: "$ENDSTATE_ROOT/"}
	guard, err := BeginLiveWithBoundary(ctx, stateDir, "legacy-boundary-cleanup", nil, boundary)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	member, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := guard.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	boundary.rejectPath = workRoot
	if err := guard.MarkLegacyMemberReverted(ctx, member); err == nil || !strings.Contains(err.Error(), "deliberate member boundary rejection") {
		t.Fatalf("MarkLegacyMemberReverted() error = %v, want cleanup boundary rejection", err)
	}
	if _, err := os.Lstat(filepath.Join(guard.legacyReverts, member.memberID+".json")); err != nil {
		t.Fatalf("cleanup rejection prevented marker publication: %v", err)
	}
	if _, err := os.Lstat(workRoot); err != nil {
		t.Fatalf("boundary-rejected cleanup removed work: %v", err)
	}
}

func TestBeginLivePreservesUnmarkedRegisteredLegacyRevertWork(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	first, err := BeginLive(ctx, stateDir, "legacy-resume-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := writeLegacyRevertTestJournal(t)
	member, err := first.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	populateLegacyRevertWork(t, journalPath, workRoot)
	workFiles := readLegacyRevertWorkFiles(t, workRoot)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginLive(ctx, stateDir, "legacy-resume-recovery", nil)
	if err != nil {
		t.Fatalf("BeginLive() rejected resumable legacy work: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if got := readLegacyRevertWorkFiles(t, workRoot); !equalLegacyRevertWorkFiles(got, workFiles) {
		t.Fatalf("resumable legacy work changed = %#v, want %#v", got, workFiles)
	}
}

func TestBeginLiveReconcilesLegitimateLegacyRevertCrashScratch(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	first, err := BeginLive(ctx, stateDir, "legacy-scratch-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := writeLegacyRevertTestJournal(t)
	member, err := first.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	populateLegacyRevertWork(t, journalPath, workRoot)
	recordTemp := filepath.Join(workRoot, ".legacy-revert-record-123456.tmp")
	registryTemp := filepath.Join(workRoot, ".registry-state-654321.reg")
	if err := os.WriteFile(recordTemp, []byte("torn record publication"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryTemp, []byte("registry export scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginLive(ctx, stateDir, "legacy-scratch-recovery", nil)
	if err != nil {
		t.Fatalf("BeginLive() recovery error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := os.Lstat(recordTemp); err != nil {
		t.Fatalf("durable-record publication residue was removed: %v", err)
	}
	if _, err := os.Lstat(registryTemp); !os.IsNotExist(err) {
		t.Fatalf("registry export scratch remains after recovery: %v", err)
	}
}

func TestBeginLiveRejectsLegacyCompletionWithImpossibleDeletedAction(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	first, err := BeginLive(ctx, stateDir, "legacy-completion-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := writeLegacyRevertTestJournal(t)
	member, err := first.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	populateLegacyRevertWork(t, journalPath, workRoot)
	completedPath := filepath.Join(workRoot, "entry-000000-completed.json")
	data, err := os.ReadFile(completedPath)
	if err != nil {
		t.Fatal(err)
	}
	var completed legacyRevertWorkCompleted
	if err := json.Unmarshal(data, &completed); err != nil {
		t.Fatal(err)
	}
	completed.Action = "deleted"
	data, err = json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(completedPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginLive(ctx, stateDir, "legacy-completion-recovery", nil)
	if second != nil {
		_ = second.Close()
	}
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("BeginLive() error = %v, want ErrRecoveryRequired", err)
	}
}

func TestBeginLiveRejectsDeletedCompletionWhenJournalRequiresPriorState(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	first, err := BeginLive(ctx, stateDir, "legacy-deleted-state-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := writeLegacyRevertTestJournal(t)
	member, err := first.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	populateLegacyRevertWork(t, journalPath, workRoot)
	preparedPath := filepath.Join(workRoot, "entry-000000.json")
	data, err := os.ReadFile(preparedPath)
	if err != nil {
		t.Fatal(err)
	}
	var prepared legacyRevertWorkPrepared
	if err := json.Unmarshal(data, &prepared); err != nil {
		t.Fatal(err)
	}
	prepared.Desired = legacyRevertWorkState{Kind: "absent", Digest: strings.Repeat("0", 64)}
	data, err = json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preparedPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	completedPath := filepath.Join(workRoot, "entry-000000-completed.json")
	data, err = os.ReadFile(completedPath)
	if err != nil {
		t.Fatal(err)
	}
	var completed legacyRevertWorkCompleted
	if err := json.Unmarshal(data, &completed); err != nil {
		t.Fatal(err)
	}
	completed.Action = "deleted"
	data, err = json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(completedPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginLive(ctx, stateDir, "legacy-deleted-state-recovery", nil)
	if second != nil {
		_ = second.Close()
	}
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("BeginLive() error = %v, want ErrRecoveryRequired", err)
	}
}

func TestBeginLiveRejectsLegacyPreparedStateKindForFilesystemEntry(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	first, err := BeginLive(ctx, stateDir, "legacy-state-kind-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := writeLegacyRevertTestJournal(t)
	member, err := first.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	populateLegacyRevertWork(t, journalPath, workRoot)
	preparedPath := filepath.Join(workRoot, "entry-000000.json")
	data, err := os.ReadFile(preparedPath)
	if err != nil {
		t.Fatal(err)
	}
	var prepared legacyRevertWorkPrepared
	if err := json.Unmarshal(data, &prepared); err != nil {
		t.Fatal(err)
	}
	prepared.Before.Kind = "registry-key"
	data, err = json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preparedPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginLive(ctx, stateDir, "legacy-state-kind-recovery", nil)
	if second != nil {
		_ = second.Close()
	}
	if !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("BeginLive() error = %v, want ErrRecoveryRequired", err)
	}
}

func TestBeginLivePreservesBoundRegistryImportStage(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	first, err := BeginLive(ctx, stateDir, "legacy-registry-stage-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	journalPath, entry := writeLegacyRegistryImportTestJournal(t)
	member, err := first.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := legacyRevertJournalEntryDigest(entry)
	if err != nil {
		t.Fatal(err)
	}
	stage, held := testLegacyRegistryImportScratch(entry.TargetPath, digest)
	prepared := legacyRevertWorkPrepared{
		Version: 1, EntryIndex: 0, EntryDigest: digest, Target: entry.TargetPath,
		Before:        legacyRevertWorkState{Kind: "registry-key", Digest: strings.Repeat("a", 64)},
		Desired:       legacyRevertWorkState{Kind: "registry-key", Digest: strings.Repeat("b", 64)},
		DesiredSource: entry.BackupPath, StagePath: stage, HeldPath: held,
	}
	data, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workRoot, "entry-000000.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	stageFile := filepath.Join(workRoot, "entry-000000-registry-stage.reg")
	if err := os.WriteFile(stageFile, []byte("Windows Registry Editor Version 5.00\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := BeginLive(ctx, stateDir, "legacy-registry-stage-recovery", nil)
	if err != nil {
		t.Fatalf("BeginLive() recovery error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := os.Lstat(stageFile); err != nil {
		t.Fatalf("bound registry-import stage was removed: %v", err)
	}
}

func TestBeginLiveRejectsUnsafeLegacyRevertWork(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name  string
		forge func(t *testing.T, guard *Guard, member *StoreMember, workRoot string)
	}{
		{
			name: "unknown member directory",
			forge: func(t *testing.T, guard *Guard, _ *StoreMember, _ string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(guard.legacyRevertWork, strings.Repeat("a", 32)), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mismatched marker",
			forge: func(t *testing.T, guard *Guard, member *StoreMember, workRoot string) {
				t.Helper()
				disk, encoded, err := newMemberRevert(StoreMemberLegacy, member.memberID, strings.Repeat("0", 64))
				if err != nil {
					t.Fatal(err)
				}
				if disk.MemberID != member.memberID {
					t.Fatal("forged marker did not retain member identity")
				}
				if err := os.WriteFile(filepath.Join(guard.legacyReverts, member.memberID+".json"), encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(workRoot); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "linked work root",
			forge: func(t *testing.T, guard *Guard, _ *StoreMember, workRoot string) {
				t.Helper()
				if err := os.Remove(workRoot); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "unrelated")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, workRoot); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			},
		},
		{
			name: "linked work entry",
			forge: func(t *testing.T, _ *Guard, _ *StoreMember, workRoot string) {
				t.Helper()
				target := filepath.Join(t.TempDir(), "unrelated")
				if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(workRoot, "entry-000000.json")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			first, err := BeginLive(ctx, stateDir, "legacy-work-source", nil)
			if err != nil {
				t.Fatal(err)
			}
			journalPath := filepath.Join(t.TempDir(), "restore-legacy.json")
			if err := os.WriteFile(journalPath, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			member, err := first.RegisterLegacyJournal(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			workRoot, err := first.LegacyMemberRevertRoot(ctx, member)
			if err != nil {
				t.Fatal(err)
			}
			test.forge(t, first, member, workRoot)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}

			guard, err := BeginLive(ctx, stateDir, "legacy-work-recovery", nil)
			if guard != nil {
				_ = guard.Close()
			}
			if !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("BeginLive() error = %v, want ErrRecoveryRequired", err)
			}
		})
	}
}

func TestRevertGenerationMemberRejectsUnrelatedDriftWithoutConsumption(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-drift", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	target := commitStoredDelete(t, guard, "apply-drift", "capture-drift", "before")
	if err := os.WriteFile(target, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("ActiveStoreRuns() = %+v, %v", runs, err)
	}
	if _, err := guard.RevertGenerationMember(ctx, runs[0].Members()[0]); err == nil {
		t.Fatal("RevertGenerationMember() accepted unrelated drift")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "unrelated" {
		t.Fatalf("drift changed = %q, %v", data, err)
	}
	active, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(active) != 1 || len(active[0].Members()) != 1 {
		t.Fatalf("member was consumed after failed revert: %+v, %v", active, err)
	}
}

func TestRevertGenerationMemberPreflightsAllActionsBeforeFirstMutation(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-preflight", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	root, err := guard.CreateTransactionRoot("capture-preflight")
	if err != nil {
		t.Fatal(err)
	}
	targets := []string{filepath.Join(t.TempDir(), "first.json"), filepath.Join(t.TempDir(), "second.json")}
	for index, target := range targets {
		if err := os.WriteFile(target, []byte{byte('a' + index)}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{
			{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[0], SnapshotRequired: true},
			{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[1], SnapshotRequired: true},
		}}, TransactionRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID, lineage.CaptureID = "apply-preflight", "capture-preflight"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ExecuteConfigSetTransaction(ctx, TransactionRequest{Prepared: prepared, Intent: intent}); err != nil || result.Status() != TransactionRestored {
		t.Fatalf("commit transaction = %+v, %v", result, err)
	}
	if err := os.WriteFile(targets[0], []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("ActiveStoreRuns() = %+v, %v", runs, err)
	}
	member := runs[0].Members()[0]
	if _, err := guard.RevertGenerationMember(ctx, member); err == nil {
		t.Fatal("RevertGenerationMember() accepted unrelated drift")
	}
	if _, err := os.Lstat(targets[1]); !os.IsNotExist(err) {
		t.Fatalf("later action was partially reverted before drift discovery, err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "reverted.json")); !os.IsNotExist(err) {
		t.Fatalf("failed revert published consumption sidecar, err=%v", err)
	}
}

func TestActiveStoreRunsClassifiesRevertedLegacyBeforeReadingOldJournal(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-legacy", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	journalPath := filepath.Join(t.TempDir(), "restore-legacy.json")
	if err := os.WriteFile(journalPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	member, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, member); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() read consumed legacy journal: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("active runs = %+v, want none", runs)
	}
}

func TestActiveStoreRunsClassifiesRevertedGenerationBeforeReadingBackup(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-generation", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	commitStoredDelete(t, guard, "apply-generation", "capture-generation", "before")
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("ActiveStoreRuns() = %+v, %v", runs, err)
	}
	member := runs[0].Members()[0]
	root := filepath.Join(guard.transactions, member.memberID)
	intent, err := ReadJournalIntent(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	backup := intent.Actions()[0].Prior.BackupPath
	if _, err := guard.RevertGenerationMember(ctx, member); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	runs, err = guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() read consumed generation backup: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("active runs = %+v, want none", runs)
	}
}

func TestActiveStoreRunsRemovesPrePublicationStoreTempOrphan(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-temp-orphan", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	tempPath := filepath.Join(guard.legacyMembers, ".store-record-123456.tmp")
	if err := os.WriteFile(tempPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() with pre-publication temp: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("active runs = %+v, want none", runs)
	}
	if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("pre-publication temp still exists, err=%v", err)
	}
}

func TestActiveStoreRunsRemovesPostPublicationStoreTempBesideValidMember(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-temp-published", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	journalPath := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(journalPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	member, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	tempPath := filepath.Join(guard.legacyMembers, ".store-record-654321.tmp")
	memberPath := filepath.Join(guard.legacyMembers, member.memberID+".json")
	if err := os.Link(memberPath, tempPath); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() with post-publication temp: %v", err)
	}
	if len(runs) != 1 || len(runs[0].Members()) != 1 || runs[0].Members()[0].memberID != member.memberID {
		t.Fatalf("active runs = %+v, want registered legacy member", runs)
	}
	if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("post-publication temp still exists, err=%v", err)
	}
}

func TestActiveStoreRunsRejectsLinkedStoreTempFailClosed(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-temp-link", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	target := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	tempPath := filepath.Join(guard.legacyMembers, ".store-record-777777.tmp")
	if err := os.Symlink(target, tempPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := guard.ActiveStoreRuns(ctx); err == nil {
		t.Fatal("ActiveStoreRuns() removed or ignored linked store temp")
	}
	if info, err := os.Lstat(tempPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("linked temp was changed: info=%v err=%v", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "untouched" {
		t.Fatalf("linked temp target changed = %q, %v", data, err)
	}
}

func commitStoredDelete(t *testing.T, guard *Guard, runID, captureID, prior string) string {
	t.Helper()
	ctx := context.Background()
	root, err := guard.CreateTransactionRoot(captureID)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}}, TransactionRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID, lineage.CaptureID = runID, captureID
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteConfigSetTransaction(ctx, TransactionRequest{Prepared: prepared, Intent: intent})
	if err != nil || result.Status() != TransactionRestored {
		t.Fatalf("commit transaction = %+v, %v", result, err)
	}
	return target
}

func writeLegacyRevertTestJournal(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	target := filepath.Join(directory, "settings.json")
	backup := filepath.Join(directory, "settings.backup.json")
	if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := restore.Journal{RunID: "legacy-work", Entries: []restore.JournalEntry{{
		TargetPath: target, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: backup, Action: "restored", RestoreType: "copy",
	}}}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "restore-legacy.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func populateLegacyRevertWork(t *testing.T, journalPath, workRoot string) {
	t.Helper()
	journal, err := restore.ReadJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restore.RunRevertDurable(journal, "", workRoot); err != nil {
		t.Fatal(err)
	}
}

func readLegacyRevertWorkFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("legacy revert work unexpectedly contains directory %q", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = data
	}
	return files
}

func equalLegacyRevertWorkFiles(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for name, data := range left {
		other, exists := right[name]
		if !exists || string(data) != string(other) {
			return false
		}
	}
	return true
}

func writeLegacyRegistryImportTestJournal(t *testing.T) (string, restore.JournalEntry) {
	t.Helper()
	directory := t.TempDir()
	backup := filepath.Join(directory, "prior.reg")
	if err := os.WriteFile(backup, []byte("Windows Registry Editor Version 5.00\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := restore.JournalEntry{
		TargetPath: `HKCU\Software\Endstate\Target`, TargetExistedBefore: true,
		BackupCreated: true, BackupPath: backup, Action: "restored", RestoreType: "registry-import",
	}
	data, err := json.Marshal(restore.Journal{RunID: "legacy-registry", Entries: []restore.JournalEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "restore-registry.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, entry
}

func testLegacyRegistryImportScratch(target, digest string) (string, string) {
	parts := strings.Split(target, `\`)
	parent := strings.Join(parts[1:len(parts)-1], `\`)
	name := parts[len(parts)-1]
	prefix := `HKCU\` + parent + `\`
	suffix := digest[:16]
	return prefix + "." + name + ".endstate-revert-" + suffix + "-stage",
		prefix + "." + name + ".endstate-revert-" + suffix + "-held"
}
