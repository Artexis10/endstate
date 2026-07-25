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
