// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings/dconf"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings/dconf/dconftest"
)

const dconfClockKey = "/org/gnome/desktop/interface/clock-show-date"

// A crash after `dconf write` leaves the pinned value behind. Recovery must reset
// the key to absence through the journaled dconf scope, which the real backend
// must reproduce after a restart. TestDesktopInterruptedMutationRecovery covers
// the stops for a missing boundary, a third value, and a changed scope.
func TestTypedValueTransactionRecoversInterruptedMutation(t *testing.T) {
	session := dconftest.New(t)
	ctx := desktopsettings.WithBackend(context.Background(), dconf.New(session.Host()))
	state := t.TempDir()
	guard, err := BeginLive(ctx, state, "dconf-interrupted", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := guard.CreateTransactionRoot("capture-clock")
	if err != nil {
		t.Fatal(err)
	}
	target := desktopTestTarget()
	pinned := false
	desired := desktopsettings.Value{Type: "boolean", Present: true, Value: &pinned}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{TransactionRoot: root, Set: &MaterializedSet{Actions: []Action{{Kind: ActionDesktopValue, Strategy: "desktop-value", Target: target.Identity(), DesktopTarget: &target, DesktopValue: &desired, SnapshotRequired: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID = "dconf-interrupted"
	lineage.CaptureID = "capture-clock"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		if phase == transactionPhaseAfterCommitMutation {
			panic("simulated process interruption")
		}
		return nil
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("crash checkpoint did not run")
			}
		}()
		executor.Execute(ctx, TransactionRequest{Prepared: prepared, Intent: intent})
	}()
	if session.Values[dconfClockKey] != "false" || session.Writes != 1 {
		t.Fatalf("interruption did not leave the pinned value: %v writes=%d", session.Values, session.Writes)
	}
	if offline, err := ReadJournalIntent(context.Background(), root); err != nil || offline.Actions()[0].Prior.Desktop.Present {
		t.Fatalf("offline inspection of the pending write: %+v %v", offline, err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := BeginLive(ctx, state, "recover-clock", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if _, pinnedStill := session.Values[dconfClockKey]; pinnedStill || session.Writes != 2 {
		t.Fatalf("recovery did not reset the key to absence: %v writes=%d", session.Values, session.Writes)
	}
	if len(session.Rejected) != 0 {
		t.Fatalf("unexpected commands: %q", session.Rejected)
	}
	marker, err := readJournalMarkerFile(root, journalMarkerPath(filepath.Join(root, "journal"), JournalRolledBack, intent.Digest()), intent)
	if err != nil || marker.State() != JournalRolledBack {
		t.Fatalf("recovery terminal state: %+v %v", marker, err)
	}
}
