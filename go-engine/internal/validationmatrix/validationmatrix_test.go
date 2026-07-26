// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestLoadCatalogRejectsMissingDuplicateStaleAndInvalidSidecars(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		code  string
	}{
		{
			name: "missing sidecar",
			setup: func(t *testing.T, root string) {
				writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
			},
			code: CodeMissingSidecar,
		},
		{
			name: "duplicate module identity",
			setup: func(t *testing.T, root string) {
				alpha := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
				writeValidation(t, root, "alpha", validV1Validation("apps.alpha", alpha.Revision))
				beta := writeModule(t, root, "beta", schemaV1Module("apps.beta", true))
				record := validV1Validation("apps.alpha", beta.Revision)
				writeValidation(t, root, "beta", record)
			},
			code: CodeDuplicateSidecar,
		},
		{
			name: "stale module revision",
			setup: func(t *testing.T, root string) {
				writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
				record := validV1Validation("apps.alpha", strings.Repeat("a", 64))
				writeValidation(t, root, "alpha", record)
			},
			code: CodeStaleSidecar,
		},
		{
			name: "invalid jsonc",
			setup: func(t *testing.T, root string) {
				writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
				writeRawValidation(t, root, "alpha", []byte(`{"schemaVersion": 1, // comment\n`))
			},
			code: CodeInvalidSidecar,
		},
		{
			name: "unknown scenario kind",
			setup: func(t *testing.T, root string) {
				mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
				record := validV1Validation("apps.alpha", mod.Revision)
				record.Synthetic.Scenarios[0].Mode = ScenarioKind("future-contract")
				writeValidation(t, root, "alpha", record)
			},
			code: CodeUnknownScenarioKind,
		},
		{
			name: "unknown live mode",
			setup: func(t *testing.T, root string) {
				mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
				record := validV1Validation("apps.alpha", mod.Revision)
				record.Live.Mode = LiveMode("future")
				writeValidation(t, root, "alpha", record)
			},
			code: CodeUnknownLiveMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.setup(t, root)
			_, err := LoadCatalog(root, now)
			if got := ErrorCode(err); got != tt.code {
				t.Fatalf("LoadCatalog error = %v (code %q), want code %q", err, got, tt.code)
			}
		})
	}
}

func TestLoadedValidationRecordPinsImmutableSourceSnapshot(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	writeValidation(t, root, "alpha", validV1Validation("apps.alpha", mod.Revision))

	catalog, err := LoadCatalog(root, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	record := catalog.Records["apps.alpha"]
	before := record.SourceSnapshot()
	if len(before) == 0 {
		t.Fatal("validation source snapshot is empty")
	}
	if err := os.WriteFile(record.FilePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := record.SourceSnapshot(); !bytes.Equal(got, before) {
		t.Fatal("validation source snapshot changed after source edit")
	}
	before[0] ^= 0xff
	if got := record.SourceSnapshot(); bytes.Equal(got, before) {
		t.Fatal("SourceSnapshot returned mutable backing storage")
	}
}

func TestLoadCatalogUsesCanonicalJSONCParser(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "urls", strings.Replace(schemaV1Module("apps.urls", true), `"displayName": "Fixture"`, `"displayName": "https://example.test/path//literal"`, 1))
	record := validV1Validation("apps.urls", mod.Revision)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte("// validation metadata\n"), data...)
	writeRawValidation(t, root, "urls", data)

	catalog, err := LoadCatalog(root, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("LoadCatalog returned %v", err)
	}
	if got := catalog.Modules["apps.urls"].DisplayName; got != "https://example.test/path//literal" {
		t.Fatalf("module display name = %q", got)
	}
}

func TestValidationScenarioMinimaAndModuleClassification(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		module string
		mutate func(*ValidationRecord)
		code   string
	}{
		{
			name:   "roundtrip omits provenance assertion",
			module: schemaV1Module("apps.alpha", true),
			mutate: func(record *ValidationRecord) {
				delete(record.Synthetic.Scenarios[0].MinimumAssertions, AssertionProvenance)
			},
			code: CodeMissingAssertionMinimum,
		},
		{
			name:   "roundtrip assertion is zero",
			module: schemaV1Module("apps.alpha", true),
			mutate: func(record *ValidationRecord) { record.Synthetic.Scenarios[0].MinimumAssertions[AssertionRevert] = 0 },
			code:   CodeMissingAssertionMinimum,
		},
		{
			name:   "config module is mislabeled install only",
			module: schemaV1Module("apps.alpha", true),
			mutate: func(record *ValidationRecord) { record.Synthetic.Scenarios[0] = installScenario("default-install") },
			code:   CodeInvalidClassification,
		},
		{
			name:   "install only lacks a verifier",
			module: schemaV1Module("apps.alpha", false),
			mutate: func(record *ValidationRecord) { record.Synthetic.Scenarios[0] = installScenario("default-install") },
			code:   CodeInvalidClassification,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "alpha", tt.module)
			record := validV1Validation("apps.alpha", mod.Revision)
			tt.mutate(&record)
			writeValidation(t, root, "alpha", record)
			_, err := LoadCatalog(root, now)
			if got := ErrorCode(err); got != tt.code {
				t.Fatalf("LoadCatalog error = %v (code %q), want code %q", err, got, tt.code)
			}
		})
	}
}

