// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestCompileCaptureContractBindsExactProductionMGBAAuthority(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", ".."))
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.mgba"]
	record := catalog.Records["apps.mgba"]
	if mod == nil || len(record.Synthetic.Scenarios) != 1 {
		t.Fatalf("production mGBA authority is absent: module=%v scenarios=%d", mod != nil, len(record.Synthetic.Scenarios))
	}

	plan, failure := compileCaptureContract(mod, record.Synthetic.Scenarios[0])
	if failure != nil {
		t.Fatalf("compile capture contract: %+v", failure)
	}
	if plan.ModuleID != "apps.mgba" || plan.ModuleRevision != mod.Revision || plan.ScenarioID != "reviewed-capture-v1" {
		t.Fatalf("compiled identity = %+v", plan)
	}
	if plan.Inventory.AppID != "mgba-emu-mgba" || plan.Inventory.Driver != "winget" || plan.Inventory.Ref != "mgba-emu.mgba" || plan.Inventory.Source != "winget" || plan.Inventory.InitialState != "present" {
		t.Fatalf("compiled inventory = %+v", plan.Inventory)
	}
	if len(plan.Targets) != 1 {
		t.Fatalf("compiled targets = %+v", plan.Targets)
	}
	target := plan.Targets[0]
	if target.AuthoredSource != `%APPDATA%\mGBA\config.ini` || target.Destination != "apps/mgba/config.ini" || !target.Optional {
		t.Fatalf("compiled target = %+v", target)
	}
	if !bytes.Equal(target.Content, captureContractMGBAINI) {
		t.Fatalf("compiled content = %q", target.Content)
	}
	if len(plan.Verifiers) != 1 || plan.Verifiers[0].Type != "file-exists" || plan.Verifiers[0].Path != target.AuthoredSource {
		t.Fatalf("compiled verifier projection = %+v", plan.Verifiers)
	}
}

func TestCaptureContractJourneyCapturesThenProvesAllOptionalAbsenceCannotPass(t *testing.T) {
	plan := productionMGBACapturePlan(t)
	validationContext := fixtureValidationContext(t, plan.ModuleID, plan.ScenarioID)
	root := validationContext.Root()
	plan.context = validationContext
	plan.root = root
	resolved, err := validationContext.ResolveHostPath(plan.Targets[0].AuthoredSource, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	plan.Targets[0].Resolved = resolved
	runtime := &scenarioRuntime{
		Module: &modules.Module{ID: plan.ModuleID, Revision: plan.ModuleRevision},
		Scenario: validationmatrix.Scenario{
			ID: plan.ScenarioID, Mode: validationmatrix.ScenarioCaptureContract,
			MinimumAssertions: map[string]int{
				validationmatrix.AssertionCaptured: 1, validationmatrix.AssertionContent: 1,
				validationmatrix.AssertionPayload: 1, validationmatrix.AssertionProvenance: 1,
			},
		},
		CapturePlan: plan, Root: root, Inventory: plan.Inventory,
	}
	executor := &captureContractJourneyFixture{target: plan.Targets[0]}

	result := executeCaptureContractJourney(context.Background(), runtime, executor)
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("capture contract journey = %+v", result)
	}
	if !reflect.DeepEqual(executor.calls, []string{"capture", "optional-absent"}) {
		t.Fatalf("calls = %v", executor.calls)
	}
	wantCounts := map[string]int{
		validationmatrix.AssertionCaptured: 1, validationmatrix.AssertionContent: 1,
		validationmatrix.AssertionPayload: 1, validationmatrix.AssertionProvenance: 1,
	}
	if !reflect.DeepEqual(result.AssertionCounts, wantCounts) {
		t.Fatalf("assertion counts = %v", result.AssertionCounts)
	}
	wantProof := []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract}
	if !reflect.DeepEqual(result.ProofLevels, wantProof) {
		t.Fatalf("proof levels = %v", result.ProofLevels)
	}
}

type captureContractJourneyFixture struct {
	calls  []string
	target CaptureContractTarget
}

func (fixture *captureContractJourneyFixture) CaptureContract(context.Context, *scenarioRuntime) (captureContractEvidence, *Failure) {
	fixture.calls = append(fixture.calls, "capture")
	data, err := os.ReadFile(fixture.target.Resolved)
	if err != nil || !bytes.Equal(data, fixture.target.Content) {
		return captureContractEvidence{}, fail(CodeContentMismatch, "capture", fixture.target.Coordinate, "captured source was not materialized exactly")
	}
	return captureContractEvidence{AssertionCounts: map[string]int{
		validationmatrix.AssertionCaptured: 1, validationmatrix.AssertionContent: 1,
		validationmatrix.AssertionPayload: 1, validationmatrix.AssertionProvenance: 1,
	}}, nil
}

