// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type recordingHostBoundary struct {
	root                string
	resolved            map[string]string
	resolveCalls        []string
	resolvedInstances   []modules.ConfigInstance
	validateCalls       []string
	rejectPath          string
	rejectOnce          bool
	rejectMustBeMissing bool
}

func (boundary *recordingHostBoundary) ResolveHostPath(authored string, instance modules.ConfigInstance) (string, error) {
	boundary.resolveCalls = append(boundary.resolveCalls, authored+"\x00"+instance.ID)
	boundary.resolvedInstances = append(boundary.resolvedInstances, instance)
	resolved, ok := boundary.resolved[authored]
	if !ok {
		return "", fmt.Errorf("unrecognized authored host path %q", authored)
	}
	return resolved, nil
}

func (boundary *recordingHostBoundary) ResolveFilesystemIdentity(identity string) (string, error) {
	if strings.HasPrefix(identity, "$BOUNDARY/") {
		return filepath.Join(boundary.root, filepath.FromSlash(strings.TrimPrefix(identity, "$BOUNDARY/"))), nil
	}
	resolved, ok := boundary.resolved[identity]
	if !ok {
		return "", fmt.Errorf("unrecognized filesystem identity %q", identity)
	}
	return resolved, nil
}

func (boundary *recordingHostBoundary) ProjectFilesystemIdentity(absolute string) (string, error) {
	for semantic, resolved := range boundary.resolved {
		if resolved == absolute {
			return semantic, nil
		}
	}
	relative, err := filepath.Rel(boundary.root, absolute)
	if err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "$BOUNDARY/" + filepath.ToSlash(relative), nil
	}
	return "", fmt.Errorf("unrecognized physical host path %q", absolute)
}

func (boundary *recordingHostBoundary) ValidateFilesystemTarget(absolute string) error {
	boundary.validateCalls = append(boundary.validateCalls, absolute)
	if absolute == boundary.rejectPath {
		if boundary.rejectMustBeMissing {
			if _, err := os.Lstat(absolute); err == nil || !os.IsNotExist(err) {
				return fmt.Errorf("mutation preceded member boundary rejection")
			}
		}
		if boundary.rejectOnce {
			boundary.rejectPath = ""
		}
		return fmt.Errorf("deliberate member boundary rejection")
	}
	relative, err := filepath.Rel(boundary.root, absolute)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escaped disposable authority", absolute)
	}
	return nil
}

func TestValidationSnapshotRevalidatesEveryDirectoryMember(t *testing.T) {
	disposableRoot := t.TempDir()
	stageRoot := filepath.Join(disposableRoot, "stage")
	sourceRoot := filepath.Join(stageRoot, "prefs")
	nestedSource := filepath.Join(sourceRoot, "nested", "settings.json")
	target := filepath.Join(disposableRoot, "sandbox", "prefs")
	transactionRoot := filepath.Join(disposableRoot, "state", "transaction")
	for _, directory := range []string{filepath.Dir(nestedSource), filepath.Dir(target), transactionRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, nestedSource, "desired")
	const authoredTarget = `%APPDATA%\Vendor\prefs`
	boundary := &recordingHostBoundary{
		root: disposableRoot, rejectPath: nestedSource,
		resolved: map[string]string{authoredTarget: target},
	}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "copy", Source: "prefs", Target: authoredTarget,
	}}}
	set, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(filepath.Join(disposableRoot, "unused-instance"), generation), Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: transactionRoot, Boundary: boundary,
	})
	if prepared != nil || err == nil || !strings.Contains(err.Error(), "deliberate member boundary rejection") {
		t.Fatalf("PrepareSnapshots() = (%#v, %v), want nested member rejection", prepared, err)
	}
	found := false
	for _, call := range boundary.validateCalls {
		found = found || call == nestedSource
	}
	if !found {
		t.Fatalf("nested source was not independently revalidated: %#v", boundary.validateCalls)
	}
}

