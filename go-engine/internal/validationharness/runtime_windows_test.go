// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestRunFreshBuiltEngineTrackedNotepadDefaultV1(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(engineRoot)
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(withoutTestEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}

	resultPath := filepath.Join(t.TempDir(), "notepad-default-v1.json")
	result, err := Run(context.Background(), Request{
		EnginePath: engine, RepoRoot: repoRoot, ModuleID: "apps.notepad-plus-plus",
		ScenarioID: "default-v1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("failure = %+v; counts=%v timings=%v", result.Failure, result.AssertionCounts, result.PhaseTimings)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Result
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status != ResultStatusPassed || persisted.ModuleRevision == "" || len(persisted.ProofLevels) != 3 {
		t.Fatalf("persisted result = %+v", persisted)
	}
}

func TestRunFreshBuiltEngineTrackedSchemaV1FileMerges(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(engineRoot)
	catalog, err := validationmatrix.LoadCatalog(repoRoot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(withoutTestEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}
	rows := []string{"apps.beekeeper-studio", "apps.copyq", "apps.core-temp", "apps.crystaldiskinfo", "apps.drawio-desktop", "apps.duckstation", "apps.files", "apps.flameshot", "apps.mkvtoolnix", "apps.nomacs", "apps.pip", "apps.smplayer", "apps.wiztree"}
	targets := map[string]int{}
	total := 0
	for _, moduleID := range rows {
		mod, record := catalog.Modules[moduleID], catalog.Records[moduleID]
		var scenario validationmatrix.Scenario
		for _, candidate := range record.Synthetic.Scenarios {
			if candidate.ID == "default-v1" {
				scenario = candidate
				break
			}
		}
		definitions, failure := compileFixtureDefinitionsAt(repoRoot, mod, scenario)
		if failure != nil {
			t.Fatalf("compile %s: %+v", moduleID, failure)
		}
		targets[moduleID] = len(definitions.Entries)
		total += len(definitions.Entries)
	}
	if total != 32 {
		t.Fatalf("merge target total = %d, want 32", total)
	}
	for _, moduleID := range rows {
		moduleID := moduleID
		t.Run(moduleID, func(t *testing.T) {
			targetCount := targets[moduleID]
			result, err := Run(context.Background(), Request{
				EnginePath: engine, RepoRoot: repoRoot, ModuleID: moduleID, ScenarioID: "default-v1",
				ResultPath: filepath.Join(t.TempDir(), moduleID+".json"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if moduleID == "apps.pip" {
				if result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeArtifactContract || result.Failure.Phase != "capture" || result.Failure.Coordinate != "configs" || result.Failure.Detail != "capture ZIP omits an expected config payload" {
					t.Fatalf("pip result = %+v", result)
				}
				return
			}
			if result.Status != ResultStatusPassed || result.Failure != nil {
				t.Fatalf("failure = %+v; counts=%v", result.Failure, result.AssertionCounts)
			}
			want := map[string]int{"captured": targetCount, "payload": targetCount, "rewrittenRestore": targetCount,
				"content": 3 * targetCount, "provenance": 1, "rebuild": 3, "nestedSummary": 3, "revert": 1, "verify": 2}
			if len(result.AssertionCounts) != len(want) {
				t.Fatalf("counts = %v, want %v", result.AssertionCounts, want)
			}
			for name, count := range want {
				if result.AssertionCounts[name] != count {
					t.Fatalf("counts = %v, want %v", result.AssertionCounts, want)
				}
			}
			wantProof := []string{"catalog", "engine-contract", "config-roundtrip-v1"}
			if len(result.ProofLevels) != len(wantProof) {
				t.Fatalf("proof levels = %v", result.ProofLevels)
			}
			for index, proof := range wantProof {
				if string(result.ProofLevels[index]) != proof {
					t.Fatalf("proof levels = %v", result.ProofLevels)
				}
			}
		})
	}
}

func TestRunFreshBuiltEngineRegistryRoundtrips(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(engineRoot)
	catalog, err := validationmatrix.LoadCatalog(repoRoot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]string, 0, 25)
	validationRoot := filepath.Join(t.TempDir(), "endstate-validation-registry-contract")
	for moduleID, mod := range catalog.Modules {
		if mod.Capture == nil || len(mod.Capture.RegistryKeys) == 0 {
			continue
		}
		var scenario validationmatrix.Scenario
		for _, candidate := range catalog.Records[moduleID].Synthetic.Scenarios {
			if candidate.ID == "default-v1" {
				scenario = candidate
				break
			}
		}
		if scenario.ID == "" {
			continue
		}
		validationContext, restore := reusableFixtureValidationContext(t, validationRoot, mod.ID, scenario.ID)
		_, failure := compileCompositeFixturePlanAt(repoRoot, validationContext, mod, scenario, &recordingRegistryFixture{})
		restore()
		if failure != nil {
			if (moduleID != "apps.ccleaner" && moduleID != "apps.displayfusion" && moduleID != "apps.revo-uninstaller" && moduleID != "apps.tableplus") || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "capture.registryKeys[0].key" {
				t.Fatalf("registry contract %s = %+v", moduleID, failure)
			}
			continue
		}
		rows = append(rows, moduleID)
	}
	sort.Strings(rows)
	if len(rows) != 25 {
		t.Fatalf("safe registry rows = %d, want 25 (%v)", len(rows), rows)
	}
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(withoutTestEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}
	for _, moduleID := range rows {
		result, err := Run(context.Background(), Request{
			EnginePath: engine, RepoRoot: repoRoot, ModuleID: moduleID, ScenarioID: "default-v1",
			ResultPath: filepath.Join(t.TempDir(), moduleID+".json"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != ResultStatusPassed || result.Failure != nil {
			t.Fatalf("registry roundtrip %s = %+v; counts=%v", moduleID, result.Failure, result.AssertionCounts)
		}
	}
}

func TestRunFreshBuiltEngineTrackedKubectlInstallV1(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(engineRoot)
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(withoutTestEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}

	resultPath := filepath.Join(t.TempDir(), "kubectl-install-v1.json")
	result, err := Run(context.Background(), Request{
		EnginePath: engine, RepoRoot: repoRoot, ModuleID: "apps.kubectl",
		ScenarioID: "install-v1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("failure = %+v; counts=%v timings=%v", result.Failure, result.AssertionCounts, result.PhaseTimings)
	}
	if result.AssertionCounts["appReferences"] != 1 || result.AssertionCounts["verify"] != 1 {
		t.Fatalf("assertion counts = %v", result.AssertionCounts)
	}
	want := []string{"catalog", "engine-contract"}
	if len(result.ProofLevels) != len(want) {
		t.Fatalf("proof levels = %v", result.ProofLevels)
	}
	for index, proof := range result.ProofLevels {
		if string(proof) != want[index] {
			t.Fatalf("proof levels = %v", result.ProofLevels)
		}
	}
}

func TestRunFreshBuiltEngineTrackedMGBACaptureV1(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(engineRoot)
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(withoutTestEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}

	resultPath := filepath.Join(t.TempDir(), "mgba-capture-v1.json")
	result, err := Run(context.Background(), Request{
		EnginePath: engine, RepoRoot: repoRoot, ModuleID: "apps.mgba",
		ScenarioID: "reviewed-capture-v1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("failure = %+v; counts=%v timings=%v", result.Failure, result.AssertionCounts, result.PhaseTimings)
	}
	wantCounts := map[string]int{"captured": 1, "content": 1, "payload": 1, "provenance": 1}
	if len(result.AssertionCounts) != len(wantCounts) {
		t.Fatalf("assertion counts = %v", result.AssertionCounts)
	}
	for name, want := range wantCounts {
		if result.AssertionCounts[name] != want {
			t.Fatalf("assertion counts = %v", result.AssertionCounts)
		}
	}
	wantProof := []string{"catalog", "engine-contract"}
	if len(result.ProofLevels) != len(wantProof) {
		t.Fatalf("proof levels = %v", result.ProofLevels)
	}
	for index, proof := range result.ProofLevels {
		if string(proof) != wantProof[index] {
			t.Fatalf("proof levels = %v", result.ProofLevels)
		}
	}
}

func TestRunFreshBuiltEngineTrackedRestoreContractV1(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := materializeRestoreContractRepository(t)
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(withoutTestEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}

	resultPath := filepath.Join(t.TempDir(), "restore-contract-v1.json")
	result, err := Run(context.Background(), Request{
		EnginePath: engine, RepoRoot: repoRoot, ModuleID: restoreContractFixtureModuleID,
		ScenarioID: "reviewed-restore-v1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("failure = %+v; counts=%v timings=%v", result.Failure, result.AssertionCounts, result.PhaseTimings)
	}
	wantCounts := map[string]int{"restored": 1, "content": 1, "nestedSummary": 1, "revert": 1, "verify": 1}
	if len(result.AssertionCounts) != len(wantCounts) {
		t.Fatalf("assertion counts = %v", result.AssertionCounts)
	}
	for name, want := range wantCounts {
		if result.AssertionCounts[name] != want {
			t.Fatalf("assertion counts = %v", result.AssertionCounts)
		}
	}
	if len(result.ProofLevels) != 1 || result.ProofLevels[0] != "engine-contract" {
		t.Fatalf("proof levels = %v", result.ProofLevels)
	}
}

func TestRunFreshBuiltEngineTrackedSchemaV2(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(engineRoot)
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(withoutTestEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}

	tests := []struct{ module, scenario string }{
		{"apps.windows-terminal", "generation-preferences-g1-97631ba2d2e5"},
		{"apps.owncloud", "generation-preferences-g1-1c4479cb88b9"},
		{"apps.owncloud", "generation-preferences-g2-899536c068d4"},
		{"apps.owncloud", "migration-preferences-g1-to-g2"},
		{"apps.studio-one", "generation-preferences-g1-61e9f6f3c254"},
	}
	for _, test := range tests {
		t.Run(test.module+"/"+test.scenario, func(t *testing.T) {
			resultPath := filepath.Join(t.TempDir(), "result.json")
			result, err := Run(context.Background(), Request{
				EnginePath: engine, RepoRoot: repoRoot, ModuleID: test.module,
				ScenarioID: test.scenario, ResultPath: resultPath,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != ResultStatusPassed || result.Failure != nil {
				t.Fatalf("failure = %+v; counts=%v timings=%v", result.Failure, result.AssertionCounts, result.PhaseTimings)
			}
			want := []string{"catalog", "engine-contract", "config-roundtrip-v2"}
			if len(result.ProofLevels) != len(want) {
				t.Fatalf("proof levels = %v", result.ProofLevels)
			}
			for index, proof := range result.ProofLevels {
				if string(proof) != want[index] {
					t.Fatalf("proof levels = %v", result.ProofLevels)
				}
			}
		})
	}
}

func withoutTestEnvironment(values []string, names ...string) []string {
	blocked := map[string]struct{}{}
	for _, name := range names {
		blocked[name] = struct{}{}
	}
	var result []string
	for _, value := range values {
		name := value
		if index := len(name); index > 0 {
			for i, character := range name {
				if character == '=' {
					name = name[:i]
					break
				}
			}
		}
		if _, exists := blocked[name]; !exists {
			result = append(result, value)
		}
	}
	return result
}
