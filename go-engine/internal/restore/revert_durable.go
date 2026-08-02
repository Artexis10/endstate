// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const durableLegacyRevertVersion = 1

var durableRevertCheckpoint = func(string, int) error { return nil }

type durableLegacyRevertState struct {
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
}

type durableLegacyRevertPrepared struct {
	Version       int                      `json:"version"`
	EntryIndex    int                      `json:"entryIndex"`
	EntryDigest   string                   `json:"entryDigest"`
	Target        string                   `json:"target"`
	Before        durableLegacyRevertState `json:"before"`
	Desired       durableLegacyRevertState `json:"desired"`
	DesiredSource string                   `json:"desiredSource,omitempty"`
	StagePath     string                   `json:"stagePath,omitempty"`
	HeldPath      string                   `json:"heldPath,omitempty"`
}

type durableLegacyRevertCompleted struct {
	Version     int    `json:"version"`
	EntryIndex  int    `json:"entryIndex"`
	EntryDigest string `json:"entryDigest"`
	Action      string `json:"action"`
}

// RunRevertDurable reverts legacy filesystem entries with an immutable
// per-entry before-state record. A retry may continue only from the exact
// state recorded before undo or from the verified desired prior state; any
// unrelated edit fails closed without being overwritten.
func RunRevertDurable(journal *Journal, backupDir, workRoot string) ([]RevertResult, error) {
	return RunRevertDurableWithValidation(journal, backupDir, workRoot, nil)
}

// RunRevertDurableWithValidation reverts a legacy journal while resolving
// semantic journal identities only at the filesystem and registry I/O boundary.
// A nil validation context is exactly the production RunRevertDurable path.
func RunRevertDurableWithValidation(
	journal *Journal,
	backupDir, workRoot string,
	context *validationmode.Context,
) (_ []RevertResult, returnErr error) {
	boundary := legacyValidationBoundary{context: context, backupDir: backupDir}
	defer func() {
		if returnErr != nil && context != nil {
			returnErr = fmt.Errorf("%s", replaceFoldMany(returnErr.Error(), [][2]string{
				{context.Root(), "$ENDSTATE_ROOT"},
				{context.RegistryNamespace(), "HKCU"},
				{context.Descriptor().Nonce, "validation"},
			}))
		}
	}()
	if journal == nil {
		return nil, fmt.Errorf("restore journal is required")
	}
	if workRoot == "" || !filepath.IsAbs(workRoot) || filepath.Clean(workRoot) != workRoot {
		return nil, fmt.Errorf("legacy revert work root must be a clean absolute path")
	}
	if err := ValidateFilesystemTarget(workRoot); err != nil {
		return nil, fmt.Errorf("validate legacy revert work root: %w", err)
	}
	if context != nil {
		if err := context.ValidateSandboxPath(workRoot); err != nil {
			return nil, fmt.Errorf("validate legacy revert work root: %w", err)
		}
	}
	info, err := os.Lstat(workRoot)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return nil, fmt.Errorf("legacy revert work root is not a safe directory")
	}
	if err := prepareDurableLegacyRevertEntries(journal, workRoot, boundary); err != nil {
		return nil, err
	}

	results := make([]RevertResult, 0, len(journal.Entries))
	for index := len(journal.Entries) - 1; index >= 0; index-- {
		entry := journal.Entries[index]
		if entry.Action != "restored" {
			results = append(results, RevertResult{Target: entry.TargetPath, Action: "skipped"})
			continue
		}
		if entry.RestoreType == "registry-import" || entry.RestoreType == "registry-set" {
			result, err := runDurableRegistryRevertEntry(entry, index, workRoot, boundary)
			if err != nil {
				return results, err
			}
			results = append(results, result)
			if result.Action != "skipped" {
				if err := durableRevertCheckpoint("after_entry_completed", index); err != nil {
					return results, err
				}
			}
			continue
		}
		result, err := runDurableFilesystemRevertEntry(entry, index, workRoot, boundary)
		if err != nil {
			return results, err
		}
		results = append(results, result)
		if result.Action != "skipped" {
			if err := durableRevertCheckpoint("after_entry_completed", index); err != nil {
				return results, err
			}
		}
	}
	return results, nil
}

