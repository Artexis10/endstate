// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type v2StorageSnapshot struct {
	store        boundaryTree
	storeExisted bool
	logs         boundaryTree
	logsExisted  bool
}

type v2TransactionBinding struct {
	Root, ID, DescriptorDigest, IntentDigest, TerminalDigest string
	Targets                                                  map[string]string
}

func snapshotV2Storage(runtime *scenarioRuntime) (v2StorageSnapshot, *Failure) {
	if runtime == nil || runtime.V2Plan == nil {
		return v2StorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "storage", "schema-v2 storage authority is absent")
	}
	store, storeExisted, err := runtime.snapshotOwnedTree(filepath.Join(runtime.Root, "state", "config-restore"))
	if err != nil {
		return v2StorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "storage", "snapshot config-restore store")
	}
	logs, logsExisted, err := runtime.snapshotOwnedTree(filepath.Join(runtime.Root, "logs"))
	if err != nil {
		return v2StorageSnapshot{}, fail(CodeIsolationFailure, "rebuild", "logs", "snapshot restore logs")
	}
	return v2StorageSnapshot{store: store, storeExisted: storeExisted, logs: logs, logsExisted: logsExisted}, nil
}

func validateV2RebuildStorage(ctx context.Context, runtime *scenarioRuntime, iteration int, before v2StorageSnapshot, evidence v2RebuildEvidence) (v2TransactionBinding, *Failure) {
	after, failure := snapshotV2Storage(runtime)
	if failure != nil {
		return v2TransactionBinding{}, failure
	}
	if iteration == 2 {
		if before.storeExisted != after.storeExisted || before.logsExisted != after.logsExisted ||
			!equalBoundaryTrees(before.store, after.store) || !equalBoundaryTrees(before.logs, after.logs) {
			return v2TransactionBinding{}, fail(CodeGenerationContract, "repeat-rebuild", "storage", "repeat convergence changed transaction, store, journal, or log content")
		}
		return v2TransactionBinding{}, nil
	}
	if before.logsExisted != after.logsExisted || !equalBoundaryTrees(before.logs, after.logs) {
		return v2TransactionBinding{}, fail(CodeGenerationContract, "rebuild", "logs", "generation-only restore unexpectedly created a legacy journal")
	}
	newRoots := newV2TransactionRoots(runtime, before, after)
	if len(newRoots) != 1 {
		return v2TransactionBinding{}, fail(CodeGenerationContract, "rebuild", "transactions", "restoring rebuild must create exactly one transaction")
	}
	root := newRoots[0]
	storeRoot := filepath.Join(runtime.Root, "state", "config-restore")
	relativeRoot, err := filepath.Rel(storeRoot, root)
	if err != nil || filepath.IsAbs(relativeRoot) || relativeRoot == "." || strings.HasPrefix(filepath.ToSlash(relativeRoot), "../") {
		return v2TransactionBinding{}, fail(CodeIsolationFailure, "rebuild", "storage", "transaction subtree escaped config-restore storage")
	}
	relativeRoot = filepath.ToSlash(relativeRoot)
	if before.storeExisted {
		for relative := range before.store {
			if relative == relativeRoot || strings.HasPrefix(relative, relativeRoot+"/") {
				return v2TransactionBinding{}, fail(CodeGenerationContract, "rebuild", "storage", "transaction was mixed into a pre-existing subtree")
			}
		}
	}
	expected := expectedV2TransactionStoreMembers(runtime, relativeRoot)
	allowed := make(map[string]struct{}, len(expected)+4)
	for relative := range expected {
		allowed[relative] = struct{}{}
	}
	for relative, kind := range map[string]string{
		"mutation.lock":         "file",
		"v1/legacy-members":     "directory",
		"v1/legacy-reverts":     "directory",
		"v1/legacy-revert-work": "directory",
	} {
		if entry, exists := after.store[relative]; exists {
			if entry.Kind != kind || kind == "file" && entry.Size != 0 {
				return v2TransactionBinding{}, fail(CodeGenerationContract, "rebuild", "storage", "config-restore store scaffolding differs")
			}
			allowed[relative] = struct{}{}
		}
	}
	for relative, kind := range expected {
		if entry, exists := after.store[relative]; exists && entry.Kind != kind {
			return v2TransactionBinding{}, fail(CodeGenerationContract, "rebuild", "storage", "transaction store member type differs")
		}
	}
	if difference := boundaryAdditionsDifference(before.store, before.storeExisted, after.store, after.storeExisted, allowed); difference != "" {
		return v2TransactionBinding{}, fail(CodeGenerationContract, "rebuild", "storage", "rebuild store delta differs from one new transaction subtree: "+difference)
	}
	binding, failure := validateV2Transaction(ctx, runtime, root, evidence.RestoreItems)
	if failure != nil {
		return v2TransactionBinding{}, failure
	}
	return binding, nil
}

