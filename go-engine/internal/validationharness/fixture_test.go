// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestFixturePlanDeterministicFilesDirectoriesOptionalAndExclusion(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	firstContext := fixtureValidationContext(t, mod.ID, scenario.ID)
	first, failure := compileFixturePlan(firstContext, mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	secondContext := fixtureValidationContext(t, mod.ID, scenario.ID)
	second, failure := compileFixturePlan(secondContext, mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(first.Targets) != 2 || len(second.Targets) != 2 {
		t.Fatalf("targets = %d/%d, want 2", len(first.Targets), len(second.Targets))
	}
	for index := range first.Targets {
		if first.Targets[index].Captured != second.Targets[index].Captured || first.Targets[index].Mutated != second.Targets[index].Mutated {
			t.Fatalf("target %d sentinel is not deterministic", index)
		}
	}
	if first.Targets[0].Directory || !first.Targets[1].Directory || !first.Targets[0].Optional || len(first.Targets[1].OverlappingExcluded) == 0 {
		t.Fatalf("compiled fixture shape = %+v", first.Targets)
	}

	if failure := first.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	if failure := first.CompareCaptureSeed(); failure != nil {
		t.Fatal(failure)
	}
	for _, excluded := range first.Targets[1].OverlappingExcluded {
		if _, err := os.Stat(excluded.Path); err != nil {
			t.Fatalf("excluded fixture was not materialized: %v", err)
		}
	}
	if failure := first.Mutate(); failure != nil {
		t.Fatal(failure)
	}
	if failure := first.CompareMutated(); failure != nil {
		t.Fatal(failure)
	}
	if failure := first.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	if failure := first.CompareCaptureSeed(); failure != nil {
		t.Fatalf("repeat materialization did not converge: %v", failure)
	}
	directory := first.Targets[1].Resolved
	if _, err := os.Stat(filepath.Join(directory, filepath.Base(directory))); !os.IsNotExist(err) {
		t.Fatalf("directory copy nested itself at %s", directory)
	}
}

func TestProductionDolphinAutoFixtureUsesDirectoriesAndBindsNestedVerifier(t *testing.T) {
	repo := productionLiveRepoRoot(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.dolphin-emulator"]
	record := catalog.Records["apps.dolphin-emulator"]
	if mod == nil || len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("Dolphin catalog authority = module=%+v scenarios=%+v", mod, record.Synthetic.Scenarios)
	}
	scenario := record.Synthetic.Scenarios[0]
	definitions, failure := compileFixtureDefinitionsAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.Targets) != 8 {
		t.Fatalf("Dolphin targets = %d, want 8", len(plan.Targets))
	}
	globalOnlyOwners := 0
	for _, target := range plan.Targets {
		if len(target.CaptureExcluded) != 0 {
			globalOnlyOwners++
		}
		if target.Authored != `%USERPROFILE%\Documents\Dolphin Emulator\Config` {
			continue
		}
		if len(target.RestoreExcluded) != 0 || len(target.OverlappingExcluded) != 2 {
			t.Fatalf("Dolphin legacy Config exclusions = capture:%+v restore:%+v overlap:%+v", target.CaptureExcluded, target.RestoreExcluded, target.OverlappingExcluded)
		}
		for _, witness := range target.OverlappingExcluded {
			if len(witness.CapturePatterns) == 0 || len(witness.RestorePatterns) == 0 {
				t.Fatalf("Dolphin legacy Config witness lacks global capture and restore roles: %+v", witness)
			}
		}
	}
	if globalOnlyOwners != 1 {
		t.Fatalf("Dolphin global-only capture witness owners = %d, want 1", globalOnlyOwners)
	}
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	for _, target := range plan.Targets {
		if !target.Directory {
			t.Fatalf("Dolphin %s is not a deterministic directory fixture", target.Coordinate)
		}
		if target.Authored != `%APPDATA%\Dolphin Emulator\Config` {
			continue
		}
		if !target.Directory || target.PayloadPath != filepath.Join(target.Resolved, "Dolphin.ini") {
			t.Fatalf("Dolphin Config target = %+v, want nested verifier directory payload", target)
		}
		info, err := os.Stat(target.PayloadPath)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("Dolphin nested verifier payload = %v, %v; want regular file", info, err)
		}
		return
	}
	t.Fatal("Dolphin Config capture target is absent")
}

