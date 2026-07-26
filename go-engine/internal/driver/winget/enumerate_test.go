// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

func TestEnumerateInstalledUsesDetailsWithoutLegacyTable(t *testing.T) {
	origExport, origList, origDetails := exportInstalledFn, listInstalledPackagesFn, detailsInstalledPackagesFn
	exportInstalledFn = func() ([]snapshot.SnapshotApp, error) {
		return []snapshot.SnapshotApp{{ID: "Git.Git"}, {ID: "Microsoft.VisualStudioCode"}}, nil
	}
	listInstalledPackagesFn = func() ([]snapshot.SnapshotApp, error) {
		t.Fatal("legacy list should not run when details covers exported packages")
		return nil, nil
	}
	detailsInstalledPackagesFn = func(string) (map[string]snapshot.SnapshotApp, error) {
		return map[string]snapshot.SnapshotApp{
			"git.git":                    {ID: "Git.Git", Name: "Git", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\Git`}, InventoryRelationshipKnown: true},
			"microsoft.visualstudiocode": {ID: "Microsoft.VisualStudioCode", Name: "Visual Studio Code", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\VSCode`, `ARP\Machine\X86\VSCode`}, InventoryRelationshipKnown: true},
		}, nil
	}
	t.Cleanup(func() {
		exportInstalledFn, listInstalledPackagesFn, detailsInstalledPackagesFn = origExport, origList, origDetails
	})

	got, err := New().EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled: %v", err)
	}
	want := []driver.InstalledPackage{
		{Ref: "Git.Git", DisplayName: "Git", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\Git`}, InventoryRelationshipKnown: true},
		{Ref: "Microsoft.VisualStudioCode", DisplayName: "Visual Studio Code", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\VSCode`, `ARP\Machine\X86\VSCode`}, InventoryRelationshipKnown: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("package %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestEnumerateInstalledPropagatesExportFailure(t *testing.T) {
	orig := exportInstalledFn
	want := errors.New("winget missing")
	exportInstalledFn = func() ([]snapshot.SnapshotApp, error) { return nil, want }
	t.Cleanup(func() { exportInstalledFn = orig })
	_, err := New().EnumerateInstalled()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
