// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/catalogplan"
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
