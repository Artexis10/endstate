// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestCompatibilityResolverResolveSources_TargetSelection(t *testing.T) {
	baseModule := selectionTestModule(true)
	tests := []struct {
		name       string
		current    *modules.Module
		source     SourceCapture
		targets    []TargetInstance
		mapping    string
		want       Resolution
		wantReason *ResolutionReason
		wantStatus TerminalStatus
		wantTarget string
	}{
		{
			name:       "zero detected",
			current:    baseModule,
			source:     selectionTestSource("g1", "1.5", "1.5"),
			want:       ResolutionUnknown,
			wantReason: reasonPointer(ReasonTargetNotDetected),
			wantStatus: StatusSkipped,
		},
		{
			name:       "one viable target",
			current:    baseModule,
			source:     selectionTestSource("g1", "", ""),
			targets:    []TargetInstance{selectionTestTarget("target-g2", "2.5", "2.5")},
			want:       ResolutionMigrate,
			wantTarget: "target-g2",
		},
		{
			name:    "unique exact raw version",
			current: baseModule,
			source:  selectionTestSource("g1", "2.5", ""),
			targets: []TargetInstance{
				selectionTestTarget("target-g3", "3.5", "3.5"),
				selectionTestTarget("target-g2", "2.5", "2.5"),
			},
			want:       ResolutionMigrate,
			wantTarget: "target-g2",
		},
		{
			name:    "unique exact normalized version",
			current: baseModule,
			source:  selectionTestSource("g1", "vendor-2.5", "2.5"),
			targets: []TargetInstance{
				selectionTestTarget("target-g3", "3.5", "3.5"),
				selectionTestTarget("target-g2", "2.5", "2.5"),
			},
			want:       ResolutionMigrate,
			wantTarget: "target-g2",
		},
		{
			name:    "multiple exact versions remain ambiguous",
			current: baseModule,
			source:  selectionTestSource("g1", "2.5", "2.5"),
			targets: []TargetInstance{
				selectionTestTarget("target-b", "2.5", "2.5"),
				selectionTestTarget("target-a", "2.5", "2.5"),
			},
			want:       ResolutionUnknown,
			wantReason: reasonPointer(ReasonAmbiguousTargetInstance),
			wantStatus: StatusSkipped,
		},
		{
			name:    "direct is not preferred over migration",
			current: baseModule,
			source:  selectionTestSource("g1", "", ""),
			targets: []TargetInstance{
				selectionTestTarget("target-direct", "1.5", "1.5"),
				selectionTestTarget("target-newest", "3.5", "3.5"),
			},
			want:       ResolutionUnknown,
			wantReason: reasonPointer(ReasonAmbiguousTargetInstance),
			wantStatus: StatusSkipped,
		},
		{
			name:    "explicit mapping selects viable target",
			current: baseModule,
			source:  selectionTestSource("g1", "1.5", "1.5"),
			targets: []TargetInstance{
				selectionTestTarget("target-direct", "1.5", "1.5"),
				selectionTestTarget("target-g2", "2.5", "2.5"),
			},
			mapping:    "target-g2",
			want:       ResolutionMigrate,
			wantTarget: "target-g2",
		},
		{
			name:       "explicit mapped target absent",
			current:    baseModule,
			source:     selectionTestSource("g1", "", ""),
			targets:    []TargetInstance{selectionTestTarget("target-direct", "1.5", "1.5")},
			mapping:    "missing-target",
			want:       ResolutionUnknown,
			wantReason: reasonPointer(ReasonMappedTargetNotDetected),
			wantStatus: StatusSkipped,
			wantTarget: "missing-target",
		},
		{
			name:       "explicit mapped target incompatible",
			current:    baseModule,
			source:     selectionTestSource("g3", "", ""),
			targets:    []TargetInstance{selectionTestTarget("target-g1", "1.5", "1.5")},
			mapping:    "target-g1",
			want:       ResolutionIncompatible,
			wantReason: reasonPointer(ReasonMappedTargetIncompatible),
			wantStatus: StatusSkipped,
			wantTarget: "target-g1",
		},
		{
			name:    "zero viable candidates share common outcome",
			current: baseModule,
			source:  selectionTestSource("g3", "", ""),
			targets: []TargetInstance{
				selectionTestTarget("target-g1b", "1.6", "1.6"),
				selectionTestTarget("target-g1a", "1.5", "1.5"),
			},
			want:       ResolutionIncompatible,
			wantReason: reasonPointer(ReasonDowngradeUnsupported),
			wantStatus: StatusSkipped,
		},
		{
			name:    "zero viable mixed outcomes are ambiguous",
			current: baseModule,
			source:  selectionTestSource("g2", "", ""),
			targets: []TargetInstance{
				selectionTestTarget("target-unknown", "9", "9"),
				selectionTestTarget("target-g1", "1.5", "1.5"),
			},
			want:       ResolutionUnknown,
			wantReason: reasonPointer(ReasonAmbiguousTargetInstance),
			wantStatus: StatusSkipped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewCompatibilityResolver(map[string]*modules.Module{tt.current.ID: tt.current}, nil)
			mappings := map[string]string{}
			if tt.mapping != "" {
				mappings[tt.source.CaptureID] = tt.mapping
			}

			plan := resolver.ResolveSources(
				[]SourceCapture{tt.source},
				map[string][]TargetInstance{tt.source.ModuleID: tt.targets},
				mappings,
			)

			if len(plan.Sets) != 1 {
				t.Fatalf("sets = %d, want 1", len(plan.Sets))
			}
			got := plan.Sets[0].Resolution
			if got.Resolution != tt.want || got.Status != tt.wantStatus ||
				!reflect.DeepEqual(got.Reason, tt.wantReason) || got.TargetInstanceID != tt.wantTarget {
				t.Fatalf("resolution = %+v, want %s/%v/%s target %q", got, tt.want, tt.wantReason, tt.wantStatus, tt.wantTarget)
			}
			if (tt.want == ResolutionDirect || tt.want == ResolutionMigrate) && (got.Reason != nil || got.Status != "") {
				t.Fatalf("viable selection reason/status = %v/%q, want nil/empty", got.Reason, got.Status)
			}
		})
	}
}

