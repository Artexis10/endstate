// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// ---------------------------------------------------------------------------
// parseOnlyIDs unit tests
// ---------------------------------------------------------------------------

func TestParseOnlyIDs_Empty_ReturnsNil(t *testing.T) {
	got := parseOnlyIDs("")
	if got != nil {
		t.Errorf("parseOnlyIDs(\"\") = %v, want nil", got)
	}
}

func TestParseOnlyIDs_Single(t *testing.T) {
	got := parseOnlyIDs("git")
	if len(got) != 1 || got[0] != "git" {
		t.Errorf("parseOnlyIDs(\"git\") = %v, want [git]", got)
	}
}

func TestParseOnlyIDs_Multiple(t *testing.T) {
	got := parseOnlyIDs("git,vscode")
	if len(got) != 2 || got[0] != "git" || got[1] != "vscode" {
		t.Errorf("parseOnlyIDs(\"git,vscode\") = %v, want [git vscode]", got)
	}
}

func TestParseOnlyIDs_TrimsWhitespace(t *testing.T) {
	got := parseOnlyIDs(" git , vscode ")
	if len(got) != 2 || got[0] != "git" || got[1] != "vscode" {
		t.Errorf("parseOnlyIDs with spaces = %v, want [git vscode]", got)
	}
}

func TestParseOnlyIDs_DropsEmpties(t *testing.T) {
	got := parseOnlyIDs(",git,,vscode,")
	if len(got) != 2 || got[0] != "git" || got[1] != "vscode" {
		t.Errorf("parseOnlyIDs with empties = %v, want [git vscode]", got)
	}
}

func TestParseOnlyIDs_Deduplicates(t *testing.T) {
	got := parseOnlyIDs("git,git,vscode")
	if len(got) != 2 {
		t.Errorf("parseOnlyIDs dedup: got %v (len %d), want 2 unique ids", got, len(got))
	}
}

// ---------------------------------------------------------------------------
// filterAppsByOnly unit tests
// ---------------------------------------------------------------------------

func appsFixture() []manifest.App {
	return []manifest.App{
		{ID: "git", Refs: map[string]string{"windows": "Git.Git"}},
		{ID: "vscode", Refs: map[string]string{"windows": "Microsoft.VisualStudioCode"}},
		{ID: "7zip", Refs: map[string]string{"windows": "7zip.7zip"}},
	}
}

func TestFilterAppsByOnly_SubsetFiltered(t *testing.T) {
	apps := appsFixture()
	filtered, unknown := filterAppsByOnly(apps, []string{"git", "vscode"})
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "git" || filtered[1].ID != "vscode" {
		t.Errorf("filtered ids = [%s %s], want [git vscode]", filtered[0].ID, filtered[1].ID)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown = %v, want empty", unknown)
	}
}

func TestFilterAppsByOnly_UnknownIDReported(t *testing.T) {
	apps := appsFixture()
	filtered, unknown := filterAppsByOnly(apps, []string{"git", "not-a-real-id"})
	if len(filtered) != 1 || filtered[0].ID != "git" {
		t.Errorf("filtered = %v, want [git]", filtered)
	}
	if len(unknown) != 1 || unknown[0] != "not-a-real-id" {
		t.Errorf("unknown = %v, want [not-a-real-id]", unknown)
	}
}

func TestFilterAppsByOnly_AllUnknown(t *testing.T) {
	apps := appsFixture()
	filtered, unknown := filterAppsByOnly(apps, []string{"no-such-app"})
	if len(filtered) != 0 {
		t.Errorf("filtered = %v, want empty", filtered)
	}
	if len(unknown) != 1 || unknown[0] != "no-such-app" {
		t.Errorf("unknown = %v, want [no-such-app]", unknown)
	}
}

// ---------------------------------------------------------------------------
// validateOnly unit tests
// ---------------------------------------------------------------------------

func makeManifestWith(ids ...string) *manifest.Manifest {
	apps := make([]manifest.App, len(ids))
	for i, id := range ids {
		apps[i] = manifest.App{ID: id, Refs: map[string]string{"windows": "Vendor." + id}}
	}
	return &manifest.Manifest{Apps: apps}
}

func TestValidateOnly_Disabled_ReturnsNil(t *testing.T) {
	mf := makeManifestWith("git", "vscode")
	ids, err := validateOnly(ApplyFlags{Only: ""}, mf)
	if err != nil {
		t.Fatalf("validateOnly disabled: got error %v, want nil", err)
	}
	if ids != nil {
		t.Errorf("validateOnly disabled: got ids %v, want nil", ids)
	}
}