func (fixture *captureContractJourneyFixture) CaptureContractOptionalAbsent(context.Context, *scenarioRuntime) *Failure {
	fixture.calls = append(fixture.calls, "optional-absent")
	if _, err := os.Lstat(fixture.target.Resolved); !os.IsNotExist(err) {
		return fail(CodeArtifactContract, "capture", fixture.target.Coordinate, "optional source remained present")
	}
	return nil
}

func productionMGBACapturePlan(t *testing.T) *CaptureContractPlan {
	t.Helper()
	repo := filepath.Clean(filepath.Join("..", "..", ".."))
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, failure := compileCaptureContract(catalog.Modules["apps.mgba"], catalog.Records["apps.mgba"].Synthetic.Scenarios[0])
	if failure != nil {
		t.Fatal(failure)
	}
	return plan
}

func TestCompileCaptureContractRejectsForeignUnsafeOrAmbiguousAuthority(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", ".."))
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	production := catalog.Modules["apps.mgba"]
	scenario := catalog.Records["apps.mgba"].Synthetic.Scenarios[0]
	clone := func(t *testing.T) *modules.Module {
		t.Helper()
		mod, err := modules.ParseModuleJSON(production.CanonicalSnapshot())
		if err != nil {
			t.Fatal(err)
		}
		return mod
	}
	tests := []struct {
		name    string
		mutable bool
		mutate  func(*modules.Module, *validationmatrix.Scenario)
	}{
		{"mutable module identity", true, func(mod *modules.Module, _ *validationmatrix.Scenario) { mod.DisplayName = "Foreign" }},
		{"restore declaration", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Restore = []modules.RestoreDef{{Type: "copy", Source: "./payload/apps/mgba/config.ini", Target: `%APPDATA%\mGBA\config.ini`, Backup: true}}
		}},
		{"schema v2 declaration", false, func(mod *modules.Module, _ *validationmatrix.Scenario) { mod.Config = &modules.ConfigDef{} }},
		{"registry capture", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Capture.Files = nil
			mod.Capture.RegistryKeys = []modules.CaptureRegistryKey{{Key: `HKCU:\Software\mGBA`, Dest: "apps/mgba/registry.reg"}}
		}},
		{"unreviewed", false, func(_ *modules.Module, scenario *validationmatrix.Scenario) { scenario.Review = nil }},
		{"declarative fixture", false, func(_ *modules.Module, scenario *validationmatrix.Scenario) {
			scenario.Fixture.Type = validationmatrix.FixtureDeclarative
		}},
		{"ambiguous package", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Matches.Winget = append(mod.Matches.Winget, "Foreign.mGBA")
		}},
		{"relative source", false, func(mod *modules.Module, _ *validationmatrix.Scenario) { mod.Capture.Files[0].Source = "config.ini" }},
		{"traversing source", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Capture.Files[0].Source = `%APPDATA%\..\host.ini`
		}},
		{"raw host source", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Capture.Files[0].Source = `C:\Users\host\config.ini`
		}},
		{"escaping destination", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Capture.Files[0].Dest = "apps/mgba/../foreign.ini"
		}},
		{"applicable exclude", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Capture.ExcludeGlobs = []string{"**/config.ini"}
		}},
		{"malformed exclude", false, func(mod *modules.Module, _ *validationmatrix.Scenario) { mod.Capture.ExcludeGlobs = []string{"["} }},
		{"foreign verifier", false, func(mod *modules.Module, _ *validationmatrix.Scenario) {
			mod.Verify[0].Path = `%APPDATA%\mGBA\foreign.ini`
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod := clone(t)
			candidate := scenario
			test.mutate(mod, &candidate)
			if !test.mutable {
				raw, err := json.Marshal(mod)
				if err != nil {
					t.Fatal(err)
				}
				mod, err = modules.ParseModuleJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
			}
			if plan, failure := compileCaptureContract(mod, candidate); failure == nil {
				t.Fatalf("foreign capture contract compiled: %+v", plan)
			}
		})
	}
}

func TestCompileSelectionRoutesReviewedCaptureContractExplicitly(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	selected, failure := compileSelection(Request{
		EnginePath: engine, RepoRoot: repo, ModuleID: "apps.mgba", ScenarioID: "reviewed-capture-v1",
		ResultPath: filepath.Join(t.TempDir(), "result.json"),
	}, time.Now().UTC())
	if failure != nil {
		t.Fatalf("compile selection: %+v", failure)
	}
	if selected.capturePlan == nil || selected.capturePlan.ModuleID != "apps.mgba" {
		t.Fatalf("capture plan = %+v", selected.capturePlan)
	}
}