func TestCompatibilityResolverResolveSources_IsIndependentOfInputOrder(t *testing.T) {
	current := selectionTestModule(true)
	resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	source := selectionTestSource("g1", "2.5", "2.5")
	targets := []TargetInstance{
		selectionTestTarget("target-g3", "3.5", "3.5"),
		selectionTestTarget("target-g2", "2.5", "2.5"),
	}

	forward := resolver.ResolveSources([]SourceCapture{source}, map[string][]TargetInstance{source.ModuleID: targets}, nil)
	reverse := resolver.ResolveSources([]SourceCapture{source}, map[string][]TargetInstance{source.ModuleID: {targets[1], targets[0]}}, nil)

	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("input order changed plan:\nforward=%+v\nreverse=%+v", forward, reverse)
	}
}

func selectionTestModule(withMigrations bool) *modules.Module {
	current := compatibilityTestModule(
		compatibilityTestGeneration("g1", 1, ">=1 <2", "fingerprint-g1"),
		compatibilityTestGeneration("g2", 2, ">=2 <3", "fingerprint-g2"),
		compatibilityTestGeneration("g3", 3, ">=3 <4", "fingerprint-g3"),
	)
	if withMigrations {
		current.Config.Sets[0].Migrations = []modules.MigrationEdgeDef{
			compatibilityTestMigration("g2", "g3"),
			compatibilityTestMigration("g1", "g2"),
		}
	}
	return current
}

func selectionTestSource(generation, rawVersion, normalizedVersion string) SourceCapture {
	source := compatibilityTestSource(generation, "fingerprint-"+generation)
	source.Instance.RawVersion = rawVersion
	source.Instance.NormalizedVersion = normalizedVersion
	return source
}

func selectionTestTarget(id, rawVersion, normalizedVersion string) TargetInstance {
	target := compatibilityTestTarget(rawVersion)
	target.ID = id
	target.RawVersion = rawVersion
	target.NormalizedVersion = normalizedVersion
	return target
}
