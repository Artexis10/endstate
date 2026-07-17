// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validV2Module = `{
  "moduleSchemaVersion": 2,
  "id": "apps.versioned",
  "displayName": "Versioned App",
  "sensitivity": "low",
  "matches": {"winget": ["Vendor.Versioned"]},
  "config": {
    "instanceDetectors": [
      {"id": "installed", "type": "package"},
      {"id": "profiles", "type": "path", "glob": "%APPDATA%\\Vendor\\App *", "versionPattern": "^App (?P<version>[0-9.]+)$"}
    ],
    "sets": [{
      "id": "preferences",
      "displayName": "Preferences",
      "generations": [
        {
          "id": "g1",
          "order": 1,
          "matches": [{"versionRange": ">=25 <28"}],
          "capture": {"files": [{"source": "${instance.root}\\prefs.json", "dest": "prefs/prefs.json"}]},
          "restore": [{"type": "copy", "source": "prefs/prefs.json", "target": "${instance.root}\\prefs.json", "backup": true}],
          "validate": [{"type": "json-parse", "path": "prefs/prefs.json"}]
        },
        {
          "id": "g2",
          "order": 2,
          "matches": [{"versionPattern": "^2[89](?:\\.[0-9]+)*$"}],
          "capture": {"files": [{"source": "${instance.root}\\settings.json", "dest": "settings.json"}]},
          "restore": [{"type": "copy", "source": "settings.json", "target": "${instance.root}\\settings.json", "backup": true}],
          "validate": [{"type": "json-path-exists", "path": "settings.json", "jsonPath": "$.theme"}]
        }
      ],
      "migrations": [{
        "from": "g1",
        "to": "g2",
        "operations": [
          {"type": "file-move", "source": "prefs/prefs.json", "target": "settings.json"},
          {"type": "json-set", "path": "settings.json", "jsonPath": "$.theme", "value": "system"}
        ],
        "validate": [{"type": "json-path-exists", "path": "settings.json", "jsonPath": "$.theme"}]
      }]
    }]
  }
}`

func writeTestModule(t *testing.T, root, dir, content string) string {
	t.Helper()
	moduleDir := filepath.Join(root, dir)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	moduleFile := filepath.Join(moduleDir, "module.jsonc")
	if err := os.WriteFile(moduleFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return moduleFile
}

func TestParseModuleJSONPreservesMigrationOperationNumbers(t *testing.T) {
	content := strings.Replace(validV2Module, `"value": "system"`, `"value": 900719925474099312345`, 1)
	mod, err := ParseModuleJSON([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	value := mod.Config.Sets[0].Migrations[0].Operations[1].Value
	number, ok := value.(json.Number)
	if !ok || number.String() != "900719925474099312345" {
		t.Fatalf("operation value = %T(%v), want exact json.Number", value, value)
	}
}

func TestParseModuleJSONRejectsUnknownFieldsAtEverySchemaLevel(t *testing.T) {
	for name, content := range map[string]string{
		"top level":           strings.Replace(validV2Module, `"moduleSchemaVersion": 2,`, `"moduleSchemaVersion": 2, "unknownTopLevel": true,`, 1),
		"migration operation": strings.Replace(validV2Module, `"jsonPath": "$.theme", "value": "system"`, `"jsonPath": "$.theme", "value": "system", "shell": "nope"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseModuleJSON([]byte(content)); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("ParseModuleJSON error = %v", err)
			}
		})
	}
}

func TestParseModuleJSONKeepsLegacyUnknownFieldsBackwardCompatible(t *testing.T) {
	if _, err := ParseModuleJSON([]byte(`{
		"id":"apps.legacy", "displayName":"Legacy", "matches":{"winget":["Vendor.Legacy"]},
		"curation":{"status":"historical"}
	}`)); err != nil {
		t.Fatalf("legacy extension field rejected: %v", err)
	}
}

