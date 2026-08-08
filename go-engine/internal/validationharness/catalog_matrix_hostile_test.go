// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/catalogplan"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type catalogMatrixHelperContract struct {
	Action      catalogplan.Action `json:"action"`
	ExitNonzero bool               `json:"exitNonzero"`
	Behavior    string             `json:"helperBehavior,omitempty"`
	Modules     []string           `json:"modules,omitempty"`
}

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "catalog-plan" {
		runCatalogMatrixHelperProcess()
	}
	os.Exit(m.Run())
}

func runCatalogMatrixHelperProcess() {
	bundle := ""
	for index := 0; index+1 < len(os.Args); index++ {
		if os.Args[index] == "--bundle" {
			bundle = os.Args[index+1]
			break
		}
	}
	data, err := os.ReadFile(bundle)
	if err != nil {
		os.Exit(2)
	}
	contract := catalogMatrixHelperContract{}
	if err := json.Unmarshal(data, &contract); err != nil {
		os.Exit(2)
	}
	switch contract.Behavior {
	case "nonzero":
		_, _ = fmt.Fprintln(os.Stdout, `{"schemaVersion":"1.0"}`)
		os.Exit(1)
	case "timeout":
		time.Sleep(time.Second)
		os.Exit(0)
	case "overflow":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("0", 64))
		os.Exit(0)
	}
	if contract.Action.ModuleID == "" {
		contract.Action = catalogplan.Action{
			ModuleID: "apps.work", ModuleRevision: "revision", ModuleSchemaVersion: 1,
			ValidationHash: "validation", ValidationScenarioCount: 1, Status: "resolved",
		}
	}
	identity, failure := expectedCatalogBundleIdentity(bundle)
	if failure != nil {
		os.Exit(2)
	}
	contract.Action.BundleID, contract.Action.BundleHash = identity.ID, identity.Hash
	result := catalogplan.Result{
		Proof: "catalog", Bundle: catalogplan.Bundle{ID: identity.ID, Name: identity.ID, Path: identity.Path, Hash: identity.Hash, Version: 1},
		MembershipCount: 1, ActionCount: 1, Actions: []catalogplan.Action{contract.Action},
	}
	runID := "catalog-matrix-helper"
	seenModules := make(map[string]struct{}, len(contract.Modules))
	for _, moduleID := range contract.Modules {
		if _, duplicate := seenModules[moduleID]; !duplicate {
			seenModules[moduleID] = struct{}{}
			continue
		}
		if !strings.HasPrefix(moduleID, "apps.") {
			moduleID = "apps." + moduleID
		}
		result.MembershipCount = len(contract.Modules)
		result.ActionCount = 0
		result.Actions = []catalogplan.Action{}
		result.Failures = []catalogplan.Failure{{ModuleID: moduleID, Reason: "duplicate_membership"}}
		envelope := catalogEnvelope{SchemaVersion: "1.0", CLIVersion: "test", Command: "catalog-plan", RunID: runID, TimestampUTC: time.Now().UTC().Format(time.RFC3339), Success: false, Data: mustMarshalCatalogMatrixHelper(result), Error: json.RawMessage(`{"code":"CATALOG_PLAN_INVALID","message":"bad"}`)}
		if err := json.NewEncoder(os.Stdout).Encode(envelope); err != nil {
			os.Exit(2)
		}
		os.Exit(1)
	}
	envelope := catalogEnvelope{SchemaVersion: "1.0", CLIVersion: "test", Command: "catalog-plan", RunID: runID, TimestampUTC: time.Now().UTC().Format(time.RFC3339), Success: true, Data: mustMarshalCatalogMatrixHelper(result), Error: json.RawMessage("null")}
	if err := json.NewEncoder(os.Stdout).Encode(envelope); err != nil {
		os.Exit(2)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = fmt.Fprintf(os.Stderr, "{\"version\":1,\"runId\":%q,\"timestamp\":%q,\"event\":\"phase\",\"phase\":\"plan\"}\n", runID, timestamp)
	_, _ = fmt.Fprintf(os.Stderr, "{\"version\":1,\"runId\":%q,\"timestamp\":%q,\"event\":\"item\",\"id\":%q,\"driver\":\"catalog\",\"status\":\"present\",\"reason\":\"detected\"}\n", runID, timestamp, contract.Action.ModuleID)
	_, _ = fmt.Fprintf(os.Stderr, "{\"version\":1,\"runId\":%q,\"timestamp\":%q,\"event\":\"summary\",\"phase\":\"plan\",\"total\":1,\"success\":1,\"skipped\":0,\"failed\":0}\n", runID, timestamp)
	if contract.ExitNonzero {
		os.Exit(1)
	}
	os.Exit(0)
}