func TestScenarioIDAcceptsAnyNonBlankUniqueValue(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	record := validV1Validation("apps.alpha", mod.Revision)
	record.Synthetic.Scenarios[0].ID = "Default V1 / Windows"
	writeValidation(t, root, "alpha", record)

	if _, err := LoadCatalog(root, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("LoadCatalog returned %v", err)
	}
}

func TestSchemaV2RequiresEveryGenerationFingerprintAndMigration(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*ValidationRecord)
		code   string
	}{
		{
			name: "missing accepted fingerprint alternative",
			mutate: func(record *ValidationRecord) {
				record.Synthetic.Scenarios = append(record.Synthetic.Scenarios[:1], record.Synthetic.Scenarios[2:]...)
			},
			code: CodeMissingV2Scenario,
		},
		{
			name: "missing migration edge",
			mutate: func(record *ValidationRecord) {
				record.Synthetic.Scenarios = record.Synthetic.Scenarios[:3]
			},
			code: CodeMissingV2Scenario,
		},
		{
			name: "duplicate generation alternative",
			mutate: func(record *ValidationRecord) {
				record.Synthetic.Scenarios = append(record.Synthetic.Scenarios, record.Synthetic.Scenarios[0])
				record.Synthetic.Scenarios[len(record.Synthetic.Scenarios)-1].ID = "duplicate"
			},
			code: CodeDuplicateScenario,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "v2", schemaV2Module("apps.v2"))
			record := validV2Validation(t, mod)
			tt.mutate(&record)
			writeValidation(t, root, "v2", record)
			_, err := LoadCatalog(root, now)
			if got := ErrorCode(err); got != tt.code {
				t.Fatalf("LoadCatalog error = %v (code %q), want code %q", err, got, tt.code)
			}
		})
	}

	t.Run("complete schema v2 classification", func(t *testing.T) {
		root := t.TempDir()
		mod := writeModule(t, root, "v2", schemaV2Module("apps.v2"))
		writeValidation(t, root, "v2", validV2Validation(t, mod))
		if _, err := LoadCatalog(root, now); err != nil {
			t.Fatalf("LoadCatalog returned %v", err)
		}
	})
}

func TestQuarantineMustBeScopedOwnedIssueBoundAndUnexpired(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	valid := Quarantine{
		ProofLevel:         ProofLiveConfigRoundtrip,
		OS:                 "windows",
		RunnerImage:        "windows-latest",
		FailureFingerprint: strings.Repeat("b", 64),
		IssueURL:           "https://github.com/Artexis10/endstate/issues/123",
		ReasonCode:         "upstream-installer-flake",
		Owner:              "@maintainer",
		ExpiresOn:          "2026-08-01",
	}
	tests := []struct {
		name   string
		mutate func(*Quarantine)
	}{
		{"unscoped", func(q *Quarantine) { q.ProofLevel = "" }},
		{"ownerless", func(q *Quarantine) { q.Owner = "" }},
		{"missing issue", func(q *Quarantine) { q.IssueURL = "" }},
		{"malformed fingerprint", func(q *Quarantine) { q.FailureFingerprint = "abc" }},
		{"permanent", func(q *Quarantine) { q.ExpiresOn = "" }},
		{"expired", func(q *Quarantine) { q.ExpiresOn = "2026-07-21" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
			record := validV1Validation("apps.alpha", mod.Revision)
			quarantine := valid
			tt.mutate(&quarantine)
			record.Quarantines = []Quarantine{quarantine}
			writeValidation(t, root, "alpha", record)
			_, err := LoadCatalog(root, now)
			if got := ErrorCode(err); got != CodeInvalidQuarantine {
				t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeInvalidQuarantine)
			}
		})
	}
}

func TestLoadCatalogRejectsMismatchedAndUnknownSidecarFields(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	t.Run("mismatched sibling identity", func(t *testing.T) {
		root := t.TempDir()
		mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
		record := validV1Validation("apps.other", mod.Revision)
		writeValidation(t, root, "alpha", record)
		_, err := LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeMismatchedSidecar {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeMismatchedSidecar)
		}
	})

	t.Run("unknown nested field", func(t *testing.T) {
		root := t.TempDir()
		mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
		record := validV1Validation("apps.alpha", mod.Revision)
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.Replace(string(data), `"fixture":{"type":"auto"}`, `"fixture":{"type":"auto","future":true}`, 1))
		writeRawValidation(t, root, "alpha", data)
		_, err = LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeInvalidSidecar {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeInvalidSidecar)
		}
	})
}

