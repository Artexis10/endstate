// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestRunRevertGenerationOnlyHistoryDoesNotRequireLegacyJournal(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	target := filepath.Join(root, "settings.json")
	prior := []byte("generation-only-prior")
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := configrestore.BeginLive(context.Background(), stateRoot, "generation-only-restore", nil)
	if err != nil {
		t.Fatal(err)
	}
	transactionRoot, err := guard.CreateTransactionRoot("generation-only-capture")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := configrestore.PrepareSnapshots(context.Background(), configrestore.SnapshotRequest{
		Set: &configrestore.MaterializedSet{Actions: []configrestore.Action{{
			Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := configRestoreTestLineage("generation-only-capture")
	lineage.RunID = "generation-only-restore"
	intent, err := configrestore.PersistJournalIntent(context.Background(), configrestore.JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: lineage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configrestore.ExecuteConfigSetTransaction(context.Background(), configrestore.TransactionRequest{Prepared: prepared, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	originalRoot := resolveRepoRootFn
	resolveRepoRootFn = func() string { return root }
	t.Cleanup(func() { resolveRepoRootFn = originalRoot })

	got, envErr := RunRevert(RevertFlags{})
	if envErr != nil {
		t.Fatalf("generation-only RunRevert: %+v", envErr)
	}
	if result := got.(*RevertData); len(result.Results) != 1 || result.Results[0].Target != target {
		t.Fatalf("generation-only revert result = %+v", result)
	}
	if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, prior) {
		t.Fatalf("generation-only reverted bytes = %q, %v", data, err)
	}
}

func TestConfigGenerationCaptureMigrationRestoreRevertRoundTrip(t *testing.T) {
	root := t.TempDir()
	captureRoot := filepath.Join(root, "capture-source")
	targetRoot := filepath.Join(root, "target")
	if err := os.MkdirAll(captureRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	capturedBytes := []byte("{\n  \"schema\": 1,\n  \"theme\": \"roundtrip-dark\"\n}\n")
	if err := os.WriteFile(filepath.Join(captureRoot, "settings.json"), capturedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_ROUNDTRIP_CAPTURE", captureRoot)
	t.Setenv("ENDSTATE_ROUNDTRIP_TARGET", targetRoot)

	moduleDir := filepath.Join(root, "modules", "apps", "roundtrip")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleDeclaration := map[string]any{
		"moduleSchemaVersion": 2,
		"id":                  "apps.roundtrip",
		"displayName":         "Roundtrip Fixture",
		"sensitivity":         "low",
		"matches":             map[string]any{"winget": []string{"Vendor.RoundTrip"}},
		"config": map[string]any{
			"instanceDetectors": []any{map[string]any{"id": "installed", "type": "package"}},
			"sets": []any{map[string]any{
				"id": "preferences", "displayName": "Preferences",
				"generations": []any{
					map[string]any{
						"id": "g1", "order": 1,
						"matches": []any{map[string]any{"versionRange": ">=1 <2"}},
						"capture": map[string]any{"files": []any{map[string]any{
							"source": captureTestEnvPath("ENDSTATE_ROUNDTRIP_CAPTURE", "settings.json"), "dest": "settings.json",
						}}},
						"restore": []any{map[string]any{
							"type": "copy", "source": "settings.json", "target": captureTestEnvPath("ENDSTATE_ROUNDTRIP_TARGET", "settings.json"), "backup": true,
						}},
						"validate": []any{map[string]any{"type": "json-path-exists", "path": "settings.json", "jsonPath": "$.schema"}},
					},
					map[string]any{
						"id": "g2", "order": 2,
						"matches": []any{map[string]any{"versionRange": ">=2 <3"}},
						"restore": []any{map[string]any{
							"type": "copy", "source": "settings.json", "target": captureTestEnvPath("ENDSTATE_ROUNDTRIP_TARGET", "settings.json"), "backup": true,
						}},
						"validate": []any{map[string]any{"type": "json-path-exists", "path": "settings.json", "jsonPath": "$.migrated"}},
					},
				},
				"migrations": []any{map[string]any{
					"from": "g1", "to": "g2",
					"operations": []any{map[string]any{"type": "json-set", "path": "settings.json", "jsonPath": "$.migrated", "value": true}},
					"validate":   []any{map[string]any{"type": "json-path-exists", "path": "settings.json", "jsonPath": "$.migrated"}},
				}},
			}},
		},
	}
	moduleBytes, err := json.MarshalIndent(moduleDeclaration, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "module.jsonc"), moduleBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	parsedCatalog, diagnostics, err := modules.GetCatalogWithDiagnostics(root)
	if err != nil || len(diagnostics) != 0 || len(parsedCatalog) != 1 {
		t.Fatalf("parse roundtrip module catalog: modules=%d diagnostics=%+v err=%v", len(parsedCatalog), diagnostics, err)
	}
	parsedModule := parsedCatalog["apps.roundtrip"]
	if parsedModule == nil || parsedModule.Revision == "" || len(parsedModule.Config.Sets[0].Generations) != 2 ||
		parsedModule.Config.Sets[0].Generations[0].Fingerprint == "" || parsedModule.Config.Sets[0].Generations[1].Fingerprint == "" {
		t.Fatalf("parsed immutable module identities = %+v", parsedModule)
	}
	if parsedModule.Config.Sets[0].Generations[0].Fingerprint == parsedModule.Config.Sets[0].Generations[1].Fingerprint {
		t.Fatalf("distinct generation definitions share fingerprint %q", parsedModule.Config.Sets[0].Generations[0].Fingerprint)
	}
	preflight := planCaptureConfig(parsedCatalog, []manifest.App{{
		ID: "vendor-roundtrip", Refs: map[string]string{"windows": "Vendor.RoundTrip"},
		Installed: true, InstalledVersion: "1.4.0", Backend: "winget",
	}}, nil)
	if len(preflight.GenerationPlans) != 1 {
		t.Fatalf("roundtrip generation capture preflight = %+v", preflight)
	}

	originalRoot := resolveRepoRootFn
	originalGOOS := captureGOOSFn
	originalRealizer := newRealizerFn
	originalEnumerator := resolveCaptureEnumeratorFn
	originalCaptureCatalog := loadCaptureModuleCatalogFn
	resolveRepoRootFn = func() string { return root }
	captureGOOSFn = func() string { return "windows" }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	// TestMain installs an empty default catalog for unrelated capture tests;
	// this proof deliberately restores the production on-disk catalog loader.
	loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
	resolveCaptureEnumeratorFn = func(name string, _ bool) (driver.InstalledEnumerator, error) {
		if name != "winget" {
			return nil, errors.New("unexpected package driver " + name)
		}
		return fakeInstalledEnumerator{packages: []driver.InstalledPackage{{
			Ref: "Vendor.RoundTrip", DisplayName: "Roundtrip Fixture", Version: "1.4.0",
		}}}, nil
	}
	t.Cleanup(func() {
		resolveRepoRootFn = originalRoot
		captureGOOSFn = originalGOOS
		newRealizerFn = originalRealizer
		resolveCaptureEnumeratorFn = originalEnumerator
		loadCaptureModuleCatalogFn = originalCaptureCatalog
	})

	rawCapture, captureErr := RunCapture(CaptureFlags{
		Out: filepath.Join(root, "roundtrip.jsonc"), Drivers: []string{"winget"},
	})
	if captureErr != nil {
		t.Fatalf("RunCapture: %+v", captureErr)
	}
	captureResult := rawCapture.(*CaptureResult)
	if captureResult.OutputFormat != "zip" || captureResult.BundleSchemaVersion != "2.0" || captureResult.ManifestVersion != 2 {
		t.Fatalf("generation-aware capture result = %+v", captureResult)
	}
	if len(captureResult.ConfigCapture.ConfigSets) != 1 || captureResult.ConfigCapture.ConfigSets[0].SourceGeneration != "g1" {
		t.Fatalf("generation-aware capture provenance = %+v", captureResult.ConfigCapture)
	}
	bundleHash := roundTripFileSHA256(t, captureResult.OutputPath)

	extractedManifestPath, err := bundle.ExtractBundle(captureResult.OutputPath)
	if err != nil {
		t.Fatalf("extract captured bundle: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(extractedManifestPath)) })
	extractedManifest, err := manifest.LoadManifest(extractedManifestPath)
	if err != nil {
		t.Fatalf("load extracted manifest: %v", err)
	}
	if extractedManifest.Version != 2 || len(extractedManifest.ConfigCaptures) != 1 || extractedManifest.ConfigCaptures[0].SourceGeneration != "g1" {
		t.Fatalf("captured v2 manifest = %+v", extractedManifest)
	}
	if afterExtract := roundTripFileSHA256(t, captureResult.OutputPath); afterExtract != bundleHash {
		t.Fatalf("bundle hash changed during extraction: before %s after %s", bundleHash, afterExtract)
	}

	captureRow := captureResult.ConfigCapture.ConfigSets[0]
	if captureRow.CaptureModuleRevision != parsedModule.Revision ||
		captureRow.SourceGenerationFingerprint != parsedModule.Config.Sets[0].Generations[0].Fingerprint {
		t.Fatalf("capture immutable identities = %+v, module revision %q", captureRow, parsedModule.Revision)
	}
	preRunBytes := []byte("{\r\n  \"distinctivePreRun\": \"exact-bytes-must-return\",\r\n  \"schema\": 200\r\n}\r\n")
	targetPath := filepath.Join(targetRoot, "settings.json")
	if err := os.WriteFile(targetPath, preRunBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	targetDriver := &laneTestDriver{
		name: "winget", installed: map[string]bool{"Vendor.RoundTrip": true},
		versions: map[string]string{"Vendor.RoundTrip": "2.4.0"},
	}
	var restoreData *RestoreData
	var restoreErr *envelope.Error
	var restoreEvents string
	withNamedDriverLanes(t, map[string]driver.Driver{"winget": targetDriver}, nil, func() {
		restoreEvents = captureStderr(t, func() {
			rawRestore, runErr := RunRestore(RestoreFlags{
				Manifest: extractedManifestPath, EnableRestore: true, Events: "jsonl",
			})
			restoreErr = runErr
			if rawRestore != nil {
				restoreData = rawRestore.(*RestoreData)
			}
		})
	})
	if restoreErr != nil {
		t.Fatalf("RunRestore: %+v\nevents:\n%s", restoreErr, restoreEvents)
	}
	if targetDriver.batchCalls != 2 || len(targetDriver.detectCalls) != 0 {
		t.Fatalf("restore package evidence calls: batch=%d detect=%v", targetDriver.batchCalls, targetDriver.detectCalls)
	}
	if restoreData == nil || restoreData.ConfigResultFields == nil || len(restoreData.ConfigResolutions) != 1 || len(restoreData.RestoreItems) != 1 {
		t.Fatalf("restore command data = %+v", restoreData)
	}
	resolution := restoreData.ConfigResolutions[0]
	if resolution.SourceGeneration != "g1" || resolution.TargetGeneration != "g2" ||
		resolution.Resolution != planner.ResolutionMigrate || resolution.Status != planner.StatusRestored ||
		!equalStrings(resolution.MigrationPath, []string{"g1", "g2"}) {
		t.Fatalf("roundtrip resolution = %+v", resolution)
	}
	resolutionJSON, err := json.Marshal(resolution)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(resolutionJSON), bytes.ToLower([]byte(targetRoot))) {
		t.Fatalf("public resolution exposed host root %q: %s", targetRoot, resolutionJSON)
	}

	restoredBytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	var restoredDocument map[string]any
	if err := json.Unmarshal(restoredBytes, &restoredDocument); err != nil {
		t.Fatalf("restored JSON = %q: %v", restoredBytes, err)
	}
	if restoredDocument["schema"] != float64(1) || restoredDocument["theme"] != "roundtrip-dark" || restoredDocument["migrated"] != true || bytes.Equal(restoredBytes, preRunBytes) {
		t.Fatalf("migrated target bytes = %q", restoredBytes)
	}

	restoreEnvelope := envelope.NewSuccess("restore", "restore-roundtrip-envelope", "1.0", "test", restoreData)
	envelopeBytes, err := envelope.Marshal(restoreEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsAny(envelopeBytes, "\r\n") {
		t.Fatalf("compact success envelope = %q", envelopeBytes)
	}
	roundTripAssertRestoreEnvelope(t, envelopeBytes, captureRow, parsedModule, targetRoot)

	roundTripAssertRestoreEvents(t, restoreEvents, captureRow.CaptureID, targetRoot)
	roundTripAssertCommittedJournal(t, filepath.Join(root, "state"), captureRow, parsedModule, resolution.TargetInstanceID)
	if afterRestore := roundTripFileSHA256(t, captureResult.OutputPath); afterRestore != bundleHash {
		t.Fatalf("bundle hash changed after restore: before %s after %s", bundleHash, afterRestore)
	}

	var revertData *RevertData
	var revertErr *envelope.Error
	revertEvents := captureStderr(t, func() {
		rawRevert, runErr := RunRevert(RevertFlags{Events: "jsonl"})
		revertErr = runErr
		if rawRevert != nil {
			revertData = rawRevert.(*RevertData)
		}
	})
	if revertErr != nil {
		t.Fatalf("RunRevert: %+v\nevents:\n%s", revertErr, revertEvents)
	}
	if revertData == nil || len(revertData.Results) != 1 || revertData.Results[0].Target != targetPath || revertData.Results[0].Action != "reverted" {
		t.Fatalf("revert terminal results = %+v", revertData)
	}
	if revertedBytes, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(revertedBytes, preRunBytes) {
		t.Fatalf("reverted bytes = %q, want exact %q (err=%v)", revertedBytes, preRunBytes, err)
	}
	roundTripAssertRevertEvents(t, revertEvents, targetPath)
	roundTripAssertRevertMarker(t, filepath.Join(root, "state"))
	if afterRevert := roundTripFileSHA256(t, captureResult.OutputPath); afterRevert != bundleHash {
		t.Fatalf("bundle hash changed after revert: before %s after %s", bundleHash, afterRevert)
	}
	if _, secondRevertErr := RunRevert(RevertFlags{}); secondRevertErr == nil {
		t.Fatal("generation journal was not consumed after one revert")
	}
	if afterSecondRevert, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(afterSecondRevert, preRunBytes) {
		t.Fatalf("one-shot revert changed target = %q, %v", afterSecondRevert, err)
	}
}

func roundTripFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func roundTripParseJSONL(t *testing.T, raw string) []map[string]any {
	t.Helper()
	if raw == "" || strings.HasPrefix(raw, "\n") || strings.Contains(raw, "\n\n") {
		t.Fatalf("stderr is not pure non-empty JSONL: %q", raw)
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	events := make([]map[string]any, len(lines))
	for index, line := range lines {
		if strings.TrimSpace(line) != line || line == "" {
			t.Fatalf("stderr line %d is not compact JSON: %q", index, line)
		}
		if err := json.Unmarshal([]byte(line), &events[index]); err != nil {
			t.Fatalf("stderr line %d is not JSON: %v\n%s", index, err, line)
		}
		if events[index]["version"] != float64(1) || events[index]["runId"] == "" || events[index]["timestamp"] == "" || events[index]["event"] == "" {
			t.Fatalf("stderr line %d lacks event contract fields: %#v", index, events[index])
		}
	}
	return events
}

func roundTripAssertRestoreEvents(t *testing.T, raw, captureID, hostRoot string) {
	t.Helper()
	events := roundTripParseJSONL(t, raw)
	type eventStep struct {
		event  string
		stage  string
		status string
	}
	want := []eventStep{
		{event: "phase"},
		{event: "config-migration", stage: "staging", status: "started"},
		{event: "config-migration", stage: "staging", status: "completed"},
		{event: "config-migration", stage: "edge", status: "started"},
		{event: "config-migration", stage: "edge", status: "completed"},
		{event: "config-migration", stage: "validation", status: "started"},
		{event: "config-migration", stage: "validation", status: "completed"},
		{event: "config-migration", stage: "validation", status: "started"},
		{event: "config-migration", stage: "validation", status: "completed"},
		{event: "config-resolution"},
		{event: "restore-item", status: "restoring"},
		{event: "config-migration", stage: "commit", status: "started"},
		{event: "config-migration", stage: "commit", status: "completed"},
		{event: "config-migration", stage: "validation", status: "started"},
		{event: "config-migration", stage: "validation", status: "completed"},
		{event: "restore-item", status: "restored"},
		{event: "summary"},
	}
	if len(events) != len(want) {
		t.Fatalf("restore event count = %d, want %d\n%s", len(events), len(want), raw)
	}
	for index, step := range want {
		event := events[index]
		if event["event"] != step.event || step.stage != "" && event["stage"] != step.stage || step.status != "" && event["status"] != step.status {
			t.Fatalf("restore event %d = %#v, want event=%q stage=%q status=%q\n%s", index, event, step.event, step.stage, step.status, raw)
		}
		if event["status"] == "failed" || event["stage"] == "rollback" {
			t.Fatalf("successful restore emitted failure/rollback event at %d: %#v", index, event)
		}
		if step.event == "config-migration" {
			if event["captureId"] != captureID || event["configSetId"] != "preferences" {
				t.Fatalf("migration identity at %d = %#v", index, event)
			}
			if step.stage == "edge" && (event["fromGeneration"] != "g1" || event["toGeneration"] != "g2") {
				t.Fatalf("migration edge at %d = %#v", index, event)
			}
			encoded, _ := json.Marshal(event)
			if bytes.Contains(bytes.ToLower(encoded), bytes.ToLower([]byte(hostRoot))) {
				t.Fatalf("portable migration event exposed host root: %s", encoded)
			}
		}
	}
	if events[0]["phase"] != "restore" || events[len(events)-1]["phase"] != "restore" {
		t.Fatalf("restore event framing = %#v", events)
	}
	resolution := events[9]
	if resolution["captureId"] != captureID || resolution["configSetId"] != "preferences" || resolution["sourceGeneration"] != "g1" ||
		resolution["targetGeneration"] != "g2" || resolution["resolution"] != "migrate" {
		t.Fatalf("config-resolution event = %#v", resolution)
	}
	path, ok := resolution["migrationPath"].([]any)
	if !ok || len(path) != 2 || path[0] != "g1" || path[1] != "g2" {
		t.Fatalf("config-resolution migration path = %#v", resolution["migrationPath"])
	}
	encodedResolution, _ := json.Marshal(resolution)
	if bytes.Contains(bytes.ToLower(encodedResolution), bytes.ToLower([]byte(hostRoot))) {
		t.Fatalf("portable resolution event exposed host root: %s", encodedResolution)
	}
	if events[10]["id"] == "" || events[10]["id"] != events[15]["id"] || events[16]["success"] != float64(1) {
		t.Fatalf("restore item/summary terminal identity = %#v %#v %#v", events[10], events[15], events[16])
	}
}

func roundTripAssertRestoreEnvelope(t *testing.T, encoded []byte, capture CaptureConfigSetResult, module *modules.Module, hostRoot string) {
	t.Helper()
	var wire struct {
		SchemaVersion string          `json:"schemaVersion"`
		CLIVersion    string          `json:"cliVersion"`
		Command       string          `json:"command"`
		RunID         string          `json:"runId"`
		TimestampUTC  string          `json:"timestampUtc"`
		Success       bool            `json:"success"`
		Data          RestoreData     `json:"data"`
		Error         json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode restore envelope: %v", err)
	}
	if wire.SchemaVersion != "1.0" || wire.CLIVersion != "test" || wire.Command != "restore" ||
		wire.RunID != "restore-roundtrip-envelope" || wire.TimestampUTC == "" || !wire.Success || string(wire.Error) != "null" {
		t.Fatalf("restore envelope metadata = %+v error=%s", wire, wire.Error)
	}
	if wire.Data.ConfigResultFields == nil || len(wire.Data.ConfigResolutions) != 1 || len(wire.Data.RestoreItems) != 1 {
		t.Fatalf("serialized restore data = %+v", wire.Data)
	}
	resolution := wire.Data.ConfigResolutions[0]
	if resolution.CaptureID != capture.CaptureID || resolution.ModuleID != capture.ModuleID || resolution.ConfigSetID != capture.ConfigSetID ||
		resolution.SourceGeneration != "g1" || resolution.TargetGeneration != "g2" || resolution.Resolution != planner.ResolutionMigrate ||
		resolution.Status != planner.StatusRestored || !equalStrings(resolution.MigrationPath, []string{"g1", "g2"}) ||
		resolution.SourceGenerationFingerprint != capture.SourceGenerationFingerprint ||
		resolution.CaptureModuleRevision != capture.CaptureModuleRevision || resolution.RestoreModuleRevision != module.Revision {
		t.Fatalf("serialized config resolution = %+v", resolution)
	}
	resolutionJSON, _ := json.Marshal(resolution)
	if bytes.Contains(bytes.ToLower(resolutionJSON), bytes.ToLower([]byte(hostRoot))) {
		t.Fatalf("serialized config resolution exposed host root %q: %s", hostRoot, resolutionJSON)
	}
	summary := wire.Data.ConfigResolutionSummary
	if summary.Total != 1 || summary.Migrate != 1 || summary.Selected != 1 || summary.Direct != 0 || summary.Incompatible != 0 ||
		summary.Unknown != 0 || summary.LegacyUnverified != 0 || summary.Skipped != 0 || summary.Failed != 0 {
		t.Fatalf("serialized config resolution summary = %+v", summary)
	}
	item := wire.Data.RestoreItems[0]
	if item.Status != "restored" || item.CaptureID != capture.CaptureID || item.ConfigSetID != capture.ConfigSetID ||
		item.SourceGeneration != "g1" || item.TargetGeneration != "g2" || item.Target == "" {
		t.Fatalf("serialized restore item = %+v", item)
	}
}

func roundTripAssertRevertEvents(t *testing.T, raw, target string) {
	t.Helper()
	events := roundTripParseJSONL(t, raw)
	if len(events) != 3 || events[0]["event"] != "phase" || events[0]["phase"] != "restore" ||
		events[1]["event"] != "item" || events[1]["id"] != target || events[1]["status"] != "installed" ||
		events[2]["event"] != "summary" || events[2]["phase"] != "restore" || events[2]["success"] != float64(1) {
		t.Fatalf("ordered revert JSONL = %#v\n%s", events, raw)
	}
}

func roundTripJournalDocuments(t *testing.T, stateRoot string) []map[string]any {
	t.Helper()
	var documents []map[string]any
	err := filepath.Walk(stateRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var document map[string]any
		if json.Unmarshal(data, &document) == nil {
			document["_path"] = path
			documents = append(documents, document)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read real journal artifacts: %v", err)
	}
	return documents
}

func roundTripAssertCommittedJournal(t *testing.T, stateRoot string, captureRow CaptureConfigSetResult, module *modules.Module, targetInstanceID string) {
	t.Helper()
	documents := roundTripJournalDocuments(t, stateRoot)
	var intent, committed map[string]any
	for _, document := range documents {
		switch document["format"] {
		case "endstate.config-restore-intent":
			intent = document
		case "endstate.config-restore-marker":
			if document["state"] == "committed" {
				committed = document
			}
		}
	}
	if intent == nil || committed == nil || intent["state"] != "pending" || committed["validationStatus"] != "passed" || committed["intentDigest"] != intent["intentDigest"] {
		t.Fatalf("real committed journal artifacts = %#v", documents)
	}
	lineage, ok := intent["lineage"].(map[string]any)
	if !ok {
		t.Fatalf("journal lineage = %#v", intent)
	}
	path, ok := lineage["migrationPath"].([]any)
	if !ok || len(path) != 2 || path[0] != "g1" || path[1] != "g2" ||
		lineage["captureId"] != captureRow.CaptureID || lineage["moduleId"] != "apps.roundtrip" || lineage["configSetId"] != "preferences" ||
		targetInstanceID == "" || lineage["targetInstanceId"] != targetInstanceID ||
		lineage["sourceGeneration"] != "g1" || lineage["targetGeneration"] != "g2" ||
		lineage["sourceGenerationFingerprint"] != module.Config.Sets[0].Generations[0].Fingerprint ||
		lineage["captureModuleRevision"] != captureRow.CaptureModuleRevision || lineage["restoreModuleRevision"] != module.Revision {
		t.Fatalf("committed journal lineage = %#v", lineage)
	}
}

func roundTripAssertRevertMarker(t *testing.T, stateRoot string) {
	t.Helper()
	for _, document := range roundTripJournalDocuments(t, stateRoot) {
		if document["format"] == "endstate.config-restore-member-revert" && document["kind"] == "generation" && document["revertDigest"] != "" {
			return
		}
	}
	t.Fatal("real generation journal has no durable one-shot revert marker")
}
