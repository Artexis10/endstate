// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestV2StorageRejectsMissingExtraAndRepeatTransactionDrift(t *testing.T) {
	t.Run("missing transaction", func(t *testing.T) {
		runtime, _, _ := v2EvidenceFixture(t)
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 0, before, v2RebuildEvidence{}); failure == nil || failure.Coordinate != "transactions" {
			t.Fatalf("missing transaction was accepted: %+v", failure)
		}
	})

	t.Run("extra transaction", func(t *testing.T) {
		runtime, _, _ := v2EvidenceFixture(t)
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		for _, id := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
			path := filepath.Join(runtime.Root, "state", "config-restore", "v1", "transactions", id, "transaction.json")
			writeV2TestFile(t, path, []byte(`{}`))
		}
		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 0, before, v2RebuildEvidence{}); failure == nil || failure.Coordinate != "transactions" {
			t.Fatalf("extra transaction was accepted: %+v", failure)
		}
	})

	t.Run("missing intent", func(t *testing.T) {
		runtime, _, item := v2EvidenceFixture(t)
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		id := strings.Repeat("c", 32)
		root := filepath.Join(runtime.Root, "state", "config-restore", "v1", "transactions", id)
		descriptor := validV2TransactionDescriptor(runtime, id)
		writeV2TestFile(t, filepath.Join(root, "transaction.json"), mustV2JSON(t, descriptor))
		evidence := v2RebuildEvidence{RestoreItems: []restore.RestoreResult{item}}
		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 0, before, evidence); failure == nil || failure.Coordinate != "intent" {
			t.Fatalf("transaction without intent was accepted: %+v", failure)
		}
	})

	t.Run("repeat storage member", func(t *testing.T) {
		runtime, _, _ := v2EvidenceFixture(t)
		store := filepath.Join(runtime.Root, "state", "config-restore", "v1")
		if err := os.MkdirAll(store, 0o700); err != nil {
			t.Fatal(err)
		}
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		writeV2TestFile(t, filepath.Join(store, "repeat-drift.json"), []byte(`{"unexpected":true}`))
		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 2, before, v2RebuildEvidence{}); failure == nil || failure.Coordinate != "storage" {
			t.Fatalf("repeat storage drift was accepted: %+v", failure)
		}
	})
}

func TestV2StorageRejectsUncitedOrPreexistingTransactionStoreChanges(t *testing.T) {
	transactionID := strings.Repeat("9", 32)

	t.Run("foreign addition", func(t *testing.T) {
		runtime, _, _ := v2EvidenceFixture(t)
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		store := filepath.Join(runtime.Root, "state", "config-restore")
		writeV2TestFile(t, filepath.Join(store, "v1", "transactions", transactionID, "transaction.json"), []byte(`{}`))
		writeV2TestFile(t, filepath.Join(store, "v1", "foreign.txt"), []byte("foreign"))

		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 0, before, v2RebuildEvidence{}); failure == nil || failure.Coordinate != "storage" {
			t.Fatalf("uncited store addition was accepted: %+v", failure)
		}
	})

	t.Run("existing member mutation", func(t *testing.T) {
		runtime, _, _ := v2EvidenceFixture(t)
		store := filepath.Join(runtime.Root, "state", "config-restore")
		existing := filepath.Join(store, "v1", "existing.txt")
		writeV2TestFile(t, existing, []byte("before"))
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		writeV2TestFile(t, existing, []byte("after"))
		writeV2TestFile(t, filepath.Join(store, "v1", "transactions", transactionID, "transaction.json"), []byte(`{}`))

		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 0, before, v2RebuildEvidence{}); failure == nil || failure.Coordinate != "storage" {
			t.Fatalf("existing store mutation was accepted: %+v", failure)
		}
	})

	t.Run("preexisting transaction subtree", func(t *testing.T) {
		runtime, _, _ := v2EvidenceFixture(t)
		store := filepath.Join(runtime.Root, "state", "config-restore")
		root := filepath.Join(store, "v1", "transactions", transactionID)
		writeV2TestFile(t, filepath.Join(root, "seed.txt"), []byte("preexisting"))
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		writeV2TestFile(t, filepath.Join(root, "transaction.json"), []byte(`{}`))

		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 0, before, v2RebuildEvidence{}); failure == nil || failure.Coordinate != "storage" {
			t.Fatalf("transaction mixed into a preexisting subtree was accepted: %+v", failure)
		}
	})
}

