// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/upload"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/schedule"
)

func replayQueue(t *testing.T) (string, *schedule.Config) {
	t.Helper()
	stateDir := t.TempDir()
	artifact := filepath.Join(stateDir, "schedule", "pending", "capture.json")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("capture"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("capture"))
	return stateDir, &schedule.Config{PendingUploads: []schedule.PendingUpload{{Artifact: artifact, SHA256: fmt.Sprintf("%x", sum)}}}
}

func withReplayScheduleSeams(t *testing.T, fn func(BackupFlags) (interface{}, *envelope.Error)) {
	t.Helper()
	oldBackup, oldWrite := scheduleBackupFn, scheduleWriteConfigFn
	scheduleBackupFn = fn
	scheduleWriteConfigFn = func(string, *schedule.Config) error { return nil }
	t.Cleanup(func() { scheduleBackupFn, scheduleWriteConfigFn = oldBackup, oldWrite })
}

func TestScheduleReplay_RestartReusesPersistedOperationAndSpool(t *testing.T) {
	stateDir, cfg := replayQueue(t)
	spool := filepath.Join(stateDir, "schedule", "create-spool", "op-1.json")
	var first upload.ScheduledCreate
	withReplayScheduleSeams(t, func(flags BackupFlags) (interface{}, *envelope.Error) {
		first = *flags.ScheduledCreate
		first.BackupID, first.OperationID, first.CreateSpool = "b-1", "op-1", spool
		if err := first.Persist(first); err != nil {
			t.Fatal(err)
		}
		return nil, envelope.NewError(envelope.ErrBackendUnreachable, "lost response")
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "offline" {
		t.Fatalf("first outcome = %q", got.Outcome)
	}
	withReplayScheduleSeams(t, func(flags BackupFlags) (interface{}, *envelope.Error) {
		got := flags.ScheduledCreate
		if got.BackupID != "b-1" || got.OperationID != "op-1" || got.CreateSpool != spool {
			t.Fatalf("restart state = %#v", got)
		}
		return &PushResult{BackupID: "b-1", VersionID: "v-1"}, nil
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "pushed" {
		t.Fatalf("restart outcome = %q", got.Outcome)
	}
}

func TestScheduleReplay_CommittedDrainHasNoPUTWork(t *testing.T) {
	stateDir, cfg := replayQueue(t)
	cfg.PendingUploads[0].BackupID, cfg.PendingUploads[0].OperationID = "b-1", "op-1"
	cfg.PendingUploads[0].CreateSpool = filepath.Join(stateDir, "schedule", "create-spool", "op-1.json")
	puts := 0
	withReplayScheduleSeams(t, func(flags BackupFlags) (interface{}, *envelope.Error) {
		if flags.ScheduledCreate.OperationID != "op-1" {
			t.Fatal("missing persisted operation")
		}
		return &PushResult{BackupID: "b-1", VersionID: "v-1"}, nil // committed replay is terminal: no PUT callback runs.
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "pushed" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	if puts != 0 || len(cfg.PendingUploads) != 0 {
		t.Fatalf("puts=%d queue=%d", puts, len(cfg.PendingUploads))
	}
}

func TestScheduleReplay_CapabilityLossFailsClosedWithoutCreate(t *testing.T) {
	stateDir, cfg := replayQueue(t)
	cfg.PendingUploads[0].BackupID, cfg.PendingUploads[0].OperationID, cfg.PendingUploads[0].CreateSpool = "b-1", "op-1", filepath.Join(stateDir, "schedule", "create-spool", "op-1.json")
	posts := 0
	withReplayScheduleSeams(t, func(BackupFlags) (interface{}, *envelope.Error) {
		posts++
		return nil, envelope.NewError(envelope.ErrBackendIncompatible, "capability disappeared")
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "error" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	if posts != 1 || cfg.PendingUploads[0].OperationID != "op-1" {
		t.Fatalf("posts=%d queue=%#v", posts, cfg.PendingUploads)
	}
}

func TestScheduleReplay_LegacyDefiniteFailureClearsThenRetries(t *testing.T) {
	stateDir, cfg := replayQueue(t)
	withReplayScheduleSeams(t, func(flags BackupFlags) (interface{}, *envelope.Error) {
		state := *flags.ScheduledCreate
		state.BackupID, state.LegacyCreateStarted = "b-1", true
		if err := state.Persist(state); err != nil {
			t.Fatal(err)
		}
		state.LegacyCreateStarted = false
		if err := state.Persist(state); err != nil {
			t.Fatal(err)
		}
		return nil, envelope.NewError(envelope.ErrSubscriptionRequired, "subscription")
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "subscription_required" {
		t.Fatalf("outcome=%q", got.Outcome)
	}
	withReplayScheduleSeams(t, func(flags BackupFlags) (interface{}, *envelope.Error) {
		if flags.ScheduledCreate.LegacyCreateStarted {
			t.Fatal("definite 402 marker was not cleared")
		}
		return &PushResult{BackupID: "b-1", VersionID: "v-1"}, nil
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "pushed" {
		t.Fatalf("retry=%q", got.Outcome)
	}
}

func TestScheduleReplay_LegacyAmbiguityDoesNotPostTwice(t *testing.T) {
	stateDir, cfg := replayQueue(t)
	posts := 0
	withReplayScheduleSeams(t, func(flags BackupFlags) (interface{}, *envelope.Error) {
		posts++
		state := *flags.ScheduledCreate
		state.BackupID, state.LegacyCreateStarted = "b-1", true
		if err := state.Persist(state); err != nil {
			t.Fatal(err)
		}
		return nil, envelope.NewError(envelope.ErrBackendUnreachable, "transport ambiguous")
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "offline" {
		t.Fatalf("first=%q", got.Outcome)
	}
	withReplayScheduleSeams(t, func(flags BackupFlags) (interface{}, *envelope.Error) {
		if flags.ScheduledCreate.LegacyCreateStarted {
			return nil, envelope.NewError(envelope.ErrBackupUploadUncertain, "legacy-create-ambiguous")
		}
		posts++
		return nil, nil
	})
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "upload_uncertain" {
		t.Fatalf("outcome=%q", got.Outcome)
	}
	if posts != 1 {
		t.Fatalf("legacy ambiguity posted %d times", posts)
	}
}

func TestPruneScheduledCreateSpools_RetriesOrphansAndNeverLeavesStateRoot(t *testing.T) {
	stateDir, cfg := replayQueue(t)
	dir := filepath.Join(stateDir, "schedule", "create-spool")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, "orphan.json")
	kept := filepath.Join(dir, "kept.json")
	external := filepath.Join(t.TempDir(), "external.json")
	for _, p := range []string{orphan, kept, external, filepath.Join(dir, "nested", "inside.json")} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg.PendingUploads[0].CreateSpool = kept
	orig := scheduleRemoveArtifactFn
	fail := true
	scheduleRemoveArtifactFn = func(path string) error {
		if path == orphan && fail {
			return fmt.Errorf("transient")
		}
		return os.Remove(path)
	}
	t.Cleanup(func() { scheduleRemoveArtifactFn = orig })
	if err := pruneScheduledCreateSpools(stateDir, cfg); err == nil {
		t.Fatal("transient removal succeeded")
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatal("orphan removed despite transient failure")
	}
	fail = false
	if err := pruneScheduledCreateSpools(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan not removed on later sweep")
	}
	for _, p := range []string{kept, external, filepath.Join(dir, "nested", "inside.json")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("unsafe prune touched %q: %v", p, err)
		}
	}
}
