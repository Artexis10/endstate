// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	if definition.Observer.WingetRef != definition.WingetRef || !exactStrings(definition.Observer.UninstallDisplayName, []string{`^Notepad\+\+`}) || !exactStrings(definition.Observer.ExecutableNames, []string{"notepad++.exe"}) {
		t.Fatalf("definition observer = %+v", definition.Observer)
	}
	if len(definition.Comparator.Mappings) != 5 || definition.Comparator.MinimumExistingMappings != 1 {
		t.Fatalf("definition comparator = %+v", definition.Comparator)
	}
	for _, mapping := range definition.Comparator.Mappings {
		if mapping.Identity == "apps/notepad-plus-plus/userDefineLangs" {
			t.Fatalf("directory mapping was included: %+v", mapping)
		}
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

func TestCompileLiveFixtureDefinitionsFailsClosedWithoutOneDeclarativeScenario(t *testing.T) {
	t.Parallel()

	repo := productionLiveRepoRoot(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	record := catalog.Records["apps.notepad-plus-plus"]
	module := catalog.Modules[record.ModuleID]
	cases := []struct {
		name   string
		mutate func(*validationmatrix.ValidationRecord)
	}{
		{name: "multiple roundtrip scenarios", mutate: func(record *validationmatrix.ValidationRecord) {
			record.Synthetic.Scenarios = append(record.Synthetic.Scenarios, record.Synthetic.Scenarios[0])
		}},
		{name: "automatic fixture", mutate: func(record *validationmatrix.ValidationRecord) {
			record.Synthetic.Scenarios[0].Fixture = validationmatrix.Fixture{Type: validationmatrix.FixtureAuto}
		}},
		{name: "mismatched fixture hash", mutate: func(record *validationmatrix.ValidationRecord) {
			record.Synthetic.Scenarios[0].Fixture.SHA256 = strings.Repeat("0", 64)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := record
			candidate.Synthetic.Scenarios = append([]validationmatrix.Scenario(nil), record.Synthetic.Scenarios...)
			test.mutate(&candidate)
			if _, err := compileLiveFixtureDefinitions(repo, candidate, module); err == nil {
				t.Fatal("live compiler accepted ambiguous or non-hash-bound fixture authority")
			}
		})
	}
}

func TestCompileLiveFixtureDefinitionsRejectsAutoFixtureWithoutDirectoryMappings(t *testing.T) {
	t.Parallel()

	repo := productionLiveRepoRoot(t)
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	record := catalog.Records["apps.notepad-plus-plus"]
	record.Synthetic.Scenarios[0].Fixture = validationmatrix.Fixture{Type: validationmatrix.FixtureAuto}
	original := catalog.Modules[record.ModuleID]
	module := *original
	module.Capture = &modules.CaptureDef{Files: append([]modules.CaptureFile(nil), original.Capture.Files[:4]...)}
	module.Capture.Files = append(module.Capture.Files, original.Capture.Files[5])
	module.Restore = append([]modules.RestoreDef(nil), original.Restore[:4]...)
	module.Restore = append(module.Restore, original.Restore[5])
	if _, err := compileLiveFixtureDefinitions(repo, record, &module); err == nil {
		t.Fatal("live compiler accepted an auto fixture without explicit kinds")
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

func TestValidateLiveResultEnforcesAttemptStateMachine(t *testing.T) {
	t.Parallel()

	passedAttempt := func(number int) LiveAttempt {
		return LiveAttempt{Number: number, Phase: LivePhaseCompare, Status: LiveStatusPassed, Package: PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}, Comparator: []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "passed"}}}
	}
	failedAttempt := func(number int, category LiveFailureCategory) LiveAttempt {
		attempt := LiveAttempt{Number: number, Status: LiveStatusFailed, FailureCategory: category}
		switch category {
		case LiveFailureDefinition, LiveFailureEnvironment:
			attempt.Phase = LivePhasePreparation
			attempt.Package.Status = "not-observed"
		case LiveFailurePackage:
			attempt.Phase = LivePhasePackage
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "failed"}
		case LiveFailureSeed:
			attempt.Phase = LivePhaseSeed
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
		case LiveFailureCapture:
			attempt.Phase = LivePhaseCapture
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
		case LiveFailureRestore:
			attempt.Phase = LivePhaseRestore
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
		case LiveFailureComparison:
			attempt.Phase = LivePhaseCompare
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
			attempt.Comparator = []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "failed"}}
		}
		return attempt
	}
	result := func(status LiveStatus, attempts []LiveAttempt, category LiveFailureCategory) LiveResult {
		return LiveResult{SchemaVersion: LiveResultSchemaVersion, ModuleID: "apps.fixture", ModuleRevision: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", ValidationSourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", DefinitionSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", Status: status, Attempts: attempts, FailureCategory: category}
	}
	cases := []struct {
		name    string
		result  LiveResult
		wantErr bool
	}{
		{name: "pending without attempts", result: result(LiveStatusPending, nil, LiveFailureNone)},
		{name: "passed after retry", result: result(LiveStatusPassed, []LiveAttempt{failedAttempt(1, LiveFailureSeed), passedAttempt(2)}, LiveFailureNone)},
		{name: "failed with matching final failure", result: result(LiveStatusFailed, []LiveAttempt{failedAttempt(1, LiveFailureComparison)}, LiveFailureComparison)},
		{name: "pending with attempt", result: result(LiveStatusPending, []LiveAttempt{failedAttempt(1, LiveFailureSeed)}, LiveFailureNone), wantErr: true},
		{name: "pending with failure", result: result(LiveStatusPending, nil, LiveFailureSeed), wantErr: true},
		{name: "passed without attempts", result: result(LiveStatusPassed, nil, LiveFailureNone), wantErr: true},
		{name: "passed with failed final attempt", result: result(LiveStatusPassed, []LiveAttempt{failedAttempt(1, LiveFailureSeed)}, LiveFailureNone), wantErr: true},
		{name: "passed with top failure", result: result(LiveStatusPassed, []LiveAttempt{passedAttempt(1)}, LiveFailureSeed), wantErr: true},
		{name: "passed attempt package not observed", result: result(LiveStatusPassed, []LiveAttempt{{Number: 1, Phase: LivePhasePackage, Status: LiveStatusPassed, Package: PackageObservation{Ref: "Vendor.Fixture", Status: "not-observed"}, Comparator: []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "passed"}}}}, LiveFailureNone), wantErr: true},
		{name: "passed attempt empty comparator", result: result(LiveStatusPassed, []LiveAttempt{{Number: 1, Phase: LivePhaseCompare, Status: LiveStatusPassed, Package: PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}}}, LiveFailureNone), wantErr: true},
		{name: "passed attempt failed comparator", result: result(LiveStatusPassed, []LiveAttempt{{Number: 1, Phase: LivePhaseCompare, Status: LiveStatusPassed, Package: PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}, Comparator: []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "failed"}}}}, LiveFailureNone), wantErr: true},
		{name: "passed attempt duplicate comparator", result: result(LiveStatusPassed, []LiveAttempt{{Number: 1, Phase: LivePhaseCompare, Status: LiveStatusPassed, Package: PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}, Comparator: []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "passed"}, {Identity: "apps/fixture/settings.json", Status: "passed"}}}}, LiveFailureNone), wantErr: true},
		{name: "passed retry did not fail", result: result(LiveStatusPassed, []LiveAttempt{passedAttempt(1), passedAttempt(2)}, LiveFailureNone), wantErr: true},
		{name: "failed with passed final attempt", result: result(LiveStatusFailed, []LiveAttempt{passedAttempt(1)}, LiveFailureComparison), wantErr: true},
		{name: "failed without top failure", result: result(LiveStatusFailed, []LiveAttempt{failedAttempt(1, LiveFailureComparison)}, LiveFailureNone), wantErr: true},
		{name: "failed with mismatched final failure", result: result(LiveStatusFailed, []LiveAttempt{failedAttempt(1, LiveFailureSeed)}, LiveFailureComparison), wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateLiveResult(test.result)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateLiveResult() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateLiveResultEnforcesFailedAttemptEvidence(t *testing.T) {
	t.Parallel()

	failed := func(category LiveFailureCategory) LiveAttempt {
		attempt := LiveAttempt{Number: 1, Status: LiveStatusFailed, FailureCategory: category}
		switch category {
		case LiveFailureDefinition, LiveFailureEnvironment:
			attempt.Phase = LivePhasePreparation
			attempt.Package.Status = "not-observed"
		case LiveFailurePackage:
			attempt.Phase = LivePhasePackage
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "failed"}
		case LiveFailureSeed:
			attempt.Phase = LivePhaseSeed
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
		case LiveFailureCapture:
			attempt.Phase = LivePhaseCapture
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
		case LiveFailureRestore:
			attempt.Phase = LivePhaseRestore
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
		case LiveFailureComparison:
			attempt.Phase = LivePhaseCompare
			attempt.Package = PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}
			attempt.Comparator = []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "failed"}}
		}
		return attempt
	}
	result := func(attempt LiveAttempt) LiveResult {
		return LiveResult{SchemaVersion: LiveResultSchemaVersion, ModuleID: "apps.fixture", ModuleRevision: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", ValidationSourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", DefinitionSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", Status: LiveStatusFailed, Attempts: []LiveAttempt{attempt}, FailureCategory: attempt.FailureCategory}
	}
	cases := []struct {
		name    string
		attempt LiveAttempt
		wantErr bool
	}{
		{name: "definition preparation", attempt: failed(LiveFailureDefinition)},
		{name: "environment preparation", attempt: failed(LiveFailureEnvironment)},
		{name: "package observation failed", attempt: failed(LiveFailurePackage)},
		{name: "seed after package", attempt: failed(LiveFailureSeed)},
		{name: "capture after package", attempt: failed(LiveFailureCapture)},
		{name: "restore after package", attempt: failed(LiveFailureRestore)},
		{name: "comparison observation failed", attempt: failed(LiveFailureComparison)},
		{name: "package category with passed package", attempt: func() LiveAttempt {
			attempt := failed(LiveFailurePackage)
			attempt.Package.Status = "passed"
			return attempt
		}(), wantErr: true},
		{name: "comparison category without failed comparator", attempt: func() LiveAttempt {
			attempt := failed(LiveFailureComparison)
			attempt.Comparator[0].Status = "passed"
			return attempt
		}(), wantErr: true},
		{name: "seed category at compare phase", attempt: func() LiveAttempt {
			attempt := failed(LiveFailureSeed)
			attempt.Phase = LivePhaseCompare
			return attempt
		}(), wantErr: true},
		{name: "capture category with failed package", attempt: func() LiveAttempt {
			attempt := failed(LiveFailureCapture)
			attempt.Package.Status = "failed"
			return attempt
		}(), wantErr: true},
		{name: "restore category after completed comparison", attempt: func() LiveAttempt {
			attempt := failed(LiveFailureRestore)
			attempt.Comparator = []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "passed"}}
			return attempt
		}(), wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateLiveResult(result(test.attempt))
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateLiveResult() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}

	passed := LiveAttempt{Number: 2, Phase: LivePhaseCompare, Status: LiveStatusPassed, Package: PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}, Comparator: []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "passed"}}}
	invalidRetry := failed(LiveFailureSeed)
	invalidRetry.Phase = LivePhaseCompare
	passedResult := result(invalidRetry)
	passedResult.Status = LiveStatusPassed
	passedResult.FailureCategory = LiveFailureNone
	passedResult.Attempts = []LiveAttempt{invalidRetry, passed}
	if err := ValidateLiveResult(passedResult); err == nil {
		t.Fatal("passed result accepted an internally inconsistent failed retry")
	}
}

