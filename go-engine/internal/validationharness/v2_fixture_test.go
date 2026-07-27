// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestV2FixtureShapeRejectsUnknownMissingAndMalformedInput(t *testing.T) {
	valid := `{
	  "schemaVersion": 1,
	  "sourceVersion": "2.4",
	  "entries": [{
	    "captureCoordinate": "config.sets[0].generations[0].capture.files[0]",
	    "restoreCoordinate": "config.sets[0].generations[0].restore[0]",
	    "kind": "file",
	    "format": "ini"
	  }]
	}`
	fixture, err := decodeV2Fixture([]byte(valid))
	if err != nil || fixture.SourceVersion != "2.4" || len(fixture.Entries) != 1 {
		t.Fatalf("decode valid v2 fixture = %+v, %v", fixture, err)
	}

	tests := []struct {
		name string
		raw  string
	}{
		{"unknown field", strings.Replace(valid, `"sourceVersion": "2.4",`, `"sourceVersion": "2.4", "extra": true,`, 1)},
		{"missing source version", strings.Replace(valid, `"sourceVersion": "2.4",`, ``, 1)},
		{"missing capture coordinate", strings.Replace(valid, `"captureCoordinate": "config.sets[0].generations[0].capture.files[0]",`, ``, 1)},
		{"extra json value", valid + `{}`},
		{"wrong kind", strings.Replace(valid, `"kind": "file"`, `"kind": "auto"`, 1)},
		{"wrong format", strings.Replace(valid, `"format": "ini"`, `"format": "yaml"`, 1)},
		{"non-version source", strings.Replace(valid, `"sourceVersion": "2.4"`, `"sourceVersion": "not-a-version"`, 1)},
		{"control source", strings.Replace(valid, `"sourceVersion": "2.4"`, `"sourceVersion": "2.4\\n"`, 1)},
		{"malformed source", strings.Replace(valid, `"sourceVersion": "2.4"`, `"sourceVersion": "2..4"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeV2Fixture([]byte(test.raw)); err == nil {
				t.Fatal("malformed v2 fixture was accepted")
			}
		})
	}
}

func TestSchemaV2SingleFileExcludesMustBeProvablyInapplicable(t *testing.T) {
	tests := []struct {
		module, scenario string
		inventory        validationmode.Inventory
	}{
		{module: "apps.windows-terminal", scenario: "generation-preferences-g1-97631ba2d2e5", inventory: validationmode.Inventory{AppID: "windows-terminal", Driver: "winget", Ref: "Microsoft.WindowsTerminal", DisplayName: "Windows Terminal", Version: "1.20.11781.0", InitialState: "present"}},
		{module: "apps.owncloud", scenario: "generation-preferences-g1-1c4479cb88b9", inventory: validationmode.Inventory{AppID: "owncloud", Driver: "winget", Ref: "ownCloud.ownCloudDesktop", DisplayName: "ownCloud", Version: "2.4", InitialState: "present"}},
	}
	for _, test := range tests {
		t.Run(test.module, func(t *testing.T) {
			repo, mod, scenario := trackedV2Fixture(t, test.module, test.scenario)
			compiled, failure := compileV2FixtureAt(repo, mod, scenario)
			if failure != nil {
				t.Fatal(failure)
			}
			context := fixtureValidationContext(t, mod.ID, scenario.ID)
			plan, failure := compileV2FixturePlan(context, mod, scenario, compiled, test.inventory)
			if failure != nil || len(plan.Targets) != 1 || len(plan.Targets[0].Excluded) != 0 {
				t.Fatalf("tracked single-file exclude proof = %+v, %+v", plan, failure)
			}
			for _, mutation := range []struct{ name, pattern string }{
				{name: "applicable", pattern: filepath.Base(plan.Targets[0].Resolved)},
				{name: "unknown", pattern: "invalid[glob"},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					candidate := compiled
					candidate.Entries = append([]v2CompiledEntry(nil), compiled.Entries...)
					candidate.Entries[0].CaptureOnly = append(append([]string(nil), candidate.Entries[0].CaptureOnly...), mutation.pattern)
					if _, failure := compileV2FixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, candidate, test.inventory); failure == nil || failure.Coordinate != "exclude" || failure.Code != CodeUnsupportedFixture {
						t.Fatalf("single-file exclude %q accepted: %+v", mutation.pattern, failure)
					}
				})
			}
		})
	}
}

func TestV2MatchesExcludeNormalizesMixedCatalogSeparators(t *testing.T) {
	if !v2MatchesExclude(`profiles\Cache/mixed\entry.json`, `**/Cache\**`) {
		t.Fatal("mixed-separator catalog exclude did not match")
	}
}

func TestSchemaV2TrackedDirectFixturesCompileFromProduction(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		module, scenario, generation, format string
	}{
		{"apps.windows-terminal", "generation-preferences-g1-97631ba2d2e5", "g1", "json"},
		{"apps.owncloud", "generation-preferences-g1-1c4479cb88b9", "g1", "ini"},
		{"apps.owncloud", "generation-preferences-g2-899536c068d4", "g2", "ini"},
		{"apps.studio-one", "generation-preferences-g1-61e9f6f3c254", "g1", "tree"},
	}
	for _, test := range tests {
		t.Run(test.module+"/"+test.scenario, func(t *testing.T) {
			mod := catalog.Modules[test.module]
			record := catalog.Records[test.module]
			var scenario validationmatrix.Scenario
			for _, candidate := range record.Synthetic.Scenarios {
				if candidate.ID == test.scenario {
					scenario = candidate
				}
			}
			compiled, failure := compileV2FixtureAt(repo, mod, scenario)
			if failure != nil {
				t.Fatalf("compile tracked fixture: %+v", failure)
			}
			if compiled.Generation.ID != test.generation || len(compiled.Entries) != 1 || string(compiled.Entries[0].Shape.Format) != test.format {
				t.Fatalf("compiled fixture = %+v", compiled)
			}
		})
	}
}

func TestSchemaV2OwnCloudMigrationFixtureCompilesProductionEdge(t *testing.T) {
	_, mod, scenario := trackedV2Fixture(t, "apps.owncloud", "migration-preferences-g1-to-g2")
	raw := []byte(`{
	  "schemaVersion": 1,
	  "sourceVersion": "2.4",
	  "targetVersion": "2.5",
	  "entries": [{
	    "captureCoordinate": "config.sets[0].generations[0].capture.files[0]",
	    "restoreCoordinate": "config.sets[0].generations[1].restore[0]",
	    "kind": "file",
	    "format": "ini"
	  }]
	}`)
	digest := sha256.Sum256(raw)
	repo := t.TempDir()
	writeV2TestFile(t, filepath.Join(repo, "migration.jsonc"), raw)
	scenario.Fixture = validationmatrix.Fixture{
		Type: validationmatrix.FixtureDeclarative, Path: "migration.jsonc", SHA256: hex.EncodeToString(digest[:]),
	}

	if _, failure := compileV2FixtureAt(repo, mod, scenario); failure != nil {
		t.Fatalf("compile exact ownCloud g1-to-g2 fixture: %+v", failure)
	}
}

func TestSchemaV2TrackedOwnCloudMigrationFixtureBindsSourceTargetAndEdge(t *testing.T) {
	repo, mod, scenario := trackedV2Fixture(t, "apps.owncloud", "migration-preferences-g1-to-g2")
	compiled, failure := compileV2FixtureAt(repo, mod, scenario)
	if failure != nil {
		t.Fatalf("compile tracked ownCloud migration fixture: %+v", failure)
	}
	if compiled.Generation.ID != "g1" || compiled.TargetGeneration.ID != "g2" || compiled.Migration == nil ||
		compiled.Migration.From != "g1" || compiled.Migration.To != "g2" || len(compiled.Migration.Operations) != 1 ||
		compiled.Migration.Operations[0].Type != "file-move" || len(compiled.Entries) != 1 ||
		compiled.Entries[0].Capture.Source != `%LOCALAPPDATA%\ownCloud\owncloud.cfg` ||
		compiled.Entries[0].Restore.Target != `%APPDATA%\ownCloud\owncloud.cfg` ||
		len(compiled.Entries[0].MigrationValidations) != 1 || len(compiled.Entries[0].TargetValidations) != 1 {
		t.Fatalf("tracked migration fixture did not bind exact production source/target/edge: %+v", compiled)
	}
}

func TestSchemaV2OwnCloudMigrationPlanUsesDistinctProductionHostStates(t *testing.T) {
	repo, mod, scenario := trackedV2Fixture(t, "apps.owncloud", "migration-preferences-g1-to-g2")
	compiled, failure := compileV2FixtureAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	context := fixtureValidationContext(t, mod.ID, scenario.ID)
	inventory := validationInventory(mod)
	inventory.Version = compiled.Definition.SourceVersion
	plan, failure := compileV2FixturePlan(context, mod, scenario, compiled, inventory)
	if failure != nil {
		t.Fatalf("compile ownCloud migration plan: %+v", failure)
	}
	local, _ := context.VirtualRoot("LOCALAPPDATA")
	roaming, _ := context.VirtualRoot("APPDATA")
	wantSource := filepath.Join(local, "ownCloud", "owncloud.cfg")
	wantTarget := filepath.Join(roaming, "ownCloud", "owncloud.cfg")
	if len(plan.CaptureTargets) != 1 || len(plan.Targets) != 1 ||
		!strings.EqualFold(plan.CaptureTargets[0].Resolved, wantSource) ||
		!strings.EqualFold(plan.Targets[0].Resolved, wantTarget) ||
		strings.EqualFold(plan.CaptureTargets[0].Resolved, plan.Targets[0].Resolved) ||
		plan.Instance.Version.Raw != "2.4" || plan.TargetInstance.Version.Raw != "2.5" {
		t.Fatalf("migration source/target host states = %+v", plan)
	}
}

func TestV2FixtureContentFormatsAreSyntacticallyValid(t *testing.T) {
	for _, format := range []v2FixtureFormat{v2FormatJSON, v2FormatINI, v2FormatFile} {
		captured, mutated, err := v2FixtureContents("apps.fixture", "generation-g1", "coordinate", format)
		if err != nil || len(captured) == 0 || len(mutated) == 0 || string(captured) == string(mutated) {
			t.Fatalf("contents(%s) = (%q, %q, %v)", format, captured, mutated, err)
		}
		if err := validateV2FixtureContent(format, captured); err != nil {
			t.Fatalf("captured %s is invalid: %v", format, err)
		}
		if err := validateV2FixtureContent(format, mutated); err != nil {
			t.Fatalf("mutated %s is invalid: %v", format, err)
		}
	}
	if err := validateV2FixtureContent(v2FormatJSON, []byte(`{"broken":`)); err == nil {
		t.Fatal("invalid JSON fixture content was accepted")
	}
	if err := validateV2FixtureContent(v2FormatINI, []byte("not-an-ini-document")); err == nil {
		t.Fatal("invalid INI fixture content was accepted")
	}
}

func TestSchemaV2FixtureCompilerRejectsStaleOrRefabricatedAuthority(t *testing.T) {
	repo, mod, scenario := trackedV2Fixture(t, "apps.studio-one", "generation-preferences-g1-61e9f6f3c254")
	tests := []struct {
		name       string
		mutate     func(*validationmatrix.Scenario)
		coordinate string
		code       string
	}{
		{name: "bad fixture sha", coordinate: "fixture.sha256", code: CodeUnsupportedFixture, mutate: func(value *validationmatrix.Scenario) {
			value.Fixture.SHA256 = strings.Repeat("0", 64)
		}},
		{name: "unknown detector", coordinate: "expected.detectorId", code: CodeGenerationContract, mutate: func(value *validationmatrix.Scenario) {
			value.Expected.DetectorID = "foreign"
		}},
		{name: "unknown config set", coordinate: "expected.configSetId", code: CodeGenerationContract, mutate: func(value *validationmatrix.Scenario) {
			value.Expected.ConfigSetID = "foreign"
		}},
		{name: "wrong generation", coordinate: "sourceVersion", code: CodeGenerationContract, mutate: func(value *validationmatrix.Scenario) {
			value.Expected.GenerationID = "g2"
		}},
		{name: "refabricated accepted historical fingerprint", coordinate: "expected.fingerprint", code: CodeGenerationContract, mutate: func(value *validationmatrix.Scenario) {
			value.Expected.Fingerprint = "abc0141add2928b64ab8fd6b82319c2f57fb086c1ed4b16b776b991d22882444"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := scenario
			expected := *scenario.Expected
			candidate.Expected = &expected
			test.mutate(&candidate)
			if _, failure := compileV2FixtureAt(repo, mod, candidate); failure == nil || failure.Coordinate != test.coordinate || failure.Code != test.code {
				t.Fatalf("failure = %+v, want %s at coordinate %q", failure, test.code, test.coordinate)
			}
		})
	}
}

func TestSchemaV2FixtureCompilerRejectsMissingExtraCoordinatesAndUnwitnessedDeclarations(t *testing.T) {
	repo, mod, scenario := trackedV2Fixture(t, "apps.studio-one", "generation-preferences-g1-61e9f6f3c254")
	compiled, failure := compileV2FixtureAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}

	missing := compiled.Definition
	missing.Entries = nil
	if _, err := decodeV2Fixture(mustV2FixtureJSON(t, missing)); err == nil {
		t.Fatal("fixture decoder accepted a missing coordinate set")
	}
	extra := compiled.Definition
	extra.Entries = append(extra.Entries, extra.Entries[0])
	if _, err := decodeV2Fixture(mustV2FixtureJSON(t, extra)); err == nil {
		t.Fatal("fixture decoder accepted an extra duplicate coordinate")
	}

	entry := compiled.Entries[0]
	entry.CaptureOnly = []string{"invalid[glob"}
	if _, excludeFailure := compileV2ExcludeWitnesses(mod.ID, scenario.ID, compiled.Definition.InstanceLocator, entry); excludeFailure == nil || excludeFailure.Code != CodeUnsupportedFixture {
		t.Fatal("fixture compiler accepted an exclude with no deterministic witness")
	}
	if v2ValidationWitnessed(entry.Shape, entry.Capture, modules.ValidationDef{Type: "file-exists", Path: "settings/missing.settings"}) {
		t.Fatal("fixture compiler treated an absent validation member as witnessed")
	}
}

func TestSchemaV2DirectoryExcludeWitnessCannotEscapeFixtureRoot(t *testing.T) {
	entry := v2CompiledEntry{CaptureOnly: []string{`**\..\**`}}
	root := t.TempDir()
	if witnesses, failure := compileV2ExcludeWitnesses("apps.fixture", "generation-g1", root, entry); failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != "exclude" {
		t.Fatalf("escaping exclude witness = (%+v, %+v), want unsupported fixture", witnesses, failure)
	}
}

func TestSchemaV2FixtureCompilerClassifiesBadShapeAndCoordinateAsUnsupported(t *testing.T) {
	repository, mod, scenario := trackedV2Fixture(t, "apps.studio-one", "generation-preferences-g1-61e9f6f3c254")
	compiled, failure := compileV2FixtureAt(repository, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	for _, test := range []struct {
		name       string
		mutate     func(*v2FixtureDefinition)
		coordinate string
	}{
		{name: "bad shape", coordinate: "fixture.path", mutate: func(value *v2FixtureDefinition) {
			value.Entries[0].Kind = fixtureKind("auto")
		}},
		{name: "unknown coordinate", coordinate: "entries[0]", mutate: func(value *v2FixtureDefinition) {
			value.Entries[0].CaptureCoordinate = "config.sets[0].generations[0].capture.files[999]"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := compiled.Definition
			definition.Entries = append([]v2FixtureEntry(nil), definition.Entries...)
			test.mutate(&definition)
			raw := mustV2FixtureJSON(t, definition)
			digest := sha256.Sum256(raw)
			repo := t.TempDir()
			writeV2TestFile(t, filepath.Join(repo, "fixture.json"), raw)
			candidate := scenario
			candidate.Fixture.Path = "fixture.json"
			candidate.Fixture.SHA256 = hex.EncodeToString(digest[:])
			if _, failure := compileV2FixtureAt(repo, mod, candidate); failure == nil || failure.Code != CodeUnsupportedFixture || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v, want unsupported_fixture at %q", failure, test.coordinate)
			}
		})
	}
}

func TestStudioOneFixtureRequiresExactlyOneDetectorRootAndPortablePathEvidence(t *testing.T) {
	repo, mod, scenario := trackedV2Fixture(t, "apps.studio-one", "generation-preferences-g1-61e9f6f3c254")
	compiled, failure := compileV2FixtureAt(repo, mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	inventory := validationmode.Inventory{
		AppID: "studio-one", Driver: "validation", Ref: "studio-one", DisplayName: "PreSonus Studio One",
		Version: compiled.Definition.SourceVersion, InitialState: "present",
	}

	t.Run("missing locator", func(t *testing.T) {
		context := fixtureValidationContext(t, mod.ID, scenario.ID)
		candidate := compiled
		candidate.Definition.InstanceLocator = ""
		if _, failure := compileV2FixturePlan(context, mod, scenario, candidate, inventory); failure == nil || failure.Coordinate != "instanceLocator" {
			t.Fatalf("missing Studio root locator was accepted: %+v", failure)
		}
	})

	t.Run("multiple matching roots", func(t *testing.T) {
		context := fixtureValidationContext(t, mod.ID, scenario.ID)
		appData, ok := context.VirtualRoot("APPDATA")
		if !ok {
			t.Fatal("APPDATA validation root is absent")
		}
		if err := os.MkdirAll(filepath.Join(appData, "PreSonus", "Studio One 6"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, failure := compileV2FixturePlan(context, mod, scenario, compiled, inventory); failure == nil || failure.Coordinate != "detector" {
			t.Fatalf("multiple Studio roots were accepted: %+v", failure)
		}
	})

	pathInstance := modules.ConfigInstance{Evidence: modules.InstanceEvidence{Type: "path"}}
	if !exactV2SourceEvidence(&manifest.ConfigSourceInstanceEvidence{Type: "path"}, pathInstance) {
		t.Fatal("portable path evidence was rejected")
	}
	if exactV2SourceEvidence(&manifest.ConfigSourceInstanceEvidence{Type: "path", Ref: repo}, pathInstance) {
		t.Fatal("physical path data was accepted in portable detector evidence")
	}
}

func trackedV2Fixture(t *testing.T, moduleID, scenarioID string) (string, *modules.Module, validationmatrix.Scenario) {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules[moduleID]
	for _, scenario := range catalog.Records[moduleID].Synthetic.Scenarios {
		if scenario.ID == scenarioID {
			return repo, mod, scenario
		}
	}
	t.Fatalf("tracked scenario %s/%s is absent", moduleID, scenarioID)
	return "", nil, validationmatrix.Scenario{}
}

func mustV2FixtureJSON(t *testing.T, fixture v2FixtureDefinition) []byte {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
