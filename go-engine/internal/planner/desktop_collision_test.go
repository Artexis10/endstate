// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// Schema aliases must conflict on the backing key; case-distinct keys must coexist.
func TestDesktopCollisionUsesCaseSensitiveBackingKeyAcrossSchemas(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(map[bool]string{true: "same-key", false: "case-distinct"}[sameKey], func(t *testing.T) {
			left := desktopsettings.Target{Schema: "org.example.one", Path: "/org/example/", Key: "clock-show-date", Type: "boolean"}
			right := left
			right.Schema = "org.example.two"
			if !sameKey {
				right.Key = "Clock-show-date"
			}
			current := collisionTestModule(collisionTestSet{id: "preferences", restore: []modules.RestoreDef{{Type: "desktop-value", Source: "clock.json", Desktop: &left}}}, collisionTestSet{id: "presets", restore: []modules.RestoreDef{{Type: "desktop-value", Source: "clock.json", Desktop: &right}}})
			resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
			plan := resolver.ResolveSources([]SourceCapture{collisionTestSource("preferences"), collisionTestSource("presets")}, map[string][]TargetInstance{current.ID: {collisionTestTarget("desktop", "")}}, nil)
			failed := failedCaptureIDs(plan)
			if sameKey && len(failed) != 2 || !sameKey && len(failed) != 0 {
				t.Fatalf("desktop collisions=%v", failed)
			}
		})
	}
}

// A restore of the dconf user database, or a directory above it, would be undone by
// the session's dconf writer on its next write, so it must conflict with every GNOME value.
func TestDesktopValueConflictsWithRestoreOfDconfDatabase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG coordinates are not valid on Windows")
	}
	t.Setenv("HOME", "/home/test")
	t.Setenv("XDG_CONFIG_HOME", "/home/test/.config")
	clock := desktopsettings.Target{Schema: "org.gnome.desktop.interface", Path: "/org/gnome/desktop/interface/", Key: "clock-show-date", Type: "boolean"}
	for _, test := range []struct {
		target   string
		conflict bool
	}{
		{"${xdg.config}/dconf/user", true},
		{"${xdg.config}", true},
		{"${xdg.config}/dconf-keyfiles/user", false},
	} {
		t.Run(test.target, func(t *testing.T) {
			current := collisionTestModule(
				collisionTestSet{id: "preferences", restore: []modules.RestoreDef{{Type: "desktop-value", Source: "clock.json", Desktop: &clock}}},
				collisionTestSet{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("copy", test.target)}},
			)
			resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
			plan := resolver.ResolveSources([]SourceCapture{collisionTestSource("preferences"), collisionTestSource("presets")}, map[string][]TargetInstance{current.ID: {collisionTestTarget("desktop", "")}}, nil)
			if failed := failedCaptureIDs(plan); test.conflict && len(failed) != 2 || !test.conflict && len(failed) != 0 {
				t.Fatalf("collisions=%v", failed)
			}
		})
	}
}