func TestJSONSetDistinguishesOmittedValueFromExplicitNull(t *testing.T) {
	omitted := strings.Replace(validV2Module, `, "value": "system"`, ``, 1)
	omittedModule, err := ParseModuleJSON([]byte(omitted))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateModule(omittedModule, "module.jsonc"); err == nil || DiagnosticCode(err) != DiagnosticInvalidMigrationOperation {
		t.Fatalf("omitted value validation = %v", err)
	}

	explicitNull := strings.Replace(validV2Module, `"value": "system"`, `"value": null`, 1)
	nullModule, err := ParseModuleJSON([]byte(explicitNull))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateModule(nullModule, "module.jsonc"); err != nil {
		t.Fatalf("explicit null validation = %v", err)
	}
	encoded, err := json.Marshal(nullModule.Config.Sets[0].Migrations[0].Operations[1])
	if err != nil || !strings.Contains(string(encoded), `"value":null`) {
		t.Fatalf("explicit null encoding = %s, %v", encoded, err)
	}
}

func TestLoadedGenerationFingerprintDoesNotRoundLargeSemanticNumbers(t *testing.T) {
	left := strings.Replace(validV2Module, `"order": 1`, `"order": 9007199254740992`, 1)
	right := strings.Replace(validV2Module, `"order": 1`, `"order": 9007199254740993`, 1)
	leftModule, err := ParseModuleJSON([]byte(left))
	if err != nil {
		t.Fatal(err)
	}
	rightModule, err := ParseModuleJSON([]byte(right))
	if err != nil {
		t.Fatal(err)
	}
	leftFingerprint := leftModule.Config.Sets[0].Generations[0].Fingerprint
	rightFingerprint := rightModule.Config.Sets[0].Generations[0].Fingerprint
	if leftFingerprint == rightFingerprint {
		t.Fatalf("distinct exact operation numbers produced one fingerprint: %s", leftFingerprint)
	}
}

