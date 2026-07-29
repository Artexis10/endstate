// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestCaptureContractAssertionsBindSyntheticOneFileModule(t *testing.T) {
	runtime, entries := syntheticCaptureContractArtifactFixture(t)
	artifact := writeV2ArtifactZip(t, runtime.Root, "synthetic-capture-contract.zip", entries)
	if _, failure := inspectCaptureContractArtifact(runtime, artifact); failure != nil {
		t.Fatalf("synthetic capture artifact: %+v", failure)
	}
	raw, events := captureContractEvidenceFixture(t, runtime)
	if failure := validateCaptureContractCommandEvidence(raw, events, runtime, "captured.zip"); failure != nil {
		t.Fatalf("synthetic capture command evidence: %+v", failure)
	}
	optionalRaw, optionalEvents, optionalEntries := captureContractOptionalFixture(t, runtime)
	optionalArtifact := writeV2ArtifactZip(t, runtime.Root, "synthetic-optional-absent.zip", optionalEntries)
	optionalRaw, optionalEvents = retargetOptionalCaptureEvidence(t, optionalRaw, optionalEvents, filepath.Base(optionalArtifact))
	if failure := validateCaptureContractOptionalAbsentOutcome(optionalRaw, optionalEvents, runtime, optionalArtifact); failure != nil {
		t.Fatalf("synthetic optional-absence evidence: %+v", failure)
	}
}

func syntheticCaptureContractArtifactFixture(t *testing.T) (*scenarioRuntime, map[string][]byte) {
	t.Helper()
	mod, err := modules.ParseModuleJSON([]byte(`{
		"id":"apps.fixture-capture","displayName":"Fixture Capture","sensitivity":"none",
		"matches":{"winget":["Vendor.FixtureCapture"]},
		"verify":[{"type":"file-exists","path":"%APPDATA%\\Fixture Capture\\Settings.TOML"}],
		"capture":{"files":[{"source":"%APPDATA%\\Fixture Capture\\Settings.TOML","dest":"apps/fixture-capture/Settings.TOML","optional":true}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	scenario := validationmatrix.Scenario{
		ID: "synthetic-capture-contract", Mode: validationmatrix.ScenarioCaptureContract, Fixture: validationmatrix.Fixture{Type: validationmatrix.FixtureAuto},
		Review: &validationmatrix.OneWayReview{Decision: "approved-one-way"},
	}
	plan, failure := compileCaptureContract(mod, scenario)
	if failure != nil {
		t.Fatalf("compile synthetic capture contract: %+v", failure)
	}
	validationContext := fixtureValidationContext(t, plan.ModuleID, plan.ScenarioID)
	plan.context, plan.root = validationContext, validationContext.Root()
	resolved, err := validationContext.ResolveHostPath(plan.Targets[0].AuthoredSource, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	plan.Targets[0].Resolved = resolved
	runtime := &scenarioRuntime{Module: mod, Scenario: scenario, CapturePlan: plan, Root: plan.root, Inventory: plan.Inventory}
	moduleName := strings.TrimPrefix(mod.ID, "apps.")
	payloadPath := v1ArtifactPayloadPath(mod.ID, plan.Targets[0].Destination)
	captured := manifest.Manifest{
		Version: 1, Name: "captured", Captured: time.Now().UTC().Format(time.RFC3339),
		Apps:   []manifest.App{{ID: plan.Inventory.AppID, DisplayName: plan.Inventory.DisplayName, Refs: map[string]string{"windows": plan.Inventory.Ref}}},
		Verify: []manifest.VerifyEntry{{Type: "file-exists", Path: plan.Verifiers[0].Path}}, ConfigModules: []string{plan.ModuleID},
	}
	metadata := bundle.BundleMetadata{
		SchemaVersion: "1.0", CapturedAt: time.Now().UTC().Format(time.RFC3339), MachineName: "validation-host", EndstateVersion: "test", OS: "windows",
		ConfigModulesIncluded: []string{moduleName}, ConfigModulesSkipped: []string{}, CaptureWarnings: []string{},
	}
	return runtime, map[string][]byte{
		"configs/": {}, "configs/" + moduleName + "/": {}, "manifest.jsonc": mustV2JSON(t, captured), "metadata.json": mustV2JSON(t, metadata),
		payloadPath: append([]byte(nil), plan.Targets[0].Content...),
	}
}
