// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestRunSyntheticShardRejectsInvalidShardBounds(t *testing.T) {
	_, err := RunSyntheticShard(ShardRequest{ShardCount: 8, Shard: 8})
	if err == nil {
		t.Fatal("RunSyntheticShard accepted an out-of-range shard")
	}
}

func TestFinalEvidenceUsesRunnerTempAndHarnessUsesPrivateSystemTempScratch(t *testing.T) {
	runnerTemp := filepath.Join(t.TempDir(), "runner-temp")
	if err := os.Mkdir(runnerTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNNER_TEMP", runnerTemp)
	resultRoot := filepath.Join(runnerTemp, "endstate-validation-results")
	if err := os.Mkdir(resultRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	request := testShardRequest(t, filepath.Join(resultRoot, "shard-0.json"))
	var scratch string
	request.Run = func(_ context.Context, got validationharness.Request) (validationharness.Result, error) {
		if !strictDescendant(os.TempDir(), got.ResultPath) {
			t.Fatalf("harness scratch result %q is not below os.TempDir %q", got.ResultPath, os.TempDir())
		}
		if strictDescendant(runnerTemp, got.ResultPath) {
			t.Fatalf("harness scratch result %q leaked into runner evidence", got.ResultPath)
		}
		scratch = filepath.Dir(got.ResultPath)
		return passedResultForRequest(t, request.RepoRoot, got), nil
	}
	if _, err := RunSyntheticShard(request); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(scratch); !os.IsNotExist(err) {
		t.Fatalf("harness scratch remains after shard: %v", err)
	}
	if err := validateResultPath(filepath.Join(os.TempDir(), "endstate-validation-results", "shard-0.json"), "shard-0.json"); err == nil {
		t.Fatal("accepted final evidence outside RUNNER_TEMP")
	}
}

func TestRunCanaryUsesPrivateSystemTempScratch(t *testing.T) {
	runnerTemp := filepath.Join(t.TempDir(), "runner-temp")
	if err := os.Mkdir(runnerTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNNER_TEMP", runnerTemp)
	resultRoot := filepath.Join(runnerTemp, "endstate-validation-results")
	if err := os.Mkdir(resultRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	shard := testShardRequest(t, filepath.Join(resultRoot, "canary.json"))
	request := CanaryRequest{EnginePath: shard.EnginePath, RepoRoot: shard.RepoRoot, Commit: shard.Commit, ResultPath: shard.ResultPath}
	var scratch string
	request.Run = func(_ context.Context, got validationharness.Request) (validationharness.Result, error) {
		if !strictDescendant(os.TempDir(), got.ResultPath) || strictDescendant(runnerTemp, got.ResultPath) {
			t.Fatalf("canary harness scratch result = %q", got.ResultPath)
		}
		scratch = filepath.Dir(got.ResultPath)
		return passedResultForRequest(t, request.RepoRoot, got), nil
	}
	result, err := RunCanary(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != validationharness.ResultStatusPassed || result.Identity.ModuleID != "apps.notepad-plus-plus" || result.Identity.ScenarioID != "default-v1" {
		t.Fatalf("canary = %+v", result)
	}
	if _, err := os.Lstat(scratch); !os.IsNotExist(err) {
		t.Fatalf("harness scratch remains after canary: %v", err)
	}
}

func TestAggregateRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, input string, state aggregateFixture)
	}{
		{"missing evidence", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			if err := os.Remove(filepath.Join(input, "shard-0.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing canary", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			if err := os.Remove(filepath.Join(input, "canary.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra evidence", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(input, "extra.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"duplicate row", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			shard.Rows = append(shard.Rows, shard.Rows[0])
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"foreign commit", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			shard.Commit = strings.Repeat("a", 40)
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"foreign engine", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			shard.EngineSHA256 = strings.Repeat("a", 64)
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"foreign repository", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			shard.RepositoryHash = strings.Repeat("a", 64)
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"failed shard", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			shard.Status = validationharness.ResultStatusFailed
			shard.Failure = "failed row"
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"wrong proof", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			shard.Rows[0].Result.ProofLevels = []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract}
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"zero assertion", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			for name := range shard.Rows[0].Result.AssertionCounts {
				shard.Rows[0].Result.AssertionCounts[name] = 0
				break
			}
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"below minimum assertion", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			var shard ShardResult
			readJSON(t, filepath.Join(input, "shard-0.json"), &shard)
			for name, minimum := range shard.Rows[0].Identity.Scenario.MinimumAssertions {
				shard.Rows[0].Result.AssertionCounts[name] = minimum - 1
				break
			}
			writeJSON(t, filepath.Join(input, "shard-0.json"), shard)
		}},
		{"foreign canary", func(t *testing.T, input string, state aggregateFixture) {
			t.Helper()
			state.Canary.Commit = strings.Repeat("b", 40)
			writeJSON(t, filepath.Join(input, "canary.json"), state.Canary)
		}},
		{"canary wrong proof", func(t *testing.T, input string, state aggregateFixture) {
			t.Helper()
			state.Canary.Result.ProofLevels = []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract}
			writeJSON(t, filepath.Join(input, "canary.json"), state.Canary)
		}},
		{"canary zero assertion", func(t *testing.T, input string, state aggregateFixture) {
			t.Helper()
			for name := range state.Canary.Result.AssertionCounts {
				state.Canary.Result.AssertionCounts[name] = 0
				break
			}
			writeJSON(t, filepath.Join(input, "canary.json"), state.Canary)
		}},
		{"trailing JSON", func(t *testing.T, input string, _ aggregateFixture) {
			t.Helper()
			path := filepath.Join(input, "catalog.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("{}")...), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAggregateFixture(t)
			tt.mutate(t, fixture.Input, fixture)
			if _, err := Aggregate(fixture.Request); err == nil {
				t.Fatal("Aggregate accepted invalid evidence")
			}
		})
	}
}

func TestAggregateAcceptsCompleteCanonicalEvidence(t *testing.T) {
	fixture := newAggregateFixture(t)
	result, err := Aggregate(fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != validationharness.ResultStatusPassed || result.Scenarios.Passed != result.Scenarios.Eligible || result.Modules.Passed != result.Modules.Eligible || result.Bundles.Passed != result.Bundles.Eligible {
		t.Fatalf("aggregate = %+v", result)
	}
}

type aggregateFixture struct {
	Request AggregateRequest
	Input   string
	Canary  CanaryResult
}

func newAggregateFixture(t *testing.T) aggregateFixture {
	t.Helper()
	runnerTemp := filepath.Join(t.TempDir(), "runner-temp")
	input := filepath.Join(runnerTemp, "validation-input")
	results := filepath.Join(runnerTemp, "endstate-validation-results")
	for _, path := range []string{runnerTemp, input, results} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RUNNER_TEMP", runnerTemp)
	repo := testRepoRoot(t)
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	engineHash, err := fileSHA256(engine)
	if err != nil {
		t.Fatal(err)
	}
	repoHash, err := repositorySHA256(repo)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := validationmatrix.PlanSynthetic(catalog, validationmatrix.SyntheticPlanOptions{ShardCount: ShardCount})
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("c", 40)
	rows := make([][]ShardRow, ShardCount)
	var canary CanaryResult
	for _, row := range plan.Rows {
		result := passedResult(row)
		rows[row.Shard] = append(rows[row.Shard], ShardRow{Identity: row, Result: result})
		if row.ModuleID == "apps.notepad-plus-plus" && row.ScenarioID == "default-v1" {
			canary = CanaryResult{SchemaVersion: SchemaVersion, Commit: commit, EngineSHA256: engineHash, RepositoryHash: repoHash, Identity: row, Result: result, Status: validationharness.ResultStatusPassed}
		}
	}
	if canary.Identity.ModuleID == "" {
		t.Fatal("missing planned canary")
	}
	for shard := range rows {
		writeJSON(t, filepath.Join(input, "shard-"+string(rune('0'+shard))+".json"), ShardResult{SchemaVersion: SchemaVersion, Commit: commit, EngineSHA256: engineHash, RepositoryHash: repoHash, ShardCount: ShardCount, Shard: shard, Status: validationharness.ResultStatusPassed, Rows: rows[shard]})
	}
	bundles, err := filepath.Glob(filepath.Join(repo, "bundles", "*.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(input, "catalog.json"), CatalogResult{SchemaVersion: SchemaVersion, Commit: commit, EngineSHA256: engineHash, RepositoryHash: repoHash, Status: validationharness.ResultStatusPassed, CatalogCount: len(bundles), Passed: len(bundles), Memberships: 1, UniqueModules: 1})
	writeJSON(t, filepath.Join(input, "canary.json"), canary)
	return aggregateFixture{Request: AggregateRequest{EnginePath: engine, RepoRoot: repo, Commit: commit, InputDir: input, ResultPath: filepath.Join(results, "aggregate.json")}, Input: input, Canary: canary}
}

func testShardRequest(t *testing.T, resultPath string) ShardRequest {
	t.Helper()
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	return ShardRequest{EnginePath: engine, RepoRoot: testRepoRoot(t), Commit: strings.Repeat("a", 40), ShardCount: ShardCount, Shard: 0, ResultPath: resultPath}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	source := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	repo := filepath.Join(t.TempDir(), "repo")
	for _, relative := range []string{
		"modules/apps/notepad-plus-plus/module.jsonc",
		"modules/apps/notepad-plus-plus/validation.jsonc",
		"modules/apps/notepad-plus-plus/validation-fixtures/default-v1.jsonc",
		"modules/apps/notepad-plus-plus/seed.ps1",
	} {
		data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(repo, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bundle := filepath.Join(repo, "bundles", "test.jsonc")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func passedResultForRequest(t *testing.T, repo string, request validationharness.Request) validationharness.Result {
	t.Helper()
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := validationmatrix.PlanSynthetic(catalog, validationmatrix.SyntheticPlanOptions{ShardCount: ShardCount})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range plan.Rows {
		if row.ModuleID == request.ModuleID && row.ScenarioID == request.ScenarioID {
			return passedResult(row)
		}
	}
	t.Fatalf("missing planned row %s/%s", request.ModuleID, request.ScenarioID)
	return validationharness.Result{}
}

func passedResult(row validationmatrix.SyntheticRow) validationharness.Result {
	assertions := make(map[string]int, len(row.Scenario.MinimumAssertions))
	for name, count := range row.Scenario.MinimumAssertions {
		assertions[name] = count
	}
	return validationharness.Result{SchemaVersion: validationharness.ResultSchemaVersion, ModuleID: row.ModuleID, ModuleRevision: row.ModuleRevision, ScenarioID: row.ScenarioID, Kind: row.ScenarioKind, Status: validationharness.ResultStatusPassed, ProofLevels: canonicalProofs(row.ScenarioKind), AssertionCounts: assertions, PhaseTimings: map[string]time.Duration{}}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}
func strictDescendant(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
