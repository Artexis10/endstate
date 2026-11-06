// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package linuxparity

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

func TestBuildClassifiesEveryWindowsModuleFromIndependentEvidence(t *testing.T) {
	moduleCatalog := map[string]*modules.Module{
		"apps.alacritty": {Tier: modules.AppsTier, ID: "apps.alacritty", DisplayName: "Alacritty"},
		"apps.git":       {Tier: modules.AppsTier, ID: "apps.git", DisplayName: "Git"},
		"apps.native": {
			Tier: modules.AppsTier, ID: "apps.native", DisplayName: "Native",
			ModuleSchemaVersion: 3,
			Platforms: map[string]modules.PlatformVariant{
				"windows": parityVariant(true),
				"linux":   parityVariant(true),
			},
		},
		"apps.capture-only": {
			Tier: modules.AppsTier, ID: "apps.capture-only", DisplayName: "Capture only",
			ModuleSchemaVersion: 3,
			Platforms: map[string]modules.PlatformVariant{
				"windows": parityVariant(true),
				"linux":   parityVariant(false),
			},
		},
		"apps.package-only": {Tier: modules.AppsTier, ID: "apps.package-only", DisplayName: "Package only"},
		"apps.unknown":      {Tier: modules.AppsTier, ID: "apps.unknown", DisplayName: "Unknown"},
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
		"apps.alacritty":    StatusAdapterRequired,
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
	if matrix.Counts.Supported != 2 || matrix.Counts.AdapterRequired != 3 || matrix.Counts.ReviewRequired != 1 || matrix.Ready {
		t.Fatalf("matrix summary = %+v ready=%v", matrix.Counts, matrix.Ready)
	}
	if err := matrix.RequireReady(); err == nil || !strings.Contains(err.Error(), "apps.git") || !strings.Contains(err.Error(), "apps.unknown") {
		t.Fatalf("readiness error = %v", err)
	}
}

func TestBuildKeepsDesktopSettingsOutsideApplicationParity(t *testing.T) {
	matrix, err := Build(map[string]*modules.Module{
		"apps.editor":            {ID: "apps.editor", Tier: modules.AppsTier},
		"windows-settings.theme": {ID: "windows-settings.theme", Tier: "windows-settings"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Counts.Total != 1 || len(matrix.Rows) != 1 || matrix.Rows[0].ModuleID != "apps.editor" {
		t.Fatalf("application parity includes non-application settings: %+v", matrix)
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
		map[string]*modules.Module{"apps.vscode": {Tier: modules.AppsTier, ID: "apps.vscode", DisplayName: "VS Code"}},
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

func TestBuildKeepsConflictingAdaptersFromAbortingOtherRows(t *testing.T) {
	matrix, err := Build(
		map[string]*modules.Module{"apps.same": {Tier: modules.AppsTier, ID: "apps.same"}, "apps.independent": {Tier: modules.AppsTier, ID: "apps.independent"}},
		nil,
		&hmregistry.Registry{Programs: []string{"one", "two"}, Entries: []hmregistry.Entry{
			{ID: "one", Program: "one", ModuleID: "apps.same", Disposition: hmregistry.FileRoundTrip},
			{ID: "two", Program: "two", ModuleID: "apps.same", Disposition: hmregistry.FileRoundTrip},
		}},
	)
	if err != nil || len(matrix.Rows) != 2 || matrix.Ready {
		t.Fatalf("conflicting adapter discarded independent evidence: matrix=%+v error=%v", matrix, err)
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

func TestBuildDoesNotLetRegistryAuthorizeLegacyWindowsSettings(t *testing.T) {
	matrix, err := Build(map[string]*modules.Module{"apps.example": {ID: "apps.example", Tier: modules.AppsTier, Restore: []modules.RestoreDef{{Type: "copy", Source: "./payload/apps/example/config", Target: "%APPDATA%/Example/config", Backup: true}}}}, nil, &hmregistry.Registry{Programs: []string{"example"}, Entries: []hmregistry.Entry{{ID: "example", Program: "example", ModuleID: "apps.example", Disposition: hmregistry.FileRoundTrip}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix.Rows) != 1 || matrix.Rows[0].Status != StatusAdapterRequired || matrix.Ready {
		t.Fatalf("legacy Windows settings became supported: %+v", matrix)
	}
}

func TestBuildDoesNotCountStructuralBindingAsExecutableSupport(t *testing.T) {
	registry, err := hmregistry.Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var entry hmregistry.Entry
	for _, candidate := range registry.Entries {
		if candidate.ID == "ripgrep" {
			entry = candidate
		}
	}
	pkg, err := packagecatalog.ParseEntry([]byte(`{"schemaVersion":1,"id":"ripgrep","displayName":"ripgrep","nix":{"attribute":"ripgrep"},"aliases":{"executable":["rg"]},"configModule":"apps.ripgrep"}`), "ripgrep.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	variant := modules.PlatformVariant{Realization: "endstate-restore", Matches: modules.MatchCriteria{}, HomeManagerAdapter: &modules.HomeManagerAdapterBinding{ID: entry.ID, ReviewFingerprint: entry.ReviewFingerprint, ConfigSetID: "default", GenerationID: "g1"}, Capture: &modules.CaptureDef{}}
	for index, target := range entry.Targets {
		dest := fmt.Sprintf("apps/ripgrep/settings-%d", index)
		variant.Capture.Files = append(variant.Capture.Files, modules.CaptureFile{Source: target.Coordinate, Dest: dest, Optional: target.Optional})
		variant.Restore = append(variant.Restore, modules.RestoreDef{Type: "copy", Source: "./payload/" + dest, Target: target.Coordinate, Optional: target.Optional, Backup: true})
	}
	body, err := json.Marshal(modules.Module{ModuleSchemaVersion: 3, ID: entry.ModuleID, DisplayName: "ripgrep", Sensitivity: "low", Platforms: map[string]modules.PlatformVariant{"linux": variant}})
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseAndValidateModuleJSON(body, "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	mod.Tier = modules.AppsTier
	catalog := map[string]*modules.Module{mod.ID: mod}
	authorities := hmregistry.Join(&hmregistry.Registry{Entries: []hmregistry.Entry{entry}}, catalog, []*packagecatalog.Entry{pkg})
	if len(authorities) != 1 || !authorities[0].Bound {
		t.Fatalf("fixture lacks coherent structural binding: %+v", authorities)
	}
	matrix, err := Build(catalog, []*packagecatalog.Entry{pkg}, &hmregistry.Registry{Entries: []hmregistry.Entry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Ready || matrix.Counts.Total != 1 || matrix.Rows[0].Status != StatusAdapterRequired || matrix.Rows[0].Reason != "settings_content_policy_unavailable" {
		t.Fatalf("structural evidence became support: %+v", matrix)
	}
}
