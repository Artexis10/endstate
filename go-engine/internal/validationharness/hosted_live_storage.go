// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type hostedLiveStorageSnapshot struct {
	root                                                      string
	backups, store, logs, generations                         boundaryTree
	backupsExists, storeExists, logsExists, generationsExists bool
	members                                                   []hostedLiveStoreMember
}

// hostedLiveStorageRoot is an opaque storage authority derived only from an
// authenticated attempt-root capability.
type hostedLiveStorageRoot struct {
	path     string
	validate func(string) error
}

func (root hostedLiveStorageRoot) valid() error {
	if root.path == "" || root.validate == nil || !filepath.IsAbs(root.path) || filepath.Clean(root.path) != root.path || safepath.ValidateRoot(root.path) != nil {
		return fmt.Errorf("hosted live storage root is invalid")
	}
	return root.validate(root.path)
}

type hostedLiveStoreMember struct {
	runID   string
	member  configrestore.StoreMemberInspection
	actions []configrestore.StoreActionInspection
}

type hostedLiveRestoreProof struct {
	nestedApplyRunID, journalIdentity, journalDigest string
	declaredTargets                                  []string
	restoreResults                                   []restore.RestoreResult
}

type hostedLiveRestoreBinding struct {
	memberID, memberDigest, runID                string
	journalIdentity, journalDigest, revertDigest string
	ordinal                                      uint64
}

type hostedLiveStorageBoundary struct{ root string }

func (boundary hostedLiveStorageBoundary) ResolveHostPath(authored string, _ modules.ConfigInstance) (string, error) {
	return boundary.ResolveFilesystemIdentity(authored)
}
func (boundary hostedLiveStorageBoundary) ResolveFilesystemIdentity(identity string) (string, error) {
	if !filepath.IsAbs(identity) || filepath.Clean(identity) != identity {
		return "", fmt.Errorf("live storage identity is invalid")
	}
	return identity, boundary.ValidateFilesystemTarget(identity)
}
func (boundary hostedLiveStorageBoundary) ProjectFilesystemIdentity(absolute string) (string, error) {
	if err := boundary.ValidateFilesystemTarget(absolute); err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}
func (boundary hostedLiveStorageBoundary) ValidateFilesystemTarget(absolute string) error {
	relative, err := filepath.Rel(boundary.root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("live storage path escaped ENDSTATE_ROOT")
	}
	return nil
}

func inspectHostedLiveStore(root hostedLiveStorageRoot) ([]hostedLiveStoreMember, error) {
	if err := root.valid(); err != nil {
		return nil, fmt.Errorf("hosted live storage root is unavailable")
	}
	storeRoot := filepath.Join(root.path, "state", "config-restore", "v1")
	inspection, err := configrestore.InspectStoreWithBoundary(storeRoot, true, hostedLiveStorageBoundary{root: root.path})
	if err != nil {
		return nil, err
	}
	var members []hostedLiveStoreMember
	for _, run := range inspection.Runs() {
		for _, member := range run.Members() {
			members = append(members, hostedLiveStoreMember{runID: run.RunID, member: member, actions: member.Actions()})
		}
	}
	return members, nil
}

func bindHostedLiveFirstRestore(before, after hostedLiveStorageSnapshot, proof hostedLiveRestoreProof) (hostedLiveRestoreBinding, error) {
	return bindHostedLiveRestoreTransition(before, after, proof, true)
}

