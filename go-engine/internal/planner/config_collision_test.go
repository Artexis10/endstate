// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestCompatibilityResolverResolveSources_TargetCollisions(t *testing.T) {
	tests := []struct {
		name       string
		sets       []collisionTestSet
		sourceSets []string
		wantFailed []string
	}{
		{
			name: "exact filesystem target",
			sets: []collisionTestSet{
				{id: "preferences", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("shared.json"))}},
				{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("merge-json", collisionPath("shared.json"))}},
			},
			sourceSets: []string{"preferences", "presets"},
			wantFailed: []string{"capture-preferences", "capture-presets"},
		},
		{
			name: "parent child filesystem targets",
			sets: []collisionTestSet{
				{id: "preferences", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("Config"))}},
				{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("append", collisionPath("Config", "child.json"))}},
			},
			sourceSets: []string{"preferences", "presets"},
			wantFailed: []string{"capture-preferences", "capture-presets"},
		},
		{
			name: "windows case folded target",
			sets: []collisionTestSet{
				{id: "preferences", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("Config", "Settings.JSON"))}},
				{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("merge-ini", collisionPath("config", "settings.json"))}},
			},
			sourceSets: []string{"preferences", "presets"},
			wantFailed: []string{"capture-preferences", "capture-presets"},
		},
		{
			name: "delete glob directory overlaps child",
			sets: []collisionTestSet{
				{id: "preferences", restore: []modules.RestoreDef{{Type: "delete-glob", Target: collisionPath("Cache"), Pattern: "*.tmp"}}},
				{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("Cache", "keep.json"))}},
			},
			sourceSets: []string{"preferences", "presets"},
			wantFailed: []string{"capture-preferences", "capture-presets"},
		},
		{
			name: "registry key and value are case insensitive",
			sets: []collisionTestSet{
				{id: "preferences", restore: []modules.RestoreDef{{Type: "registry-set", Key: `HKCU:\Software\Vendor\${instance.id}`, ValueName: "Setting"}}},
				{id: "presets", restore: []modules.RestoreDef{{Type: "registry-set", Key: `hkcu:\software\vendor\${instance.id}`, ValueName: "setting"}}},
			},
			sourceSets: []string{"preferences", "presets"},
			wantFailed: []string{"capture-preferences", "capture-presets"},
		},
		{
			name: "non-overlapping targets",
			sets: []collisionTestSet{
				{id: "preferences", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("Preferences", "settings.json"))}},
				{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("Presets", "settings.json"))}},
			},
			sourceSets: []string{"preferences", "presets"},
		},
		{
			name: "overlap within one config set transaction is allowed",
			sets: []collisionTestSet{{
				id: "preferences",
				restore: []modules.RestoreDef{
					collisionFileRestore("copy", collisionPath("Config")),
					collisionFileRestore("copy", collisionPath("Config", "child.json")),
				},
			}},
			sourceSets: []string{"preferences"},
		},
		{
			name: "unsupported restore kind fails closed",
			sets: []collisionTestSet{{
				id:      "preferences",
				restore: []modules.RestoreDef{{Type: "future-mutator", Target: collisionPath("unknown")}},
			}},
			sourceSets: []string{"preferences"},
			wantFailed: []string{"capture-preferences"},
		},
		{
			name: "failed closed partial claims do not poison safe sets",
			sets: []collisionTestSet{
				{
					id: "preferences",
					restore: []modules.RestoreDef{
						collisionFileRestore("copy", collisionPath("shared.json")),
						{Type: "future-mutator", Target: collisionPath("unknown")},
					},
				},
				{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("shared.json"))}},
			},
			sourceSets: []string{"preferences", "presets"},
			wantFailed: []string{"capture-preferences"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			current := collisionTestModule(tt.sets...)
			resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
			sources := make([]SourceCapture, len(tt.sourceSets))
			for index, setID := range tt.sourceSets {
				sources[index] = collisionTestSource(setID)
			}

			plan := resolver.ResolveSources(
				sources,
				map[string][]TargetInstance{current.ID: {collisionTestTarget("target-a", root)}},
				nil,
			)

			gotFailed := failedCaptureIDs(plan)
			if !reflect.DeepEqual(gotFailed, tt.wantFailed) {
				t.Fatalf("failed captures = %v, want %v; plan=%+v", gotFailed, tt.wantFailed, plan)
			}
			for _, set := range plan.Sets {
				failed := containsTestString(tt.wantFailed, set.Source.CaptureID)
				if failed {
					if set.Resolution.Resolution != ResolutionDirect || set.Resolution.Status != StatusFailed ||
						set.Resolution.Reason == nil || *set.Resolution.Reason != ReasonTargetCollision {
						t.Fatalf("colliding set = %+v, want direct/failed/target_collision", set.Resolution)
					}
				} else if set.Resolution.Status != "" || set.Resolution.Reason != nil {
					t.Fatalf("non-colliding set reason/status = %v/%q", set.Resolution.Reason, set.Resolution.Status)
				}
			}
		})
	}
}