func TestValidationTransactionRevalidatesDirectoryMembersBeforeMutation(t *testing.T) {
	disposableRoot := t.TempDir()
	stageRoot := filepath.Join(disposableRoot, "stage")
	nestedSource := filepath.Join(stageRoot, "prefs", "nested", "settings.json")
	target := filepath.Join(disposableRoot, "sandbox", "prefs")
	nestedTarget := filepath.Join(target, "nested", "settings.json")
	transactionRoot := filepath.Join(disposableRoot, "state", "transaction")
	for _, directory := range []string{filepath.Dir(nestedSource), filepath.Dir(target), transactionRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, nestedSource, "desired")
	const authoredTarget = `%APPDATA%\Vendor\prefs`
	boundary := &recordingHostBoundary{
		root:     disposableRoot,
		resolved: map[string]string{authoredTarget: target},
	}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "copy", Source: "prefs", Target: authoredTarget,
	}}}
	set, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(filepath.Join(disposableRoot, "unused-instance"), generation), Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: transactionRoot, Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatalf("PersistJournalIntent() error = %v", err)
	}
	boundary.rejectPath = nestedTarget
	boundary.rejectOnce = true
	boundary.rejectMustBeMissing = true
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: prepared, Intent: intent, Boundary: boundary,
	})
	if err == nil || result.Status() != TransactionRolledBack || !strings.Contains(err.Error(), "deliberate member boundary rejection") {
		t.Fatalf("ExecuteConfigSetTransaction() = (%#v, %v), want contained rollback", result, err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("rolled-back target still exists: %v", statErr)
	}
}

func TestValidationCrashRecoveryRebuildsSemanticJournalAuthority(t *testing.T) {
	disposableRoot := t.TempDir()
	stageRoot := filepath.Join(disposableRoot, "stage")
	target := filepath.Join(disposableRoot, "sandbox", "settings.txt")
	stateDir := filepath.Join(disposableRoot, "state")
	for _, directory := range []string{stageRoot, filepath.Dir(target), stateDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(stageRoot, "settings.txt"), "desired")
	writeTestFile(t, target, "prior")
	const authoredTarget = `%APPDATA%\Vendor\settings.txt`
	boundary := &recordingHostBoundary{
		root:     disposableRoot,
		resolved: map[string]string{authoredTarget: target},
	}
	guard, err := BeginLiveWithBoundary(context.Background(), stateDir, "validation-crash", nil, boundary)
	if err != nil {
		t.Fatalf("BeginLiveWithBoundary() error = %v", err)
	}
	transactionRoot, err := guard.CreateTransactionRoot("capture-validation")
	if err != nil {
		t.Fatal(err)
	}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "copy", Source: "settings.txt", Target: authoredTarget,
	}}}
	set, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(filepath.Join(disposableRoot, "unused-instance"), generation), Boundary: boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: transactionRoot, Boundary: boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID = "validation-crash"
	lineage.CaptureID = "capture-validation"
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: lineage,
	})
	if err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(intent.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journalBytes), disposableRoot) {
		t.Fatalf("pending journal leaked disposable root: %s", journalBytes)
	}
	writeTestFile(t, target, "desired")
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := BeginLiveWithBoundary(context.Background(), stateDir, "validation-recovery", nil, boundary)
	if err != nil {
		t.Fatalf("recovery BeginLiveWithBoundary() error = %v", err)
	}
	defer recovered.Close()
	assertTestFile(t, target, "prior")

	committedRoot, err := recovered.CreateTransactionRoot("capture-committed")
	if err != nil {
		t.Fatal(err)
	}
	committedPrepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: committedRoot, Boundary: boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	committedLineage := testJournalLineage()
	committedLineage.RunID = "validation-recovery"
	committedLineage.CaptureID = "capture-committed"
	committedIntent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: committedPrepared, TransactionRoot: committedRoot, Lineage: committedLineage,
	})
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: committedPrepared, Intent: committedIntent, Boundary: boundary,
	})
	if err != nil || transaction.Status() != TransactionRestored {
		t.Fatalf("committed transaction = (%#v, %v)", transaction, err)
	}
	assertTestFile(t, target, "desired")
	runs, err := recovered.ActiveStoreRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("active runs = %#v", runs)
	}
	revert, err := recovered.RevertGenerationMember(context.Background(), runs[0].Members()[0])
	if err != nil {
		t.Fatalf("RevertGenerationMember() error = %v", err)
	}
	assertTestFile(t, target, "prior")
	if len(revert.Actions) != 1 || revert.Actions[0].Target != authoredTarget || strings.Contains(revert.Actions[0].Target, disposableRoot) {
		t.Fatalf("semantic generation revert = %#v", revert)
	}
}

