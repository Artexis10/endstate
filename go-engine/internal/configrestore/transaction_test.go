// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestExecuteConfigSetTransactionCommitsValidatesAndDurablyCloses(t *testing.T) {
	fixture := prepareFileTransactionFixture(t, true)
	var observations []TransactionObservation
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: fixture.prepared,
		Intent:   fixture.intent,
		Observer: TransactionObserverFunc(func(observation TransactionObservation) {
			observations = append(observations, observation)
		}),
	})
	if err != nil {
		t.Fatalf("ExecuteConfigSetTransaction() error = %v", err)
	}
	if result.Status() != TransactionRestored || result.Reason() != "" || !result.MutationBegan() ||
		result.FailStop() || result.PrimaryError() != nil || result.RollbackError() != nil {
		t.Fatalf("result = status %q reason %q mutated %v failStop %v primary %v rollback %v",
			result.Status(), result.Reason(), result.MutationBegan(), result.FailStop(),
			result.PrimaryError(), result.RollbackError())
	}
	marker := result.Marker()
	if marker == nil || marker.State() != JournalCommitted || marker.IntentDigest() != fixture.intent.Digest() {
		t.Fatalf("committed marker = %#v", marker)
	}
	assertTestFile(t, fixture.copyTarget, `{"copied":true}`)
	assertTestFile(t, fixture.writeTarget, `{"written":true}`)
	if _, err := os.Lstat(fixture.deleteTarget); !os.IsNotExist(err) {
		t.Fatalf("delete target still exists: %v", err)
	}
	assertTransactionObservation(t, observations, TransactionStageCommit, TransactionProgressCompleted)
	assertTransactionObservation(t, observations, TransactionStageValidation, TransactionProgressCompleted)
}