func TestProductionClinkAutoFixtureUsesOneDirectoryAndOverlappingExclusions(t *testing.T) {
	repo := productionLiveRepoRoot(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.clink"]
	record := catalog.Records["apps.clink"]
	if mod == nil || len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("Clink catalog authority = module=%+v scenarios=%+v", mod, record.Synthetic.Scenarios)
	}
	scenario := record.Synthetic.Scenarios[0]
	definitions, failure := compileFixtureDefinitionsAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(definitions.Entries) != 1 || definitions.Entries[0].Destination != "apps/clink/profile" {
		t.Fatalf("Clink capture destination = %+v, want existing profile lane", definitions.Entries)
	}
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.Targets) != 1 {
		t.Fatalf("Clink targets = %+v, want one parent directory", plan.Targets)
	}
	target := plan.Targets[0]
	if !target.Directory || target.Authored != `%LOCALAPPDATA%\clink` || target.PayloadPath != filepath.Join(target.Resolved, "clink_settings") {
		t.Fatalf("Clink target = %+v", target)
	}
	if len(target.CaptureExcluded) != 0 || len(target.RestoreExcluded) != 0 || len(target.OverlappingExcluded) != 2 {
		t.Fatalf("Clink exclusions = capture:%+v restore:%+v overlap:%+v", target.CaptureExcluded, target.RestoreExcluded, target.OverlappingExcluded)
	}
	for _, witness := range target.OverlappingExcluded {
		if len(witness.CapturePatterns) == 0 || len(witness.RestorePatterns) == 0 {
			t.Fatalf("Clink overlapping witness = %+v", witness)
		}
	}
}

func TestProductionWinampAutoFixtureHasNoRedundantMilkdropTargetOrOverlap(t *testing.T) {
	repo := productionLiveRepoRoot(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.winamp"]
	record := catalog.Records["apps.winamp"]
	if mod == nil || len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("Winamp catalog authority = module=%+v scenarios=%+v", mod, record.Synthetic.Scenarios)
	}
	scenario := record.Synthetic.Scenarios[0]
	definitions, failure := compileFixtureDefinitionsAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.Targets) != 8 {
		t.Fatalf("Winamp targets = %d, want 8", len(plan.Targets))
	}
	for index, target := range plan.Targets {
		if target.Authored == `%APPDATA%\Winamp\Plugins\milk2.ini` {
			t.Fatalf("Winamp target retains redundant Milkdrop = %+v", target)
		}
		for other := range plan.Targets[:index] {
			if fixtureTargetRootsOverlap(target.Resolved, plan.Targets[other].Resolved) {
				t.Fatalf("Winamp fixture target roots overlap: %q and %q", target.Resolved, plan.Targets[other].Resolved)
			}
		}
	}

	wantCachePatterns := []string{
		`**\main.dat`, `**\main.idx`, `**\recent.dat`, `**\recent.idx`,
		`**\met*.vmd`, `**\Cache\**`, `**\*.log`, `**\Temp\**`,
	}
	if !exactStrings(mod.Capture.ExcludeGlobs, wantCachePatterns) {
		t.Fatalf("Winamp capture exclusions = %v, want precise cache patterns %v", mod.Capture.ExcludeGlobs, wantCachePatterns)
	}
	for _, relative := range []string{"ml/settings.ini", "ml/theme.xml"} {
		for _, pattern := range mod.Capture.ExcludeGlobs {
			matched, err := bundle.ConfigPathMatchesExcludeGlob(relative, pattern)
			if err != nil {
				t.Fatalf("Winamp capture pattern %q is invalid: %v", pattern, err)
			}
			if matched {
				t.Fatalf("Winamp capture pattern %q suppresses non-cache path %q", pattern, relative)
			}
		}
	}
	for _, relative := range []string{"ml/main.dat", "ml/recent.idx", "ml/metadata.vmd"} {
		matched := false
		for _, pattern := range mod.Capture.ExcludeGlobs {
			if ok, err := bundle.ConfigPathMatchesExcludeGlob(relative, pattern); err == nil && ok {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("Winamp capture keeps known cache path %q", relative)
		}
	}
	pluginsRestore := mod.Restore[len(mod.Restore)-1]
	if pluginsRestore.Target != `%APPDATA%\Winamp\Plugins` || !exactStrings(pluginsRestore.Exclude, wantCachePatterns) {
		t.Fatalf("Winamp Plugins restore exclusions = target:%q patterns:%v, want %v", pluginsRestore.Target, pluginsRestore.Exclude, wantCachePatterns)
	}
}

