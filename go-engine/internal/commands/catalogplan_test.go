// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunCatalogPlan_EmitsCatalogOnlyNonSkippedModuleActions(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	t.Setenv("ENDSTATE_ROOT", root)

	raw, err := RunCatalogPlan(CatalogPlanFlags{Bundle: "bundles/dev-tools.jsonc"})
	if err != nil {
		t.Fatal(err)
	}
	data, ok := raw.(*CatalogPlanResult)
	if !ok {
		t.Fatalf("result = %T, want *CatalogPlanResult", raw)
	}
	if data.Proof != "catalog" || data.ActionCount == 0 || data.ActionCount != data.MembershipCount || len(data.Actions) != data.ActionCount {
		t.Fatalf("catalog plan = %+v", data)
	}
	if data.Bundle.Path != "bundles/dev-tools.jsonc" || strings.Contains(data.Bundle.Path, root) {
		t.Fatalf("bundle path = %q", data.Bundle.Path)
	}
	for _, action := range data.Actions {
		if action.Skipped || action.Status != "resolved" || !strings.HasPrefix(action.ModuleID, "apps.") || action.ValidationScenarioCount == 0 {
			t.Fatalf("action = %+v", action)
		}
	}
}
