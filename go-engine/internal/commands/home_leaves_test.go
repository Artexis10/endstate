// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// useFakeHomeStore points the activation reader at a temporary directory that
// stands in for the Nix store.
func useFakeHomeStore(t *testing.T) string {
	t.Helper()
	store := t.TempDir()
	old := homeStoreRoot
	homeStoreRoot = store
	t.Cleanup(func() { homeStoreRoot = old })
	return store
}

// builtActivation returns a fake built activation package whose files tree holds
// the given leaves, as slash paths below the home directory.
func builtActivation(t *testing.T, store string, leaves ...string) string {
	t.Helper()
	output := t.TempDir()
	writeBuiltActivation(t, store, output, leaves...)
	return output
}

// writeBuiltActivation writes the package format that Home Manager builds: a
// format marker, and a link to a store directory that holds one empty file per leaf.
func writeBuiltActivation(t *testing.T, store, output string, leaves ...string) {
	t.Helper()
	files := filepath.Join(store, filepath.Base(output)+"-home-manager-files")
	if err := os.MkdirAll(files, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, leaf := range leaves {
		path := filepath.Join(files, filepath.FromSlash(leaf))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "gen-version"), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(files, filepath.Join(output, "home-files")); err != nil {
		t.Fatal(err)
	}
}

// leafApplyHome is a home directory with a restore payload beside the manifest.
func leafApplyHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Home Manager symlink workflow; os.UserHomeDir reads USERPROFILE there")
	}
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	t.Setenv("HOME", root)
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("restored"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// applyHomeBesideDirectRestores applies an explicit Home Manager flake and one
// package beside direct restores of the payload to the given targets.
func applyHomeBesideDirectRestores(t *testing.T, root string, backend *expectationHomeBackend, dryRun bool, targets ...string) (*ApplyResult, *envelope.Error) {
	t.Helper()
	inputs := emptyConfigRestoreInputs()
	for _, target := range targets {
		inputs.ordinaryRestores = append(inputs.ordinaryRestores, manifest.RestoreEntry{Type: "copy", Source: "payload", Target: target})
	}
	backend.planDiff.ToAdd = []realizer.Installable{{ID: "tool", Ref: "nixpkgs#tool"}}
	backend.realizeResult = realizer.Result{Advanced: true, ToGeneration: 2}
	flags := ApplyFlags{Manifest: filepath.Join(root, "manifest.json"), EnableRestore: true, DryRun: dryRun,
		configRestoreRepoRoot: root, configRestoreRuntime: newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot())}
	mf := nixManifest(nixApp("tool", "nixpkgs#tool"))
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: "path:/fixture#profile"}
	raw, err := runApplyRealizer(flags, mf, backend, noopEmitter(), "leaves", nil, nil, nil, nil, nil)
	result, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("apply returned no result: %T %+v", raw, err)
	}
	return result, err
}

func assertBlockedActivation(t *testing.T, err *envelope.Error, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("activation was not blocked with %q", reason)
	}
	if detail, _ := err.Detail.(map[string]string); err.Code != envelope.ErrRestoreFailed || detail["reason"] != reason {
		t.Fatalf("activation was not blocked with %q: %+v", reason, err)
	}
}

func TestApplyDirectRestoreInsideRecursiveHomeManagerRootIsBlocked(t *testing.T) {
	root := leafApplyHome(t)
	inside := filepath.Join(root, ".config", "themes", "dark.conf")
	unrelated := filepath.Join(root, "unrelated")
	// Evaluation lists only the flat target; the recursive root expands to the other leaves.
	flat := filepath.Join(root, ".gitconfig")
	backend := &expectationHomeBackend{recursive: true, targets: []string{flat},
		output: builtActivation(t, useFakeHomeStore(t), ".gitconfig", ".config/themes/dark.conf", ".config/themes/light.conf")}
	result, err := applyHomeBesideDirectRestores(t, root, backend, false, inside, unrelated)
	assertBlockedActivation(t, err, "target_collision")
	if result.HomeManager.Activated || backend.homeObservation.Generation != 0 {
		t.Fatalf("activation ran over a direct restore: %+v", result.HomeManager)
	}
	if _, statErr := os.Lstat(inside); !os.IsNotExist(statErr) {
		t.Fatalf("blocked direct restore wrote its target: %v", statErr)
	}
	if got, readErr := os.ReadFile(unrelated); readErr != nil || string(got) != "restored" {
		t.Fatalf("unrelated direct restore was lost: %q %v", got, readErr)
	}
	if backend.realizeCalls != 1 {
		t.Fatalf("package outcome was lost: %d realizations", backend.realizeCalls)
	}
}

func TestApplyDirectRestoreOnActivationOnlyFlakeFileIsBlocked(t *testing.T) {
	root := leafApplyHome(t)
	placed := filepath.Join(root, ".config", "tool", "settings.toml")
	// An activation-only flake exposes no parent configuration, so evaluation lists nothing.
	backend := &expectationHomeBackend{output: builtActivation(t, useFakeHomeStore(t), ".config/tool/settings.toml")}
	result, err := applyHomeBesideDirectRestores(t, root, backend, false, placed)
	assertBlockedActivation(t, err, "target_collision")
	if result.HomeManager.Activated {
		t.Fatalf("activation ran over a direct restore: %+v", result.HomeManager)
	}
	if _, statErr := os.Lstat(placed); !os.IsNotExist(statErr) {
		t.Fatalf("blocked direct restore wrote its target: %v", statErr)
	}
}

