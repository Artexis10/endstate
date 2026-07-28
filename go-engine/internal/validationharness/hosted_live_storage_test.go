// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestSnapshotHostedLiveStorageRejectsUnsafeRoot(t *testing.T) {
	if _, err := snapshotHostedLiveStorage(hostedLiveStorageRoot{}); err == nil {
		t.Fatal("snapshotHostedLiveStorage() accepted an empty root")
	}
}

func TestHostedLiveStorageTransitionProofRejectsSubstitutionAndPartialEvidence(t *testing.T) {
	targets := []string{"target-1", "target-2", "target-3", "target-4", "target-5", "target-6"}
	storageRoot := t.TempDir()
	journal := filepath.Join(storageRoot, "logs", "journal.json")
	identity, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proof := hostedLiveRestoreProof{
		nestedApplyRunID: "apply-run", journalIdentity: journal, journalDigest: "journal-digest", declaredTargets: targets,
		restoreResults: hostedLiveRestoreResults(targets),
	}
	before := hostedLiveStorageSnapshot{root: storageRoot, storeExists: true, store: boundaryTree{".": {Kind: "directory", Identity: identity}, "v1": {Kind: "directory", Identity: identity}, "v1/legacy-reverts": {Kind: "directory", Identity: identity}}, members: []hostedLiveStoreMember{hostedLiveStoreMemberForTest("existing", "old-run", filepath.Join(storageRoot, "logs", "old-journal.json"), "old-digest", 0, false, targets)}}
	after := cloneHostedLiveStorageSnapshot(before)
	after.members = append(after.members, hostedLiveStoreMemberForTest("first", "apply-run", journal, "journal-digest", 1, false, targets))
	binding, err := bindHostedLiveFirstRestore(before, after, proof)
	if err != nil || binding.memberID != "first" {
		t.Fatalf("first transition = %+v, %v", binding, err)
	}
	recoveryBefore := cloneHostedLiveStorageSnapshot(after)
	recoveryAfter := cloneHostedLiveStorageSnapshot(recoveryBefore)
	recoveryJournal := filepath.Join(storageRoot, "logs", "recovery-journal.json")
	recoveryAfter.members = append(recoveryAfter.members, hostedLiveStoreMemberForTest("recovery", "recovery-run", recoveryJournal, "recovery-digest", 2, false, targets))
	recoveryProof := hostedLiveRestoreProof{nestedApplyRunID: "recovery-run", declaredTargets: targets, restoreResults: hostedLiveRestoreResults(targets)}
	recoveryBinding, err := bindHostedLiveRecovery(recoveryBefore, recoveryAfter, recoveryProof)
	if err != nil || recoveryBinding.memberID != "recovery" || recoveryBinding.journalIdentity != recoveryJournal || recoveryBinding.journalDigest != "recovery-digest" {
		t.Fatalf("recovery transition = %+v, %v", recoveryBinding, err)
	}
	for _, mutate := range []struct {
		name  string
		apply func(*hostedLiveStorageSnapshot, *hostedLiveRestoreProof)
	}{
		{name: "wrong run", apply: func(_ *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.nestedApplyRunID = "foreign-run"
		}},
		{name: "multiple members", apply: func(after *hostedLiveStorageSnapshot, _ *hostedLiveRestoreProof) {
			after.members = append(after.members, hostedLiveStoreMemberForTest("extra", "recovery-run", filepath.Join(storageRoot, "logs", "extra.json"), "extra-digest", 3, false, targets))
		}},
		{name: "action substitution", apply: func(after *hostedLiveStorageSnapshot, _ *hostedLiveRestoreProof) {
			after.members[2].actions[0].SourceIdentity = configrestore.InspectionIdentity("foreign-source")
		}},
		{name: "missing inspected journal", apply: func(after *hostedLiveStorageSnapshot, _ *hostedLiveRestoreProof) {
			after.members[2].member.LegacyJournalIdentity = ""
		}},
	} {
		t.Run("recovery "+mutate.name, func(t *testing.T) {
			candidateAfter := cloneHostedLiveStorageSnapshot(recoveryAfter)
			candidateProof := recoveryProof
			candidateProof.declaredTargets = append([]string(nil), recoveryProof.declaredTargets...)
			candidateProof.restoreResults = append([]restore.RestoreResult(nil), recoveryProof.restoreResults...)
			mutate.apply(&candidateAfter, &candidateProof)
			if _, err := bindHostedLiveRecovery(recoveryBefore, candidateAfter, candidateProof); err == nil {
				t.Fatal("recovery transition accepted substituted proof")
			}
		})
	}

	for _, mutate := range []struct {
		name  string
		apply func(*hostedLiveStorageSnapshot, *hostedLiveRestoreProof)
	}{
		{name: "foreign apply run", apply: func(_ *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.nestedApplyRunID = "foreign-run"
		}},
		{name: "foreign journal", apply: func(_ *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.journalDigest = "foreign-digest"
		}},
		{name: "altered existing member", apply: func(after *hostedLiveStorageSnapshot, _ *hostedLiveRestoreProof) {
			after.members[0].member.MemberDigest = "altered"
		}},
		{name: "partial results", apply: func(_ *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.restoreResults = proof.restoreResults[:5]
		}},
		{name: "target mismatch", apply: func(_ *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.restoreResults[0].Target = "foreign-target"
		}},
		{name: "backup path despite false", apply: func(_ *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.restoreResults[0].BackupPath = "backup"
		}},
		{name: "source mismatch", apply: func(_ *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.restoreResults[0].Source = "foreign-source"
		}},
		{name: "unexpected restored action", apply: func(after *hostedLiveStorageSnapshot, proof *hostedLiveRestoreProof) {
			proof.restoreResults[2].Status = "restored"
			after.members[1].actions[2].Status = configrestore.StoreActionStatusRestored
		}},
		{name: "nonzero action backup", apply: func(after *hostedLiveStorageSnapshot, _ *hostedLiveRestoreProof) {
			after.members[1].actions[0].Backup.Digest = "ghost"
		}},
		{name: "backup tree delta", apply: func(after *hostedLiveStorageSnapshot, _ *hostedLiveRestoreProof) {
			after.backupsExists = true
			after.backups = boundaryTree{"added": {Kind: "file"}}
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			candidateAfter := cloneHostedLiveStorageSnapshot(after)
			candidateProof := proof
			candidateProof.restoreResults = append([]restore.RestoreResult(nil), proof.restoreResults...)
			candidateProof.declaredTargets = append([]string(nil), proof.declaredTargets...)
			mutate.apply(&candidateAfter, &candidateProof)
			if _, err := bindHostedLiveFirstRestore(before, candidateAfter, candidateProof); err == nil {
				t.Fatal("first transition accepted substituted storage proof")
			}
		})
	}

	reverted := cloneHostedLiveStorageSnapshot(after)
	reverted.members[1].member.Reverted = true
	reverted.members[1].member.RevertDigest = "revert-digest"
	reverted.store["v1/legacy-reverts/first.json"] = boundaryEntry{Kind: "file", Size: 1, Identity: identity}
	revert := commands.RevertData{JournalUsed: journal}
	if err := bindHostedLiveRevert(after, reverted, binding, revert); err != nil {
		t.Fatalf("revert transition error = %v", err)
	}
	if err := bindHostedLiveRevert(after, reverted, hostedLiveRestoreBinding{memberID: "foreign"}, revert); err == nil {
		t.Fatal("revert accepted a foreign member binding")
	}
	changedMember := cloneHostedLiveStorageSnapshot(reverted)
	changedMember.members[0].member.MemberDigest = "changed"
	if err := bindHostedLiveRevert(after, changedMember, binding, revert); err == nil {
		t.Fatal("revert accepted a non-bound member change")
	}
	if err := bindHostedLiveRevert(after, reverted, binding, commands.RevertData{JournalUsed: filepath.Join(storageRoot, "logs", "foreign.json")}); err == nil {
		t.Fatal("revert accepted a foreign output journal")
	}
	if err := requireHostedLiveConvergence(reverted, reverted); err != nil {
		t.Fatalf("convergence error = %v", err)
	}
	converged := cloneHostedLiveStorageSnapshot(reverted)
	converged.members[1].member.RevertDigest = "changed"
	if err := requireHostedLiveConvergence(reverted, converged); err == nil {
		t.Fatal("convergence accepted a member delta")
	}
	converged = cloneHostedLiveStorageSnapshot(reverted)
	converged.generationsExists = true
	converged.generations = boundaryTree{"hidden-generation": {Kind: "file", Size: 1, Identity: identity}}
	if err := requireHostedLiveConvergence(reverted, converged); err == nil {
		t.Fatal("convergence accepted a hidden generation delta")
	}
}