func TestFixtureContract(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		fixture Fixture
		wantErr bool
	}{
		{"auto", Fixture{Type: FixtureAuto}, false},
		{"auto with path", Fixture{Type: FixtureAuto, Path: "tests/fixture.json"}, true},
		{"declarative", Fixture{Type: FixtureDeclarative, Path: "tests/fixtures/module.json", SHA256: strings.Repeat("a", 64)}, false},
		{"declarative without hash", Fixture{Type: FixtureDeclarative, Path: "tests/fixtures/module.json"}, true},
		{"declarative with traversal", Fixture{Type: FixtureDeclarative, Path: "../fixture.json", SHA256: strings.Repeat("a", 64)}, true},
		{"declarative with uppercase hash", Fixture{Type: FixtureDeclarative, Path: "tests/fixture.json", SHA256: strings.Repeat("A", 64)}, true},
		{"unknown type", Fixture{Type: FixtureType("script")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
			record := validV1Validation("apps.alpha", mod.Revision)
			record.Synthetic.Scenarios[0].Fixture = tt.fixture
			writeValidation(t, root, "alpha", record)
			_, err := LoadCatalog(root, now)
			if tt.wantErr && ErrorCode(err) != CodeInvalidFixture {
				t.Fatalf("LoadCatalog error = %v, want %q", err, CodeInvalidFixture)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("LoadCatalog returned %v", err)
			}
		})
	}
}

func TestPortableRepositoryRelativePathRejectsHostRoots(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "tests/fixtures/module.json", want: true},
		{path: `tests\fixtures\module.json`, want: true},
		{path: `C:\outside.json`, want: false},
		{path: "C:/outside.json", want: false},
		{path: `\\server\share\outside.json`, want: false},
		{path: "//server/share/outside.json", want: false},
		{path: `/outside.json`, want: false},
		{path: `\outside.json`, want: false},
		{path: "../outside.json", want: false},
		{path: "fixtures//outside.json", want: false},
		{path: "fixtures/./outside.json", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isPortableRepositoryRelativePath(tt.path); got != tt.want {
				t.Fatalf("isPortableRepositoryRelativePath(%q) = %t, want %t", tt.path, got, tt.want)
			}
		})
	}
}

func TestLivePolicyContract(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		live    LivePolicy
		wantErr bool
	}{
		{"candidate", nonHostedLivePolicy(LiveCandidate), false},
		{"blocked", nonHostedLivePolicy(LiveBlocked), false},
		{"lab", nonHostedLivePolicy(LiveLab), false},
		{"manual", nonHostedLivePolicy(LiveManual), false},
		{"not applicable", nonHostedLivePolicy(LiveNotApplicable), false},
		{"unknown mode", LivePolicy{Mode: LiveMode("future")}, true},
		{"invalid reason code", LivePolicy{Mode: LiveBlocked, ReasonCode: "UPSTREAM_FLAKE", Explanation: "blocked"}, true},
		{"non-hosted execution policy", LivePolicy{Mode: LiveCandidate, ReasonCode: "candidate", Explanation: "candidate", Driver: "winget"}, true},
		{"hosted live install", hostedInstallPolicy(), false},
		{"hosted missing reference", func() LivePolicy { p := hostedInstallPolicy(); p.Ref = ""; return p }(), true},
		{"hosted named seed without trust hash", func() LivePolicy { p := hostedInstallPolicy(); p.Seed = "seed.ps1"; return p }(), true},
		{"hosted hash without seed", func() LivePolicy {
			p := hostedInstallPolicy()
			p.Trust = &TrustHashes{SeedSHA256: strings.Repeat("a", 64)}
			return p
		}(), true},
		{"hosted config roundtrip", hostedConfigPolicy(), false},
		{"hosted config missing comparator", func() LivePolicy { p := hostedConfigPolicy(); p.Comparator = ""; return p }(), true},
		{"hosted config malformed trust", func() LivePolicy { p := hostedConfigPolicy(); p.Trust.SeedSHA256 = strings.Repeat("A", 64); return p }(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
			record := validV1Validation("apps.alpha", mod.Revision)
			record.Live = tt.live
			writeValidation(t, root, "alpha", record)
			_, err := LoadCatalog(root, now)
			wantCode := CodeInvalidLivePolicy
			if tt.live.Mode == LiveMode("future") {
				wantCode = CodeUnknownLiveMode
			}
			if tt.wantErr && ErrorCode(err) != wantCode {
				t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, ErrorCode(err), wantCode)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("LoadCatalog returned %v", err)
			}
		})
	}
}

