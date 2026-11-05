// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestFlatPlatformCaptureContractBindsSourceAndKeepsCaptureOnlyProof(t *testing.T) {
	repo := productionLiveRepoRoot(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.ripgrep"]
	scenario := catalog.Records[mod.ID].Synthetic.Scenarios[0]
	plan, failure := compileCaptureContractAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	enginePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runtime, cleanup, failure, err := prepareScenarioRuntime(&selection{request: Request{RepoRoot: repo, EnginePath: enginePath}, module: mod, record: catalog.Records[mod.ID], scenario: scenario, capturePlan: plan})
	if err != nil || failure != nil {
		t.Fatalf("bind flat platform capture runtime: %v, %+v", err, failure)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	pinned := runtime.CapturePlan.generationPlan
	if pinned == nil || pinned.Instance.ID == "" || pinned.Instance.Version.Raw != "" || pinned.Instance.Evidence.Platform != "windows" {
		t.Fatalf("bound generation invented version or lost platform: %+v", pinned)
	}
	expected, failure := expectedCaptureContractGeneration(runtime)
	if failure != nil {
		t.Fatal(failure)
	}
	captured := manifest.Manifest{Version: 2, Name: "captured", Captured: time.Now().UTC().Format(time.RFC3339), Apps: []manifest.App{{ID: runtime.Inventory.AppID, DisplayName: runtime.Inventory.DisplayName, Refs: map[string]string{"windows": runtime.Inventory.Ref}}}, ConfigCaptures: []manifest.ConfigCapture{expected}}
	for _, verifier := range plan.Verifiers {
		captured.Verify = append(captured.Verify, manifest.VerifyEntry{Type: verifier.Type, Command: verifier.Command, Path: verifier.Path})
	}
	metadata := bundle.BundleMetadata{SchemaVersion: "2.0", ManifestVersion: 2, CapturedAt: captured.Captured, MachineName: "validation-host", EndstateVersion: "test", OS: "windows", ConfigCapturesIncluded: []string{expected.CaptureID}, ConfigModulesIncluded: []string{}, ConfigModulesSkipped: []string{}, CaptureWarnings: []string{}}
	entries := map[string][]byte{"manifest.jsonc": mustV2JSON(t, captured), "metadata.json": mustV2JSON(t, metadata), expected.CaptureModule.SnapshotPath: mod.CanonicalSnapshot()}
	for _, target := range runtime.CapturePlan.Targets {
		entries[expected.PayloadRoot+"/"+target.Destination] = target.Content
	}
	artifact := writeV2ArtifactZip(t, runtime.Root, "platform.zip", entries)
	evidence, failure := inspectCaptureContractArtifact(runtime, artifact)
	if failure != nil {
		t.Fatalf("exact schema-v3 capture artifact: %+v", failure)
	}
	if evidence.AssertionCounts[validationmatrix.AssertionContent] != 2 || evidence.AssertionCounts[validationmatrix.AssertionValidation] != 0 || evidence.AssertionCounts[validationmatrix.AssertionGeneration] != 0 {
		t.Fatalf("capture-only evidence claimed generation proof: %v", evidence.AssertionCounts)
	}
	proof, failure := evaluateAssertions(scenario, evidence.AssertionCounts, OperationCounts{Executed: 2}, []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract})
	if failure != nil || !reflect.DeepEqual(proof, []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract}) {
		t.Fatalf("capture-only proof: %v, %+v", proof, failure)
	}
	for _, attack := range []struct {
		name   string
		mutate func(map[string][]byte)
	}{
		{"platform", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) {
				value["configCaptures"].([]any)[0].(map[string]any)["sourcePlatform"] = "linux"
			})
		}},
		{"fingerprint", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) {
				value["configCaptures"].([]any)[0].(map[string]any)["sourceGenerationFingerprint"] = "changed"
			})
		}},
		{"schema downgrade", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) {
				value["configCaptures"].([]any)[0].(map[string]any)["captureModule"].(map[string]any)["schemaVersion"] = 2
			})
		}},
		{"snapshot", func(values map[string][]byte) {
			values[expected.CaptureModule.SnapshotPath] = []byte(`{"moduleSchemaVersion":2}`)
		}},
		{"payload", func(values map[string][]byte) {
			values[expected.PayloadRoot+"/"+runtime.CapturePlan.Targets[0].Destination] = []byte("foreign")
		}},
	} {
		t.Run(attack.name, func(t *testing.T) {
			candidate := cloneV2Entries(entries)
			attack.mutate(candidate)
			path := writeV2ArtifactZip(t, runtime.Root, "attack-"+filepath.Base(artifact), candidate)
			if _, failure := inspectCaptureContractArtifact(runtime, path); failure == nil {
				t.Fatal("forged schema-v3 source passed capture-contract")
			}
		})
	}

	raw, events := captureContractEvidenceFixture(t, runtime)
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	module := data["configModules"].([]any)[0].(map[string]any)
	module["paths"], module["filesCaptured"] = captureContractPayloadPaths(runtime), len(runtime.CapturePlan.Targets)
	data["bundleSchemaVersion"], data["manifestVersion"] = "2.0", 2
	data["configCapture"] = map[string]any{
		"modules":    []any{map[string]any{"id": mod.ID, "displayName": mod.DisplayName, "entries": len(runtime.CapturePlan.Restores), "files": captureContractDestinations(runtime)}},
		"configSets": []any{map[string]any{"captureId": expected.CaptureID, "moduleId": mod.ID, "configSetId": expected.ConfigSetID, "displayName": expected.ConfigSetID, "sourcePlatform": expected.SourcePlatform, "sourceInstance": expected.SourceInstance, "sourceGeneration": expected.SourceGeneration, "sourceGenerationFingerprint": expected.SourceGenerationFingerprint, "captureModuleRevision": mod.Revision, "filesCaptured": 2, "status": "captured", "reason": nil}},
		"counts":     map[string]int{"total": 1, "captured": 1, "skipped": 0, "failed": 0}, "diagnostics": []any{},
	}
	if failure := validateCaptureContractCommandEvidence(mustV2JSON(t, data), events, runtime, "captured.zip"); failure != nil {
		t.Fatalf("schema-v3 capture envelope: %+v", failure)
	}
	delete(data, "bundleSchemaVersion")
	delete(data, "manifestVersion")
	module["paths"], module["filesCaptured"], module["status"] = []string{}, 0, "skipped"
	data["configsIncluded"], data["configsSkipped"], data["captureWarnings"] = []string{}, []string{captureContractModuleName(runtime)}, captureContractOptionalWarnings(runtime)
	summary := data["configCapture"].(map[string]any)
	row := summary["configSets"].([]any)[0].(map[string]any)
	row["filesCaptured"], row["status"], row["reason"] = 0, "skipped", bundle.CaptureBundleDiagnosticEmpty
	summary["counts"] = map[string]int{"total": 1, "captured": 0, "skipped": 1, "failed": 0}
	summary["diagnostics"] = []bundle.CaptureBundleDiagnostic{{CaptureID: expected.CaptureID, ModuleID: mod.ID, ConfigSetID: expected.ConfigSetID, InstanceID: expected.SourceInstance.ID, Status: "skipped", Code: bundle.CaptureBundleDiagnosticEmpty, Detail: "generation capture produced no regular files"}}
	captured.Version, captured.ConfigCaptures, captured.Verify = 1, nil, nil
	metadata.SchemaVersion, metadata.ManifestVersion, metadata.ConfigCapturesIncluded, metadata.CaptureWarnings = "1.0", 0, nil, captureContractOptionalWarnings(runtime)
	optional := writeV2ArtifactZip(t, runtime.Root, "optional-platform.zip", map[string][]byte{"manifest.jsonc": mustV2JSON(t, captured), "metadata.json": mustV2JSON(t, metadata)})
	optionalRaw, optionalEvents := retargetOptionalCaptureEvidence(t, mustV2JSON(t, data), events, filepath.Base(optional))
	if failure := validateCaptureContractOptionalAbsentOutcome(optionalRaw, optionalEvents, runtime, optional); failure != nil {
		t.Fatalf("schema-v3 optional absence: %+v", failure)
	}
	row["filesCaptured"] = 1
	forged, _ := retargetOptionalCaptureEvidence(t, mustV2JSON(t, data), events, filepath.Base(optional))
	if failure := validateCaptureContractOptionalAbsentOutcome(forged, optionalEvents, runtime, optional); failure == nil {
		t.Fatal("optional absence minted captured settings evidence")
	}
}

func TestCaptureContractRejectsAuthoredConfigMasqueradingAsFlatPlatform(t *testing.T) {
	raw := []byte(`{"moduleSchemaVersion":3,"id":"apps.fixture","displayName":"Fixture","sensitivity":"low","platforms":{"windows":{"realization":"endstate-restore","matches":{"winget":["Vendor.Fixture"]},"config":{"instanceDetectors":[{"id":"default","type":"path","glob":"${home}"}],"sets":[{"id":"default","generations":[{"id":"g1","order":1}]}]}}}}`)
	authored, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	projected := modules.ProjectModuleForPlatform(authored, "windows")
	if projected.UsesImplicitConfigGeneration() {
		t.Fatal("authored config was reclassified by reserved IDs")
	}
	scenario := validationmatrix.Scenario{Mode: validationmatrix.ScenarioCaptureContract, Review: &validationmatrix.OneWayReview{Decision: "approved-one-way"}}
	if _, failure := compileCaptureContract(projected, scenario); failure == nil {
		t.Fatal("explicit generation bypassed its validation proof")
	}
}