func TestValidateLiveResultRequiresSuccessfulComparisonPhase(t *testing.T) {
	t.Parallel()

	for _, phase := range []LivePhase{LivePhasePreparation, LivePhasePackage} {
		t.Run(string(phase), func(t *testing.T) {
			result := LiveResult{
				SchemaVersion: LiveResultSchemaVersion, ModuleID: "apps.fixture", ModuleRevision: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ValidationSourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", DefinitionSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
				Status:   LiveStatusPassed,
				Attempts: []LiveAttempt{{Number: 1, Phase: phase, Status: LiveStatusPassed, Package: PackageObservation{Ref: "Vendor.Fixture", Status: "passed"}, Comparator: []ComparatorOutcome{{Identity: "apps/fixture/settings.json", Status: "passed"}}}},
			}
			if err := ValidateLiveResult(result); err == nil {
				t.Fatalf("passed %s attempt was accepted", phase)
			}
		})
	}
}

func TestValidateLiveResultForDefinitionRejectsStalePolicyAndMappings(t *testing.T) {
	t.Parallel()

	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	result := liveResultForDefinition(t, definition)
	if err := ValidateLiveResultForDefinition(result, definition); err != nil {
		t.Fatalf("current result rejected: %v", err)
	}
	foreignPackage := result
	foreignPackage.Attempts = append([]LiveAttempt(nil), result.Attempts...)
	foreignPackage.Attempts[0].Package.Ref = "Vendor.Foreign"
	if err := ValidateLiveResultForDefinition(foreignPackage, definition); err == nil {
		t.Fatal("result accepted a package reference outside the definition")
	}

	changedPolicy := definition
	changedPolicy.Policy.ReasonCode = "changed-policy"
	if err := ValidateLiveResultForDefinition(result, changedPolicy); err == nil {
		t.Fatal("stale result accepted after validation policy change")
	}

	changedMappings := definition
	changedMappings.Comparator.Mappings = append([]ComparatorMapping(nil), definition.Comparator.Mappings...)
	changedMappings.Comparator.Mappings[0].Identity = "apps/notepad-plus-plus/changed-config.xml"
	result.DefinitionSHA256 = canonicalLiveDefinitionSHA256(t, changedMappings)
	if err := ValidateLiveResultForDefinition(result, changedMappings); err == nil {
		t.Fatal("result accepted with stale comparator identities")
	}
}

