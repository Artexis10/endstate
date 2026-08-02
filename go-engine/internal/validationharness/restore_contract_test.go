// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const restoreContractFixtureModuleID = "apps.validation-restore-contract"

func TestCompileRestoreContractAtBindsTrackedProductionFormFixture(t *testing.T) {
	repo := materializeRestoreContractRepository(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules[restoreContractFixtureModuleID]
	record := catalog.Records[restoreContractFixtureModuleID]
	if mod == nil || len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("tracked restore authority is absent: module=%v scenarios=%d", mod != nil, len(record.Synthetic.Scenarios))
	}

	plan, failure := compileRestoreContractAt(repo, mod, record.Synthetic.Scenarios[0])
	if failure != nil {
		t.Fatalf("compile restore contract: %+v", failure)
	}
	if plan.ModuleID != restoreContractFixtureModuleID || plan.ModuleRevision != mod.Revision || plan.ScenarioID != "reviewed-restore-v1" {
		t.Fatalf("compiled identity = %+v", plan)
	}
	if plan.Inventory.Driver != "winget" || plan.Inventory.Ref != "Endstate.ValidationRestoreContract" || plan.Inventory.InitialState != "present" {
		t.Fatalf("compiled inventory = %+v", plan.Inventory)
	}
	if plan.Restore.Type != "copy" || plan.Restore.Source != "./payload/apps/validation-restore-contract/settings.ini" || plan.Restore.Target != `%APPDATA%\EndstateValidationRestore\settings.ini` || !plan.Restore.Backup {
		t.Fatalf("compiled restore = %+v", plan.Restore)
	}
	if len(plan.Verifiers) != 1 || plan.Verifiers[0].Type != "file-exists" || plan.Verifiers[0].Path != plan.Restore.Target {
		t.Fatalf("compiled verifier projection = %+v", plan.Verifiers)
	}
	if !bytes.Equal(plan.Restored, []byte("[endstate]\nstate=restored\n")) || !bytes.Equal(plan.Original, []byte("[endstate]\nstate=original\n")) {
		t.Fatalf("compiled content: restored=%q original=%q", plan.Restored, plan.Original)
	}
}

func TestTrackedRestoreContractFixtureHashesAreExact(t *testing.T) {
	root := filepath.Join("testdata", "restore-contract")
	moduleBytes := mustReadFile(t, filepath.Join(root, "module.jsonc"))
	revision, err := modules.ComputeModuleRevision(moduleBytes)
	if err != nil {
		t.Fatal(err)
	}
	if revision != "b99f33c6217f437fed61a58adbd0e9c1516c170d79907757b35e1987cceb2332" {
		t.Fatalf("module revision = %s", revision)
	}
	payload := mustReadFile(t, filepath.Join(root, "payload", "settings.ini"))
	if sha256Hex(payload) != "41b3a9d6116cc7df3337f2e0ab065df4397d88804f1788ba9a9093020a0bcdee" || !bytes.Equal(payload, []byte("[endstate]\nstate=restored\n")) {
		t.Fatalf("payload hash/content changed: hash=%s content=%q", sha256Hex(payload), payload)
	}
	fixture := mustReadFile(t, filepath.Join(root, "fixture.jsonc"))
	if sha256Hex(fixture) != "29d90f7c9d898858dad3c488bbe127f2462f8d50e688e01c5c8ad5b5e7f058f5" {
		t.Fatalf("fixture hash = %s", sha256Hex(fixture))
	}
}

func TestCompileSelectionRoutesReviewedRestoreContractExplicitly(t *testing.T) {
	repo := materializeRestoreContractRepository(t)
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	selected, failure := compileSelection(Request{
		EnginePath: engine, RepoRoot: repo, ModuleID: restoreContractFixtureModuleID,
		ScenarioID: "reviewed-restore-v1", ResultPath: filepath.Join(t.TempDir(), "result.json"),
	}, time.Now().UTC())
	if failure != nil {
		t.Fatalf("compile selection: %+v", failure)
	}
	if selected.restorePlan == nil || selected.restorePlan.ModuleID != restoreContractFixtureModuleID {
		t.Fatalf("restore plan = %+v", selected.restorePlan)
	}
}

