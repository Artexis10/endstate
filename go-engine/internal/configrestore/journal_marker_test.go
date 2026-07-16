// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistCommittedMarkerWritesCanonicalVerifiedRecordAndIsIdempotent(t *testing.T) {
	transactionRoot, prepared, target := prepareJournalDelete(t)
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := PersistCommittedMarker(context.Background(), intent)
	if err != nil {
		t.Fatalf("PersistCommittedMarker() error = %v", err)
	}
	if marker.State() != JournalCommitted || marker.ValidationStatus() != ValidationPassed ||
		marker.RollbackOutcome() != RollbackNotRequired || marker.IntentDigest() != intent.Digest() ||
		len(marker.Digest()) != 64 {
		t.Fatalf("committed marker = state %q validation %q rollback %q intent %q digest %q",
			marker.State(), marker.ValidationStatus(), marker.RollbackOutcome(), marker.IntentDigest(), marker.Digest())
	}
	wantPath := filepath.Join(transactionRoot, "journal", "committed-"+intent.Digest()+".json")
	if marker.Path() != wantPath {
		t.Fatalf("marker path = %q, want %q", marker.Path(), wantPath)
	}
	identity := fmt.Sprintf(
		`{"format":"endstate.config-restore-marker","version":1,"intentDigest":"%s","state":"committed","validationStatus":"passed","rollbackOutcome":"not_required"}`,
		intent.Digest(),
	)
	digestBytes := sha256.Sum256([]byte(identity))
	wantDigest := hex.EncodeToString(digestBytes[:])
	wantBytes := strings.TrimSuffix(identity, "}") + `,"markerDigest":"` + wantDigest + `"}` + "\n"
	gotBytes, err := os.ReadFile(marker.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != wantBytes || marker.Digest() != wantDigest {
		t.Fatalf("marker bytes/digest:\n got: %s %q\nwant: %s %q", gotBytes, marker.Digest(), wantBytes, wantDigest)
	}
	second, err := PersistCommittedMarker(context.Background(), intent)
	if err != nil || second.Digest() != marker.Digest() {
		t.Fatalf("idempotent committed marker = %#v err=%v", second, err)
	}
	assertJournalMarkerAccessorsAreDefensive(t, marker)
	assertTestFile(t, target, "prior")
}

func TestPersistRolledBackMarkerRecordsValidationAndRejectsOppositeTerminalState(t *testing.T) {
	transactionRoot, prepared, _ := prepareJournalDelete(t)
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := PersistRolledBackMarker(context.Background(), intent, ValidationFailed)
	if err != nil {
		t.Fatal(err)
	}
	if marker.State() != JournalRolledBack || marker.ValidationStatus() != ValidationFailed ||
		marker.RollbackOutcome() != RollbackSucceeded {
		t.Fatalf("rolled-back marker = state %q validation %q outcome %q",
			marker.State(), marker.ValidationStatus(), marker.RollbackOutcome())
	}
	if conflicting, err := PersistCommittedMarker(context.Background(), intent); conflicting != nil ||
		CodeOf(err) != CodeJournalCompletionFailed {
		t.Fatalf("opposite marker result=%#v err=%v code=%q", conflicting, err, CodeOf(err))
	}
	if _, err := os.Lstat(filepath.Join(transactionRoot, "journal", "committed-"+intent.Digest()+".json")); !os.IsNotExist(err) {
		t.Fatalf("conflicting committed marker exists: %v", err)
	}
	if result, err := PersistRolledBackMarker(context.Background(), intent, ValidationPending); result != nil ||
		CodeOf(err) != CodeJournalCompletionFailed {
		t.Fatalf("invalid validation result=%#v err=%v code=%q", result, err, CodeOf(err))
	}
}

func TestPersistJournalMarkerCancellationAndFailureCleanTempsButKeepPendingIntent(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*JournalWriter) context.Context
	}{
		{
			name: "canceled",
			prepare: func(_ *JournalWriter) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{
			name: "injected before publication",
			prepare: func(writer *JournalWriter) context.Context {
				writer.checkpoint = func(_ context.Context, phase journalPhase, _ string) error {
					if phase == journalPhaseBeforeMarkerPublish {
						return errors.New("injected marker failure")
					}
					return nil
				}
				return context.Background()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transactionRoot, prepared, target := prepareJournalDelete(t)
			intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
				Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
			})
			if err != nil {
				t.Fatal(err)
			}
			writer := NewJournalWriter()
			ctx := tt.prepare(writer)
			marker, err := writer.PersistCommitted(ctx, intent)
			if marker != nil || CodeOf(err) != CodeJournalCompletionFailed {
				t.Fatalf("marker=%#v err=%v code=%q", marker, err, CodeOf(err))
			}
			assertNoTerminalMarkersOrTemps(t, transactionRoot, intent.Digest())
			if loaded, err := ReadJournalIntent(context.Background(), transactionRoot); err != nil || loaded.Digest() != intent.Digest() {
				t.Fatalf("pending intent after marker failure = %#v err=%v", loaded, err)
			}
			assertTestFile(t, target, "prior")
		})
	}
}