func TestCompatibilityResolverResolveSources_SameTargetInstanceAndSetCompetition(t *testing.T) {
	root := t.TempDir()
	current := collisionTestModule(collisionTestSet{id: "preferences"})
	resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	left := collisionTestSource("preferences")
	right := collisionTestSource("preferences")
	right.CaptureID = "capture-preferences-b"

	plan := resolver.ResolveSources(
		[]SourceCapture{right, left},
		map[string][]TargetInstance{current.ID: {collisionTestTarget("target-a", root)}},
		nil,
	)

	want := []string{"capture-preferences", "capture-preferences-b"}
	if got := failedCaptureIDs(plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("competing captures = %v, want %v", got, want)
	}
}

func TestCompatibilityResolverResolveSources_ResolvedTargetsAreStableSortedAndCopied(t *testing.T) {
	root := t.TempDir()
	current := collisionTestModule(collisionTestSet{
		id: "preferences",
		restore: []modules.RestoreDef{
			collisionFileRestore("copy", collisionPath("z.json")),
			{Type: "registry-set", Key: `HKCU:\Software\Vendor\${instance.id}`, ValueName: "Setting"},
			collisionFileRestore("merge-json", collisionPath("a.json")),
			{Type: "delete-glob", Target: collisionPath("cache"), Pattern: "*.tmp"},
		},
	})
	resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	source := collisionTestSource("preferences")
	targets := map[string][]TargetInstance{current.ID: {collisionTestTarget("target-a", root)}}

	first := resolver.ResolveSources([]SourceCapture{source}, targets, nil)
	want := []string{
		filepath.Join(root, "a.json"),
		filepath.Join(root, "cache"),
		filepath.Join(root, "z.json"),
		`HKCU:\Software\Vendor\target-a\Setting`,
	}
	if got := first.Sets[0].Resolution.ResolvedTargets; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved targets = %#v, want %#v", got, want)
	}
	first.Sets[0].Resolution.ResolvedTargets[0] = "mutated"
	second := resolver.ResolveSources([]SourceCapture{source}, targets, nil)
	if got := second.Sets[0].Resolution.ResolvedTargets; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved target mutation leaked: %#v", got)
	}
}

func TestCompatibilityResolverResolveSources_CollisionResultsIgnoreInputOrder(t *testing.T) {
	root := t.TempDir()
	current := collisionTestModule(
		collisionTestSet{id: "preferences", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("Config"))}},
		collisionTestSet{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("Config", "child.json"))}},
	)
	resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	preferences := collisionTestSource("preferences")
	presets := collisionTestSource("presets")
	targetA := collisionTestTarget("target-a", root)
	targetB := collisionTestTarget("target-b", root)

	forward := resolver.ResolveSources(
		[]SourceCapture{preferences, presets},
		map[string][]TargetInstance{current.ID: {targetA, targetB}},
		map[string]string{preferences.CaptureID: targetA.ID, presets.CaptureID: targetB.ID},
	)
	reverse := resolver.ResolveSources(
		[]SourceCapture{presets, preferences},
		map[string][]TargetInstance{current.ID: {targetB, targetA}},
		map[string]string{preferences.CaptureID: targetA.ID, presets.CaptureID: targetB.ID},
	)

	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("input order changed collision plan:\nforward=%+v\nreverse=%+v", forward, reverse)
	}
}

type collisionTestSet struct {
	id      string
	restore []modules.RestoreDef
}

func collisionTestModule(sets ...collisionTestSet) *modules.Module {
	current := &modules.Module{
		ModuleSchemaVersion: 2,
		ID:                  "apps.example",
		Revision:            "restore-revision",
		Config:              &modules.ConfigDef{},
	}
	for _, set := range sets {
		current.Config.Sets = append(current.Config.Sets, modules.ConfigSetDef{
			ID: set.id,
			Generations: []modules.GenerationDef{{
				ID:          "g1",
				Order:       1,
				Fingerprint: "fingerprint-" + set.id,
				Restore:     set.restore,
			}},
		})
	}
	return current
}

func collisionTestSource(setID string) SourceCapture {
	source := compatibilityTestSource("g1", "fingerprint-"+setID)
	source.CaptureID = "capture-" + setID
	source.ConfigSetID = setID
	return source
}

func collisionTestTarget(id, root string) TargetInstance {
	target := selectionTestTarget(id, "1.5", "1.5")
	target.Root = root
	return target
}

func collisionFileRestore(kind, target string) modules.RestoreDef {
	return modules.RestoreDef{Type: kind, Source: "payload.json", Target: target}
}

func collisionPath(parts ...string) string {
	return filepath.Join(append([]string{"${instance.root}"}, parts...)...)
}

func failedCaptureIDs(plan ConfigPlan) []string {
	var captures []string
	for _, set := range plan.Sets {
		if set.Resolution.Status == StatusFailed {
			captures = append(captures, set.Source.CaptureID)
		}
	}
	sort.Strings(captures)
	return captures
}

func containsTestString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