func TestOneWayScenarioReviewContract(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	validReview := func(scenario map[string]any) {
		scenario["review"] = map[string]any{
			"decision":   "approved-one-way",
			"reasonCode": "vendor-format-is-export-only",
			"reviewer":   "@module-owner",
			"reviewedOn": "2026-07-21",
			"evidence":   "The vendor documents export without a compatible import path.",
		}
	}
	tests := []struct {
		name     string
		mode     ScenarioKind
		module   string
		mutate   func(map[string]any)
		wantCode string
	}{
		{name: "valid capture review", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), mutate: validReview},
		{name: "valid restore review", mode: ScenarioRestoreContract, module: restoreOnlyModule("apps.fixture"), mutate: validReview},
		{name: "missing review", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), wantCode: "invalid_one_way_review"},
		{name: "future review", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), mutate: func(scenario map[string]any) {
			validReview(scenario)
			scenario["review"].(map[string]any)["reviewedOn"] = "2026-07-23"
		}, wantCode: "invalid_one_way_review"},
		{name: "malformed review date", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), mutate: func(scenario map[string]any) {
			validReview(scenario)
			scenario["review"].(map[string]any)["reviewedOn"] = "2026-7-21"
		}, wantCode: "invalid_one_way_review"},
		{name: "blank reason", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), mutate: func(scenario map[string]any) {
			validReview(scenario)
			scenario["review"].(map[string]any)["reasonCode"] = " "
		}, wantCode: "invalid_one_way_review"},
		{name: "blank reviewer", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), mutate: func(scenario map[string]any) {
			validReview(scenario)
			scenario["review"].(map[string]any)["reviewer"] = " "
		}, wantCode: "invalid_one_way_review"},
		{name: "blank evidence", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), mutate: func(scenario map[string]any) {
			validReview(scenario)
			scenario["review"].(map[string]any)["evidence"] = " "
		}, wantCode: "invalid_one_way_review"},
		{name: "wrong decision", mode: ScenarioCaptureContract, module: captureOnlyModule("apps.fixture"), mutate: func(scenario map[string]any) {
			validReview(scenario)
			scenario["review"].(map[string]any)["decision"] = "pending"
		}, wantCode: "invalid_one_way_review"},
		{name: "review forbidden on config scenario", mode: ScenarioConfigRoundtripV1, module: schemaV1Module("apps.fixture", true), mutate: validReview, wantCode: "invalid_one_way_review"},
		{name: "review forbidden on install scenario", mode: ScenarioInstallContract, module: installOnlyModule("apps.fixture"), mutate: validReview, wantCode: "invalid_one_way_review"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "fixture", tt.module)
			record := validV1Validation(mod.ID, mod.Revision)
			switch tt.mode {
			case ScenarioCaptureContract:
				record.Synthetic.Scenarios = []Scenario{captureScenario("one-way")}
			case ScenarioRestoreContract:
				record.Synthetic.Scenarios = []Scenario{restoreScenario("one-way")}
			case ScenarioInstallContract:
				record.Synthetic.Scenarios = []Scenario{installScenario("install")}
			}
			writeValidationWithMutation(t, root, "fixture", record, func(document map[string]any) {
				delete(firstScenarioJSON(t, document), "review")
				if tt.mutate != nil {
					tt.mutate(firstScenarioJSON(t, document))
				}
			})
			_, err := LoadCatalog(root, now)
			if got := ErrorCode(err); got != tt.wantCode {
				t.Fatalf("LoadCatalog error = %v (code %q), want code %q", err, got, tt.wantCode)
			}
		})
	}
}

func TestSchemaV2ExpectationIdentityStrategies(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	setLiteral := func(expected map[string]any) {
		expected["identityMode"] = "literal"
		expected["captureId"] = "capture"
		expected["instanceId"] = "default"
		delete(expected, "detectorId")
	}
	setDerived := func(expected map[string]any) {
		expected["identityMode"] = "derived-from-fixture"
		expected["detectorId"] = "installed-package"
		delete(expected, "captureId")
		delete(expected, "instanceId")
	}
	tests := []struct {
		name     string
		mutate   func(map[string]any)
		wantCode string
	}{
		{name: "literal", mutate: setLiteral},
		{name: "derived from declared detector", mutate: setDerived},
		{name: "missing mode", mutate: func(expected map[string]any) { delete(expected, "identityMode") }, wantCode: "invalid_schema_v2_identity"},
		{name: "unknown mode", mutate: func(expected map[string]any) { setLiteral(expected); expected["identityMode"] = "computed" }, wantCode: "invalid_schema_v2_identity"},
		{name: "literal missing capture id", mutate: func(expected map[string]any) { setLiteral(expected); delete(expected, "captureId") }, wantCode: "invalid_schema_v2_identity"},
		{name: "literal blank capture id", mutate: func(expected map[string]any) { setLiteral(expected); expected["captureId"] = " " }, wantCode: "invalid_schema_v2_identity"},
		{name: "literal missing instance id", mutate: func(expected map[string]any) { setLiteral(expected); delete(expected, "instanceId") }, wantCode: "invalid_schema_v2_identity"},
		{name: "literal blank instance id", mutate: func(expected map[string]any) { setLiteral(expected); expected["instanceId"] = " " }, wantCode: "invalid_schema_v2_identity"},
		{name: "literal forbids detector", mutate: func(expected map[string]any) { setLiteral(expected); expected["detectorId"] = "installed-package" }, wantCode: "invalid_schema_v2_identity"},
		{name: "literal forbids empty detector field", mutate: func(expected map[string]any) { setLiteral(expected); expected["detectorId"] = "" }, wantCode: "invalid_schema_v2_identity"},
		{name: "derived missing detector", mutate: func(expected map[string]any) { setDerived(expected); delete(expected, "detectorId") }, wantCode: "invalid_schema_v2_identity"},
		{name: "derived blank detector", mutate: func(expected map[string]any) { setDerived(expected); expected["detectorId"] = " " }, wantCode: "invalid_schema_v2_identity"},
		{name: "derived rejects unknown detector", mutate: func(expected map[string]any) { setDerived(expected); expected["detectorId"] = "missing" }, wantCode: "invalid_schema_v2_identity"},
		{name: "derived forbids capture id", mutate: func(expected map[string]any) { setDerived(expected); expected["captureId"] = "capture" }, wantCode: "invalid_schema_v2_identity"},
		{name: "derived forbids empty capture id field", mutate: func(expected map[string]any) { setDerived(expected); expected["captureId"] = "" }, wantCode: "invalid_schema_v2_identity"},
		{name: "derived forbids instance id", mutate: func(expected map[string]any) { setDerived(expected); expected["instanceId"] = "default" }, wantCode: "invalid_schema_v2_identity"},
		{name: "derived forbids empty instance id field", mutate: func(expected map[string]any) { setDerived(expected); expected["instanceId"] = "" }, wantCode: "invalid_schema_v2_identity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "v2", schemaV2Module("apps.v2"))
			record := validV2Validation(t, mod)
			writeValidationWithMutation(t, root, "v2", record, func(document map[string]any) {
				forEachExpectedJSON(t, document, setLiteral)
				tt.mutate(firstExpectedJSON(t, document))
			})
			_, err := LoadCatalog(root, now)
			if got := ErrorCode(err); got != tt.wantCode {
				t.Fatalf("LoadCatalog error = %v (code %q), want code %q", err, got, tt.wantCode)
			}
		})
	}
}