func bindHostedLiveRevert(before, after hostedLiveStorageSnapshot, binding hostedLiveRestoreBinding, revert commands.RevertData) error {
	if !sameHostedLiveStorageRoot(before, after) || !hostedLiveStorageContainsJournal(after.root, revert.JournalUsed) || revert.JournalUsed != binding.journalIdentity {
		return fmt.Errorf("hosted revert output journal is not bound to the owned restore member")
	}
	beforeMembers, afterMembers := hostedLiveStoreMemberMap(before.members), hostedLiveStoreMemberMap(after.members)
	beforeMember, beforeExists := beforeMembers[binding.memberID]
	afterMember, afterExists := afterMembers[binding.memberID]
	if !beforeExists || !afterExists || binding.memberID == "" || beforeMember.member.Kind != configrestore.StoreMemberLegacy || beforeMember.member.Reverted || beforeMember.member.MemberDigest != binding.memberDigest || beforeMember.runID != binding.runID || beforeMember.member.Ordinal != binding.ordinal || beforeMember.member.LegacyJournalIdentity != binding.journalIdentity || beforeMember.member.LegacyJournalDigest != binding.journalDigest || afterMember.member.Kind != configrestore.StoreMemberLegacy || !afterMember.member.Reverted || afterMember.member.RevertDigest == "" || afterMember.member.MemberDigest != binding.memberDigest || afterMember.runID != binding.runID || afterMember.member.Ordinal != binding.ordinal || afterMember.member.LegacyJournalIdentity != binding.journalIdentity || afterMember.member.LegacyJournalDigest != binding.journalDigest || len(before.members) != len(after.members) {
		return fmt.Errorf("hosted revert lacks the bound reverted legacy member")
	}
	for id, member := range beforeMembers {
		if id != binding.memberID && !reflect.DeepEqual(afterMembers[id], member) {
			return fmt.Errorf("hosted revert changed a non-bound store member")
		}
	}
	marker := "v1/legacy-reverts/" + binding.memberID + ".json"
	allowed := map[string]struct{}{"v1": {}, "v1/legacy-reverts": {}, marker: {}}
	if difference := boundaryAdditionsDifference(before.store, before.storeExists, after.store, after.storeExists, allowed); difference != "" {
		return fmt.Errorf("hosted revert storage delta differs from the bound marker: %s", difference)
	}
	if entry, exists := after.store[marker]; !exists || entry.Kind != "file" || entry.Size == 0 {
		return fmt.Errorf("hosted revert lacks the bound marker")
	}
	if before.backupsExists != after.backupsExists || !equalBoundaryTrees(before.backups, after.backups) || before.logsExists != after.logsExists || !equalBoundaryTrees(before.logs, after.logs) || before.generationsExists != after.generationsExists || !equalBoundaryTrees(before.generations, after.generations) {
		return fmt.Errorf("hosted revert changed storage outside the bound marker")
	}
	return nil
}

func bindHostedLiveRecovery(before, after hostedLiveStorageSnapshot, proof hostedLiveRestoreProof) (hostedLiveRestoreBinding, error) {
	return bindHostedLiveRestoreTransition(before, after, proof, false)
}

func requireHostedLiveConvergence(before, after hostedLiveStorageSnapshot) error {
	if !sameHostedLiveStorageRoot(before, after) || before.backupsExists != after.backupsExists || !equalBoundaryTrees(before.backups, after.backups) || before.storeExists != after.storeExists || !equalBoundaryTrees(before.store, after.store) || before.logsExists != after.logsExists || !equalBoundaryTrees(before.logs, after.logs) || before.generationsExists != after.generationsExists || !equalBoundaryTrees(before.generations, after.generations) || len(before.members) != len(after.members) {
		return fmt.Errorf("hosted convergence changed store member count")
	}
	for id, want := range hostedLiveStoreMemberMap(before.members) {
		got, exists := hostedLiveStoreMemberMap(after.members)[id]
		if !exists || !reflect.DeepEqual(got, want) {
			return fmt.Errorf("hosted convergence changed store member %q", id)
		}
	}
	return nil
}