func TestValidateOnly_BlankValue_Rejected(t *testing.T) {
	mf := makeManifestWith("git")
	_, err := validateOnly(ApplyFlags{Only: "  ,  , "}, mf)
	if err == nil {
		t.Fatal("validateOnly blank value: want error, got nil")
	}
	if err.Code != envelope.ErrManifestValidationError {
		t.Errorf("error code = %q, want MANIFEST_VALIDATION_ERROR", err.Code)
	}
}

func TestValidateOnly_UnknownID_NamesIt(t *testing.T) {
	mf := makeManifestWith("git", "vscode")
	_, err := validateOnly(ApplyFlags{Only: "git,not-a-real-id"}, mf)
	if err == nil {
		t.Fatal("validateOnly unknown id: want error, got nil")
	}
	if err.Code != envelope.ErrManifestValidationError {
		t.Errorf("error code = %q, want MANIFEST_VALIDATION_ERROR", err.Code)
	}
	// The error message must name the unknown id.
	if err.Message == "" {
		t.Error("error message is empty")
	}
	// Check "not-a-real-id" appears in the message.
	found := false
	for _, r := range []string{err.Message, err.Remediation} {
		if len(r) > 0 && containsStr(r, "not-a-real-id") {
			found = true
		}
	}
	if !containsStr(err.Message, "not-a-real-id") {
		t.Errorf("error message %q does not name the unknown id %q; found in any field = %v", err.Message, "not-a-real-id", found)
	}
}

func TestValidateOnly_OnlyPrune_Rejected(t *testing.T) {
	mf := makeManifestWith("git", "vscode")
	_, err := validateOnly(ApplyFlags{Only: "git", Prune: true}, mf)
	if err == nil {
		t.Fatal("validateOnly only+prune: want error, got nil")
	}
	if err.Code != envelope.ErrManifestValidationError {
		t.Errorf("error code = %q, want MANIFEST_VALIDATION_ERROR", err.Code)
	}
}

func TestValidateOnly_ValidSubset_ReturnsIDs(t *testing.T) {
	mf := makeManifestWith("git", "vscode", "7zip")
	ids, err := validateOnly(ApplyFlags{Only: "git,vscode"}, mf)
	if err != nil {
		t.Fatalf("validateOnly valid: got error %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("ids = %v, want [git vscode]", ids)
	}
}

// containsStr is a simple substring check used in test assertions.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// RunApply --only integration tests (driver path, hermetic)
// ---------------------------------------------------------------------------

// threeAppsManifest writes an inline manifest with three apps to a temp file
// and returns the path.
func threeAppsManifest(t *testing.T) string {
	t.Helper()
	content := `{
		"name": "subset-test",
		"apps": [
			{ "id": "git",   "refs": { "windows": "Git.Git" } },
			{ "id": "vscode","refs": { "windows": "Microsoft.VisualStudioCode" } },
			{ "id": "7zip",  "refs": { "windows": "7zip.7zip" } }
		]
	}`
	path := filepath.Join(t.TempDir(), "m.jsonc")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunApply_Only_SubsetFiltersActions verifies that --only limits the plan
// and summary to the selected apps, and other apps are not in the actions list.
func TestRunApply_Only_SubsetFiltersActions(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}
	path := threeAppsManifest(t)

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: path,
			DryRun:   true,
			Only:     "git,vscode",
		})
		if err != nil {
			t.Fatalf("RunApply --only: unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if result.Summary.Total != 2 {
		t.Errorf("summary.total = %d, want 2 (only git and vscode)", result.Summary.Total)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("len(actions) = %d, want 2", len(result.Actions))
	}
	ids := map[string]bool{}
	for _, a := range result.Actions {
		ids[a.ID] = true
	}
	if !ids["git"] {
		t.Error("expected git in actions")
	}
	if !ids["vscode"] {
		t.Error("expected vscode in actions")
	}
	if ids["7zip"] {
		t.Error("7zip must NOT be in actions when not selected by --only")
	}
}