func expectedV2TransactionStoreMembers(runtime *scenarioRuntime, relativeRoot string) map[string]string {
	expected := map[string]string{
		".":                                   "directory",
		"v1":                                  "directory",
		"v1/transactions":                     "directory",
		relativeRoot:                          "directory",
		relativeRoot + "/transaction.json":    "file",
		relativeRoot + "/snapshots":           "directory",
		relativeRoot + "/journal":             "directory",
		relativeRoot + "/journal/intent.json": "file",
	}
	for index, target := range runtime.V2Plan.Targets {
		actionRoot := fmt.Sprintf("%s/snapshots/%06d", relativeRoot, index)
		expected[actionRoot] = "directory"
		prior := actionRoot + "/prior"
		if !target.Directory {
			expected[prior] = "file"
			continue
		}
		expected[prior] = "directory"
		for _, member := range append(append([]V2FixtureFile(nil), target.Members...), v2ExcludedAsFixtureFiles(target.Excluded)...) {
			addExpectedV2StoreFile(expected, prior, member.Relative)
		}
	}
	intentPath := filepath.Join(runtime.Root, "state", "config-restore", filepath.FromSlash(relativeRoot), "journal", "intent.json")
	data, _, err := safepath.ReadRegularFile(intentPath)
	var intentIdentity struct {
		IntentDigest string `json:"intentDigest"`
	}
	if err == nil && json.Unmarshal(data, &intentIdentity) == nil && v2Digest(intentIdentity.IntentDigest) {
		expected[relativeRoot+"/journal/terminal-"+intentIdentity.IntentDigest+".json"] = "file"
	}
	return expected
}

func v2ExcludedAsFixtureFiles(excluded []V2ExcludedFixture) []V2FixtureFile {
	result := make([]V2FixtureFile, len(excluded))
	for index, item := range excluded {
		result[index].Relative = item.Relative
	}
	return result
}

func addExpectedV2StoreFile(expected map[string]string, root, relative string) {
	relative = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	path := root + "/" + relative
	expected[path] = "file"
	for parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path))); parent != root; parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent))) {
		expected[parent] = "directory"
	}
}

func newV2TransactionRoots(runtime *scenarioRuntime, before, after v2StorageSnapshot) []string {
	var result []string
	for relative, entry := range after.store {
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if entry.Kind != "file" || len(parts) != 4 || parts[0] != "v1" || parts[1] != "transactions" || parts[3] != "transaction.json" || !v2OpaqueID(parts[2]) {
			continue
		}
		if before.storeExisted {
			if _, exists := before.store[relative]; exists {
				continue
			}
		}
		result = append(result, filepath.Join(runtime.Root, "state", "config-restore", "v1", "transactions", parts[2]))
	}
	sort.Strings(result)
	return result
}

type v2TransactionDescriptor struct {
	Format           string `json:"format"`
	Version          int    `json:"version"`
	TransactionID    string `json:"transactionId"`
	RestoreRunID     string `json:"restoreRunId"`
	RunID            string `json:"runId"`
	RunStartedAtUTC  string `json:"runStartedAtUtc"`
	MutationOrdinal  uint64 `json:"mutationOrdinal"`
	CaptureID        string `json:"captureId"`
	DescriptorDigest string `json:"descriptorDigest"`
}

type v2TransactionDescriptorIdentity struct {
	Format          string `json:"format"`
	Version         int    `json:"version"`
	TransactionID   string `json:"transactionId"`
	RestoreRunID    string `json:"restoreRunId"`
	RunID           string `json:"runId"`
	RunStartedAtUTC string `json:"runStartedAtUtc"`
	MutationOrdinal uint64 `json:"mutationOrdinal"`
	CaptureID       string `json:"captureId"`
}

