// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestCompileSelectionRejectsInvalidAuthorityBeforeExecution(t *testing.T) {
	repo, engine, result := writeSelectionRepository(t)
	valid := Request{EnginePath: engine, RepoRoot: repo, ModuleID: "apps.fixture", ScenarioID: "default-v1", ResultPath: result}

	tests := []struct {
		name   string
		mutate func(*Request)
		code   string
	}{
		{"relative engine", func(r *Request) { r.EnginePath = filepath.Base(engine) }, CodeInvalidEngine},
		{"engine directory", func(r *Request) { r.EnginePath = t.TempDir() }, CodeInvalidEngine},
		{"missing scenario", func(r *Request) { r.ScenarioID = "absent" }, CodeScenarioSelection},
		{"unsafe relative result", func(r *Request) { r.ResultPath = "result.json" }, CodeInvalidResultPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			tt.mutate(&request)
			if _, failure := compileSelection(request, time.Now().UTC()); failure == nil || failure.Code != tt.code {
				t.Fatalf("failure = %+v, want code %q", failure, tt.code)
			}
		})
	}
}

func TestSelectedValidationSidecarMustMatchCapturedRepositoryBoundary(t *testing.T) {
	repository := t.TempDir()
	relative := filepath.FromSlash("modules/apps/kubectl/validation.jsonc")
	path := filepath.Join(repository, relative)
	snapshot := []byte("selected validation authority")
	digest := sha256.Sum256(snapshot)
	boundary := boundaryTree{filepath.ToSlash(relative): {
		Kind: "file", Digest: digest, Size: int64(len(snapshot)),
	}}

	if err := validateSelectedSidecarBoundary(repository, path, snapshot, boundary); err != nil {
		t.Fatalf("valid sidecar boundary: %v", err)
	}
	changed := append([]byte(nil), snapshot...)
	changed[0] ^= 0xff
	if err := validateSelectedSidecarBoundary(repository, path, changed, boundary); err == nil {
		t.Fatal("changed selected sidecar snapshot passed repository boundary")
	}
	if err := validateSelectedSidecarBoundary(repository, filepath.Join(repository, "..", "validation.jsonc"), snapshot, boundary); err == nil {
		t.Fatal("sidecar path outside repository passed boundary")
	}
}