// TestRunApply_Only_DryRun verifies that --only --dry-run reflects only the
// selected subset and sets dryRun=true.
func TestRunApply_Only_DryRun(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{"Git.Git": true}}
	path := threeAppsManifest(t)

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: path,
			DryRun:   true,
			Only:     "git",
		})
		if err != nil {
			t.Fatalf("RunApply --only --dry-run: unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if !result.DryRun {
		t.Error("dryRun = false, want true")
	}
	if result.Summary.Total != 1 {
		t.Errorf("summary.total = %d, want 1", result.Summary.Total)
	}
	if len(result.Actions) != 1 || result.Actions[0].ID != "git" {
		t.Errorf("actions = %v, want [git]", result.Actions)
	}
}

// TestRunApply_Only_SummaryCountsSubset verifies that after a real (non-dry-run)
// apply with --only, summary counts reflect the subset only.
func TestRunApply_Only_SummaryCountsSubset(t *testing.T) {
	// vscode is already installed; git is missing → will be installed.
	md := &mockDriver{installed: map[string]bool{"Microsoft.VisualStudioCode": true}}
	path := threeAppsManifest(t)

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: path,
			Only:     "vscode,git",
		})
		if err != nil {
			t.Fatalf("RunApply --only subset: unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	// Total should be 2 (vscode + git), not 3.
	if result.Summary.Total != 2 {
		t.Errorf("summary.total = %d, want 2", result.Summary.Total)
	}
	// git installed → success=1, vscode already present → skipped=1.
	if result.Summary.Success != 1 {
		t.Errorf("summary.success = %d, want 1 (git installed)", result.Summary.Success)
	}
	if result.Summary.Skipped != 1 {
		t.Errorf("summary.skipped = %d, want 1 (vscode already present)", result.Summary.Skipped)
	}
	if result.Summary.Failed != 0 {
		t.Errorf("summary.failed = %d, want 0", result.Summary.Failed)
	}
}

// TestRunApply_Only_UnknownID_FailsWithNamedError verifies that an unknown id
// in --only fails with MANIFEST_VALIDATION_ERROR naming the unknown id, before
// any installation.
func TestRunApply_Only_UnknownID_FailsWithNamedError(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}
	path := threeAppsManifest(t)

	var eerr *envelope.Error
	withMockDriver(md, func() {
		_, eerr = RunApply(ApplyFlags{
			Manifest: path,
			Only:     "git,not-a-real-id",
		})
	})

	if eerr == nil {
		t.Fatal("expected error for unknown --only id, got nil")
	}
	if eerr.Code != envelope.ErrManifestValidationError {
		t.Errorf("error code = %q, want MANIFEST_VALIDATION_ERROR", eerr.Code)
	}
	if !containsStr(eerr.Message, "not-a-real-id") {
		t.Errorf("error message %q does not name the unknown id", eerr.Message)
	}
	// Nothing should have been installed.
	if md.installCalls != 0 {
		t.Errorf("Install was called %d times; should be 0 (pre-execution rejection)", md.installCalls)
	}
}

// TestRunApply_Only_EmptyValue_Rejected verifies that --only "" fails with
// MANIFEST_VALIDATION_ERROR.
func TestRunApply_Only_EmptyValue_Rejected(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}
	path := threeAppsManifest(t)

	var eerr *envelope.Error
	withMockDriver(md, func() {
		_, eerr = RunApply(ApplyFlags{
			Manifest: path,
			Only:     "  ,  ", // normalises to empty
		})
	})

	if eerr == nil {
		t.Fatal("expected error for empty --only, got nil")
	}
	if eerr.Code != envelope.ErrManifestValidationError {
		t.Errorf("error code = %q, want MANIFEST_VALIDATION_ERROR", eerr.Code)
	}
}

// TestRunApply_Only_WithPrune_Rejected verifies that --only + --prune fails
// with MANIFEST_VALIDATION_ERROR before any execution.
func TestRunApply_Only_WithPrune_Rejected(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}
	path := threeAppsManifest(t)

	var eerr *envelope.Error
	withMockDriver(md, func() {
		_, eerr = RunApply(ApplyFlags{
			Manifest: path,
			Only:     "git",
			Prune:    true,
		})
	})

	if eerr == nil {
		t.Fatal("expected error for --only + --prune, got nil")
	}
	if eerr.Code != envelope.ErrManifestValidationError {
		t.Errorf("error code = %q, want MANIFEST_VALIDATION_ERROR", eerr.Code)
	}
	if md.installCalls != 0 {
		t.Errorf("Install called %d times; should be 0 (rejected before execution)", md.installCalls)
	}
}

