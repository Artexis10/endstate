// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestExecuteLiveConfigRestoreSetPersistsIntentBeforeTransaction(t *testing.T) {
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	sequence := []string{}
	originalPersist := persistConfigRestoreJournalIntentFn
	originalExecute := executeConfigRestoreTransactionFn
	persistConfigRestoreJournalIntentFn = func(ctx context.Context, request configrestore.JournalIntentRequest) (*configrestore.JournalIntent, error) {
		sequence = append(sequence, "intent")
		return originalPersist(ctx, request)
	}
	executeConfigRestoreTransactionFn = func(ctx context.Context, request configrestore.TransactionRequest) (*configrestore.TransactionResult, error) {
		sequence = append(sequence, "execute")
		return originalExecute(ctx, request)
	}
	t.Cleanup(func() {
		persistConfigRestoreJournalIntentFn = originalPersist
		executeConfigRestoreTransactionFn = originalExecute
	})

	outcome := executeLiveConfigRestoreSet(context.Background(), configRestoreLiveSetRequest{
		Materialized: &configrestore.MaterializedSet{Actions: []configrestore.Action{{
			Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}},
		TransactionRoot: root,
		Lineage:         configRestoreTestLineage("capture-a"),
	})
	if outcome.Status != planner.StatusRestored || outcome.Reason != nil || outcome.Err != nil || !outcome.CanContinue {
		t.Fatalf("outcome = %+v", outcome)
	}
	if strings.Join(sequence, ",") != "intent,execute" {
		t.Fatalf("sequence = %v", sequence)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target was not committed: %v", err)
	}
}

func TestExecuteLiveConfigRestoreSetJournalFailurePreventsTransaction(t *testing.T) {
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	executeCalled := false
	originalPersist := persistConfigRestoreJournalIntentFn
	originalExecute := executeConfigRestoreTransactionFn
	persistConfigRestoreJournalIntentFn = func(context.Context, configrestore.JournalIntentRequest) (*configrestore.JournalIntent, error) {
		return nil, errors.New("disk full")
	}
	executeConfigRestoreTransactionFn = func(context.Context, configrestore.TransactionRequest) (*configrestore.TransactionResult, error) {
		executeCalled = true
		return nil, errors.New("must not execute")
	}
	t.Cleanup(func() {
		persistConfigRestoreJournalIntentFn = originalPersist
		executeConfigRestoreTransactionFn = originalExecute
	})

	outcome := executeLiveConfigRestoreSet(context.Background(), configRestoreLiveSetRequest{
		Materialized: &configrestore.MaterializedSet{Actions: []configrestore.Action{{
			Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}},
		TransactionRoot: t.TempDir(),
		Lineage:         configRestoreTestLineage("capture-a"),
	})
	if executeCalled || outcome.Status != planner.StatusFailed || outcome.Reason == nil ||
		*outcome.Reason != planner.ReasonJournalIntentFailed || outcome.CanContinue || outcome.Err == nil {
		t.Fatalf("outcome = %+v executeCalled=%v", outcome, executeCalled)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("journal failure changed target: %v", err)
	}
}

func configRestoreTestLineage(captureID string) configrestore.JournalLineage {
	digest := strings.Repeat("a", 64)
	return configrestore.JournalLineage{
		RunID: "restore-test", CaptureID: captureID, ModuleID: "apps.example", ConfigSetID: "preferences",
		TargetInstanceID: "instance-target", SourceGeneration: "g1", TargetGeneration: "g1",
		MigrationPath: []string{}, SourceGenerationFingerprint: digest,
		CaptureModuleRevision: digest, RestoreModuleRevision: digest,
	}
}