func TestSchemaV2UsesGenerationValidationAndConditionalAppVerification(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)

	t.Run("generation validation without top-level app verifier", func(t *testing.T) {
		root := t.TempDir()
		content := mutateSchemaV2Module(t, schemaV2Module("apps.studio-like"), func(mod *modules.Module) {
			mod.Verify = nil
		})
		mod := writeModule(t, root, "studio-like", content)
		record := validV2Validation(t, mod)
		for index := range record.Synthetic.Scenarios {
			delete(record.Synthetic.Scenarios[index].MinimumAssertions, AssertionVerify)
		}
		writeValidation(t, root, "studio-like", record)
		if _, err := LoadCatalog(root, now); err != nil {
			t.Fatalf("LoadCatalog returned %v", err)
		}
	})

	t.Run("top-level app verifier requires a verify minimum", func(t *testing.T) {
		root := t.TempDir()
		mod := writeModule(t, root, "with-app-verifier", schemaV2Module("apps.with-app-verifier"))
		record := validV2Validation(t, mod)
		delete(record.Synthetic.Scenarios[0].MinimumAssertions, AssertionVerify)
		writeValidation(t, root, "with-app-verifier", record)
		_, err := LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeMissingAssertionMinimum {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeMissingAssertionMinimum)
		}
	})

	t.Run("missing app verifier forbids a fabricated verify minimum", func(t *testing.T) {
		root := t.TempDir()
		content := mutateSchemaV2Module(t, schemaV2Module("apps.no-app-verifier"), func(mod *modules.Module) {
			mod.Verify = nil
		})
		mod := writeModule(t, root, "no-app-verifier", content)
		record := validV2Validation(t, mod)
		writeValidation(t, root, "no-app-verifier", record)
		_, err := LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeInvalidSidecar {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeInvalidSidecar)
		}
	})

	t.Run("generation cannot fabricate validation evidence", func(t *testing.T) {
		root := t.TempDir()
		content := mutateSchemaV2Module(t, schemaV2Module("apps.no-generation-validation"), func(mod *modules.Module) {
			mod.Config.Sets[0].Generations[0].Validate = nil
		})
		mod := writeModule(t, root, "no-generation-validation", content)
		record := validV2Validation(t, mod)
		writeValidation(t, root, "no-generation-validation", record)
		_, err := LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeMissingProductionValidation {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeMissingProductionValidation)
		}
	})

	t.Run("migration requires target generation validation", func(t *testing.T) {
		root := t.TempDir()
		content := mutateSchemaV2Module(t, schemaV2Module("apps.no-target-validation"), func(mod *modules.Module) {
			mod.Config.Sets[0].Generations[1].Validate = nil
		})
		mod := writeModule(t, root, "no-target-validation", content)
		record := validV2Validation(t, mod)
		scenarios := record.Synthetic.Scenarios
		record.Synthetic.Scenarios = append([]Scenario{scenarios[3]}, scenarios[:3]...)
		writeValidation(t, root, "no-target-validation", record)
		_, err := LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeMissingProductionValidation {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeMissingProductionValidation)
		}
	})

	t.Run("migration edge validation remains a production catalog requirement", func(t *testing.T) {
		root := t.TempDir()
		content := mutateSchemaV2Module(t, schemaV2Module("apps.no-edge-validation"), func(mod *modules.Module) {
			mod.Config.Sets[0].Migrations[0].Validate = nil
		})
		mod := writeModule(t, root, "no-edge-validation", content)
		writeValidation(t, root, "no-edge-validation", validV2Validation(t, mod))
		_, err := LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeInvalidModuleCatalog {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeInvalidModuleCatalog)
		}
	})
}