func validateV2Transaction(ctx context.Context, runtime *scenarioRuntime, root string, items []restore.RestoreResult) (v2TransactionBinding, *Failure) {
	contractCode := CodeGenerationContract
	if runtime.V2Plan.Compiled.Migration != nil {
		contractCode = CodeMigrationContract
	}
	data, _, err := safepath.ReadRegularFile(filepath.Join(root, "transaction.json"))
	var descriptor v2TransactionDescriptor
	if err != nil || strictV2JSON(data, &descriptor) != nil || descriptor.Format != "endstate.config-restore-transaction" || descriptor.Version != 1 ||
		descriptor.TransactionID != filepath.Base(root) || !v2OpaqueID(descriptor.TransactionID) || !v2OpaqueID(descriptor.RestoreRunID) ||
		!strings.HasPrefix(descriptor.RunID, "apply-") || descriptor.CaptureID != runtime.V2Plan.CaptureID || !v2Digest(descriptor.DescriptorDigest) {
		return v2TransactionBinding{}, fail(contractCode, "rebuild", "transaction.json", "transaction descriptor identity is malformed")
	}
	started, err := time.Parse(time.RFC3339Nano, descriptor.RunStartedAtUTC)
	identity := v2TransactionDescriptorIdentity{descriptor.Format, descriptor.Version, descriptor.TransactionID, descriptor.RestoreRunID, descriptor.RunID, descriptor.RunStartedAtUTC, descriptor.MutationOrdinal, descriptor.CaptureID}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	if err != nil || started.Location() != time.UTC || descriptor.DescriptorDigest != hex.EncodeToString(digest[:]) {
		return v2TransactionBinding{}, fail(contractCode, "rebuild", "transaction.json", "transaction descriptor timestamp or digest differs")
	}
	intent, err := configrestore.ReadJournalIntentWithBoundary(ctx, root, v2HostBoundary{runtime.validationContext()})
	if err != nil || intent == nil || intent.State() != configrestore.JournalPending || !v2Digest(intent.Digest()) {
		return v2TransactionBinding{}, fail(contractCode, "rebuild", "intent", "transaction intent or physical snapshots are invalid")
	}
	lineage := intent.Lineage()
	plan := runtime.V2Plan
	targetInstanceID := plan.Instance.ID
	targetGenerationID := plan.Compiled.Generation.ID
	wantMigrationPath := []string(nil)
	if plan.Compiled.Migration != nil {
		targetInstanceID = plan.TargetInstance.ID
		targetGenerationID = plan.Compiled.TargetGeneration.ID
		wantMigrationPath = []string{plan.Compiled.Generation.ID, plan.Compiled.TargetGeneration.ID}
	}
	if lineage.RunID != descriptor.RunID || lineage.CaptureID != plan.CaptureID || lineage.ModuleID != runtime.Module.ID || lineage.ConfigSetID != plan.Compiled.Set.ID ||
		lineage.TargetInstanceID != targetInstanceID || lineage.SourceGeneration != plan.Compiled.Generation.ID || lineage.TargetGeneration != targetGenerationID ||
		!slices.Equal(lineage.MigrationPath, wantMigrationPath) || lineage.SourceGenerationFingerprint != plan.Compiled.Generation.Fingerprint ||
		lineage.CaptureModuleRevision != runtime.Module.Revision || lineage.RestoreModuleRevision != runtime.Module.Revision {
		return v2TransactionBinding{}, fail(contractCode, "rebuild", "intent.lineage", "transaction lineage differs from exact schema-v2 resolution")
	}
	actions := intent.Actions()
	if len(actions) != len(plan.Targets) || len(items) != len(plan.Targets) || len(intent.Validations()) != plan.Validations {
		return v2TransactionBinding{}, fail(contractCode, "rebuild", "intent", "transaction actions or target-generation validations are incomplete")
	}
	binding := v2TransactionBinding{Root: root, ID: descriptor.TransactionID, DescriptorDigest: descriptor.DescriptorDigest, IntentDigest: intent.Digest(), Targets: map[string]string{}}
	for index, action := range actions {
		target, item := plan.Targets[index], items[index]
		expectedPrior, expectedDesired, expectedSource, stateFailure := expectedV2ActionStates(target, item.BackupPath)
		if stateFailure != nil {
			return v2TransactionBinding{}, stateFailure
		}
		if action.Index != index || action.Kind != configrestore.ActionCopy || action.Strategy != "copy" || !strings.EqualFold(action.Target, item.Target) ||
			action.Prior.BackupPath != item.BackupPath || !v2Digest(action.Prior.Digest) || !v2Digest(action.Desired.Digest) || action.Prior.Digest == action.Desired.Digest ||
			!v2Digest(action.SourceDigest) || action.Prior.Kind != v2TargetStateKind(target) || action.Desired.Kind != v2TargetStateKind(target) ||
			len(action.MissingParents) != 0 || !equalV2JournalState(action.Prior, expectedPrior) || !equalV2JournalState(action.Desired, expectedDesired) ||
			action.SourceDigest != expectedSource.Digest {
			return v2TransactionBinding{}, fail(contractCode, "rebuild", "intent.actions", "transaction action does not bind exact target/prior/desired state")
		}
		backup, err := resolveV2SemanticPath(runtime, item.BackupPath, root)
		if err != nil {
			return v2TransactionBinding{}, fail(CodeIsolationFailure, "rebuild", "prior", "prior snapshot escaped transaction root")
		}
		if failure := validateV2MutatedBackup(runtime, target, backup); failure != nil {
			return v2TransactionBinding{}, failure
		}
		binding.Targets[strings.ToLower(item.Target)] = backup
	}
	targetValidations := plan.Compiled.TargetGeneration.Validate
	if plan.Compiled.Migration == nil && len(targetValidations) == 0 {
		targetValidations = plan.Compiled.Generation.Validate
	}
	for index, validation := range intent.Validations() {
		if index >= len(targetValidations) {
			return v2TransactionBinding{}, fail(contractCode, "rebuild", "intent.validations", "transaction has an extra target-generation validation")
		}
		declaration := targetValidations[index]
		hostPath, hostErr := (v2HostBoundary{runtime.validationContext()}).ResolveFilesystemIdentity(validation.HostPath)
		wantHost, found := v2ValidationHostPath(plan, declaration.Path)
		if validation.Type != declaration.Type || validation.Path != declaration.Path || validation.JSONPath != declaration.JSONPath || validation.Section != declaration.Section || validation.Key != declaration.Key || hostErr != nil || !found || !strings.EqualFold(filepath.Clean(hostPath), filepath.Clean(wantHost)) {
			return v2TransactionBinding{}, fail(contractCode, "rebuild", "intent.validations", "target-generation validation does not bind the production primitive and resolved target")
		}
	}
	terminalDigest, failure := validateV2CommittedMarker(root, intent.Digest())
	if failure != nil {
		return v2TransactionBinding{}, failure
	}
	binding.TerminalDigest = terminalDigest
	return binding, nil
}

