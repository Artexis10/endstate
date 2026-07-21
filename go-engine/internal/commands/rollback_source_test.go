// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

type sourceRollbackDriver struct{ sources []string }

func (d *sourceRollbackDriver) Name() string                                  { return "winget" }
func (d *sourceRollbackDriver) Detect(string) (bool, string, error)           { return false, "", nil }
func (d *sourceRollbackDriver) Install(string) (*driver.InstallResult, error) { return nil, nil }
func (d *sourceRollbackDriver) Uninstall(string) (*driver.UninstallResult, error) {
	panic("unscoped uninstall used")
}
func (d *sourceRollbackDriver) UninstallSource(ref, source string) (*driver.UninstallResult, error) {
	d.sources = append(d.sources, source)
	return &driver.UninstallResult{Status: driver.StatusUninstalled}, nil
}

func TestRunDriverRollback_PreservesSourceAwareHistory(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{
		RunID: "apply-store", Backend: "winget", AddedRefs: []string{"9NBLGGH4NNS1"},
		AddedPackages: []provision.PackageRecord{{Ref: "9NBLGGH4NNS1", Source: "msstore"}},
	}); err != nil {
		t.Fatal(err)
	}
	d := &sourceRollbackDriver{}
	raw, eerr := runDriverRollback(RollbackFlags{Confirm: true}, d, d)
	if eerr != nil {
		t.Fatalf("rollback: %+v", eerr)
	}
	if len(d.sources) != 1 || d.sources[0] != "msstore" {
		t.Fatalf("sources = %v", d.sources)
	}
	if raw.(*RollbackResult).RemovedRefs[0] != "9NBLGGH4NNS1" {
		t.Fatalf("result = %+v", raw)
	}
	gens, err := provision.List()
	if err != nil || len(gens) != 2 {
		t.Fatalf("generations=%+v err=%v", gens, err)
	}
	if len(gens[0].RemovedPackages) != 1 || gens[0].RemovedPackages[0].Source != "msstore" {
		t.Fatalf("removedPackages = %+v", gens[0].RemovedPackages)
	}
}

func TestGroupRollbackGenerations_LegacyStoreRefUsesClassifier(t *testing.T) {
	groups := groupRollbackGenerations([]*provision.Generation{{Backend: "winget", AddedRefs: []string{"9NBLGGH4NNS1", "Vendor.App"}}})
	if len(groups) != 1 || len(groups[0].packages) != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].packages[0].Source != "msstore" || groups[0].packages[1].Source != "winget" {
		t.Fatalf("packages = %+v", groups[0].packages)
	}
}
