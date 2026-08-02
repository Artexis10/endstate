// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type rebuildStorageSnapshot struct {
	backups        boundaryTree
	backupsExisted bool
	store          boundaryTree
	storeExisted   bool
	storeMembers   map[string]configrestore.StoreMemberInspection
	logs           boundaryTree
	logsExisted    bool
}

func snapshotRebuildStorage(runtime *scenarioRuntime) (rebuildStorageSnapshot, *Failure) {
	if runtime == nil || runtime.Plan == nil || runtime.Plan.context == nil {
		return rebuildStorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "storage", "validation storage authority is unavailable")
	}
	backups, backupsExisted, err := runtime.snapshotOwnedTree(filepath.Join(runtime.Root, "state", "backups"))
	if err != nil {
		return rebuildStorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "backups", "snapshot rebuild backup storage")
	}
	store, storeExisted, err := runtime.snapshotOwnedTree(filepath.Join(runtime.Root, "state", "config-restore"))
	if err != nil {
		return rebuildStorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "storage", "snapshot config-restore storage")
	}
	storeMembers, err := snapshotLegacyStoreMembers(runtime, storeExisted)
	if err != nil {
		return rebuildStorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "storage", "inspect config-restore store: "+err.Error())
	}
	logs, logsExisted, err := runtime.snapshotOwnedTree(filepath.Join(runtime.Root, "logs"))
	if err != nil {
		return rebuildStorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "journal", "snapshot rebuild journal storage")
	}
	return rebuildStorageSnapshot{
		backups: backups, backupsExisted: backupsExisted,
		store: store, storeExisted: storeExisted, storeMembers: storeMembers,
		logs: logs, logsExisted: logsExisted,
	}, nil
}

func snapshotLegacyStoreMembers(runtime *scenarioRuntime, exists bool) (map[string]configrestore.StoreMemberInspection, error) {
	members := map[string]configrestore.StoreMemberInspection{}
	if !exists {
		return members, nil
	}
	inspection, err := configrestore.InspectStoreWithBoundary(
		filepath.Join(runtime.Root, "state", "config-restore", "v1"), true,
		v2HostBoundary{runtime.validationContext()},
	)
	if err != nil {
		return nil, err
	}
	for _, run := range inspection.Runs() {
		for _, member := range run.Members() {
			if member.ID == "" {
				return nil, fmt.Errorf("store member identity is empty")
			}
			if _, duplicate := members[member.ID]; duplicate {
				return nil, fmt.Errorf("store member identity is duplicated")
			}
			members[member.ID] = member
		}
	}
	return members, nil
}

