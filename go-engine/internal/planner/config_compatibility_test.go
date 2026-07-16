// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestCompatibilityResolverResolveCandidate_Direct(t *testing.T) {
	current := compatibilityTestModule(
		compatibilityTestGeneration("g1", 1, "<2", "fingerprint-g1"),
	)
	resolver := NewCompatibilityResolver(
		map[string]*modules.Module{current.ID: current},
		nil,
	)

	plan := resolver.ResolveCandidate(
		compatibilityTestSource("g1", "fingerprint-g1"),
		compatibilityTestTarget("1.5"),
	)

	want := ConfigResolution{
		CaptureID:                   "capture-a",
		ModuleID:                    "apps.example",
		ConfigSetID:                 "preferences",
		SourceInstanceID:            "source-a",
		TargetInstanceID:            "target-a",
		SourceGeneration:            "g1",
		SourceGenerationFingerprint: "fingerprint-g1",
		TargetGeneration:            "g1",
		Resolution:                  ResolutionDirect,
		MigrationPath:               []string{},
		CaptureModuleRevision:       "capture-revision",
		RestoreModuleRevision:       "restore-revision",
		ResolvedTargets:             []string{},
	}
	if !reflect.DeepEqual(plan.Resolution, want) {
		t.Fatalf("resolution = %+v, want %+v", plan.Resolution, want)
	}
	if plan.Resolution.Reason != nil {
		t.Fatalf("successful direct reason = %q, want nil", *plan.Resolution.Reason)
	}
	if plan.TargetGenerationDef == nil || plan.TargetGenerationDef.ID != "g1" {
		t.Fatalf("target generation definition = %+v, want copied g1", plan.TargetGenerationDef)
	}
}