func hostedLiveStoreMemberForTest(id, runID, journal, journalDigest string, ordinal uint64, reverted bool, targets []string) hostedLiveStoreMember {
	return hostedLiveStoreMember{
		runID:   runID,
		member:  configrestore.StoreMemberInspection{ID: id, Kind: configrestore.StoreMemberLegacy, Ordinal: ordinal, MemberDigest: id + "-digest", Reverted: reverted, LegacyJournalIdentity: journal, LegacyJournalDigest: journalDigest},
		actions: hostedLiveStoreActionsForTest(targets),
	}
}

func hostedLiveStoreActionsForTest(targets []string) []configrestore.StoreActionInspection {
	actions := make([]configrestore.StoreActionInspection, len(targets))
	for index, target := range targets {
		status := configrestore.StoreActionStatusSkippedMissingSource
		if index < 2 {
			status = configrestore.StoreActionStatusRestored
		}
		actions[index] = configrestore.StoreActionInspection{Index: index, Status: status, SourceIdentity: configrestore.InspectionIdentity(target), TargetIdentity: configrestore.InspectionIdentity(target)}
	}
	return actions
}

func hostedLiveRestoreResults(targets []string) []restore.RestoreResult {
	results := make([]restore.RestoreResult, len(targets))
	for index, target := range targets {
		status := "skipped_missing_source"
		if index < 2 {
			status = "restored"
		}
		results[index] = restore.RestoreResult{ID: "restore", Source: target, Target: target, Status: status, BackupCreated: false, BackupPath: ""}
	}
	return results
}