func mustMarshalCatalogMatrixHelper(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func TestInvokeCatalogPlanProcessFailures(t *testing.T) {
	repo := t.TempDir()
	engine, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, behavior string
		timeout        time.Duration
		out            int
		coordinate     string
	}{
		{"nonzero", "nonzero", time.Second, 1024, "envelope"},
		{"timeout", "timeout", 10 * time.Millisecond, 1024, "timeout"},
		{"overflow", "overflow", time.Second, 8, "output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundle := filepath.Join(repo, test.name+".jsonc")
			data, err := json.Marshal(catalogMatrixHelperContract{Behavior: test.behavior})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bundle, data, 0o600); err != nil {
				t.Fatal(err)
			}
			_, failure := invokeCatalogPlanWithLimits(context.Background(), engine, repo, bundle, test.timeout, test.out, test.out)
			if failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}
}

func TestInvokeCatalogPlanRejectsSuccessEnvelopeFromNonzeroHelperProcess(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "work.jsonc")
	data, err := json.Marshal(catalogMatrixHelperContract{ExitNonzero: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, data, 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, failure := invokeCatalogPlanWithLimits(context.Background(), engine, t.TempDir(), bundle, time.Second, 16*1024, 16*1024)
	if failure == nil || failure.Code != CodeExecutionFailure || failure.Coordinate != "exit" || result.Bundle.ID != "" || len(result.Actions) != 0 {
		t.Fatalf("result=%+v failure=%+v", result, failure)
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
	for _, stdout := range [][]byte{
		[]byte("{"),
		[]byte(valid + "\n" + valid),
		[]byte(strings.Replace(valid, `"runId":"catalog-plan-test"`, `"runId":"catalog-plan-test","runId":"forged"`, 1)),
		[]byte(strings.Replace(valid, `"moduleRevision":"rev"`, `"moduleRevision":"rev","moduleRevision":"forged"`, 1)),
		[]byte(strings.Replace(valid, `"validationHash":"hash"`, `"validationHash":"hash","validationHash":"forged"`, 1)),
		[]byte(strings.Replace(valid, `"moduleId":"apps.work"`, `"moduleId":"apps.work","moduleId":"apps.forged"`, 1)),
	} {
		if _, _, failure := decodeCatalogEnvelope(stdout); failure == nil {
			t.Fatal("hostile stdout passed")
		}
	}
	failed := `{"schemaVersion":"1.0","cliVersion":"2.27.4","command":"catalog-plan","runId":"catalog-plan-test","timestampUtc":"2026-07-26T11:00:00Z","success":false,"data":{"proof":"catalog","bundle":{"id":"work","name":"Work","path":"bundles/work.jsonc","hash":"abc","version":1},"membershipCount":1,"actionCount":0,"actions":[],"failures":[{"moduleId":"apps.missing","reason":"missing_module","reason":"forged"}]},"error":{"code":"CATALOG_PLAN_INVALID","message":"bad"}}`
	if _, _, failure := decodeCatalogEnvelope([]byte(failed)); failure == nil {
		t.Fatal("duplicate failure evidence passed")
	}
	result, runID, failure := decodeCatalogEnvelope([]byte(valid))
	if failure != nil {
		t.Fatal(failure)
	}
	validEvents := "{\"version\":1,\"runId\":\"" + runID + "\",\"timestamp\":\"2026-07-26T11:00:00Z\",\"event\":\"phase\",\"phase\":\"plan\"}\n" +
		"{\"version\":1,\"runId\":\"" + runID + "\",\"timestamp\":\"2026-07-26T11:00:00Z\",\"event\":\"item\",\"id\":\"apps.work\",\"driver\":\"catalog\",\"status\":\"present\",\"reason\":\"detected\"}\n" +
		"{\"version\":1,\"runId\":\"" + runID + "\",\"timestamp\":\"2026-07-26T11:00:00Z\",\"event\":\"summary\",\"phase\":\"plan\",\"total\":1,\"success\":1,\"skipped\":0,\"failed\":0}\n"
	for _, stderr := range [][]byte{nil, []byte(validEvents + validEvents), []byte(strings.Replace(validEvents, runID, "foreign", 1)), []byte("not-json\n"), []byte(strings.Replace(validEvents, `"event":"phase"`, `"event":"phase","event":"forged"`, 1))} {
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
	resultDirectory := filepath.Join(os.TempDir(), "endstate-validation-results")
	if err := os.MkdirAll(resultDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(resultDirectory, "repository")
	repositoryResultDirectory := filepath.Join(repo, "endstate-validation-results")
	if err := os.MkdirAll(repositoryResultDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(resultDirectory, "endstate.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	if failure := validateCatalogResultPath(filepath.Join(repositoryResultDirectory, "result.json"), repo, engine); failure == nil || failure.Code != CodeInvalidResultPath || failure.Coordinate != "repository" {
		t.Fatalf("repository failure=%+v", failure)
	}
	if failure := validateCatalogResultPath(engine, repo, engine); failure == nil || failure.Code != CodeInvalidResultPath || failure.Coordinate != "engine" {
		t.Fatalf("engine failure=%+v", failure)
	}
}