func TestExecuteConfigSetTransactionCommitsAndRollsBackExactRegistryValue(t *testing.T) {
	key := `HKCU\Software\Vendor\Transaction`
	valueName := "Theme"
	identity := key + "\x00" + valueName
	prior := RegistryReadResult{Exists: true, ValueType: RegistryTypeSZ, Data: []byte{'o', 0, 'l', 0, 'd', 0, 0, 0}}
	registry := &memoryRegistryMutator{values: map[string]RegistryReadResult{identity: prior}}
	transactionRoot := t.TempDir()
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionRegistrySet, Strategy: "registry-set", Target: key + `\` + valueName,
			RegistryValue:    &RegistryValue{Key: key, ValueName: valueName, ValueType: "REG_SZ", Data: "new"},
			SnapshotRequired: true,
		}}},
		TransactionRoot: transactionRoot,
		RegistryReader:  registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: prepared, Intent: intent, Registry: registry,
	})
	if err != nil || result.Status() != TransactionRestored {
		t.Fatalf("registry commit result=%#v err=%v", result, err)
	}
	desired, err := desiredRegistrySnapshot(prepared.Actions()[0].Action().RegistryValue)
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.values[identity]; !got.Exists || got.ValueType != desired.ValueType || string(got.Data) != string(desired.Data) {
		t.Fatalf("committed registry value = %#v, want %#v", got, desired)
	}
}

type fileTransactionFixture struct {
	transactionRoot string
	prepared        *PreparedSet
	intent          *JournalIntent
	copyTarget      string
	writeTarget     string
	deleteTarget    string
}

func prepareFileTransactionFixture(t *testing.T, validCopy bool) fileTransactionFixture {
	t.Helper()
	transactionRoot := t.TempDir()
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	copySource := filepath.Join(stageRoot, "copy.json")
	copyTarget := filepath.Join(hostRoot, "copy.json")
	writeTarget := filepath.Join(hostRoot, "write.json")
	deleteTarget := filepath.Join(hostRoot, "delete.txt")
	copyContent := `{"copied":true}`
	if !validCopy {
		copyContent = `{invalid`
	}
	writeTestFile(t, copySource, copyContent)
	writeTestFile(t, copyTarget, `{"copied":false}`)
	writeTestFile(t, writeTarget, `{"written":false}`)
	writeTestFile(t, deleteTarget, "delete me")
	sourceInfo, err := os.Lstat(copySource)
	if err != nil {
		t.Fatal(err)
	}
	validations := []configvalidate.ResolvedValidation{
		{
			Definition: modules.ValidationDef{Type: "json-parse", Path: "copy.json"},
			HostPath:   copyTarget,
		},
		{
			Definition: modules.ValidationDef{Type: "json-path-exists", Path: "write.json", JSONPath: "$.written"},
			HostPath:   writeTarget,
		},
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{
			Actions: []Action{
				{
					Kind: ActionCopy, Strategy: "copy", Source: copySource, Target: copyTarget,
					SourceMode: sourceInfo.Mode(), SnapshotRequired: true,
				},
				{
					Kind: ActionWriteFile, Strategy: "merge-json", Target: writeTarget,
					DesiredContent: []byte(`{"written":true}`), SnapshotRequired: true,
				},
				{
					Kind: ActionDeleteFile, Strategy: "delete-glob", Target: deleteTarget,
					SnapshotRequired: true,
				},
			},
			Validations: validations,
		},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return fileTransactionFixture{
		transactionRoot: transactionRoot,
		prepared:        prepared,
		intent:          intent,
		copyTarget:      copyTarget,
		writeTarget:     writeTarget,
		deleteTarget:    deleteTarget,
	}
}

func assertFileTransactionPrior(t *testing.T, fixture fileTransactionFixture) {
	t.Helper()
	assertTestFile(t, fixture.copyTarget, `{"copied":false}`)
	assertTestFile(t, fixture.writeTarget, `{"written":false}`)
	assertTestFile(t, fixture.deleteTarget, "delete me")
}

func assertFileTransactionDesired(t *testing.T, fixture fileTransactionFixture) {
	t.Helper()
	assertTestFile(t, fixture.copyTarget, `{"copied":true}`)
	assertTestFile(t, fixture.writeTarget, `{"written":true}`)
	if _, err := os.Lstat(fixture.deleteTarget); !os.IsNotExist(err) {
		t.Fatalf("delete target still exists: %v", err)
	}
}

func assertTransactionObservation(
	t *testing.T,
	observations []TransactionObservation,
	stage TransactionStage,
	progress TransactionProgress,
) {
	t.Helper()
	for _, observation := range observations {
		if observation.Stage == stage && observation.Progress == progress {
			return
		}
	}
	t.Fatalf("missing observation stage=%q progress=%q in %#v", stage, progress, observations)
}

type memoryRegistryMutator struct {
	values map[string]RegistryReadResult
}

func (m *memoryRegistryMutator) ReadValue(_ context.Context, key, valueName string) (RegistryReadResult, error) {
	value, exists := m.values[key+"\x00"+valueName]
	if !exists {
		return RegistryReadResult{}, nil
	}
	value.Data = append([]byte(nil), value.Data...)
	return value, nil
}

func (m *memoryRegistryMutator) SetValue(
	_ context.Context,
	key string,
	valueName string,
	valueType uint32,
	data []byte,
) error {
	m.values[key+"\x00"+valueName] = RegistryReadResult{
		Exists: true, ValueType: valueType, Data: append([]byte(nil), data...),
	}
	return nil
}

func (m *memoryRegistryMutator) DeleteValue(_ context.Context, key, valueName string) error {
	delete(m.values, key+"\x00"+valueName)
	return nil
}

var _ RegistryMutator = (*memoryRegistryMutator)(nil)

func assertPrimaryCause(t *testing.T, result *TransactionResult, err, cause error) {
	t.Helper()
	if !errors.Is(err, cause) || !errors.Is(result.PrimaryError(), cause) {
		t.Fatalf("result primary=%v err=%v, want cause %v", result.PrimaryError(), err, cause)
	}
}