// prepareDurableLegacyRevertEntries durably records the complete undo plan
// before the first target mutation. For repeated targets, the next reverse
// entry expects the prior entry's desired state rather than rescanning the
// still-current final state.
func prepareDurableLegacyRevertEntries(journal *Journal, workRoot string, boundary legacyValidationBoundary) error {
	virtual := make(map[string]durableLegacyRevertState)
	for index := len(journal.Entries) - 1; index >= 0; index-- {
		entry := journal.Entries[index]
		if entry.Action != "restored" {
			continue
		}
		entryDigest, err := durableLegacyJournalEntryDigest(entry)
		if err != nil {
			return err
		}
		preparedPath := filepath.Join(workRoot, fmt.Sprintf("entry-%06d.json", index))
		prepared, found, err := readDurableLegacyPrepared(preparedPath)
		if err != nil {
			return err
		}

		if entry.RestoreType == "registry-import" || entry.RestoreType == "registry-set" {
			if !entry.BackupCreated && entry.TargetExistedBefore {
				continue
			}
			actual, desired, err := durableLegacyRegistryStates(entry, workRoot, boundary)
			if err != nil {
				return err
			}
			key := durableLegacyVirtualTarget(entry)
			before, chained := virtual[key]
			if !chained {
				before = actual
			}
			stagePath, heldPath, err := durableLegacyRegistryScratchTargets(entry, entryDigest, boundary)
			if err != nil {
				return err
			}
			expected := durableLegacyRevertPrepared{
				Version: durableLegacyRevertVersion, EntryIndex: index, EntryDigest: entryDigest,
				Target: entry.TargetPath, Before: before, Desired: desired, DesiredSource: entry.BackupPath,
				StagePath: stagePath, HeldPath: heldPath,
			}
			if !found {
				if err := validateDurableLegacyRegistryScratchAvailable(stagePath, heldPath, workRoot, boundary); err != nil {
					return err
				}
				if err := writeImmutableDurableJSON(preparedPath, expected); err != nil {
					return err
				}
				prepared = expected
			} else if err := validateDurableLegacyPrepared(prepared, expected, chained); err != nil {
				return fmt.Errorf("legacy registry revert prepared record differs from journal entry %d: %w", index, err)
			}
			virtual[key] = prepared.Desired
			continue
		}

		desired, desiredSource, mutates, err := durableLegacyDesiredState(entry, boundary)
		if err != nil {
			return err
		}
		if !mutates {
			continue
		}
		key := durableLegacyVirtualTarget(entry)
		before, chained := virtual[key]
		if !chained {
			before, err = scanDurableLegacyFilesystemState(entry.TargetPath, boundary)
			if err != nil {
				return fmt.Errorf("capture revert target %q: %w", entry.TargetPath, err)
			}
		}
		suffix := entryDigest[:16]
		expected := durableLegacyRevertPrepared{
			Version: durableLegacyRevertVersion, EntryIndex: index, EntryDigest: entryDigest,
			Target: filepath.Clean(entry.TargetPath), Before: before, Desired: desired, DesiredSource: desiredSource,
			StagePath: semanticFilesystemScratch(entry.TargetPath, suffix, "stage"),
			HeldPath:  semanticFilesystemScratch(entry.TargetPath, suffix, "held"),
		}
		if !found {
			for _, identity := range []string{expected.StagePath, expected.HeldPath} {
				path, resolveErr := boundary.resolveHost(identity)
				if resolveErr != nil {
					return resolveErr
				}
				if err := boundary.validateConcrete(path); err != nil {
					return err
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					if err == nil {
						err = fmt.Errorf("path already exists")
					}
					return fmt.Errorf("legacy revert scratch path %q is unavailable: %w", identity, err)
				}
			}
			if err := writeImmutableDurableJSON(preparedPath, expected); err != nil {
				return err
			}
			prepared = expected
		} else if err := validateDurableLegacyPrepared(prepared, expected, chained); err != nil {
			return fmt.Errorf("legacy revert prepared record differs from journal entry %d: %w", index, err)
		}
		virtual[key] = prepared.Desired
	}
	return nil
}

