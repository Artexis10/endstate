// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestMaterializeAllowsPackageTargetWithoutInstanceRoot(t *testing.T) {
	stageRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "settings.json")
	writeTestFile(t, filepath.Join(stageRoot, "settings.json"), `{"theme":"dark"}`)
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "copy", Source: "settings.json", Target: target,
	}}}
	plan := testPlan("", generation)

	materialized, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  plan,
	})
	if err != nil {
		t.Fatalf("Materialize package target: %v", err)
	}
	if len(materialized.Actions) != 1 || materialized.Actions[0].Target != target {
		t.Fatalf("materialized actions = %+v", materialized.Actions)
	}
}
