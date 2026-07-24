// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// testCollisionModule builds a schema-v1 legacy module that captures one file
// and restores it to target. matches sets the module's matcher identity so the
// collision guard's precedence can be exercised deterministically.
func testCollisionModule(t *testing.T, dir, moduleID, leaf, target string, matches modules.MatchCriteria) *modules.Module {
	t.Helper()
	source := filepath.Join(dir, leaf+".json")
	writeCaptureFile(t, source, []byte("payload-"+leaf))
	return &modules.Module{
		ID:          moduleID,
		DisplayName: moduleID,
		Matches:     matches,
		Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
			Source: source, Dest: "apps/" + leaf + "/" + leaf + ".json",
		}}},
		Restore: []modules.RestoreDef{{
			Type: "copy", Source: "./payload/apps/" + leaf + "/" + leaf + ".json", Target: target, Backup: true,
		}},
	}
}

func restoresForTarget(loadedRestores []restoreTargetRow, target string) []restoreTargetRow {
	var rows []restoreTargetRow
	for _, row := range loadedRestores {
		if row.Target == target {
			rows = append(rows, row)
		}
	}
	return rows
}

type restoreTargetRow struct {
	Target     string
	FromModule string
}

func loadedRestoreRows(t *testing.T, zipPath string) []restoreTargetRow {
	t.Helper()
	loaded, _ := loadCaptureBundle(t, zipPath)
	rows := make([]restoreTargetRow, 0, len(loaded.Restore))
	for _, entry := range loaded.Restore {
		rows = append(rows, restoreTargetRow{Target: entry.Target, FromModule: entry.FromModule})
	}
	return rows
}

// TestCaptureCollisionPackageIdentifiedBeatsPathExists verifies that when two
// captured modules claim the same restore target, the package-identified module
// wins, the pathExists-only loser's colliding entry is dropped, and exactly one
// friendly warning is surfaced.
func TestCaptureCollisionPackageIdentifiedBeatsPathExists(t *testing.T) {
	dir := t.TempDir()
	target := `%USERPROFILE%\.wslconfig`
	winner := testCollisionModule(t, dir, "apps.zeta-winget", "zeta", target, modules.MatchCriteria{Winget: []string{"Zeta.Pkg"}})
	loser := testCollisionModule(t, dir, "apps.alpha-path", "alpha", target, modules.MatchCriteria{PathExists: []string{`%USERPROFILE%\.wslconfig`}})

	request := testCaptureBundleRequest(t, dir, []*modules.Module{loser, winner}, nil)
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}

	rows := loadedRestoreRows(t, request.OutputPath)
	claimants := restoresForTarget(rows, target)
	if len(claimants) != 1 {
		t.Fatalf("target %q claimed by %d restore entries, want 1: %+v", target, len(claimants), rows)
	}
	if claimants[0].FromModule != winner.ID {
		t.Fatalf("winner = %q, want %q (package-identified beats pathExists-only)", claimants[0].FromModule, winner.ID)
	}
	if !containsString(result.CaptureWarnings, captureTargetCollisionWarning) {
		t.Fatalf("capture warnings %q missing collision warning %q", result.CaptureWarnings, captureTargetCollisionWarning)
	}
	warningCount := 0
	for _, warning := range result.CaptureWarnings {
		if warning == captureTargetCollisionWarning {
			warningCount++
		}
	}
	if warningCount != 1 {
		t.Fatalf("collision warning emitted %d times, want exactly 1: %q", warningCount, result.CaptureWarnings)
	}
}

// TestCaptureCollisionTieBreaksByModuleID verifies that two same-tier
// (pathExists-only) modules resolve deterministically to the lexicographically
// smaller module ID.
func TestCaptureCollisionTieBreaksByModuleID(t *testing.T) {
	dir := t.TempDir()
	target := `%USERPROFILE%\.bashrc`
	pathMatch := modules.MatchCriteria{PathExists: []string{`%USERPROFILE%\.bashrc`}}
	first := testCollisionModule(t, dir, "apps.aaa", "aaa", target, pathMatch)
	second := testCollisionModule(t, dir, "apps.bbb", "bbb", target, pathMatch)

	request := testCaptureBundleRequest(t, dir, []*modules.Module{second, first}, nil)
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}
	claimants := restoresForTarget(loadedRestoreRows(t, request.OutputPath), target)
	if len(claimants) != 1 || claimants[0].FromModule != first.ID {
		t.Fatalf("tie-break winner = %+v, want single entry from %q", claimants, first.ID)
	}
	if !containsString(result.CaptureWarnings, captureTargetCollisionWarning) {
		t.Fatalf("capture warnings %q missing collision warning", result.CaptureWarnings)
	}
}

