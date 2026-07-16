// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestStandaloneConfigRestoreEvidencePrefersRealizerAndBrewBeforeDriver(t *testing.T) {
	originalRealizer := newRealizerFn
	originalBrew := newBrewDriverFn
	originalDriver := newDriverFn
	t.Cleanup(func() {
		newRealizerFn = originalRealizer
		newBrewDriverFn = originalBrew
		newDriverFn = originalDriver
	})

	nixRef := "nixpkgs#nix-tool"
	brewRef := "brew-tool"
	nixBackend := &fakeRealizer{currentSet: realizer.Set{Elements: map[string]realizer.Element{
		leafAttr(nixRef): {Name: leafAttr(nixRef)},
	}}}
	brewBackend := &mockDriver{
		installed: map[string]bool{brewRef: true},
		versions:  map[string]string{brewRef: "2.0"},
	}
	driverCalls := 0
	newRealizerFn = func() (realizer.Realizer, error) { return nixBackend, nil }
	newBrewDriverFn = func() (driver.Driver, error) { return brewBackend, nil }
	newDriverFn = func() (driver.Driver, error) {
		driverCalls++
		return nil, errors.New("driver fallback must not be selected")
	}

	apps := []manifest.App{
		{ID: "nix-tool", Refs: map[string]string{runtime.GOOS: nixRef}},
		{ID: "brew-tool", Driver: "brew", Refs: map[string]string{runtime.GOOS: brewRef}},
	}
	requestedModules := map[string]*modules.Module{
		"apps.nix-tool":  {ID: "apps.nix-tool"},
		"apps.brew-tool": {ID: "apps.brew-tool"},
	}
	evidence, err := newStandaloneConfigRestoreEvidenceSource(apps).Snapshot(
		context.Background(),
		configRestoreDetectionRequest{Modules: requestedModules},
	)
	if err != nil {
		t.Fatal(err)
	}
	if driverCalls != 0 {
		t.Fatalf("driver fallback calls = %d, want 0", driverCalls)
	}
	if nixBackend.currentCalls != 1 {
		t.Fatalf("realizer Current calls = %d, want 1", nixBackend.currentCalls)
	}
	if got := evidence.PackagesByModule["apps.nix-tool"]; len(got) != 1 || got[0].Backend != "nix" {
		t.Fatalf("nix evidence = %+v", got)
	}
	if got := evidence.PackagesByModule["apps.brew-tool"]; len(got) != 1 || got[0].RawVersion != "2.0" {
		t.Fatalf("brew evidence = %+v", got)
	}
}

func TestRealizerEvidenceCurrentFailureDoesNotPoisonBrewOrPathOnlyModules(t *testing.T) {
	nixRef := "nixpkgs#broken-current"
	brewRef := "brew-still-detectable"
	nixBackend := &fakeRealizer{currentErr: errors.New("nix current failed")}
	brewBackend := &mockDriver{
		installed: map[string]bool{brewRef: true},
		versions:  map[string]string{brewRef: "3.1"},
	}
	source := newRealizerConfigRestoreEvidenceSource(nixBackend, brewBackend, []manifest.App{
		{ID: "nix-tool", Refs: map[string]string{runtime.GOOS: nixRef}},
		{ID: "brew-tool", Driver: "brew", Refs: map[string]string{runtime.GOOS: brewRef}},
	})

	evidence, err := source.Snapshot(context.Background(), configRestoreDetectionRequest{Modules: map[string]*modules.Module{
		"apps.nix-tool":  {ID: "apps.nix-tool"},
		"apps.brew-tool": {ID: "apps.brew-tool"},
		"apps.path-only": {ID: "apps.path-only"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := evidence.FailedModules["apps.nix-tool"]; !failed {
		t.Fatalf("nix-backed module not marked failed: %+v", evidence.FailedModules)
	}
	if _, failed := evidence.FailedModules["apps.brew-tool"]; failed {
		t.Fatalf("brew-backed module poisoned by nix failure: %+v", evidence.FailedModules)
	}
	if _, failed := evidence.FailedModules["apps.path-only"]; failed {
		t.Fatalf("path-only module poisoned by nix failure: %+v", evidence.FailedModules)
	}
	if got := evidence.PackagesByModule["apps.brew-tool"]; len(got) != 1 || got[0].RawVersion != "3.1" {
		t.Fatalf("brew evidence = %+v", got)
	}
	if evidence.Glob == nil {
		t.Fatal("path-only glob evidence was not preserved")
	}
}

func TestDriverConfigRestoreEvidenceReadsFreshInstalledVersionEveryPass(t *testing.T) {
	driver := &mockDriver{
		installed: map[string]bool{"Vendor.App": true},
		versions:  map[string]string{"Vendor.App": "1.0"},
	}
	module := &modules.Module{
		ID: "apps.example", ModuleSchemaVersion: 2,
		Matches: modules.MatchCriteria{Winget: []string{"Vendor.App"}},
		Config:  &modules.ConfigDef{InstanceDetectors: []modules.InstanceDetectorDef{{ID: "package", Type: "package"}}},
	}
	source := newDriverConfigRestoreEvidenceSource(driver, []manifest.App{{
		ID: "example", Refs: map[string]string{"windows": "Vendor.App"},
	}})
	request := configRestoreDetectionRequest{Modules: map[string]*modules.Module{"apps.example": module}}

	first, err := source.Snapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	driver.versions["Vendor.App"] = "2.0"
	second, err := source.Snapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.PackagesByModule["apps.example"][0].RawVersion; got != "1.0" {
		t.Fatalf("preview version = %q", got)
	}
	if got := second.PackagesByModule["apps.example"][0].RawVersion; got != "2.0" {
		t.Fatalf("final version = %q", got)
	}
}

type selectiveFailureDriver struct{}

func (selectiveFailureDriver) Name() string { return "selective" }
func (selectiveFailureDriver) Detect(ref string) (bool, string, error) {
	if ref == "Vendor.Broken" {
		return false, "", errors.New("backend query failed")
	}
	return true, ref, nil
}
func (selectiveFailureDriver) Install(string) (*driver.InstallResult, error) {
	return nil, errors.New("not used")
}

func TestDriverConfigRestoreEvidenceIsolatesDetectionFailureByModule(t *testing.T) {
	modulesByID := map[string]*modules.Module{
		"apps.broken": {ID: "apps.broken", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Broken"}}},
		"apps.safe":   {ID: "apps.safe", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Safe"}}},
	}
	source := newDriverConfigRestoreEvidenceSource(selectiveFailureDriver{}, []manifest.App{
		{ID: "broken", Refs: map[string]string{"windows": "Vendor.Broken"}},
		{ID: "safe", Refs: map[string]string{"windows": "Vendor.Safe"}},
	})
	evidence, err := source.Snapshot(context.Background(), configRestoreDetectionRequest{Modules: modulesByID})
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := evidence.FailedModules["apps.broken"]; !failed {
		t.Fatalf("broken module was not isolated: %+v", evidence.FailedModules)
	}
	if _, failed := evidence.FailedModules["apps.safe"]; failed || len(evidence.PackagesByModule["apps.safe"]) != 1 {
		t.Fatalf("safe module evidence = %+v failed=%v", evidence.PackagesByModule["apps.safe"], failed)
	}
}