func TestEvaluateRestoreContractAcceptsOnlyEngineContractProof(t *testing.T) {
	scenario := validationmatrix.Scenario{
		ID: "reviewed-restore-v1", Mode: validationmatrix.ScenarioRestoreContract,
		MinimumAssertions: map[string]int{
			validationmatrix.AssertionRestored: 1, validationmatrix.AssertionContent: 1,
			validationmatrix.AssertionNestedSummary: 1, validationmatrix.AssertionRevert: 1,
			validationmatrix.AssertionVerify: 1,
		},
	}
	counts := map[string]int{
		validationmatrix.AssertionRestored: 1, validationmatrix.AssertionContent: 1,
		validationmatrix.AssertionNestedSummary: 1, validationmatrix.AssertionRevert: 1,
		validationmatrix.AssertionVerify: 1,
	}
	proof, failure := evaluateAssertions(scenario, counts, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract})
	if failure != nil {
		t.Fatalf("evaluate restore contract: %+v", failure)
	}
	if len(proof) != 1 || proof[0] != validationmatrix.ProofEngineContract {
		t.Fatalf("proof = %v", proof)
	}
}

func TestRestoreContractJourneyRestoresAndRevertsExactPreState(t *testing.T) {
	repo := materializeRestoreContractRepository(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules[restoreContractFixtureModuleID]
	scenario := catalog.Records[restoreContractFixtureModuleID].Synthetic.Scenarios[0]
	restorePlan, failure := compileRestoreContractAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	validationContext := fixtureValidationContext(t, mod.ID, scenario.ID)
	resolved, err := validationContext.ResolveHostPath(restorePlan.Restore.Target, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &scenarioRuntime{
		Module: mod, Scenario: scenario, RestorePlan: restorePlan, Root: validationContext.Root(), Inventory: restorePlan.Inventory,
		Plan: &FixturePlan{context: validationContext, Targets: []FixtureTarget{{
			Coordinate: "restore[0]", Authored: restorePlan.Restore.Target,
			Destination: "apps/validation-restore-contract/settings.ini", Resolved: resolved, PayloadPath: resolved,
			Captured: string(restorePlan.Restored), Mutated: string(restorePlan.Original),
		}}},
	}
	executor := &restoreContractJourneyFixture{target: runtime.Plan.Targets[0]}

	result := executeRestoreContractJourney(context.Background(), runtime, executor)
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("restore journey = %+v", result)
	}
	if strings.Join(executor.calls, ",") != "rebuild,revert" {
		t.Fatalf("calls = %v", executor.calls)
	}
	wantCounts := map[string]int{
		validationmatrix.AssertionRestored: 1, validationmatrix.AssertionContent: 1,
		validationmatrix.AssertionNestedSummary: 1, validationmatrix.AssertionRevert: 1,
		validationmatrix.AssertionVerify: 1,
	}
	if len(result.AssertionCounts) != len(wantCounts) {
		t.Fatalf("assertion counts = %v", result.AssertionCounts)
	}
	for name, want := range wantCounts {
		if result.AssertionCounts[name] != want {
			t.Fatalf("assertion counts = %v", result.AssertionCounts)
		}
	}
	if len(result.ProofLevels) != 1 || result.ProofLevels[0] != validationmatrix.ProofEngineContract {
		t.Fatalf("proof levels = %v", result.ProofLevels)
	}
}

type restoreContractJourneyFixture struct {
	calls  []string
	target FixtureTarget
}

func TestPrepareScenarioRuntimeMaterializesBareRestoreArtifact(t *testing.T) {
	repo := materializeRestoreContractRepository(t)
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	selected, selectionFailure := compileSelection(Request{
		EnginePath: engine, RepoRoot: repo, ModuleID: restoreContractFixtureModuleID,
		ScenarioID: "reviewed-restore-v1", ResultPath: filepath.Join(t.TempDir(), "result.json"),
	}, time.Now().UTC())
	if selectionFailure != nil {
		t.Fatal(selectionFailure)
	}
	runtime, cleanup, runtimeFailure, err := prepareScenarioRuntime(selected)
	if err != nil || runtimeFailure != nil {
		t.Fatalf("prepare runtime: err=%v failure=%+v", err, runtimeFailure)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	if runtime.RestorePlan == nil || runtime.Plan == nil || len(runtime.Plan.Targets) != 1 {
		t.Fatalf("restore runtime = %+v", runtime)
	}
	loaded, err := manifest.LoadManifest(runtime.RestorePlan.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Apps) != 1 || loaded.Apps[0].ID != runtime.Inventory.AppID || len(loaded.ConfigModules) != 1 || loaded.ConfigModules[0] != runtime.Module.ID || len(loaded.Restore) != 1 || len(loaded.Verify) != 1 {
		t.Fatalf("materialized manifest = %+v", loaded)
	}
	if loaded.Restore[0].Source != runtime.RestorePlan.Restore.Source || loaded.Restore[0].Target != runtime.RestorePlan.Restore.Target || loaded.Restore[0].FromModule != runtime.Module.ID || !loaded.Restore[0].Backup {
		t.Fatalf("materialized restore = %+v", loaded.Restore)
	}
	entries, artifactFailure := readCaptureArtifactEntries(runtime.RestorePlan.ArtifactPath)
	if artifactFailure != nil {
		t.Fatal(artifactFailure)
	}
	wantPayload := "payload/apps/validation-restore-contract/settings.ini"
	if len(entries) != 2 || !bytes.Equal(entries["manifest.jsonc"], mustReadFile(t, runtime.RestorePlan.ManifestPath)) || !bytes.Equal(entries[wantPayload], runtime.RestorePlan.Restored) {
		t.Fatalf("artifact entries = %v", entries)
	}
}

func TestValidateRestoreContractRebuildEvidenceBindsExactNestedSuccess(t *testing.T) {
	runtime := restoreContractRuntimeForTest(t)
	payload := map[string]any{
		"apply": map[string]any{
			"summary":                 map[string]any{"total": 1, "success": 0, "skipped": 1, "failed": 0},
			"actions":                 []any{map[string]any{"id": runtime.Inventory.AppID, "driver": runtime.Inventory.Driver, "status": "present", "reason": "already_installed"}},
			"configResolutionSummary": map[string]any{"total": 1, "selected": 1, "skipped": 0, "failed": 0},
			"configResolutions":       []any{map[string]any{"status": "restored", "resolution": "legacy_unverified", "reason": nil}},
			"restoreItems": []any{map[string]any{
				"target": runtime.RestorePlan.Restore.Target, "source": runtime.RestorePlan.Restore.Source,
				"restoreType": "", "targetExistedBefore": true, "status": "restored",
				"backupCreated": true, "backupPath": "$ENDSTATE_ROOT/state/backups/rebuild/item",
			}},
		},
		"configResolutionSummary": map[string]any{"total": 1, "selected": 1, "skipped": 0, "failed": 0},
		"configResolutions":       []any{map[string]any{"status": "restored", "resolution": "legacy_unverified", "reason": nil}},
		"restoreItems": []any{map[string]any{
			"target": runtime.RestorePlan.Restore.Target, "source": runtime.RestorePlan.Restore.Source,
			"restoreType": "", "targetExistedBefore": true, "status": "restored",
			"backupCreated": true, "backupPath": "$ENDSTATE_ROOT/state/backups/rebuild/item",
		}},
		"verify": json.RawMessage(validVerifyEvidenceData(runtime)),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if failure := validateRestoreContractRebuildEvidence(raw, runtime); failure != nil {
		t.Fatalf("valid restore evidence: %+v", failure)
	}
	for _, mutation := range []func(map[string]any){
		func(value map[string]any) { value["configResolutionSummary"].(map[string]any)["failed"] = 1 },
		func(value map[string]any) {
			value["apply"].(map[string]any)["configResolutionSummary"].(map[string]any)["failed"] = 1
		},
		func(value map[string]any) { value["restoreItems"].([]any)[0].(map[string]any)["status"] = "failed" },
		func(value map[string]any) {
			value["restoreItems"].([]any)[0].(map[string]any)["source"] = "./payload/apps/foreign/settings.ini"
		},
		func(value map[string]any) { value["verify"].(map[string]any)["summary"].(map[string]any)["fail"] = 1 },
	} {
		var candidate map[string]any
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		mutation(candidate)
		invalid, _ := json.Marshal(candidate)
		if failure := validateRestoreContractRebuildEvidence(invalid, runtime); failure == nil {
			t.Fatal("nested restore failure passed under outer success")
		}
	}
}

func TestValidateRestoreContractRebuildEventsBindsRestoreLifecycleAndVerifier(t *testing.T) {
	runtime := restoreContractRuntimeForTest(t)
	restore := runtime.RestorePlan.Restore
	start := map[string]any{
		"event": "restore-item", "module": runtime.Module.ID, "restorer": restore.Type,
		"source": restore.Source, "target": restore.Target, "status": "restoring", "reason": nil,
		"backupPath": nil, "targetExisted": true,
	}
	complete := map[string]any{}
	for key, value := range start {
		complete[key] = value
	}
	complete["status"] = "restored"
	complete["backupPath"] = "$ENDSTATE_ROOT/state/backups/rebuild/item"
	verifySegment := func(total int) []map[string]any {
		return []map[string]any{
			{"event": "phase", "phase": "verify"},
			{"event": "item", "id": runtime.Inventory.Ref, "driver": runtime.Inventory.Driver},
			{"event": "summary", "phase": "verify", "total": json.Number(strconv.Itoa(total)), "success": json.Number(strconv.Itoa(total)), "skipped": json.Number("0"), "failed": json.Number("0")},
		}
	}
	events := []map[string]any{
		{"event": "phase", "phase": "restore"},
		{"event": "config-resolution", "moduleId": runtime.Module.ID, "resolution": "legacy_unverified", "reason": nil},
		start, complete,
		{"event": "summary", "phase": "restore", "total": json.Number("1"), "success": json.Number("1"), "skipped": json.Number("0"), "failed": json.Number("0")},
	}
	events = append(events, verifySegment(1)...)
	events = append(events, verifySegment(2)...)
	if failure := validateRestoreContractRebuildEvents(events, runtime); failure != nil {
		t.Fatalf("valid restore events: %+v", failure)
	}
	for _, mutation := range []struct {
		index int
		field string
		value any
	}{
		{1, "moduleId", "apps.foreign"},
		{2, "source", "./payload/apps/foreign/settings.ini"},
		{3, "status", "failed"},
		{4, "success", json.Number("0")},
		{10, "failed", json.Number("1")},
	} {
		candidate := make([]map[string]any, len(events))
		for index := range events {
			candidate[index] = map[string]any{}
			for key, value := range events[index] {
				candidate[index][key] = value
			}
		}
		candidate[mutation.index][mutation.field] = mutation.value
		if failure := validateRestoreContractRebuildEvents(candidate, runtime); failure == nil {
			t.Fatalf("mutated restore event %s passed", mutation.field)
		}
	}
}

func TestValidateRestoreContractRevertEvidenceBindsJournalBackupAndEvents(t *testing.T) {
	runtime := restoreContractRuntimeForTest(t)
	target := runtime.RestorePlan.Restore.Target
	journal := "$ENDSTATE_ROOT/logs/restore-journal-rebuild.json"
	backup := "$ENDSTATE_ROOT/state/backups/rebuild/item"
	binding := rebuildEvidenceBinding{
		Journal: journal, BackupsByTarget: map[string]string{strings.ToLower(target): backup},
	}
	raw, _ := json.Marshal(map[string]any{
		"journalUsed": journal,
		"results":     []any{map[string]any{"target": target, "action": "reverted", "backupUsed": backup}},
	})
	events := []map[string]any{
		{"event": "phase", "phase": "restore"},
		{"event": "item", "id": target, "driver": "restore", "status": "installed", "reason": ""},
		{"event": "summary", "phase": "restore", "total": json.Number("1"), "success": json.Number("1"), "skipped": json.Number("0"), "failed": json.Number("0")},
	}
	if failure := validateRestoreContractRevertEvidence(raw, events, runtime, binding); failure != nil {
		t.Fatalf("valid revert evidence: %+v", failure)
	}
	tests := []struct {
		name   string
		raw    []byte
		events []map[string]any
	}{
		{name: "missing journal", raw: []byte(`{"journalUsed":"","results":[]}`), events: events},
		{name: "mismatched backup", raw: []byte(`{"journalUsed":"$ENDSTATE_ROOT/logs/restore-journal-rebuild.json","results":[{"target":"%APPDATA%\\EndstateValidationRestore\\settings.ini","action":"reverted","backupUsed":"$ENDSTATE_ROOT/state/backups/foreign"}]}`), events: events},
		{name: "failed event", raw: raw, events: []map[string]any{events[0], events[1], {"event": "summary", "phase": "restore", "total": json.Number("1"), "success": json.Number("0"), "skipped": json.Number("0"), "failed": json.Number("1")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if failure := validateRestoreContractRevertEvidence(test.raw, test.events, runtime, binding); failure == nil {
				t.Fatal("invalid revert evidence passed")
			}
		})
	}
}

func TestCLIJourneyExecutorImplementsRestoreContractJourney(t *testing.T) {
	var executor restoreContractJourneyExecutor = &cliJourneyExecutor{}
	if executor == nil {
		t.Fatal("restore executor is nil")
	}
}

func TestRestoreContractFixtureStaysOutsideProductionCatalogAndPlanner(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Modules[restoreContractFixtureModuleID] != nil {
		t.Fatal("harness restore fixture entered the production module catalog")
	}
	plan, err := validationmatrix.PlanSynthetic(catalog, validationmatrix.SyntheticPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	restoreRows := 0
	for _, row := range plan.Rows {
		if row.ModuleID == restoreContractFixtureModuleID {
			t.Fatal("harness restore fixture entered the production synthetic planner")
		}
		if row.ScenarioKind == validationmatrix.ScenarioRestoreContract {
			restoreRows++
		}
	}
	if restoreRows != 0 {
		t.Fatalf("production restore-contract rows = %d, want 0", restoreRows)
	}
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	if selected, failure := compileSelection(Request{
		EnginePath: engine, RepoRoot: repo, ModuleID: restoreContractFixtureModuleID,
		ScenarioID: "reviewed-restore-v1", ResultPath: filepath.Join(t.TempDir(), "result.json"),
	}, time.Now().UTC()); failure == nil || selected != nil || failure.Code != CodeScenarioSelection {
		t.Fatalf("production selection admitted harness fixture: selected=%+v failure=%+v", selected, failure)
	}
}

func TestRestoreContractLedgerRejectsVacuityAndProofInflation(t *testing.T) {
	scenario := validationmatrix.Scenario{
		ID: "reviewed-restore-v1", Mode: validationmatrix.ScenarioRestoreContract,
		MinimumAssertions: map[string]int{
			validationmatrix.AssertionRestored: 1, validationmatrix.AssertionContent: 1,
			validationmatrix.AssertionNestedSummary: 1, validationmatrix.AssertionRevert: 1,
			validationmatrix.AssertionVerify: 1,
		},
	}
	counts := map[string]int{
		validationmatrix.AssertionRestored: 1, validationmatrix.AssertionContent: 1,
		validationmatrix.AssertionNestedSummary: 1, validationmatrix.AssertionRevert: 1,
		validationmatrix.AssertionVerify: 1,
	}
	tests := []struct {
		name   string
		counts map[string]int
		ops    OperationCounts
		proof  []validationmatrix.ProofLevel
	}{
		{name: "all skipped", counts: counts, ops: OperationCounts{Skipped: 1}, proof: []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract}},
		{name: "zero assertion", counts: map[string]int{}, ops: OperationCounts{Executed: 1}, proof: []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract}},
		{name: "catalog inflation", counts: counts, ops: OperationCounts{Executed: 1}, proof: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract}},
		{name: "roundtrip inflation", counts: counts, ops: OperationCounts{Executed: 1}, proof: []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV1}},
		{name: "live inflation", counts: counts, ops: OperationCounts{Executed: 1}, proof: []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract, validationmatrix.ProofLiveInstall}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if proof, failure := evaluateAssertions(scenario, test.counts, test.ops, test.proof); failure == nil || len(proof) != 0 {
				t.Fatalf("inflated/vacuous restore result passed: proof=%v failure=%+v", proof, failure)
			}
		})
	}
}