func TestApplyUnreadableHomeFilesTreeBlocksOnlyTheActivation(t *testing.T) {
	for _, test := range []struct {
		name     string
		limit    int
		prepare  func(t *testing.T, output string)
		diagnose string
	}{
		{name: "unreadable package", diagnose: "gen-version", prepare: func(t *testing.T, output string) {
			if err := os.Remove(filepath.Join(output, "gen-version")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "tree above the entry bound", limit: 2, diagnose: "~/.config"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := leafApplyHome(t)
			if test.limit > 0 {
				old := homeEntryLimit
				homeEntryLimit = test.limit
				t.Cleanup(func() { homeEntryLimit = old })
			}
			output := builtActivation(t, useFakeHomeStore(t), ".config/a", ".config/b", ".config/c")
			if test.prepare != nil {
				test.prepare(t, output)
			}
			direct := filepath.Join(root, "direct")
			backend := &expectationHomeBackend{output: output}
			result, err := applyHomeBesideDirectRestores(t, root, backend, false, direct)
			assertBlockedActivation(t, err, "target_detection_failed")
			if detail, _ := err.Detail.(map[string]string); !strings.Contains(detail["diagnostic"], test.diagnose) {
				t.Fatalf("diagnostic %q does not name %q", detail["diagnostic"], test.diagnose)
			}
			if result.HomeManager.Activated {
				t.Fatalf("activation ran without a checked tree: %+v", result.HomeManager)
			}
			if got, readErr := os.ReadFile(direct); readErr != nil || string(got) != "restored" {
				t.Fatalf("direct restore stopped for an unrelated tree failure: %q %v", got, readErr)
			}
			if backend.realizeCalls != 1 {
				t.Fatalf("package outcome was lost: %d realizations", backend.realizeCalls)
			}
		})
	}
}

func TestApplyUnreadableHomeFilesTreeStillActivatesWhenNoOtherOwnerClaimsAFile(t *testing.T) {
	root := leafApplyHome(t)
	output := builtActivation(t, useFakeHomeStore(t), ".config/a")
	if err := os.Remove(filepath.Join(output, "gen-version")); err != nil {
		t.Fatal(err)
	}
	backend := &expectationHomeBackend{output: output}
	result, err := applyHomeBesideDirectRestores(t, root, backend, false)
	if err != nil || !result.HomeManager.Activated {
		t.Fatalf("activation was withheld for a tree nothing could collide with: activated=%v err=%+v", result.HomeManager.Activated, err)
	}
}

func TestCapturedHomeManagerPlacementInsideExplicitRecursiveRootCollides(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "explicit-rc"), []byte("--explicit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declareInManifest(t, path, map[string]any{"homeManager": map[string]any{"settings": map[string]any{"files": map[string]string{".explicitrc": "./explicit-rc"}}}})
	backend.targets = append(backend.targets, filepath.Join(home, ".explicitrc"))
	// The explicit activation also places a recursive root that holds a captured target.
	backend.hiddenLeaves = []string{".config/ripgrep/config"}
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err == nil || err.Code != envelope.ErrRestoreFailed {
		t.Fatalf("placement inside a recursive root was not rejected: %+v", err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusFailed, planner.ReasonTargetCollision)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	if backend.activated != 0 || backend.prepared != 1 {
		t.Fatalf("composed activation was prepared or ran: prepared=%d activated=%d", backend.prepared, backend.activated)
	}
	assertNoFile(t, backend.targets...)
}

func TestCapturedHomeManagerUnreadableTreeFailsRowsAsDetectionFailure(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	old := homeEntryLimit
	homeEntryLimit = 1
	t.Cleanup(func() { homeEntryLimit = old })
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	assertBlockedActivation(t, err, "target_detection_failed")
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusFailed, planner.ReasonTargetDetectionFailed)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	if backend.activated != 0 {
		t.Fatalf("activation ran without a checked tree: %d", backend.activated)
	}
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

func TestPreviewWarnsWhenHomeManagerCollisionCheckIsIncomplete(t *testing.T) {
	for _, test := range []struct {
		name    string
		backend expectationHomeBackend
		warned  bool
	}{
		{name: "recursive declaration", backend: expectationHomeBackend{targets: []string{}, recursive: true}, warned: true},
		{name: "no parent configuration", backend: expectationHomeBackend{}, warned: true},
		{name: "all targets evaluated", backend: expectationHomeBackend{targets: []string{"/home/user/.gitconfig"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := leafApplyHome(t)
			backend := test.backend
			result, err := applyHomeBesideDirectRestores(t, root, &backend, true, filepath.Join(root, "direct"))
			if err != nil {
				t.Fatal(err)
			}
			var codes []string
			for _, warning := range result.Warnings {
				codes = append(codes, warning.Code)
			}
			var want []string
			if test.warned {
				want = []string{"home_manager_collision_check_incomplete"}
			}
			if !slices.Equal(codes, want) {
				t.Fatalf("warnings = %v, want %v", codes, want)
			}
		})
	}
}

func TestCapturedHomeManagerPreviewReportsTheBlockedRowThatApplyWould(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "ordinary-src"), []byte("ordinary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".ripgreprc")
	declareInManifest(t, path, map[string]any{"restore": []map[string]any{{"type": "copy", "source": "ordinary-src", "target": target}}})
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true, DryRun: true})
	if err == nil || err.Code != envelope.ErrRestoreFailed {
		t.Fatalf("overlap was not rejected in preview: %+v", err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusFailed, planner.ReasonTargetCollision)
	if backend.built || backend.activated != 0 {
		t.Fatalf("preview built or activated: built=%v activated=%d", backend.built, backend.activated)
	}
	assertNoFile(t, target)
}
