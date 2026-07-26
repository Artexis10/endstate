// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestInstallJourneyExercisesDryRunThenNegativeAndPositiveVerification(t *testing.T) {
	plan := productionKubectlInstallPlan(t)
	toolRoot := t.TempDir()
	runtime := &scenarioRuntime{
		Module: &modules.Module{ID: plan.ModuleID, Revision: plan.ModuleRevision},
		Scenario: validationmatrix.Scenario{
			ID: plan.ScenarioID, Mode: validationmatrix.ScenarioInstallContract,
			MinimumAssertions: map[string]int{validationmatrix.AssertionAppReferences: 1, validationmatrix.AssertionVerify: 1},
		},
		InstallPlan: plan, ToolRoot: toolRoot,
	}
	executor := &installJourneyFixture{executable: filepath.Join(toolRoot, "kubectl.exe")}
	result := executeInstallJourney(context.Background(), runtime, executor)
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("install journey = %+v", result)
	}
	if !reflect.DeepEqual(executor.calls, []string{"apply-dry-run", "verify-absent", "verify-present"}) {
		t.Fatalf("calls = %v", executor.calls)
	}
	if !reflect.DeepEqual(result.AssertionCounts, map[string]int{validationmatrix.AssertionAppReferences: 1, validationmatrix.AssertionVerify: 1}) {
		t.Fatalf("assertion counts = %v", result.AssertionCounts)
	}
	wantProof := []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract}
	if !reflect.DeepEqual(result.ProofLevels, wantProof) {
		t.Fatalf("proof levels = %v", result.ProofLevels)
	}
}

type installJourneyFixture struct {
	calls      []string
	executable string
}

func (fixture *installJourneyFixture) ApplyDryRun(context.Context, *scenarioRuntime) *Failure {
	fixture.calls = append(fixture.calls, "apply-dry-run")
	if _, err := os.Lstat(fixture.executable); !os.IsNotExist(err) {
		return fail(CodeIsolationFailure, "apply", "toolRoot", "verifier existed before dry-run")
	}
	return nil
}

func (fixture *installJourneyFixture) VerifyAbsent(context.Context, *scenarioRuntime) *Failure {
	fixture.calls = append(fixture.calls, "verify-absent")
	if _, err := os.Lstat(fixture.executable); !os.IsNotExist(err) {
		return fail(CodeIsolationFailure, "verify", "toolRoot", "negative verifier was not absent")
	}
	return nil
}

func (fixture *installJourneyFixture) VerifyPresent(context.Context, *scenarioRuntime) *Failure {
	fixture.calls = append(fixture.calls, "verify-present")
	info, err := os.Lstat(fixture.executable)
	if err != nil || !info.Mode().IsRegular() {
		return fail(CodeIsolationFailure, "verify", "toolRoot", "positive verifier was not materialized")
	}
	return nil
}

func TestCompileSelectionRoutesInstallContractWithoutV2FixtureFallback(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	selected, failure := compileSelection(Request{
		EnginePath: engine, RepoRoot: repo, ModuleID: "apps.kubectl", ScenarioID: "install-v1",
		ResultPath: filepath.Join(t.TempDir(), "result.json"),
	}, time.Now().UTC())
	if failure != nil {
		t.Fatalf("compile selection: %+v", failure)
	}
	if selected.installPlan == nil || selected.installPlan.Inventory.Ref != "Kubernetes.kubectl" {
		t.Fatalf("install plan = %+v", selected.installPlan)
	}
}

func TestCompileInstallContractBindsExactProductionKubectlAuthority(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", ".."))
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.kubectl"]
	record := catalog.Records["apps.kubectl"]
	if mod == nil {
		t.Fatal("production kubectl module is absent")
	}
	if len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("kubectl scenario count = %d, want 1", len(record.Synthetic.Scenarios))
	}

	plan, failure := compileInstallContract(mod, record.Synthetic.Scenarios[0])
	if failure != nil {
		t.Fatalf("compile install contract: %+v", failure)
	}
	if plan.ModuleID != "apps.kubectl" || plan.ModuleRevision != mod.Revision || plan.ScenarioID != "install-v1" {
		t.Fatalf("compiled identity = %+v", plan)
	}
	if plan.Inventory.AppID != "kubernetes-kubectl" || plan.Inventory.Driver != "winget" || plan.Inventory.Ref != "Kubernetes.kubectl" || plan.Inventory.Source != "winget" || plan.Inventory.InitialState != "present" {
		t.Fatalf("compiled inventory = %+v", plan.Inventory)
	}
	if len(plan.Verifiers) != 1 || plan.Verifiers[0].Type != "command-exists" || plan.Verifiers[0].Command != "kubectl" || plan.CommandExecutable != "kubectl.exe" {
		t.Fatalf("compiled verifier authority = %+v executable=%q", plan.Verifiers, plan.CommandExecutable)
	}
}