func TestValidateLiveResultRequiresDefinitionHashes(t *testing.T) {
	t.Parallel()

	result := LiveResult{
		SchemaVersion: LiveResultSchemaVersion, ModuleID: "apps.fixture", ModuleRevision: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ValidationSourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", DefinitionSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", Status: LiveStatusPending,
	}
	for _, field := range []string{"validation source", "definition"} {
		candidate := result
		if field == "validation source" {
			candidate.ValidationSourceSHA256 = ""
		} else {
			candidate.DefinitionSHA256 = ""
		}
		if err := ValidateLiveResult(candidate); err == nil {
			t.Fatalf("result accepted without %s hash", field)
		}
	}
}

func liveResultForDefinition(t *testing.T, definition LiveDefinition) LiveResult {
	t.Helper()
	outcomes := make([]ComparatorOutcome, 0, len(definition.Comparator.Mappings))
	for _, mapping := range definition.Comparator.Mappings {
		outcomes = append(outcomes, ComparatorOutcome{Identity: mapping.Identity, Status: "passed"})
	}
	return LiveResult{
		SchemaVersion: LiveResultSchemaVersion, ModuleID: definition.ModuleID, ModuleRevision: definition.ModuleRevision,
		ValidationSourceSHA256: definition.ValidationSourceSHA256, DefinitionSHA256: canonicalLiveDefinitionSHA256(t, definition), Status: LiveStatusPassed,
		Attempts: []LiveAttempt{{Number: 1, Phase: LivePhaseCompare, Status: LiveStatusPassed, Package: PackageObservation{Ref: definition.WingetRef, Status: "passed"}, Comparator: outcomes}},
	}
}

