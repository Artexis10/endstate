// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestCompileLiveDefinitionProductionNotepadPlusPlusIsDiagnosticOnly(t *testing.T) {
	t.Parallel()

	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	if definition.SchemaVersion != LiveDefinitionSchemaVersion || definition.ModuleID != "apps.notepad-plus-plus" || definition.ModuleRevision != "45e498e66bf7a84e4cf63a64207a2e8c6bf93e34d6aee1a4c1a8ee2cb5e727c9" {
		t.Fatalf("definition identity = %+v", definition)
	}
	if definition.Policy.Mode != "candidate" || definition.WingetRef != "Notepad++.Notepad++" || definition.MutationAuthorized {
		t.Fatalf("definition authority = %+v", definition)
	}
	if len(definition.Comparator.Mappings) != 6 || definition.Comparator.MinimumExistingMappings != 1 {
		t.Fatalf("definition comparator = %+v", definition.Comparator)
	}
	if definition.Comparator.Mappings[0].Identity != "apps/notepad-plus-plus/config.xml" || definition.Comparator.Mappings[0].CaptureTemplate != "%APPDATA%\\Notepad++\\config.xml" || definition.Comparator.Mappings[0].RestoreTemplate != "%APPDATA%\\Notepad++\\config.xml" {
		t.Fatalf("first comparator mapping = %+v", definition.Comparator.Mappings[0])
	}
	data, err := os.ReadFile(filepath.Join(productionLiveRepoRoot(t), "modules", "apps", "notepad-plus-plus", "validation.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if definition.ValidationSourceSHA256 != hex.EncodeToString(digest[:]) || definition.NonAuthorizing != true {
		t.Fatalf("definition binding = %+v", definition)
	}
}

func TestValidateLiveResultRejectsProofAndPathLeaks(t *testing.T) {
	t.Parallel()

	result := LiveResult{
		SchemaVersion:          LiveResultSchemaVersion,
		ModuleID:               "apps.fixture",
		ModuleRevision:         "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status:                 LiveStatusFailed,
		PublicEvidenceEligible: true,
		ProvenProofLevels:      []validationmatrix.ProofLevel{validationmatrix.ProofLiveConfigRoundtrip},
		Attempts: []LiveAttempt{{
			Number: 1, Phase: LivePhasePackage, Status: LiveStatusFailed,
			Package: PackageObservation{Ref: `C:\Users\person\secret`, Status: "failed"},
		}},
	}
	if err := ValidateLiveResult(result); err == nil {
		t.Fatal("result accepted public proof and personal path")
	}
}

func TestValidateLiveResultRejectsVacuousPassAndOversizedFields(t *testing.T) {
	t.Parallel()

	result := LiveResult{
		SchemaVersion:  LiveResultSchemaVersion,
		ModuleID:       "apps.fixture",
		ModuleRevision: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status:         LiveStatusPassed,
	}
	if err := ValidateLiveResult(result); err == nil {
		t.Fatal("result accepted a vacuous pass")
	}
	result.Status = LiveStatusFailed
	result.Attempts = []LiveAttempt{{
		Number: 1, Phase: LivePhasePackage, Status: LiveStatusFailed,
		Package: PackageObservation{Ref: string(make([]byte, maxLiveStringBytes+1)), Status: "failed"},
	}}
	if err := ValidateLiveResult(result); err == nil {
		t.Fatal("result accepted an oversized package field")
	}
}

func TestValidateLiveRequestRejectsAuthorizingDefinition(t *testing.T) {
	t.Parallel()

	request := LiveRequest{SchemaVersion: LiveDefinitionSchemaVersion, MaxAttempts: 1, Definition: LiveDefinition{
		SchemaVersion: LiveDefinitionSchemaVersion, ModuleID: "apps.fixture", ModuleRevision: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ValidationSourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Policy: validationmatrix.LivePolicy{Mode: validationmatrix.LiveCandidate, Driver: "winget", Ref: "Vendor.Fixture", Seed: "seed.ps1", Comparator: validationmatrix.ComparatorExactBytes, RunnerLabel: "windows-latest", Trust: &validationmatrix.TrustHashes{SeedSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},
		WingetRef: "Vendor.Fixture", SeedRepositoryPath: "seed.ps1", SeedSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", RunnerLabel: "windows-latest", PRTimeoutMinutes: 25, ScheduledTimeoutMinutes: 45,
		Comparator:     ExactBytesComparator{Mappings: []ComparatorMapping{{Identity: "apps/fixture/settings.json", CaptureTemplate: `%APPDATA%\Fixture\settings.json`, RestoreTemplate: `%APPDATA%\Fixture\settings.json`}}, MinimumExistingMappings: 1},
		NonAuthorizing: false, MutationAuthorized: true,
	}}
	if err := ValidateLiveRequest(request); err == nil {
		t.Fatal("authorizing request was accepted")
	}
}

func TestDeriveExactBytesComparatorRejectsUnsupportedMappings(t *testing.T) {
	t.Parallel()

	valid := func() *modules.Module {
		return &modules.Module{Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: `%APPDATA%\Fixture\settings.json`, Dest: "apps/fixture/settings.json", Optional: true}}}, Restore: []modules.RestoreDef{{Type: "copy", Source: "./payload/apps/fixture/settings.json", Target: `%APPDATA%\Fixture\settings.json`, Optional: true}}}
	}
	cases := []struct {
		name   string
		mutate func(*modules.Module)
	}{
		{name: "registry", mutate: func(module *modules.Module) {
			module.Capture.RegistryKeys = []modules.CaptureRegistryKey{{Key: "HKCU\\Fixture", Dest: "registry"}}
		}},
		{name: "directory", mutate: func(module *modules.Module) { module.Capture.Files[0].Source = `%APPDATA%\Fixture\` }},
		{name: "dynamic", mutate: func(module *modules.Module) { module.Capture.Files[0].Source = `${user.home}\Fixture\settings.json` }},
		{name: "system", mutate: func(module *modules.Module) { module.Capture.Files[0].Source = `%PROGRAMDATA%\Fixture\settings.json` }},
		{name: "asymmetric", mutate: func(module *modules.Module) { module.Restore[0].Target = `%APPDATA%\Elsewhere\settings.json` }},
		{name: "duplicate", mutate: func(module *modules.Module) {
			module.Capture.Files = append(module.Capture.Files, module.Capture.Files[0])
		}},
		{name: "no mappings", mutate: func(module *modules.Module) { module.Capture.Files = nil; module.Restore = nil }},
		{name: "foreign restore", mutate: func(module *modules.Module) { module.Restore[0].Source = "./payload/apps/fixture/foreign.json" }},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			module := valid()
			test.mutate(module)
			if _, err := deriveExactBytesComparator(module); err == nil {
				t.Fatal("unsupported mapping was accepted")
			}
		})
	}
}

func TestCompileLiveDefinitionRejectsNonCandidateAndSchemaV2(t *testing.T) {
	t.Parallel()

	module := &modules.Module{ID: "apps.fixture"}
	if _, err := compileLiveDefinition(validationmatrix.ValidationRecord{ModuleID: module.ID, Live: validationmatrix.LivePolicy{Mode: validationmatrix.LiveManual}}, module); err == nil {
		t.Fatal("non-candidate policy was accepted")
	}
	module.ModuleSchemaVersion = 2
	if _, err := compileLiveDefinition(validationmatrix.ValidationRecord{ModuleID: module.ID, Live: validationmatrix.LivePolicy{Mode: validationmatrix.LiveCandidate}}, module); err == nil {
		t.Fatal("schema-v2 module was accepted")
	}
}

func productionLiveRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}
