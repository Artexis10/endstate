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

type durableLegacyRegistryReplaceStarted struct {
	Version     int                      `json:"version"`
	EntryIndex  int                      `json:"entryIndex"`
	EntryDigest string                   `json:"entryDigest"`
	Target      string                   `json:"target"`
	Desired     durableLegacyRevertState `json:"desired"`
}

// RunRevertDurable reverts legacy filesystem entries with an immutable
// per-entry before-state record. A retry may continue only from the exact
// state recorded before undo or from the verified desired prior state; any
// unrelated edit fails closed without being overwritten.
func RunRevertDurable(journal *Journal, backupDir, workRoot string) ([]RevertResult, error) {
	_ = backupDir
	if journal == nil {
		return nil, fmt.Errorf("restore journal is required")
	}
	if workRoot == "" || !filepath.IsAbs(workRoot) || filepath.Clean(workRoot) != workRoot {
		return nil, fmt.Errorf("legacy revert work root must be a clean absolute path")
	}
	if err := ValidateFilesystemTarget(workRoot); err != nil {
		return nil, fmt.Errorf("validate legacy revert work root: %w", err)
	}
	info, err := os.Lstat(workRoot)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) {
		return nil, fmt.Errorf("legacy revert work root is not a safe directory")
	}
	if err := prepareDurableLegacyRevertEntries(journal, workRoot); err != nil {
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
			result, err := runDurableRegistryRevertEntry(entry, index, workRoot)
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
		result, err := runDurableFilesystemRevertEntry(entry, index, workRoot)
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
func prepareDurableLegacyRevertEntries(journal *Journal, workRoot string) error {
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
			actual, desired, err := durableLegacyRegistryStates(entry, workRoot)
			if err != nil {
				return err
			}
			key := durableLegacyVirtualTarget(entry)
			before, chained := virtual[key]
			if !chained {
				before = actual
			}
			expected := durableLegacyRevertPrepared{
				Version: durableLegacyRevertVersion, EntryIndex: index, EntryDigest: entryDigest,
				Target: entry.TargetPath, Before: before, Desired: desired, DesiredSource: entry.BackupPath,
			}
			if !found {
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

		desired, desiredSource, mutates, err := durableLegacyDesiredState(entry)
		if err != nil {
			return err
		}
		if !mutates {
			continue
		}
		key := durableLegacyVirtualTarget(entry)
		before, chained := virtual[key]
		if !chained {
			before, err = scanDurableLegacyFilesystemState(entry.TargetPath)
			if err != nil {
				return fmt.Errorf("capture revert target %q: %w", entry.TargetPath, err)
			}
		}
		suffix := entryDigest[:16]
		base := filepath.Base(entry.TargetPath)
		parent := filepath.Dir(entry.TargetPath)
		expected := durableLegacyRevertPrepared{
			Version: durableLegacyRevertVersion, EntryIndex: index, EntryDigest: entryDigest,
			Target: filepath.Clean(entry.TargetPath), Before: before, Desired: desired, DesiredSource: desiredSource,
			StagePath: filepath.Join(parent, "."+base+".endstate-revert-"+suffix+"-stage"),
			HeldPath:  filepath.Join(parent, "."+base+".endstate-revert-"+suffix+"-held"),
		}
		if !found {
			for _, path := range []string{expected.StagePath, expected.HeldPath} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					if err == nil {
						err = fmt.Errorf("path already exists")
					}
					return fmt.Errorf("legacy revert scratch path %q is unavailable: %w", path, err)
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

func runDurableRegistryRevertEntry(entry JournalEntry, index int, workRoot string) (RevertResult, error) {
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

	_, desired, err := durableLegacyRegistryStates(entry, workRoot)
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
	if prepared.Version != durableLegacyRevertVersion || prepared.EntryIndex != index ||
		prepared.EntryDigest != entryDigest || prepared.Target != entry.TargetPath || prepared.Desired != desired ||
		prepared.DesiredSource != entry.BackupPath {
		return RevertResult{}, fmt.Errorf("legacy registry revert prepared record differs from journal entry %d", index)
	}

	current, _, err := durableLegacyRegistryStates(entry, workRoot)
	if err != nil {
		return RevertResult{}, err
	}
	if current != prepared.Desired {
		replaceStarted, err := durableLegacyRegistryReplaceInProgress(entry, index, entryDigest, prepared, workRoot)
		if err != nil {
			return RevertResult{}, err
		}
		intermediate := entry.RestoreType == "registry-import" && entry.BackupCreated && entry.BackupPath != "" &&
			current == absentDurableRegistryState("registry-key") && replaceStarted
		if current != prepared.Before && !intermediate {
			return RevertResult{}, fmt.Errorf("legacy registry revert target %q changed after its durable before-state was recorded", entry.TargetPath)
		}
		if err := applyDurableLegacyRegistryRevert(entry, index, entryDigest, prepared, workRoot); err != nil {
			return RevertResult{}, err
		}
		if err := durableRevertCheckpoint("after_target_replaced", index); err != nil {
			return RevertResult{}, err
		}
		current, _, err = durableLegacyRegistryStates(entry, workRoot)
		if err != nil {
			return RevertResult{}, err
		}
		if current != prepared.Desired {
			return RevertResult{}, fmt.Errorf("legacy registry revert target %q does not match its recorded prior state", entry.TargetPath)
		}
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

func runDurableFilesystemRevertEntry(entry JournalEntry, index int, workRoot string) (RevertResult, error) {
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

	desired, desiredSource, mutates, err := durableLegacyDesiredState(entry)
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

	if err := applyDurableLegacyFilesystemRevert(prepared, index); err != nil {
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

func durableLegacyDesiredState(entry JournalEntry) (durableLegacyRevertState, string, bool, error) {
	if entry.BackupCreated && entry.BackupPath != "" {
		if _, err := os.Lstat(entry.BackupPath); err == nil {
			state, err := scanDurableLegacyFilesystemState(entry.BackupPath)
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

func applyDurableLegacyFilesystemRevert(prepared durableLegacyRevertPrepared, index int) error {
	targetState, err := scanDurableLegacyFilesystemState(prepared.Target)
	if err != nil {
		return err
	}
	if targetState == prepared.Desired {
		if stage, exists, err := scanOptionalDurableLegacyState(prepared.StagePath); err != nil {
			return err
		} else if exists {
			if stage != prepared.Desired {
				return fmt.Errorf("legacy revert stage changed after target replacement")
			}
			if err := removeDurableLegacyScratch(prepared.StagePath); err != nil {
				return err
			}
		}
		if held, exists, err := scanOptionalDurableLegacyState(prepared.HeldPath); err != nil {
			return err
		} else if exists {
			if held != prepared.Before {
				return fmt.Errorf("legacy revert held target changed after replacement")
			}
			if err := removeDurableLegacyScratch(prepared.HeldPath); err != nil {
				return err
			}
		}
		return nil
	}
	heldState, heldExists, err := scanOptionalDurableLegacyState(prepared.HeldPath)
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
		if err := ensureDurableLegacyStage(prepared); err != nil {
			return err
		}
	}
	if !heldExists && targetState.Kind != "absent" {
		if err := renameDurableLegacyPath(prepared.Target, prepared.HeldPath); err != nil {
			return err
		}
		if err := durableRevertCheckpoint("after_target_held", index); err != nil {
			return err
		}
		heldExists = true
	}
	if prepared.Desired.Kind != "absent" {
		current, err := scanDurableLegacyFilesystemState(prepared.Target)
		if err != nil {
			return err
		}
		if current.Kind == "absent" {
			if err := renameDurableLegacyPath(prepared.StagePath, prepared.Target); err != nil {
				return err
			}
		}
	}
	if err := durableRevertCheckpoint("after_target_replaced", index); err != nil {
		return err
	}
	actual, err := scanDurableLegacyFilesystemState(prepared.Target)
	if err != nil {
		return err
	}
	if actual != prepared.Desired {
		return fmt.Errorf("legacy revert target %q does not match its recorded prior state", prepared.Target)
	}
	if heldExists {
		if err := removeDurableLegacyScratch(prepared.HeldPath); err != nil {
			return err
		}
	}
	return removeDurableLegacyScratch(prepared.StagePath)
}

func ensureDurableLegacyStage(prepared durableLegacyRevertPrepared) error {
	if state, exists, err := scanOptionalDurableLegacyState(prepared.StagePath); err != nil {
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
	info, err := os.Lstat(prepared.DesiredSource)
	if err != nil {
		return err
	}
	if isLinkOrReparse(info) {
		return fmt.Errorf("legacy revert backup is a link or reparse point")
	}
	if info.IsDir() {
		if err := os.Mkdir(prepared.StagePath, info.Mode().Perm()); err != nil {
			return err
		}
		if err := copyDirRecursive(prepared.DesiredSource, prepared.StagePath, nil); err != nil {
			_ = removeDurableLegacyScratch(prepared.StagePath)
			return err
		}
	} else if info.Mode().IsRegular() {
		if err := atomicRestoreCopy(prepared.DesiredSource, prepared.StagePath); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("legacy revert backup has unsupported type")
	}
	if err := syncDurableLegacyTree(prepared.StagePath); err != nil {
		return err
	}
	if err := syncDurableLegacyDirectory(filepath.Dir(prepared.StagePath)); err != nil {
		return err
	}
	state, err := scanDurableLegacyFilesystemState(prepared.StagePath)
	if err != nil {
		return err
	}
	if state != prepared.Desired {
		return fmt.Errorf("legacy revert stage differs from recorded prior state")
	}
	return nil
}

func renameDurableLegacyPath(source, destination string) error {
	if err := ValidateFilesystemTarget(source); err != nil {
		return err
	}
	if err := ValidateFilesystemTarget(destination); err != nil {
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
	if err := ValidateFilesystemTarget(destination); err != nil {
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

func removeDurableLegacyScratch(path string) error {
	if path == "" {
		return nil
	}
	if err := ValidateFilesystemTarget(path); err != nil {
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
			if err := removeDurableLegacyScratch(filepath.Join(path, entry.Name())); err != nil {
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

func scanOptionalDurableLegacyState(path string) (durableLegacyRevertState, bool, error) {
	state, err := scanDurableLegacyFilesystemState(path)
	if err != nil {
		return durableLegacyRevertState{}, false, err
	}
	return state, state.Kind != "absent", nil
}

func scanDurableLegacyFilesystemState(target string) (durableLegacyRevertState, error) {
	if err := ValidateFilesystemTarget(target); err != nil {
		return durableLegacyRevertState{}, err
	}
	info, err := os.Lstat(target)
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
	err = filepath.Walk(target, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("legacy revert path %q contains a link or reparse point", path)
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
			data, mode, err := safepath.ReadRegularFile(path)
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
	if existing, _, err := safepath.ReadRegularFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("durable legacy revert record %q differs", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return syncDurableLegacyDirectory(filepath.Dir(path))
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
