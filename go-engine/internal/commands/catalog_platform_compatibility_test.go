// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// Both command boundaries must retain the complete committed source contract.
// This exercises selection, not native Windows filesystem or registry execution.
func assertCommandCatalogPlatformContracts(t *testing.T, load func(string) (map[string]*modules.Module, error)) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	authored, diagnostics, err := modules.GetCatalogWithDiagnostics(root)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("source catalog: %v; diagnostics=%v", err, diagnostics)
	}
	if len(authored) == 0 {
		t.Fatal("source catalog is empty; compatibility proof requires committed modules")
	}
	frozen := make(map[string][]byte, len(authored))
	legacy, variants := 0, 0
	for id, mod := range authored {
		frozen[id] = mod.CanonicalSnapshot()
		if mod.EffectiveSchemaVersion() <= 2 {
			legacy++
		} else {
			variants++
		}
	}
	t.Logf("source catalog: %d modules, %d legacy modules, %d authored platform modules", len(authored), legacy, variants)
	originalGOOS := captureGOOSFn
	t.Cleanup(func() { captureGOOSFn = originalGOOS })
	for _, platform := range []string{"windows", "linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			captureGOOSFn = func() string { return platform }
			catalog, err := load(root)
			if err != nil {
				t.Fatal(err)
			}
			expectedCount := 0
			for id, source := range authored {
				_, variantExists := source.Platforms[platform]
				present := source.EffectiveSchemaVersion() <= 2 && platform == "windows" || variantExists
				got := catalog[id]
				if !present {
					if got != nil {
						t.Errorf("unsupported module %s became executable", id)
					}
					continue
				}
				expectedCount++
				if got == nil {
					t.Errorf("supported module %s disappeared", id)
					continue
				}
				if !bytes.Equal(got.CanonicalSnapshot(), frozen[id]) || got.Revision != source.Revision {
					t.Errorf("%s lost its frozen source contract", id)
				}
				if source.EffectiveSchemaVersion() <= 2 {
					if !reflect.DeepEqual(got, source) {
						t.Errorf("legacy module %s changed its executable declarations", id)
					}
					continue
				}
				variant := source.Platforms[platform]
				if got.SourceSchemaVersion != 3 || got.Platform != platform || got.Realization != variant.Realization || got.Tier != source.Tier {
					t.Errorf("%s lost platform identity or catalog tier", id)
				}
				if !reflect.DeepEqual(got.Matches, variant.Matches) || !reflect.DeepEqual(got.Verify, variant.Verify) || !reflect.DeepEqual(got.Secrets, variant.Secrets) {
					t.Errorf("%s changed matching, verification or secrets", id)
				}
				if variant.Config == nil && (variant.Capture != nil || len(variant.Restore) > 0) {
					// The flat-v3 contract fixes default/g1 and strips only ./payload/.
					if got.Config == nil || len(got.Config.Sets) != 1 || len(got.Config.Sets[0].Generations) != 1 {
						t.Errorf("%s lost the shorthand generation", id)
						continue
					}
					set := got.Config.Sets[0]
					generation := set.Generations[0]
					if got.EffectiveSchemaVersion() != 2 || set.ID != "default" || generation.ID != "g1" || generation.Order != 1 || generation.Fingerprint == "" {
						t.Errorf("%s changed shorthand identity", id)
					}
					if !reflect.DeepEqual(generation.Capture, variant.Capture) {
						t.Errorf("%s changed shorthand capture declarations", id)
					}
					expectedRestores := append([]modules.RestoreDef(nil), variant.Restore...)
					for index := range expectedRestores {
						expectedRestores[index].Source = strings.TrimPrefix(expectedRestores[index].Source, "./payload/")
					}
					if !reflect.DeepEqual(generation.Restore, expectedRestores) || got.Capture != nil || len(got.Restore) != 0 {
						t.Errorf("%s changed shorthand restore declarations", id)
					}
				}
			}
			if len(catalog) != expectedCount {
				t.Errorf("catalog has %d modules, source contract requires %d", len(catalog), expectedCount)
			}
			t.Logf("%s command authority: %d modules", platform, len(catalog))
			for id, source := range authored {
				if !bytes.Equal(source.CanonicalSnapshot(), frozen[id]) {
					t.Errorf("%s source snapshot changed", id)
				}
			}
		})
	}
}
