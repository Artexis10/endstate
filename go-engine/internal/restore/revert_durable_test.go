// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRevertDurableResumesAfterMutationBeforeCompletion(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings.json")
	backup := filepath.Join(root, "settings.backup.json")
	if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: target, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: backup, Action: "restored", RestoreType: "copy",
	}}}
	workRoot := t.TempDir()
	originalCheckpoint := durableRevertCheckpoint
	fired := false
	durableRevertCheckpoint = func(phase string, _ int) error {
		if phase == "after_target_replaced" && !fired {
			fired = true
			return errors.New("simulated crash")
		}
		return nil
	}
	t.Cleanup(func() { durableRevertCheckpoint = originalCheckpoint })

	if _, err := RunRevertDurable(journal, "", workRoot); err == nil || !strings.Contains(err.Error(), "simulated crash") {
		t.Fatalf("first revert error = %v", err)
	}
	durableRevertCheckpoint = originalCheckpoint
	results, err := RunRevertDurable(journal, "", workRoot)
	if err != nil || len(results) != 1 || results[0].Action != "reverted" {
		t.Fatalf("resumed results = %+v, %v", results, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "prior" {
		t.Fatalf("resumed target = %q, %v", data, err)
	}

	if err := os.WriteFile(target, []byte("post-revert-user-edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunRevertDurable(journal, "", workRoot); err != nil {
		t.Fatalf("completed replay = %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "post-revert-user-edit" {
		t.Fatalf("completed replay overwrote target = %q, %v", data, err)
	}
}

func TestRunRevertDurableFailsClosedOnEditAfterCrash(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings.json")
	backup := filepath.Join(root, "settings.backup.json")
	if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: target, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: backup, Action: "restored", RestoreType: "copy",
	}}}
	workRoot := t.TempDir()
	originalCheckpoint := durableRevertCheckpoint
	durableRevertCheckpoint = func(phase string, _ int) error {
		if phase == "after_target_replaced" {
			return errors.New("simulated crash")
		}
		return nil
	}
	t.Cleanup(func() { durableRevertCheckpoint = originalCheckpoint })
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil {
		t.Fatal("first revert unexpectedly completed")
	}
	if err := os.WriteFile(target, []byte("user-edit-after-crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	durableRevertCheckpoint = originalCheckpoint
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("retry error = %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "user-edit-after-crash" {
		t.Fatalf("retry overwrote user edit = %q, %v", data, err)
	}
}

func TestRunRevertDurablePreparesEveryEntryBeforeFirstMutation(t *testing.T) {
	root := t.TempDir()
	firstTarget := filepath.Join(root, "first.json")
	firstBackup := filepath.Join(root, "first.backup.json")
	secondTarget := filepath.Join(root, "second.json")
	secondBackup := filepath.Join(root, "second.backup.json")
	for path, content := range map[string]string{
		firstTarget: "first-desired", firstBackup: "first-prior",
		secondTarget: "second-desired", secondBackup: "second-prior",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journal := &Journal{Entries: []JournalEntry{
		{TargetPath: firstTarget, TargetExistedBefore: true, BackupCreated: true, BackupPath: firstBackup, Action: "restored", RestoreType: "copy"},
		{TargetPath: secondTarget, TargetExistedBefore: true, BackupCreated: true, BackupPath: secondBackup, Action: "restored", RestoreType: "copy"},
	}}
	originalCheckpoint := durableRevertCheckpoint
	durableRevertCheckpoint = func(phase string, index int) error {
		if phase == "after_entry_completed" && index == 1 {
			return errors.New("simulated crash between entries")
		}
		return nil
	}
	t.Cleanup(func() { durableRevertCheckpoint = originalCheckpoint })
	workRoot := t.TempDir()
	results, err := RunRevertDurable(journal, "", workRoot)
	if err == nil || len(results) != 1 || results[0].Target != secondTarget {
		t.Fatalf("first pass results = %+v, %v", results, err)
	}
	if err := os.WriteFile(firstTarget, []byte("user-edit-after-crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	durableRevertCheckpoint = originalCheckpoint
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("retry error = %v", err)
	}
	if data, err := os.ReadFile(firstTarget); err != nil || string(data) != "user-edit-after-crash" {
		t.Fatalf("retry overwrote unstarted entry edit = %q, %v", data, err)
	}
}

func TestRunRevertDurablePreflightsRepeatedTargetAsStateChain(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings.json")
	firstBackup := filepath.Join(root, "first.backup.json")
	secondBackup := filepath.Join(root, "second.backup.json")
	for path, content := range map[string]string{
		target: "after-second", firstBackup: "original", secondBackup: "after-first",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journal := &Journal{Entries: []JournalEntry{
		{TargetPath: target, TargetExistedBefore: true, BackupCreated: true, BackupPath: firstBackup, Action: "restored", RestoreType: "copy"},
		{TargetPath: target, TargetExistedBefore: true, BackupCreated: true, BackupPath: secondBackup, Action: "restored", RestoreType: "copy"},
	}}
	results, err := RunRevertDurable(journal, "", t.TempDir())
	if err != nil || len(results) != 2 {
		t.Fatalf("repeated-target results = %+v, %v", results, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "original" {
		t.Fatalf("repeated-target final state = %q, %v", data, err)
	}
}

func TestRunRevertDurableReconcilesAbandonedRecordPublicationTemp(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings.json")
	backup := filepath.Join(root, "settings.backup.json")
	if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	workRoot := t.TempDir()
	abandoned := filepath.Join(workRoot, ".legacy-revert-record-12345.tmp")
	if err := os.WriteFile(abandoned, []byte("torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: target, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: backup, Action: "restored", RestoreType: "copy",
	}}}
	if _, err := RunRevertDurable(journal, "", workRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Fatalf("abandoned publication temp survived: %v", err)
	}
}

func TestRunRevertDurableResumesDirectorySwapAfterOriginalIsHeld(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings")
	backup := filepath.Join(root, "backup")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "desired.json"), []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "prior.json"), []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: target, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: backup, Action: "restored", RestoreType: "copy",
	}}}
	workRoot := t.TempDir()
	originalCheckpoint := durableRevertCheckpoint
	fired := false
	durableRevertCheckpoint = func(phase string, _ int) error {
		if phase == "after_target_held" && !fired {
			fired = true
			return errors.New("simulated crash")
		}
		return nil
	}
	t.Cleanup(func() { durableRevertCheckpoint = originalCheckpoint })
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil {
		t.Fatal("first revert unexpectedly completed")
	}
	durableRevertCheckpoint = originalCheckpoint
	if _, err := RunRevertDurable(journal, "", workRoot); err != nil {
		t.Fatalf("resumed directory revert = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(target, "prior.json")); err != nil || string(data) != "prior" {
		t.Fatalf("directory target = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(target, "desired.json")); !os.IsNotExist(err) {
		t.Fatalf("desired-only file survived revert: %v", err)
	}
}

func TestRunRevertDurableRejectsLinkedTargetParent(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realParent, "settings.json")
	if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup.json")
	if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linkedParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	outsideTarget := filepath.Join(outside, "settings.json")
	if err := os.WriteFile(outsideTarget, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: filepath.Join(linkedParent, "settings.json"), TargetExistedBefore: true,
		BackupCreated: true, BackupPath: backup, Action: "restored", RestoreType: "copy",
	}}}
	if _, err := RunRevertDurable(journal, "", t.TempDir()); err == nil || !strings.Contains(strings.ToLower(err.Error()), "link") {
		t.Fatalf("linked parent error = %v", err)
	}
	if data, err := os.ReadFile(outsideTarget); err != nil || string(data) != "keep" {
		t.Fatalf("linked parent target changed = %q, %v", data, err)
	}
}
