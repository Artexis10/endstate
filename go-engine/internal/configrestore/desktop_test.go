// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type desktopTestBackend struct {
	scope     string
	value     desktopsettings.Value
	unrelated bool
	writes    int
}

func (b *desktopTestBackend) Read(context.Context, desktopsettings.Target) (desktopsettings.Observation, error) {
	effective := true
	return desktopsettings.Observation{User: b.value.Clone(), Effective: &effective, Scope: b.scope}, nil
}
func (b *desktopTestBackend) Write(_ context.Context, _ desktopsettings.Target, scope string, v desktopsettings.Value) error {
	if scope != b.scope {
		return desktopsettings.ErrConflict
	}
	b.writes++
	b.value = v.Clone()
	return nil
}
func desktopTestTarget() desktopsettings.Target {
	return desktopsettings.Target{Schema: "org.gnome.desktop.interface", Path: "/org/gnome/desktop/interface/", Key: "clock-show-date", Type: "boolean"}
}

// A crash after mutation must use durable prior state, with no live desktop
// needed for inspection. A missing boundary still blocks subsequent mutations.
func TestDesktopInterruptedMutationRecovery(t *testing.T) {
	state := t.TempDir()
	backend := &desktopTestBackend{scope: "fixture/user-db", value: desktopsettings.Value{Type: "boolean"}, unrelated: true}
	ctx := desktopsettings.WithBackend(context.Background(), backend)
	guard, err := BeginLive(ctx, state, "desktop-interrupted", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := guard.CreateTransactionRoot("capture-clock")
	if err != nil {
		t.Fatal(err)
	}
	target := desktopTestTarget()
	value := false
	desired := desktopsettings.Value{Type: "boolean", Present: true, Value: &value}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{TransactionRoot: root, Set: &MaterializedSet{Actions: []Action{{Kind: ActionDesktopValue, Strategy: "desktop-value", Target: target.Identity(), DesktopTarget: &target, DesktopValue: &desired, SnapshotRequired: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID = "desktop-interrupted"
	lineage.CaptureID = "capture-clock"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	diskBytes, err := os.ReadFile(intent.Path())
	if err != nil {
		t.Fatal(err)
	}
	disk, err := decodeJournalIntent(diskBytes)
	if err != nil || disk.Version != 2 {
		t.Fatalf("desktop intent version=%d err=%v", disk.Version, err)
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
	if !backend.value.Equal(desired) {
		t.Fatal("interruption did not retain desired value")
	}
	offline, err := ReadJournalIntent(context.Background(), root)
	if err != nil || offline.Actions()[0].Prior.Desktop.Present || !offline.Actions()[0].Desired.Desktop.Equal(desired) {
		t.Fatalf("offline inspection of pending desktop: %+v %v", offline, err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := BeginLive(context.Background(), state, "without-boundary", nil); unexpected != nil || !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("pending desktop bypassed recovery stop: %v %v", unexpected, err)
	}
	for _, conflict := range []string{"third-value", "scope"} {
		beforeWrites := backend.writes
		if conflict == "scope" {
			backend.scope = "fixture/another-user-db"
		} else {
			other := true
			backend.value = desktopsettings.Value{Type: "boolean", Present: true, Value: &other}
		}
		before := backend.value.Clone()
		if unexpected, err := BeginLive(ctx, state, "conflicting-recovery", nil); unexpected != nil || !errors.Is(err, ErrRecoveryRequired) || !errors.Is(err, desktopsettings.ErrConflict) {
			t.Fatalf("pending %s conflict bypassed recovery stop: %v %v", conflict, unexpected, err)
		}
		if backend.writes != beforeWrites || !backend.value.Equal(before) {
			t.Fatal("recovery overwrote another owner")
		}
		backend.scope = "fixture/user-db"
		backend.value = desired.Clone()
	}

	recovered, err := BeginLive(ctx, state, "recover-clock", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if backend.value.Present || !backend.unrelated {
		t.Fatal("recovery lost prior absence or unrelated preference")
	}
	marker, err := readJournalMarkerFile(root, journalMarkerPath(filepath.Join(root, "journal"), JournalRolledBack, intent.Digest()), intent)
	if err != nil || marker.State() != JournalRolledBack {
		t.Fatalf("recovery terminal state: %+v %v", marker, err)
	}
}

// Terminal publication failure must roll back the value before reporting failure.
func TestDesktopTerminalJournalFailureRestoresPriorFalse(t *testing.T) {
	value := false
	prior := desktopsettings.Value{Type: "boolean", Present: true, Value: &value}
	backend := &desktopTestBackend{scope: "fixture/user-db", value: prior}
	ctx := desktopsettings.WithBackend(context.Background(), backend)
	target := desktopTestTarget()
	desired := desktopsettings.Value{Type: "boolean"}
	root := t.TempDir()
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{TransactionRoot: root, Set: &MaterializedSet{Actions: []Action{{Kind: ActionDesktopValue, Strategy: "desktop-value", Target: target.Identity(), DesktopTarget: &target, DesktopValue: &desired, SnapshotRequired: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: testJournalLineage()})
	if err != nil {
		t.Fatal(err)
	}
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		if phase == transactionPhaseBeforeCommittedMarker {
			return fmt.Errorf("terminal publication failed")
		}
		return nil
	}
	result, err := executor.Execute(ctx, TransactionRequest{Prepared: prepared, Intent: intent})
	if err == nil || result.Status() != TransactionRolledBack || !backend.value.Equal(prior) || backend.writes != 2 {
		t.Fatalf("terminal failure: result=%+v value=%+v writes=%d err=%v", result, backend.value, backend.writes, err)
	}
}

// A transferred payload cannot replace the trusted target with its own address.
func TestMaterializeDesktopRejectsPayloadTargetInjection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "clock.json"), []byte("{\"type\":\"boolean\",\"present\":true,\"value\":false,\"target\":\"other-key\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	target := desktopTestTarget()
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{Type: "desktop-value", Source: "clock.json", Desktop: &target}}}
	if set, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: root, TargetGeneration: "g2"}, Plan: testPlan("", generation)}); set != nil || CodeOf(err) != CodeMaterialization {
		t.Fatalf("payload acquired target authority: %+v %v", set, err)
	}
}