func validateDurableLegacyPrepared(actual, expected durableLegacyRevertPrepared, compareBefore bool) error {
	if actual.Version != expected.Version || actual.EntryIndex != expected.EntryIndex ||
		actual.EntryDigest != expected.EntryDigest || actual.Target != expected.Target ||
		actual.Desired != expected.Desired || actual.DesiredSource != expected.DesiredSource ||
		actual.StagePath != expected.StagePath || actual.HeldPath != expected.HeldPath {
		return fmt.Errorf("identity or desired state changed")
	}
	if compareBefore && actual.Before != expected.Before {
		return fmt.Errorf("chained before-state changed")
	}
	return nil
}

func durableLegacyVirtualTarget(entry JournalEntry) string {
	if entry.RestoreType == "registry-import" || entry.RestoreType == "registry-set" {
		return "registry\x00" + strings.ToLower(entry.TargetPath)
	}
	target := filepath.Clean(entry.TargetPath)
	if runtime.GOOS == "windows" {
		target = strings.ToLower(target)
	}
	return "filesystem\x00" + target
}

func runDurableRegistryRevertEntry(entry JournalEntry, index int, workRoot string, boundary legacyValidationBoundary) (RevertResult, error) {
	if !entry.BackupCreated && entry.TargetExistedBefore {
		return RevertResult{Target: entry.TargetPath, Action: "skipped"}, nil
	}
	entryDigest, err := durableLegacyJournalEntryDigest(entry)
	if err != nil {
		return RevertResult{}, err
	}
	preparedPath := filepath.Join(workRoot, fmt.Sprintf("entry-%06d.json", index))
	completedPath := filepath.Join(workRoot, fmt.Sprintf("entry-%06d-completed.json", index))
	if completed, found, err := readDurableLegacyCompletion(completedPath, index, entryDigest); err != nil {
		return RevertResult{}, err
	} else if found {
		return RevertResult{Target: entry.TargetPath, Action: completed.Action, BackupUsed: entry.BackupPath}, nil
	}

	_, desired, err := durableLegacyRegistryStates(entry, workRoot, boundary)
	if err != nil {
		return RevertResult{}, err
	}
	prepared, found, err := readDurableLegacyPrepared(preparedPath)
	if err != nil {
		return RevertResult{}, err
	}
	if !found {
		return RevertResult{}, fmt.Errorf("legacy registry revert entry %d was not durably prepared", index)
	}
	stagePath, heldPath, err := durableLegacyRegistryScratchTargets(entry, entryDigest, boundary)
	if err != nil {
		return RevertResult{}, err
	}
	if prepared.Version != durableLegacyRevertVersion || prepared.EntryIndex != index ||
		prepared.EntryDigest != entryDigest || prepared.Target != entry.TargetPath || prepared.Desired != desired ||
		prepared.DesiredSource != entry.BackupPath || prepared.StagePath != stagePath || prepared.HeldPath != heldPath {
		return RevertResult{}, fmt.Errorf("legacy registry revert prepared record differs from journal entry %d", index)
	}

	if entry.RestoreType == "registry-import" && entry.BackupCreated && entry.BackupPath != "" {
		if err := applyDurableLegacyRegistryImportSwap(entry, prepared, index, workRoot, boundary); err != nil {
			return RevertResult{}, err
		}
	} else {
		current, _, err := durableLegacyRegistryStates(entry, workRoot, boundary)
		if err != nil {
			return RevertResult{}, err
		}
		if current != prepared.Desired {
			if current != prepared.Before {
				return RevertResult{}, fmt.Errorf("legacy registry revert target %q changed after its durable before-state was recorded", entry.TargetPath)
			}
			if err := applyDurableLegacyRegistryRevert(entry, index, boundary); err != nil {
				return RevertResult{}, err
			}
		}
	}
	if err := durableRevertCheckpoint("after_target_replaced", index); err != nil {
		return RevertResult{}, err
	}
	current, _, err := durableLegacyRegistryStates(entry, workRoot, boundary)
	if err != nil {
		return RevertResult{}, err
	}
	if current != prepared.Desired {
		return RevertResult{}, fmt.Errorf("legacy registry revert target %q does not match its recorded prior state", entry.TargetPath)
	}
	action := "reverted"
	if desired.Kind == "absent" {
		action = "deleted"
	}
	completed := durableLegacyRevertCompleted{
		Version: durableLegacyRevertVersion, EntryIndex: index, EntryDigest: entryDigest, Action: action,
	}
	if err := writeImmutableDurableJSON(completedPath, completed); err != nil {
		return RevertResult{}, err
	}
	return RevertResult{Target: entry.TargetPath, Action: action, BackupUsed: entry.BackupPath}, nil
}