func TestV2CommittedAndRevertMarkersRejectWrongDigestOrMember(t *testing.T) {
	t.Run("committed digest", func(t *testing.T) {
		root := t.TempDir()
		intentDigest := strings.Repeat("d", 64)
		marker := v2CommittedMarkerFixture(intentDigest)
		marker["markerDigest"] = strings.Repeat("0", 64)
		writeV2TestFile(t, filepath.Join(root, "journal", "terminal-"+intentDigest+".json"), mustV2JSON(t, marker))
		if _, failure := validateV2CommittedMarker(root, intentDigest); failure == nil || failure.Coordinate != "committed" {
			t.Fatalf("wrong committed digest was accepted: %+v", failure)
		}
		marker = v2CommittedMarkerFixture(intentDigest)
		writeV2TestFile(t, filepath.Join(root, "journal", "terminal-"+intentDigest+".json"), mustV2JSON(t, marker))
		if _, failure := validateV2CommittedMarker(root, intentDigest); failure != nil {
			t.Fatalf("valid committed marker: %+v", failure)
		}
	})

	t.Run("wrong reverted member", func(t *testing.T) {
		runtime, _, _ := v2EvidenceFixture(t)
		id := strings.Repeat("e", 32)
		root := filepath.Join(runtime.Root, "state", "config-restore", "v1", "transactions", id)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		before, failure := snapshotV2Storage(runtime)
		if failure != nil {
			t.Fatal(failure)
		}
		marker := map[string]any{
			"format": "endstate.config-restore-member-revert", "version": 1, "kind": configrestore.StoreMemberGeneration,
			"memberId": strings.Repeat("f", 32), "sourceDigest": strings.Repeat("a", 64), "revertDigest": strings.Repeat("b", 64),
		}
		writeV2TestFile(t, filepath.Join(root, "reverted.json"), mustV2JSON(t, marker))
		binding := v2TransactionBinding{Root: root, ID: id, TerminalDigest: strings.Repeat("a", 64)}
		if failure := validateV2RevertStorage(runtime, before, binding); failure == nil || failure.Coordinate != "reverted.json" {
			t.Fatalf("wrong reverted member was accepted: %+v", failure)
		}
	})
}

func TestV2TransactionRejectsCorruptActionLineageValidationPriorAndMarker(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*configrestore.MaterializedSet, *configrestore.JournalLineage)
		coordinate string
	}{
		{name: "action", coordinate: "intent.actions", mutate: func(set *configrestore.MaterializedSet, _ *configrestore.JournalLineage) {
			set.Actions[0].Kind = configrestore.ActionWriteFile
			set.Actions[0].Strategy = "merge-ini"
			set.Actions[0].DesiredContent = []byte("[endstate-validation]\nvalue=foreign\n")
		}},
		{name: "lineage", coordinate: "intent.lineage", mutate: func(_ *configrestore.MaterializedSet, lineage *configrestore.JournalLineage) {
			lineage.ModuleID = "apps.foreign"
		}},
		{name: "validation", coordinate: "intent.validations", mutate: func(set *configrestore.MaterializedSet, _ *configrestore.JournalLineage) {
			set.Validations[0].Definition.Type = "file-exists"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, root, item := validV2TransactionFixture(t, test.mutate)
			if _, failure := validateV2Transaction(context.Background(), runtime, root, []restore.RestoreResult{item}); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("corrupt transaction %s was accepted: %+v", test.name, failure)
			}
		})
	}

	t.Run("prior snapshot", func(t *testing.T) {
		runtime, root, item := validV2TransactionFixture(t, nil)
		backup, err := resolveV2SemanticPath(runtime, item.BackupPath, root)
		if err != nil {
			t.Fatal(err)
		}
		writeV2TestFile(t, backup, []byte("corrupt prior"))
		if _, failure := validateV2Transaction(context.Background(), runtime, root, []restore.RestoreResult{item}); failure == nil || failure.Coordinate != "intent" {
			t.Fatalf("corrupt prior snapshot was accepted: %+v", failure)
		}
	})

	t.Run("committed marker", func(t *testing.T) {
		runtime, root, item := validV2TransactionFixture(t, nil)
		intent, err := configrestore.ReadJournalIntentWithBoundary(context.Background(), root, v2HostBoundary{runtime.validationContext()})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "journal", "terminal-"+intent.Digest()+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var marker map[string]any
		if err := json.Unmarshal(data, &marker); err != nil {
			t.Fatal(err)
		}
		marker["rollbackOutcome"] = configrestore.RollbackSucceeded
		writeV2TestFile(t, path, mustV2JSON(t, marker))
		if _, failure := validateV2Transaction(context.Background(), runtime, root, []restore.RestoreResult{item}); failure == nil || failure.Coordinate != "committed" {
			t.Fatalf("corrupt committed marker was accepted: %+v", failure)
		}
	})
}

