// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/configdoc"
)

func validateAndSyncPreparedSnapshots(
	ctx context.Context,
	writer *JournalWriter,
	transactionRoot string,
	prepared *PreparedSet,
	actions []JournalAction,
) error {
	if err := validateJournalActions(transactionRoot, actions); err != nil {
		return err
	}
	files, directories, err := verifySnapshotArtifacts(ctx, prepared.SnapshotRoot(), actions)
	if err != nil {
		return err
	}
	for _, path := range files {
		if err := writer.runCheckpoint(ctx, journalPhaseBeforeSnapshotFileSync, path); err != nil {
			return err
		}
		if err := syncDurableFile(path); err != nil {
			return err
		}
	}
	sort.Slice(directories, func(left, right int) bool {
		leftDepth := strings.Count(filepath.Clean(directories[left]), string(filepath.Separator))
		rightDepth := strings.Count(filepath.Clean(directories[right]), string(filepath.Separator))
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return directories[left] < directories[right]
	})
	for _, path := range directories {
		if err := writer.runCheckpoint(ctx, journalPhaseBeforeSnapshotDirectorySync, path); err != nil {
			return err
		}
		if err := syncDurableDirectory(path); err != nil {
			return err
		}
	}
	if _, _, err = verifySnapshotArtifacts(ctx, prepared.SnapshotRoot(), actions); err != nil {
		return err
	}
	if err := writer.runCheckpoint(ctx, journalPhaseBeforeTransactionRootSync, transactionRoot); err != nil {
		return err
	}
	return syncDurableDirectory(transactionRoot)
}