func runDurableFilesystemRevertEntry(entry JournalEntry, index int, workRoot string, boundary legacyValidationBoundary) (RevertResult, error) {
	entryDigest, err := durableLegacyJournalEntryDigest(entry)
	if err != nil {
		return RevertResult{}, err
	}
	preparedPath := filepath.Join(workRoot, fmt.Sprintf("entry-%06d.json", index))
	completedPath := filepath.Join(workRoot, fmt.Sprintf("entry-%06d-completed.json", index))
	if completed, found, err := readDurableLegacyCompletion(completedPath, index, entryDigest); err != nil {
		return RevertResult{}, err
	} else if found {
		return RevertResult{Target: entry.TargetPath, Action: completed.Action, BackupUsed: entry.BackupPath}, nil
	}

	desired, desiredSource, mutates, err := durableLegacyDesiredState(entry, boundary)
	if err != nil {
		return RevertResult{}, err
	}
	if !mutates {
		return RevertResult{Target: entry.TargetPath, Action: "skipped"}, nil
	}

	prepared, found, err := readDurableLegacyPrepared(preparedPath)
	if err != nil {
		return RevertResult{}, err
	}
	if !found {
		return RevertResult{}, fmt.Errorf("legacy revert entry %d was not durably prepared", index)
	}
	if prepared.Version != durableLegacyRevertVersion || prepared.EntryIndex != index ||
		prepared.EntryDigest != entryDigest || prepared.Target != filepath.Clean(entry.TargetPath) ||
		prepared.Desired != desired || prepared.DesiredSource != desiredSource {
		return RevertResult{}, fmt.Errorf("legacy revert prepared record differs from journal entry %d", index)
	}

	if err := applyDurableLegacyFilesystemRevert(prepared, index, boundary); err != nil {
		return RevertResult{}, err
	}
	action := "reverted"
	if desired.Kind == "absent" {
		action = "deleted"
	}
	completed := durableLegacyRevertCompleted{
		Version: durableLegacyRevertVersion, EntryIndex: index, EntryDigest: entryDigest, Action: action,
	}
	if err := writeImmutableDurableJSON(completedPath, completed); err != nil {
		return RevertResult{}, err
	}
	return RevertResult{Target: entry.TargetPath, Action: action, BackupUsed: entry.BackupPath}, nil
}

func durableLegacyDesiredState(entry JournalEntry, boundary legacyValidationBoundary) (durableLegacyRevertState, string, bool, error) {
	if entry.BackupCreated && entry.BackupPath != "" {
		backupPath, resolveErr := boundary.resolveBackup(entry.BackupPath)
		if resolveErr != nil {
			return durableLegacyRevertState{}, "", false, resolveErr
		}
		if _, err := os.Lstat(backupPath); err == nil {
			state, err := scanDurableLegacyFilesystemStatePath(backupPath, boundary)
			return state, filepath.Clean(entry.BackupPath), true, err
		} else if !os.IsNotExist(err) {
			return durableLegacyRevertState{}, "", false, err
		}
	}
	if !entry.TargetExistedBefore {
		return absentDurableLegacyState(), "", true, nil
	}
	return durableLegacyRevertState{}, "", false, nil
}

