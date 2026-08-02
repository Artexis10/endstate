//go:build windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestPrepareGuardsAndToolsMaterializesRegistryKeyExistsVerifier(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := modules.LoadCatalog(filepath.Join(repositoryRoot, "modules", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		moduleID string
		index    int
	}{
		{moduleID: "apps.heidisql", index: 0},
		{moduleID: "apps.macrium-reflect", index: 1},
		{moduleID: "apps.mobaxterm", index: 0},
	} {
		t.Run(test.moduleID, func(t *testing.T) {
			runtime := fixtureScenarioRuntime(t)
			t.Cleanup(func() {
				if runtime.RegistryFixture != nil {
					if err := runtime.RegistryFixture.Cleanup(); err != nil {
						t.Error(err)
					}
				}
			})
			runtime.Module = catalog[test.moduleID]
			runtime.GuardRoot = t.TempDir()
			if runtime.Module == nil || len(runtime.Module.Verify) <= test.index || runtime.Module.Verify[test.index].Type != "registry-key-exists" {
				t.Fatalf("registry verifier = %+v, want verify[%d] registry-key-exists", runtime.Module, test.index)
			}
			fixture, err := validationmode.NewRegistryFixture(runtime.validationContext())
			if err != nil {
				t.Fatal(err)
			}
			runtime.RegistryFixture = fixture
			if err := runtime.RegistryFixture.Cleanup(); err != nil {
				t.Fatal(err)
			}

			if err := runtime.prepareGuardsAndTools(); err != nil {
				setup, ok := registryVerifierFixtureSetup(err)
				if !ok || setup.verifierIndex != test.index || setup.verifierKind != "registry-key-exists" || setup.cause == nil {
					t.Fatalf("registry fixture setup = %+v, classified=%v, want verify[%d] registry-key-exists", setup, ok, test.index)
				}
				t.Fatalf("registry fixture setup failed: %v", setup.cause)
			}
			if runtime.RegistryFixture == nil {
				t.Fatal("registry verifier fixture was not created")
			}
			mapped, err := runtime.validationContext().MapHKCU(runtime.Module.Verify[test.index].Path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(strings.ToLower(mapped), strings.ToLower(runtime.validationContext().RegistryNamespace()+`\`)) {
				t.Fatalf("registry verifier mapped outside namespace: %q", mapped)
			}
		})
	}
}