func TestValidationProductionOperationMatrixCommitsAndConverges(t *testing.T) {
	disposableRoot := t.TempDir()
	stageRoot := filepath.Join(disposableRoot, "stage")
	transactionRoot := filepath.Join(disposableRoot, "state", "transaction-one")
	secondRoot := filepath.Join(disposableRoot, "state", "transaction-two")
	for _, directory := range []string{stageRoot, transactionRoot, secondRoot, filepath.Join(disposableRoot, "sandbox")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(stageRoot, "copy.txt"), "copied")
	writeTestFile(t, filepath.Join(stageRoot, "merge.json"), `{"portable":true}`)
	writeTestFile(t, filepath.Join(stageRoot, "merge.ini"), "[settings]\nportable=true\n")
	writeTestFile(t, filepath.Join(stageRoot, "append.txt"), "portable\n")

	targets := map[string]string{
		`%APPDATA%\Vendor\copy.txt`:      filepath.Join(disposableRoot, "sandbox", "copy.txt"),
		`%APPDATA%\Vendor\settings.json`: filepath.Join(disposableRoot, "sandbox", "settings.json"),
		`%APPDATA%\Vendor\settings.ini`:  filepath.Join(disposableRoot, "sandbox", "settings.ini"),
		`%APPDATA%\Vendor\lines.txt`:     filepath.Join(disposableRoot, "sandbox", "lines.txt"),
	}
	writeTestFile(t, targets[`%APPDATA%\Vendor\copy.txt`], "prior-copy")
	writeTestFile(t, targets[`%APPDATA%\Vendor\settings.json`], `{"prior":true}`)
	writeTestFile(t, targets[`%APPDATA%\Vendor\settings.ini`], "[settings]\nprior=true\n")
	writeTestFile(t, targets[`%APPDATA%\Vendor\lines.txt`], "prior\n")
	boundary := &recordingHostBoundary{root: disposableRoot, resolved: targets}
	const registryKey = `HKCU\Software\Vendor\Matrix`
	registry := &memoryRegistryMutator{values: map[string]RegistryReadResult{
		registryKey + "\x00Theme": {Exists: true, ValueType: RegistryTypeSZ, Data: []byte{'l', 0, 'i', 0, 'g', 0, 'h', 0, 't', 0, 0, 0}},
	}}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{
		{Type: "copy", Source: "copy.txt", Target: `%APPDATA%\Vendor\copy.txt`},
		{Type: "merge-json", Source: "merge.json", Target: `%APPDATA%\Vendor\settings.json`},
		{Type: "merge-ini", Source: "merge.ini", Target: `%APPDATA%\Vendor\settings.ini`},
		{Type: "append", Source: "append.txt", Target: `%APPDATA%\Vendor\lines.txt`},
		{Type: "registry-set", Key: registryKey, ValueName: "Theme", ValueType: "reg_sz", Data: "dark"},
	}}
	materialize := func() *MaterializedSet {
		set, err := Materialize(context.Background(), Request{
			Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
			Plan:  testPlan(filepath.Join(disposableRoot, "unused-instance"), generation), Boundary: boundary,
		})
		if err != nil {
			t.Fatalf("Materialize() error = %v", err)
		}
		return set
	}
	set := materialize()
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: transactionRoot, RegistryReader: registry, Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(intent.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journalBytes), disposableRoot) {
		t.Fatalf("operation journal leaked disposable root: %s", journalBytes)
	}
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: prepared, Intent: intent, Registry: registry, Boundary: boundary,
	})
	if err != nil || result.Status() != TransactionRestored {
		t.Fatalf("ExecuteConfigSetTransaction() = (%#v, %v)", result, err)
	}
	assertTestFile(t, targets[`%APPDATA%\Vendor\copy.txt`], "copied")
	assertTestFile(t, targets[`%APPDATA%\Vendor\settings.json`], "{\n  \"portable\": true,\n  \"prior\": true\n}\n")
	assertTestFile(t, targets[`%APPDATA%\Vendor\settings.ini`], "[settings]\nportable=true\nprior=true")
	assertTestFile(t, targets[`%APPDATA%\Vendor\lines.txt`], "prior\nportable\n")
	registryValue, err := registry.ReadValue(context.Background(), registryKey, "Theme")
	if err != nil || !registryValue.Exists {
		t.Fatalf("registry desired value = (%#v, %v)", registryValue, err)
	}

	converged, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: materialize(), TransactionRoot: secondRoot, RegistryReader: registry, Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("convergence snapshot error = %v", err)
	}
	for index, action := range converged.Actions() {
		if action.Prior().Kind != action.Desired().Kind || action.Prior().Digest != action.Desired().Digest {
			t.Fatalf("action[%d] did not converge: prior=%#v desired=%#v", index, action.Prior(), action.Desired())
		}
	}
}