func applyDurableLegacyFilesystemRevert(prepared durableLegacyRevertPrepared, index int, boundary legacyValidationBoundary) error {
	targetState, err := scanDurableLegacyFilesystemState(prepared.Target, boundary)
	if err != nil {
		return err
	}
	if targetState == prepared.Desired {
		if stage, exists, err := scanOptionalDurableLegacyState(prepared.StagePath, boundary); err != nil {
			return err
		} else if exists {
			if stage != prepared.Desired {
				return fmt.Errorf("legacy revert stage changed after target replacement")
			}
			if err := removeDurableLegacyScratch(prepared.StagePath, boundary); err != nil {
				return err
			}
		}
		if held, exists, err := scanOptionalDurableLegacyState(prepared.HeldPath, boundary); err != nil {
			return err
		} else if exists {
			if held != prepared.Before {
				return fmt.Errorf("legacy revert held target changed after replacement")
			}
			if err := removeDurableLegacyScratch(prepared.HeldPath, boundary); err != nil {
				return err
			}
		}
		return nil
	}
	heldState, heldExists, err := scanOptionalDurableLegacyState(prepared.HeldPath, boundary)
	if err != nil {
		return err
	}
	if targetState != prepared.Before {
		if targetState.Kind == "absent" && heldExists && heldState == prepared.Before {
			// A prior attempt stopped after atomically moving the original target.
		} else {
			return fmt.Errorf("legacy revert target %q changed after its durable before-state was recorded", prepared.Target)
		}
	}

	if prepared.Desired.Kind != "absent" {
		if err := ensureDurableLegacyStage(prepared, boundary); err != nil {
			return err
		}
	}
	if !heldExists && targetState.Kind != "absent" {
		if err := renameDurableLegacyPath(prepared.Target, prepared.HeldPath, boundary); err != nil {
			return err
		}
		if err := durableRevertCheckpoint("after_target_held", index); err != nil {
			return err
		}
		heldExists = true
	}
	if prepared.Desired.Kind != "absent" {
		current, err := scanDurableLegacyFilesystemState(prepared.Target, boundary)
		if err != nil {
			return err
		}
		if current.Kind == "absent" {
			if err := renameDurableLegacyPath(prepared.StagePath, prepared.Target, boundary); err != nil {
				return err
			}
		}
	}
	if err := durableRevertCheckpoint("after_target_replaced", index); err != nil {
		return err
	}
	actual, err := scanDurableLegacyFilesystemState(prepared.Target, boundary)
	if err != nil {
		return err
	}
	if actual != prepared.Desired {
		return fmt.Errorf("legacy revert target %q does not match its recorded prior state", prepared.Target)
	}
	if heldExists {
		if err := removeDurableLegacyScratch(prepared.HeldPath, boundary); err != nil {
			return err
		}
	}
	return removeDurableLegacyScratch(prepared.StagePath, boundary)
}

func ensureDurableLegacyStage(prepared durableLegacyRevertPrepared, boundary legacyValidationBoundary) error {
	if state, exists, err := scanOptionalDurableLegacyState(prepared.StagePath, boundary); err != nil {
		return err
	} else if exists {
		if state != prepared.Desired {
			return fmt.Errorf("legacy revert stage %q differs from recorded prior state", prepared.StagePath)
		}
		return nil
	}
	if prepared.DesiredSource == "" {
		return fmt.Errorf("legacy revert desired source is missing")
	}
	desiredSource, err := boundary.resolveBackup(prepared.DesiredSource)
	if err != nil {
		return err
	}
	stagePath, err := boundary.resolveHost(prepared.StagePath)
	if err != nil {
		return err
	}
	if err := boundary.validateConcrete(stagePath); err != nil {
		return err
	}
	info, err := os.Lstat(desiredSource)
	if err != nil {
		return err
	}
	if isLinkOrReparse(info) {
		return fmt.Errorf("legacy revert backup is a link or reparse point")
	}
	if info.IsDir() {
		if err := os.Mkdir(stagePath, info.Mode().Perm()); err != nil {
			return err
		}
		if err := copyDirRecursive(desiredSource, stagePath, nil); err != nil {
			_ = removeDurableLegacyScratch(prepared.StagePath, boundary)
			return err
		}
	} else if info.Mode().IsRegular() {
		if err := atomicRestoreCopy(desiredSource, stagePath); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("legacy revert backup has unsupported type")
	}
	if err := syncDurableLegacyTree(stagePath); err != nil {
		return err
	}
	if err := syncDurableLegacyDirectory(filepath.Dir(stagePath)); err != nil {
		return err
	}
	state, err := scanDurableLegacyFilesystemState(prepared.StagePath, boundary)
	if err != nil {
		return err
	}
	if state != prepared.Desired {
		return fmt.Errorf("legacy revert stage differs from recorded prior state")
	}
	return nil
}