func expectedV2ActionStates(target V2FixtureTarget, backupPath string) (configrestore.JournalActionState, configrestore.JournalActionState, configrestore.JournalActionState, *Failure) {
	prior, failure := expectedV2FixtureState(target, true, true)
	if failure != nil {
		return configrestore.JournalActionState{}, configrestore.JournalActionState{}, configrestore.JournalActionState{}, failure
	}
	prior.BackupPath = backupPath
	desired, failure := expectedV2FixtureState(target, false, true)
	if failure != nil {
		return configrestore.JournalActionState{}, configrestore.JournalActionState{}, configrestore.JournalActionState{}, failure
	}
	source, failure := expectedV2FixtureState(target, false, false)
	return prior, desired, source, failure
}

func expectedV2FixtureState(target V2FixtureTarget, mutated, includeExcluded bool) (configrestore.JournalActionState, *Failure) {
	entries := map[string]configrestore.JournalFilesystemEntry{}
	addFile := func(relative, path string, content []byte) *Failure {
		info, err := os.Lstat(path)
		if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
			return fail(CodeIsolationFailure, "rebuild", target.Coordinate, "fixture action state member is unavailable or unsafe")
		}
		digest := sha256.Sum256(content)
		entries[filepath.ToSlash(relative)] = configrestore.JournalFilesystemEntry{
			Path: filepath.ToSlash(relative), Kind: configrestore.StateFile, Mode: uint32(info.Mode().Perm()),
			Size: int64(len(content)), ContentHash: hex.EncodeToString(digest[:]),
		}
		return nil
	}
	addParents := func(relative string) *Failure {
		for parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(relative))); parent != "."; parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent))) {
			if _, exists := entries[parent]; exists {
				continue
			}
			info, err := os.Lstat(filepath.Join(target.Resolved, filepath.FromSlash(parent)))
			if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
				return fail(CodeIsolationFailure, "rebuild", target.Coordinate, "fixture action state parent is unavailable or unsafe")
			}
			entries[parent] = configrestore.JournalFilesystemEntry{Path: parent, Kind: configrestore.StateDirectory, Mode: uint32(info.Mode().Perm())}
		}
		return nil
	}
	rootInfo, err := os.Lstat(target.Resolved)
	if err != nil || safepath.IsLinkOrReparse(rootInfo) || rootInfo.IsDir() != target.Directory {
		return configrestore.JournalActionState{}, fail(CodeIsolationFailure, "rebuild", target.Coordinate, "fixture action state root is unavailable or unsafe")
	}
	if !target.Directory {
		if len(target.Members) != 1 {
			return configrestore.JournalActionState{}, fail(CodeUnsupportedFixture, "rebuild", target.Coordinate, "single-file fixture action does not have one exact member")
		}
		content := target.Members[0].Captured
		if mutated {
			content = target.Members[0].Mutated
		}
		if failure := addFile(".", target.Resolved, content); failure != nil {
			return configrestore.JournalActionState{}, failure
		}
	} else {
		entries["."] = configrestore.JournalFilesystemEntry{Path: ".", Kind: configrestore.StateDirectory, Mode: uint32(rootInfo.Mode().Perm())}
		for _, member := range target.Members {
			content := member.Captured
			if mutated {
				content = member.Mutated
			}
			if failure := addParents(member.Relative); failure != nil {
				return configrestore.JournalActionState{}, failure
			}
			if failure := addFile(member.Relative, member.Path, content); failure != nil {
				return configrestore.JournalActionState{}, failure
			}
		}
		for _, excluded := range target.Excluded {
			if !includeExcluded && len(excluded.CapturePatterns) != 0 {
				continue
			}
			content := excluded.Captured
			if includeExcluded {
				content = excluded.Mutated
			}
			if failure := addParents(excluded.Relative); failure != nil {
				return configrestore.JournalActionState{}, failure
			}
			if failure := addFile(excluded.Relative, excluded.Path, content); failure != nil {
				return configrestore.JournalActionState{}, failure
			}
		}
	}
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	ordered := make([]configrestore.JournalFilesystemEntry, 0, len(paths))
	for _, path := range paths {
		ordered = append(ordered, entries[path])
	}
	state := configrestore.JournalActionState{Kind: v2TargetStateKind(target), Mode: uint32(rootInfo.Mode().Perm()), Entries: ordered}
	state.Digest = digestV2JournalState(state)
	return state, nil
}

