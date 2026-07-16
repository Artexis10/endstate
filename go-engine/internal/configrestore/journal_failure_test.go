// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPersistJournalIntentCancellationAndInjectedFailureLeaveNoRecordOrTargetMutation(t *testing.T) {
	t.Run("pre-canceled", func(t *testing.T) {
		transactionRoot, prepared, target := prepareJournalDelete(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		intent, err := PersistJournalIntent(ctx, JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if intent != nil || CodeOf(err) != CodeJournalIntentFailed || !errors.Is(err, context.Canceled) {
			t.Fatalf("intent=%#v err=%v code=%q", intent, err, CodeOf(err))
		}
		assertNoJournalRecordsOrTemps(t, transactionRoot)
		assertTestFile(t, target, "prior")
	})

	t.Run("before publication", func(t *testing.T) {
		transactionRoot, prepared, target := prepareJournalDelete(t)
		cause := errors.New("injected publication failure")
		writer := NewJournalWriter()
		writer.checkpoint = func(_ context.Context, phase journalPhase, _ string) error {
			if phase == journalPhaseBeforeIntentPublish {
				return cause
			}
			return nil
		}
		intent, err := writer.PersistIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if intent != nil || CodeOf(err) != CodeJournalIntentFailed || !errors.Is(err, cause) {
			t.Fatalf("intent=%#v err=%v code=%q", intent, err, CodeOf(err))
		}
		assertNoJournalRecordsOrTemps(t, transactionRoot)
		assertTestFile(t, target, "prior")
	})
}

func TestPersistJournalIntentVerifiesAndSyncsEverySnapshotNodeBeforePublication(t *testing.T) {
	transactionRoot := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(target, "nested", "prior.txt"), "prior")
	if err := os.Mkdir(filepath.Join(target, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionCopy, Strategy: "copy", Source: source, Target: target,
			SourceIsDirectory: true, SnapshotRequired: true,
		}}},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantFiles, wantDirectories := snapshotTreePaths(t, prepared.SnapshotRoot())
	var syncedFiles, syncedDirectories []string
	published := false
	rootSynced := false
	writer := NewJournalWriter()
	writer.checkpoint = func(_ context.Context, phase journalPhase, path string) error {
		switch phase {
		case journalPhaseBeforeSnapshotFileSync:
			if published {
				t.Fatal("snapshot file synced after intent publication began")
			}
			syncedFiles = append(syncedFiles, path)
		case journalPhaseBeforeSnapshotDirectorySync:
			if published {
				t.Fatal("snapshot directory synced after intent publication began")
			}
			syncedDirectories = append(syncedDirectories, path)
		case journalPhaseBeforeIntentPublish:
			if !rootSynced {
				t.Fatal("transaction root was not synced before intent publication")
			}
			published = true
		case journalPhaseBeforeTransactionRootSync:
			rootSynced = true
		}
		return nil
	}
	if _, err := writer.PersistIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(syncedFiles)
	sort.Strings(syncedDirectories)
	if strings.Join(syncedFiles, "\x00") != strings.Join(wantFiles, "\x00") {
		t.Fatalf("synced files = %#v, want %#v", syncedFiles, wantFiles)
	}
	if strings.Join(syncedDirectories, "\x00") != strings.Join(wantDirectories, "\x00") {
		t.Fatalf("synced directories = %#v, want %#v", syncedDirectories, wantDirectories)
	}
	assertTestFile(t, target+string(filepath.Separator)+"nested"+string(filepath.Separator)+"prior.txt", "prior")
}

func TestPersistJournalIntentRejectsSnapshotDriftUnsafeTreesAndUnexpectedArtifacts(t *testing.T) {
	t.Run("backup bytes changed", func(t *testing.T) {
		transactionRoot, prepared, target := prepareJournalDelete(t)
		if err := os.WriteFile(prepared.Actions()[0].Prior().BackupPath, []byte("tampered"), 0o600); err != nil {
			t.Fatal(err)
		}
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if intent != nil || CodeOf(err) != CodeJournalIntentFailed {
			t.Fatalf("intent=%#v err=%v code=%q", intent, err, CodeOf(err))
		}
		assertNoJournalRecordsOrTemps(t, transactionRoot)
		assertTestFile(t, target, "prior")
	})

	t.Run("backup replaced by link", func(t *testing.T) {
		transactionRoot, prepared, target := prepareJournalDelete(t)
		backup := prepared.Actions()[0].Prior().BackupPath
		outside := filepath.Join(t.TempDir(), "outside")
		writeTestFile(t, outside, "outside")
		if err := os.Remove(backup); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, backup); err != nil {
			t.Skipf("creating test symlink is unavailable: %v", err)
		}
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if intent != nil || CodeOf(err) != CodeJournalIntentFailed {
			t.Fatalf("intent=%#v err=%v code=%q", intent, err, CodeOf(err))
		}
		assertNoJournalRecordsOrTemps(t, transactionRoot)
		assertTestFile(t, outside, "outside")
		assertTestFile(t, target, "prior")
	})

	t.Run("unexpected snapshot artifact", func(t *testing.T) {
		transactionRoot, prepared, _ := prepareJournalDelete(t)
		writeTestFile(t, filepath.Join(prepared.SnapshotRoot(), "unexpected"), "unexpected")
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if intent != nil || CodeOf(err) != CodeJournalIntentFailed {
			t.Fatalf("intent=%#v err=%v code=%q", intent, err, CodeOf(err))
		}
		assertNoJournalRecordsOrTemps(t, transactionRoot)
	})

	t.Run("linked journal directory", func(t *testing.T) {
		transactionRoot, prepared, _ := prepareJournalDelete(t)
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(transactionRoot, "journal")); err != nil {
			t.Skipf("creating test symlink is unavailable: %v", err)
		}
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if intent != nil || CodeOf(err) != CodeJournalIntentFailed {
			t.Fatalf("intent=%#v err=%v code=%q", intent, err, CodeOf(err))
		}
	})
}

