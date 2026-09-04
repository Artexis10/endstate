// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package linuxparity

import (
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

func TestBuildClassifiesEveryWindowsModuleFromIndependentEvidence(t *testing.T) {
	moduleCatalog := map[string]*modules.Module{
		"apps.alacritty": {ID: "apps.alacritty", DisplayName: "Alacritty"},
		"apps.git":       {ID: "apps.git", DisplayName: "Git"},
		"apps.native": {
			ID: "apps.native", DisplayName: "Native",
			ModuleSchemaVersion: 3,
			Platforms: map[string]modules.PlatformVariant{
				"windows": parityVariant(true),
				"linux":   parityVariant(true),
			},
		},
		"apps.capture-only": {
			ID: "apps.capture-only", DisplayName: "Capture only",
			ModuleSchemaVersion: 3,
			Platforms: map[string]modules.PlatformVariant{
				"windows": parityVariant(true),
				"linux":   parityVariant(false),
			},
		},
		"apps.package-only": {ID: "apps.package-only", DisplayName: "Package only"},
		"apps.unknown":      {ID: "apps.unknown", DisplayName: "Unknown"},
	}
	packages := []*packagecatalog.Entry{{ID: "package-only", ConfigModule: "apps.package-only"}}
	registry := &hmregistry.Registry{
		Programs: []string{"alacritty", "git"},
		Entries: []hmregistry.Entry{
			{ID: "alacritty", Program: "alacritty", ModuleID: "apps.alacritty", Disposition: hmregistry.FileRoundTrip},
			{ID: "git", Program: "git", ModuleID: "apps.git", Disposition: hmregistry.Excluded, ExclusionReason: "semantic codec required"},
		},
	}

	matrix, err := Build(moduleCatalog, packages, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix.Rows) != len(moduleCatalog) {
		t.Fatalf("rows = %d, want %d", len(matrix.Rows), len(moduleCatalog))
	}
	want := map[string]Status{
		"apps.alacritty":    StatusSupported,
		"apps.capture-only": StatusAdapterRequired,
		"apps.git":          StatusAdapterRequired,
		"apps.native":       StatusSupported,
		"apps.package-only": StatusSupported,
		"apps.unknown":      StatusReviewRequired,
	}
	for _, row := range matrix.Rows {
		if row.Status != want[row.ModuleID] {
			t.Errorf("%s status = %s, want %s", row.ModuleID, row.Status, want[row.ModuleID])
		}
		delete(want, row.ModuleID)
	}
	if len(want) != 0 {
		t.Fatalf("missing rows = %+v", want)
	}
	if matrix.Counts.Supported != 3 || matrix.Counts.AdapterRequired != 2 || matrix.Counts.ReviewRequired != 1 || matrix.Ready {
		t.Fatalf("matrix summary = %+v ready=%v", matrix.Counts, matrix.Ready)
	}
	if err := matrix.RequireReady(); err == nil || !strings.Contains(err.Error(), "apps.git") || !strings.Contains(err.Error(), "apps.unknown") {
		t.Fatalf("readiness error = %v", err)
	}
}

func parityVariant(withRestore bool) modules.PlatformVariant {
	variant := modules.PlatformVariant{
		Realization: "endstate-restore",
		Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
			Source: "${home}/config", Dest: "apps/example/config",
		}}},
	}
	if withRestore {
		variant.Restore = []modules.RestoreDef{{
			Type: "copy", Source: "./payload/apps/example/config", Target: "${home}/config", Backup: true,
		}}
	}
	return variant
}

func TestBuildUsesFullHomeManagerIndexToExposeUnreviewedCounterpart(t *testing.T) {
	matrix, err := Build(
		map[string]*modules.Module{"apps.vscode": {ID: "apps.vscode", DisplayName: "VS Code"}},
		nil,
		&hmregistry.Registry{Programs: []string{"vscode"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	row := matrix.Rows[0]
	if row.Status != StatusAdapterRequired || !contains(row.Evidence, EvidenceHomeManagerProgram) {
		t.Fatalf("row = %+v", row)
	}
}

func TestBuildRejectsDuplicateLinuxAuthorities(t *testing.T) {
	_, err := Build(
		map[string]*modules.Module{"apps.same": {ID: "apps.same"}},
		nil,
		&hmregistry.Registry{Programs: []string{"one", "two"}, Entries: []hmregistry.Entry{
			{ID: "one", Program: "one", ModuleID: "apps.same", Disposition: hmregistry.FileRoundTrip},
			{ID: "two", Program: "two", ModuleID: "apps.same", Disposition: hmregistry.FileRoundTrip},
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "multiple Home Manager adapters") {
		t.Fatalf("duplicate authority error = %v", err)
	}
}

func TestCaptureDoesWorkIncludesValueLevelSettings(t *testing.T) {
	capture := &modules.CaptureDef{RegistryValues: []modules.CaptureRegistryValue{{
		Key: "HKCU:\\Software\\Example", ValueName: "Theme",
	}}}
	if !captureDoesWork(capture) {
		t.Fatal("value-level Windows settings were treated as an empty capture surface")
	}
}

func contains(values []Evidence, want Evidence) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