func digestV2JournalState(state configrestore.JournalActionState) string {
	hasher := sha256.New()
	writeUint := func(value uint64) {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], value)
		_, _ = hasher.Write(encoded[:])
	}
	writeString := func(value string) {
		writeUint(uint64(len(value)))
		_, _ = hasher.Write([]byte(value))
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

func equalV2JournalState(left, right configrestore.JournalActionState) bool {
	if left.Kind != right.Kind || left.Digest != right.Digest || left.Mode != right.Mode || left.BackupPath != right.BackupPath || len(left.Entries) != len(right.Entries) {
		return false
	}
	for index := range left.Entries {
		if left.Entries[index] != right.Entries[index] {
			return false
		}
	}
	return true
}

func v2ValidationHostPath(plan *V2FixturePlan, portable string) (string, bool) {
	portable = filepath.ToSlash(portable)
	for _, target := range plan.Targets {
		destination := strings.TrimSuffix(filepath.ToSlash(target.Destination), "/")
		if portable == destination {
			return target.Resolved, true
		}
		if strings.HasPrefix(portable, destination+"/") {
			return filepath.Join(target.Resolved, filepath.FromSlash(strings.TrimPrefix(portable, destination+"/"))), true
		}
	}
	return "", false
}

func validateV2CommittedMarker(root, intentDigest string) (string, *Failure) {
	path := filepath.Join(root, "journal", "terminal-"+intentDigest+".json")
	data, _, err := safepath.ReadRegularFile(path)
	type marker struct {
		Format           string                         `json:"format"`
		Version          int                            `json:"version"`
		IntentDigest     string                         `json:"intentDigest"`
		State            configrestore.JournalState     `json:"state"`
		ValidationStatus configrestore.ValidationStatus `json:"validationStatus"`
		RollbackOutcome  configrestore.RollbackOutcome  `json:"rollbackOutcome"`
		MarkerDigest     string                         `json:"markerDigest"`
	}
	type identity struct {
		Format           string                         `json:"format"`
		Version          int                            `json:"version"`
		IntentDigest     string                         `json:"intentDigest"`
		State            configrestore.JournalState     `json:"state"`
		ValidationStatus configrestore.ValidationStatus `json:"validationStatus"`
		RollbackOutcome  configrestore.RollbackOutcome  `json:"rollbackOutcome"`
	}
	var disk marker
	if err != nil || strictV2JSON(data, &disk) != nil || disk.Format != "endstate.config-restore-marker" || disk.Version != 1 || disk.IntentDigest != intentDigest || disk.State != configrestore.JournalCommitted || disk.ValidationStatus != configrestore.ValidationPassed || disk.RollbackOutcome != configrestore.RollbackNotRequired {
		return "", fail(CodeGenerationContract, "rebuild", "committed", "committed transaction marker differs")
	}
	encoded, _ := json.Marshal(identity{disk.Format, disk.Version, disk.IntentDigest, disk.State, disk.ValidationStatus, disk.RollbackOutcome})
	digest := sha256.Sum256(encoded)
	if disk.MarkerDigest != hex.EncodeToString(digest[:]) {
		return "", fail(CodeGenerationContract, "rebuild", "committed", "committed marker digest differs")
	}
	return disk.MarkerDigest, nil
}

func validateV2MutatedBackup(runtime *scenarioRuntime, target V2FixtureTarget, backup string) *Failure {
	info, err := os.Lstat(backup)
	if err != nil || safepath.IsLinkOrReparse(info) || info.IsDir() != target.Directory {
		return fail(CodeContentMismatch, "rebuild", target.Coordinate, "physical prior snapshot type differs")
	}
	expected := map[string]expectedFixtureEntry{".": {Directory: target.Directory}}
	if !target.Directory {
		expected["."] = expectedFixtureEntry{Content: string(target.Members[0].Mutated)}
	}
	for _, member := range target.Members {
		if target.Directory {
			addV2Expected(expected, member.Relative, member.Mutated)
		}
	}
	for _, excluded := range target.Excluded {
		addV2Expected(expected, excluded.Relative, excluded.Mutated)
	}
	seen := map[string]struct{}{}
	err = filepath.Walk(backup, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || safepath.IsLinkOrReparse(info) {
			return fmt.Errorf("unsafe prior")
		}
		relative, relErr := filepath.Rel(backup, path)
		if relErr != nil {
			return relErr
		}
		want, ok := expected[relative]
		if !ok || want.Directory != info.IsDir() {
			return fmt.Errorf("prior tree differs")
		}
		if !info.IsDir() {
			content, _, readErr := safepath.ReadRegularFile(path)
			if readErr != nil || string(content) != want.Content {
				return fmt.Errorf("prior bytes differ")
			}
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil || len(seen) != len(expected) {
		return fail(CodeContentMismatch, "rebuild", target.Coordinate, "physical prior snapshot differs from exact mutated fixture")
	}
	return nil
}

func addV2Expected(expected map[string]expectedFixtureEntry, relative string, content []byte) {
	relative = filepath.Clean(filepath.FromSlash(relative))
	expected[relative] = expectedFixtureEntry{Content: string(content)}
	for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
		expected[parent] = expectedFixtureEntry{Directory: true}
	}
}

func validateV2RevertStorage(runtime *scenarioRuntime, before v2StorageSnapshot, binding v2TransactionBinding) *Failure {
	after, failure := snapshotV2Storage(runtime)
	if failure != nil {
		return failure
	}
	if before.logsExisted != after.logsExisted || !equalBoundaryTrees(before.logs, after.logs) {
		return fail(CodeRevertFailure, "revert", "logs", "generation revert changed legacy journal storage")
	}
	relative, _ := filepath.Rel(filepath.Join(runtime.Root, "state", "config-restore"), filepath.Join(binding.Root, "reverted.json"))
	relative = filepath.ToSlash(relative)
	allowed := map[string]struct{}{relative: {}}
	if difference := boundaryAdditionsDifference(before.store, before.storeExisted, after.store, after.storeExisted, allowed); difference != "" {
		return fail(CodeRevertFailure, "revert", "storage", "revert store delta differs from one durable member marker: "+difference)
	}
	data, _, err := safepath.ReadRegularFile(filepath.Join(binding.Root, "reverted.json"))
	type disk struct {
		Format       string                        `json:"format"`
		Version      int                           `json:"version"`
		Kind         configrestore.StoreMemberKind `json:"kind"`
		MemberID     string                        `json:"memberId"`
		SourceDigest string                        `json:"sourceDigest"`
		RevertDigest string                        `json:"revertDigest"`
	}
	type identity struct {
		Format       string                        `json:"format"`
		Version      int                           `json:"version"`
		Kind         configrestore.StoreMemberKind `json:"kind"`
		MemberID     string                        `json:"memberId"`
		SourceDigest string                        `json:"sourceDigest"`
	}
	var marker disk
	if err != nil || strictV2JSON(data, &marker) != nil || marker.Format != "endstate.config-restore-member-revert" || marker.Version != 1 || marker.Kind != configrestore.StoreMemberGeneration || marker.MemberID != binding.ID || marker.SourceDigest != binding.TerminalDigest {
		return fail(CodeRevertFailure, "revert", "reverted.json", "generation member revert marker differs")
	}
	encoded, _ := json.Marshal(identity{marker.Format, marker.Version, marker.Kind, marker.MemberID, marker.SourceDigest})
	digest := sha256.Sum256(encoded)
	if marker.RevertDigest != hex.EncodeToString(digest[:]) {
		return fail(CodeRevertFailure, "revert", "reverted.json", "generation revert digest differs")
	}
	return nil
}

func resolveV2SemanticPath(runtime *scenarioRuntime, semantic, scope string) (string, error) {
	const prefix = "$ENDSTATE_ROOT/"
	semantic = filepath.ToSlash(semantic)
	if !strings.HasPrefix(semantic, prefix) {
		return "", fmt.Errorf("bad semantic")
	}
	physical := filepath.Join(runtime.Root, filepath.FromSlash(strings.TrimPrefix(semantic, prefix)))
	if runtime.validationContext().ValidateSandboxPath(physical) != nil || !fixtureContained(scope, physical) {
		return "", fmt.Errorf("escaped semantic")
	}
	return physical, nil
}

func v2TargetStateKind(target V2FixtureTarget) configrestore.StateKind {
	if target.Directory {
		return configrestore.StateDirectory
	}
	return configrestore.StateFile
}
func v2OpaqueID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func v2Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}
func strictV2JSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

type v2HostBoundary struct{ context *validationmode.Context }

func (boundary v2HostBoundary) ResolveHostPath(authored string, instance modules.ConfigInstance) (string, error) {
	return boundary.context.ResolveHostPath(authored, validationmode.HostPathPolicy{InstanceRoot: instance.Root, AllowRoot: strings.EqualFold(authored, "${instance.root}")})
}
func (boundary v2HostBoundary) ResolveFilesystemIdentity(identity string) (string, error) {
	const token = "$ENDSTATE_ROOT/"
	if strings.HasPrefix(filepath.ToSlash(identity), token) {
		return validationmode.ResolvePortablePath(boundary.context.Root(), strings.TrimPrefix(filepath.ToSlash(identity), token))
	}
	return boundary.context.ResolveHostPath(identity, validationmode.HostPathPolicy{AllowRoot: true})
}
func (boundary v2HostBoundary) ProjectFilesystemIdentity(absolute string) (string, error) {
	return boundary.context.DisplayPath(filepath.Clean(absolute))
}
func (boundary v2HostBoundary) ValidateFilesystemTarget(absolute string) error {
	return boundary.context.ValidateSandboxPath(filepath.Clean(absolute))
}