func TestPersistCommittedMarkerHasNoFalliblePostPublicationCheckpoint(t *testing.T) {
	transactionRoot, prepared, _ := prepareJournalDelete(t)
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	writer := NewJournalWriter()
	writer.checkpoint = func(_ context.Context, phase journalPhase, _ string) error {
		if phase != journalPhaseBeforeMarkerPublish {
			return errors.New("must not run a fallible checkpoint after terminal publication")
		}
		return nil
	}
	marker, err := writer.PersistCommitted(context.Background(), intent)
	if err != nil || marker == nil || marker.State() != JournalCommitted {
		t.Fatalf("post-publication result=%#v err=%v", marker, err)
	}
}

func TestPersistJournalMarkersFailClosedOnTamperAndConflictingMarkerArtifact(t *testing.T) {
	t.Run("tampered identical marker", func(t *testing.T) {
		transactionRoot, prepared, _ := prepareJournalDelete(t)
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if err != nil {
			t.Fatal(err)
		}
		marker, err := PersistCommittedMarker(context.Background(), intent)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(marker.Path())
		if err != nil {
			t.Fatal(err)
		}
		data[len(data)-3] ^= 1
		if err := os.WriteFile(marker.Path(), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if result, err := PersistCommittedMarker(context.Background(), intent); result != nil ||
			CodeOf(err) != CodeJournalCompletionFailed {
			t.Fatalf("tampered retry result=%#v err=%v code=%q", result, err, CodeOf(err))
		}
	})

	t.Run("opposite artifact exists", func(t *testing.T) {
		transactionRoot, prepared, _ := prepareJournalDelete(t)
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if err != nil {
			t.Fatal(err)
		}
		opposite := filepath.Join(transactionRoot, "journal", "rolled-back-"+intent.Digest()+".json")
		writeTestFile(t, opposite, "conflict")
		if result, err := PersistCommittedMarker(context.Background(), intent); result != nil ||
			CodeOf(err) != CodeJournalCompletionFailed {
			t.Fatalf("conflict result=%#v err=%v code=%q", result, err, CodeOf(err))
		}
	})

	t.Run("canonical marker has invalid closed outcome", func(t *testing.T) {
		transactionRoot, prepared, _ := prepareJournalDelete(t)
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if err != nil {
			t.Fatal(err)
		}
		marker, err := PersistCommittedMarker(context.Background(), intent)
		if err != nil {
			t.Fatal(err)
		}
		_, invalid, err := newJournalMarkerDisk(
			intent.Digest(), JournalCommitted, ValidationFailed, RollbackNotRequired,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker.Path(), invalid, 0o600); err != nil {
			t.Fatal(err)
		}
		if result, err := PersistCommittedMarker(context.Background(), intent); result != nil ||
			CodeOf(err) != CodeJournalCompletionFailed {
			t.Fatalf("semantic-invalid marker result=%#v err=%v code=%q", result, err, CodeOf(err))
		}
	})

	t.Run("marker retry requires semantically verified intent", func(t *testing.T) {
		transactionRoot, prepared, _ := prepareJournalDelete(t)
		intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
			Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := PersistCommittedMarker(context.Background(), intent); err != nil {
			t.Fatal(err)
		}
		rewriteCanonicalJournalIntent(t, intent.Path(), func(disk *journalIntentDisk) {
			disk.Lineage.MigrationPath[0] = "wrong"
		})
		if result, err := PersistCommittedMarker(context.Background(), intent); result != nil ||
			CodeOf(err) != CodeJournalCompletionFailed {
			t.Fatalf("invalid-intent marker retry result=%#v err=%v code=%q", result, err, CodeOf(err))
		}
	})
}

func assertJournalMarkerAccessorsAreDefensive(t *testing.T, marker *JournalMarker) {
	t.Helper()
	lineage := marker.Lineage()
	lineage.MigrationPath[0] = "changed"
	actions := marker.Actions()
	actions[0].Prior.Entries[0] = JournalFilesystemEntry{}
	actions[0] = JournalAction{}
	validations := marker.Validations()
	if len(validations) > 0 {
		validations[0] = JournalValidation{}
	}
	if marker.Lineage().MigrationPath[0] != "g1" || marker.Actions()[0].Kind != ActionDeleteFile ||
		marker.Actions()[0].Prior.Entries[0].Path != "." {
		t.Fatal("journal marker was mutated through an accessor")
	}
}

func assertNoTerminalMarkersOrTemps(t *testing.T, transactionRoot, digest string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(transactionRoot, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") || entry.Name() == "committed-"+digest+".json" ||
			entry.Name() == "rolled-back-"+digest+".json" {
			t.Fatalf("terminal marker artifact remains: %s", entry.Name())
		}
	}
}