func validateRebuildStorageEvidence(
	runtime *scenarioRuntime,
	raw []byte,
	iteration int,
	before rebuildStorageSnapshot,
) (rebuildEvidenceBinding, rebuildStorageSnapshot, *Failure) {
	after, failure := snapshotRebuildStorage(runtime)
	if failure != nil {
		return rebuildEvidenceBinding{}, rebuildStorageSnapshot{}, failure
	}
	if iteration < 0 || iteration > 2 {
		return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "storage", "rebuild storage iteration is invalid")
	}

	binding, err := rebuildBindingFromEvidence(raw)
	repeat := iteration == 2
	expectedBackupCount := runtime.Plan.OperationCount()
	if repeat {
		expectedBackupCount = 0
	}
	if err != nil || len(binding.BackupsByTarget) != expectedBackupCount || len(binding.SourcesByTarget) != runtime.Plan.OperationCount() {
		return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "backups", "backup evidence does not cover the exact fixture target set")
	}
	backupRoot := filepath.Join(runtime.Root, "state", "backups")
	seenBackups := make(map[string]struct{}, runtime.Plan.OperationCount())
	allowedBackupAdditions := map[string]struct{}{".": {}}
	if repeat {
		if before.storeExisted != after.storeExisted || !equalBoundaryTrees(before.store, after.store) {
			return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "storage", "repeat rebuild changed config-restore storage")
		}
		if before.backupsExisted != after.backupsExisted || !equalBoundaryTrees(before.backups, after.backups) {
			return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "backups", "repeat rebuild created or changed backup evidence")
		}
		if before.logsExisted != after.logsExisted || !equalBoundaryTrees(before.logs, after.logs) {
			return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "journal", "repeat rebuild created or changed journal evidence")
		}
		return binding, after, nil
	} else {
		for _, target := range runtime.Plan.RestoreTargets() {
			key := strings.ToLower(target.Authored)
			semantic, exists := binding.BackupsByTarget[key]
			if !exists {
				return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "backups", "fixture target lacks a bound backup")
			}
			physical, err := resolveEndstateStoragePath(runtime, semantic, backupRoot)
			if err != nil {
				return rebuildEvidenceBinding{}, after, fail(CodeIsolationFailure, "rebuild", "backups", "backup evidence escaped validation-owned backup storage")
			}
			identity := strings.ToLower(filepath.Clean(physical))
			if _, duplicate := seenBackups[identity]; duplicate {
				return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "backups", "multiple restore targets cited the same backup")
			}
			seenBackups[identity] = struct{}{}
			relative, _ := filepath.Rel(backupRoot, physical)
			relative = filepath.ToSlash(relative)
			if before.backupsExisted {
				if _, existed := before.backups[relative]; existed {
					return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "backups", "rebuild cited a backup that predates this restore")
				}
			}
			allowBoundarySubtree(after.backups, relative, allowedBackupAdditions)
			if failure := validateFixtureBackup(runtime, target, physical); failure != nil {
				return rebuildEvidenceBinding{}, after, failure
			}
		}
		if difference := boundaryAdditionsDifference(before.backups, before.backupsExisted, after.backups, after.backupsExisted, allowedBackupAdditions); difference != "" {
			return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "backups", "backup storage delta differs from the cited target set: "+difference)
		}
	}

	journalPath, failure := newRestoreJournal(runtime, before, after)
	if failure != nil {
		return rebuildEvidenceBinding{}, after, failure
	}
	journalData, _, err := safepath.ReadRegularFile(journalPath)
	if err != nil {
		return rebuildEvidenceBinding{}, after, fail(CodeIsolationFailure, "rebuild", "journal", "read pinned rebuild journal")
	}
	journal, err := restore.ParseJournal(journalData)
	if err != nil || strings.TrimSpace(journal.RunID) == "" || len(journal.Entries) != runtime.Plan.OperationCount() {
		return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "journal", "rebuild journal is malformed or incomplete")
	}
	if _, err := time.Parse(time.RFC3339, journal.Timestamp); err != nil {
		return rebuildEvidenceBinding{}, after, fail(CodeEnvelopeContract, "rebuild", "journal.timestamp", "rebuild journal timestamp is not RFC3339")
	}
	if failure := validateJournalEntries(runtime, journal, binding, false); failure != nil {
		return rebuildEvidenceBinding{}, after, failure
	}
	journalSemantic, err := runtime.Plan.context.DisplayPath(journalPath)
	if err != nil {
		return rebuildEvidenceBinding{}, after, fail(CodeIsolationFailure, "rebuild", "journal", "journal identity escaped validation storage")
	}
	binding.Journal = journalSemantic
	memberID, failure := validateLegacyStoreBinding(before, after, binding)
	if failure != nil {
		return rebuildEvidenceBinding{}, after, failure
	}
	binding.StoreMemberID = memberID
	return binding, after, nil
}