func TestCompileInstallContractRejectsUnsupportedAndDriftedAuthority(t *testing.T) {
	_, mod, scenario := productionKubectlAuthority(t)
	tests := []struct {
		name      string
		mutable   bool
		mutate    func(*modules.Module, *validationmatrix.Scenario)
		wantCoord string
	}{
		{"mutable catalog identity", true, func(value *modules.Module, _ *validationmatrix.Scenario) { value.ID = "apps.foreign" }, "module"},
		{"exe only", false, func(value *modules.Module, _ *validationmatrix.Scenario) { value.Matches.Winget = nil }, "matches"},
		{"uninstall only", false, func(value *modules.Module, _ *validationmatrix.Scenario) {
			value.Matches.Winget = nil
			value.Matches.Exe = nil
		}, "matches"},
		{"ambiguous package refs", false, func(value *modules.Module, _ *validationmatrix.Scenario) {
			value.Matches.Chocolatey = []string{"kubernetes-cli"}
		}, "matches"},
		{"noncanonical package ref", false, func(value *modules.Module, _ *validationmatrix.Scenario) {
			value.Matches.Winget[0] = " Kubernetes.kubectl "
		}, "matches"},
		{"config restore", false, func(value *modules.Module, _ *validationmatrix.Scenario) {
			value.Restore = []modules.RestoreDef{{Type: "copy"}}
		}, "operations"},
		{"file verifier", false, func(value *modules.Module, _ *validationmatrix.Scenario) {
			value.Verify = []modules.VerifyDef{{Type: "file-exists", Path: `C:\host\kubectl.exe`}}
		}, "verify[0]"},
		{"escaping command verifier", false, func(value *modules.Module, _ *validationmatrix.Scenario) { value.Verify[0].Command = `..\kubectl` }, "verify[0].command"},
		{"duplicate verifier", false, func(value *modules.Module, _ *validationmatrix.Scenario) {
			value.Verify = append(value.Verify, value.Verify[0])
		}, "verify"},
		{"declarative fixture", false, func(_ *modules.Module, value *validationmatrix.Scenario) {
			value.Fixture = validationmatrix.Fixture{Type: validationmatrix.FixtureDeclarative, Path: "fixture.json", SHA256: "deadbeef"}
		}, "fixture.type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate, err := modules.ParseModuleJSON(mod.CanonicalSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			candidateScenario := scenario
			test.mutate(candidate, &candidateScenario)
			if !test.mutable {
				raw, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				candidate, err = modules.ParseModuleJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, failure := compileInstallContract(candidate, candidateScenario); failure == nil || failure.Coordinate != test.wantCoord {
				t.Fatalf("failure = %+v, want coordinate %q", failure, test.wantCoord)
			}
		})
	}
}

func TestInstallLedgerRejectsSkippedVacuousAndInflatedProof(t *testing.T) {
	_, _, scenario := productionKubectlAuthority(t)
	counts := map[string]int{validationmatrix.AssertionAppReferences: 1, validationmatrix.AssertionVerify: 1}
	wantProof := []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract}
	if proof, failure := evaluateAssertions(scenario, counts, OperationCounts{Executed: 1}, wantProof); failure != nil || !reflect.DeepEqual(proof, wantProof) {
		t.Fatalf("exact install ledger proof=%v failure=%+v", proof, failure)
	}
	for _, test := range []struct {
		name   string
		counts map[string]int
		ops    OperationCounts
		proof  []validationmatrix.ProofLevel
	}{
		{"zero verifier", map[string]int{validationmatrix.AssertionAppReferences: 1, validationmatrix.AssertionVerify: 0}, OperationCounts{Executed: 1}, wantProof},
		{"foreign assertion", map[string]int{validationmatrix.AssertionAppReferences: 1, validationmatrix.AssertionVerify: 1, "future": 1}, OperationCounts{Executed: 1}, wantProof},
		{"all skipped", counts, OperationCounts{Skipped: 1}, wantProof},
		{"missing proof", counts, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}},
		{"live install inflation", counts, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofLiveInstall}},
		{"roundtrip inflation", counts, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, failure := evaluateAssertions(scenario, test.counts, test.ops, test.proof); failure == nil {
				t.Fatal("invalid install ledger was accepted")
			}
		})
	}
}

func TestInstallExecutorRejectsHostPATHContamination(t *testing.T) {
	plan := productionKubectlInstallPlan(t)
	runtime := &scenarioRuntime{InstallPlan: plan, ToolRoot: t.TempDir(), OriginalEnvironment: map[string]string{}}
	executor := &cliJourneyExecutor{runtime: runtime, environment: childEnvironment(runtime)}
	for index, value := range executor.environment {
		if strings.HasPrefix(strings.ToUpper(value), "PATH=") {
			executor.environment[index] = value + string(os.PathListSeparator) + `C:\host-tools`
		}
	}
	if failure := executor.assertInstallPATH(); failure == nil || failure.Coordinate != "PATH" {
		t.Fatalf("PATH contamination failure = %+v", failure)
	}
}

func TestInstallManifestProjectsVerifierWithoutClaimingConfigPayloadOwnership(t *testing.T) {
	plan := productionKubectlInstallPlan(t)
	value := installManifestProjection(plan)
	if len(value.ConfigModules) != 0 || len(value.Restore) != 0 {
		t.Fatalf("install manifest claimed config payload ownership: configModules=%v restore=%v", value.ConfigModules, value.Restore)
	}
	if len(value.Verify) != 1 || value.Verify[0].Type != "command-exists" || value.Verify[0].Command != "kubectl" {
		t.Fatalf("install verifier projection = %+v", value.Verify)
	}
}

func productionKubectlInstallPlan(t *testing.T) *InstallContractPlan {
	t.Helper()
	_, mod, scenario := productionKubectlAuthority(t)
	plan, failure := compileInstallContract(mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	return plan
}

func productionKubectlAuthority(t *testing.T) (*validationmatrix.Catalog, *modules.Module, validationmatrix.Scenario) {
	t.Helper()
	repo := filepath.Clean(filepath.Join("..", "..", ".."))
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.kubectl"]
	record := catalog.Records["apps.kubectl"]
	if mod == nil || len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("production kubectl authority is absent: module=%v scenarios=%d", mod != nil, len(record.Synthetic.Scenarios))
	}
	return catalog, mod, record.Synthetic.Scenarios[0]
}