func TestLoadCatalogWithDiagnostics_AcceptsV1AndV2(t *testing.T) {
	root := t.TempDir()
	writeTestModule(t, root, "legacy", `{"id":"apps.legacy","displayName":"Legacy","matches":{"winget":["Vendor.Legacy"]}}`)
	writeTestModule(t, root, "versioned", validV2Module)

	catalog, diagnostics, err := LoadCatalogWithDiagnostics(root)
	if err != nil {
		t.Fatalf("LoadCatalogWithDiagnostics: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
	if len(catalog) != 2 {
		t.Fatalf("catalog size = %d, want 2", len(catalog))
	}

	legacy := catalog["apps.legacy"]
	if legacy == nil || !legacy.Unversioned || legacy.EffectiveSchemaVersion() != 1 {
		t.Fatalf("legacy module = %+v, want unversioned schema v1", legacy)
	}
	versioned := catalog["apps.versioned"]
	if versioned == nil || versioned.Unversioned || versioned.EffectiveSchemaVersion() != 2 {
		t.Fatalf("versioned module = %+v, want generation-aware schema v2", versioned)
	}
	if versioned.Revision == "" {
		t.Error("versioned module revision is empty")
	}
	if got := versioned.Config.Sets[0].Generations[0].Fingerprint; got == "" {
		t.Error("generation fingerprint is empty")
	}
}

func TestLoadedModulePinsCanonicalSnapshot(t *testing.T) {
	root := t.TempDir()
	moduleFile := writeTestModule(t, root, "versioned", validV2Module)
	catalog, diagnostics, err := LoadCatalogWithDiagnostics(root)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("load: diagnostics=%+v error=%v", diagnostics, err)
	}
	mod := catalog["apps.versioned"]
	beforeSnapshot := string(mod.CanonicalSnapshot())
	beforeRevision := mod.Revision
	if beforeSnapshot == "" || beforeRevision == "" {
		t.Fatal("loaded module did not retain canonical snapshot and revision")
	}

	if err := os.WriteFile(moduleFile, []byte(strings.Replace(validV2Module, "Versioned App", "Changed On Disk", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := string(mod.CanonicalSnapshot()); got != beforeSnapshot {
		t.Errorf("pinned snapshot changed after source edit: %q != %q", got, beforeSnapshot)
	}
	if mod.Revision != beforeRevision {
		t.Errorf("pinned revision changed after source edit: %s != %s", mod.Revision, beforeRevision)
	}

	copyOfSnapshot := mod.CanonicalSnapshot()
	copyOfSnapshot[0] = '['
	if got := string(mod.CanonicalSnapshot()); got != beforeSnapshot {
		t.Error("CanonicalSnapshot returned mutable backing storage")
	}
}

func TestCanonicalModuleRevision_CosmeticAndSemanticEdits(t *testing.T) {
	left := []byte("{\r\n // comment\r\n \"displayName\": \"App\", \"id\": \"apps.test\", \"matches\": {\"winget\": [\"A.B\"]}\r\n}")
	right := []byte(`{"matches":{"winget":["A.B"]},"id":"apps.test","displayName":"App"}`)
	semantic := []byte(`{"matches":{"winget":["A.C"]},"id":"apps.test","displayName":"App"}`)

	leftHash, err := ComputeModuleRevision(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := ComputeModuleRevision(right)
	if err != nil {
		t.Fatal(err)
	}
	semanticHash, err := ComputeModuleRevision(semantic)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Errorf("cosmetic edit changed revision: %s != %s", leftHash, rightHash)
	}
	if leftHash == semanticHash {
		t.Errorf("semantic edit did not change revision: %s", leftHash)
	}

	canonical, err := CanonicalizeModuleJSON(left)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canonical), "comment") || strings.Contains(string(canonical), "\n") {
		t.Errorf("canonical JSON contains cosmetic content: %q", canonical)
	}
}

func TestCanonicalModuleRevision_PreservesSemanticFieldsNamedLikeLoaderMetadata(t *testing.T) {
	left := []byte(`{
		"moduleSchemaVersion":2,
		"id":"apps.test",
		"displayName":"Test",
		"matches":{"winget":["A.B"]},
		"config":{"sets":[{"id":"prefs","generations":[{"id":"g1","order":1}],"migrations":[{
			"from":"g1","to":"g2","operations":[{"type":"json-set","path":"prefs.json","jsonPath":"$.metadata","value":{"revision":"one"}}],"validate":[]
		}]}]}
	}`)
	right := []byte(strings.Replace(string(left), `"revision":"one"`, `"revision":"two"`, 1))

	leftHash, err := ComputeModuleRevision(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := ComputeModuleRevision(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash == rightHash {
		t.Fatalf("semantic JSON value named revision was incorrectly treated as loader metadata: %s", leftHash)
	}
}

func TestGenerationFingerprint_TracksDefinitionButNotHistoryData(t *testing.T) {
	base := GenerationDef{
		ID:       "g1",
		Order:    1,
		Matches:  []VersionSelectorDef{{VersionRange: ">=1 <2"}},
		Capture:  &CaptureDef{Files: []CaptureFile{{Source: "%APPDATA%\\App\\prefs.json", Dest: "prefs.json"}}},
		Restore:  []RestoreDef{{Type: "copy", Source: "prefs.json", Target: "%APPDATA%\\App\\prefs.json", Backup: true}},
		Validate: []ValidationDef{{Type: "json-parse", Path: "prefs.json"}},
	}
	first, err := ComputeGenerationFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}

	historyOnly := base
	historyOnly.AcceptsSourceFingerprints = []string{"old-fingerprint"}
	second, err := ComputeGenerationFingerprint(historyOnly)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("accepted historical fingerprints changed identity: %s != %s", first, second)
	}

	changed := base
	changed.Restore = append([]RestoreDef(nil), base.Restore...)
	changed.Restore[0].Target = "%APPDATA%\\App\\settings.json"
	third, err := ComputeGenerationFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Errorf("changed generation definition retained fingerprint %s", first)
	}
}

func TestLoadedGenerationFingerprintIncludesCompleteParsedDefinition(t *testing.T) {
	left := validV2Module
	right := strings.Replace(validV2Module, `"id": "g1",`, `"id": "g1", "requiresAppClosed": true,`, 1)

	leftModule, err := ParseModuleJSON([]byte(left))
	if err != nil {
		t.Fatal(err)
	}
	rightModule, err := ParseModuleJSON([]byte(right))
	if err != nil {
		t.Fatal(err)
	}
	leftFingerprint := leftModule.Config.Sets[0].Generations[0].Fingerprint
	rightFingerprint := rightModule.Config.Sets[0].Generations[0].Fingerprint
	if leftFingerprint == rightFingerprint {
		t.Fatalf("parsed generation definition changed without changing fingerprint: %s", leftFingerprint)
	}
}

func TestLoadedGenerationFingerprintIgnoresCosmeticJSONCEdits(t *testing.T) {
	left := []byte("{\r\n" +
		"  // cosmetic comment\r\n" +
		"  \"moduleSchemaVersion\": 2,\r\n" +
		"  \"id\": \"apps.test\",\r\n" +
		"  \"displayName\": \"Test\",\r\n" +
		"  \"matches\": {\"winget\": [\"A.B\"]},\r\n" +
		"  \"config\": {\"instanceDetectors\": [{\"id\":\"installed\",\"type\":\"package\"}], \"sets\": [{\r\n" +
		"    \"id\": \"prefs\", \"generations\": [{\"id\":\"g1\",\"order\":1,\"matches\":[{\"versionRange\":\">=1 <2\"}],\"validate\":[{\"type\":\"file-exists\",\"path\":\"prefs.json\"}]}]\r\n" +
		"  }]}\r\n" +
		"}")
	right := []byte(`{"config":{"sets":[{"generations":[{"validate":[{"path":"prefs.json","type":"file-exists"}],"matches":[{"versionRange":">=1 <2"}],"order":1,"id":"g1"}],"id":"prefs"}],"instanceDetectors":[{"type":"package","id":"installed"}]},"matches":{"winget":["A.B"]},"displayName":"Test","id":"apps.test","moduleSchemaVersion":2}`)

	leftModule, err := ParseModuleJSON(left)
	if err != nil {
		t.Fatal(err)
	}
	rightModule, err := ParseModuleJSON(right)
	if err != nil {
		t.Fatal(err)
	}
	leftFingerprint := leftModule.Config.Sets[0].Generations[0].Fingerprint
	rightFingerprint := rightModule.Config.Sets[0].Generations[0].Fingerprint
	if leftFingerprint != rightFingerprint {
		t.Fatalf("cosmetic JSONC/key-order edit changed loaded generation fingerprint: %s != %s", leftFingerprint, rightFingerprint)
	}
}

func TestLoadCatalogWithDiagnostics_ReportsInvalidV2WithoutBreakingLegacyAPI(t *testing.T) {
	root := t.TempDir()
	writeTestModule(t, root, "good", `{"id":"apps.good","displayName":"Good","matches":{"winget":["Good.App"]}}`)
	badPath := writeTestModule(t, root, "bad", strings.Replace(validV2Module, `"versionRange": ">=25 <28"`, `"versionRange": "not-a-range"`, 1))

	catalog, diagnostics, err := LoadCatalogWithDiagnostics(root)
	if err != nil {
		t.Fatalf("LoadCatalogWithDiagnostics: %v", err)
	}
	if len(catalog) != 1 || catalog["apps.good"] == nil {
		t.Fatalf("catalog = %+v, want only valid legacy module", catalog)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one", diagnostics)
	}
	d := diagnostics[0]
	if d.Code != DiagnosticInvalidVersionRange || d.FilePath != badPath || d.ModuleID != "apps.versioned" || d.Message == "" {
		t.Errorf("diagnostic = %+v, want structured invalid range detail", d)
	}

	legacyCatalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(legacyCatalog) != 1 || legacyCatalog["apps.good"] == nil {
		t.Fatalf("legacy LoadCatalog result = %+v", legacyCatalog)
	}
}

func TestLoadCatalogWithDiagnosticsPreservesChocolateyIdentityForParseAndValidationFailures(t *testing.T) {
	tests := []struct {
		name    string
		content string
		code    string
	}{
		{
			name: "strict parse failure",
			content: `{
				"moduleSchemaVersion": 2,
				"id": "apps.git",
				"displayName": "Git",
				"matches": {"chocolatey": ["git.install"]},
				"unexpected": true
			}`,
			code: DiagnosticInvalidJSON,
		},
		{
			name: "validation failure",
			content: strings.Replace(
				strings.Replace(validV2Module, `"matches": {"winget": ["Vendor.Versioned"]}`, `"matches": {"chocolatey": ["git.install"]}`, 1),
				`"versionRange": ">=25 <28"`, `"versionRange": "not-a-range"`, 1,
			),
			code: DiagnosticInvalidVersionRange,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestModule(t, root, "git", tt.content)

			_, diagnostics, err := LoadCatalogWithDiagnostics(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 1 || diagnostics[0].Code != tt.code {
				t.Fatalf("diagnostics = %+v, want one %s", diagnostics, tt.code)
			}
			encoded, err := json.Marshal(diagnostics[0])
			if err != nil {
				t.Fatal(err)
			}
			var identity struct {
				ChocolateyRefs []string `json:"chocolateyRefs"`
			}
			if err := json.Unmarshal(encoded, &identity); err != nil {
				t.Fatal(err)
			}
			if len(identity.ChocolateyRefs) != 1 || identity.ChocolateyRefs[0] != "git.install" {
				t.Fatalf("Chocolatey diagnostic identity = %#v, want [git.install]; diagnostic JSON: %s", identity.ChocolateyRefs, encoded)
			}
		})
	}
}

func TestValidateModuleV2_Rejections(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Module)
		code string
	}{
		{"config requires schema v2", func(m *Module) { m.ModuleSchemaVersion = 0 }, DiagnosticSchemaVersionRequired},
		{"unsupported schema", func(m *Module) { m.ModuleSchemaVersion = 3 }, DiagnosticUnsupportedSchema},
		{"invalid module id", func(m *Module) { m.ID = "Apps.Versioned" }, DiagnosticInvalidID},
		{"invalid detector id", func(m *Module) { m.Config.InstanceDetectors[0].ID = "Bad ID" }, DiagnosticInvalidID},
		{"duplicate detector id", func(m *Module) {
			m.Config.InstanceDetectors = append(m.Config.InstanceDetectors, m.Config.InstanceDetectors[0])
		}, DiagnosticDuplicateID},
		{"unknown detector type", func(m *Module) { m.Config.InstanceDetectors[0].Type = "registry" }, DiagnosticUnknownDetector},
		{"path detector missing glob", func(m *Module) { m.Config.InstanceDetectors[1].Glob = "" }, DiagnosticInvalidDetector},
		{"path detector pattern unanchored", func(m *Module) { m.Config.InstanceDetectors[1].VersionPattern = "App (?P<version>[0-9.]+)" }, DiagnosticInvalidVersionPattern},
		{"path detector missing named capture", func(m *Module) { m.Config.InstanceDetectors[1].VersionPattern = "^App ([0-9.]+)$" }, DiagnosticInvalidVersionPattern},
		{"empty config sets", func(m *Module) { m.Config.Sets = nil }, DiagnosticMissingConfigSet},
		{"invalid set id", func(m *Module) { m.Config.Sets[0].ID = "Preferences!" }, DiagnosticInvalidID},
		{"duplicate set id", func(m *Module) { m.Config.Sets = append(m.Config.Sets, m.Config.Sets[0]) }, DiagnosticDuplicateID},
		{"invalid generation id", func(m *Module) { m.Config.Sets[0].Generations[0].ID = "G 1" }, DiagnosticInvalidID},
		{"duplicate generation id in set", func(m *Module) {
			m.Config.Sets[0].Generations = append(m.Config.Sets[0].Generations, m.Config.Sets[0].Generations[0])
		}, DiagnosticDuplicateID},
		{"zero generation order", func(m *Module) { m.Config.Sets[0].Generations[0].Order = 0 }, DiagnosticInvalidGenerationOrder},
		{"duplicate generation order", func(m *Module) { m.Config.Sets[0].Generations[1].Order = 1 }, DiagnosticInvalidGenerationOrder},
		{"selector has range and pattern", func(m *Module) { m.Config.Sets[0].Generations[0].Matches[0].VersionPattern = "^27$" }, DiagnosticInvalidVersionSelector},
		{"invalid numeric range", func(m *Module) { m.Config.Sets[0].Generations[0].Matches[0].VersionRange = ">=27 banana" }, DiagnosticInvalidVersionRange},
		{"unanchored raw pattern", func(m *Module) { m.Config.Sets[0].Generations[1].Matches[0].VersionPattern = "29" }, DiagnosticInvalidVersionPattern},
		{"unknown placeholder", func(m *Module) {
			m.Config.Sets[0].Generations[0].Capture.Files[0].Source = "${machine.home}\\prefs.json"
		}, DiagnosticInvalidPlaceholder},
		{"portable traversal", func(m *Module) { m.Config.Sets[0].Generations[0].Capture.Files[0].Dest = "../prefs.json" }, DiagnosticUnsafePath},
		{"absolute portable destination", func(m *Module) { m.Config.Sets[0].Generations[0].Capture.Files[0].Dest = `C:\\payload\\prefs.json` }, DiagnosticUnsafePath},
		{"restore source traversal", func(m *Module) { m.Config.Sets[0].Generations[0].Restore[0].Source = "../prefs.json" }, DiagnosticUnsafePath},
		{"unknown generation edge", func(m *Module) { m.Config.Sets[0].Migrations[0].To = "g3" }, DiagnosticUnknownGeneration},
		{"same generation edge", func(m *Module) { m.Config.Sets[0].Migrations[0].To = "g1" }, DiagnosticInvalidMigrationEdge},
		{"backward edge", func(m *Module) { m.Config.Sets[0].Migrations[0].From, m.Config.Sets[0].Migrations[0].To = "g2", "g1" }, DiagnosticInvalidMigrationEdge},
		{"duplicate edge", func(m *Module) {
			m.Config.Sets[0].Migrations = append(m.Config.Sets[0].Migrations, m.Config.Sets[0].Migrations[0])
		}, DiagnosticDuplicateMigrationEdge},
		{"unknown operation", func(m *Module) { m.Config.Sets[0].Migrations[0].Operations[0].Type = "powershell" }, DiagnosticUnknownMigrationOperation},
		{"invalid json-set path", func(m *Module) { m.Config.Sets[0].Migrations[0].Operations[1].JSONPath = "$..theme" }, DiagnosticInvalidMigrationOperation},
		{"invalid json-delete path", func(m *Module) {
			m.Config.Sets[0].Migrations[0].Operations[1] = MigrationOperationDef{Type: "json-delete", Path: "settings.json", JSONPath: "$[*]"}
		}, DiagnosticInvalidMigrationOperation},
		{"json-delete root", func(m *Module) {
			m.Config.Sets[0].Migrations[0].Operations[1] = MigrationOperationDef{Type: "json-delete", Path: "settings.json", JSONPath: "$"}
		}, DiagnosticInvalidMigrationOperation},
		{"invalid json-move source path", func(m *Module) {
			m.Config.Sets[0].Migrations[0].Operations[1] = MigrationOperationDef{Type: "json-move", Path: "settings.json", From: "$..source", To: "$.target"}
		}, DiagnosticInvalidMigrationOperation},
		{"invalid json-move destination path", func(m *Module) {
			m.Config.Sets[0].Migrations[0].Operations[1] = MigrationOperationDef{Type: "json-move", Path: "settings.json", From: "$.source", To: "$[?(@.target)]"}
		}, DiagnosticInvalidMigrationOperation},
		{"json-move root source", func(m *Module) {
			m.Config.Sets[0].Migrations[0].Operations[1] = MigrationOperationDef{Type: "json-move", Path: "settings.json", From: "$", To: "$.target"}
		}, DiagnosticInvalidMigrationOperation},
		{"json-move root destination", func(m *Module) {
			m.Config.Sets[0].Migrations[0].Operations[1] = MigrationOperationDef{Type: "json-move", Path: "settings.json", From: "$.source", To: "$"}
		}, DiagnosticInvalidMigrationOperation},
		{"ini-set value must be string", func(m *Module) {
			m.Config.Sets[0].Migrations[0].Operations[1] = MigrationOperationDef{Type: "ini-set", Path: "settings.ini", Section: "settings", Key: "theme", Value: json.Number("42")}
		}, DiagnosticInvalidMigrationOperation},
		{"absolute migration path", func(m *Module) { m.Config.Sets[0].Migrations[0].Operations[0].Source = `C:\\host\\prefs.json` }, DiagnosticUnsafePath},
		{"volume relative migration path", func(m *Module) { m.Config.Sets[0].Migrations[0].Operations[0].Source = `C:host\prefs.json` }, DiagnosticUnsafePath},
		{"embedded host expansion migration path", func(m *Module) { m.Config.Sets[0].Migrations[0].Operations[0].Source = `safe$HOME\prefs.json` }, DiagnosticUnsafePath},
		{"migration missing validation", func(m *Module) { m.Config.Sets[0].Migrations[0].Validate = nil }, DiagnosticMissingMigrationValidation},
		{"unknown validation", func(m *Module) { m.Config.Sets[0].Migrations[0].Validate[0].Type = "command" }, DiagnosticUnknownValidation},
		{"validation traversal", func(m *Module) { m.Config.Sets[0].Generations[0].Validate[0].Path = "../prefs.json" }, DiagnosticUnsafePath},
		{"validation invalid json path", func(m *Module) {
			m.Config.Sets[0].Generations[0].Validate[0] = ValidationDef{Type: "json-path-exists", Path: "settings.json", JSONPath: "$[*]"}
		}, DiagnosticInvalidValidation},
		{"validation non-canonical ini section", func(m *Module) {
			m.Config.Sets[0].Generations[0].Validate[0] = ValidationDef{Type: "ini-key-exists", Path: "settings.ini", Section: " settings", Key: "theme"}
		}, DiagnosticInvalidValidation},
		{"validation invalid ini key", func(m *Module) {
			m.Config.Sets[0].Generations[0].Validate[0] = ValidationDef{Type: "ini-key-exists", Path: "settings.ini", Section: "settings", Key: "theme=alternate"}
		}, DiagnosticInvalidValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := mustParseTestModule(t, validV2Module)
			tt.edit(mod)
			err := validateModule(mod, "module.jsonc")
			if err == nil {
				t.Fatalf("validateModule returned nil, want %s", tt.code)
			}
			if got := DiagnosticCode(err); got != tt.code {
				t.Errorf("DiagnosticCode(%v) = %q, want %q", err, got, tt.code)
			}
		})
	}
}