func renameDurableLegacyPath(sourceIdentity, destinationIdentity string, boundary legacyValidationBoundary) error {
	source, err := boundary.resolveHost(sourceIdentity)
	if err != nil {
		return err
	}
	destination, err := boundary.resolveHost(destinationIdentity)
	if err != nil {
		return err
	}
	if err := boundary.validateConcrete(source); err != nil {
		return err
	}
	if err := boundary.validateConcrete(destination); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		if err == nil {
			err = fmt.Errorf("destination exists")
		}
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	if err := boundary.validateConcrete(destination); err != nil {
		return err
	}
	sourceParent := filepath.Dir(source)
	destinationParent := filepath.Dir(destination)
	if err := syncDurableLegacyDirectory(sourceParent); err != nil {
		return err
	}
	if destinationParent != sourceParent {
		return syncDurableLegacyDirectory(destinationParent)
	}
	return nil
}

func removeDurableLegacyScratch(identity string, boundary legacyValidationBoundary) error {
	if identity == "" {
		return nil
	}
	path, err := boundary.resolveHost(identity)
	if err != nil {
		return err
	}
	return removeDurableLegacyScratchPath(path, boundary)
}

func removeDurableLegacyScratchPath(path string, boundary legacyValidationBoundary) error {
	if err := boundary.validateConcrete(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := removeDurableLegacyScratchPath(filepath.Join(path, entry.Name()), boundary); err != nil {
				return err
			}
		}
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("legacy revert scratch path %q has unsupported type", path)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDurableLegacyDirectory(filepath.Dir(path))
}