func TestV2TransactionExactlyBindsDesiredSourceAndMissingParentState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*v2TestJournalIntentDisk)
	}{
		{name: "desired content hash", mutate: func(intent *v2TestJournalIntentDisk) {
			action := &intent.Actions[0]
			action.Desired.Entries[0].ContentHash = strings.Repeat("e", 64)
			action.Desired.Digest = v2TestFilesystemStateDigest(action.Desired)
		}},
		{name: "desired mode", mutate: func(intent *v2TestJournalIntentDisk) {
			action := &intent.Actions[0]
			action.Desired.Mode = 0o400
			action.Desired.Entries[0].Mode = 0o400
			action.Desired.Digest = v2TestFilesystemStateDigest(action.Desired)
		}},
		{name: "desired entry metadata", mutate: func(intent *v2TestJournalIntentDisk) {
			action := &intent.Actions[0]
			action.Desired.Entries[0].Size++
			action.Desired.Digest = v2TestFilesystemStateDigest(action.Desired)
		}},
		{name: "source digest", mutate: func(intent *v2TestJournalIntentDisk) {
			intent.Actions[0].SourceDigest = strings.Repeat("f", 64)
		}},
		{name: "missing parent", mutate: func(intent *v2TestJournalIntentDisk) {
			target := filepath.ToSlash(intent.Actions[0].Target)
			intent.Actions[0].MissingParents = []string{target[:strings.LastIndex(target, "/")]}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, root, item := validV2TransactionFixture(t, nil)
			rewriteV2TestJournalIntent(t, root, test.mutate)
			if _, failure := validateV2Transaction(context.Background(), runtime, root, []restore.RestoreResult{item}); failure == nil || failure.Coordinate != "intent.actions" {
				t.Fatalf("semantically foreign %s was accepted: %+v", test.name, failure)
			}
		})
	}
}