func TestValidateModuleV2_RejectsAmbiguousMigrationRoutes(t *testing.T) {
	mod := mustParseTestModule(t, validV2Module)
	set := &mod.Config.Sets[0]
	set.Generations = append(set.Generations, GenerationDef{
		ID:       "g3",
		Order:    3,
		Matches:  []VersionSelectorDef{{VersionRange: ">=30"}},
		Validate: []ValidationDef{{Type: "file-exists", Path: "settings.json"}},
	})
	set.Migrations = append(set.Migrations,
		MigrationEdgeDef{From: "g2", To: "g3", Operations: []MigrationOperationDef{{Type: "file-copy", Source: "settings.json", Target: "next.json"}}, Validate: []ValidationDef{{Type: "file-exists", Path: "next.json"}}},
		MigrationEdgeDef{From: "g1", To: "g3", Operations: []MigrationOperationDef{{Type: "file-copy", Source: "prefs/prefs.json", Target: "next.json"}}, Validate: []ValidationDef{{Type: "file-exists", Path: "next.json"}}},
	)

	err := validateModule(mod, "module.jsonc")
	if err == nil || DiagnosticCode(err) != DiagnosticAmbiguousMigrationRoute {
		t.Fatalf("validateModule error = %v, want %s", err, DiagnosticAmbiguousMigrationRoute)
	}
}

func TestValidateModuleV2_RejectsMigrationCycle(t *testing.T) {
	mod := mustParseTestModule(t, validV2Module)
	mod.Config.Sets[0].Migrations = append(mod.Config.Sets[0].Migrations, MigrationEdgeDef{
		From:       "g2",
		To:         "g1",
		Operations: []MigrationOperationDef{{Type: "file-copy", Source: "settings.json", Target: "prefs/prefs.json"}},
		Validate:   []ValidationDef{{Type: "file-exists", Path: "prefs/prefs.json"}},
	})

	err := validateModule(mod, "module.jsonc")
	if err == nil || DiagnosticCode(err) != DiagnosticMigrationCycle {
		t.Fatalf("validateModule error = %v, want %s", err, DiagnosticMigrationCycle)
	}
}

func mustParseTestModule(t *testing.T, content string) *Module {
	t.Helper()
	mod, err := ParseModuleJSON([]byte(content))
	if err != nil {
		t.Fatalf("ParseModuleJSON: %v", err)
	}
	return mod
}