func canonicalLiveDefinitionSHA256(t *testing.T, definition LiveDefinition) string {
	t.Helper()
	digest, err := CanonicalLiveDefinitionSHA256(definition)
	if err != nil {
		t.Fatal(err)
	}
	return digest
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
			definitions := fixtureDefinitions{Entries: make([]fixtureDefinition, len(module.Capture.Files))}
			for index := range definitions.Entries {
				definitions.Entries[index] = fixtureDefinition{Coordinate: fmt.Sprintf("capture.files[%d]", index), Source: module.Capture.Files[index].Source, Destination: filepath.ToSlash(module.Capture.Files[index].Dest), Target: module.Capture.Files[index].Source, Optional: module.Capture.Files[index].Optional, Kind: fixtureKindFile}
			}
			if _, err := deriveExactBytesComparator(module, definitions); err == nil {
				t.Fatal("unsupported mapping was accepted")
			}
		})
	}
}

func TestLiveRestoreIdentityRejectsUnsafeOrForeignSources(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		source  string
		want    string
		wantErr bool
	}{
		{name: "portable payload identity", source: "./payload/apps/fixture/settings.json", want: "apps/fixture/settings.json"},
		{name: "absolute Windows path", source: `C:\payload\apps\fixture\settings.json`, wantErr: true},
		{name: "UNC path", source: `\\server\share\payload\apps\fixture\settings.json`, wantErr: true},
		{name: "foreign payload prefix", source: "./foreign/payload/apps/fixture/settings.json", wantErr: true},
		{name: "traversal", source: "./payload/apps/fixture/../settings.json", wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			identity, err := liveRestoreIdentity(test.source)
			if (err != nil) != test.wantErr || identity != test.want {
				t.Fatalf("liveRestoreIdentity(%q) = %q, %v; want %q, error %v", test.source, identity, err, test.want, test.wantErr)
			}
		})
	}
}

