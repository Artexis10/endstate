// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"reflect"
	"testing"
)

func TestDiscoverInstances_ChocolateyRefsDeduplicateCaseInsensitively(t *testing.T) {
	mod := packageDetectorModule(InstanceDetectorDef{ID: "installed", Type: "package"})
	packages := []PackageEvidence{
		{AppID: "upper", Backend: "Chocolatey", Platform: "windows", Ref: "Git.Install", Driver: "chocolatey", RawVersion: "2.47.1"},
		{AppID: "lower", Backend: "CHOCOLATEY", Platform: "windows", Ref: "git.install", Driver: "chocolatey", RawVersion: "2.47.1"},
	}

	instances, err := DiscoverInstances(mod, packages, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("case variants produced %d Chocolatey instances: %+v", len(instances), instances)
	}
	if instances[0].CanonicalLocator != "package:chocolatey:git.install" {
		t.Errorf("canonical locator = %q", instances[0].CanonicalLocator)
	}
	reversed, err := DiscoverInstances(mod, []PackageEvidence{packages[1], packages[0]}, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(instances, reversed) {
		t.Errorf("Chocolatey evidence changed with input order:\nforward=%+v\nreverse=%+v", instances, reversed)
	}
}