func TestPersistAndReadJournalIntentRejectConflictsAndTampering(t *testing.T) {
	transactionRoot, prepared, _ := prepareJournalDelete(t)
	request := JournalIntentRequest{Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage()}
	intent, err := PersistJournalIntent(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	conflict := request
	conflict.Lineage.RunID = "different-run"
	if result, err := PersistJournalIntent(context.Background(), conflict); result != nil || CodeOf(err) != CodeJournalIntentFailed {
		t.Fatalf("conflicting result=%#v err=%v code=%q", result, err, CodeOf(err))
	}
	data, err := os.ReadFile(intent.Path())
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-3] ^= 1
	if err := os.WriteFile(intent.Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := ReadJournalIntent(context.Background(), transactionRoot); result != nil || CodeOf(err) != CodeJournalIntentFailed {
		t.Fatalf("tampered result=%#v err=%v code=%q", result, err, CodeOf(err))
	}
}

func TestReadJournalIntentRejectsCanonicalRecomputedSemanticInvalidity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*journalIntentDisk)
	}{
		{
			name: "invalid lineage",
			mutate: func(disk *journalIntentDisk) {
				disk.Lineage.MigrationPath = []string{"wrong", disk.Lineage.TargetGeneration}
			},
		},
		{
			name: "invalid filesystem action state",
			mutate: func(disk *journalIntentDisk) {
				disk.Actions[0].Desired.Kind = StateDirectory
			},
		},
		{
			name: "unsupported concrete strategy",
			mutate: func(disk *journalIntentDisk) {
				disk.Actions[0].Strategy = "shell"
			},
		},
		{
			name: "unsupported validation",
			mutate: func(disk *journalIntentDisk) {
				disk.Validations = append(disk.Validations, JournalValidation{
					Type: "shell", Path: "settings.json", HostPath: disk.Actions[0].Target,
				})
			},
		},
		{
			name: "primitive validation has foreign fields",
			mutate: func(disk *journalIntentDisk) {
				disk.Validations = append(disk.Validations, JournalValidation{
					Type: "file-exists", Path: "settings.json", JSONPath: "$.theme",
					HostPath: disk.Actions[0].Target,
				})
			},
		},
		{
			name: "validation has host expansion path",
			mutate: func(disk *journalIntentDisk) {
				disk.Validations = append(disk.Validations, JournalValidation{
					Type: "file-exists", Path: "%APPDATA%/settings.json", HostPath: disk.Actions[0].Target,
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transactionRoot, prepared, _ := prepareJournalDelete(t)
			intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
				Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
			})
			if err != nil {
				t.Fatal(err)
			}
			rewriteCanonicalJournalIntent(t, intent.Path(), tt.mutate)
			if result, err := ReadJournalIntent(context.Background(), transactionRoot); result != nil ||
				CodeOf(err) != CodeJournalIntentFailed {
				t.Fatalf("semantic-invalid result=%#v err=%v code=%q", result, err, CodeOf(err))
			}
		})
	}
}

func TestReadJournalIntentReverifiesBackupArtifacts(t *testing.T) {
	transactionRoot, prepared, _ := prepareJournalDelete(t)
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prepared.Actions()[0].Prior().BackupPath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := ReadJournalIntent(context.Background(), transactionRoot); result != nil ||
		CodeOf(err) != CodeJournalIntentFailed {
		t.Fatalf("drifted-backup result=%#v err=%v code=%q", result, err, CodeOf(err))
	}
	if _, err := os.Stat(intent.Path()); err != nil {
		t.Fatalf("intent record disappeared after failed verification: %v", err)
	}
}

func rewriteCanonicalJournalIntent(t *testing.T, path string, mutate func(*journalIntentDisk)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := decodeJournalIntent(data)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&disk)
	_, encoded, err := newJournalIntentDisk(disk.Lineage, disk.Actions, disk.Validations)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareJournalDelete(t *testing.T) (string, *PreparedSet, string) {
	t.Helper()
	transactionRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior")
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	return transactionRoot, prepared, target
}

func snapshotTreePaths(t *testing.T, root string) ([]string, []string) {
	t.Helper()
	var files, directories []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			directories = append(directories, path)
		} else if info.Mode().IsRegular() {
			files = append(files, path)
		} else {
			return fmt.Errorf("unexpected test snapshot node %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	sort.Strings(directories)
	return files, directories
}

func assertNoJournalRecordsOrTemps(t *testing.T, transactionRoot string) {
	t.Helper()
	journalDirectory := filepath.Join(transactionRoot, "journal")
	entries, err := os.ReadDir(journalDirectory)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "intent.json" || strings.Contains(entry.Name(), ".tmp") ||
			strings.HasPrefix(entry.Name(), "committed-") || strings.HasPrefix(entry.Name(), "rolled-back-") {
			t.Fatalf("journal artifact remains after failure: %s", entry.Name())
		}
	}
}
