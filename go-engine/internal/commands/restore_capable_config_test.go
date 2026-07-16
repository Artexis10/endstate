// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestStandaloneRestoreAndRebuildExposeCanonicalLegacyConfigFields(t *testing.T) {
	manifestDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "legacy-settings.json")
	if err := os.WriteFile(filepath.Join(manifestDir, "legacy.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mf := manifest.Manifest{
		Version: 1, Name: "legacy-config", Apps: []manifest.App{},
		Restore: []manifest.RestoreEntry{{
			Type: "copy", Source: "legacy.json", Target: target, FromModule: "apps.legacy",
		}},
	}
	encoded, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(manifestDir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	originalRoot := resolveRepoRootFn
	resolveRepoRootFn = func() string { return t.TempDir() }
	t.Cleanup(func() { resolveRepoRootFn = originalRoot })

	withMockDriver(&mockDriver{installed: map[string]bool{}}, func() {
		got, envErr := RunRestore(RestoreFlags{Manifest: manifestPath, EnableRestore: true, DryRun: true})
		if envErr != nil {
			t.Fatalf("RunRestore: %+v", envErr)
		}
		result := got.(*RestoreData)
		assertLegacyConfigFields(t, result.ConfigResultFields)
		if len(result.Results) != 1 || result.Results[0].SourceGeneration != "" || result.Results[0].TargetGeneration != "" {
			t.Fatalf("standalone restore results = %+v", result.Results)
		}
	})

	withMockDriver(&mockDriver{installed: map[string]bool{}}, func() {
		got, envErr := RunRebuild(RebuildFlags{From: manifestPath, DryRun: true})
		if envErr != nil {
			t.Fatalf("RunRebuild: %+v", envErr)
		}
		result := got.(*RebuildResult)
		assertLegacyConfigFields(t, result.ConfigResultFields)
		applied := result.Apply.(*ApplyResult)
		if !reflect.DeepEqual(result.ConfigResultFields, applied.ConfigResultFields) {
			t.Fatalf("rebuild top-level config fields differ from apply: top=%+v nested=%+v", result.ConfigResultFields, applied.ConfigResultFields)
		}
	})

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("dry-run commands changed target: %v", err)
	}
}

func assertLegacyConfigFields(t *testing.T, fields *ConfigResultFields) {
	t.Helper()
	if fields == nil || len(fields.ConfigResolutions) != 1 || len(fields.RestoreItems) != 1 {
		t.Fatalf("config fields = %+v", fields)
	}
	resolution := fields.ConfigResolutions[0]
	if resolution.Resolution != planner.ResolutionLegacyUnverified || resolution.Status != planner.StatusPlanned ||
		resolution.SourceGeneration != "" || resolution.TargetGeneration != "" {
		t.Fatalf("legacy resolution = %+v", resolution)
	}
}
