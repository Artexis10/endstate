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
	"testing"
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