func TestCompatibilityResolverResolveCandidate_SourceAuthorityFailures(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*SourceCapture, *modules.Module)
		without     bool
		diagnostics []modules.CatalogDiagnostic
		wantReason  ResolutionReason
		wantStatus  TerminalStatus
	}{
		{
			name: "unsupported capture schema wins before missing catalog",
			mutate: func(source *SourceCapture, _ *modules.Module) {
				source.CaptureModuleSchemaVersion = 1
			},
			without:    true,
			wantReason: ReasonUnsupportedModuleSchema,
			wantStatus: StatusSkipped,
		},
		{
			name:    "matching rejected module diagnostic",
			without: true,
			diagnostics: []modules.CatalogDiagnostic{{
				Code:     modules.DiagnosticInvalidID,
				ModuleID: "apps.example",
			}},
			wantReason: ReasonUnsupportedModuleSchema,
			wantStatus: StatusSkipped,
		},
		{
			name:       "catalog module missing",
			without:    true,
			wantReason: ReasonCatalogModuleMissing,
			wantStatus: StatusSkipped,
		},
		{
			name: "current module schema is not v2",
			mutate: func(_ *SourceCapture, current *modules.Module) {
				current.ModuleSchemaVersion = 0
			},
			wantReason: ReasonUnsupportedModuleSchema,
			wantStatus: StatusSkipped,
		},
		{
			name: "config set missing",
			mutate: func(source *SourceCapture, _ *modules.Module) {
				source.ConfigSetID = "workspaces"
			},
			wantReason: ReasonConfigSetMissing,
			wantStatus: StatusSkipped,
		},
		{
			name: "source generation missing",
			mutate: func(source *SourceCapture, _ *modules.Module) {
				source.Generation = "g0"
			},
			wantReason: ReasonSourceGenerationUnknown,
			wantStatus: StatusSkipped,
		},
		{
			name: "source generation definition changed",
			mutate: func(source *SourceCapture, _ *modules.Module) {
				source.GenerationFingerprint = "unaccepted-fingerprint"
			},
			wantReason: ReasonSourceGenerationDefinitionChanged,
			wantStatus: StatusSkipped,
		},
		{
			name: "payload integrity failed",
			mutate: func(source *SourceCapture, _ *modules.Module) {
				source.PayloadIntegrityFailed = true
			},
			wantReason: ReasonPayloadIntegrityFailed,
			wantStatus: StatusFailed,
		},
		{
			name: "payload integrity failure wins before missing catalog",
			mutate: func(source *SourceCapture, _ *modules.Module) {
				source.PayloadIntegrityFailed = true
			},
			without:    true,
			wantReason: ReasonPayloadIntegrityFailed,
			wantStatus: StatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := compatibilityTestModule(
				compatibilityTestGeneration("g1", 1, "<2", "fingerprint-g1"),
			)
			source := compatibilityTestSource("g1", "fingerprint-g1")
			if tt.mutate != nil {
				tt.mutate(&source, current)
			}
			catalog := map[string]*modules.Module{current.ID: current}
			if tt.without {
				catalog = nil
			}
			resolver := NewCompatibilityResolver(catalog, tt.diagnostics)

			got := resolver.ResolveCandidate(source, compatibilityTestTarget("1.5")).Resolution

			if got.Resolution != ResolutionUnknown || got.Status != tt.wantStatus || got.Reason == nil || *got.Reason != tt.wantReason {
				t.Fatalf("resolution = %+v, want unknown/%s/%s", got, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

func TestCompatibilityResolverResolveCandidate_HistoricalFingerprintAcceptanceIsGenerationScoped(t *testing.T) {
	tests := []struct {
		name       string
		acceptOnG1 []string
		acceptOnG2 []string
		want       Resolution
		wantReason *ResolutionReason
	}{
		{
			name:       "accepted by same generation",
			acceptOnG1: []string{"historical-g1"},
			want:       ResolutionDirect,
		},
		{
			name:       "accepted only by another generation",
			acceptOnG2: []string{"historical-g1"},
			want:       ResolutionUnknown,
			wantReason: reasonPointer(ReasonSourceGenerationDefinitionChanged),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g1 := compatibilityTestGeneration("g1", 1, "<2", "fingerprint-g1")
			g1.AcceptsSourceFingerprints = tt.acceptOnG1
			g2 := compatibilityTestGeneration("g2", 2, ">=2", "fingerprint-g2")
			g2.AcceptsSourceFingerprints = tt.acceptOnG2
			current := compatibilityTestModule(g1, g2)
			resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)

			got := resolver.ResolveCandidate(
				compatibilityTestSource("g1", "historical-g1"),
				compatibilityTestTarget("1.5"),
			).Resolution

			if got.Resolution != tt.want || !reflect.DeepEqual(got.Reason, tt.wantReason) {
				t.Fatalf("resolution/reason = %s/%v, want %s/%v", got.Resolution, got.Reason, tt.want, tt.wantReason)
			}
		})
	}
}

func TestCompatibilityResolverResolveCandidate_CompatibilityOutcomes(t *testing.T) {
	chainModule := func() *modules.Module {
		current := compatibilityTestModule(
			compatibilityTestGeneration("g1", 1, "<2", "fingerprint-g1"),
			compatibilityTestGeneration("g2", 2, ">=2 <3", "fingerprint-g2"),
			compatibilityTestGeneration("g3", 3, ">=3 <4", "fingerprint-g3"),
		)
		current.Config.Sets[0].Migrations = []modules.MigrationEdgeDef{
			compatibilityTestMigration("g2", "g3"),
			compatibilityTestMigration("g1", "g2"),
		}
		return current
	}
	orderedChain := chainModule()
	orderedChain.Config.Sets[0].Migrations = []modules.MigrationEdgeDef{
		compatibilityTestMigration("g1", "g2"),
		compatibilityTestMigration("g2", "g3"),
	}

	tests := []struct {
		name       string
		current    *modules.Module
		source     SourceCapture
		target     TargetInstance
		want       Resolution
		wantReason *ResolutionReason
		wantPath   []string
		wantEdges  []string
		wantStatus TerminalStatus
	}{
		{
			name:      "unique forward chain from reversed declarations",
			current:   chainModule(),
			source:    compatibilityTestSource("g1", "fingerprint-g1"),
			target:    compatibilityTestTarget("3.5"),
			want:      ResolutionMigrate,
			wantPath:  []string{"g1", "g2", "g3"},
			wantEdges: []string{"g1->g2", "g2->g3"},
		},
		{
			name:      "same deterministic chain from forward declarations",
			current:   orderedChain,
			source:    compatibilityTestSource("g1", "fingerprint-g1"),
			target:    compatibilityTestTarget("3.5"),
			want:      ResolutionMigrate,
			wantPath:  []string{"g1", "g2", "g3"},
			wantEdges: []string{"g1->g2", "g2->g3"},
		},
		{
			name:       "downgrade",
			current:    chainModule(),
			source:     compatibilityTestSource("g3", "fingerprint-g3"),
			target:     compatibilityTestTarget("1.5"),
			want:       ResolutionIncompatible,
			wantReason: reasonPointer(ReasonDowngradeUnsupported),
			wantPath:   []string{},
			wantStatus: StatusSkipped,
		},
		{
			name: "higher generation without route",
			current: compatibilityTestModule(
				compatibilityTestGeneration("g1", 1, "<2", "fingerprint-g1"),
				compatibilityTestGeneration("g3", 3, ">=3 <4", "fingerprint-g3"),
			),
			source:     compatibilityTestSource("g1", "fingerprint-g1"),
			target:     compatibilityTestTarget("3.5"),
			want:       ResolutionIncompatible,
			wantReason: reasonPointer(ReasonMigrationPathMissing),
			wantPath:   []string{},
			wantStatus: StatusSkipped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewCompatibilityResolver(map[string]*modules.Module{tt.current.ID: tt.current}, nil)

			got := resolver.ResolveCandidate(tt.source, tt.target)

			if got.Resolution.Resolution != tt.want || got.Resolution.Status != tt.wantStatus ||
				!reflect.DeepEqual(got.Resolution.Reason, tt.wantReason) ||
				!reflect.DeepEqual(got.Resolution.MigrationPath, tt.wantPath) {
				t.Fatalf("resolution = %+v, want %s/%v path %v", got.Resolution, tt.want, tt.wantReason, tt.wantPath)
			}
			if gotEdges := migrationEdgeIDs(got.MigrationEdges); !reflect.DeepEqual(gotEdges, tt.wantEdges) {
				t.Fatalf("migration edges = %v, want %v", gotEdges, tt.wantEdges)
			}
			if (tt.want == ResolutionDirect || tt.want == ResolutionMigrate) && got.Resolution.Reason != nil {
				t.Fatalf("successful compatibility reason = %v, want nil", got.Resolution.Reason)
			}
		})
	}
}

func TestCompatibilityResolverResolveCandidate_MapsTargetGenerationSelectionErrors(t *testing.T) {
	tests := []struct {
		name        string
		generations []modules.GenerationDef
		target      string
		wantReason  ResolutionReason
	}{
		{
			name: "unknown target generation",
			generations: []modules.GenerationDef{
				compatibilityTestGeneration("g1", 1, "<2", "fingerprint-g1"),
			},
			target:     "5",
			wantReason: ReasonUnknownGeneration,
		},
		{
			name: "ambiguous target generation",
			generations: []modules.GenerationDef{
				compatibilityTestGeneration("g1", 1, ">=1", "fingerprint-g1"),
				compatibilityTestGeneration("g2", 2, ">=1", "fingerprint-g2"),
			},
			target:     "1.5",
			wantReason: ReasonAmbiguousGeneration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := compatibilityTestModule(tt.generations...)
			resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)

			got := resolver.ResolveCandidate(
				compatibilityTestSource("g1", "fingerprint-g1"),
				compatibilityTestTarget(tt.target),
			).Resolution

			if got.Resolution != ResolutionUnknown || got.Status != StatusSkipped || got.Reason == nil || *got.Reason != tt.wantReason {
				t.Fatalf("resolution = %+v, want unknown/skipped/%s", got, tt.wantReason)
			}
		})
	}
}