// TestRunApply_Only_RestoreScopeFollowsSubset verifies that when --only is set
// and --enable-restore is used, the config module map is scoped to the subset.
// Uses withMockCatalog to inject a catalog that matches both git and vscode.
func TestRunApply_Only_RestoreScopeFollowsSubset(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Git.Git":                    true,
		"Microsoft.VisualStudioCode": true,
	}}
	path := threeAppsManifest(t)

	catalog := map[string]*modules.Module{
		"apps.git": {
			ID:          "apps.git",
			DisplayName: "Git",
			Matches:     modules.MatchCriteria{Winget: []string{"Git.Git"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
		"apps.vscode": {
			ID:          "apps.vscode",
			DisplayName: "Visual Studio Code",
			Matches:     modules.MatchCriteria{Winget: []string{"Microsoft.VisualStudioCode"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
		"apps.7zip": {
			ID:          "apps.7zip",
			DisplayName: "7-Zip",
			Matches:     modules.MatchCriteria{Winget: []string{"7zip.7zip"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
	}

	var result *ApplyResult
	withMockDriver(md, func() {
		withMockCatalog(catalog, nil, func() {
			r, err := RunApply(ApplyFlags{
				Manifest: path,
				DryRun:   true,
				Only:     "git",
			})
			if err != nil {
				t.Fatalf("RunApply --only with catalog: unexpected error: %v", err)
			}
			result = r.(*ApplyResult)
		})
	})

	// Only git was selected; restoreModulesAvailable should only include git.
	if len(result.RestoreModulesAvailable) != 1 {
		t.Fatalf("restoreModulesAvailable len = %d, want 1 (only git)", len(result.RestoreModulesAvailable))
	}
	if result.RestoreModulesAvailable[0].ID != "apps.git" {
		t.Errorf("restoreModulesAvailable[0].ID = %q, want apps.git", result.RestoreModulesAvailable[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Capabilities: --only advertised in apply flags
// ---------------------------------------------------------------------------

func TestRunCapabilities_ApplyFlags_IncludesOnly(t *testing.T) {
	result, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities returned error: %v", err)
	}
	data := result.(CapabilitiesData)
	applyCmd, ok := data.Commands["apply"]
	if !ok {
		t.Fatal("apply command not found in capabilities")
	}
	found := false
	for _, f := range applyCmd.Flags {
		if f == "--only" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("commands.apply.flags does not contain --only; got %v", applyCmd.Flags)
	}
}

// ---------------------------------------------------------------------------
// Synthesis ordering regression: synthesized apps must be selectable via --only
// ---------------------------------------------------------------------------

// syntheticAppManifest writes a manifest with configModules that will cause
// SynthesizeAppsFromModules to produce a manual app entry for "lightroom", plus
// one regular winget app "git". Returns the manifest path.
func syntheticAppManifest(t *testing.T) string {
	t.Helper()
	content := `{
		"name": "synthesis-test",
		"apps": [
			{ "id": "git", "refs": { "windows": "Git.Git" } }
		],
		"configModules": ["apps.lightroom"]
	}`
	path := filepath.Join(t.TempDir(), "m.jsonc")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// lightroomCatalog returns a minimal module catalog with a pathExists module
// for lightroom that SynthesizeAppsFromModules will expand into a manual app.
func lightroomCatalog() map[string]*modules.Module {
	return map[string]*modules.Module{
		"apps.lightroom": {
			ID:          "apps.lightroom",
			DisplayName: "Lightroom Classic",
			Matches: modules.MatchCriteria{
				PathExists: []string{`%LOCALAPPDATA%\Adobe\Lightroom\nonexistent-sentinel.exe`},
			},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{}},
		},
	}
}

// TestRunApply_Only_SynthesizedApp_PresentWhenSelected verifies that a
// synthesized app (produced by SynthesizeAppsFromModules from a configModule
// with pathExists) can be selected via --only. The synthesized app must appear
// in the plan when its id is in --only.
func TestRunApply_Only_SynthesizedApp_PresentWhenSelected(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}
	path := syntheticAppManifest(t)

	var result *ApplyResult
	withMockDriver(md, func() {
		withMockCatalog(lightroomCatalog(), nil, func() {
			r, err := RunApply(ApplyFlags{
				Manifest: path,
				DryRun:   true,
				Only:     "lightroom", // select the synthesized app by its short id
			})
			if err != nil {
				t.Fatalf("RunApply --only synthesized app: unexpected error: %v", err)
			}
			result = r.(*ApplyResult)
		})
	})

	if result.Summary.Total != 1 {
		t.Errorf("summary.total = %d, want 1 (only lightroom)", result.Summary.Total)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].ID != "lightroom" {
		t.Errorf("actions[0].ID = %q, want lightroom", result.Actions[0].ID)
	}
	if result.Actions[0].Driver != "manual" {
		t.Errorf("actions[0].Driver = %q, want manual (synthesized app is a manual app)", result.Actions[0].Driver)
	}
}

// TestRunApply_Only_SynthesizedApp_AbsentWhenNotSelected verifies that when
// --only selects a different app (git), the synthesized lightroom app is NOT
// included in the plan. This is the regression guard for the synthesis ordering
// bug: synthesis must run before --only filtering, but the filter must still
// exclude unselected synthesized apps.
func TestRunApply_Only_SynthesizedApp_AbsentWhenNotSelected(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}
	path := syntheticAppManifest(t)

	var result *ApplyResult
	withMockDriver(md, func() {
		withMockCatalog(lightroomCatalog(), nil, func() {
			r, err := RunApply(ApplyFlags{
				Manifest: path,
				DryRun:   true,
				Only:     "git", // select only git; lightroom is synthesized but NOT selected
			})
			if err != nil {
				t.Fatalf("RunApply --only git (synthesized absent): unexpected error: %v", err)
			}
			result = r.(*ApplyResult)
		})
	})

	if result.Summary.Total != 1 {
		t.Errorf("summary.total = %d, want 1 (only git)", result.Summary.Total)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].ID != "git" {
		t.Errorf("actions[0].ID = %q, want git", result.Actions[0].ID)
	}
	for _, a := range result.Actions {
		if a.ID == "lightroom" {
			t.Error("lightroom must NOT appear in actions when not selected by --only")
		}
	}
}

// TestRunApply_Only_SynthesizedApp_UnknownIDError verifies that referencing a
// synthesized app id that is NOT in the catalog (and therefore not synthesized)
// produces MANIFEST_VALIDATION_ERROR naming the unknown id.
func TestRunApply_Only_SynthesizedApp_UnknownIDError(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}
	path := syntheticAppManifest(t)

	var eerr *envelope.Error
	withMockDriver(md, func() {
		withMockCatalog(lightroomCatalog(), nil, func() {
			_, eerr = RunApply(ApplyFlags{
				Manifest: path,
				DryRun:   true,
				Only:     "not-in-catalog", // id not in apps[] and not synthesized
			})
		})
	})

	if eerr == nil {
		t.Fatal("expected MANIFEST_VALIDATION_ERROR for unknown synthesized id, got nil")
	}
	if eerr.Code != envelope.ErrManifestValidationError {
		t.Errorf("error code = %q, want MANIFEST_VALIDATION_ERROR", eerr.Code)
	}
	if !containsStr(eerr.Message, "not-in-catalog") {
		t.Errorf("error message %q does not name the unknown id", eerr.Message)
	}
}

// ---------------------------------------------------------------------------
// Generation-recording integration test: --only (non-dry-run, mock driver)
// ---------------------------------------------------------------------------

// TestRunApply_Only_GenerationRecordsSubset verifies that after a real
// (non-dry-run) apply with --only, the written provisioning generation records
// only the selected app refs (not the unselected ones).
func TestRunApply_Only_GenerationRecordsSubset(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	// git is not installed → will be installed. vscode is not installed but is
	// NOT selected by --only → must not appear in the generation. 7zip is not
	// selected either.
	md := &mockDriver{installed: map[string]bool{}}
	path := threeAppsManifest(t)

	withMockDriver(md, func() {
		_, eerr := RunApply(ApplyFlags{
			Manifest: path,
			Only:     "git", // select only git
		})
		if eerr != nil {
			t.Fatalf("RunApply --only git: unexpected error: %v", eerr)
		}
	})

	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	g := gens[0]
	// Only git should be recorded.
	if len(g.AddedRefs) != 1 {
		t.Fatalf("addedRefs = %v, want [Git.Git]", g.AddedRefs)
	}
	if g.AddedRefs[0] != "Git.Git" {
		t.Errorf("addedRefs[0] = %q, want Git.Git", g.AddedRefs[0])
	}
	// Items must not include vscode or 7zip.
	for _, item := range g.Items {
		if item.ID == "vscode" || item.ID == "7zip" {
			t.Errorf("generation items must not include unselected app %q", item.ID)
		}
	}
}