// TestCaptureNoCollisionLeavesOutputUntouched verifies distinct targets are not
// touched and no collision warning fires.
func TestCaptureNoCollisionLeavesOutputUntouched(t *testing.T) {
	dir := t.TempDir()
	targetA := `%USERPROFILE%\.config-a`
	targetB := `%USERPROFILE%\.config-b`
	modA := testCollisionModule(t, dir, "apps.aaa", "aaa", targetA, modules.MatchCriteria{Winget: []string{"A.Pkg"}})
	modB := testCollisionModule(t, dir, "apps.bbb", "bbb", targetB, modules.MatchCriteria{PathExists: []string{targetB}})

	request := testCaptureBundleRequest(t, dir, []*modules.Module{modA, modB}, nil)
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}
	rows := loadedRestoreRows(t, request.OutputPath)
	if len(restoresForTarget(rows, targetA)) != 1 || len(restoresForTarget(rows, targetB)) != 1 {
		t.Fatalf("non-colliding restores altered: %+v", rows)
	}
	if containsString(result.CaptureWarnings, captureTargetCollisionWarning) {
		t.Fatalf("collision warning fired without a collision: %q", result.CaptureWarnings)
	}
}

// TestCaptureCollisionTotalLoserDropsItsWholeLane covers the field failure a
// stale duplicate module caused: in a mixed-v2 bundle every legacy module gets
// a lane, so a module whose only restore entry lost a collision kept a lane no
// restore entry referenced. Strict final manifest validation rejects exactly
// that, which failed the whole capture instead of the one redundant module. The
// loser must lose its lane, its configModules listing and its payload with the
// entry — the winner's bundle is otherwise unchanged.
func TestCaptureCollisionTotalLoserDropsItsWholeLane(t *testing.T) {
	dir := t.TempDir()
	generationRoot := filepath.Join(dir, "v2-root")
	writeCaptureFile(t, filepath.Join(generationRoot, "prefs.json"), []byte("v2"))
	plan := testGenerationCapturePlan(t, "apps.v2", "instance-a", generationRoot, false, false)

	target := `%USERPROFILE%\.wslconfig`
	winner := testCollisionModule(t, dir, "apps.wsl", "wsl", target, modules.MatchCriteria{Winget: []string{"Canonical.WSL"}})
	loser := testCollisionModule(t, dir, "apps.wsl-config", "wslconfig", target, modules.MatchCriteria{PathExists: []string{target}})

	request := testCaptureBundleRequest(t, dir, []*modules.Module{plan.Module, loser, winner}, []ConfigSetCapturePlan{plan})
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}
	if result.ManifestVersion != 2 {
		t.Fatalf("manifest version = %d, want a mixed-v2 bundle (lanes only exist there)", result.ManifestVersion)
	}

	loaded, _ := loadCaptureBundle(t, request.OutputPath)
	for _, lane := range loaded.LegacyConfigLanes {
		if lane.ModuleID == loser.ID {
			t.Fatalf("loser %q kept an orphaned lane %+v", loser.ID, lane)
		}
	}
	if containsString(loaded.ConfigModules, loser.ID) {
		t.Fatalf("configModules %v still lists the dropped loser %q", loaded.ConfigModules, loser.ID)
	}

	claimants := restoresForTarget(loadedRestoreRows(t, request.OutputPath), target)
	if len(claimants) != 1 || claimants[0].FromModule != winner.ID {
		t.Fatalf("target %q claimants = %+v, want one entry from %q", target, claimants, winner.ID)
	}
	winnerLanes := 0
	for _, lane := range loaded.LegacyConfigLanes {
		if lane.ModuleID == winner.ID {
			winnerLanes++
		}
	}
	if winnerLanes != 1 {
		t.Fatalf("winner %q has %d lanes, want exactly 1: %+v", winner.ID, winnerLanes, loaded.LegacyConfigLanes)
	}

	// The dropped module's payload must not ship: nothing would ever restore it.
	loserPayload := "configs/" + readableConfigDirName(loser.ID, LegacyCaptureID(loser.ID))
	for _, entry := range zipEntryNames(t, request.OutputPath) {
		if strings.HasPrefix(entry, loserPayload+"/") {
			t.Fatalf("dropped loser payload still in bundle: %q", entry)
		}
	}
	if !containsString(result.CaptureWarnings, captureTargetCollisionWarning) {
		t.Fatalf("capture warnings %q missing collision warning", result.CaptureWarnings)
	}
}
