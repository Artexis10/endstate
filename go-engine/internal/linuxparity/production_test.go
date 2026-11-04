// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package linuxparity

import (
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

func TestProductionMatrixAccountsForEveryExistingApplicationModule(t *testing.T) {
	repo := filepath.Join("..", "..", "..")
	moduleCatalog, err := modules.GetCatalog(repo)
	if err != nil {
		t.Fatal(err)
	}
	packages, err := packagecatalog.Load(filepath.Join(repo, "catalog", "packages"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := hmregistry.Load(filepath.Join(repo, "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := Build(moduleCatalog, packages.Entries(), registry)
	if err != nil {
		t.Fatal(err)
	}
	applicationCount := 0
	for _, module := range moduleCatalog {
		if module.Tier == modules.AppsTier {
			applicationCount++
		}
	}
	if len(matrix.Rows) != applicationCount || matrix.Counts.Total != applicationCount {
		t.Fatalf("matrix rows=%d total=%d applications=%d", len(matrix.Rows), matrix.Counts.Total, applicationCount)
	}

	statuses := make(map[string]Status, len(matrix.Rows))
	for _, row := range matrix.Rows {
		statuses[row.ModuleID] = row.Status
	}
	for moduleID, want := range map[string]Status{
		"apps.fastfetch": StatusSupported,
		"apps.git":       StatusSupported,
		"apps.ripgrep":   StatusSupported,
		"apps.vscode":    StatusAdapterRequired,
	} {
		if got := statuses[moduleID]; got != want {
			t.Errorf("%s status = %s, want %s", moduleID, got, want)
		}
	}
}