func TestV2ActionSourceStateIncludesOnlyCapturedExcludeWitnessRoles(t *testing.T) {
	root := t.TempDir()
	member := V2FixtureFile{
		Relative: "settings/main.ini",
		Path:     filepath.Join(root, "settings", "main.ini"),
		Captured: []byte("captured member"),
		Mutated:  []byte("mutated member"),
	}
	excluded := []V2ExcludedFixture{
		{
			Relative:        "capture-only.txt",
			Path:            filepath.Join(root, "capture-only.txt"),
			Captured:        []byte("captured capture-only"),
			Mutated:         []byte("mutated capture-only"),
			CapturePatterns: []string{"capture-only.txt"},
		},
		{
			Relative:        "restore-only.txt",
			Path:            filepath.Join(root, "restore-only.txt"),
			Captured:        []byte("captured restore-only"),
			Mutated:         []byte("mutated restore-only"),
			RestorePatterns: []string{"restore-only.txt"},
		},
		{
			Relative:        "overlap.txt",
			Path:            filepath.Join(root, "overlap.txt"),
			Captured:        []byte("captured overlap"),
			Mutated:         []byte("mutated overlap"),
			CapturePatterns: []string{"overlap.txt"},
			RestorePatterns: []string{"overlap.txt"},
		},
	}
	writeV2TestFile(t, member.Path, member.Mutated)
	for _, witness := range excluded {
		writeV2TestFile(t, witness.Path, witness.Mutated)
	}

	state, failure := expectedV2FixtureState(V2FixtureTarget{
		Coordinate: "configSets.settings.generations.current.capture[0]",
		Resolved:   root,
		Directory:  true,
		Members:    []V2FixtureFile{member},
		Excluded:   excluded,
	}, false, false)
	if failure != nil {
		t.Fatal(failure)
	}
	entries := make(map[string]configrestore.JournalFilesystemEntry, len(state.Entries))
	for _, entry := range state.Entries {
		entries[entry.Path] = entry
	}

	tests := []struct {
		name    string
		path    string
		present bool
		content []byte
	}{
		{name: "capture-only", path: "capture-only.txt"},
		{name: "restore-only", path: "restore-only.txt", present: true, content: excluded[1].Captured},
		{name: "overlap", path: "overlap.txt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, found := entries[test.path]
			if found != test.present {
				t.Fatalf("source state presence for %q = %t, want %t", test.path, found, test.present)
			}
			if !test.present {
				return
			}
			digest := sha256.Sum256(test.content)
			if entry.Kind != configrestore.StateFile || entry.Size != int64(len(test.content)) || entry.ContentHash != hex.EncodeToString(digest[:]) {
				t.Fatalf("source state entry for %q = %+v, want captured file metadata", test.path, entry)
			}
		})
	}
}

func TestV2RecoveryBindsRestoreEvidenceToTheNewTransaction(t *testing.T) {
	t.Run("exact recovery", func(t *testing.T) {
		runtime, _, item := validV2TransactionFixture(t, nil)
		binding, failure := validateV2RebuildStorage(context.Background(), runtime, 1, v2StorageSnapshot{}, v2RebuildEvidence{RestoreItems: []restore.RestoreResult{item}})
		if failure != nil || binding.ID != "0123456789abcdef0123456789abcdef" {
			t.Fatalf("exact recovery transaction = (%+v, %+v)", binding, failure)
		}
	})

	t.Run("mismatched recovery backup", func(t *testing.T) {
		runtime, _, item := validV2TransactionFixture(t, nil)
		item.BackupPath = strings.Replace(item.BackupPath, "/snapshots/000000/", "/snapshots/000001/", 1)
		if _, failure := validateV2RebuildStorage(context.Background(), runtime, 1, v2StorageSnapshot{}, v2RebuildEvidence{RestoreItems: []restore.RestoreResult{item}}); failure == nil || failure.Coordinate != "intent.actions" {
			t.Fatalf("recovery evidence from a different action was accepted: %+v", failure)
		}
	})
}

func TestV2StorageRejectsUncitedMembersInsideTheNewTransaction(t *testing.T) {
	for _, relative := range []string{
		"debug.json",
		"journal/extra.json",
		"snapshots/000000/debug.json",
	} {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			runtime, root, item := validV2TransactionFixture(t, nil)
			writeV2TestFile(t, filepath.Join(root, filepath.FromSlash(relative)), []byte(`{"debug":true}`))
			if _, failure := validateV2RebuildStorage(context.Background(), runtime, 1, v2StorageSnapshot{}, v2RebuildEvidence{RestoreItems: []restore.RestoreResult{item}}); failure == nil || failure.Coordinate != "storage" {
				t.Fatalf("uncited transaction member %q was accepted: %+v", relative, failure)
			}
		})
	}
}