func validateJournalActions(transactionRoot string, actions []JournalAction) error {
	if len(actions) == 0 {
		return fmt.Errorf("journal intent requires at least one action")
	}
	snapshotRoot := filepath.Join(transactionRoot, "snapshots")
	missingParentOwners := make(map[string]int)
	for index, action := range actions {
		if action.Index != index || action.Target == "" || action.Strategy == "" ||
			action.Strategy != strings.TrimSpace(action.Strategy) || strings.ContainsRune(action.Strategy, 0) {
			return fmt.Errorf("journal action[%d] has inconsistent identity", index)
		}
		if err := validateJournalActionStrategy(action); err != nil {
			return fmt.Errorf("journal action[%d]: %w", index, err)
		}
		if action.Desired.BackupPath != "" {
			return fmt.Errorf("journal action[%d] desired state has a backup path", index)
		}
		actionRoot := filepath.Join(snapshotRoot, formatActionIndex(index))
		switch action.Kind {
		case ActionCopy, ActionWriteFile, ActionDeleteFile:
			if err := validateJournalMissingParents(transactionRoot, action); err != nil {
				return fmt.Errorf("journal filesystem action[%d] missing parents: %w", index, err)
			}
			for _, parent := range action.MissingParents {
				canonical := canonicalFilesystemTarget(parent)
				if owner, exists := missingParentOwners[canonical]; exists {
					return fmt.Errorf(
						"journal filesystem action[%d] duplicates missing parent owned by action[%d]",
						index,
						owner,
					)
				}
				missingParentOwners[canonical] = index
			}
			if err := validateConcreteHostPath(action.Target); err != nil || containsControl(action.Target) {
				return fmt.Errorf("journal filesystem action[%d] has unsafe target", index)
			}
			if err := validateSnapshotPathSeparation(transactionRoot, action.Target); err != nil {
				return fmt.Errorf("journal filesystem action[%d] target: %w", index, err)
			}
			if action.RegistryKey != "" || action.RegistryValueName != "" {
				return fmt.Errorf("journal filesystem action[%d] has registry identity", index)
			}
			if err := validateJournalFilesystemActionKinds(action); err != nil {
				return fmt.Errorf("journal filesystem action[%d]: %w", index, err)
			}
			if err := validateJournalFilesystemState(action.Prior); err != nil {
				return fmt.Errorf("journal action[%d] prior state: %w", index, err)
			}
			if err := validateJournalFilesystemState(action.Desired); err != nil {
				return fmt.Errorf("journal action[%d] desired state: %w", index, err)
			}
			if action.Prior.Kind == StateAbsent {
				if action.Prior.BackupPath != "" {
					return fmt.Errorf("journal action[%d] absent prior state has a backup", index)
				}
			} else if action.Prior.BackupPath != filepath.Join(actionRoot, "prior") {
				return fmt.Errorf("journal action[%d] backup path differs from deterministic snapshot path", index)
			}
			if action.Kind == ActionCopy {
				if !isLowerHexDigest(action.SourceDigest) {
					return fmt.Errorf("journal action[%d] copy source digest is invalid", index)
				}
			} else if action.SourceDigest != "" {
				return fmt.Errorf("journal action[%d] non-copy source digest is not empty", index)
			}
		case ActionRegistrySet:
			normalizedKey, err := normalizeHKCUKey(action.RegistryKey)
			if err != nil || normalizedKey != action.RegistryKey || action.RegistryValueName == "" ||
				action.RegistryValueName != strings.TrimSpace(action.RegistryValueName) || containsControl(action.RegistryValueName) ||
				action.Target != action.RegistryKey+`\`+action.RegistryValueName {
				return fmt.Errorf("journal registry action[%d] has invalid exact identity", index)
			}
			if len(action.MissingParents) != 0 || action.Prior.BackupPath != filepath.Join(actionRoot, "prior.registry") || action.Desired.BackupPath != "" ||
				len(action.Prior.Entries) != 0 || len(action.Desired.Entries) != 0 || action.SourceDigest != "" ||
				action.Prior.Mode != 0 || action.Desired.Mode != 0 {
				return fmt.Errorf("journal registry action[%d] has inconsistent state", index)
			}
			if action.Prior.Kind != StateAbsent && action.Prior.Kind != StateRegistryValue {
				return fmt.Errorf("journal registry action[%d] has invalid prior kind %q", index, action.Prior.Kind)
			}
			if action.Desired.Kind != StateRegistryValue {
				return fmt.Errorf("journal registry action[%d] desired kind is %q", index, action.Desired.Kind)
			}
			if !isLowerHexDigest(action.Prior.Digest) || !isLowerHexDigest(action.Desired.Digest) {
				return fmt.Errorf("journal registry action[%d] has invalid digest", index)
			}
		default:
			return fmt.Errorf("journal action[%d] has unsupported kind %q", index, action.Kind)
		}
	}
	return nil
}

func validateJournalMissingParents(transactionRoot string, action JournalAction) error {
	if len(action.MissingParents) == 0 {
		return nil
	}
	for index, parent := range action.MissingParents {
		if containsControl(parent) {
			return fmt.Errorf("missing parent contains control characters")
		}
		if err := validateSnapshotPathSeparation(transactionRoot, parent); err != nil {
			return err
		}
		if index > 0 && filepath.Dir(parent) != action.MissingParents[index-1] {
			return fmt.Errorf("missing parent chain is not contiguous")
		}
	}
	if action.MissingParents[len(action.MissingParents)-1] != filepath.Dir(action.Target) {
		return fmt.Errorf("missing parent chain does not end at target parent")
	}
	return nil
}

func validateJournalActionStrategy(action JournalAction) error {
	valid := false
	switch action.Kind {
	case ActionCopy:
		valid = action.Strategy == "copy"
	case ActionWriteFile:
		valid = action.Strategy == "merge-json" || action.Strategy == "merge-ini" || action.Strategy == "append"
	case ActionDeleteFile:
		valid = action.Strategy == "delete-glob"
	case ActionRegistrySet:
		valid = action.Strategy == "registry-set"
	}
	if !valid {
		return fmt.Errorf("kind %q has unsupported strategy %q", action.Kind, action.Strategy)
	}
	return nil
}

func validateJournalFilesystemActionKinds(action JournalAction) error {
	priorFile := action.Prior.Kind == StateAbsent || action.Prior.Kind == StateFile
	switch action.Kind {
	case ActionCopy:
		if !priorFile && action.Prior.Kind != StateDirectory {
			return fmt.Errorf("copy prior kind is %q", action.Prior.Kind)
		}
		if action.Desired.Kind != StateFile && action.Desired.Kind != StateDirectory {
			return fmt.Errorf("copy desired kind is %q", action.Desired.Kind)
		}
	case ActionWriteFile:
		if !priorFile || action.Desired.Kind != StateFile {
			return fmt.Errorf("write-file requires absent/file prior and file desired state")
		}
	case ActionDeleteFile:
		if !priorFile || action.Desired.Kind != StateAbsent {
			return fmt.Errorf("delete-file requires absent/file prior and absent desired state")
		}
	default:
		return fmt.Errorf("unsupported filesystem action kind %q", action.Kind)
	}
	return nil
}

func validateJournalFilesystemState(state JournalActionState) error {
	if state.Mode != uint32(os.FileMode(state.Mode).Perm()) {
		return fmt.Errorf("filesystem state has non-permission mode bits")
	}
	record := StateRecord{
		Kind: state.Kind, Digest: state.Digest, Mode: os.FileMode(state.Mode), BackupPath: state.BackupPath,
		entries: stateEntriesFromJournal(state.Entries),
	}
	filesystem, err := filesystemStateFromRecord(record)
	if err != nil {
		return err
	}
	if filesystem.Digest != state.Digest {
		return fmt.Errorf("filesystem manifest digest mismatch")
	}
	return nil
}

func validateJournalValidations(
	transactionRoot string,
	actions []JournalAction,
	validations []JournalValidation,
) error {
	for index, validation := range validations {
		if err := validateJournalValidationPath(validation.Path); err != nil {
			return fmt.Errorf("journal validation[%d] path: %w", index, err)
		}
		if err := validateConcreteHostPath(validation.HostPath); err != nil || containsControl(validation.HostPath) {
			return fmt.Errorf("journal validation[%d] host path is unsafe", index)
		}
		if err := validateSnapshotPathSeparation(transactionRoot, validation.HostPath); err != nil {
			return fmt.Errorf("journal validation[%d] host path: %w", index, err)
		}
		switch validation.Type {
		case "file-exists", "json-parse", "ini-parse":
			if validation.JSONPath != "" || validation.Section != "" || validation.Key != "" {
				return fmt.Errorf("journal validation[%d] %q has foreign primitive fields", index, validation.Type)
			}
		case "json-path-exists":
			if validation.Section != "" || validation.Key != "" {
				return fmt.Errorf("journal validation[%d] JSON path has INI fields", index)
			}
			if err := configdoc.ValidateJSONPath(validation.JSONPath); err != nil {
				return fmt.Errorf("journal validation[%d] JSON path: %w", index, err)
			}
		case "ini-key-exists":
			if validation.JSONPath != "" {
				return fmt.Errorf("journal validation[%d] INI address has a JSON path", index)
			}
			if err := configdoc.ValidateINIAddress(validation.Section, validation.Key); err != nil {
				return fmt.Errorf("journal validation[%d] INI address: %w", index, err)
			}
		default:
			return fmt.Errorf("journal validation[%d] has unsupported type %q", index, validation.Type)
		}
		mappingCount := 0
		for _, action := range actions {
			if journalValidationMapsToAction(validation, action) {
				mappingCount++
			}
		}
		if mappingCount != 1 {
			return fmt.Errorf(
				"journal validation[%d] must map through exactly one recorded filesystem action, got %d",
				index,
				mappingCount,
			)
		}
	}
	return nil
}

func journalValidationMapsToAction(validation JournalValidation, action JournalAction) bool {
	if action.Kind == ActionDeleteFile || action.Kind == ActionRegistrySet {
		return false
	}
	if action.Desired.Kind == StateFile {
		return validation.HostPath == action.Target
	}
	if action.Kind != ActionCopy || action.Desired.Kind != StateDirectory ||
		!pathContained(action.Target, validation.HostPath) {
		return false
	}
	relative, err := filepath.Rel(action.Target, validation.HostPath)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	portableRelative := filepath.ToSlash(relative)
	if portableRelative == "." {
		return true
	}
	entryExists := false
	for _, entry := range action.Desired.Entries {
		if canonicalPortablePath(entry.Path) == canonicalPortablePath(portableRelative) {
			entryExists = true
			break
		}
	}
	if !entryExists {
		return false
	}
	validationPath := canonicalPortablePath(validation.Path)
	relativePath := canonicalPortablePath(portableRelative)
	return validationPath == relativePath || strings.HasSuffix(validationPath, "/"+relativePath)
}

func validateJournalValidationPath(value string) error {
	if value == "" || value != strings.TrimSpace(value) || path.IsAbs(value) || path.Clean(value) != value ||
		value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, `\`) || containsControl(value) ||
		strings.HasPrefix(value, "~") || strings.HasPrefix(value, "$") || strings.Contains(value, "%") ||
		strings.Contains(value, "${") || (len(value) >= 2 && value[1] == ':') {
		return fmt.Errorf("path must be a clean portable relative path")
	}
	return nil
}

func stateEntriesFromJournal(entries []JournalFilesystemEntry) []StateEntry {
	result := make([]StateEntry, len(entries))
	for index, entry := range entries {
		result[index] = StateEntry{
			Path: entry.Path, Kind: entry.Kind, Mode: os.FileMode(entry.Mode), Size: entry.Size, ContentHash: entry.ContentHash,
		}
	}
	return result
}

func verifySnapshotArtifacts(ctx context.Context, snapshotRoot string, actions []JournalAction) ([]string, []string, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return nil, nil, err
	}
	if err := rejectExistingTargetLinks(snapshotRoot); err != nil {
		return nil, nil, err
	}
	rootEntries, err := os.ReadDir(snapshotRoot)
	if err != nil {
		return nil, nil, err
	}
	if len(rootEntries) != len(actions) {
		return nil, nil, fmt.Errorf("snapshot root contains unexpected action artifacts")
	}
	for index, entry := range rootEntries {
		if entry.Name() != formatActionIndex(index) || !entry.IsDir() {
			return nil, nil, fmt.Errorf("snapshot root action layout is not canonical")
		}
	}
	for index, action := range actions {
		actionRoot := filepath.Join(snapshotRoot, formatActionIndex(index))
		entries, err := os.ReadDir(actionRoot)
		if err != nil {
			return nil, nil, err
		}
		switch action.Kind {
		case ActionRegistrySet:
			if len(entries) != 1 || entries[0].Name() != "prior.registry" || entries[0].IsDir() {
				return nil, nil, fmt.Errorf("registry snapshot action[%d] layout is not canonical", index)
			}
			snapshot, err := loadRegistrySnapshot(action.Prior.BackupPath)
			if err != nil {
				return nil, nil, err
			}
			if snapshot.Key != action.RegistryKey || snapshot.ValueName != action.RegistryValueName ||
				snapshot.Exists != (action.Prior.Kind == StateRegistryValue) ||
				digestRegistrySnapshot(snapshot) != action.Prior.Digest {
				return nil, nil, fmt.Errorf("registry snapshot action[%d] digest mismatch", index)
			}
		default:
			if action.Prior.Kind == StateAbsent {
				if len(entries) != 0 {
					return nil, nil, fmt.Errorf("absent snapshot action[%d] contains backup artifacts", index)
				}
				continue
			}
			if len(entries) != 1 || entries[0].Name() != "prior" {
				return nil, nil, fmt.Errorf("filesystem snapshot action[%d] layout is not canonical", index)
			}
			actual, err := scanFilesystemState(ctx, action.Prior.BackupPath)
			if err != nil {
				return nil, nil, err
			}
			if actual.Digest != action.Prior.Digest || uint32(actual.Mode.Perm()) != action.Prior.Mode {
				return nil, nil, fmt.Errorf("filesystem snapshot action[%d] digest mismatch", index)
			}
		}
	}
	var files, directories []string
	err = filepath.Walk(snapshotRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := checkSnapshotContext(ctx); err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("snapshot tree contains link or reparse point %q", path)
		}
		switch {
		case info.IsDir():
			directories = append(directories, path)
		case info.Mode().IsRegular():
			files = append(files, path)
		default:
			return fmt.Errorf("snapshot tree contains unsupported special file %q", path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	sort.Strings(directories)
	return files, directories, nil
}
