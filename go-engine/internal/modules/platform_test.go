// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import "testing"

func TestFilterCatalogForPlatformTreatsEveryFlatModuleAsWindowsOnly(t *testing.T) {
	v1 := &Module{ID: "apps.legacy", Matches: MatchCriteria{PathExists: []string{"/tmp/coincidental"}}, Capture: &CaptureDef{Files: []CaptureFile{{Source: "/tmp/coincidental", Dest: "prefs"}}}}
	v2 := &Module{ModuleSchemaVersion: 2, ID: "apps.generation", Matches: MatchCriteria{PathExists: []string{"/tmp/coincidental"}}, Config: &ConfigDef{
		InstanceDetectors: []InstanceDetectorDef{{ID: "installed", Type: "package"}},
		Sets:              []ConfigSetDef{{ID: "preferences", Generations: []GenerationDef{{ID: "g1", Order: 1}}}},
	}}
	catalog := map[string]*Module{v1.ID: v1, v2.ID: v2}

	windows := FilterCatalogForPlatform(catalog, "windows")
	if len(windows) != 2 || windows[v1.ID] != v1 || windows[v2.ID] != v2 {
		t.Fatalf("Windows flat catalog changed: %+v", windows)
	}
	for _, platform := range []string{"linux", "darwin"} {
		if got := FilterCatalogForPlatform(catalog, platform); len(got) != 0 {
			t.Fatalf("%s catalog attached flat Windows modules: %+v", platform, got)
		}
	}
}

func TestFilterCatalogForPlatformDoesNotMutateInput(t *testing.T) {
	mod := &Module{ID: "apps.legacy"}
	catalog := map[string]*Module{mod.ID: mod}
	_ = FilterCatalogForPlatform(catalog, "linux")
	if len(catalog) != 1 || catalog[mod.ID] != mod {
		t.Fatalf("input catalog mutated: %+v", catalog)
	}
}
