// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunFreshBuiltEngineReviewedMultiFileCaptureContracts(t *testing.T) {
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

	tests := map[string]int{
		"apps.claude-desktop":          3,
		"apps.glasswire":               2,
		"apps.ocenaudio":               3,
		"apps.okular":                  2,
		"apps.xtreme-download-manager": 2,
		"apps.yubikey-manager":         2,
	}
	for moduleID, targetCount := range tests {
		t.Run(moduleID, func(t *testing.T) {
			result, err := Run(context.Background(), Request{
				EnginePath: engine, RepoRoot: repoRoot, ModuleID: moduleID,
				ScenarioID: "reviewed-capture-v1", ResultPath: filepath.Join(t.TempDir(), "result.json"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != ResultStatusPassed || result.Failure != nil {
				t.Fatalf("failure = %+v; counts=%v timings=%v", result.Failure, result.AssertionCounts, result.PhaseTimings)
			}
			for _, name := range []string{"captured", "content", "payload", "provenance"} {
				if result.AssertionCounts[name] != targetCount {
					t.Fatalf("assertion counts = %v, want %s=%d", result.AssertionCounts, name, targetCount)
				}
			}
			if len(result.ProofLevels) != 2 || result.ProofLevels[0] != "catalog" || result.ProofLevels[1] != "engine-contract" {
				t.Fatalf("proof levels = %v", result.ProofLevels)
			}
		})
	}
}
