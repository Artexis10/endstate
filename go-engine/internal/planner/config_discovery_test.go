// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestCompatibilityResolverPinsDiscoveryDeclarationsAndReturnsDefensiveCopies(t *testing.T) {
	module := compatibilityTestModule(compatibilityTestGeneration("g1", 1, ">=1 <2", "fingerprint-g1"))
	module.Matches.Exe = []string{"zeta.exe", "alpha.exe", "alpha.exe"}
	module.Config.InstanceDetectors = []modules.InstanceDetectorDef{{
		ID: "installed", Type: "path", Glob: "original/*", VersionPattern: `^(?P<version>\d+)$`,
	}}
	resolver := NewCompatibilityResolver(map[string]*modules.Module{module.ID: module}, nil)

	module.Matches.Exe[0] = "mutated.exe"
	module.Config.InstanceDetectors[0].Glob = "mutated/*"
	patterns := resolver.ProcessPatterns(module.ID)
	if !reflect.DeepEqual(patterns, []string{"alpha.exe", "zeta.exe"}) {
		t.Fatalf("process patterns = %v", patterns)
	}
	patterns[0] = "caller-mutated.exe"
	if got := resolver.ProcessPatterns(module.ID); !reflect.DeepEqual(got, []string{"alpha.exe", "zeta.exe"}) {
		t.Fatalf("returned process patterns alias resolver state: %v", got)
	}

	seenPattern := ""
	targets, err := resolver.DiscoverTargets(module.ID, nil, modules.DiscoveryOptions{
		Glob: func(pattern string) ([]string, error) {
			seenPattern = pattern
			return []string{t.TempDir() + "/17"}, nil
		},
	})
	if err != nil {
		t.Fatalf("DiscoverTargets: %v", err)
	}
	if seenPattern != "original/*" {
		t.Fatalf("detector glob = %q, want pinned declaration", seenPattern)
	}
	if len(targets) != 1 || targets[0].RawVersion != "17" || targets[0].NormalizedVersion != "17" ||
		targets[0].Evidence.Type != "path" || targets[0].Evidence.AppID != "" || targets[0].Root == "" {
		t.Fatalf("portable target conversion = %+v", targets)
	}
	targets[0].RawVersion = "caller-mutated"
	again, err := resolver.DiscoverTargets(module.ID, nil, modules.DiscoveryOptions{
		Glob: func(string) ([]string, error) { return []string{t.TempDir() + "/18"}, nil },
	})
	if err != nil || len(again) != 1 || again[0].RawVersion != "18" {
		t.Fatalf("returned targets alias resolver state: targets=%+v err=%v", again, err)
	}
}

func TestCompatibilityResolverDiscoverTargetsUsesOnlySuppliedPackageEvidence(t *testing.T) {
	module := compatibilityTestModule(compatibilityTestGeneration("g1", 1, ">=1 <3", "fingerprint-g1"))
	module.Config.InstanceDetectors = []modules.InstanceDetectorDef{{ID: "installed", Type: "package"}}
	resolver := NewCompatibilityResolver(map[string]*modules.Module{module.ID: module}, nil)

	targets, err := resolver.DiscoverTargets(module.ID, []modules.PackageEvidence{
		{AppID: module.ID, Backend: "winget", Ref: "Vendor.App.2", RawVersion: "2.5"},
		{AppID: module.ID, Backend: "winget", Ref: "Vendor.App.1", RawVersion: "1.5"},
	}, modules.DiscoveryOptions{})
	if err != nil {
		t.Fatalf("DiscoverTargets: %v", err)
	}
	if len(targets) != 2 || targets[0].ID == targets[1].ID {
		t.Fatalf("side-by-side package targets = %+v", targets)
	}
	for _, target := range targets {
		if target.ModuleID != module.ID || target.DetectorID != "installed" || target.Evidence.Type != "package" ||
			target.Evidence.Backend != "winget" || target.Evidence.Ref == "" || target.Root != "" {
			t.Fatalf("portable target = %+v", target)
		}
	}

	absent, err := resolver.DiscoverTargets(module.ID, nil, modules.DiscoveryOptions{})
	if err != nil || len(absent) != 0 {
		t.Fatalf("absent package evidence = %+v err=%v", absent, err)
	}
}
