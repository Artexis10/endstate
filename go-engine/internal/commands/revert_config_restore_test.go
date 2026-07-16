// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestRunRevertInterleavesGenerationAndLegacyMembersInReverseOrdinalOrder(t *testing.T) {
	for _, registered := range []bool{true, false} {
		name := "synthetic-unregistered"
		if registered {
			name = "registered"
		}
		t.Run(name, func(t *testing.T) {
			repoRoot := t.TempDir()
			stateDir := filepath.Join(repoRoot, "state")
			logsDir := filepath.Join(repoRoot, "logs")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			sourceRunID := "apply-source-" + name
			guard, err := configrestore.BeginLive(context.Background(), stateDir, sourceRunID, nil)
			if err != nil {
				t.Fatal(err)
			}

			generationTarget := filepath.Join(repoRoot, "generation-settings.json")
			if err := os.WriteFile(generationTarget, []byte("generation-prior"), 0o600); err != nil {
				t.Fatal(err)
			}
			transactionRoot, err := guard.CreateTransactionRoot("capture-generation")
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := configrestore.PrepareSnapshots(context.Background(), configrestore.SnapshotRequest{
				Set: &configrestore.MaterializedSet{Actions: []configrestore.Action{{
					Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob",
					Target: generationTarget, SnapshotRequired: true,
				}}},
				TransactionRoot: transactionRoot,
			})
			if err != nil {
				t.Fatal(err)
			}
			lineage := configRestoreTestLineage("capture-generation")
			lineage.RunID = sourceRunID
			intent, err := configrestore.PersistJournalIntent(context.Background(), configrestore.JournalIntentRequest{
				Prepared: prepared, TransactionRoot: transactionRoot, Lineage: lineage,
			})
			if err != nil {
				t.Fatal(err)
			}
			transaction, err := configrestore.ExecuteConfigSetTransaction(context.Background(), configrestore.TransactionRequest{
				Prepared: prepared, Intent: intent,
			})
			if err != nil || transaction.Status() != configrestore.TransactionRestored {
				t.Fatalf("transaction = %+v, %v", transaction, err)
			}
			firstLegacyTarget := ""
			if registered {
				firstLegacySource := filepath.Join(repoRoot, "legacy-first-source.json")
				firstLegacyTarget = filepath.Join(repoRoot, "legacy-first-settings.json")
				if err := os.WriteFile(firstLegacySource, []byte("legacy-first-desired"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(firstLegacyTarget, []byte("legacy-first-prior"), 0o600); err != nil {
					t.Fatal(err)
				}
				firstResults, err := restore.RunRestore([]restore.RestoreAction{{
					ID: "legacy-first", Type: "copy", Source: filepath.Base(firstLegacySource), Target: firstLegacyTarget, Backup: true,
				}}, restore.RestoreOptions{
					ManifestDir: repoRoot, BackupDir: filepath.Join(stateDir, "backups", sourceRunID, "first"), RunID: sourceRunID,
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := restore.WriteJournal(logsDir, sourceRunID, "", repoRoot, "", firstResults); err != nil {
					t.Fatal(err)
				}
				standard := filepath.Join(logsDir, "restore-journal-"+sourceRunID+".json")
				firstJournal := filepath.Join(logsDir, "restore-journal-"+sourceRunID+"-first.json")
				if err := os.Rename(standard, firstJournal); err != nil {
					t.Fatal(err)
				}
				if _, err := guard.RegisterLegacyJournal(firstJournal); err != nil {
					t.Fatal(err)
				}
			}

			legacySource := filepath.Join(repoRoot, "legacy-source.json")
			legacyTarget := filepath.Join(repoRoot, "legacy-settings.json")
			if err := os.WriteFile(legacySource, []byte("legacy-desired"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacyTarget, []byte("legacy-prior"), 0o600); err != nil {
				t.Fatal(err)
			}
			legacyResults, err := restore.RunRestore([]restore.RestoreAction{{
				ID: "legacy", Type: "copy", Source: filepath.Base(legacySource), Target: legacyTarget, Backup: true,
			}}, restore.RestoreOptions{
				ManifestDir: repoRoot, BackupDir: filepath.Join(stateDir, "backups", sourceRunID), RunID: sourceRunID,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := restore.WriteJournal(logsDir, sourceRunID, "", repoRoot, "", legacyResults); err != nil {
				t.Fatal(err)
			}
			journalPath := filepath.Join(logsDir, "restore-journal-"+sourceRunID+".json")
			if registered {
				if _, err := guard.RegisterLegacyJournal(journalPath); err != nil {
					t.Fatal(err)
				}
			}
			if err := guard.Close(); err != nil {
				t.Fatal(err)
			}

			originalRoot := resolveRepoRootFn
			resolveRepoRootFn = func() string { return repoRoot }
			t.Cleanup(func() { resolveRepoRootFn = originalRoot })
			got, envErr := RunRevert(RevertFlags{})
			if envErr != nil {
				t.Fatalf("RunRevert: %+v", envErr)
			}
			result := got.(*RevertData)
			wantCount := 2
			if registered {
				wantCount = 3
			}
			if len(result.Results) != wantCount || result.Results[0].Target != legacyTarget || result.Results[len(result.Results)-1].Target != generationTarget {
				t.Fatalf("reverse interleave = %+v", result.Results)
			}
			if registered && result.Results[1].Target != firstLegacyTarget {
				t.Fatalf("multiple legacy order = %+v", result.Results)
			}
			if data, _ := os.ReadFile(legacyTarget); string(data) != "legacy-prior" {
				t.Fatalf("legacy target = %q", data)
			}
			if data, _ := os.ReadFile(generationTarget); string(data) != "generation-prior" {
				t.Fatalf("generation target = %q", data)
			}
			if registered {
				if data, _ := os.ReadFile(firstLegacyTarget); string(data) != "legacy-first-prior" {
					t.Fatalf("first legacy target = %q", data)
				}
			}
			if err := os.WriteFile(legacyTarget, []byte("user-edit-after-revert"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, secondErr := RunRevert(RevertFlags{}); secondErr == nil {
				t.Fatal("consumed legacy journal was replayed")
			}
			if data, _ := os.ReadFile(legacyTarget); string(data) != "user-edit-after-revert" {
				t.Fatalf("second revert overwrote user edit = %q", data)
			}
		})
	}
}

func TestEmitConfigRevertResultsUsesContractedRestoreLifecycle(t *testing.T) {
	buffer := &bytes.Buffer{}
	emitter := events.NewEmitterWithWriter("revert-events", true, buffer)
	emitter.EmitPhase("restore")
	emitConfigRevertResults(emitter, []restore.RevertResult{
		{Target: "restored-target", Action: "reverted"},
		{Target: "deleted-target", Action: "deleted"},
		{Target: "skipped-target", Action: "skipped"},
	}, false)
	lines := bytes.Split(bytes.TrimSpace(buffer.Bytes()), []byte("\n"))
	if len(lines) != 5 {
		t.Fatalf("events = %s", buffer.String())
	}
	decoded := make([]map[string]any, len(lines))
	for index, line := range lines {
		if err := json.Unmarshal(line, &decoded[index]); err != nil {
			t.Fatal(err)
		}
	}
	if decoded[0]["phase"] != "restore" || decoded[1]["status"] != "installed" ||
		decoded[2]["status"] != "installed" || decoded[3]["status"] != "skipped" ||
		decoded[4]["event"] != "summary" || decoded[4]["phase"] != "restore" {
		t.Fatalf("revert lifecycle = %#v", decoded)
	}

	buffer.Reset()
	emitConfigRevertResults(emitter, nil, true)
	var summary map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["phase"] != "restore" || summary["total"] != float64(1) || summary["failed"] != float64(1) {
		t.Fatalf("failed revert summary = %#v", summary)
	}
}

func TestDurableLegacyRevertCompletionClosesCrashBeforeConsumptionWindow(t *testing.T) {
	ctx := context.Background()
	repoRoot := t.TempDir()
	stateDir := filepath.Join(repoRoot, "state")
	target := filepath.Join(repoRoot, "settings.json")
	backup := filepath.Join(repoRoot, "settings.backup.json")
	if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := &restore.Journal{RunID: "apply-before-crash", Entries: []restore.JournalEntry{{
		TargetPath: target, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: backup, Action: "restored", RestoreType: "copy",
	}}}
	journalData, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(repoRoot, "restore-journal-apply-before-crash.json")
	if err := os.WriteFile(journalPath, journalData, 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := configrestore.BeginLive(ctx, stateDir, "revert-crashes-before-consumption", nil)
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
	if _, err := restore.RunRevertDurable(journal, "", workRoot); err != nil {
		t.Fatal(err)
	}
	// Simulate process death before MarkLegacyMemberReverted.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("post-crash-user-edit"), 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := configrestore.BeginLive(ctx, stateDir, "revert-resume", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	runs, err := second.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("active member after crash = %+v, %v", runs, err)
	}
	member = runs[0].Members()[0]
	workRoot, err = second.LegacyMemberRevertRoot(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restore.RunRevertDurable(journal, "", workRoot); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "post-crash-user-edit" {
		t.Fatalf("resume overwrote user edit = %q, %v", data, err)
	}
	if err := second.MarkLegacyMemberReverted(ctx, member); err != nil {
		t.Fatal(err)
	}
}

func TestRunRevertChoosesNewerStandaloneLegacyJournalBeforeOlderStoreRun(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := filepath.Join(repoRoot, "state")
	logsDir := filepath.Join(repoRoot, "logs")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	guard, err := configrestore.BeginLive(context.Background(), stateDir, "older-generation-run", nil)
	if err != nil {
		t.Fatal(err)
	}
	generationTarget := filepath.Join(repoRoot, "generation-settings.json")
	if err := os.WriteFile(generationTarget, []byte("generation-prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := guard.CreateTransactionRoot("capture-generation")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := configrestore.PrepareSnapshots(context.Background(), configrestore.SnapshotRequest{
		Set: &configrestore.MaterializedSet{Actions: []configrestore.Action{{
			Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob", Target: generationTarget, SnapshotRequired: true,
		}}}, TransactionRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := configRestoreTestLineage("capture-generation")
	lineage.RunID = "older-generation-run"
	intent, err := configrestore.PersistJournalIntent(context.Background(), configrestore.JournalIntentRequest{
		Prepared: prepared, TransactionRoot: root, Lineage: lineage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configrestore.ExecuteConfigSetTransaction(context.Background(), configrestore.TransactionRequest{
		Prepared: prepared, Intent: intent,
	}); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}

	legacyTarget := filepath.Join(repoRoot, "legacy-settings.json")
	legacyBackup := filepath.Join(repoRoot, "legacy-settings.backup")
	if err := os.WriteFile(legacyTarget, []byte("legacy-desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyBackup, []byte("legacy-prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := restore.Journal{
		RunID: "newer-standalone-legacy", Timestamp: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		Entries: []restore.JournalEntry{{
			TargetPath: legacyTarget, TargetExistedBefore: true, BackupCreated: true,
			BackupPath: legacyBackup, Action: "restored", RestoreType: "copy",
		}},
	}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "restore-journal-newer-standalone-legacy.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	originalRoot := resolveRepoRootFn
	resolveRepoRootFn = func() string { return repoRoot }
	t.Cleanup(func() { resolveRepoRootFn = originalRoot })
	got, envErr := RunRevert(RevertFlags{})
	if envErr != nil {
		t.Fatalf("RunRevert: %+v", envErr)
	}
	result := got.(*RevertData)
	if len(result.Results) != 1 || result.Results[0].Target != legacyTarget {
		t.Fatalf("newest history selection = %+v", result.Results)
	}
	if data, _ := os.ReadFile(legacyTarget); string(data) != "legacy-prior" {
		t.Fatalf("legacy target = %q", data)
	}
	if _, err := os.Stat(generationTarget); !os.IsNotExist(err) {
		t.Fatalf("older generation was reverted before newer legacy history: %v", err)
	}
	inspection, err := configrestore.BeginLive(context.Background(), stateDir, "chronology-inspection", nil)
	if err != nil {
		t.Fatal(err)
	}
	activeRuns, err := inspection.ActiveStoreRuns(context.Background())
	if closeErr := inspection.Close(); err == nil {
		err = closeErr
	}
	if err != nil || len(activeRuns) == 0 {
		t.Fatalf("active store runs = %d, %v", len(activeRuns), err)
	}
	sameSecond := &restore.Journal{
		RunID: "different-run", Timestamp: activeRuns[0].StartedAt().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	if _, err := newerStandaloneLegacyJournal(activeRuns[0], sameSecond); err == nil {
		t.Fatal("same-second history was treated as safely ordered")
	}

	// The standalone journal is still the newest visible legacy history. If its
	// timestamp becomes unorderable, revert must fail closed instead of falling
	// through to the older active generation run.
	journal.Timestamp = "not-a-timestamp"
	data, err = json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "restore-journal-newer-standalone-legacy.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyTarget, []byte("legacy-desired-again"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, envErr = RunRevert(RevertFlags{})
	if envErr == nil {
		t.Fatal("malformed chronology did not fail closed")
	}
	detail, ok := envErr.Detail.(map[string]string)
	if !ok || detail["reason"] != "history_order_unknown" {
		t.Fatalf("chronology detail = %#v", envErr.Detail)
	}
	if data, _ := os.ReadFile(legacyTarget); string(data) != "legacy-desired-again" {
		t.Fatalf("failed chronology mutated newer legacy target = %q", data)
	}
	if _, err := os.Stat(generationTarget); !os.IsNotExist(err) {
		t.Fatalf("failed chronology mutated older generation target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "restore-journal-newer-standalone-legacy.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, envErr = RunRevert(RevertFlags{})
	if envErr == nil {
		t.Fatal("unreadable latest journal did not fail closed")
	}
	detail, ok = envErr.Detail.(map[string]string)
	if !ok || detail["reason"] != "history_order_unknown" {
		t.Fatalf("unreadable history detail = %#v", envErr.Detail)
	}
	if data, _ := os.ReadFile(legacyTarget); string(data) != "legacy-desired-again" {
		t.Fatalf("unreadable history mutated newer legacy target = %q", data)
	}
	if _, err := os.Stat(generationTarget); !os.IsNotExist(err) {
		t.Fatalf("unreadable history mutated older generation target: %v", err)
	}
}