func TestCompatibilityResolverPinsCatalogAndReturnsIndependentMigrationPlans(t *testing.T) {
	g1 := compatibilityTestGeneration("g1", 1, "<2", "fingerprint-g1")
	g2 := compatibilityTestGeneration("g2", 2, ">=2 <3", "fingerprint-g2")
	g3 := compatibilityTestGeneration("g3", 3, ">=3 <4", "fingerprint-g3")
	g3.Restore = []modules.RestoreDef{{
		Type:    "copy",
		Source:  "g3.json",
		Target:  `${instance.root}\g3.json`,
		Exclude: []string{"cache/**"},
	}}
	current := compatibilityTestModule(g1, g2, g3)
	firstEdge := compatibilityTestMigration("g1", "g2")
	firstEdge.Operations[0].Value = map[string]any{
		"nested": []any{"original"},
	}
	current.Config.Sets[0].Migrations = []modules.MigrationEdgeDef{
		compatibilityTestMigration("g2", "g3"),
		firstEdge,
	}
	resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)

	current.Revision = "mutated-revision"
	current.Config.Sets[0].Generations[0].Fingerprint = "mutated-fingerprint"
	current.Config.Sets[0].Generations[2].Restore[0].Target = "mutated-target"
	current.Config.Sets[0].Generations[2].Restore[0].Exclude[0] = "mutated-exclude"
	current.Config.Sets[0].Migrations[1].From = "mutated-source"
	current.Config.Sets[0].Migrations[1].Operations[0].Source = "mutated-source.json"
	current.Config.Sets[0].Migrations[1].Operations[0].Value.(map[string]any)["nested"].([]any)[0] = "mutated"

	first := resolver.ResolveCandidate(
		compatibilityTestSource("g1", "fingerprint-g1"),
		compatibilityTestTarget("3.5"),
	)

	if first.Resolution.Resolution != ResolutionMigrate || first.Resolution.RestoreModuleRevision != "restore-revision" ||
		!reflect.DeepEqual(first.Resolution.MigrationPath, []string{"g1", "g2", "g3"}) {
		t.Fatalf("resolution changed with caller catalog mutation: %+v", first.Resolution)
	}
	if got := first.TargetGenerationDef.Restore[0]; got.Target != `${instance.root}\g3.json` ||
		!reflect.DeepEqual(got.Exclude, []string{"cache/**"}) {
		t.Fatalf("target generation was not deeply pinned: %+v", got)
	}
	value := first.MigrationEdges[0].Operations[0].Value.(map[string]any)
	if got := value["nested"].([]any)[0]; got != "original" {
		t.Fatalf("migration operation value = %v, want original", got)
	}

	first.TargetGenerationDef.Restore[0].Target = "plan-mutated-target"
	first.MigrationEdges[0].Operations[0].Source = "plan-mutated-source"
	first.MigrationEdges[0].Operations[0].Value.(map[string]any)["nested"].([]any)[0] = "plan-mutated"
	second := resolver.ResolveCandidate(
		compatibilityTestSource("g1", "fingerprint-g1"),
		compatibilityTestTarget("3.5"),
	)
	if second.TargetGenerationDef.Restore[0].Target != `${instance.root}\g3.json` ||
		second.MigrationEdges[0].Operations[0].Source != "g1.json" ||
		second.MigrationEdges[0].Operations[0].Value.(map[string]any)["nested"].([]any)[0] != "original" {
		t.Fatalf("returned plan mutation leaked into resolver: target=%q edge=%+v",
			second.TargetGenerationDef.Restore[0].Target, second.MigrationEdges[0])
	}
}