func TestInstallContractAcceptsEveryProductionAppReferenceFamily(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		matches  string
		wantCode string
	}{
		{name: "winget", matches: `"winget":["Vendor.Fixture"]`},
		{name: "chocolatey", matches: `"chocolatey":["fixture"]`},
		{name: "executable", matches: `"exe":["fixture.exe"]`},
		{name: "uninstall display name", matches: `"uninstallDisplayName":["^Fixture"]`},
		{name: "path exists", matches: `"pathExists":["%PROGRAMFILES%\\Fixture\\fixture.exe"]`},
		{name: "empty", matches: `"winget":[],"chocolatey":[],"exe":[],"uninstallDisplayName":[],"pathExists":[]`, wantCode: CodeInvalidModuleCatalog},
		{name: "whitespace only", matches: `"winget":[" "],"chocolatey":["\t"],"exe":[" "],"uninstallDisplayName":["\r\n"],"pathExists":[" "]`, wantCode: CodeInvalidClassification},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, "install", installOnlyModuleWithMatches("apps.install", tt.matches))
			record := validV1Validation(mod.ID, mod.Revision)
			record.Synthetic.Scenarios = []Scenario{installScenario("install")}
			writeValidation(t, root, "install", record)
			_, err := LoadCatalog(root, now)
			if got := ErrorCode(err); got != tt.wantCode {
				t.Fatalf("LoadCatalog error = %v (code %q), want code %q", err, got, tt.wantCode)
			}
		})
	}
}

func TestCanonicalOneWayScenarioClassifications(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		module   string
		scenario Scenario
	}{
		{"capture contract", captureOnlyModule("apps.capture"), captureScenario("capture")},
		{"restore contract", restoreOnlyModule("apps.restore"), restoreScenario("restore")},
		{"install contract", installOnlyModule("apps.install"), installScenario("install")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mod := writeModule(t, root, strings.TrimPrefix(modIDFromJSON(t, tt.module), "apps."), tt.module)
			record := validV1Validation(mod.ID, mod.Revision)
			record.Synthetic.Scenarios = []Scenario{tt.scenario}
			writeValidation(t, root, strings.TrimPrefix(mod.ID, "apps."), record)
			if _, err := LoadCatalog(root, now); err != nil {
				t.Fatalf("LoadCatalog returned %v", err)
			}
		})
	}

	t.Run("capture-only cannot claim roundtrip", func(t *testing.T) {
		root := t.TempDir()
		mod := writeModule(t, root, "capture", captureOnlyModule("apps.capture"))
		record := validV1Validation(mod.ID, mod.Revision)
		writeValidation(t, root, "capture", record)
		_, err := LoadCatalog(root, now)
		if got := ErrorCode(err); got != CodeInvalidClassification {
			t.Fatalf("LoadCatalog error = %v (code %q), want %q", err, got, CodeInvalidClassification)
		}
	})
}

func validV1Validation(moduleID, revision string) ValidationRecord {
	return ValidationRecord{
		SchemaVersion:  1,
		ModuleID:       moduleID,
		ModuleRevision: revision,
		Synthetic: SyntheticPolicy{Scenarios: []Scenario{{
			ID:             "default-v1",
			Mode:           ScenarioConfigRoundtripV1,
			Fixture:        Fixture{Type: FixtureAuto},
			TimeoutSeconds: 60,
			MinimumAssertions: map[string]int{
				AssertionCaptured:         1,
				AssertionPayload:          1,
				AssertionProvenance:       1,
				AssertionRewrittenRestore: 1,
				AssertionContent:          1,
				AssertionRebuild:          1,
				AssertionVerify:           1,
				AssertionNestedSummary:    1,
				AssertionRevert:           1,
			},
		}}},
		Live: nonHostedLivePolicy(LiveNotApplicable),
	}
}