func TestRestoreContractStorageRequiresPhysicalBackupAndValidJournal(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			runtime := restoreContractRuntimeForTest(t)
			if failure := runtime.Plan.Mutate(); failure != nil {
				t.Fatal(failure)
			}
			before, failure := snapshotRebuildStorage(runtime)
			if failure != nil {
				t.Fatal(failure)
			}
			target := runtime.Plan.Targets[0]
			backupPath := filepath.Join(runtime.Root, "state", "backups", "rebuild", "item")
			if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backupPath, runtime.RestorePlan.Original, 0o600); err != nil {
				t.Fatal(err)
			}
			backupSemantic := "$ENDSTATE_ROOT/state/backups/rebuild/item"
			journalPath := filepath.Join(runtime.Root, "logs", "restore-journal-rebuild.json")
			if mode != "missing" {
				if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
					t.Fatal(err)
				}
				journalData, _ := json.Marshal(map[string]any{
					"runId": "rebuild", "timestamp": "2026-07-26T12:00:00Z",
					"manifestPath": "$ENDSTATE_ROOT/manifests/restore-contract/manifest.jsonc",
					"manifestDir":  "$ENDSTATE_ROOT/manifests/restore-contract",
					"entries": []any{map[string]any{
						"resolvedSourcePath": runtime.RestorePlan.Restore.Source, "targetPath": target.Authored,
						"targetExistedBefore": true, "backupRequested": true, "backupCreated": true,
						"backupPath": backupSemantic, "action": "restored", "restoreType": "copy",
					}},
				})
				if mode == "corrupt" {
					journalData = []byte("{")
				}
				if err := os.WriteFile(journalPath, journalData, 0o600); err != nil {
					t.Fatal(err)
				}
				if mode == "valid" {
					guard, err := configrestore.BeginLiveWithBoundary(
						context.Background(), filepath.Join(runtime.Root, "state"), "rebuild", nil,
						v2HostBoundary{runtime.validationContext()},
					)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := guard.RegisterLegacyJournal(journalPath); err != nil {
						_ = guard.Close()
						t.Fatal(err)
					}
					if err := guard.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			raw, _ := json.Marshal(map[string]any{"restoreItems": []any{map[string]any{
				"target": target.Authored, "source": runtime.RestorePlan.Restore.Source,
				"restoreType": "", "targetExistedBefore": true, "status": "restored",
				"backupCreated": true, "backupPath": backupSemantic,
			}}})
			binding, _, storageFailure := validateRebuildStorageEvidence(runtime, raw, 0, before)
			if mode == "valid" {
				if storageFailure != nil || binding.Journal != "$ENDSTATE_ROOT/logs/restore-journal-rebuild.json" || binding.BackupsByTarget[strings.ToLower(target.Authored)] != backupSemantic {
					t.Fatalf("valid storage rejected: binding=%+v failure=%+v", binding, storageFailure)
				}
			} else if storageFailure == nil || storageFailure.Coordinate != "journal" {
				t.Fatalf("%s journal passed: %+v", mode, storageFailure)
			}
		})
	}
}