func reasonPointer(reason ResolutionReason) *ResolutionReason { return &reason }

func compatibilityTestModule(generations ...modules.GenerationDef) *modules.Module {
	return &modules.Module{
		ModuleSchemaVersion: 2,
		ID:                  "apps.example",
		Revision:            "restore-revision",
		Config: &modules.ConfigDef{Sets: []modules.ConfigSetDef{{
			ID:          "preferences",
			Generations: generations,
		}}},
	}
}

func compatibilityTestGeneration(id string, order int, versionRange, fingerprint string) modules.GenerationDef {
	return modules.GenerationDef{
		ID:          id,
		Order:       order,
		Matches:     []modules.VersionSelectorDef{{VersionRange: versionRange}},
		Fingerprint: fingerprint,
	}
}

func compatibilityTestMigration(from, to string) modules.MigrationEdgeDef {
	return modules.MigrationEdgeDef{
		From: from,
		To:   to,
		Operations: []modules.MigrationOperationDef{{
			Type:   "file-copy",
			Source: from + ".json",
			Target: to + ".json",
		}},
		Validate: []modules.ValidationDef{{Type: "file-exists", Path: to + ".json"}},
	}
}

func migrationEdgeIDs(edges []modules.MigrationEdgeDef) []string {
	if len(edges) == 0 {
		return nil
	}
	ids := make([]string, len(edges))
	for index, edge := range edges {
		ids[index] = edge.From + "->" + edge.To
	}
	return ids
}

func compatibilityTestSource(generation, fingerprint string) SourceCapture {
	return SourceCapture{
		CaptureID:                  "capture-a",
		ModuleID:                   "apps.example",
		ConfigSetID:                "preferences",
		Instance:                   SourceInstance{ID: "source-a"},
		Generation:                 generation,
		GenerationFingerprint:      fingerprint,
		ModuleRevision:             "capture-revision",
		CaptureModuleSchemaVersion: 2,
	}
}

func compatibilityTestTarget(rawVersion string) TargetInstance {
	evidence := modules.NewVersionEvidence(rawVersion)
	return TargetInstance{
		ID:                "target-a",
		ModuleID:          "apps.example",
		RawVersion:        evidence.Raw,
		NormalizedVersion: evidence.Normalized,
		Root:              `C:\Users\example\AppData\Roaming\Example`,
	}
}