func TestSelectDeclaredScenarioRejectsDuplicateAndForeignCatalogObjects(t *testing.T) {
	repo, _, _ := writeSelectionRepository(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.fixture"]
	record := catalog.Records["apps.fixture"]

	if _, failure := selectDeclaredScenario(catalog, &modules.Module{ID: mod.ID}, record, "default-v1"); failure == nil || failure.Code != CodeScenarioSelection {
		t.Fatalf("foreign module failure = %+v", failure)
	}
	record.Synthetic.Scenarios = append(record.Synthetic.Scenarios, record.Synthetic.Scenarios[0])
	if _, failure := selectDeclaredScenario(catalog, mod, record, "default-v1"); failure == nil || failure.Code != CodeScenarioSelection {
		t.Fatalf("duplicate scenario failure = %+v", failure)
	}
}

func TestCompileFixtureRejectsUnsupportedOperation(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(strings.Replace(fixtureModuleJSON, `"type":"copy"`, `"type":"append"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	scenario := fixtureScenario()
	if _, failure := compileFixtureDefinitions(mod, scenario); failure == nil || failure.Code != CodeUnsupportedFixture {
		t.Fatalf("failure = %+v", failure)
	}

	t.Run("unmatched restore", func(t *testing.T) {
		mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
		if err != nil {
			t.Fatal(err)
		}
		mod.Restore = append(mod.Restore, modules.RestoreDef{
			Type: "copy", Source: "./payload/apps/fixture/extra.json", Target: "%APPDATA%\\Fixture\\extra.json", Backup: true,
		})
		if _, failure := compileFixtureDefinitions(mod, scenario); failure == nil || failure.Code != CodeUnsupportedFixture {
			t.Fatalf("failure = %+v", failure)
		}
	})

	t.Run("registry capture", func(t *testing.T) {
		mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
		if err != nil {
			t.Fatal(err)
		}
		mod.Capture.RegistryKeys = []modules.CaptureRegistryKey{{Key: `HKCU\Software\Fixture`, Dest: "fixture.reg"}}
		if _, failure := compileFixtureDefinitions(mod, scenario); failure == nil || failure.Code != CodeUnsupportedFixture {
			t.Fatalf("failure = %+v", failure)
		}
	})
}

func TestCompileFixtureAcceptsTypedFileMerges(t *testing.T) {
	for _, strategy := range []string{"merge-json", "merge-ini"} {
		t.Run(strategy, func(t *testing.T) {
			mod, err := modules.ParseModuleJSON([]byte(strings.Replace(fixtureModuleJSON, `"type":"copy"`, `"type":"`+strategy+`"`, 1)))
			if err != nil {
				t.Fatal(err)
			}
			definitions, failure := compileFixtureDefinitions(mod, fixtureScenario())
			if failure != nil {
				t.Fatal(failure)
			}
			if len(definitions.Entries) != 1 || definitions.Entries[0].Kind != fixtureKindFile || definitions.Entries[0].Strategy != strategy {
				t.Fatalf("definitions = %+v", definitions)
			}
		})
	}
}

func TestTrackedSchemaV1NonCopyFixturePartition(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repoRoot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mergeIDs := map[string]struct{}{
		"apps.beekeeper-studio": {}, "apps.copyq": {}, "apps.core-temp": {}, "apps.crystaldiskinfo": {}, "apps.drawio-desktop": {},
		"apps.duckstation": {}, "apps.files": {}, "apps.flameshot": {}, "apps.mkvtoolnix": {}, "apps.nomacs": {}, "apps.pip": {},
		"apps.smplayer": {}, "apps.wiztree": {},
	}
	mergeTargets, copyTargets := 0, 0
	for id := range mergeIDs {
		mod, record := catalog.Modules[id], catalog.Records[id]
		if mod == nil {
			t.Fatalf("merge module %q is absent", id)
		}
		var scenario validationmatrix.Scenario
		for _, candidate := range record.Synthetic.Scenarios {
			if candidate.Mode == validationmatrix.ScenarioConfigRoundtripV1 {
				scenario = candidate
				break
			}
		}
		definitions, failure := compileFixtureDefinitionsAt(repoRoot, mod, scenario)
		if failure != nil {
			t.Fatalf("merge module %q failure = %+v", id, failure)
		}
		if len(definitions.Entries) == 0 {
			t.Fatalf("merge module %q has no file fixture entries", id)
		}
		for _, definition := range definitions.Entries {
			switch definition.Strategy {
			case "merge-json", "merge-ini":
				mergeTargets++
				if definition.Kind != fixtureKindFile {
					t.Fatalf("merge module %q definition = %+v", id, definition)
				}
			case "copy":
				copyTargets++
			default:
				t.Fatalf("merge module %q definition = %+v", id, definition)
			}
		}
	}

	registryFailures := 0
	for id, mod := range catalog.Modules {
		if _, merge := mergeIDs[id]; merge || mod.Capture == nil || len(mod.Capture.RegistryKeys) == 0 && len(mod.Capture.RegistryValues) == 0 {
			continue
		}
		hasRegistryRestore := false
		for _, restore := range mod.Restore {
			hasRegistryRestore = hasRegistryRestore || restore.Type == "registry-import" || restore.Type == "registry-set"
		}
		if !hasRegistryRestore {
			continue
		}
		record := catalog.Records[id]
		for _, scenario := range record.Synthetic.Scenarios {
			if scenario.Mode != validationmatrix.ScenarioConfigRoundtripV1 {
				continue
			}
			if _, failure := compileFixtureDefinitionsAt(repoRoot, mod, scenario); failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "capture.registry" {
				t.Fatalf("registry module %q failure = %+v", id, failure)
			}
			registryFailures++
		}
	}
	if len(mergeIDs) != 13 || mergeTargets != 17 || copyTargets != 15 || registryFailures != 29 {
		t.Fatalf("non-copy partition = %d modules, %d merge + %d copy targets, %d registry; want 13, 17, 15, 29", len(mergeIDs), mergeTargets, copyTargets, registryFailures)
	}
}

func TestAutoFixtureUsesDirectoryForExtensionlessCaptureDestination(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	definitions, failure := compileFixtureDefinitions(mod, fixtureScenario())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(definitions.Entries) != 2 || definitions.Entries[0].Kind != fixtureKindFile || definitions.Entries[1].Kind != fixtureKindDirectory {
		t.Fatalf("auto fixture kinds = %+v", definitions.Entries)
	}
}

func TestCompileFixtureDefinitionsNormalizesMixedCatalogSeparators(t *testing.T) {
	raw := strings.ReplaceAll(fixtureModuleJSON, `./payload/apps/fixture/settings.json`, `.\\payload/apps\\fixture/settings.json`)
	raw = strings.ReplaceAll(raw, `apps/fixture/settings.json`, `apps\\fixture/settings.json`)
	mod, err := modules.ParseModuleJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	definitions, failure := compileFixtureDefinitions(mod, fixtureScenario())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(definitions.Entries) != 1 || definitions.Entries[0].Destination != "apps/fixture/settings.json" {
		t.Fatalf("definitions = %+v, want normalized catalog destination", definitions)
	}
}

func TestCompileFixtureDefinitionsRejectsCaseInsensitiveFlattenedDestinationCollision(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	mod.Capture.Files = append(mod.Capture.Files, modules.CaptureFile{
		Source: `%APPDATA%\Fixture\nested\Settings.JSON`, Dest: "apps/fixture/nested/Settings.JSON",
	})
	mod.Restore = append(mod.Restore, modules.RestoreDef{
		Type: "copy", Source: "./payload/apps/fixture/nested/Settings.JSON", Target: `%APPDATA%\Fixture\nested\Settings.JSON`, Backup: true,
	})
	if _, failure := compileFixtureDefinitions(mod, fixtureScenario()); failure == nil || failure.Code != CodeUnsupportedFixture || failure.Phase != "fixture" || failure.Coordinate != "capture.files[1].dest" || failure.Detail != "capture destinations collide after the production flattened payload rewrite" {
		t.Fatalf("flattened destination collision failure = %+v", failure)
	}
}

func TestCompileFixtureDefinitionsPreservesNestedDestinationsWithDistinctBasenames(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	mod.Capture.Files = append(mod.Capture.Files, modules.CaptureFile{
		Source: `%APPDATA%\Fixture\nested\preferences.json`, Dest: "apps/fixture/nested/preferences.json",
	})
	mod.Restore = append(mod.Restore, modules.RestoreDef{
		Type: "copy", Source: "./payload/apps/fixture/nested/preferences.json", Target: `%APPDATA%\Fixture\nested\preferences.json`, Backup: true,
	})
	definitions, failure := compileFixtureDefinitions(mod, fixtureScenario())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(definitions.Entries) != 2 || definitions.Entries[0].Destination != "apps/fixture/settings.json" || definitions.Entries[1].Destination != "apps/fixture/nested/preferences.json" {
		t.Fatalf("definitions = %+v, want authored nested destinations", definitions.Entries)
	}
}

func TestDeclarativeFixtureLoadsExactHashedCoordinateKinds(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	path := filepath.Join(repo, "fixtures", "shape.jsonc")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{
	  // Order is intentionally unrelated to module capture order.
	  "schemaVersion": 1,
	  "entries": [
	    {"coordinate":"capture.files[1]","kind":"directory"},
	    {"coordinate":"capture.files[0]","kind":"file"}
	  ]
	}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	scenario := fixtureScenario()
	scenario.Fixture = validationmatrix.Fixture{Type: validationmatrix.FixtureDeclarative, Path: "fixtures/shape.jsonc", SHA256: fmt.Sprintf("%x", digest)}
	definitions, failure := compileFixtureDefinitionsAt(repo, mod, scenario)
	if failure != nil {
		t.Fatalf("declarative fixture failed: %+v", failure)
	}
	if len(definitions.Entries) != 2 || definitions.Entries[0].Kind != fixtureKindFile || definitions.Entries[1].Kind != fixtureKindDirectory {
		t.Fatalf("definitions = %+v", definitions.Entries)
	}
}

func TestDeclarativeFixtureRejectsDuplicateJSONFields(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	path := filepath.Join(repo, "fixtures", "shape.jsonc")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schemaVersion":1,"schemaVersion":1,"entries":[{"coordinate":"capture.files[0]","kind":"file"},{"coordinate":"capture.files[1]","kind":"directory"}]}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	scenario := fixtureScenario()
	scenario.Fixture = validationmatrix.Fixture{Type: validationmatrix.FixtureDeclarative, Path: "fixtures/shape.jsonc", SHA256: fmt.Sprintf("%x", digest)}
	if _, failure := compileFixtureDefinitionsAt(repo, mod, scenario); failure == nil || failure.Code != CodeUnsupportedFixture {
		t.Fatalf("duplicate field failure = %+v", failure)
	}
}

func TestAssertionLedgerRejectsVacuousAndInflatedProof(t *testing.T) {
	minimum := fixtureScenario().MinimumAssertions
	exact := map[string]int{}
	for name, count := range minimum {
		exact[name] = count
	}
	tests := []struct {
		name   string
		counts map[string]int
		ops    OperationCounts
		proof  []validationmatrix.ProofLevel
		ok     bool
	}{
		{"exact non-vacuous", exact, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV1}, true},
		{"zero", withCount(exact, validationmatrix.AssertionCaptured, 0), OperationCounts{Executed: 1}, nil, false},
		{"below minimum", withCount(exact, validationmatrix.AssertionContent, 0), OperationCounts{Executed: 1}, nil, false},
		{"unknown assertion", withCount(exact, "future", 1), OperationCounts{Executed: 1}, nil, false},
		{"all skipped", exact, OperationCounts{Skipped: 2}, nil, false},
		{"missing proof", exact, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract}, false},
		{"proof inflation", exact, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofLiveInstall}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, failure := evaluateAssertions(fixtureScenario(), tt.counts, tt.ops, tt.proof)
			if (failure == nil) != tt.ok {
				t.Fatalf("failure = %+v, want ok=%v", failure, tt.ok)
			}
			if failure != nil && len(failure.ProofLevels) != 0 {
				t.Fatalf("failure carried passing proof: %+v", failure)
			}
		})
	}
}

func TestDecodeEnvelopeRejectsMalformedIdentityAndNestedFailure(t *testing.T) {
	valid := `{"schemaVersion":"1.0","cliVersion":"0.1.0","command":"rebuild","runId":"rebuild-run","timestampUtc":"2026-07-25T12:00:00Z","success":true,"testMode":{"active":true,"scenarioId":"default-v1","moduleId":"apps.fixture"},"data":{"apply":{"summary":{"total":1,"success":1,"skipped":0,"failed":0}},"configResolutionSummary":{"total":1,"selected":1,"skipped":0,"failed":0},"configResolutions":[{"status":"restored"}],"restoreItems":[{"status":"restored"}],"verify":{"summary":{"pass":2,"fail":0}}},"error":null}`
	tests := []struct {
		name string
		text string
	}{
		{"malformed", `{`},
		{"multiple", valid + ` {}`},
		{"duplicate top-level key", strings.Replace(valid, `"runId":"rebuild-run"`, `"runId":"rebuild-run","runId":"forged"`, 1)},
		{"duplicate nested key", strings.Replace(valid, `"failed":0`, `"failed":0,"failed":1`, 1)},
		{"wrong command", strings.Replace(valid, `"rebuild"`, `"apply"`, 1)},
		{"missing test mode", strings.Replace(valid, `"testMode":{"active":true,"scenarioId":"default-v1","moduleId":"apps.fixture"},`, "", 1)},
		{"mismatched scenario", strings.Replace(valid, `"default-v1"`, `"foreign"`, 1)},
		{"nested apply failure", strings.Replace(valid, `"failed":0`, `"failed":1`, 1)},
		{"config summary failure", strings.Replace(valid, `"selected":1,"skipped":0,"failed":0`, `"selected":1,"skipped":0,"failed":1`, 1)},
		{"config terminal failure", strings.Replace(valid, `"status":"restored"`, `"status":"rolled_back"`, 1)},
		{"restore item failure", strings.Replace(valid, `"restoreItems":[{"status":"restored"}]`, `"restoreItems":[{"status":"failed"}]`, 1)},
		{"nested verify failure", strings.Replace(valid, `"fail":0`, `"fail":1`, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, failure := decodeEnvelope([]byte(tt.text), "rebuild", "apps.fixture", "default-v1", "", ""); failure == nil {
				t.Fatal("decoder accepted invalid envelope")
			}
		})
	}
	if _, failure := decodeEnvelope([]byte(valid), "rebuild", "apps.fixture", "default-v1", "", ""); failure != nil {
		t.Fatalf("valid envelope failed: %+v", failure)
	}
	alreadyPresent := strings.Replace(valid, `"success":1,"skipped":0`, `"success":0,"skipped":1`, 1)
	if _, failure := decodeEnvelope([]byte(alreadyPresent), "rebuild", "apps.fixture", "default-v1", "", ""); failure != nil {
		t.Fatalf("successful package no-op was rejected: %+v", failure)
	}
	failing := strings.Replace(valid, `"success":true`, `"success":false`, 1)
	failing = strings.Replace(failing, `"error":null`, `"error":{"code":"TEST_MODE_ISOLATION_VIOLATION","message":"safe failure"}`, 1)
	if _, failure := decodeEnvelope([]byte(failing), "rebuild", "apps.fixture", "default-v1", "", ""); failure == nil || !strings.Contains(failure.Detail, "TEST_MODE_ISOLATION_VIOLATION") {
		t.Fatalf("engine failure detail = %+v", failure)
	}
}

func TestDecodeEventsRejectsMalformedIdentityAndLeaks(t *testing.T) {
	first := "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:00Z\",\"event\":\"phase\",\"phase\":\"capture\"}\n"
	progress := "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:01Z\",\"event\":\"progress\",\"phase\":\"capture\",\"stage\":\"inventory\"}\n" +
		"{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:02Z\",\"event\":\"progress\",\"phase\":\"capture\",\"stage\":\"settings\"}\n" +
		"{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:03Z\",\"event\":\"progress\",\"phase\":\"capture\",\"stage\":\"packaging\"}\n"
	artifact := "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:04Z\",\"event\":\"artifact\",\"phase\":\"capture\",\"kind\":\"manifest\",\"path\":\"$ENDSTATE_ROOT/manifests/captured.zip\"}\n"
	second := "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:05Z\",\"event\":\"summary\",\"phase\":\"capture\",\"total\":1,\"success\":1,\"skipped\":0,\"failed\":0}\n"
	valid := first + progress + artifact + second
	if _, failure := decodeEvents([]byte(valid), "capture", "different-envelope-run", "secret-root", "secret-nonce"); failure != nil {
		t.Fatalf("valid events failed: %+v", failure)
	}
	composed := eventSegment("apply-run", []string{"plan", "apply", "restore", "verify"}) + eventSegment("verify-run", []string{"verify"})
	if _, failure := decodeEvents([]byte(composed), "rebuild", "different-envelope-run", "secret-root", "secret-nonce"); failure != nil {
		t.Fatalf("composed rebuild events failed: %+v", failure)
	}
	for _, text := range []string{
		"not-json\n",
		second,
		first,
		first + strings.Replace(second, "capture-run", "foreign-run", 1),
		strings.Replace(valid, `"phase"`, `"future-event"`, 1),
		strings.Replace(valid, `"phase":"capture"`, `"phase":"future"`, 1),
		strings.Replace(valid, `"stage":"inventory"`, `"stage":"future"`, 1),
		strings.Replace(valid, `"event":"phase"`, `"event":"phase","future":true`, 1),
		strings.Replace(valid, `"event":"phase"`, `"event":"phase","event":"forged"`, 1),
		strings.Replace(valid, `"phase":"capture","total"`, `"phase":"verify","total"`, 1),
		strings.Replace(valid, `"timestamp":"2026-07-25T12:00:00Z"`, `"timestamp":"not-time"`, 1),
		strings.Replace(valid, `"failed":0`, `"failed":1`, 1),
		strings.Replace(valid, `"total":1,"success":1`, `"total":2,"success":1`, 1),
		strings.Replace(valid, `"total":1`, `"total":-1`, 1),
		first + "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:01Z\",\"event\":\"error\",\"scope\":\"engine\",\"message\":\"failed\"}\n" + second,
		first + "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:01Z\",\"event\":\"item\",\"id\":\"fixture\",\"driver\":\"winget\",\"status\":\"future\",\"reason\":\"detected\"}\n" + second,
		first + "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:01Z\",\"event\":\"item\",\"id\":\"fixture\",\"driver\":\"winget\",\"status\":\"present\",\"reason\":\"future\"}\n" + second,
		first + "{\"version\":1,\"runId\":\"capture-run\",\"timestamp\":\"2026-07-25T12:00:01Z\",\"event\":\"item\",\"id\":\"fixture\",\"status\":\"present\",\"reason\":\"detected\"}\n" + second,
		eventSegment("capture-run", []string{"capture"}) + eventSegment("verify-run", []string{"verify"}),
		valid + "secret-nonce\n",
	} {
		if _, failure := decodeEvents([]byte(text), "capture", "different-envelope-run", "secret-root", "secret-nonce"); failure == nil {
			t.Fatalf("accepted events %q", text)
		}
	}
}

func TestRuntimeForbiddenOutputValuesCoverAuthoritiesArtifactsAndFixtureSentinels(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	runtime.GuardRoot = filepath.Join(runtime.AuthorityRoot, "endstate-validation-guard-test")
	runtime.ToolRoot = filepath.Join(runtime.Root, "state", "validation-tools")
	runtime.enginePath = filepath.Join(runtime.AuthorityRoot, "engine.exe")
	runtime.Nonce = strings.Repeat("a", 32)
	values := runtime.forbiddenOutputValues()
	set := map[string]struct{}{}
	for _, value := range values {
		set[strings.ToLower(value)] = struct{}{}
	}
	required := []string{
		runtime.AuthorityRoot, runtime.Root, runtime.GuardRoot, runtime.ChildWorkingDir, runtime.ToolRoot, runtime.enginePath,
		filepath.Join(runtime.Root, "manifests", "captured.jsonc"), filepath.Join(runtime.Root, "manifests", "captured.zip"),
		filepath.Join(runtime.Root, "state", "backups"), filepath.Join(runtime.Root, "logs"), runtime.Nonce,
	}
	for _, target := range runtime.Plan.Targets {
		required = append(required, target.Resolved, target.PayloadPath, target.Captured, target.Mutated, target.Restored)
		for _, excluded := range append(append(append([]FixtureExcluded(nil), target.CaptureExcluded...), target.RestoreExcluded...), target.OverlappingExcluded...) {
			required = append(required, excluded.Path, excluded.Captured, excluded.Mutated)
		}
	}
	for _, value := range required {
		if _, ok := set[strings.ToLower(value)]; !ok {
			t.Fatalf("forbidden output set omitted %q", value)
		}
		if !leaked([]byte("wire:"+value), values...) {
			t.Fatalf("forbidden value was not detected: %q", value)
		}
	}
	for _, safe := range []string{"restore", "logs", "state", "capture", runtime.Module.ID, runtime.Scenario.ID} {
		if leaked([]byte(safe), values...) {
			t.Fatalf("compact public label produced a false-positive leak: %q", safe)
		}
	}
}

func TestCLIOutputRejectsRestoredFixtureBytes(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	runtime.Plan.Targets = []FixtureTarget{{Restored: fixtureSentinel(runtime.Module.ID, runtime.Scenario.ID, "capture.files[0]", "restored-only")}}
	forbidden := runtime.forbiddenOutputValues()
	value := runtime.Plan.Targets[0].Restored
	if _, failure := decodeEnvelope([]byte(`{"debug":`+mustContractJSON(t, value)+`}`), "rebuild", runtime.Module.ID, runtime.Scenario.ID, forbidden...); failure == nil || failure.Code != CodeIsolationFailure || failure.Coordinate != "stdout" {
		t.Fatalf("restored stdout leak failure = %+v", failure)
	}
	if _, failure := decodeEvents([]byte(`{"debug":`+mustContractJSON(t, value)+`}\n`), "rebuild", "rebuild-run", forbidden...); failure == nil || failure.Code != CodeIsolationFailure || failure.Coordinate != "stderr" {
		t.Fatalf("restored stderr leak failure = %+v", failure)
	}
}

func TestLeakDetectionRejectsJSONEscapedNestedStrings(t *testing.T) {
	forbidden := `C:\Users\validation\AppData\Roaming\Endstate`
	tests := []struct {
		name string
		wire any
	}{
		{name: "message", wire: map[string]any{"message": forbidden}},
		{name: "nested debug", wire: map[string]any{"message": map[string]any{"debug": forbidden}}},
		{name: "nested path array", wire: map[string]any{"debug": []any{map[string]any{"path": forbidden}}}},
		{name: "encoded nested json", wire: map[string]any{"message": mustContractJSON(t, map[string]any{"path": forbidden})}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.wire)
			if err != nil {
				t.Fatal(err)
			}
			if !leaked(raw, forbidden) {
				t.Fatalf("escaped nested forbidden value was not detected: %s", raw)
			}
		})
	}
}

func TestLeakDetectionNormalizesWindowsSlashDirection(t *testing.T) {
	forbidden := `C:\Users\validation\AppData\Roaming\Endstate`
	if !leaked([]byte(`{"path":"C:/Users/validation/AppData/Roaming/Endstate/config.json"}`), forbidden) {
		t.Fatal("forward-slash rendering of a forbidden Windows path was accepted")
	}
}

func TestLeakDetectionFailsClosedBeyondRecursiveDecodeLimit(t *testing.T) {
	encode := func(value string, depth int) []byte {
		var nested any = value
		for range depth {
			raw, err := json.Marshal(map[string]any{"message": nested})
			if err != nil {
				t.Fatal(err)
			}
			nested = string(raw)
		}
		raw, err := json.Marshal(map[string]any{"message": nested})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	forbidden := `C:\Users\validation\AppData\Roaming\Endstate`
	if !leaked(encode(forbidden, 12), forbidden) {
		t.Fatal("forbidden path nested beyond recursive decode limit was accepted")
	}
	if !leaked(encode("public-safe-value", 12), forbidden) {
		t.Fatal("excessive recursive encoding did not fail closed")
	}
	if leaked(encode("public-safe-value", 3), forbidden) {
		t.Fatal("safe bounded recursive encoding was rejected")
	}
}

func mustContractJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func eventSegment(runID string, phases []string) string {
	var result strings.Builder
	for index, phase := range phases {
		fmt.Fprintf(&result, "{\"version\":1,\"runId\":%q,\"timestamp\":\"2026-07-25T12:%02d:00Z\",\"event\":\"phase\",\"phase\":%q}\n", runID, index*2, phase)
		fmt.Fprintf(&result, "{\"version\":1,\"runId\":%q,\"timestamp\":\"2026-07-25T12:%02d:01Z\",\"event\":\"summary\",\"phase\":%q,\"total\":1,\"success\":1,\"skipped\":0,\"failed\":0}\n", runID, index*2, phase)
	}
	return result.String()
}

func TestArtifactNamesAcceptContainedDirectoryEntries(t *testing.T) {
	for _, name := range []string{"configs/", "configs/notepad-plus-plus/", "manifest.jsonc"} {
		if !safeArtifactName(name) {
			t.Fatalf("safe artifact member rejected: %q", name)
		}
	}
	for _, name := range []string{"../escape", "/rooted", `configs\escape`, "C:/host"} {
		if safeArtifactName(name) {
			t.Fatalf("unsafe artifact member accepted: %q", name)
		}
	}
}

func TestCapturedManifestUsesProductionJSONCParser(t *testing.T) {
	var captured manifest.Manifest
	if failure := decodeCapturedManifest([]byte("{/* comment */\"version\":1,\"name\":\"fixture\"}"), &captured); failure != nil || captured.Name != "fixture" {
		t.Fatalf("JSONC manifest decode = %+v failure=%+v", captured, failure)
	}
}

func TestArtifactConfigPayloadSetRejectsUnexpectedMember(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	entries := make(map[string][]byte)
	for _, target := range runtime.Plan.Targets {
		name, ok := targetArtifactPayloadName(runtime.Module.ID, target)
		if !ok {
			t.Fatalf("target %s has no artifact payload", target.Coordinate)
		}
		entries[strings.ToLower(name)] = []byte(target.Captured)
	}
	if failure := validateArtifactConfigPayloadSet(runtime, entries); failure != nil {
		t.Fatalf("exact config payload set failed: %+v", failure)
	}
	entries["configs/fixture/unexpected.json"] = []byte("unexpected")
	if failure := validateArtifactConfigPayloadSet(runtime, entries); failure == nil || failure.Code != CodeArtifactContract {
		t.Fatalf("unexpected configs member failure = %+v", failure)
	}
}

func TestMergeFixtureStatesBindEachPayloadAndRestoreTarget(t *testing.T) {
	for _, strategy := range []string{"merge-json", "merge-ini"} {
		t.Run(strategy, func(t *testing.T) {
			firstCaptured, firstMutated, firstRestored := fixtureStates("apps.fixture", "default-v1", "capture.files[0]", strategy)
			secondCaptured, secondMutated, secondRestored := fixtureStates("apps.fixture", "default-v1", "capture.files[1]", strategy)
			if firstCaptured == secondCaptured || firstMutated == secondMutated || firstRestored == secondRestored {
				t.Fatalf("same-strategy fixture states are not target-distinct: first=%q/%q/%q second=%q/%q/%q", firstCaptured, firstMutated, firstRestored, secondCaptured, secondMutated, secondRestored)
			}
			if strategy == "merge-json" && (!strings.HasSuffix(firstRestored, "\n") || !strings.Contains(firstRestored, "\n  ")) {
				t.Fatalf("JSON merge oracle lost canonical formatting: %q", firstRestored)
			}
			plan := fixtureScenarioRuntime(t).Plan
			plan.Targets[0].Captured, plan.Targets[0].Mutated, plan.Targets[0].Restored = firstCaptured, firstMutated, firstRestored
			plan.Targets[1].Captured, plan.Targets[1].Mutated, plan.Targets[1].Restored = secondCaptured, secondMutated, secondRestored
			if failure := plan.MaterializeCaptured(); failure != nil {
				t.Fatal(failure)
			}
			if err := os.WriteFile(plan.Targets[0].PayloadPath, []byte(secondCaptured), 0o600); err != nil {
				t.Fatal(err)
			}
			if failure := plan.CompareCaptured(); failure == nil || failure.Coordinate != "capture.files[0]" {
				t.Fatalf("swapped capture payload failure = %+v", failure)
			}
			if failure := plan.MaterializeRestored(); failure != nil {
				t.Fatal(failure)
			}
			if err := os.WriteFile(plan.Targets[0].PayloadPath, []byte(secondRestored), 0o600); err != nil {
				t.Fatal(err)
			}
			if failure := plan.CompareRestored(); failure == nil || failure.Coordinate != "capture.files[0]" {
				t.Fatalf("swapped restored payload failure = %+v", failure)
			}
		})
	}
}

func TestOptionalAbsentArtifactRequiresExactRequiredProjectionOnly(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(splitExcludeFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	runtime := &scenarioRuntime{Module: mod, Scenario: scenario, Plan: plan, Root: plan.context.Root(), Inventory: validationInventory(mod)}
	captured := exactCapturedManifestForRuntime(t, runtime)
	manifestBytes, err := json.Marshal(captured)
	if err != nil {
		t.Fatal(err)
	}
	required := runtime.Plan.Targets[1]
	root := targetArtifactRoot(runtime.Module.ID, required)
	entries := map[string][]byte{
		"manifest.jsonc": manifestBytes,
		strings.ToLower(root + "/" + fixturePayloadName):                                     []byte(required.Captured),
		strings.ToLower(root + "/" + filepath.ToSlash(required.RestoreExcluded[0].Relative)): []byte(required.RestoreExcluded[0].Captured),
	}
	if failure := validateOptionalAbsentArtifactEntries(runtime, entries); failure != nil {
		t.Fatalf("exact required optional-absence projection failed: %+v", failure)
	}
	optionalName, ok := targetArtifactPayloadName(runtime.Module.ID, runtime.Plan.Targets[0])
	if !ok {
		t.Fatal("optional target has no artifact payload")
	}
	entries[optionalName] = []byte(runtime.Plan.Targets[0].Captured)
	if failure := validateOptionalAbsentArtifactEntries(runtime, entries); failure == nil || failure.Code != CodeArtifactContract {
		t.Fatalf("optional payload was accepted: %+v", failure)
	}
	delete(entries, optionalName)
	entries[strings.ToLower(root+"/"+fixturePayloadName)] = []byte("wrong")
	if failure := validateOptionalAbsentArtifactEntries(runtime, entries); failure == nil || failure.Code != CodeArtifactContract {
		t.Fatalf("wrong required payload was accepted: %+v", failure)
	}
}

func exactCapturedManifestForRuntime(t *testing.T, runtime *scenarioRuntime) manifest.Manifest {
	t.Helper()
	captured := manifest.Manifest{
		Apps: []manifest.App{{
			ID: runtime.Inventory.AppID, Driver: runtime.Inventory.Driver, Source: runtime.Inventory.Source,
			Refs: map[string]string{"windows": runtime.Inventory.Ref},
		}},
		ConfigModules: []string{runtime.Module.ID},
	}
	for _, verifier := range runtime.Module.Verify {
		captured.Verify = append(captured.Verify, manifest.VerifyEntry{
			Type: verifier.Type, Path: verifier.Path, Command: verifier.Command,
			ValueName: verifier.ValueName, ValueType: verifier.ValueType, Data: verifier.Data,
		})
	}
	for _, restore := range runtime.Module.Restore {
		rewritten, ok := expectedCapturedRestoreSource(runtime.Plan, runtime.Module.ID, restore)
		if !ok {
			t.Fatalf("restore has no fixture projection: %+v", restore)
		}
		captured.Restore = append(captured.Restore, manifest.RestoreEntry{
			Type: restore.Type, Source: rewritten, Target: restore.Target, Pattern: restore.Pattern,
			Reason: restore.Reason, Backup: restore.Backup, Optional: restore.Optional,
			Exclude: append([]string(nil), restore.Exclude...), FromModule: runtime.Module.ID,
			Key: restore.Key, ValueName: restore.ValueName, ValueType: restore.ValueType, Data: restore.Data,
		})
	}
	return captured
}

func TestValidationInventoryUsesEffectiveWingetSource(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	inventory := validationInventory(mod)
	if inventory.Driver != "winget" || inventory.Ref != "Vendor.Fixture" || inventory.Source != "winget" {
		t.Fatalf("inventory = %+v, want exact winget package identity", inventory)
	}
	chocolatey := validationInventory(&modules.Module{ID: "apps.fixture", DisplayName: "Fixture", Matches: modules.MatchCriteria{Chocolatey: []string{"fixture"}}})
	if chocolatey.Driver != "chocolatey" || chocolatey.Ref != "fixture" || chocolatey.Source != "" {
		t.Fatalf("chocolatey inventory = %+v, want source-less driver identity", chocolatey)
	}
}

func TestCapturedProjectionRequiresExactRestoreAndVerifierSemantics(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	captured := manifest.Manifest{}
	for _, verifier := range runtime.Module.Verify {
		captured.Verify = append(captured.Verify, manifest.VerifyEntry{
			Type: verifier.Type, Path: verifier.Path, Command: verifier.Command,
			ValueName: verifier.ValueName, ValueType: verifier.ValueType, Data: verifier.Data,
		})
	}
	for _, restore := range runtime.Module.Restore {
		var rewritten string
		for _, target := range runtime.Plan.Targets {
			if target.Authored == restore.Target {
				rewritten = v1RestoreSource(runtime.Module.ID, target.Destination)
			}
		}
		captured.Restore = append(captured.Restore, manifest.RestoreEntry{
			Type: restore.Type, Source: rewritten, Target: restore.Target, Pattern: restore.Pattern,
			Reason: restore.Reason, Backup: restore.Backup, Optional: restore.Optional,
			Exclude: append([]string(nil), restore.Exclude...), FromModule: runtime.Module.ID,
			Key: restore.Key, ValueName: restore.ValueName, ValueType: restore.ValueType, Data: restore.Data,
		})
	}
	if failure := validateCapturedProjection(runtime, &captured); failure != nil {
		t.Fatalf("exact projection failed: %+v", failure)
	}
	captured.Verify[0].ValueType = "invented"
	if failure := validateCapturedProjection(runtime, &captured); failure == nil {
		t.Fatal("corrupted verifier projection passed")
	}
	captured.Verify[0].ValueType = runtime.Module.Verify[0].ValueType
	captured.Restore[0].Reason = "invented"
	if failure := validateCapturedProjection(runtime, &captured); failure == nil {
		t.Fatal("corrupted restore projection passed")
	}
}

func TestRunReturnsFixtureResolutionAsPersistedOperationalFailure(t *testing.T) {
	repo, engine, resultPath := writeSelectionRepository(t)
	raw := []byte(strings.ReplaceAll(fixtureModuleJSON, "%APPDATA%", "%UNKNOWN%"))
	mod, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(repo, "modules", "apps", "fixture")
	if err := os.WriteFile(filepath.Join(directory, "module.jsonc"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	record := validationmatrix.ValidationRecord{
		SchemaVersion: 1, ModuleID: mod.ID, ModuleRevision: mod.Revision,
		Synthetic: validationmatrix.SyntheticPolicy{Scenarios: []validationmatrix.Scenario{fixtureScenario()}},
		Live:      validationmatrix.LivePolicy{Mode: validationmatrix.LiveCandidate, ReasonCode: "test", Explanation: "test fixture"},
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "validation.jsonc"), recordJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), Request{EnginePath: engine, RepoRoot: repo, ModuleID: mod.ID, ScenarioID: "default-v1", ResultPath: resultPath})
	if err != nil {
		t.Fatalf("operational fixture failure escaped as Go error: %v", err)
	}
	if result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeUnsupportedFixture || result.Failure.Coordinate != "capture.files[0]" {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("failed result was not persisted: %v", err)
	}
	var persisted Result
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.Failure == nil || persisted.Failure.Coordinate != "capture.files[0]" {
		t.Fatalf("persisted result = %+v err=%v", persisted, err)
	}
}

func TestRunPreservesSelectedIdentityWhenFixtureCompilationFails(t *testing.T) {
	repo, engine, resultPath := writeSelectionRepository(t)
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario := fixtureScenario()
	scenario.Fixture = validationmatrix.Fixture{
		Type: validationmatrix.FixtureDeclarative, Path: "fixtures/missing.jsonc", SHA256: strings.Repeat("0", 64),
	}
	record := validationmatrix.ValidationRecord{
		SchemaVersion: 1, ModuleID: mod.ID, ModuleRevision: mod.Revision,
		Synthetic: validationmatrix.SyntheticPolicy{Scenarios: []validationmatrix.Scenario{scenario}},
		Live:      validationmatrix.LivePolicy{Mode: validationmatrix.LiveCandidate, ReasonCode: "test", Explanation: "test fixture"},
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	validationPath := filepath.Join(repo, "modules", "apps", "fixture", "validation.jsonc")
	if err := os.WriteFile(validationPath, recordJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), Request{
		EnginePath: engine, RepoRoot: repo, ModuleID: mod.ID, ScenarioID: scenario.ID, ResultPath: resultPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeUnsupportedFixture ||
		result.ModuleRevision != mod.Revision || result.Kind != scenario.Mode {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Result
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.ModuleRevision != mod.Revision || persisted.Kind != scenario.Mode {
		t.Fatalf("persisted result = %+v err=%v", persisted, err)
	}
}

func TestCLIJourneyExecutorUsesValidationOwnedWorkingDirectory(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	repository, err := os.MkdirTemp(filepath.Dir(runtime.Plan.context.Root()), "endstate-validation-repository-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repository) })
	selected := &selection{request: Request{RepoRoot: repository}}
	executor := newCLIJourneyExecutor(selected, runtime)
	if !fixtureContained(runtime.AuthorityRoot, executor.workingDir) || filepath.Dir(executor.workingDir) != runtime.AuthorityRoot {
		t.Fatalf("working directory = %q, want direct task-authority child of %q", executor.workingDir, runtime.AuthorityRoot)
	}
	if runtime.Plan.context.ValidateSandboxPath(executor.workingDir) == nil {
		t.Fatalf("working directory = %q overlaps engine mutation authority %q", executor.workingDir, runtime.Plan.context.Root())
	}
}

func TestIndependentBoundariesDetectWritesOutsideValidationRoot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, runtime *scenarioRuntime, repository, engine string)
	}{
		{
			name: "repository",
			mutate: func(t *testing.T, _ *scenarioRuntime, repository, _ string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(repository, "unexpected.txt"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "repository same-size bytes with restored timestamp",
			mutate: func(t *testing.T, _ *scenarioRuntime, repository, _ string) {
				t.Helper()
				path := filepath.Join(repository, "tracked.txt")
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("mutated!"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "engine",
			mutate: func(t *testing.T, _ *scenarioRuntime, _, engine string) {
				t.Helper()
				if err := os.WriteFile(engine, []byte("changed engine"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "guard",
			mutate: func(t *testing.T, runtime *scenarioRuntime, _, _ string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(runtime.GuardRoot, "sentinel.txt"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "authority sibling",
			mutate: func(t *testing.T, runtime *scenarioRuntime, _, _ string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(runtime.AuthorityRoot, "unexpected"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, repository, engine := boundaryRuntime(t)
			if err := runtime.captureIndependentBoundaries(repository, engine); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, runtime, repository, engine)
			if failure := runtime.assertIndependentBoundaries(); failure == nil || failure.Code != CodeIsolationFailure {
				t.Fatalf("failure = %+v, want isolation failure", failure)
			}
		})
	}
}

func boundaryRuntime(t *testing.T) (*scenarioRuntime, string, string) {
	t.Helper()
	authority := t.TempDir()
	root := filepath.Join(authority, "endstate-validation-test")
	guard := filepath.Join(authority, "endstate-validation-guard-test")
	working := filepath.Join(authority, "endstate-validation-cwd-test")
	for _, directory := range []string{root, guard, working} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(guard, "sentinel.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	if err := os.WriteFile(filepath.Join(repository, "tracked.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	engineDirectory := t.TempDir()
	engine := filepath.Join(engineDirectory, "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &scenarioRuntime{AuthorityRoot: authority, Root: root, GuardRoot: guard, ChildWorkingDir: working}, repository, engine
}

func TestVerifyEvidenceRequiresExactAppAndModuleVerifierAttribution(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	runtime.Inventory = validationInventory(runtime.Module)
	valid := validVerifyEvidenceData(runtime)
	if failure := validateVerifyEvidence(valid, runtime, "verify"); failure != nil {
		t.Fatalf("valid evidence rejected: %+v", failure)
	}

	var payload map[string]any
	if err := json.Unmarshal(valid, &payload); err != nil {
		t.Fatal(err)
	}
	results := payload["results"].([]any)
	results[1].(map[string]any)["type"] = "command-exists"
	invalid, _ := json.Marshal(payload)
	if failure := validateVerifyEvidence(invalid, runtime, "verify"); failure == nil {
		t.Fatal("wrong verifier attribution was accepted")
	}
}

func TestRebuildEvidenceRequiresExactIterationOutcomes(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	runtime.Inventory = validationInventory(runtime.Module)
	for iteration := 0; iteration < 3; iteration++ {
		payload := map[string]any{
			"apply": map[string]any{
				"summary": map[string]any{"total": 1, "success": 0, "skipped": 1, "failed": 0},
				"actions": []any{map[string]any{"id": runtime.Inventory.AppID, "driver": runtime.Inventory.Driver, "status": "present", "reason": "already_installed"}},
			},
			"configResolutionSummary": map[string]any{"total": 1, "selected": 1, "skipped": 0, "failed": 0},
			"configResolutions":       []any{map[string]any{"status": "restored", "resolution": "legacy_unverified", "reason": nil}},
			"restoreItems":            []any{},
			"verify":                  json.RawMessage(validVerifyEvidenceData(runtime)),
		}
		for _, target := range runtime.Plan.Targets {
			item := map[string]any{
				"target": target.Authored, "source": v1RestoreSource(runtime.Module.ID, target.Destination),
				"restoreType": "", "targetExistedBefore": true,
			}
			if iteration < 2 {
				item["status"], item["backupCreated"], item["backupPath"] = "restored", true, "$ENDSTATE_ROOT/state/backups/item"
			} else {
				item["status"], item["backupCreated"] = "skipped_up_to_date", false
				payload["configResolutionSummary"].(map[string]any)["selected"] = 0
				payload["configResolutionSummary"].(map[string]any)["skipped"] = 1
				payload["configResolutions"].([]any)[0].(map[string]any)["status"] = "skipped"
				payload["configResolutions"].([]any)[0].(map[string]any)["reason"] = "already_up_to_date"
			}
			payload["restoreItems"] = append(payload["restoreItems"].([]any), item)
		}
		payload["apply"].(map[string]any)["configResolutionSummary"] = payload["configResolutionSummary"]
		payload["apply"].(map[string]any)["configResolutions"] = payload["configResolutions"]
		payload["apply"].(map[string]any)["restoreItems"] = payload["restoreItems"]
		raw, _ := json.Marshal(payload)
		if failure := validateRebuildEvidence(raw, runtime, iteration); failure != nil {
			t.Fatalf("iteration %d valid evidence rejected: %+v", iteration, failure)
		}
		if iteration == 2 {
			payload["configResolutionSummary"] = map[string]any{"total": 1, "selected": 1, "skipped": 0, "failed": 0}
			payload["configResolutions"] = []any{map[string]any{"status": "restored", "resolution": "legacy_unverified", "reason": nil}}
			payload["apply"].(map[string]any)["configResolutionSummary"] = payload["configResolutionSummary"]
			payload["apply"].(map[string]any)["configResolutions"] = payload["configResolutions"]
			invalid, _ := json.Marshal(payload)
			if failure := validateRebuildEvidence(invalid, runtime, iteration); failure == nil {
				t.Fatal("selected convergence resolution was accepted for skipped restore items")
			}
		}
		if iteration == 0 {
			payload["apply"].(map[string]any)["actions"].([]any)[0].(map[string]any)["reason"] = "future"
			invalid, _ := json.Marshal(payload)
			if failure := validateRebuildEvidence(invalid, runtime, iteration); failure == nil {
				t.Fatal("unknown apply reason was accepted")
			}
			payload["apply"].(map[string]any)["actions"].([]any)[0].(map[string]any)["reason"] = "already_installed"
			payload["configResolutions"].([]any)[0].(map[string]any)["resolution"] = "future"
			payload["apply"].(map[string]any)["configResolutions"] = payload["configResolutions"]
			invalid, _ = json.Marshal(payload)
			if failure := validateRebuildEvidence(invalid, runtime, iteration); failure == nil {
				t.Fatal("unknown config resolution was accepted")
			}
		}
	}
}

func TestV1RebuildEventsCrossBindTerminalBackupPaths(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	runtime.Inventory = validationInventory(runtime.Module)
	backups := make(map[string]string, len(runtime.Plan.Targets))
	for index, target := range runtime.Plan.Targets {
		backups[strings.ToLower(target.Authored)] = fmt.Sprintf("$ENDSTATE_ROOT/state/backups/rebuild/item-%d", index)
	}
	binding := rebuildEvidenceBinding{BackupsByTarget: backups}
	events := v1RebuildEventsForTest(runtime, binding, false)
	if failure := validateV1RebuildEvents(events, runtime, 0, binding); failure != nil {
		t.Fatalf("valid restore events = %+v", failure)
	}
	for _, test := range []struct {
		name   string
		mutate func([]map[string]any)
	}{
		{"foreign path", func(events []map[string]any) { events[3]["backupPath"] = "$ENDSTATE_ROOT/state/backups/foreign/item" }},
		{"swapped paths", func(events []map[string]any) {
			events[3]["backupPath"], events[5]["backupPath"] = events[5]["backupPath"], events[3]["backupPath"]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := make([]map[string]any, len(events))
			for index, event := range events {
				candidate[index] = make(map[string]any, len(event))
				for key, value := range event {
					candidate[index][key] = value
				}
			}
			test.mutate(candidate)
			if failure := validateV1RebuildEvents(candidate, runtime, 0, binding); failure == nil || failure.Coordinate != "restore-item" {
				t.Fatalf("%s terminal backup accepted: %+v", test.name, failure)
			}
		})
	}
	if failure := validateV1RebuildEvents(v1RebuildEventsForTest(runtime, rebuildEvidenceBinding{}, true), runtime, 2, rebuildEvidenceBinding{}); failure != nil {
		t.Fatalf("repeat restore event backup nullability = %+v", failure)
	}
}

func v1RebuildEventsForTest(runtime *scenarioRuntime, binding rebuildEvidenceBinding, repeat bool) []map[string]any {
	events := []map[string]any{
		{"event": "phase", "phase": "restore"},
		{"event": "config-resolution"},
	}
	for _, target := range runtime.Plan.Targets {
		start := map[string]any{
			"event": "restore-item", "module": runtime.Module.ID, "restorer": fixtureStrategy(target),
			"source": v1RestoreSource(runtime.Module.ID, target.Destination), "target": target.Authored,
			"status": "restoring", "reason": nil, "backupPath": nil, "targetExisted": true,
		}
		terminal := map[string]any{}
		for key, value := range start {
			terminal[key] = value
		}
		if repeat {
			terminal["status"], terminal["reason"] = "skipped_up_to_date", "already_up_to_date"
		} else {
			terminal["status"], terminal["backupPath"] = "restored", binding.BackupsByTarget[strings.ToLower(target.Authored)]
		}
		events = append(events, start, terminal)
	}
	success, skipped := len(runtime.Plan.Targets), 0
	if repeat {
		success, skipped = 0, len(runtime.Plan.Targets)
	}
	events = append(events, map[string]any{"event": "summary", "phase": "restore", "total": json.Number(strconv.Itoa(len(runtime.Plan.Targets))), "success": json.Number(strconv.Itoa(success)), "skipped": json.Number(strconv.Itoa(skipped)), "failed": json.Number("0")})
	for _, total := range []int{1, 1 + len(runtime.Module.Verify)} {
		events = append(events,
			map[string]any{"event": "phase", "phase": "verify"},
			map[string]any{"event": "item", "id": runtime.Inventory.Ref, "driver": runtime.Inventory.Driver},
			map[string]any{"event": "summary", "phase": "verify", "total": json.Number(strconv.Itoa(total)), "success": json.Number(strconv.Itoa(total)), "skipped": json.Number("0"), "failed": json.Number("0")},
		)
	}
	return events
}

func TestRevertEvidenceRequiresJournalAndExactActions(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	journal := "$ENDSTATE_ROOT/logs/restore-journal-apply-test.json"
	backups := map[string]string{}
	results := make([]any, 0, len(runtime.Plan.Targets))
	for index, target := range runtime.Plan.Targets {
		backup := fmt.Sprintf("$ENDSTATE_ROOT/state/backups/apply-test/item-%d", index)
		backups[strings.ToLower(target.Authored)] = backup
		results = append(results, map[string]any{"target": target.Authored, "action": "reverted", "backupUsed": backup})
	}
	binding := rebuildEvidenceBinding{Journal: journal, BackupsByTarget: backups}
	raw, _ := json.Marshal(map[string]any{"journalUsed": journal, "results": results})
	if failure := validateRevertEvidence(raw, runtime, binding); failure != nil {
		t.Fatalf("valid revert evidence rejected: %+v", failure)
	}
	raw, _ = json.Marshal(map[string]any{"journalUsed": "", "results": results})
	if failure := validateRevertEvidence(raw, runtime, binding); failure == nil {
		t.Fatal("empty revert journal was accepted")
	}
	results[0].(map[string]any)["backupUsed"] = "$ENDSTATE_ROOT/state/backups/foreign"
	raw, _ = json.Marshal(map[string]any{"journalUsed": journal, "results": results})
	if failure := validateRevertEvidence(raw, runtime, binding); failure == nil {
		t.Fatal("foreign revert backup was accepted")
	}
}

func TestRebuildStorageEvidenceBindsContainedBackupsAndJournal(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	runtime.Inventory = validationInventory(runtime.Module)
	if failure := runtime.Plan.Mutate(); failure != nil {
		t.Fatal(failure)
	}
	before, failure := snapshotRebuildStorage(runtime)
	if failure != nil {
		t.Fatal(failure)
	}
	backupValues := map[string]string{}
	restoreItems := []any{}
	journalEntries := []any{}
	repeatRestoreItems := []any{}
	for index, target := range runtime.Plan.Targets {
		semantic := fmt.Sprintf("$ENDSTATE_ROOT/state/backups/apply-test/item-%d", index)
		physical := filepath.Join(runtime.Root, "state", "backups", "apply-test", fmt.Sprintf("item-%d", index))
		copyFixtureTreeForTest(t, target.Resolved, physical)
		backupValues[strings.ToLower(target.Authored)] = semantic
		source := v1RestoreSource(runtime.Module.ID, target.Destination)
		restoreItems = append(restoreItems, map[string]any{
			"target": target.Authored, "source": source, "restoreType": "", "targetExistedBefore": true,
			"status": "restored", "backupCreated": true, "backupPath": semantic,
		})
		journalEntries = append(journalEntries, map[string]any{
			"resolvedSourcePath": source, "targetPath": target.Authored, "targetExistedBefore": true,
			"backupRequested": true, "backupCreated": true, "backupPath": semantic, "action": "restored", "restoreType": "copy",
		})
		repeatRestoreItems = append(repeatRestoreItems, map[string]any{
			"target": target.Authored, "source": source, "restoreType": "", "targetExistedBefore": true,
			"status": "skipped_up_to_date", "backupCreated": false, "backupPath": "",
		})
	}
	journalPath := filepath.Join(runtime.Root, "logs", "restore-journal-apply-test.json")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	journalData, _ := json.Marshal(map[string]any{
		"runId": "apply-test", "timestamp": "2026-07-25T12:00:00Z", "manifestPath": "$ENDSTATE_ROOT/manifests/captured.jsonc",
		"manifestDir": "$ENDSTATE_ROOT/manifests", "entries": journalEntries,
	})
	if err := os.WriteFile(journalPath, journalData, 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := configrestore.BeginLiveWithBoundary(
		context.Background(), filepath.Join(runtime.Root, "state"), "apply-test", nil,
		v2HostBoundary{runtime.validationContext()},
	)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"apply": map[string]any{
			"summary": map[string]any{"total": 1, "success": 0, "skipped": 1, "failed": 0},
			"actions": []any{map[string]any{"id": runtime.Inventory.AppID, "driver": runtime.Inventory.Driver, "status": "present", "reason": "already_installed"}},
		},
		"configResolutionSummary": map[string]any{"total": 1, "selected": 1, "skipped": 0, "failed": 0},
		"configResolutions":       []any{map[string]any{"status": "restored", "resolution": "legacy_unverified", "reason": nil}},
		"restoreItems":            restoreItems,
		"verify":                  json.RawMessage(validVerifyEvidenceData(runtime)),
	}
	raw, _ := json.Marshal(payload)
	binding, after, failure := validateRebuildStorageEvidence(runtime, raw, 0, before)
	if failure != nil {
		t.Fatalf("exact storage evidence rejected: %+v", failure)
	}
	journalSemantic := "$ENDSTATE_ROOT/logs/restore-journal-apply-test.json"
	if binding.Journal != journalSemantic || len(binding.BackupsByTarget) != len(backupValues) {
		t.Fatalf("binding = %+v", binding)
	}
	for target, backup := range backupValues {
		if binding.BackupsByTarget[target] != backup {
			t.Fatalf("backup binding[%q] = %q, want %q", target, binding.BackupsByTarget[target], backup)
		}
	}
	firstBackup := filepath.Join(runtime.Root, "state", "backups", "apply-test", "item-0")
	if runtime.Plan.Targets[0].Directory {
		t.Fatal("storage tamper fixture requires its first target to be a file")
	}
	if err := os.WriteFile(firstBackup, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, failure := validateRebuildStorageEvidence(runtime, raw, 0, before); failure == nil {
		t.Fatal("tampered backup evidence was accepted")
	}
	if err := os.WriteFile(firstBackup, []byte(runtime.Plan.Targets[0].Mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	payload["configResolutionSummary"] = map[string]any{"total": 1, "selected": 0, "skipped": 1, "failed": 0}
	payload["configResolutions"] = []any{map[string]any{"status": "skipped", "resolution": "legacy_unverified", "reason": "already_up_to_date"}}
	payload["restoreItems"] = repeatRestoreItems
	repeatRaw, _ := json.Marshal(payload)
	if repeatBinding, _, failure := validateRebuildStorageEvidence(runtime, repeatRaw, 2, after); failure != nil {
		t.Fatalf("exact repeat no-op evidence rejected: %+v", failure)
	} else if repeatBinding.Journal != "" || len(repeatBinding.BackupsByTarget) != 0 {
		t.Fatalf("repeat binding = %+v", repeatBinding)
	}
	if err := os.MkdirAll(filepath.Join(runtime.Root, "state", "config-restore"), 0o700); err != nil {
		t.Fatal(err)
	}
	hiddenDelta := filepath.Join(runtime.Root, "state", "config-restore", "hidden-delta")
	if err := os.WriteFile(hiddenDelta, []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, failure := validateRebuildStorageEvidence(runtime, repeatRaw, 2, after); failure == nil || failure.Coordinate != "storage" {
		t.Fatalf("repeat convergence accepted hidden persistent delta: %+v", failure)
	}
	if err := os.Remove(hiddenDelta); err != nil {
		t.Fatal(err)
	}
	beforeRevert, failure := snapshotRebuildStorage(runtime)
	if failure != nil {
		t.Fatal(failure)
	}
	if failure := validateLegacyRevertStorage(runtime, beforeRevert, binding); failure == nil {
		t.Fatal("revert storage accepted an omitted legacy member marker")
	}
	guard, err = configrestore.BeginLiveWithBoundary(
		context.Background(), filepath.Join(runtime.Root, "state"), "revert-test", nil,
		v2HostBoundary{runtime.validationContext()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.MarkLegacyMemberReverted(context.Background(), registered); err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if failure := validateLegacyRevertStorage(runtime, beforeRevert, binding); failure != nil {
		t.Fatalf("exact legacy member revert marker rejected: %+v", failure)
	}
	markerPath := filepath.Join(runtime.Root, "state", "config-restore", "v1", "legacy-reverts", onlyStoreMemberID(t, beforeRevert)+".json")
	if err := os.WriteFile(filepath.Join(runtime.Root, "state", "config-restore", "v1", "legacy-reverts", "unrelated.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := validateLegacyRevertStorage(runtime, beforeRevert, binding); failure == nil {
		t.Fatal("revert storage accepted an uncited persistent delta")
	}
	if err := os.Remove(filepath.Join(runtime.Root, "state", "config-restore", "v1", "legacy-reverts", "unrelated.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := validateLegacyRevertStorage(runtime, beforeRevert, binding); failure == nil {
		t.Fatal("revert storage accepted a malformed legacy member marker")
	}
}

func onlyStoreMemberID(t *testing.T, snapshot rebuildStorageSnapshot) string {
	t.Helper()
	if len(snapshot.storeMembers) != 1 {
		t.Fatalf("store members = %d, want 1", len(snapshot.storeMembers))
	}
	for id := range snapshot.storeMembers {
		return id
	}
	return ""
}

func copyFixtureTreeForTest(t *testing.T, source, target string) {
	t.Helper()
	info, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCleanupFailureCannotLeavePassingResult(t *testing.T) {
	result := Result{Status: ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofConfigRoundtripV1}}
	result = applyCleanupFailure(result, fmt.Errorf("cleanup failed"))
	if result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeIsolationFailure || len(result.ProofLevels) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCleanupFailureSurfacesAfterJourneyFailure(t *testing.T) {
	result := Result{Status: ResultStatusFailed, Failure: fail(CodeExecutionFailure, "rebuild", "fixture", "rebuild failed")}
	result = applyCleanupFailure(result, fmt.Errorf("cleanup failed"))
	if result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeIsolationFailure {
		t.Fatalf("result = %+v", result)
	}
}

func TestAbandonScenarioRuntimeSurfacesRegistryCleanupFailureAfterSetupFailure(t *testing.T) {
	registryFixtureCreated := true
	cleanupCalls := 0
	failure, err := abandonScenarioRuntime(func() error {
		cleanupCalls++
		if !registryFixtureCreated {
			t.Fatal("registry fixture was not created before setup failed")
		}
		return fmt.Errorf("registry fixture cleanup failed")
	}, fail(CodeExecutionFailure, "setup", "boundary", "later setup failed"), nil)
	if err != nil || failure == nil || failure.Code != CodeIsolationFailure || failure.Phase != "cleanup" || cleanupCalls != 1 {
		t.Fatalf("failure = %+v, err = %v, cleanup calls = %d", failure, err, cleanupCalls)
	}
}

func TestPersistResultRejectsSwappedParentLink(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "results")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "result.json")
	external := t.TempDir()
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, parent); err != nil {
		t.Skipf("host cannot create a test directory link: %v", err)
	}
	if err := persistResult(path, Result{Status: ResultStatusFailed}); err == nil {
		t.Fatal("result persistence followed a swapped parent link")
	}
	if _, err := os.Lstat(filepath.Join(external, "result.json")); !os.IsNotExist(err) {
		t.Fatalf("external result path was touched: %v", err)
	}
}

func validVerifyEvidenceData(runtime *scenarioRuntime) []byte {
	results := []any{map[string]any{
		"type": "app", "id": runtime.Inventory.AppID, "ref": runtime.Inventory.Ref,
		"driver": runtime.Inventory.Driver, "status": "pass", "reason": "",
	}}
	for _, verifier := range runtime.Module.Verify {
		results = append(results, map[string]any{"type": verifier.Type, "status": "pass", "reason": ""})
	}
	raw, _ := json.Marshal(map[string]any{
		"summary": map[string]any{"total": len(results), "pass": len(results), "fail": 0, "skipped": 0},
		"results": results,
	})
	return raw
}

func writeSelectionRepository(t *testing.T) (string, string, string) {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, "modules", "apps", "fixture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "module.jsonc"), []byte(fixtureModuleJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON([]byte(fixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	record := validationmatrix.ValidationRecord{
		SchemaVersion: 1, ModuleID: mod.ID, ModuleRevision: mod.Revision,
		Synthetic: validationmatrix.SyntheticPolicy{Scenarios: []validationmatrix.Scenario{fixtureScenario()}},
		Live:      validationmatrix.LivePolicy{Mode: validationmatrix.LiveCandidate, ReasonCode: "test", Explanation: "test fixture"},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "validation.jsonc"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	return repo, engine, filepath.Join(t.TempDir(), "result.json")
}

func fixtureScenario() validationmatrix.Scenario {
	return validationmatrix.Scenario{
		ID: "default-v1", Mode: validationmatrix.ScenarioConfigRoundtripV1,
		Fixture: validationmatrix.Fixture{Type: validationmatrix.FixtureAuto}, TimeoutSeconds: 30,
		MinimumAssertions: map[string]int{
			validationmatrix.AssertionCaptured: 1, validationmatrix.AssertionPayload: 1,
			validationmatrix.AssertionProvenance: 1, validationmatrix.AssertionRewrittenRestore: 1,
			validationmatrix.AssertionContent: 1, validationmatrix.AssertionRebuild: 1,
			validationmatrix.AssertionVerify: 1, validationmatrix.AssertionNestedSummary: 1,
			validationmatrix.AssertionRevert: 1,
		},
	}
}

func withCount(source map[string]int, name string, value int) map[string]int {
	result := map[string]int{}
	for key, count := range source {
		result[key] = count
	}
	result[name] = value
	return result
}

const fixtureModuleJSON = `{
  "id":"apps.fixture","displayName":"Fixture","sensitivity":"none",
  "matches":{"winget":["Vendor.Fixture"]},
  "verify":[{"type":"file-exists","path":"%APPDATA%\\Fixture\\settings.json"}],
  "restore":[{"type":"copy","source":"./payload/apps/fixture/settings.json","target":"%APPDATA%\\Fixture\\settings.json","backup":true}],
  "capture":{"files":[{"source":"%APPDATA%\\Fixture\\settings.json","dest":"apps/fixture/settings.json"}]}
}`