func TestStrictV2JSONRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"format":"x","extra":true}`),
		[]byte(`{"format":"x"}{}`),
	} {
		var target struct {
			Format string `json:"format"`
		}
		if err := strictV2JSON(raw, &target); err == nil {
			t.Fatalf("strict v2 JSON accepted %s", raw)
		}
	}
}

func validV2TransactionDescriptor(runtime *scenarioRuntime, id string) v2TransactionDescriptor {
	started := time.Now().UTC().Format(time.RFC3339Nano)
	descriptor := v2TransactionDescriptor{
		Format: "endstate.config-restore-transaction", Version: 1, TransactionID: id, RestoreRunID: strings.Repeat("1", 32),
		RunID: "apply-test", RunStartedAtUTC: started, MutationOrdinal: 1, CaptureID: runtime.V2Plan.CaptureID,
	}
	identity := v2TransactionDescriptorIdentity{
		descriptor.Format, descriptor.Version, descriptor.TransactionID, descriptor.RestoreRunID, descriptor.RunID,
		descriptor.RunStartedAtUTC, descriptor.MutationOrdinal, descriptor.CaptureID,
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	descriptor.DescriptorDigest = hex.EncodeToString(digest[:])
	return descriptor
}

func validV2TransactionFixture(t *testing.T, mutate func(*configrestore.MaterializedSet, *configrestore.JournalLineage)) (*scenarioRuntime, string, restore.RestoreResult) {
	t.Helper()
	runtime, _, item := v2EvidenceFixture(t)
	target := &runtime.V2Plan.Targets[0]
	captured := []byte("[endstate-validation]\nvalue=captured\n")
	mutated := []byte("[endstate-validation]\nvalue=mutated\n")
	target.Members = []V2FixtureFile{{Relative: ".", Path: target.Resolved, Captured: captured, Mutated: mutated}}
	validation := modules.ValidationDef{Type: "ini-parse", Path: target.Destination}
	runtime.V2Plan.Compiled.Generation.Validate = []modules.ValidationDef{validation}
	runtime.V2Plan.Validations = 1

	source := filepath.Join(runtime.Root, "state", "stage", "settings.ini")
	writeV2TestFile(t, source, captured)
	writeV2TestFile(t, target.Resolved, mutated)
	sourceIdentity, err := runtime.validationContext().DisplayPath(source)
	if err != nil {
		t.Fatal(err)
	}
	set := &configrestore.MaterializedSet{
		Actions: []configrestore.Action{{
			Kind: configrestore.ActionCopy, Strategy: "copy", Source: sourceIdentity, Target: item.Target,
			SnapshotRequired: true,
		}},
		Validations: []configvalidate.ResolvedValidation{{Definition: validation, HostPath: item.Target}},
	}
	lineage := configrestore.JournalLineage{
		RunID: "apply-test", CaptureID: runtime.V2Plan.CaptureID, ModuleID: runtime.Module.ID,
		ConfigSetID: runtime.V2Plan.Compiled.Set.ID, TargetInstanceID: runtime.V2Plan.Instance.ID,
		SourceGeneration: runtime.V2Plan.Compiled.Generation.ID, TargetGeneration: runtime.V2Plan.Compiled.Generation.ID,
		MigrationPath: []string{}, SourceGenerationFingerprint: runtime.V2Plan.Compiled.Generation.Fingerprint,
		CaptureModuleRevision: runtime.Module.Revision, RestoreModuleRevision: runtime.Module.Revision,
	}
	if mutate != nil {
		mutate(set, &lineage)
	}
	transactionID := "0123456789abcdef0123456789abcdef"
	root := filepath.Join(runtime.Root, "state", "config-restore", "v1", "transactions", transactionID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	boundary := v2HostBoundary{runtime.validationContext()}
	prepared, err := configrestore.PrepareSnapshots(context.Background(), configrestore.SnapshotRequest{Set: set, TransactionRoot: root, Boundary: boundary})
	if err != nil {
		t.Fatal(err)
	}
	item.BackupPath = prepared.Actions()[0].Prior().BackupPath
	intent, err := configrestore.PersistJournalIntent(context.Background(), configrestore.JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configrestore.PersistCommittedMarker(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	descriptor := validV2TransactionDescriptor(runtime, transactionID)
	writeV2TestFile(t, filepath.Join(root, "transaction.json"), mustV2JSON(t, descriptor))
	return runtime, root, item
}

type v2TestJournalIntentIdentity struct {
	Format           string                            `json:"format"`
	Version          int                               `json:"version"`
	State            configrestore.JournalState        `json:"state"`
	Lineage          configrestore.JournalLineage      `json:"lineage"`
	Actions          []configrestore.JournalAction     `json:"actions"`
	Validations      []configrestore.JournalValidation `json:"validations"`
	ValidationStatus configrestore.ValidationStatus    `json:"validationStatus"`
	RollbackOutcome  configrestore.RollbackOutcome     `json:"rollbackOutcome"`
}

type v2TestJournalIntentDisk struct {
	Format           string                            `json:"format"`
	Version          int                               `json:"version"`
	State            configrestore.JournalState        `json:"state"`
	Lineage          configrestore.JournalLineage      `json:"lineage"`
	Actions          []configrestore.JournalAction     `json:"actions"`
	Validations      []configrestore.JournalValidation `json:"validations"`
	ValidationStatus configrestore.ValidationStatus    `json:"validationStatus"`
	RollbackOutcome  configrestore.RollbackOutcome     `json:"rollbackOutcome"`
	IntentDigest     string                            `json:"intentDigest"`
}

func rewriteV2TestJournalIntent(t *testing.T, root string, mutate func(*v2TestJournalIntentDisk)) {
	t.Helper()
	path := filepath.Join(root, "journal", "intent.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk v2TestJournalIntentDisk
	if err := strictV2JSON(data, &disk); err != nil {
		t.Fatal(err)
	}
	oldDigest := disk.IntentDigest
	mutate(&disk)
	identity := v2TestJournalIntentIdentity{
		Format: disk.Format, Version: disk.Version, State: disk.State, Lineage: disk.Lineage,
		Actions: disk.Actions, Validations: disk.Validations, ValidationStatus: disk.ValidationStatus, RollbackOutcome: disk.RollbackOutcome,
	}
	encoded := mustV2JSON(t, identity)
	digest := sha256.Sum256(encoded)
	disk.IntentDigest = hex.EncodeToString(digest[:])
	writeV2TestFile(t, path, append(mustV2JSON(t, disk), '\n'))
	if err := os.Remove(filepath.Join(root, "journal", "terminal-"+oldDigest+".json")); err != nil {
		t.Fatal(err)
	}
	writeV2TestFile(t, filepath.Join(root, "journal", "terminal-"+disk.IntentDigest+".json"), mustV2JSON(t, v2CommittedMarkerFixture(disk.IntentDigest)))
}

func v2TestFilesystemStateDigest(state configrestore.JournalActionState) string {
	hasher := sha256.New()
	writeString := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(value))
	}
	writeUint := func(value uint64) {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], value)
		_, _ = hasher.Write(encoded[:])
	}
	writeString("endstate-filesystem-state-v1")
	writeString(string(state.Kind))
	writeUint(uint64(len(state.Entries)))
	for _, entry := range state.Entries {
		writeString(entry.Path)
		writeString(string(entry.Kind))
		writeUint(uint64(entry.Mode))
		writeUint(uint64(entry.Size))
		writeString(entry.ContentHash)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func v2CommittedMarkerFixture(intentDigest string) map[string]any {
	identity := struct {
		Format           string                         `json:"format"`
		Version          int                            `json:"version"`
		IntentDigest     string                         `json:"intentDigest"`
		State            configrestore.JournalState     `json:"state"`
		ValidationStatus configrestore.ValidationStatus `json:"validationStatus"`
		RollbackOutcome  configrestore.RollbackOutcome  `json:"rollbackOutcome"`
	}{
		"endstate.config-restore-marker", 1, intentDigest, configrestore.JournalCommitted,
		configrestore.ValidationPassed, configrestore.RollbackNotRequired,
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return map[string]any{
		"format": identity.Format, "version": identity.Version, "intentDigest": identity.IntentDigest, "state": identity.State,
		"validationStatus": identity.ValidationStatus, "rollbackOutcome": identity.RollbackOutcome, "markerDigest": hex.EncodeToString(digest[:]),
	}
}

func writeV2TestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