func TestProductionWaveLinkV1ArtifactFlattensNestedDestination(t *testing.T) {
	repo := productionLiveRepoRoot(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.wave-link"]
	record := catalog.Records["apps.wave-link"]
	if mod == nil || len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("Wave Link catalog authority = module=%+v scenarios=%+v", mod, record.Synthetic.Scenarios)
	}
	scenario := record.Synthetic.Scenarios[0]
	definitions, failure := compileFixtureDefinitionsAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	for _, target := range plan.Targets {
		if target.Destination != "apps/wave-link/WaveLink3/Backup" {
			continue
		}
		if got, want := v1ArtifactPayloadPath(mod.ID, target.Destination), "configs/wave-link/Backup"; got != want {
			t.Fatalf("flattened v1 payload root = %q, want %q", got, want)
		}
		if got, want := v1RestoreSource(mod.ID, target.Destination), "./configs/wave-link/Backup"; got != want {
			t.Fatalf("rewritten v1 restore source = %q, want %q", got, want)
		}
		if got, ok := targetArtifactPayloadName(mod.ID, target); !ok || got != "configs/wave-link/Backup/"+fixturePayloadName {
			t.Fatalf("Wave Link artifact member = %q, %t", got, ok)
		}
		return
	}
	t.Fatal("Wave Link Backup capture target is absent")
}