func syncDurableLegacyTree(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		return syncDurableLegacyFile(path)
	}
	if !info.IsDir() || isLinkOrReparse(info) {
		return fmt.Errorf("durability path %q has unsupported type", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := syncDurableLegacyTree(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return syncDurableLegacyDirectory(path)
}

func scanOptionalDurableLegacyState(identity string, boundary legacyValidationBoundary) (durableLegacyRevertState, bool, error) {
	state, err := scanDurableLegacyFilesystemState(identity, boundary)
	if err != nil {
		return durableLegacyRevertState{}, false, err
	}
	return state, state.Kind != "absent", nil
}

func scanDurableLegacyFilesystemState(identity string, boundary legacyValidationBoundary) (durableLegacyRevertState, error) {
	target, err := boundary.resolveHost(identity)
	if err != nil {
		return durableLegacyRevertState{}, err
	}
	return scanDurableLegacyFilesystemStatePath(target, boundary)
}

func scanDurableLegacyFilesystemStatePath(target string, boundary legacyValidationBoundary) (durableLegacyRevertState, error) {
	if err := boundary.validateConcrete(target); err != nil {
		return durableLegacyRevertState{}, err
	}
	info, err := boundary.lstat("durable-scan-root-lstat", target)
	if os.IsNotExist(err) {
		return absentDurableLegacyState(), nil
	}
	if err != nil {
		return durableLegacyRevertState{}, err
	}
	if isLinkOrReparse(info) {
		return durableLegacyRevertState{}, fmt.Errorf("legacy revert path %q is a link or reparse point", target)
	}
	type entry struct {
		path, kind, digest string
		mode               os.FileMode
	}
	entries := []entry{}
	err = walkTreeWithBoundary(target, boundary, "durable-scan", func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("legacy revert path %q contains a link or reparse point", path)
		}
		if err := boundary.validateConcrete(path); err != nil {
			return err
		}
		relative, err := filepath.Rel(target, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		item := entry{path: relative, mode: info.Mode().Perm()}
		switch {
		case info.IsDir():
			item.kind = "directory"
		case info.Mode().IsRegular():
			item.kind = "file"
			data, mode, err := boundary.readRegularFile("durable-scan-member-read", path)
			if err != nil {
				return err
			}
			item.mode = mode.Perm()
			sum := sha256.Sum256(data)
			item.digest = hex.EncodeToString(sum[:])
		default:
			return fmt.Errorf("legacy revert path %q has unsupported type", path)
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		return durableLegacyRevertState{}, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].path < entries[right].path })
	hasher := sha256.New()
	writeDurableDigestString(hasher, "endstate-legacy-revert-filesystem-v1")
	for _, item := range entries {
		writeDurableDigestString(hasher, item.path)
		writeDurableDigestString(hasher, item.kind)
		writeDurableDigestString(hasher, fmt.Sprintf("%o", item.mode.Perm()))
		writeDurableDigestString(hasher, item.digest)
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return durableLegacyRevertState{Kind: kind, Digest: hex.EncodeToString(hasher.Sum(nil))}, nil
}

func absentDurableLegacyState() durableLegacyRevertState {
	sum := sha256.Sum256([]byte("endstate-legacy-revert-filesystem-v1:absent"))
	return durableLegacyRevertState{Kind: "absent", Digest: hex.EncodeToString(sum[:])}
}

func writeDurableDigestString(writer io.Writer, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = io.WriteString(writer, value)
}

func durableLegacyJournalEntryDigest(entry JournalEntry) (string, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeImmutableDurableJSON(path string, value any) (resultErr error) {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := reconcileDurableLegacyRecordTemps(directory); err != nil {
		return err
	}
	if existing, _, err := safepath.ReadRegularFile(path); err == nil {
		if bytes.Equal(existing, data) {
			if err := syncDurableLegacyFile(path); err != nil {
				return err
			}
			return syncDurableLegacyDirectory(directory)
		}
		return fmt.Errorf("durable legacy revert record %q differs", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(directory, ".legacy-revert-record-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() {
		_ = file.Close()
		if err := os.Remove(temporaryPath); err == nil {
			_ = syncDurableLegacyDirectory(directory)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := publishDurableLegacyRecordNoReplace(temporaryPath, path); err != nil {
		existing, _, readErr := safepath.ReadRegularFile(path)
		if readErr != nil || !bytes.Equal(existing, data) {
			return errors.Join(err, readErr)
		}
		if syncErr := syncDurableLegacyFile(path); syncErr != nil {
			return errors.Join(err, syncErr)
		}
		if syncErr := syncDurableLegacyDirectory(directory); syncErr != nil {
			return errors.Join(err, syncErr)
		}
		return nil
	}
	return syncDurableLegacyDirectory(directory)
}

func reconcileDurableLegacyRecordTemps(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		if !isDurableLegacyRecordTemp(entry.Name()) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || isLinkOrReparse(info) {
			return fmt.Errorf("durable legacy record temp %q is not a safe regular file", path)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncDurableLegacyDirectory(directory)
	}
	return nil
}

func isDurableLegacyRecordTemp(name string) bool {
	const prefix = ".legacy-revert-record-"
	const suffix = ".tmp"
	if len(name) <= len(prefix)+len(suffix) || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	for _, character := range name[len(prefix) : len(name)-len(suffix)] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func readDurableLegacyPrepared(path string) (durableLegacyRevertPrepared, bool, error) {
	var record durableLegacyRevertPrepared
	found, err := readStrictDurableJSON(path, &record)
	return record, found, err
}

func readDurableLegacyCompletion(path string, index int, entryDigest string) (durableLegacyRevertCompleted, bool, error) {
	var record durableLegacyRevertCompleted
	found, err := readStrictDurableJSON(path, &record)
	if err != nil || !found {
		return record, found, err
	}
	if record.Version != durableLegacyRevertVersion || record.EntryIndex != index || record.EntryDigest != entryDigest ||
		(record.Action != "reverted" && record.Action != "deleted") {
		return durableLegacyRevertCompleted{}, false, fmt.Errorf("legacy revert completion record is invalid")
	}
	return record, true, nil
}

func readStrictDurableJSON(path string, value any) (bool, error) {
	data, _, err := safepath.ReadRegularFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return false, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("trailing JSON value")
		}
		return false, err
	}
	return true, nil
}
