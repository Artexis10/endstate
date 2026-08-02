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

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestRunCatalogMatrixFreshBuiltEngineProductionBundles(t *testing.T) {
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

	result, err := RunCatalogMatrix(context.Background(), CatalogMatrixRequest{EnginePath: engine, RepoRoot: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusPassed || result.Failure != nil || len(result.ProofLevels) != 1 || result.ProofLevels[0] != validationmatrix.ProofCatalog {
		t.Fatalf("result = %+v", result)
	}
	if result.CatalogCount != 12 || result.Attempted != 12 || result.Passed != 12 || result.Failed != 0 || result.MembershipCount != 315 || result.UniqueModules != 313 {
		t.Fatalf("counts = %+v", result)
	}
	wantReuse := []CatalogReuse{
		{ModuleID: "apps.msi-afterburner", Bundles: []string{"core-utilities", "system-utilities"}},
		{ModuleID: "apps.powertoys", Bundles: []string{"core-utilities", "system-utilities"}},
	}
	if len(result.Reuse) != len(wantReuse) {
		t.Fatalf("reuse = %+v", result.Reuse)
	}
	for index := range wantReuse {
		if result.Reuse[index].ModuleID != wantReuse[index].ModuleID || len(result.Reuse[index].Bundles) != len(wantReuse[index].Bundles) {
			t.Fatalf("reuse = %+v", result.Reuse)
		}
		for bundleIndex := range wantReuse[index].Bundles {
			if result.Reuse[index].Bundles[bundleIndex] != wantReuse[index].Bundles[bundleIndex] {
				t.Fatalf("reuse = %+v", result.Reuse)
			}
		}
	}
	for _, row := range result.Rows {
		if row.Status != ResultStatusPassed || row.Failure != nil || row.PlanExecutions != 2 || row.AssertionCounts["catalogPlan"] != 2 || row.AssertionCounts["actions"] != row.MembershipCount || len(row.ProofLevels) != 1 || row.ProofLevels[0] != validationmatrix.ProofCatalog || row.PhaseTimings["firstPlan"] <= 0 || row.PhaseTimings["secondPlan"] <= 0 {
			t.Fatalf("row = %+v", row)
		}
	}
}

func TestValidateCatalogResultPathRejectsJunctionInRootChain(t *testing.T) {
	resultRoot := filepath.Join(os.TempDir(), "endstate-validation-results")
	outside := t.TempDir()
	if err := os.MkdirAll(resultRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "endstate-validation-results"), 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(resultRoot, filepath.Base(filepath.Dir(outside)))
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Skipf("junction unavailable: %v\n%s", err, output)
	}
	path := filepath.Join(junction, "endstate-validation-results", "result.json")
	if failure := validateCatalogResultPath(path, "", ""); failure == nil {
		t.Fatal("result path with junction in root chain passed")
	}
}
