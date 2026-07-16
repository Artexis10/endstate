// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestLoadGenerationHistory_InitialRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generation-history.json")
	content := `{
  "schemaVersion": 1,
  "generations": [
    {
      "moduleId": "apps.foo",
      "configSetId": "preferences",
      "generationId": "g1",
      "fingerprints": ["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadGenerationHistory(path)
	if err != nil {
		t.Fatalf("LoadGenerationHistory() error = %v", err)
	}
	if history.SchemaVersion != 1 || len(history.Generations) != 1 {
		t.Fatalf("LoadGenerationHistory() = %+v, want one schema-v1 entry", history)
	}
	if got := history.Generations[0].Identity(); got != "apps.foo/preferences/g1" {
		t.Fatalf("Identity() = %q, want apps.foo/preferences/g1", got)
	}
}

func TestLoadGenerationHistory_StrictJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
		code    string
	}{
		{
			name:    "malformed",
			content: `{"schemaVersion":1,"generations":[}`,
			code:    HistoryDiagnosticInvalidJSON,
		},
		{
			name:    "unknown field",
			content: `{"schemaVersion":1,"generations":[],"comment":"not schema"}`,
			code:    HistoryDiagnosticInvalidJSON,
		},
		{
			name:    "trailing value",
			content: `{"schemaVersion":1,"generations":[]} {}`,
			code:    HistoryDiagnosticInvalidJSON,
		},
		{
			name:    "unsupported schema",
			content: `{"schemaVersion":2,"generations":[]}`,
			code:    HistoryDiagnosticUnsupportedSchema,
		},
		{
			name:    "missing generations",
			content: `{"schemaVersion":1}`,
			code:    HistoryDiagnosticMissingGenerations,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "generation-history.json")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadGenerationHistory(path)
			if got := GenerationHistoryDiagnosticCode(err); got != tt.code {
				t.Fatalf("diagnostic code = %q for error %v, want %q", got, err, tt.code)
			}
		})
	}
}

func TestValidateGenerationHistory_RequiresSortedUniqueStableData(t *testing.T) {
	fpA := testFingerprint("a")
	fpB := testFingerprint("b")
	valid := func() *GenerationHistory {
		return &GenerationHistory{
			SchemaVersion: 1,
			Generations: []GenerationHistoryEntry{
				{ModuleID: "apps.alpha", ConfigSetID: "preferences", GenerationID: "g1", Fingerprints: []string{fpA, fpB}},
				{ModuleID: "apps.alpha", ConfigSetID: "presets", GenerationID: "g1", Fingerprints: []string{fpA}},
				{ModuleID: "apps.beta", ConfigSetID: "preferences", GenerationID: "g1", Fingerprints: []string{fpA}},
			},
		}
	}

	tests := []struct {
		name   string
		mutate func(*GenerationHistory)
		code   string
	}{
		{"invalid module id", func(h *GenerationHistory) { h.Generations[0].ModuleID = "Apps.Alpha" }, HistoryDiagnosticInvalidIdentity},
		{"invalid config set id", func(h *GenerationHistory) { h.Generations[0].ConfigSetID = "user prefs" }, HistoryDiagnosticInvalidIdentity},
		{"invalid generation id", func(h *GenerationHistory) { h.Generations[0].GenerationID = "1g" }, HistoryDiagnosticInvalidIdentity},
		{"missing fingerprints", func(h *GenerationHistory) { h.Generations[0].Fingerprints = nil }, HistoryDiagnosticMissingFingerprint},
		{"malformed fingerprint", func(h *GenerationHistory) { h.Generations[0].Fingerprints[0] = strings.ToUpper(fpA) }, HistoryDiagnosticInvalidFingerprint},
		{"duplicate fingerprint", func(h *GenerationHistory) { h.Generations[0].Fingerprints[1] = fpA }, HistoryDiagnosticDuplicateFingerprint},
		{"unsorted fingerprints", func(h *GenerationHistory) { h.Generations[0].Fingerprints = []string{fpB, fpA} }, HistoryDiagnosticUnsortedFingerprint},
		{"duplicate identity", func(h *GenerationHistory) { h.Generations[1] = h.Generations[0] }, HistoryDiagnosticDuplicateIdentity},
		{"unsorted identities", func(h *GenerationHistory) { h.Generations[0], h.Generations[1] = h.Generations[1], h.Generations[0] }, HistoryDiagnosticUnsortedIdentity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history := valid()
			tt.mutate(history)
			err := ValidateGenerationHistory(history)
			if got := GenerationHistoryDiagnosticCode(err); got != tt.code {
				t.Fatalf("diagnostic code = %q for error %v, want %q", got, err, tt.code)
			}
		})
	}
}

func TestValidateGenerationHistory_AllowsEmptySortedLedger(t *testing.T) {
	history := &GenerationHistory{SchemaVersion: 1, Generations: []GenerationHistoryEntry{}}
	if err := ValidateGenerationHistory(history); err != nil {
		t.Fatalf("ValidateGenerationHistory() error = %v", err)
	}
}

func TestValidateRepositoryGenerationHistory_InitialAndCosmeticRegistration(t *testing.T) {
	left, err := ParseModuleJSON([]byte(testHistoryModuleJSON(false)))
	if err != nil {
		t.Fatal(err)
	}
	right, err := ParseModuleJSON([]byte(testHistoryModuleJSON(true)))
	if err != nil {
		t.Fatal(err)
	}
	leftFingerprint := left.Config.Sets[0].Generations[0].Fingerprint
	rightFingerprint := right.Config.Sets[0].Generations[0].Fingerprint
	if leftFingerprint != rightFingerprint {
		t.Fatalf("cosmetic edits changed fingerprint: %s != %s", leftFingerprint, rightFingerprint)
	}
	history := testHistory(testHistoryEntry("apps.foo", "preferences", "g1", leftFingerprint))

	for _, mod := range []*Module{left, right} {
		if err := ValidateRepositoryGenerationHistory(map[string]*Module{mod.ID: mod}, history); err != nil {
			t.Fatalf("ValidateRepositoryGenerationHistory() error = %v", err)
		}
	}
}

func TestValidateRepositoryGenerationHistory_RejectsSilentSemanticReuse(t *testing.T) {
	oldFingerprint := fingerprintGeneration(t, GenerationDef{ID: "g1", Order: 1})
	changed := GenerationDef{ID: "g1", Order: 1, RequiresAppClosed: true}
	changed.Fingerprint = fingerprintGeneration(t, changed)
	history := testHistory(testHistoryEntry("apps.foo", "preferences", "g1", oldFingerprint))

	err := ValidateRepositoryGenerationHistory(testCatalog("apps.foo", "preferences", changed), history)
	if got := GenerationHistoryDiagnosticCode(err); got != HistoryDiagnosticCurrentFingerprintMissing {
		t.Fatalf("diagnostic code = %q for error %v, want %q", got, err, HistoryDiagnosticCurrentFingerprintMissing)
	}
}

func TestValidateRepositoryGenerationHistory_AllowsExplicitAcceptedEvolution(t *testing.T) {
	oldFingerprint := fingerprintGeneration(t, GenerationDef{ID: "g1", Order: 1})
	current := GenerationDef{ID: "g1", Order: 1, RequiresAppClosed: true}
	current.Fingerprint = fingerprintGeneration(t, current)
	current.AcceptsSourceFingerprints = []string{oldFingerprint}
	history := testHistory(testHistoryEntry("apps.foo", "preferences", "g1", oldFingerprint, current.Fingerprint))

	if err := ValidateRepositoryGenerationHistory(testCatalog("apps.foo", "preferences", current), history); err != nil {
		t.Fatalf("ValidateRepositoryGenerationHistory() error = %v", err)
	}
}

func TestValidateRepositoryGenerationHistory_AllowsNewGenerationAndRetiredIdentity(t *testing.T) {
	g1 := GenerationDef{ID: "g1", Order: 1}
	g1.Fingerprint = fingerprintGeneration(t, g1)
	g2 := GenerationDef{ID: "g2", Order: 2, RequiresAppClosed: true}
	g2.Fingerprint = fingerprintGeneration(t, g2)
	history := testHistory(
		testHistoryEntry("apps.foo", "preferences", "g1", g1.Fingerprint),
		testHistoryEntry("apps.foo", "preferences", "g2", g2.Fingerprint),
		testHistoryEntry("apps.retired", "preferences", "g1", testFingerprint("f")),
	)
	catalog := testCatalog("apps.foo", "preferences", g1, g2)

	if err := ValidateRepositoryGenerationHistory(catalog, history); err != nil {
		t.Fatalf("ValidateRepositoryGenerationHistory() error = %v", err)
	}
	if err := ValidateRepositoryGenerationHistory(map[string]*Module{}, testHistory(history.Generations[2])); err != nil {
		t.Fatalf("retired ledger identity error = %v", err)
	}
}

func TestValidateRepositoryGenerationHistory_RequiresCurrentRegistration(t *testing.T) {
	current := GenerationDef{ID: "g1", Order: 1}
	current.Fingerprint = fingerprintGeneration(t, current)

	tests := []struct {
		name    string
		history *GenerationHistory
		code    string
	}{
		{
			name:    "missing current identity",
			history: testHistory(),
			code:    HistoryDiagnosticCurrentIdentityMissing,
		},
		{
			name:    "unknown current fingerprint",
			history: testHistory(testHistoryEntry("apps.foo", "preferences", "g1", testFingerprint("f"))),
			code:    HistoryDiagnosticCurrentFingerprintMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRepositoryGenerationHistory(testCatalog("apps.foo", "preferences", current), tt.history)
			if got := GenerationHistoryDiagnosticCode(err); got != tt.code {
				t.Fatalf("diagnostic code = %q for error %v, want %q", got, err, tt.code)
			}
		})
	}
}

func TestValidateRepositoryGenerationHistory_ValidatesAcceptanceDeclarations(t *testing.T) {
	current := GenerationDef{ID: "g1", Order: 1, RequiresAppClosed: true}
	current.Fingerprint = fingerprintGeneration(t, current)
	oldFingerprint := fingerprintGeneration(t, GenerationDef{ID: "g1", Order: 1})
	otherFingerprint := testFingerprint("f")
	baseHistory := func() *GenerationHistory {
		return testHistory(
			testHistoryEntry("apps.foo", "preferences", "g1", oldFingerprint, current.Fingerprint),
			testHistoryEntry("apps.foo", "presets", "g1", otherFingerprint),
		)
	}

	tests := []struct {
		name     string
		accepted []string
		history  func() *GenerationHistory
		code     string
	}{
		{"older fingerprint not accepted", nil, baseHistory, HistoryDiagnosticHistoricalFingerprintNotAccepted},
		{"malformed accepted fingerprint", []string{"ABC"}, baseHistory, HistoryDiagnosticInvalidAcceptedFingerprint},
		{"duplicate accepted fingerprint", []string{oldFingerprint, oldFingerprint}, baseHistory, HistoryDiagnosticDuplicateAcceptedFingerprint},
		{"current fingerprint accepted", []string{current.Fingerprint}, baseHistory, HistoryDiagnosticCurrentFingerprintAccepted},
		{"accepted fingerprint not recorded", []string{testFingerprint("e")}, baseHistory, HistoryDiagnosticAcceptedFingerprintNotRecorded},
		{"accepted fingerprint belongs to other identity", []string{otherFingerprint}, baseHistory, HistoryDiagnosticAcceptedFingerprintNotRecorded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generation := current
			generation.AcceptsSourceFingerprints = tt.accepted
			err := ValidateRepositoryGenerationHistory(testCatalog("apps.foo", "preferences", generation), tt.history())
			if got := GenerationHistoryDiagnosticCode(err); got != tt.code {
				t.Fatalf("diagnostic code = %q for error %v, want %q", got, err, tt.code)
			}
		})
	}
}

func TestRepositoryGenerationHistory(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	catalog, diagnostics, err := LoadCatalogWithDiagnostics(filepath.Join(repoRoot, "modules", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("repository module diagnostics = %+v", diagnostics)
	}
	history, err := LoadGenerationHistory(filepath.Join(repoRoot, "modules", "generation-history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRepositoryGenerationHistory(catalog, history); err != nil {
		t.Fatal(err)
	}
}

func testHistory(entries ...GenerationHistoryEntry) *GenerationHistory {
	if entries == nil {
		entries = []GenerationHistoryEntry{}
	}
	sort.Slice(entries, func(i, j int) bool { return compareHistoryIdentity(entries[i], entries[j]) < 0 })
	return &GenerationHistory{SchemaVersion: 1, Generations: entries}
}

func testHistoryEntry(moduleID, configSetID, generationID string, fingerprints ...string) GenerationHistoryEntry {
	sort.Strings(fingerprints)
	return GenerationHistoryEntry{
		ModuleID:     moduleID,
		ConfigSetID:  configSetID,
		GenerationID: generationID,
		Fingerprints: fingerprints,
	}
}

func testCatalog(moduleID, configSetID string, generations ...GenerationDef) map[string]*Module {
	return map[string]*Module{
		moduleID: {
			ModuleSchemaVersion: 2,
			ID:                  moduleID,
			Config: &ConfigDef{Sets: []ConfigSetDef{{
				ID:          configSetID,
				Generations: generations,
			}}},
		},
	}
}

func fingerprintGeneration(t *testing.T, generation GenerationDef) string {
	t.Helper()
	fingerprint, err := ComputeGenerationFingerprint(generation)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func testHistoryModuleJSON(cosmetic bool) string {
	if cosmetic {
		return `{
  // This edit changes only comments, whitespace, and property order.
  "config": {"sets": [{"generations": [{"validate": [{"path": "prefs.json", "type": "file-exists"}], "order": 1, "id": "g1"}], "id": "preferences"}], "instanceDetectors": [{"type": "package", "id": "installed"}]},
  "matches": {"winget": ["Vendor.Foo"]},
  "displayName": "Foo", "id": "apps.foo", "moduleSchemaVersion": 2
}`
	}
	return `{"moduleSchemaVersion":2,"id":"apps.foo","displayName":"Foo","matches":{"winget":["Vendor.Foo"]},"config":{"instanceDetectors":[{"id":"installed","type":"package"}],"sets":[{"id":"preferences","generations":[{"id":"g1","order":1,"validate":[{"type":"file-exists","path":"prefs.json"}]}]}]}}`
}

func testFingerprint(digit string) string {
	return strings.Repeat(digit, 64)
}