func validateLegacyStoreBinding(before, after rebuildStorageSnapshot, binding rebuildEvidenceBinding) (string, *Failure) {
	if !after.storeExisted {
		return "", fail(CodeEnvelopeContract, "rebuild", "storage", "restoring rebuild emitted no config-restore store")
	}
	var added configrestore.StoreMemberInspection
	addedCount := 0
	for id, member := range after.storeMembers {
		if _, existed := before.storeMembers[id]; existed {
			continue
		}
		added, addedCount = member, addedCount+1
	}
	if addedCount != 1 || added.Kind != configrestore.StoreMemberLegacy || added.Reverted || added.LegacyJournalIdentity != binding.Journal || added.LegacyJournalDigest == "" {
		return "", fail(CodeEnvelopeContract, "rebuild", "storage", "restoring rebuild lacks one bound legacy journal store member")
	}
	allowed := map[string]struct{}{
		".": {}, "v1/legacy-members/" + added.ID + ".json": {},
	}
	if !before.storeExisted {
		for _, path := range []string{
			"mutation.lock", "v1", "v1/transactions", "v1/legacy-members", "v1/legacy-reverts", "v1/legacy-revert-work",
		} {
			allowed[path] = struct{}{}
		}
	}
	if difference := boundaryAdditionsDifference(before.store, before.storeExisted, after.store, after.storeExisted, allowed); difference != "" {
		return "", fail(CodeEnvelopeContract, "rebuild", "storage", "config-restore store delta differs from one bound legacy member: "+difference)
	}
	memberPath := "v1/legacy-members/" + added.ID + ".json"
	if entry, exists := after.store[memberPath]; !exists || entry.Kind != "file" || entry.Size == 0 {
		return "", fail(CodeEnvelopeContract, "rebuild", "storage", "bound legacy store member record is malformed")
	}
	if !before.storeExisted {
		for _, path := range []string{".", "v1", "v1/transactions", "v1/legacy-members", "v1/legacy-reverts", "v1/legacy-revert-work"} {
			if entry, exists := after.store[path]; !exists || entry.Kind != "directory" {
				return "", fail(CodeEnvelopeContract, "rebuild", "storage", "config-restore store scaffolding is malformed")
			}
		}
		if entry, exists := after.store["mutation.lock"]; !exists || entry.Kind != "file" || entry.Size != 0 {
			return "", fail(CodeEnvelopeContract, "rebuild", "storage", "config-restore mutation lock differs")
		}
	}
	return added.ID, nil
}

func validateLegacyRevertStorage(runtime *scenarioRuntime, before rebuildStorageSnapshot, binding rebuildEvidenceBinding) *Failure {
	after, failure := snapshotRebuildStorage(runtime)
	if failure != nil {
		return failure
	}
	if binding.StoreMemberID == "" || !before.storeExisted || !after.storeExisted {
		return fail(CodeEnvelopeContract, "revert", "storage", "revert lacks a bound legacy store member")
	}
	beforeMember, exists := before.storeMembers[binding.StoreMemberID]
	if !exists || beforeMember.Kind != configrestore.StoreMemberLegacy || beforeMember.Reverted || beforeMember.LegacyJournalIdentity != binding.Journal || beforeMember.LegacyJournalDigest == "" {
		return fail(CodeEnvelopeContract, "revert", "storage", "bound legacy store member was not active before revert")
	}
	afterMember, exists := after.storeMembers[binding.StoreMemberID]
	if !exists || afterMember.Kind != configrestore.StoreMemberLegacy || !afterMember.Reverted || afterMember.RevertDigest == "" ||
		afterMember.MemberDigest != beforeMember.MemberDigest || afterMember.LegacyJournalIdentity != beforeMember.LegacyJournalIdentity || afterMember.LegacyJournalDigest != beforeMember.LegacyJournalDigest ||
		len(before.storeMembers) != len(after.storeMembers) {
		return fail(CodeEnvelopeContract, "revert", "storage", "bound legacy store member did not transition to a valid reverted state")
	}
	markerPath := "v1/legacy-reverts/" + binding.StoreMemberID + ".json"
	if difference := boundaryAdditionsDifference(before.store, before.storeExisted, after.store, after.storeExisted, map[string]struct{}{markerPath: {}}); difference != "" {
		return fail(CodeEnvelopeContract, "revert", "storage", "config-restore revert delta differs from one bound marker: "+difference)
	}
	if entry, exists := after.store[markerPath]; !exists || entry.Kind != "file" || entry.Size == 0 {
		return fail(CodeEnvelopeContract, "revert", "storage", "bound legacy revert marker is malformed")
	}
	return nil
}

