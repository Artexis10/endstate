// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package linuxparity

import (
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
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
		"apps.fastfetch": StatusAdapterRequired,
		"apps.git":       StatusAdapterRequired,
		"apps.ripgrep":   StatusSupported,
		"apps.vscode":    StatusAdapterRequired,
	} {
		if got := statuses[moduleID]; got != want {
			t.Errorf("%s status = %s, want %s", moduleID, got, want)
		}
	}
}

// The release job builds each fixture, so a schema leaf that no fixture sets would
// ship without any proof that Home Manager renders it.
func TestProductionTypedEntriesHaveFixturesThatSetEverySchemaLeaf(t *testing.T) {
	registry, err := hmregistry.Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	typed := 0
	for _, entry := range registry.Entries {
		if entry.Disposition != hmregistry.TypedRoundTrip {
			continue
		}
		typed++
		codec, ok := settingscodec.Typed(entry.Codec)
		if !ok {
			t.Fatalf("typed entry %s names codec %q, which is not loaded", entry.ID, entry.Codec)
		}
		fixtures, err := settingscodec.TypedFixtures(entry.Codec)
		if err != nil {
			t.Fatal(err)
		}
		covered := map[string]bool{}
		for _, fixture := range fixtures {
			value, err := codec.Decode(fixture.Data)
			if err != nil {
				t.Fatalf("typed entry %s fixture %s does not decode: %v", entry.ID, fixture.Name, err)
			}
			for _, leaf := range codec.ValueLeaves(value) {
				covered[leaf] = true
			}
		}
		for _, leaf := range codec.Leaves() {
			if !covered[leaf] {
				t.Errorf("typed entry %s: no fixture sets schema leaf %s", entry.ID, leaf)
			}
		}
	}
	if typed == 0 {
		t.Fatal("the production registry has no typed entry")
	}
}