func restoreContractRuntimeForTest(t *testing.T) *scenarioRuntime {
	t.Helper()
	repo := materializeRestoreContractRepository(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules[restoreContractFixtureModuleID]
	scenario := catalog.Records[restoreContractFixtureModuleID].Synthetic.Scenarios[0]
	restorePlan, failure := compileRestoreContractAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	validationContext := fixtureValidationContext(t, mod.ID, scenario.ID)
	resolved, err := validationContext.ResolveHostPath(restorePlan.Restore.Target, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	restorePlan.context = validationContext
	restorePlan.root = validationContext.Root()
	return &scenarioRuntime{
		Module: mod, Scenario: scenario, RestorePlan: restorePlan, Root: validationContext.Root(), Inventory: restorePlan.Inventory,
		Plan: &FixturePlan{context: validationContext, Targets: []FixtureTarget{{
			Coordinate: "restore[0]", Authored: restorePlan.Restore.Target,
			Destination: "apps/validation-restore-contract/settings.ini", Resolved: resolved, PayloadPath: resolved,
			Captured: string(restorePlan.Restored), Mutated: string(restorePlan.Original),
		}}},
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (fixture *restoreContractJourneyFixture) RebuildRestoreContract(context.Context, *scenarioRuntime) *Failure {
	fixture.calls = append(fixture.calls, "rebuild")
	data, err := os.ReadFile(fixture.target.Resolved)
	if err != nil || !bytes.Equal(data, []byte(fixture.target.Mutated)) {
		return fail(CodeContentMismatch, "rebuild", fixture.target.Coordinate, "pre-restore state is not exact")
	}
	if err := os.WriteFile(fixture.target.Resolved, []byte(fixture.target.Captured), 0o600); err != nil {
		return fail(CodeExecutionFailure, "rebuild", fixture.target.Coordinate, "write restored state")
	}
	return nil
}

func (fixture *restoreContractJourneyFixture) RevertRestoreContract(context.Context, *scenarioRuntime) *Failure {
	fixture.calls = append(fixture.calls, "revert")
	data, err := os.ReadFile(fixture.target.Resolved)
	if err != nil || !bytes.Equal(data, []byte(fixture.target.Captured)) {
		return fail(CodeContentMismatch, "revert", fixture.target.Coordinate, "restored state is not exact")
	}
	if err := os.WriteFile(fixture.target.Resolved, []byte(fixture.target.Mutated), 0o600); err != nil {
		return fail(CodeExecutionFailure, "revert", fixture.target.Coordinate, "write reverted state")
	}
	return nil
}

func TestCompileRestoreContractAtRejectsAmbiguousOrUnsafeAuthority(t *testing.T) {
	tests := []struct {
		name          string
		mutateModule  func(*modules.Module)
		mutateFixture func(string, *validationmatrix.Scenario)
	}{
		{name: "missing restore", mutateModule: func(mod *modules.Module) { mod.Restore = nil }},
		{name: "duplicate restore", mutateModule: func(mod *modules.Module) { mod.Restore = append(mod.Restore, mod.Restore[0]) }},
		{name: "backup disabled", mutateModule: func(mod *modules.Module) { mod.Restore[0].Backup = false }},
		{name: "optional restore", mutateModule: func(mod *modules.Module) { mod.Restore[0].Optional = true }},
		{name: "capture declaration", mutateModule: func(mod *modules.Module) { mod.Capture = &modules.CaptureDef{} }},
		{name: "foreign package family", mutateModule: func(mod *modules.Module) { mod.Matches.Chocolatey = []string{"foreign"} }},
		{name: "foreign restore source", mutateModule: func(mod *modules.Module) { mod.Restore[0].Source = "./payload/apps/foreign/settings.ini" }},
		{name: "traversing restore source", mutateModule: func(mod *modules.Module) {
			mod.Restore[0].Source = "./payload/apps/validation-restore-contract/../foreign.ini"
		}},
		{name: "unsafe target", mutateModule: func(mod *modules.Module) { mod.Restore[0].Target = `C:\host\settings.ini` }},
		{name: "foreign verifier", mutateModule: func(mod *modules.Module) { mod.Verify[0].Path = `%APPDATA%\Foreign\settings.ini` }},
		{name: "missing payload", mutateFixture: func(repo string, _ *validationmatrix.Scenario) {
			if err := os.Remove(filepath.Join(repo, "tests", "fixtures", "validation-restore-contract", "payload", "settings.ini")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong payload content", mutateFixture: func(repo string, _ *validationmatrix.Scenario) {
			if err := os.WriteFile(filepath.Join(repo, "tests", "fixtures", "validation-restore-contract", "payload", "settings.ini"), []byte("foreign\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong descriptor target", mutateFixture: mutateRestoreFixture(t, `%APPDATA%\\Foreign\\settings.ini`, "")},
		{name: "wrong descriptor content", mutateFixture: mutateRestoreFixture(t, "", "foreign\\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := materializeRestoreContractRepository(t)
			catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			mod := catalog.Modules[restoreContractFixtureModuleID]
			scenario := catalog.Records[restoreContractFixtureModuleID].Synthetic.Scenarios[0]
			if test.mutateModule != nil {
				test.mutateModule(mod)
				data, err := json.Marshal(mod)
				if err != nil {
					t.Fatal(err)
				}
				mod, err = modules.ParseModuleJSON(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			if test.mutateFixture != nil {
				test.mutateFixture(repo, &scenario)
			}
			if plan, failure := compileRestoreContractAt(repo, mod, scenario); failure == nil {
				t.Fatalf("unsafe restore contract compiled: %+v", plan)
			}
		})
	}
}

func mutateRestoreFixture(t *testing.T, target, restored string) func(string, *validationmatrix.Scenario) {
	t.Helper()
	return func(repo string, scenario *validationmatrix.Scenario) {
		fixturePath := filepath.Join(repo, "tests", "fixtures", "validation-restore-contract", "fixture.jsonc")
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if target != "" {
			data = []byte(strings.Replace(string(data), `%APPDATA%\\EndstateValidationRestore\\settings.ini`, target, 1))
		}
		if restored != "" {
			data = []byte(strings.Replace(string(data), `[endstate]\nstate=restored\n`, restored, 1))
		}
		if err := os.WriteFile(fixturePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		scenario.Fixture.SHA256 = sha256Hex(data)
	}
}

func materializeRestoreContractRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	source := filepath.Join("testdata", "restore-contract")
	files := map[string]string{
		"module.jsonc":         "modules/apps/validation-restore-contract/module.jsonc",
		"validation.jsonc":     "modules/apps/validation-restore-contract/validation.jsonc",
		"fixture.jsonc":        "tests/fixtures/validation-restore-contract/fixture.jsonc",
		"payload/settings.ini": "tests/fixtures/validation-restore-contract/payload/settings.ini",
	}
	for from, to := range files {
		data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(from)))
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(repo, filepath.FromSlash(to))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}
