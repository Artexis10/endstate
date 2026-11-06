// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeSettingsValidationDoesNotAdoptAnAbsentTransfer(t *testing.T) {
	stage, host := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(stage, "selected"), "--hidden\n")
	writeTestFile(t, filepath.Join(host, "absent"), "--pre=local-command\n")
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{
		{Type: "copy", Source: "selected", Target: instancePath("selected"), Optional: true},
		{Type: "copy", Source: "absent", Target: instancePath("absent"), Optional: true},
	}, Validate: []modules.ValidationDef{
		{Type: "settings-codec", Path: "selected", Codec: settingscodec.RipgrepPortableArgumentsV1, Optional: true},
		{Type: "settings-codec", Path: "absent", Codec: settingscodec.RipgrepPortableArgumentsV1, Optional: true},
	}}
	got, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g2"}, Plan: testPlan(host, generation)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Actions) != 1 || len(got.Validations) != 1 || got.Validations[0].Definition.Optional {
		t.Fatalf("transferred-file validation was optional or adopted absent source: %+v", got.Validations)
	}
}

func TestSettingsCodecSurvivesJournalAndRejectsCommittedContent(t *testing.T) {
	host, transactionRoot := t.TempDir(), t.TempDir()
	target := filepath.Join(host, "config")
	writeTestFile(t, target, "--hidden\n")
	source := filepath.Join(t.TempDir(), "config")
	writeTestFile(t, source, "--pre=external-command\n")
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: &MaterializedSet{
		Actions:     []Action{{Kind: ActionCopy, Strategy: "copy", Source: source, Target: target, SourceMode: info.Mode(), SnapshotRequired: true}},
		Validations: []configvalidate.ResolvedValidation{{Definition: modules.ValidationDef{Type: "settings-codec", Path: "config", Codec: settingscodec.RipgrepPortableArgumentsV1}, HostPath: target}},
	}, TransactionRoot: transactionRoot})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent})
	if err == nil {
		t.Fatal("unsupported committed content did not report a validation error")
	}
	if result.Status() != TransactionRolledBack || result.Reason() != ReasonTargetValidationFailed {
		t.Fatalf("unsupported committed content survived: status %s reason %s", result.Status(), result.Reason())
	}
	assertTestFile(t, target, "--hidden\n")
}
