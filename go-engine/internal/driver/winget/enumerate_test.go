// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

func TestEnumerateInstalledReturnsNeutralExportLedgerWithListEvidence(t *testing.T) {
	origExport, origList := exportInstalledFn, listInstalledPackagesFn
	exportInstalledFn = func() ([]snapshot.SnapshotApp, error) {
		return []snapshot.SnapshotApp{{ID: "Git.Git"}, {ID: "Microsoft.VisualStudioCode"}}, nil
	}
	listInstalledPackagesFn = func() ([]snapshot.SnapshotApp, error) {
		return []snapshot.SnapshotApp{
			{ID: "Git.Git", Name: "Git", Version: "2.45"},
			{ID: "Microsoft.VisualStudioCode", Name: "Visual Studio Code", Version: "1.90"},
		}, nil
	}
	t.Cleanup(func() { exportInstalledFn, listInstalledPackagesFn = origExport, origList })

	got, err := New().EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled: %v", err)
	}
	want := []driver.InstalledPackage{
		{Ref: "Git.Git", DisplayName: "Git", Version: "2.45"},
		{Ref: "Microsoft.VisualStudioCode", DisplayName: "Visual Studio Code", Version: "1.90"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
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
