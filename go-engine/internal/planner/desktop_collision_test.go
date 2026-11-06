// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"testing"
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