func TestValidationSnapshotTransactionJournalAndRollbackStaySemantic(t *testing.T) {
	disposableRoot := t.TempDir()
	stageRoot := filepath.Join(disposableRoot, "stage")
	target := filepath.Join(disposableRoot, "sandbox", "settings.txt")
	transactionRoot := filepath.Join(disposableRoot, "state", "transaction")
	for _, directory := range []string{stageRoot, filepath.Dir(target), transactionRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(stageRoot, "settings.txt"), "desired")
	writeTestFile(t, target, "prior")
	if err := validateConcreteHostPath(target); err != nil {
		t.Fatalf("test target is not concrete: %q: %v", target, err)
	}

	const authoredTarget = `%APPDATA%\Vendor\settings.txt`
	boundary := &recordingHostBoundary{
		root: disposableRoot,
		resolved: map[string]string{
			authoredTarget: target,
		},
	}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "copy", Source: "settings.txt", Target: authoredTarget,
	}}}
	set, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(filepath.Join(disposableRoot, "unused-instance"), generation), Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: transactionRoot, Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	if prepared.boundary == nil {
		t.Fatal("prepared set lost host boundary")
	}
	preparedAction := prepared.Actions()[0]
	probe, err := resolveJournalActionForHostIO(JournalAction{
		Kind: ActionCopy, Target: preparedAction.Action().Target,
		Prior: JournalActionState{BackupPath: preparedAction.Prior().BackupPath},
	}, prepared.boundary)
	if err != nil || probe.Target != target {
		t.Fatalf("journal target probe = (%#v, %v), want %q", probe, err, target)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatalf("PersistJournalIntent() error = %v", err)
	}
	journalBytes, err := os.ReadFile(intent.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journalBytes), disposableRoot) {
		t.Fatalf("journal leaked disposable root: %s", journalBytes)
	}
	actions := intent.Actions()
	if len(actions) != 1 || actions[0].Target != authoredTarget || !strings.HasPrefix(actions[0].Prior.BackupPath, "$BOUNDARY/") {
		t.Fatalf("semantic journal actions = %#v", actions)
	}

	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		if phase == transactionPhaseAfterCommitMutation {
			return errors.New("force rollback after mutation")
		}
		return nil
	}
	result, err := executor.Execute(context.Background(), TransactionRequest{
		Prepared: prepared, Intent: intent, Boundary: boundary,
	})
	if err == nil || result.Status() != TransactionRolledBack {
		t.Fatalf("Execute() = (%#v, %v), want rolled back failure", result, err)
	}
	assertTestFile(t, target, "prior")
	for _, call := range boundary.validateCalls {
		if strings.Contains(call, authoredTarget) {
			t.Fatalf("boundary validated semantic path instead of physical target: %q", call)
		}
	}
}