func TestDeriveExactBytesComparatorRejectsUnsafeRestoreSources(t *testing.T) {
	t.Parallel()

	valid := func() (*modules.Module, fixtureDefinitions) {
		module := &modules.Module{Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: `%APPDATA%\Fixture\settings.json`, Dest: "apps/fixture/settings.json", Optional: true}}}, Restore: []modules.RestoreDef{{Type: "copy", Source: "./payload/apps/fixture/settings.json", Target: `%APPDATA%\Fixture\settings.json`, Optional: true}}}
		definitions := fixtureDefinitions{Entries: []fixtureDefinition{{Coordinate: "capture.files[0]", Source: `%APPDATA%\Fixture\settings.json`, Destination: "apps/fixture/settings.json", Target: `%APPDATA%\Fixture\settings.json`, Optional: true, Kind: fixtureKindFile}}}
		return module, definitions
	}
	for _, source := range []string{`C:\payload\apps\fixture\settings.json`, `\\server\share\payload\apps\fixture\settings.json`, "./foreign/payload/apps/fixture/settings.json"} {
		t.Run(source, func(t *testing.T) {
			module, definitions := valid()
			module.Restore[0].Source = source
			if _, err := deriveExactBytesComparator(module, definitions); err == nil {
				t.Fatal("unsafe restore source was accepted")
			}
		})
	}
}

func TestValidLiveUserTemplateRejectsDynamicAndUnsafeWindowsPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "production Notepad template", value: `%APPDATA%\Notepad++\config.xml`, valid: true},
		{name: "NTFS alternate data stream", value: `%APPDATA%\Fixture\settings.json:stream`},
		{name: "nested environment placeholder", value: `%APPDATA%\Fixture\%TEMP%\settings.json`},
		{name: "NUL", value: "%APPDATA%\\Fixture\\settings\x00.json"},
		{name: "reserved device", value: `%APPDATA%\Fixture\CON`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := validLiveUserTemplate(test.value); got != test.valid {
				t.Fatalf("validLiveUserTemplate(%q) = %v, want %v", test.value, got, test.valid)
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
