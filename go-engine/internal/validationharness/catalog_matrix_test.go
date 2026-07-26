// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/catalogplan"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestRunCatalogMatrixRejectsMissingEngine(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunCatalogMatrix(context.Background(), CatalogMatrixRequest{
		EnginePath: filepath.Join(t.TempDir(), "missing.exe"),
		RepoRoot:   repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusFailed || result.Failure == nil || len(result.ProofLevels) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunCatalogMatrixDoesNotPersistUnsafeSetupFailure(t *testing.T) {
	resultRoot := filepath.Join(os.TempDir(), "endstate-validation-results")
	repo := filepath.Join(resultRoot, t.Name())
	resultDirectory := filepath.Join(repo, "endstate-validation-results")
	if err := os.MkdirAll(resultDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(resultDirectory, "result.json")
	defer os.Remove(resultPath)

	result, err := RunCatalogMatrix(context.Background(), CatalogMatrixRequest{
		EnginePath: filepath.Join(t.TempDir(), "missing.exe"),
		RepoRoot:   repo,
		ResultPath: resultPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusFailed || result.Failure == nil || len(result.ProofLevels) != 0 {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Fatalf("unsafe setup failure persisted result: %v", err)
	}
}

func TestValidateCatalogPlanBundleIdentityRejectsForeignHash(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "work.jsonc")
	if err := os.WriteFile(bundle, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := catalogplan.Result{Bundle: catalogplan.Bundle{ID: "work", Path: "bundles/work.jsonc", Hash: "foreign", Version: 1}}
	if failure := validateCatalogPlanBundleIdentity(result, bundle); failure == nil {
		t.Fatal("foreign bundle hash passed")
	}
}

func TestRunCatalogMatrixRowPreservesDuplicateMembershipFailureEvidence(t *testing.T) {
	repo := t.TempDir()
	bundleDirectory := filepath.Join(repo, "bundles")
	if err := os.MkdirAll(bundleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(bundleDirectory, "work.jsonc")
	if err := os.WriteFile(bundle, []byte(`{"version":1,"id":"work","name":"Work","modules":["foo","foo"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "catalog-plan.cmd")
	stdout := `{"schemaVersion":"1.0","cliVersion":"test","command":"catalog-plan","runId":"catalog-plan-test","timestampUtc":"2026-07-26T11:00:00Z","success":false,"data":{"proof":"catalog","bundle":{"id":"work","name":"Work","path":"bundles/work.jsonc","hash":"abc","version":1},"membershipCount":2,"actionCount":0,"actions":[],"failures":[{"moduleId":"apps.foo","reason":"duplicate_membership"}]},"error":{"code":"CATALOG_PLAN_INVALID","message":"bad"}}`
	if err := os.WriteFile(engine, []byte("@echo off\r\necho "+stdout+"\r\nexit /b 1\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	row := runCatalogMatrixRow(context.Background(), engine, repo, bundle, nil)
	if row.Failure == nil || len(row.Failures) != 1 || row.Failures[0].ModuleID != "apps.foo" || row.Failures[0].Reason != "duplicate_membership" {
		t.Fatalf("row=%+v", row)
	}
}

func TestValidateCatalogResultPathRejectsLinkInRootChain(t *testing.T) {
	resultRoot := filepath.Join(os.TempDir(), "endstate-validation-results")
	outside := t.TempDir()
	if err := os.MkdirAll(resultRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "endstate-validation-results"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(resultRoot, filepath.Base(filepath.Dir(outside)))
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(link, "endstate-validation-results", "result.json")
	if failure := validateCatalogResultPath(path, "", ""); failure == nil {
		t.Fatal("result path with link in root chain passed")
	}
}

func TestRunCatalogMatrixStripsPersistedProofAfterRepositoryMutation(t *testing.T) {
	sourceRepo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	moduleDirectory := filepath.Join(repo, "modules", "apps", "notepad-plus-plus")
	if err := os.MkdirAll(moduleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"module.jsonc", "validation.jsonc"} {
		data, err := os.ReadFile(filepath.Join(sourceRepo, "modules", "apps", "notepad-plus-plus", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(moduleDirectory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := validationmatrix.LoadCatalog(repo, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	module := catalog.Modules["apps.notepad-plus-plus"]
	record := catalog.Records[module.ID]
	validationBytes := bytes.ReplaceAll(record.SourceSnapshot(), []byte("\r\n"), []byte("\n"))
	validationHash := sha256.Sum256(validationBytes)
	contract, err := json.Marshal(catalogMatrixHelperContract{Action: catalogplan.Action{
		ModuleID: module.ID, ModuleRevision: module.Revision, ModuleSchemaVersion: module.EffectiveSchemaVersion(),
		ValidationHash: hex.EncodeToString(validationHash[:]), ValidationScenarioCount: len(record.Synthetic.Scenarios), Status: "resolved",
	}})
	if err != nil {
		t.Fatal(err)
	}
	bundleDirectory := filepath.Join(repo, "bundles")
	if err := os.MkdirAll(bundleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDirectory, "fixture.jsonc"), contract, 0o600); err != nil {
		t.Fatal(err)
	}
	resultDirectory := filepath.Join(os.TempDir(), "endstate-validation-results")
	if err := os.MkdirAll(resultDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(resultDirectory, "catalog-matrix-post-persistence-"+filepath.Base(repo)+".json")
	defer os.Remove(resultPath)
	engine, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	modulePath := filepath.Join(moduleDirectory, "module.jsonc")
	catalogAfterPersistHook = func() {
		persistedBytes, readErr := os.ReadFile(resultPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var persisted CatalogMatrixResult
		if unmarshalErr := json.Unmarshal(persistedBytes, &persisted); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		if persisted.Status != ResultStatusPassed || len(persisted.ProofLevels) != 1 || len(persisted.Rows) != 1 || len(persisted.Rows[0].ProofLevels) != 1 {
			t.Fatalf("pre-mutation persisted=%+v", persisted)
		}
		data, readErr := os.ReadFile(modulePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if writeErr := os.WriteFile(modulePath, append(data, '\n'), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	defer func() { catalogAfterPersistHook = nil }()

	result, err := RunCatalogMatrix(context.Background(), CatalogMatrixRequest{EnginePath: engine, RepoRoot: repo, ResultPath: resultPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeIsolationFailure || result.Failure.Coordinate != "repository" || len(result.ProofLevels) != 0 || len(result.Rows) != 1 || len(result.Rows[0].ProofLevels) != 0 {
		t.Fatalf("result=%+v", result)
	}
	persistedBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted CatalogMatrixResult
	if err := json.Unmarshal(persistedBytes, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status != ResultStatusFailed || persisted.Failure == nil || len(persisted.ProofLevels) != 0 || len(persisted.Rows) != 1 || len(persisted.Rows[0].ProofLevels) != 0 {
		t.Fatalf("persisted=%+v", persisted)
	}
}