func TestValidationMaterializeReadsDisposableTargetAndKeepsSemanticIdentity(t *testing.T) {
	disposableRoot := t.TempDir()
	stageRoot := filepath.Join(disposableRoot, "stage")
	originalRoot := t.TempDir()
	writeTestFile(t, filepath.Join(stageRoot, "merge.json"), `{"portable":true}`)
	writeTestFile(t, filepath.Join(disposableRoot, "settings.json"), `{"sandbox":true}`)
	writeTestFile(t, filepath.Join(originalRoot, "settings.json"), `{"original":true}`)

	const authoredTarget = `${instance.root}\settings.json`
	boundary := &recordingHostBoundary{
		root: disposableRoot,
		resolved: map[string]string{
			authoredTarget: filepath.Join(disposableRoot, "settings.json"),
		},
	}
	generation := modules.GenerationDef{
		ID: "g2",
		Restore: []modules.RestoreDef{{
			Type: "merge-json", Source: "merge.json", Target: authoredTarget,
		}},
	}
	plan := testPlan(originalRoot, generation)
	plan.TargetInstances[0].ID = "instance-validation"
	plan.TargetInstances[0].DetectorID = "path-main"
	plan.TargetInstances[0].RawVersion = "2.7.1"
	plan.TargetInstances[0].NormalizedVersion = "2.7.1"
	plan.TargetInstances[0].Evidence.Type = "path"
	plan.TargetInstances[0].Evidence.Platform = "windows"
	plan.TargetInstances[0].Evidence.Ref = "detected-instance"
	plan.TargetInstances[0].Evidence.Driver = "glob"
	plan.Resolution.TargetInstanceID = "instance-validation"

	set, err := Materialize(context.Background(), Request{
		Stage:    &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:     plan,
		Boundary: boundary,
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(set.Actions) != 1 {
		t.Fatalf("actions = %#v, want one", set.Actions)
	}
	action := set.Actions[0]
	if action.Target != authoredTarget || action.Source != "merge.json" {
		t.Fatalf("semantic action = %#v", action)
	}
	if got := string(action.DesiredContent); got != "{\n  \"portable\": true,\n  \"sandbox\": true\n}\n" {
		t.Fatalf("desired content = %q", got)
	}
	if len(boundary.resolveCalls) != 1 || boundary.resolveCalls[0] != authoredTarget+"\x00instance-validation" {
		t.Fatalf("resolve calls = %#v", boundary.resolveCalls)
	}
	if len(boundary.resolvedInstances) != 1 {
		t.Fatalf("resolved instances = %#v", boundary.resolvedInstances)
	}
	resolvedInstance := boundary.resolvedInstances[0]
	if resolvedInstance.ID != "instance-validation" || resolvedInstance.ModuleID != plan.TargetInstances[0].ModuleID ||
		resolvedInstance.DetectorID != "path-main" || resolvedInstance.Root != originalRoot ||
		resolvedInstance.Version.Raw != "2.7.1" || resolvedInstance.Version.Normalized != "2.7.1" ||
		!resolvedInstance.Version.Numeric || resolvedInstance.Evidence.Type != "path" ||
		resolvedInstance.Evidence.Platform != "windows" || resolvedInstance.Evidence.Ref != "detected-instance" ||
		resolvedInstance.Evidence.Driver != "glob" {
		t.Fatalf("exact selected detector evidence was not forwarded: %#v", resolvedInstance)
	}
	if len(boundary.validateCalls) == 0 {
		t.Fatal("materialization did not validate the disposable filesystem target")
	}
	assertTestFile(t, filepath.Join(originalRoot, "settings.json"), `{"original":true}`)
	for _, value := range []string{action.Source, action.Target, string(action.DesiredContent)} {
		if strings.Contains(value, disposableRoot) || strings.Contains(value, originalRoot) {
			t.Fatalf("materialized action leaked physical root in %q", value)
		}
	}
	if _, err := os.Stat(filepath.Join(disposableRoot, "settings.json")); err != nil {
		t.Fatalf("sandbox target changed during materialization: %v", err)
	}
}
