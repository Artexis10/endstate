// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

type sourcePlanDriver struct{ calls []string }

func (d *sourcePlanDriver) Name() string                                  { return "winget" }
func (d *sourcePlanDriver) Detect(string) (bool, string, error)           { return false, "", nil }
func (d *sourcePlanDriver) Install(string) (*driver.InstallResult, error) { return nil, nil }
func (d *sourcePlanDriver) DetectBatchSource(refs []string, source string) (map[string]driver.DetectResult, error) {
	d.calls = append(d.calls, source)
	result := map[string]driver.DetectResult{}
	for _, ref := range refs {
		result[ref] = driver.DetectResult{Installed: source == "winget"}
	}
	return result, nil
}

type nonWingetPlanDriver struct{ name string }

func (d nonWingetPlanDriver) Name() string                                  { return d.name }
func (d nonWingetPlanDriver) Detect(string) (bool, string, error)           { return false, "", nil }
func (d nonWingetPlanDriver) Install(string) (*driver.InstallResult, error) { return nil, nil }

func TestComputePlan_PartitionsEqualWingetRefsBySource(t *testing.T) {
	d := &sourcePlanDriver{}
	m := &manifest.Manifest{Apps: []manifest.App{
		{ID: "community", Driver: "winget", Source: "winget", Refs: map[string]string{"windows": "Same.Ref"}},
		{ID: "store", Driver: "winget", Source: "msstore", Refs: map[string]string{"windows": "Same.Ref"}},
	}}
	plan, err := ComputePlan(m, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 2 || plan.Actions[0].CurrentStatus != "present" || plan.Actions[1].CurrentStatus != "missing" {
		t.Fatalf("actions = %+v", plan.Actions)
	}
	if len(d.calls) != 2 || d.calls[0] != "winget" || d.calls[1] != "msstore" {
		t.Fatalf("source batches = %v", d.calls)
	}
}

func TestComputePlan_NonWingetDriversKeepSourceEmpty(t *testing.T) {
	for _, driverName := range []string{"brew", "nix", "chocolatey"} {
		t.Run(driverName, func(t *testing.T) {
			m := &manifest.Manifest{Apps: []manifest.App{{
				ID: driverName + "-app", Driver: driverName, Source: "msstore",
				Refs: map[string]string{"windows": "vendor.app"},
			}}}
			plan, err := ComputePlan(m, nonWingetPlanDriver{name: driverName})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Actions) != 1 || plan.Actions[0].Source != "" {
				t.Fatalf("%s actions = %+v, want empty source", driverName, plan.Actions)
			}
		})
	}
}
