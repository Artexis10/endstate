// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"testing"
)

// Only an explicit Linux generation with a real desktop detector grants this lane.
func TestDesktopGenerationRequiresLinuxRestoreAndDesktopDetector(t *testing.T) {
	for _, test := range []struct {
		name, platform, strategy, detector string
		valid                              bool
	}{
		{"supported", "linux", "endstate-restore", "desktop", true},
		{"other-platform", "darwin", "endstate-restore", "desktop", false},
		{"other-strategy", "linux", "home-manager", "desktop", false},
		{"fake-path", "linux", "endstate-restore", "path", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := desktopsettings.Target{Schema: "org.gnome.desktop.interface", Path: "/org/gnome/desktop/interface/", Key: "clock-show-date", Type: "boolean"}
			detector := InstanceDetectorDef{ID: "session", Type: test.detector}
			if test.detector == "desktop" {
				detector.Desktop = &target
			} else {
				detector.Glob = "${home}"
			}
			module := Module{ModuleSchemaVersion: 3, ID: "desktop.clock", DisplayName: "Clock preference", Sensitivity: "low", Platforms: map[string]PlatformVariant{test.platform: {Realization: test.strategy, Config: &ConfigDef{InstanceDetectors: []InstanceDetectorDef{detector}, Sets: []ConfigSetDef{{ID: "preferences", Generations: []GenerationDef{{ID: "g1", Order: 1, Capture: &CaptureDef{DesktopValues: []CaptureDesktopValue{{Target: target, Dest: "clock.json"}}}, Restore: []RestoreDef{{Type: "desktop-value", Source: "clock.json", Desktop: &target}}}}}}}}}}
			raw, err := json.Marshal(module)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseModuleJSON(raw)
			if err == nil {
				err = validateModule(parsed, "fixture/module.jsonc")
			}
			if (err == nil) != test.valid {
				t.Fatalf("desktop validation: %v", err)
			}
			if test.valid {
				projected := ProjectModuleForPlatform(parsed, "linux")
				instances, err := DiscoverInstances(projected, nil, DiscoveryOptions{})
				if err == nil || len(instances) != 0 {
					t.Fatalf("unavailable production desktop fabricated an instance: %+v %v", instances, err)
				}
			}
		})
	}
}