func bindHostedLiveRestoreTransition(before, after hostedLiveStorageSnapshot, proof hostedLiveRestoreProof, requireExpectedJournal bool) (hostedLiveRestoreBinding, error) {
	if requireExpectedJournal && (proof.journalIdentity == "" || proof.journalDigest == "") || !requireExpectedJournal && (proof.journalIdentity != "" || proof.journalDigest != "") {
		return hostedLiveRestoreBinding{}, fmt.Errorf("hosted restore journal expectation is invalid")
	}
	if !sameHostedLiveStorageRoot(before, after) || before.backupsExists != after.backupsExists || !equalBoundaryTrees(before.backups, after.backups) {
		return hostedLiveRestoreBinding{}, fmt.Errorf("hosted wiped restore changed backup storage")
	}
	known := hostedLiveStoreMemberMap(before.members)
	afterMembers := hostedLiveStoreMemberMap(after.members)
	if len(afterMembers) != len(after.members) {
		return hostedLiveRestoreBinding{}, fmt.Errorf("hosted restore duplicated a store member")
	}
	for id, member := range known {
		if actual, exists := afterMembers[id]; !exists || !reflect.DeepEqual(actual, member) {
			return hostedLiveRestoreBinding{}, fmt.Errorf("hosted restore changed a preexisting store member")
		}
	}
	var added []hostedLiveStoreMember
	for id, member := range afterMembers {
		if _, exists := known[id]; !exists {
			added = append(added, member)
		}
	}
	if len(added) != 1 {
		return hostedLiveRestoreBinding{}, fmt.Errorf("hosted restore lacks one new legacy member")
	}
	member := added[0]
	if member.member.ID == "" || member.member.Kind != configrestore.StoreMemberLegacy || member.member.Reverted || member.member.MemberDigest == "" || member.member.LegacyJournalIdentity == "" || member.member.LegacyJournalDigest == "" || member.runID != proof.nestedApplyRunID || requireExpectedJournal && (member.member.LegacyJournalIdentity != proof.journalIdentity || member.member.LegacyJournalDigest != proof.journalDigest) {
		return hostedLiveRestoreBinding{}, fmt.Errorf("hosted restore member is not bound to the typed apply run and journal")
	}
	if err := validateHostedLiveRestoreActions(member.actions, proof); err != nil {
		return hostedLiveRestoreBinding{}, err
	}
	return hostedLiveRestoreBinding{memberID: member.member.ID, memberDigest: member.member.MemberDigest, runID: member.runID, journalIdentity: member.member.LegacyJournalIdentity, journalDigest: member.member.LegacyJournalDigest, ordinal: member.member.Ordinal}, nil
}

func validateHostedLiveRestoreActions(actions []configrestore.StoreActionInspection, proof hostedLiveRestoreProof) error {
	if proof.nestedApplyRunID == "" || len(proof.declaredTargets) != 6 || len(proof.restoreResults) != len(proof.declaredTargets) || len(actions) != len(proof.restoreResults) {
		return fmt.Errorf("hosted restore proof is incomplete")
	}
	targets := make(map[string]struct{}, len(proof.declaredTargets))
	for _, target := range proof.declaredTargets {
		if target == "" {
			return fmt.Errorf("hosted restore target is empty")
		}
		if _, duplicate := targets[target]; duplicate {
			return fmt.Errorf("hosted restore target is duplicated")
		}
		targets[target] = struct{}{}
	}
	seen := make(map[string]struct{}, len(proof.restoreResults))
	restored, skippedMissingSource := 0, 0
	for index, result := range proof.restoreResults {
		if _, declared := targets[result.Target]; !declared || result.Source == "" || result.BackupCreated || result.BackupPath != "" || result.Error != "" {
			return fmt.Errorf("hosted restore result is not backup-free exact target evidence")
		}
		var expectedStatus configrestore.StoreActionStatus
		switch result.Status {
		case "restored":
			restored++
			expectedStatus = configrestore.StoreActionStatusRestored
		case "skipped_missing_source":
			skippedMissingSource++
			expectedStatus = configrestore.StoreActionStatusSkippedMissingSource
		default:
			return fmt.Errorf("hosted restore result has an unsupported status")
		}
		if _, duplicate := seen[result.Target]; duplicate {
			return fmt.Errorf("hosted restore result target is duplicated")
		}
		seen[result.Target] = struct{}{}
		action := actions[index]
		if action.Index != index || action.Status != expectedStatus || action.TargetIdentity != configrestore.InspectionIdentity(result.Target) || action.SourceIdentity != configrestore.InspectionIdentity(result.Source) || action.Backup != (configrestore.StoreBackupInspection{}) {
			return fmt.Errorf("hosted store action does not match official restore result")
		}
	}
	if len(seen) != len(targets) || restored != 2 || skippedMissingSource != 4 {
		return fmt.Errorf("hosted restore result set differs from the seeded Notepad++ targets")
	}
	return nil
}

