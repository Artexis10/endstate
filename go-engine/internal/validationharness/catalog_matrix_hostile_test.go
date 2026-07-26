// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/catalogplan"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestInvokeCatalogPlanProcessFailures(t *testing.T) {
	repo := t.TempDir()
	bundle := filepath.Join(repo, "work.jsonc")
	if err := os.WriteFile(bundle, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body string
		timeout    time.Duration
		out        int
		coordinate string
	}{
		{"nonzero", "echo {\"schemaVersion\":\"1.0\"}\r\nexit /b 1", time.Second, 1024, "envelope"},
		{"timeout", "ping 127.0.0.1 -n 3 >nul", 10 * time.Millisecond, 1024, "timeout"},
		{"overflow", "echo 012345678901234567890123456789012345678901234567890123456789", time.Second, 8, "output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := filepath.Join(t.TempDir(), "child.cmd")
			if err := os.WriteFile(engine, []byte("@echo off\r\n"+test.body+"\r\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			_, failure := invokeCatalogPlanWithLimits(context.Background(), engine, repo, bundle, test.timeout, test.out, test.out)
			if failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}
}

func TestValidateCatalogPlanActionIdentityRejectsWrongRevision(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.notepad-plus-plus"]
	record := catalog.Records[mod.ID]
	result := catalogplan.Result{Actions: []catalogplan.Action{{
		ModuleID: mod.ID, ModuleRevision: "wrong", ModuleSchemaVersion: mod.EffectiveSchemaVersion(),
		ValidationHash: "placeholder", ValidationScenarioCount: len(record.Synthetic.Scenarios),
	}}}
	if failure := validateCatalogPlanActionIdentity(result, catalog); failure == nil {
		t.Fatal("wrong module revision passed")
	}
}

func TestValidateCatalogPlanActionIdentityRejectsEveryPinnedField(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog.Modules["apps.notepad-plus-plus"]
	record := catalog.Records[mod.ID]
	snapshot := bytes.ReplaceAll(record.SourceSnapshot(), []byte("\r\n"), []byte("\n"))
	digest := sha256HexForTest(snapshot)
	base := catalogplan.Action{ModuleID: mod.ID, ModuleRevision: mod.Revision, ModuleSchemaVersion: mod.EffectiveSchemaVersion(), ValidationHash: digest, ValidationScenarioCount: len(record.Synthetic.Scenarios)}
	for _, mutate := range []func(*catalogplan.Action){
		func(action *catalogplan.Action) { action.ModuleSchemaVersion++ },
		func(action *catalogplan.Action) { action.ValidationHash = "wrong" },
		func(action *catalogplan.Action) { action.ValidationScenarioCount++ },
	} {
		action := base
		mutate(&action)
		if failure := validateCatalogPlanActionIdentity(catalogplan.Result{Actions: []catalogplan.Action{action}}, catalog); failure == nil {
			t.Fatal("wrong pinned action field passed")
		}
	}
}

func TestCatalogDecoderRejectsHostileOutput(t *testing.T) {
	valid := `{"schemaVersion":"1.0","cliVersion":"2.27.4","command":"catalog-plan","runId":"catalog-plan-test","timestampUtc":"2026-07-26T11:00:00Z","success":true,"data":{"proof":"catalog","bundle":{"id":"work","name":"Work","path":"bundles/work.jsonc","hash":"abc","version":1},"membershipCount":1,"actionCount":1,"actions":[{"bundleId":"work","bundleHash":"abc","moduleId":"apps.work","moduleRevision":"rev","moduleSchemaVersion":1,"validationHash":"hash","validationScenarioCount":1,"status":"resolved","skipped":false}]},"error":null}`
	for _, stdout := range [][]byte{[]byte("{"), []byte(valid + "\n" + valid)} {
		if _, _, failure := decodeCatalogEnvelope(stdout); failure == nil {
			t.Fatal("hostile stdout passed")
		}
	}
	result, runID, failure := decodeCatalogEnvelope([]byte(valid))
	if failure != nil {
		t.Fatal(failure)
	}
	validEvents := "{\"version\":1,\"runId\":\"" + runID + "\",\"timestamp\":\"2026-07-26T11:00:00Z\",\"event\":\"phase\",\"phase\":\"plan\"}\n" +
		"{\"version\":1,\"runId\":\"" + runID + "\",\"timestamp\":\"2026-07-26T11:00:00Z\",\"event\":\"item\",\"id\":\"apps.work\",\"driver\":\"catalog\",\"status\":\"present\",\"reason\":\"detected\"}\n" +
		"{\"version\":1,\"runId\":\"" + runID + "\",\"timestamp\":\"2026-07-26T11:00:00Z\",\"event\":\"summary\",\"phase\":\"plan\",\"total\":1,\"success\":1,\"skipped\":0,\"failed\":0}\n"
	for _, stderr := range [][]byte{nil, []byte(validEvents + validEvents), []byte(strings.Replace(validEvents, runID, "foreign", 1)), []byte("not-json\n")} {
		if failure := decodeCatalogEvents(stderr, runID, result); failure == nil {
			t.Fatal("hostile event stream passed")
		}
	}
}

func sha256HexForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestDecodeCatalogEnvelopePreservesStructuredFailureEvidence(t *testing.T) {
	stdout := []byte(`{"schemaVersion":"1.0","cliVersion":"2.27.4","command":"catalog-plan","runId":"catalog-plan-test","timestampUtc":"2026-07-26T11:00:00Z","success":false,"data":{"proof":"catalog","bundle":{"id":"work","name":"Work","path":"bundles/work.jsonc","hash":"abc","version":1},"membershipCount":1,"actionCount":0,"actions":[],"failures":[{"moduleId":"apps.missing","reason":"missing_module"}]},"error":{"code":"CATALOG_PLAN_INVALID","message":"bad"}}`)
	result, _, failure := decodeCatalogEnvelope(stdout)
	if failure == nil || len(result.Failures) != 1 || result.Failures[0].ModuleID != "apps.missing" || result.Failures[0].Reason != "missing_module" {
		t.Fatalf("result=%+v failure=%+v", result, failure)
	}
}

func TestCatalogChildEnvironmentIsAllowlisted(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "secret")
	t.Setenv("NPM_TOKEN", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "secret")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "secret")
	t.Setenv("ENDSTATE_TESTMODE", "1")
	t.Setenv("UNRELATED_SECRET", "secret")
	values := catalogChildEnvironment(t.TempDir())
	for _, value := range values {
		if value == "GITHUB_TOKEN=secret" || value == "ACTIONS_ID_TOKEN_REQUEST_TOKEN=secret" || value == "NPM_TOKEN=secret" || value == "AWS_SESSION_TOKEN=secret" || value == "GOOGLE_APPLICATION_CREDENTIALS=secret" || value == "ENDSTATE_TESTMODE=1" || value == "UNRELATED_SECRET=secret" {
			t.Fatalf("credential leaked into child environment: %q", value)
		}
	}
}

func TestStripCatalogProofInvalidatesEveryRow(t *testing.T) {
	result := CatalogMatrixResult{Status: ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}, Rows: []CatalogMatrixRow{{Status: ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}}}}
	stripCatalogProof(&result, fail(CodeIsolationFailure, "guard", "repository", "changed"))
	if result.Status != ResultStatusFailed || len(result.ProofLevels) != 0 || result.Rows[0].Status != ResultStatusFailed || len(result.Rows[0].ProofLevels) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestValidateCatalogResultPathRejectsRepositoryAndEngineTargets(t *testing.T) {
	repo := t.TempDir()
	engine := filepath.Join(t.TempDir(), "endstate.exe")
	if failure := validateCatalogResultPath(filepath.Join(repo, "endstate-validation-results", "result.json"), repo, engine); failure == nil {
		t.Fatal("repository result path passed")
	}
	if failure := validateCatalogResultPath(engine, repo, engine); failure == nil {
		t.Fatal("engine result path passed")
	}
}