func resolveEndstateStoragePath(runtime *scenarioRuntime, semantic, scope string) (string, error) {
	const prefix = "$ENDSTATE_ROOT/"
	if runtime == nil || runtime.Plan == nil || runtime.Plan.context == nil || !strings.HasPrefix(semantic, prefix) || strings.Contains(semantic, `\`) {
		return "", fmt.Errorf("invalid semantic path")
	}
	relative := strings.TrimPrefix(semantic, prefix)
	if relative == "" || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative {
		return "", fmt.Errorf("non-canonical semantic path")
	}
	physical := filepath.Join(runtime.Root, filepath.FromSlash(relative))
	if !fixtureContained(scope, physical) || runtime.Plan.context.ValidateSandboxPath(physical) != nil {
		return "", fmt.Errorf("semantic path escaped scope")
	}
	display, err := runtime.Plan.context.DisplayPath(physical)
	if err != nil || display != semantic {
		return "", fmt.Errorf("semantic path is not canonical")
	}
	return physical, nil
}

func newRestoreJournal(runtime *scenarioRuntime, before, after rebuildStorageSnapshot) (string, *Failure) {
	if !after.logsExisted {
		return "", fail(CodeEnvelopeContract, "rebuild", "journal", "rebuild emitted no journal storage")
	}
	var journalRelative string
	for path, entry := range after.logs {
		if path == "." || entry.Kind != "file" || !strings.HasPrefix(filepath.Base(path), "restore-journal-") || !strings.HasSuffix(path, ".json") {
			continue
		}
		if before.logsExisted {
			if _, existed := before.logs[path]; existed {
				continue
			}
		}
		if journalRelative != "" {
			return "", fail(CodeEnvelopeContract, "rebuild", "journal", "rebuild emitted multiple new restore journals")
		}
		journalRelative = path
	}
	if journalRelative == "" {
		return "", fail(CodeEnvelopeContract, "rebuild", "journal", "rebuild emitted no new restore journal")
	}
	allowed := map[string]struct{}{".": {}, journalRelative: {}}
	if difference := boundaryAdditionsDifference(before.logs, before.logsExisted, after.logs, after.logsExisted, allowed); difference != "" {
		return "", fail(CodeEnvelopeContract, "rebuild", "journal", "journal storage delta differs from one new journal: "+difference)
	}
	journalPath := filepath.Join(runtime.Root, "logs", filepath.FromSlash(journalRelative))
	if err := runtime.Plan.context.ValidateSandboxPath(journalPath); err != nil {
		return "", fail(CodeIsolationFailure, "rebuild", "journal", "new restore journal escaped validation storage")
	}
	return journalPath, nil
}

func validateJournalEntries(runtime *scenarioRuntime, journal *restore.Journal, binding rebuildEvidenceBinding, repeat bool) *Failure {
	expected := make(map[string]string, runtime.Plan.OperationCount())
	for _, target := range runtime.Plan.RestoreTargets() {
		expected[strings.ToLower(target.Authored)] = target.Strategy
	}
	if len(expected) != runtime.Plan.OperationCount() {
		return fail(CodeEnvelopeContract, "rebuild", "journal.entries", "fixture restore target identities are not unique")
	}
	for _, entry := range journal.Entries {
		key := strings.ToLower(entry.TargetPath)
		strategy, known := expected[key]
		restoreType := entry.RestoreType
		if restoreType == "" {
			restoreType = "copy"
		}
		common := known && entry.ResolvedSourcePath == binding.SourcesByTarget[key] && entry.TargetExistedBefore && entry.Error == "" && restoreType == strategy
		restored := !repeat && entry.BackupRequested && entry.BackupCreated && entry.BackupPath == binding.BackupsByTarget[key] && entry.Action == "restored"
		converged := repeat && !entry.BackupRequested && !entry.BackupCreated && entry.BackupPath == "" && entry.Action == "skipped_up_to_date"
		if !common || !restored && !converged {
			return fail(CodeEnvelopeContract, "rebuild", "journal.entries", "journal entry does not exactly bind a successful restore item")
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		return fail(CodeEnvelopeContract, "rebuild", "journal.entries", "journal omitted a fixture restore target")
	}
	return nil
}

func validateFixtureBackup(runtime *scenarioRuntime, target FixtureRestoreTarget, backup string) *Failure {
	if target.Registry {
		info, err := os.Lstat(backup)
		if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() || info.Size() == 0 || !strings.EqualFold(filepath.Ext(backup), ".reg") {
			return fail(CodeContentMismatch, "rebuild", target.Coordinate, "registry backup is not a regular nonempty .reg file")
		}
		data, _, err := safepath.ReadRegularFile(backup)
		if err != nil {
			return fail(CodeContentMismatch, "rebuild", target.Coordinate, "registry backup cannot be read safely")
		}
		if _, err := registryfile.RewriteSubtree(data, target.Authored, target.Authored); err != nil {
			return fail(CodeContentMismatch, "rebuild", target.Coordinate, "registry backup is malformed or leaves its authored scope")
		}
		return nil
	}
	for _, fileTarget := range runtime.Plan.Targets {
		if fileTarget.Authored == target.Authored && fileTarget.Destination == target.Destination {
			return validateMutatedBackup(runtime, fileTarget, backup)
		}
	}
	return fail(CodeIsolationFailure, "rebuild", target.Coordinate, "file backup target is absent from fixture plan")
}

func allowBoundarySubtree(tree boundaryTree, root string, allowed map[string]struct{}) {
	for path := range tree {
		if path == root || strings.HasPrefix(path, root+"/") {
			allowed[path] = struct{}{}
		}
	}
	for parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(root))); parent != "."; parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent))) {
		allowed[parent] = struct{}{}
	}
}

func boundaryAdditionsDifference(before boundaryTree, beforeExisted bool, after boundaryTree, afterExisted bool, allowed map[string]struct{}) string {
	if !afterExisted {
		return "storage root is absent"
	}
	if beforeExisted {
		for path, expected := range before {
			actual, exists := after[path]
			if !exists {
				return fmt.Sprintf("existing member %q was removed", path)
			}
			if difference := boundaryEntryDifference(expected, actual); difference != "" {
				return fmt.Sprintf("existing member %q %s", path, difference)
			}
		}
	}
	for path := range after {
		if beforeExisted {
			if _, existed := before[path]; existed {
				continue
			}
		}
		if _, ok := allowed[path]; !ok {
			return fmt.Sprintf("uncited member %q was added", path)
		}
	}
	return ""
}

func validateMutatedBackup(runtime *scenarioRuntime, target FixtureTarget, backup string) *Failure {
	info, err := os.Lstat(backup)
	if err != nil || safepath.IsLinkOrReparse(info) || info.IsDir() != target.Directory || !info.IsDir() && !info.Mode().IsRegular() {
		return fail(CodeContentMismatch, "rebuild", target.Coordinate, "backup root type differs from the pre-rebuild fixture")
	}
	expected := map[string]expectedFixtureEntry{".": {Directory: target.Directory}}
	if target.Directory {
		relative, ok := targetPayloadRelative(target)
		if !ok {
			return fail(CodeIsolationFailure, "rebuild", target.Coordinate, "directory payload left fixture authority")
		}
		expected[filepath.FromSlash(relative)] = expectedFixtureEntry{Content: target.Mutated}
		for parent := filepath.Dir(filepath.FromSlash(relative)); parent != "."; parent = filepath.Dir(parent) {
			expected[parent] = expectedFixtureEntry{Directory: true}
		}
		for _, excluded := range target.RestoreExcluded {
			relative, err := filepath.Rel(target.Resolved, excluded.Path)
			if err != nil || relative == "." || !fixtureContained(target.Resolved, excluded.Path) {
				return fail(CodeIsolationFailure, "rebuild", target.Coordinate, "restore-excluded backup witness escaped fixture authority")
			}
			expected[relative] = expectedFixtureEntry{Content: excluded.Mutated}
			for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
				expected[parent] = expectedFixtureEntry{Directory: true}
			}
		}
	} else {
		expected["."] = expectedFixtureEntry{Content: target.Mutated}
	}
	seen := make(map[string]struct{}, len(expected))
	err = filepath.Walk(backup, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if runtime.Plan.context.ValidateSandboxPath(path) != nil || !fixtureContained(backup, path) || safepath.IsLinkOrReparse(info) || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe backup member")
		}
		relative, err := filepath.Rel(backup, path)
		if err != nil {
			return err
		}
		entry, exists := expected[relative]
		if !exists || entry.Directory != info.IsDir() {
			return fmt.Errorf("backup tree differs")
		}
		if !info.IsDir() {
			data, _, err := safepath.ReadRegularFile(path)
			if err != nil || string(data) != entry.Content {
				return fmt.Errorf("backup content differs")
			}
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil || len(seen) != len(expected) {
		return fail(CodeContentMismatch, "rebuild", target.Coordinate, "backup tree or bytes differ from the exact pre-rebuild fixture")
	}
	return nil
}