func validV2Validation(t *testing.T, mod *modules.Module) ValidationRecord {
	t.Helper()
	set := mod.Config.Sets[0]
	first := set.Generations[0]
	second := set.Generations[1]
	base := map[string]int{
		AssertionCaptured: 1, AssertionPayload: 1, AssertionProvenance: 1,
		AssertionRewrittenRestore: 1, AssertionContent: 1, AssertionRebuild: 1,
		AssertionVerify: 1, AssertionNestedSummary: 1, AssertionRevert: 1,
		AssertionGeneration: 1, AssertionValidation: 1,
	}
	generation := func(id, generationID, fingerprint string) Scenario {
		return Scenario{
			ID: id, Mode: ScenarioConfigGenerationV2, Fixture: Fixture{Type: FixtureAuto}, TimeoutSeconds: 60,
			MinimumAssertions: cloneAssertions(base),
			Expected:          &SchemaV2Expectation{IdentityMode: IdentityLiteral, CaptureID: "capture", ConfigSetID: set.ID, InstanceID: "default", GenerationID: generationID, Fingerprint: fingerprint},
		}
	}
	migrationAssertions := cloneAssertions(base)
	migrationAssertions[AssertionMigration] = 1
	return ValidationRecord{
		SchemaVersion: 1, ModuleID: mod.ID, ModuleRevision: mod.Revision,
		Synthetic: SyntheticPolicy{Scenarios: []Scenario{
			generation("g1-current", first.ID, first.Fingerprint),
			generation("g1-history", first.ID, first.AcceptsSourceFingerprints[0]),
			generation("g2-current", second.ID, second.Fingerprint),
			{
				ID: "g1-to-g2", Mode: ScenarioConfigMigrationV2, Fixture: Fixture{Type: FixtureAuto}, TimeoutSeconds: 60,
				MinimumAssertions: migrationAssertions,
				Expected:          &SchemaV2Expectation{IdentityMode: IdentityLiteral, CaptureID: "capture", ConfigSetID: set.ID, InstanceID: "default", GenerationID: second.ID, Fingerprint: second.Fingerprint, MigrationFrom: first.ID, MigrationTo: second.ID},
			},
		}},
		Live: nonHostedLivePolicy(LiveNotApplicable),
	}
}

func installScenario(id string) Scenario {
	return Scenario{
		ID: id, Mode: ScenarioInstallContract, Fixture: Fixture{Type: FixtureAuto}, TimeoutSeconds: 60,
		MinimumAssertions: map[string]int{AssertionAppReferences: 1, AssertionVerify: 1},
	}
}

func captureScenario(id string) Scenario {
	return Scenario{
		ID: id, Mode: ScenarioCaptureContract, Fixture: Fixture{Type: FixtureAuto}, TimeoutSeconds: 60,
		Review: validOneWayReview(),
		MinimumAssertions: map[string]int{
			AssertionCaptured: 1, AssertionPayload: 1, AssertionProvenance: 1, AssertionContent: 1,
		},
	}
}

func restoreScenario(id string) Scenario {
	return Scenario{
		ID: id, Mode: ScenarioRestoreContract, Fixture: Fixture{Type: FixtureAuto}, TimeoutSeconds: 60,
		Review: validOneWayReview(),
		MinimumAssertions: map[string]int{
			AssertionRestored: 1, AssertionContent: 1, AssertionNestedSummary: 1, AssertionVerify: 1, AssertionRevert: 1,
		},
	}
}

func validOneWayReview() *OneWayReview {
	return &OneWayReview{
		Decision: "approved-one-way", ReasonCode: "vendor-contract-is-one-way", Reviewer: "@module-owner",
		ReviewedOn: "2026-07-21", Evidence: "The fixture represents a reviewed production one-way contract.",
	}
}

func nonHostedLivePolicy(mode LiveMode) LivePolicy {
	return LivePolicy{Mode: mode, ReasonCode: "not-hosted", Explanation: "fixture is not eligible for hosted live validation"}
}

func hostedInstallPolicy() LivePolicy {
	return LivePolicy{
		Mode: LiveHosted, Driver: "winget", Ref: "Vendor.Fixture", ProofMode: ProofLiveInstall,
		PRTimeoutMinutes: 20, ScheduledTimeoutMinutes: 30, RunnerLabel: "windows-latest",
	}
}

func hostedConfigPolicy() LivePolicy {
	policy := hostedInstallPolicy()
	policy.ProofMode = ProofLiveConfigRoundtrip
	policy.Seed = "seed.ps1"
	policy.Comparator = "exact-json"
	policy.Trust = &TrustHashes{SeedSHA256: strings.Repeat("a", 64), ComparatorSHA256: strings.Repeat("b", 64)}
	return policy
}

func writeModule(t *testing.T, root, directory, content string) *modules.Module {
	t.Helper()
	dir := filepath.Join(root, "modules", "apps", directory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "module.jsonc")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON([]byte(content))
	if err != nil {
		t.Fatalf("parse test module: %v", err)
	}
	return mod
}

func writeValidation(t *testing.T, root, directory string, record ValidationRecord) {
	t.Helper()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeRawValidation(t, root, directory, data)
}

func writeValidationWithMutation(t *testing.T, root, directory string, record ValidationRecord, mutate func(map[string]any)) {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	mutate(document)
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeRawValidation(t, root, directory, data)
}

func firstScenarioJSON(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	synthetic := document["synthetic"].(map[string]any)
	scenarios := synthetic["scenarios"].([]any)
	return scenarios[0].(map[string]any)
}

func firstExpectedJSON(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	return firstScenarioJSON(t, document)["expected"].(map[string]any)
}