func hostedLiveStoreMemberMap(members []hostedLiveStoreMember) map[string]hostedLiveStoreMember {
	result := make(map[string]hostedLiveStoreMember, len(members))
	for _, member := range members {
		result[member.member.ID] = member
	}
	return result
}

func snapshotHostedLiveStorage(root hostedLiveStorageRoot) (hostedLiveStorageSnapshot, error) {
	if err := root.valid(); err != nil {
		return hostedLiveStorageSnapshot{}, err
	}
	snapshot := hostedLiveStorageSnapshot{root: root.path}
	var err error
	if snapshot.backups, snapshot.backupsExists, err = snapshotHostedLiveStorageTree(filepath.Join(root.path, "state", "backups")); err != nil {
		return hostedLiveStorageSnapshot{}, err
	}
	if snapshot.store, snapshot.storeExists, err = snapshotHostedLiveStorageTree(filepath.Join(root.path, "state", "config-restore")); err != nil {
		return hostedLiveStorageSnapshot{}, err
	}
	if snapshot.storeExists {
		if snapshot.members, err = inspectHostedLiveStore(root); err != nil {
			return hostedLiveStorageSnapshot{}, err
		}
	}
	if snapshot.logs, snapshot.logsExists, err = snapshotHostedLiveStorageTree(filepath.Join(root.path, "logs")); err != nil {
		return hostedLiveStorageSnapshot{}, err
	}
	if snapshot.generations, snapshot.generationsExists, err = snapshotHostedLiveStorageTree(filepath.Join(root.path, "state", "generations")); err != nil {
		return hostedLiveStorageSnapshot{}, err
	}
	return snapshot, nil
}

func cloneHostedLiveStorageSnapshot(snapshot hostedLiveStorageSnapshot) hostedLiveStorageSnapshot {
	clone := snapshot
	clone.backups = cloneHostedLiveBoundaryTree(snapshot.backups)
	clone.store = cloneHostedLiveBoundaryTree(snapshot.store)
	clone.logs = cloneHostedLiveBoundaryTree(snapshot.logs)
	clone.generations = cloneHostedLiveBoundaryTree(snapshot.generations)
	clone.members = append([]hostedLiveStoreMember(nil), snapshot.members...)
	for index := range clone.members {
		clone.members[index].actions = append([]configrestore.StoreActionInspection(nil), snapshot.members[index].actions...)
	}
	return clone
}

func cloneHostedLiveBoundaryTree(tree boundaryTree) boundaryTree {
	if tree == nil {
		return nil
	}
	clone := make(boundaryTree, len(tree))
	for path, entry := range tree {
		clone[path] = entry
	}
	return clone
}

func snapshotHostedLiveStorageTree(path string) (boundaryTree, bool, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return boundaryTree{}, false, nil
	} else if err != nil {
		return nil, false, err
	}
	tree, err := snapshotBoundaryTree(path)
	return tree, err == nil, err
}

func sameHostedLiveStorageRoot(left, right hostedLiveStorageSnapshot) bool {
	return left.root != "" && left.root == right.root && filepath.IsAbs(left.root) && filepath.Clean(left.root) == left.root
}

func hostedLiveStorageContainsJournal(root, journal string) bool {
	if root == "" || journal == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(journal) || filepath.Clean(journal) != journal {
		return false
	}
	relative, err := filepath.Rel(root, journal)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