func TestFixtureComparisonDetectsContentMismatch(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	if err := os.WriteFile(plan.Targets[0].PayloadPath, []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := plan.CompareCaptured(); failure == nil || failure.Code != CodeContentMismatch {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestFixtureComparisonRejectsExtraDirectoryDescendant(t *testing.T) {
	plan := fixtureScenarioRuntime(t).Plan
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.Mutate(); failure != nil {
		t.Fatal(failure)
	}
	for _, target := range plan.Targets {
		if err := os.WriteFile(target.PayloadPath, []byte(target.Captured), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	extra := filepath.Join(plan.Targets[1].Resolved, "unexpected.txt")
	if err := os.WriteFile(extra, []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := plan.CompareCaptured(); failure == nil || failure.Code != CodeContentMismatch {
		t.Fatalf("extra descendant failure = %+v", failure)
	}
}

func TestFixturePlanRejectsUnwitnessableExcludeGlob(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	definitions.Entries[1].TargetExclude = []string{"**/profile-??.tmp"}
	if _, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions); failure == nil || failure.Code != CodeUnsupportedFixture {
		t.Fatalf("unwitnessable exclude failure = %+v", failure)
	}
}

func TestFixturePlanWitnessesWildcardDirectoryExcludeWithProductionMatcher(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	definitions.Entries[1].TargetExclude = []string{"**/Crash*/**"}
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	witnesses := plan.Targets[1].RestoreExcluded
	if len(witnesses) != 1 {
		t.Fatalf("wildcard directory witnesses = %+v, want exactly one", witnesses)
	}
	if got, want := witnesses[0].Relative, "Crashfixture/"+fixturePayloadName; got != want {
		t.Fatalf("wildcard directory witness = %q, want %q", got, want)
	}
	matched, err := bundle.ConfigPathMatchesExcludeGlob(witnesses[0].Relative, "**/Crash*/**")
	if err != nil || !matched {
		t.Fatalf("production matcher witness = %t, %v; want true, nil", matched, err)
	}
}

func TestExcludedFixtureRelativesWitnessesBasenameWildcardsWithProductionMatcher(t *testing.T) {
	patterns := []string{"**/clink_history*", "**/met*.vmd"}
	witnesses, ok := excludedFixtureRelatives(patterns)
	if !ok || !exactStrings(witnesses, []string{"clink_historyfixture", "metfixture.vmd"}) {
		t.Fatalf("basename wildcard witnesses = %v, %t", witnesses, ok)
	}
	for index, witness := range witnesses {
		matched, err := bundle.ConfigPathMatchesExcludeGlob(witness, patterns[index])
		if err != nil || !matched {
			t.Fatalf("production matcher witness %q for %q = %t, %v", witness, patterns[index], matched, err)
		}
	}
}

func TestExcludedFixtureRelativesRejectsUnsafeOrUnreachablePatterns(t *testing.T) {
	for _, pattern := range []string{"../history*", "**/profile-??.tmp", "**/broken[.tmp", "**/nested/path*.tmp"} {
		if witnesses, ok := excludedFixtureRelatives([]string{pattern}); ok || witnesses != nil {
			t.Fatalf("unsafe pattern %q witnesses = %v, %t", pattern, witnesses, ok)
		}
	}
}

func TestFixturePlanWitnessesWildcardGlobalDirectoryExcludeWithProductionMatcher(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	for index := range definitions.Entries {
		definitions.Entries[index].GlobalExclude = []string{"**/Crash*/**"}
	}
	first, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	second, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	for _, plan := range []*FixturePlan{first, second} {
		witnesses := plan.Targets[1].CaptureExcluded
		if len(witnesses) != 1 {
			t.Fatalf("wildcard global directory witnesses = %+v, want exactly one", witnesses)
		}
		if got, want := witnesses[0].Relative, "Crashfixture/"+fixturePayloadName; got != want {
			t.Fatalf("wildcard global directory witness = %q, want %q", got, want)
		}
		matched, err := bundle.ConfigPathMatchesExcludeGlob(witnesses[0].Relative, "**/Crash*/**")
		if err != nil || !matched {
			t.Fatalf("production matcher global witness = %t, %v; want true, nil", matched, err)
		}
	}
	if first.Targets[1].CaptureExcluded[0].Relative != second.Targets[1].CaptureExcluded[0].Relative {
		t.Fatal("wildcard global directory witness is not deterministic")
	}
}

func TestFixturePlanClassifiesConcreteRestoreWitnessMatchingGlobalCapturePattern(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	for index := range definitions.Entries {
		definitions.Entries[index].GlobalExclude = []string{"**/*fixture*"}
	}
	definitions.Entries[1].TargetExclude = []string{"**/Logs/**"}
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	target := plan.Targets[1]
	if len(target.CaptureExcluded) != 1 || len(target.RestoreExcluded) != 0 || len(target.OverlappingExcluded) != 1 {
		t.Fatalf("concrete global/restore roles = capture:%+v restore:%+v overlap:%+v", target.CaptureExcluded, target.RestoreExcluded, target.OverlappingExcluded)
	}
	witness := target.OverlappingExcluded[0]
	if !exactStrings(witness.CapturePatterns, []string{"**/*fixture*"}) || !exactStrings(witness.RestorePatterns, []string{"**/Logs/**"}) {
		t.Fatalf("concrete global/restore witness = %+v", witness)
	}
}

func TestFixturePlanSeparatesCaptureAndRestoreExcludeWitnesses(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(splitExcludeFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	target := &plan.Targets[1]
	if len(target.CaptureExcluded) != 1 || len(target.RestoreExcluded) != 1 || len(target.OverlappingExcluded) != 0 {
		t.Fatalf("exclusion roles = capture:%+v restore:%+v overlap:%+v", target.CaptureExcluded, target.RestoreExcluded, target.OverlappingExcluded)
	}
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	assertFixtureWitnessContent(t, target.CaptureExcluded[0].Path, target.CaptureExcluded[0].Captured)
	assertFixtureWitnessContent(t, target.RestoreExcluded[0].Path, target.RestoreExcluded[0].Captured)
	if failure := plan.Mutate(); failure != nil {
		t.Fatal(failure)
	}
	if _, err := os.Lstat(target.CaptureExcluded[0].Path); !os.IsNotExist(err) {
		t.Fatalf("capture-only witness survived target mutation: %v", err)
	}
	assertFixtureWitnessContent(t, target.RestoreExcluded[0].Path, target.RestoreExcluded[0].Mutated)
	if failure := plan.MaterializeRestored(); failure != nil {
		t.Fatal(failure)
	}
	if failure := plan.CompareCaptured(); failure != nil {
		t.Fatalf("restore-only witness was not preserved through restore: %+v", failure)
	}
}

func TestFixturePlanModelsCaptureRestoreExcludeOverlapWithoutRestoreClaim(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	target := plan.Targets[1]
	if len(target.CaptureExcluded) != 0 || len(target.RestoreExcluded) != 0 || len(target.OverlappingExcluded) != 1 {
		t.Fatalf("overlapping exclusion was overclaimed: capture:%+v restore:%+v overlap:%+v", target.CaptureExcluded, target.RestoreExcluded, target.OverlappingExcluded)
	}
}

func assertFixtureWitnessContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("fixture witness %s = %q, %v; want %q", filepath.Base(path), data, err, want)
	}
}

func TestFixturePlanAcceptsFileOnlyNonmatchingGlobalExcludeWithoutWitness(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	mod.Capture.ExcludeGlobs = []string{`**\*.log`}
	scenario := fixtureScenario()
	definitions, failure := compileFixtureDefinitions(mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].Directory || len(plan.Targets[0].CaptureExcluded) != 0 {
		t.Fatalf("file-only nonmatching global exclude plan = %+v", plan)
	}
}

func TestFixturePlanRejectsFileOnlyGlobalExcludeMatchingExactSourceBasename(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	mod.Capture.ExcludeGlobs = []string{`**\settings.json`}
	scenario := fixtureScenario()
	definitions, failure := compileFixtureDefinitions(mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	if plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions); plan != nil || failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "capture.files[0]" {
		t.Fatalf("matching file-only global exclude plan=%+v failure=%+v", plan, failure)
	}
}

func TestFixturePlanRejectsMalformedFileOnlyGlobalExclude(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	mod.Capture.ExcludeGlobs = []string{`**\broken[.tmp`}
	scenario := fixtureScenario()
	definitions, failure := compileFixtureDefinitions(mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	if plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions); plan != nil || failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "capture.excludeGlobs[0]" {
		t.Fatalf("malformed file-only global exclude plan=%+v failure=%+v", plan, failure)
	}
}

func TestFixturePlanMixedGlobalExcludeRejectsMatchingDirectFile(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	for index := range definitions.Entries {
		definitions.Entries[index].GlobalExclude = []string{`**\settings.json`}
	}
	if plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions); plan != nil || failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "capture.files[0]" {
		t.Fatalf("matching mixed global exclude plan=%+v failure=%+v", plan, failure)
	}
}

func TestFixturePlanRejectsOverlappingTargetRootsBeforeMaterialization(t *testing.T) {
	for _, targets := range [][2]string{
		{`%APPDATA%\Fixture\Profiles`, `%APPDATA%\fixture\profiles`},
		{`%APPDATA%\Fixture/Profiles`, `%APPDATA%\fixture\Profiles\Nested`},
	} {
		mod, err := modules.ParseModuleJSON([]byte(fixtureTargetTopologyModuleJSON(targets[0], targets[1])))
		if err != nil {
			t.Fatal(err)
		}
		scenario := fixtureScenario()
		definitions, failure := compileFixtureDefinitions(mod, scenario)
		if failure != nil {
			t.Fatal(failure)
		}
		plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
		if plan != nil || failure == nil || failure.Code != CodeUnsupportedFixture {
			t.Fatalf("overlapping targets plan=%+v failure=%+v", plan, failure)
		}
	}

	mod, err := modules.ParseModuleJSON([]byte(fixtureTargetTopologyModuleJSON(`%APPDATA%\Fixture\Profiles`, `%APPDATA%\Fixture\ProfilesBackup`)))
	if err != nil {
		t.Fatal(err)
	}
	scenario := fixtureScenario()
	definitions, failure := compileFixtureDefinitions(mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	if plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions); failure != nil || len(plan.Targets) != 2 {
		t.Fatalf("sibling target plan=%+v failure=%+v", plan, failure)
	}
}

func TestRoundtripFixtureRejectsRestoreWithoutBackupEvidence(t *testing.T) {
	raw := strings.Replace(directoryFixtureModuleJSON, `"backup":true`, `"backup":false`, 1)
	mod, err := modules.ParseModuleJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := compileFixtureDefinitions(mod, fixtureScenario()); failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "restore[0]" {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestCompileFixtureDefinitionsRejectsWildcardAuthoredOperationsBeforePlanning(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := modules.LoadCatalog(filepath.Join(repositoryRoot, "modules", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	betterbird := catalog["apps.betterbird"]
	if betterbird == nil {
		t.Fatal("apps.betterbird is absent from catalog")
	}
	betterbirdCaptureWildcard := *betterbird
	betterbirdCaptureWildcard.Restore = append([]modules.RestoreDef(nil), betterbird.Restore...)
	betterbirdCaptureWildcard.Restore[0].Target = strings.Replace(betterbirdCaptureWildcard.Restore[0].Target, "*.default-release", "default-release", 1)
	betterbirdCapture := *betterbird.Capture
	betterbirdCapture.Files = append([]modules.CaptureFile(nil), betterbird.Capture.Files...)
	betterbirdCapture.Files[0].Source = strings.Replace(betterbirdCapture.Files[0].Source, "*.default-release", "?.default-release", 1)
	betterbirdCaptureWildcard.Capture = &betterbirdCapture
	blenderCharacterClass := *catalog["apps.blender"]
	blenderCharacterClass.Verify = append([]modules.VerifyDef(nil), blenderCharacterClass.Verify...)
	blenderCharacterClass.Verify[0].Path = strings.Replace(blenderCharacterClass.Verify[0].Path, "Blender*", "Blender[0-9]", 1)

	for _, test := range []struct {
		name       string
		module     *modules.Module
		coordinate string
	}{
		{name: "Betterbird restore target", module: betterbird, coordinate: "restore[0].target"},
		{name: "Betterbird capture source", module: &betterbirdCaptureWildcard, coordinate: "capture.files[0].source"},
		{name: "Blender file verifier", module: catalog["apps.blender"], coordinate: "verify[0].path"},
		{name: "Blender file verifier character class", module: &blenderCharacterClass, coordinate: "verify[0].path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, failure := compileFixtureDefinitions(test.module, fixtureScenario())
			if failure == nil || failure.Code != CodeUnsupportedFixture || failure.Phase != "fixture" || failure.Coordinate != test.coordinate || failure.Detail != "authored operation does not support wildcard paths" {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}

func directoryFixtureDefinitions(t *testing.T, mod *modules.Module) (validationmatrix.Scenario, fixtureDefinitions) {
	t.Helper()
	scenario := fixtureScenario()
	scenario.Fixture = validationmatrix.Fixture{Type: validationmatrix.FixtureDeclarative}
	definitions, failure := compileFixtureDefinitions(mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	definitions.Entries[0].Kind = fixtureKindFile
	definitions.Entries[1].Kind = fixtureKindDirectory
	return scenario, definitions
}

func TestFixtureMutationRefusesLinkedMember(t *testing.T) {
	plan := fixtureScenarioRuntime(t).Plan
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	external := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(external, []byte("outside-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(plan.Targets[1].Resolved, "linked.txt")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("host cannot create a test link: %v", err)
	}
	if failure := plan.Mutate(); failure == nil || failure.Code != CodeIsolationFailure {
		t.Fatalf("failure = %+v", failure)
	}
	data, err := os.ReadFile(external)
	if err != nil || string(data) != "outside-sentinel" {
		t.Fatalf("external link target changed: %q err=%v", data, err)
	}
}

func TestFixtureComparisonRejectsPayloadLinkSwap(t *testing.T) {
	plan := fixtureScenarioRuntime(t).Plan
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	target := plan.Targets[0]
	external := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(external, []byte(target.Captured), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target.PayloadPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, target.PayloadPath); err != nil {
		t.Skipf("host cannot create a test link: %v", err)
	}
	if failure := plan.CompareCaptured(); failure == nil || failure.Code != CodeIsolationFailure {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestFixtureCleanupDoesNotUseRecursiveRemoveAll(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate fixture test")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "os.RemoveAll(") {
		t.Fatal("fixture cleanup must use guarded postorder non-recursive removal")
	}
}

func TestFixtureCleanupRejectsFileToDirectorySwapBeforeRemoval(t *testing.T) {
	plan := fixtureScenarioRuntime(t).Plan
	if failure := plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	target := &plan.Targets[0]
	entries, err := fixtureTreePostorder(target.Resolved)
	if err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(target.Resolved), "later-sibling.txt")
	if err := os.WriteFile(sibling, []byte("preserve-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target.Resolved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target.Resolved, 0o700); err != nil {
		t.Fatal(err)
	}
	if failure := plan.removeFixtureEntries(target, entries); failure == nil || failure.Code != CodeIsolationFailure {
		t.Fatalf("failure = %+v", failure)
	}
	if info, err := os.Stat(target.Resolved); err != nil || !info.IsDir() {
		t.Fatalf("swapped target was removed: info=%v err=%v", info, err)
	}
	data, err := os.ReadFile(sibling)
	if err != nil || string(data) != "preserve-me" {
		t.Fatalf("later sibling changed: %q err=%v", data, err)
	}
}

func fixtureValidationContext(t *testing.T, moduleID, scenarioID string) *validationmode.Context {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "endstate-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	nonce := strings.TrimPrefix(filepath.Base(root), "endstate-validation-")
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: scenarioID, Nonce: nonce, ModuleID: moduleID,
		Inventory: validationmode.Inventory{AppID: "vendor-fixture", Driver: "winget", Ref: "Vendor.Fixture", DisplayName: "Fixture", InitialState: "present"},
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	context, err := validationmode.LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	restore, err := context.Activate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restore() })
	return context
}

const directoryFixtureModuleJSON = `{
  "id":"apps.fixture","displayName":"Fixture","sensitivity":"none",
  "matches":{"winget":["Vendor.Fixture"]},
  "verify":[{"type":"file-exists","path":"%APPDATA%\\Fixture\\settings.json"}],
  "restore":[
    {"type":"copy","source":"./payload/apps/fixture/settings.json","target":"%APPDATA%\\Fixture\\settings.json","backup":true,"optional":true},
    {"type":"copy","source":"./payload/apps/fixture/profiles","target":"%APPDATA%\\Fixture\\profiles","backup":true,"exclude":["**\\Cache\\**"]}
  ],
  "capture":{"files":[
    {"source":"%APPDATA%\\Fixture\\settings.json","dest":"apps/fixture/settings.json","optional":true},
    {"source":"%APPDATA%\\Fixture\\profiles","dest":"apps/fixture/profiles"}
  ],"excludeGlobs":["**\\Cache\\**"]}
}`

const splitExcludeFixtureModuleJSON = `{
  "id":"apps.fixture","displayName":"Fixture","sensitivity":"none",
  "matches":{"winget":["Vendor.Fixture"]},
  "verify":[{"type":"file-exists","path":"%APPDATA%\\Fixture\\settings.json"}],
  "restore":[
    {"type":"copy","source":"./payload/apps/fixture/settings.json","target":"%APPDATA%\\Fixture\\settings.json","backup":true,"optional":true},
    {"type":"copy","source":"./payload/apps/fixture/profiles","target":"%APPDATA%\\Fixture\\profiles","backup":true,"exclude":["**\\RestoreOnly\\**"]}
  ],
  "capture":{"files":[
    {"source":"%APPDATA%\\Fixture\\settings.json","dest":"apps/fixture/settings.json","optional":true},
    {"source":"%APPDATA%\\Fixture\\profiles","dest":"apps/fixture/profiles"}
  ],"excludeGlobs":["**\\CaptureOnly\\**"]}
}`

func fixtureTargetTopologyModuleJSON(first, second string) string {
	return fmt.Sprintf(`{
  "id":"apps.fixture","displayName":"Fixture","sensitivity":"none",
  "matches":{"winget":["Vendor.Fixture"]},
  "restore":[
    {"type":"copy","source":"./payload/apps/fixture/first","target":%q,"backup":true},
    {"type":"copy","source":"./payload/apps/fixture/second","target":%q,"backup":true}
  ],
  "capture":{"files":[
    {"source":%q,"dest":"apps/fixture/first"},
    {"source":%q,"dest":"apps/fixture/second"}
  ]}
}`, first, second, first, second)
}