func forEachExpectedJSON(t *testing.T, document map[string]any, mutate func(map[string]any)) {
	t.Helper()
	synthetic := document["synthetic"].(map[string]any)
	for _, rawScenario := range synthetic["scenarios"].([]any) {
		scenario := rawScenario.(map[string]any)
		mutate(scenario["expected"].(map[string]any))
	}
}

func writeRawValidation(t *testing.T, root, directory string, data []byte) {
	t.Helper()
	dir := filepath.Join(root, "modules", "apps", directory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "validation.jsonc"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func schemaV1Module(id string, config bool) string {
	configBody := `"verify": [], "restore": [],`
	if config {
		configBody = `
    "verify": [{"type": "file-exists", "path": "%APPDATA%\\Fixture\\settings.json"}],
    "restore": [{"type": "copy", "source": "./payload/apps/fixture/settings.json", "target": "%APPDATA%\\Fixture\\settings.json", "backup": true}],
    "capture": {"files": [{"source": "%APPDATA%\\Fixture\\settings.json", "dest": "apps/fixture/settings.json"}]},`
	}
	return fmt.Sprintf(`{
    "id": %q,
    "displayName": "Fixture",
    "sensitivity": "none",
    "matches": {"winget": ["Vendor.Fixture"]},
    %s
    "notes": "test"
}`, id, configBody)
}

func captureOnlyModule(id string) string {
	return fmt.Sprintf(`{
  "id": %q,
  "displayName": "Capture Fixture",
  "sensitivity": "none",
  "matches": {"winget": ["Vendor.Capture"]},
  "verify": [{"type": "file-exists", "path": "%%APPDATA%%\\Fixture\\settings.json"}],
  "restore": [],
  "capture": {"files": [{"source": "%%APPDATA%%\\Fixture\\settings.json", "dest": "apps/capture/settings.json"}]}
}`, id)
}

func restoreOnlyModule(id string) string {
	return fmt.Sprintf(`{
  "id": %q,
  "displayName": "Restore Fixture",
  "sensitivity": "none",
  "matches": {"winget": ["Vendor.Restore"]},
  "verify": [{"type": "file-exists", "path": "%%APPDATA%%\\Fixture\\settings.json"}],
  "restore": [{"type": "copy", "source": "./payload/apps/restore/settings.json", "target": "%%APPDATA%%\\Fixture\\settings.json", "backup": true}]
}`, id)
}

func installOnlyModule(id string) string {
	return installOnlyModuleWithMatches(id, `"winget":["Vendor.Install"]`)
}

func installOnlyModuleWithMatches(id, matches string) string {
	return fmt.Sprintf(`{
  "id": %q,
  "displayName": "Install Fixture",
  "sensitivity": "none",
  "matches": {%s},
  "verify": [{"type": "command-exists", "command": "fixture"}],
  "restore": []
}`, id, matches)
}

func modIDFromJSON(t *testing.T, content string) string {
	t.Helper()
	mod, err := modules.ParseModuleJSON([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return mod.ID
}

func schemaV2Module(id string) string {
	return fmt.Sprintf(`{
  "moduleSchemaVersion": 2,
  "id": %q,
  "displayName": "V2 Fixture",
  "sensitivity": "none",
  "matches": {"winget": ["Vendor.V2"]},
  "verify": [{"type": "file-exists", "path": "%%APPDATA%%\\Fixture\\settings.json"}],
	"config": {
	"instanceDetectors": [{"id": "installed-package", "type": "package"}],
    "sets": [{
      "id": "preferences",
      "generations": [
        {
          "id": "g1", "order": 1,
          "acceptsSourceFingerprints": ["%s"],
          "capture": {"files": [{"source": "%%APPDATA%%\\Fixture\\settings.json", "dest": "settings.json"}]},
          "restore": [{"type": "copy", "source": "settings.json", "target": "%%APPDATA%%\\Fixture\\settings.json", "backup": true}],
          "validate": [{"type": "file-exists", "path": "settings.json"}]
        },
        {
          "id": "g2", "order": 2,
          "capture": {"files": [{"source": "%%APPDATA%%\\Fixture\\settings-v2.json", "dest": "settings-v2.json"}]},
          "restore": [{"type": "copy", "source": "settings-v2.json", "target": "%%APPDATA%%\\Fixture\\settings-v2.json", "backup": true}],
          "validate": [{"type": "file-exists", "path": "settings-v2.json"}]
        }
      ],
      "migrations": [{
        "from": "g1", "to": "g2",
        "operations": [{"type": "file-move", "source": "settings.json", "target": "settings-v2.json"}],
        "validate": [{"type": "file-exists", "path": "settings-v2.json"}]
      }]
    }]
  }
}`, id, strings.Repeat("c", 64))
}

func mutateSchemaV2Module(t *testing.T, content string, mutate func(*modules.Module)) string {
	t.Helper()
	mod, err := modules.ParseModuleJSON([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	mutate(mod)
	data, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func cloneAssertions(source map[string]int) map[string]int {
	clone := make(map[string]int, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
