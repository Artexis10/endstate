// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// TestCompatibilityResolver_ResolutionMatrix exercises the public planner seam.
// Focused unit tests in config_compatibility_test.go, config_selection_test.go,
// and config_collision_test.go retain the lower-level graph and path details.
func TestCompatibilityResolver_ResolutionMatrix(t *testing.T) {
	type outcome struct {
		captureID        string
		resolution       Resolution
		reason           *ResolutionReason
		status           TerminalStatus
		targetID         string
		targetGeneration string
		migrationPath    []string
	}
	type targetKnowledge struct {
		generation  string
		fingerprint string
		revision    string
	}
	type matrixCase struct {
		name         string
		resolve      func(*testing.T) ConfigPlan
		want         []outcome
		wantTargets  map[string]targetKnowledge
		forbidLegacy bool
	}

	basePlan := func(current *modules.Module, source SourceCapture, targets []TargetInstance, mapping string) ConfigPlan {
		resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
		mappings := map[string]string{}
		if mapping != "" {
			mappings[source.CaptureID] = mapping
		}
		return resolver.ResolveSources(
			[]SourceCapture{source},
			map[string][]TargetInstance{source.ModuleID: targets},
			mappings,
		)
	}

	cases := []matrixCase{
		{
			name: "current fingerprint is direct",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "1.5", "1.5"), []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "")
			},
			want:        []outcome{{resolution: ResolutionDirect, targetID: "target-g1", targetGeneration: "g1", migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{"target-g1": {generation: "g1", fingerprint: "fingerprint-g1", revision: "restore-revision"}},
		},
		{
			name: "accepted historical fingerprint is generation scoped",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				current.Config.Sets[0].Generations[0].AcceptsSourceFingerprints = []string{"historical-g1"}
				source := selectionTestSource("g1", "1.5", "1.5")
				source.GenerationFingerprint = "historical-g1"
				return basePlan(current, source, []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "")
			},
			want: []outcome{{resolution: ResolutionDirect, targetID: "target-g1", targetGeneration: "g1", migrationPath: []string{}}},
		},
		{
			name: "fingerprint accepted only by another generation is rejected",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				current.Config.Sets[0].Generations[1].AcceptsSourceFingerprints = []string{"historical-g1"}
				source := selectionTestSource("g1", "1.5", "1.5")
				source.GenerationFingerprint = "historical-g1"
				return basePlan(current, source, []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "")
			},
			want:         []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonSourceGenerationDefinitionChanged), status: StatusSkipped, migrationPath: []string{}}},
			forbidLegacy: true,
		},
		{
			name: "unique forward route migrates",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "", ""), []TargetInstance{selectionTestTarget("target-g3", "3.5", "3.5")}, "")
			},
			want:        []outcome{{resolution: ResolutionMigrate, targetID: "target-g3", targetGeneration: "g3", migrationPath: []string{"g1", "g2", "g3"}}},
			wantTargets: map[string]targetKnowledge{"target-g3": {generation: "g3", fingerprint: "fingerprint-g3", revision: "restore-revision"}},
		},
		{
			name: "unknown target generation",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "", ""), []TargetInstance{selectionTestTarget("target-unknown", "9", "9")}, "")
			},
			want:        []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonUnknownGeneration), status: StatusSkipped, migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{"target-unknown": {revision: "restore-revision"}},
		},
		{
			name: "ambiguous target generation",
			resolve: func(*testing.T) ConfigPlan {
				current := compatibilityTestModule(
					compatibilityTestGeneration("g1", 1, ">=1", "fingerprint-g1"),
					compatibilityTestGeneration("g2", 2, ">=1", "fingerprint-g2"),
				)
				return basePlan(current, selectionTestSource("g1", "", ""), []TargetInstance{selectionTestTarget("target-ambiguous", "1.5", "1.5")}, "")
			},
			want:        []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonAmbiguousGeneration), status: StatusSkipped, migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{"target-ambiguous": {revision: "restore-revision"}},
		},
		{
			name: "downgrade is incompatible",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g3", "", ""), []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "")
			},
			want:        []outcome{{resolution: ResolutionIncompatible, reason: reasonPointer(ReasonDowngradeUnsupported), status: StatusSkipped, migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{"target-g1": {generation: "g1", fingerprint: "fingerprint-g1", revision: "restore-revision"}},
		},
		{
			name: "missing route is incompatible",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(false)
				return basePlan(current, selectionTestSource("g1", "", ""), []TargetInstance{selectionTestTarget("target-g2", "2.5", "2.5")}, "")
			},
			want:        []outcome{{resolution: ResolutionIncompatible, reason: reasonPointer(ReasonMigrationPathMissing), status: StatusSkipped, migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{"target-g2": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"}},
		},
		{
			name: "unique exact version wins side by side",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "2.5", "2.5"), []TargetInstance{
					selectionTestTarget("target-g3", "3.5", "3.5"),
					selectionTestTarget("target-g2", "2.5", "2.5"),
				}, "")
			},
			want: []outcome{{resolution: ResolutionMigrate, targetID: "target-g2", targetGeneration: "g2", migrationPath: []string{"g1", "g2"}}},
			wantTargets: map[string]targetKnowledge{
				"target-g2": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"},
				"target-g3": {generation: "g3", fingerprint: "fingerprint-g3", revision: "restore-revision"},
			},
		},
		{
			name: "multiple exact versions remain ambiguous",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "2.5", "2.5"), []TargetInstance{
					selectionTestTarget("target-b", "2.5", "2.5"),
					selectionTestTarget("target-a", "2.5", "2.5"),
				}, "")
			},
			want: []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonAmbiguousTargetInstance), status: StatusSkipped, migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{
				"target-a": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"},
				"target-b": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"},
			},
		},
		{
			name: "no target is detected",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "", ""), nil, "")
			},
			want: []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonTargetNotDetected), status: StatusSkipped, migrationPath: []string{}}},
		},
		{
			name: "mapped target preserves every discovered candidate",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "", ""), []TargetInstance{
					selectionTestTarget("target-g3", "3.5", "3.5"),
					selectionTestTarget("target-g2", "2.5", "2.5"),
				}, "target-g2")
			},
			want: []outcome{{resolution: ResolutionMigrate, targetID: "target-g2", targetGeneration: "g2", migrationPath: []string{"g1", "g2"}}},
			wantTargets: map[string]targetKnowledge{
				"target-g2": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"},
				"target-g3": {generation: "g3", fingerprint: "fingerprint-g3", revision: "restore-revision"},
			},
		},
		{
			name: "mapped target is absent",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g1", "", ""), []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "missing-target")
			},
			want:        []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonMappedTargetNotDetected), status: StatusSkipped, targetID: "missing-target", migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{"target-g1": {generation: "g1", fingerprint: "fingerprint-g1", revision: "restore-revision"}},
		},
		{
			name: "mapped target is incompatible",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				return basePlan(current, selectionTestSource("g3", "", ""), []TargetInstance{
					selectionTestTarget("target-g2", "2.5", "2.5"),
					selectionTestTarget("target-g1", "1.5", "1.5"),
				}, "target-g1")
			},
			want: []outcome{{resolution: ResolutionIncompatible, reason: reasonPointer(ReasonMappedTargetIncompatible), status: StatusSkipped, targetID: "target-g1", targetGeneration: "g1", migrationPath: []string{}}},
			wantTargets: map[string]targetKnowledge{
				"target-g1": {generation: "g1", fingerprint: "fingerprint-g1", revision: "restore-revision"},
				"target-g2": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"},
			},
		},
		{
			name: "collisions fail only colliding sets",
			resolve: func(t *testing.T) ConfigPlan {
				root := t.TempDir()
				current := collisionTestModule(
					collisionTestSet{id: "preferences", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("shared.json"))}},
					collisionTestSet{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("merge-json", collisionPath("shared.json"))}},
				)
				resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
				return resolver.ResolveSources(
					[]SourceCapture{collisionTestSource("preferences"), collisionTestSource("presets")},
					map[string][]TargetInstance{current.ID: {collisionTestTarget("target-a", root)}},
					nil,
				)
			},
			want: []outcome{
				{captureID: "capture-preferences", resolution: ResolutionDirect, reason: reasonPointer(ReasonTargetCollision), status: StatusFailed, targetID: "target-a", targetGeneration: "g1", migrationPath: []string{}},
				{captureID: "capture-presets", resolution: ResolutionDirect, reason: reasonPointer(ReasonTargetCollision), status: StatusFailed, targetID: "target-a", targetGeneration: "g1", migrationPath: []string{}},
			},
		},
		{
			name: "payload failure is isolated per set",
			resolve: func(t *testing.T) ConfigPlan {
				root := t.TempDir()
				current := collisionTestModule(
					collisionTestSet{id: "preferences", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("preferences.json"))}},
					collisionTestSet{id: "presets", restore: []modules.RestoreDef{collisionFileRestore("copy", collisionPath("presets.json"))}},
				)
				failed := collisionTestSource("preferences")
				failed.PayloadIntegrityFailed = true
				resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
				return resolver.ResolveSources(
					[]SourceCapture{collisionTestSource("presets"), failed},
					map[string][]TargetInstance{current.ID: {collisionTestTarget("target-a", root)}},
					nil,
				)
			},
			want: []outcome{
				{captureID: "capture-preferences", resolution: ResolutionUnknown, reason: reasonPointer(ReasonPayloadIntegrityFailed), status: StatusFailed, migrationPath: []string{}},
				{captureID: "capture-presets", resolution: ResolutionDirect, targetID: "target-a", targetGeneration: "g1", migrationPath: []string{}},
			},
			forbidLegacy: true,
		},
		{
			name: "unsupported capture schema does not fall back",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				source := selectionTestSource("g1", "", "")
				source.CaptureModuleSchemaVersion = 1
				return basePlan(current, source, []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "")
			},
			want:         []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonUnsupportedModuleSchema), status: StatusSkipped, migrationPath: []string{}}},
			forbidLegacy: true,
		},
		{
			name: "missing catalog module does not fall back",
			resolve: func(*testing.T) ConfigPlan {
				source := selectionTestSource("g1", "", "")
				resolver := NewCompatibilityResolver(nil, nil)
				return resolver.ResolveSources([]SourceCapture{source}, map[string][]TargetInstance{source.ModuleID: {selectionTestTarget("target-g1", "1.5", "1.5")}}, nil)
			},
			want:         []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonCatalogModuleMissing), status: StatusSkipped, migrationPath: []string{}}},
			forbidLegacy: true,
		},
		{
			name: "missing config set does not fall back",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				source := selectionTestSource("g1", "", "")
				source.ConfigSetID = "missing-set"
				return basePlan(current, source, []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "")
			},
			want:         []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonConfigSetMissing), status: StatusSkipped, migrationPath: []string{}}},
			forbidLegacy: true,
		},
		{
			name: "missing source generation does not fall back",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				source := selectionTestSource("missing-generation", "", "")
				return basePlan(current, source, []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")}, "")
			},
			want:         []outcome{{resolution: ResolutionUnknown, reason: reasonPointer(ReasonSourceGenerationUnknown), status: StatusSkipped, migrationPath: []string{}}},
			forbidLegacy: true,
		},
		{
			name: "post-install plan replaces stale target evidence",
			resolve: func(t *testing.T) ConfigPlan {
				current := selectionTestModule(true)
				resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
				source := selectionTestSource("g1", "1.5", "1.5")
				preview := resolver.ResolveSources(
					[]SourceCapture{source},
					map[string][]TargetInstance{current.ID: {selectionTestTarget("target-a", "1.5", "1.5")}},
					nil,
				)
				final := resolver.ResolveSources(
					[]SourceCapture{source},
					map[string][]TargetInstance{current.ID: {selectionTestTarget("target-a", "2.5", "2.5")}},
					nil,
				)
				if got := preview.Sets[0].Resolution; got.Resolution != ResolutionDirect || got.TargetGeneration != "g1" {
					t.Fatalf("preview resolution mutated after final planning: %+v", got)
				}
				if got := final.Sets[0].TargetInstances[0].RawVersion; got != "2.5" {
					t.Fatalf("final target raw version = %q, want post-install 2.5", got)
				}
				return final
			},
			want:        []outcome{{resolution: ResolutionMigrate, targetID: "target-a", targetGeneration: "g2", migrationPath: []string{"g1", "g2"}}},
			wantTargets: map[string]targetKnowledge{"target-a": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"}},
		},
		{
			name: "catalog snapshot is pinned before later edits",
			resolve: func(*testing.T) ConfigPlan {
				current := selectionTestModule(true)
				resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
				current.Revision = "edited-revision"
				current.Config.Sets[0].Generations[0].Fingerprint = "edited-g1"
				current.Config.Sets[0].Generations[1].Fingerprint = "edited-g2"
				current.Config.Sets[0].Migrations[0].From = "edited-source"
				return resolver.ResolveSources(
					[]SourceCapture{selectionTestSource("g1", "", "")},
					map[string][]TargetInstance{current.ID: {selectionTestTarget("target-g2", "2.5", "2.5")}},
					nil,
				)
			},
			want:        []outcome{{resolution: ResolutionMigrate, targetID: "target-g2", targetGeneration: "g2", migrationPath: []string{"g1", "g2"}}},
			wantTargets: map[string]targetKnowledge{"target-g2": {generation: "g2", fingerprint: "fingerprint-g2", revision: "restore-revision"}},
		},
	}

	coveredReasons := make(map[ResolutionReason]bool)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			plan := tt.resolve(t)
			if len(plan.Sets) != len(tt.want) {
				t.Fatalf("sets = %d, want %d", len(plan.Sets), len(tt.want))
			}
			byCapture := make(map[string]PlanSet, len(plan.Sets))
			for _, set := range plan.Sets {
				byCapture[set.Source.CaptureID] = set
			}
			for _, want := range tt.want {
				captureID := want.captureID
				if captureID == "" {
					captureID = "capture-a"
				}
				set, ok := byCapture[captureID]
				if !ok {
					t.Fatalf("capture %q missing from plan %+v", captureID, plan)
				}
				got := set.Resolution
				if got.Resolution != want.resolution || !reflect.DeepEqual(got.Reason, want.reason) ||
					got.Status != want.status || got.TargetInstanceID != want.targetID ||
					got.TargetGeneration != want.targetGeneration || !reflect.DeepEqual(got.MigrationPath, want.migrationPath) {
					t.Fatalf("resolution = %+v, want %s/%v/%s target=%q generation=%q path=%v",
						got, want.resolution, want.reason, want.status, want.targetID, want.targetGeneration, want.migrationPath)
				}
				if tt.forbidLegacy && got.Resolution == ResolutionLegacyUnverified {
					t.Fatalf("v2 capture fell back to legacy resolution: %+v", got)
				}
				if want.reason != nil {
					coveredReasons[*want.reason] = true
				}
			}
			for targetID, knowledge := range tt.wantTargets {
				var target *TargetInstance
				for setIndex := range plan.Sets {
					for targetIndex := range plan.Sets[setIndex].TargetInstances {
						candidate := &plan.Sets[setIndex].TargetInstances[targetIndex]
						if candidate.ID == targetID {
							target = candidate
							break
						}
					}
					if target != nil {
						break
					}
				}
				if target == nil {
					t.Fatalf("target candidate %q missing from plan %+v", targetID, plan)
				}
				if target.Generation != knowledge.generation || target.GenerationFingerprint != knowledge.fingerprint ||
					target.ModuleRevision != knowledge.revision {
					t.Fatalf("target %q knowledge = %q/%q/%q, want %q/%q/%q",
						target.ID, target.Generation, target.GenerationFingerprint, target.ModuleRevision,
						knowledge.generation, knowledge.fingerprint, knowledge.revision)
				}
			}
		})
	}

	wantReasons := []ResolutionReason{
		ReasonUnknownGeneration,
		ReasonAmbiguousGeneration,
		ReasonDowngradeUnsupported,
		ReasonMigrationPathMissing,
		ReasonAmbiguousTargetInstance,
		ReasonTargetNotDetected,
		ReasonMappedTargetNotDetected,
		ReasonMappedTargetIncompatible,
		ReasonTargetCollision,
		ReasonPayloadIntegrityFailed,
		ReasonUnsupportedModuleSchema,
		ReasonCatalogModuleMissing,
		ReasonConfigSetMissing,
		ReasonSourceGenerationUnknown,
		ReasonSourceGenerationDefinitionChanged,
	}
	sort.Slice(wantReasons, func(left, right int) bool { return wantReasons[left] < wantReasons[right] })
	var gotReasons []ResolutionReason
	for reason := range coveredReasons {
		gotReasons = append(gotReasons, reason)
	}
	sort.Slice(gotReasons, func(left, right int) bool { return gotReasons[left] < gotReasons[right] })
	if !reflect.DeepEqual(gotReasons, wantReasons) {
		t.Fatalf("resolver reason coverage = %v, want %v", gotReasons, wantReasons)
	}
}
