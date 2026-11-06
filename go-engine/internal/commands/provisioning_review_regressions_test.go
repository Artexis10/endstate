// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

func TestProvisioningReviewKeepsMixedBackendHistorySeparate(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	item := realizer.Installable{ID: "nix-app", Ref: "nixpkgs#nix-app"}
	current := nixSet("nix-app")
	backend := &fakeRealizer{planDiff: realizer.Diff{ToAdd: []realizer.Installable{item}}, currentSet: current, realizeResult: realizer.Result{Advanced: true, ToGeneration: 1, After: current}}
	brew := &fakeBrewDriver{installed: map[string]bool{}}
	mf := &manifest.Manifest{Apps: []manifest.App{nixApp(item.ID, item.Ref)}}
	brewApps := []manifest.App{{ID: "brew-app", Driver: "brew", Refs: map[string]string{"darwin": "brew-app"}}}
	raw, err := runApplyRealizer(ApplyFlags{}, mf, backend, noopEmitter(), "mixed-backends", nil, nil, nil, brewApps, brew)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.(*ApplyResult).Actions) != 2 {
		t.Fatalf("result lost a backend: %+v", raw)
	}
	gens, listErr := provision.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(gens) != 2 {
		t.Fatalf("missing backend record: %+v", gens)
	}
	for _, generation := range gens {
		expected := "nixpkgs#nix-app"
		if generation.Backend == "brew" {
			expected = "brew-app"
		}
		if len(generation.Items) != 1 || generation.Items[0].Ref != expected || len(generation.AddedRefs) != 1 || generation.AddedRefs[0] != expected {
			t.Fatalf("backend %s contains another backend's commit: %+v", generation.Backend, generation)
		}
	}
}

func TestProvisioningReviewRecordsHomeCommitBeforeActivationFailure(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	mf.Apps = nil
	activate := f.backend.Script
	f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
		_, _, _, err := activate(path, args...)
		if err != nil {
			t.Fatal(err)
		}
		return nil, []byte("installPackages failed after writeBoundary"), 1, nil
	}
	_, err := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "partial-home", nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("failed settings activation was reported as successful")
	}
	gens, listErr := provision.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(gens) != 1 || !gens[0].Partial || gens[0].HomeManager == nil || gens[0].HomeManager.Generation != 1 || gens[0].HomeManager.ActivationOutput != f.activeHome {
		t.Fatalf("committed native Home Manager identity was lost: %+v; error=%+v", gens, err)
	}
	data, _ := json.Marshal(gens[0].HomeManager)
	var home map[string]any
	_ = json.Unmarshal(data, &home)
	if home["activationFailed"] != true {
		t.Fatalf("profile commit was equated with completed settings activation: %s", data)
	}
	detail, ok := err.Detail.(map[string]string)
	if !ok || detail["stage"] != "commit" {
		t.Fatalf("post-commit failure lost its stage: %+v", err)
	}
	roots, _ := filepath.Glob(filepath.Join(filepath.Dir(f.backend.Profile), "activation-root-*"))
	if len(roots) == 0 {
		t.Fatal("committed activation roots were removed")
	}
	cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, f.backend)
	if cfg != nil || len(warnings) != 1 {
		t.Fatalf("capture trusted a failed settings activation: %+v %v", cfg, warnings)
	}
	item := checkHomeManagerGeneration(f.backend, noopEmitter())
	if item.Status != "fail" {
		t.Fatalf("verify trusted a profile commit as completed settings: %+v", item)
	}
	// The committed output remains Endstate-owned so the same declaration can repair it.
	f.backend.Script = activate
	if _, err := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "repair-home", nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	gens, listErr = provision.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(gens) != 2 || gens[0].Partial || gens[0].HomeManager == nil || gens[0].HomeManager.ActivationFailed {
		t.Fatalf("successful repair retained failed activation state: %+v", gens)
	}
	cfg, warnings = recoverHomeManagerWithWarnings(CaptureFlags{}, f.backend)
	if cfg == nil || cfg.Settings == nil || len(warnings) != 0 {
		t.Fatalf("repaired declaration did not become capturable: %+v %v", cfg, warnings)
	}
}

func TestProvisioningReviewRecordsHomeRollbackCommitBeforeFailure(t *testing.T) {
	f := newNativeFreezeFixture(t)
	resolution := &realizer.InputResolution{Derivation: "/nix/store/home.drv", Outputs: []string{"out"}, OutputPaths: map[string]string{"out": "/nix/store/home-output"}}
	target := &provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{Kind: "settings", Generated: true, Flake: "path:/old-generated#profile", Generation: 2, ActivationOutput: "/nix/store/home-output", Resolution: resolution}}
	if err := provision.Write(target); err != nil {
		t.Fatal(err)
	}
	activate := f.backend.Script
	f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
		_, _, _, err := activate(path, args...)
		if err != nil {
			t.Fatal(err)
		}
		return nil, []byte("installPackages failed after writeBoundary"), 1, nil
	}
	_, err := runRealizerRollback(RollbackFlags{To: "1", Confirm: true, EnableRestore: true}, f.backend)
	if err == nil {
		t.Fatal("failed settings rollback was reported as successful")
	}
	gens, listErr := provision.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(gens) != 2 || !gens[0].Rollback || !gens[0].Partial || gens[0].HomeManager == nil || gens[0].HomeManager.Generation != 1 || gens[0].HomeManager.ActivationOutput != f.activeHome {
		t.Fatalf("committed native rollback identity was lost: %+v; error=%+v", gens, err)
	}
	data, _ := json.Marshal(gens[0].HomeManager)
	var home map[string]any
	_ = json.Unmarshal(data, &home)
	if home["activationFailed"] != true {
		t.Fatalf("profile rollback commit was reported as completed settings: %s", data)
	}
}

func TestProvisioningReviewCaptureFindsOlderOwnedOutput(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	old := homeGenerationReference(flags, mf, &preparedHomeManager{flake: "path:/generated#profile", generated: true}, 2)
	old.ActivationOutput = "/nix/store/old-output"
	latest := *old
	latest.Generation = 3
	latest.ActivationOutput = "/nix/store/new-output"
	if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: old}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: &latest}); err != nil {
		t.Fatal(err)
	}
	f.activeHome = old.ActivationOutput
	cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, f.backend)
	if cfg == nil || cfg.Settings == nil || cfg.Settings.Git.DefaultBranch != "trunk" || len(warnings) != 0 {
		t.Fatalf("capture did not find matching older ownership: %+v %v", cfg, warnings)
	}
}

func TestProvisioningReviewCaptureDoesNotLaunderLegacyOwnership(t *testing.T) {
	f := newNativeFreezeFixture(t)
	if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{Kind: "settings", Generated: true, Generation: 2, ActivationOutput: "/nix/store/known-output"}}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{Settings: &manifest.HomeManagerSettings{Git: &manifest.GitSettings{DefaultBranch: "legacy"}}}}); err != nil {
		t.Fatal(err)
	}
	f.activeHome = "/nix/store/unrecorded-output"
	cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, f.backend)
	if cfg != nil || len(warnings) != 1 {
		t.Fatalf("legacy settings laundered unmatched output-backed history: %+v %v", cfg, warnings)
	}
}

func TestProvisioningReviewChangedTargetIsNotPresentUnderTheSameID(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	if _, err := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "original-target", nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	fresh := nix.New()
	fresh.Profile = f.backend.Profile
	fresh.Run = f.run
	mf.Apps[1].Refs[runtime.GOOS] = "newAttribute"
	desired := []realizer.Installable{{ID: mf.Apps[1].ID, Ref: "newAttribute"}}
	diff, err := fresh.Plan(desired)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.ToAdd) != 1 || len(diff.Present) != 0 {
		t.Fatalf("same portable ID concealed a changed requested target: %+v", diff)
	}
	raw, verifyErr := runVerifyRealizer(VerifyFlags{}, mf, fresh, noopEmitter(), nil, nil)
	if verifyErr != nil {
		t.Fatal(verifyErr)
	}
	found := false
	for _, item := range raw.(*VerifyResult).Results {
		if item.ID == mf.Apps[1].ID {
			found = true
			if item.Status != "fail" {
				t.Fatalf("old target verified a changed request: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("verify omitted the changed target")
	}
}

func TestProvisioningReviewFailedSettingsDoNotPassGenerationVerification(t *testing.T) {
	f := newNativeFreezeFixture(t)
	// Read the persisted contract directly so this fixture also covers older readers.
	if err := os.MkdirAll(provision.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	record := `{"schemaVersion":"2.0","number":1,"backend":"nix","homeManager":{"kind":"settings","generated":true,"generation":1,"activationOutput":"/nix/store/home-output","activationFailed":true}}`
	if err := os.WriteFile(filepath.Join(provision.Dir(), "000001.json"), []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
	f.activeHome = "/nix/store/home-output"
	profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
	if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("home-manager-1-link", profile); err != nil {
		t.Fatal(err)
	}
	item := checkHomeManagerGeneration(f.backend, noopEmitter())
	if item.Status != "fail" {
		t.Fatalf("native generation match hid failed settings activation: %+v", item)
	}
}

func TestProvisioningReviewFailedRetryInvalidatesSameActiveHomeOutput(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	mf.Apps = nil
	if _, err := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "completed-home", nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	prior := gens[0].HomeManager
	// An equivalent freshly evaluated input still produces the already-active output.
	native := f.backend.Run
	f.backend.Run = func(args ...string) ([]byte, []byte, int, error) {
		data, stderr, exit, err := native(args...)
		if args[0] == "flake" && err == nil && exit == 0 {
			var metadata map[string]any
			if err = json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			metadata["url"] = "path:/equivalent-new-source"
			data, err = json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
		}
		return data, stderr, exit, err
	}
	activate := f.backend.Script
	f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
		_, _, _, err := activate(path, args...)
		if err != nil {
			t.Fatal(err)
		}
		return nil, []byte("settings step failed with the same active profile"), 1, nil
	}
	_, failure := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "failed-same-output", nil, nil, nil, nil, nil)
	if failure == nil {
		t.Fatal("failed retry was reported as complete")
	}
	gens, err = provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 2 || !gens[0].Partial || gens[0].HomeManager == nil || !gens[0].HomeManager.ActivationFailed || gens[0].HomeManager.Generation != prior.Generation {
		t.Fatalf("failed retry left completed-settings evidence active: %+v; error=%+v", gens, failure)
	}
	current := gens[0].HomeManager
	if current.Resolution.Source.URL != prior.Resolution.Source.URL || current.ActivationOutput != prior.ActivationOutput || current.FileExpectations == nil || !reflect.DeepEqual(current.FileExpectations, prior.FileExpectations) {
		t.Fatalf("unchanged native identity acquired an unused source: prior=%+v current=%+v", prior, current)
	}
	details, ok := failure.Detail.(map[string]string)
	if !ok || details["stage"] != "activation" {
		t.Fatalf("unchanged profile was reported as a new commit: %+v", failure)
	}
	cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, f.backend)
	if cfg != nil || len(warnings) != 1 {
		t.Fatalf("failed same-output retry remained capturable: %+v %v", cfg, warnings)
	}
	if item := checkHomeManagerGeneration(f.backend, noopEmitter()); item.Status != "fail" {
		t.Fatalf("failed same-output retry remained verified: %+v", item)
	}
	if action := externalHomeManagerAction(f.backend, "path:/generated#profile", true); action != nil {
		t.Fatalf("failed retry lost existing ownership: %+v", action)
	}
}

func TestProvisioningReviewRefreshKeepsTheChangedRefResolution(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	oldResolution := &realizer.InputResolution{Derivation: "/nix/store/old-target.drv"}
	newResolution := &realizer.InputResolution{Derivation: "/nix/store/new-target.drv"}
	desired := realizer.Installable{ID: "app", Ref: "newAttribute", Resolution: newResolution}
	// A later inventory can retain an old attributed entry while its new wrapper
	// row has no resolution yet. It must not erase the observed realization's identity.
	current := realizer.Set{Generation: 2, Elements: map[string]realizer.Element{
		"old-native": {ID: "app", RequestedRef: "oldAttribute", Name: "old-native", Resolution: oldResolution},
		"new-native": {ID: "app", RequestedRef: "newAttribute", Name: "new-native", AttrPath: "packages.x86_64-linux.p1", StorePaths: []string{"/nix/store/new-target"}},
	}}
	after := realizer.Set{Generation: 2, Elements: map[string]realizer.Element{
		"new-native": {ID: "app", RequestedRef: "newAttribute", Name: "new-native", Resolution: newResolution},
	}}
	backend := &fakeRealizer{planDiff: realizer.Diff{ToAdd: []realizer.Installable{desired}}, currentSet: current, realizeResult: realizer.Result{Advanced: true, ToGeneration: 2, After: after}}
	mf := &manifest.Manifest{Apps: []manifest.App{nixApp("app", "newAttribute")}}
	if _, err := runApplyRealizer(ApplyFlags{}, mf, backend, noopEmitter(), "changed-ref-no-prune", nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 || len(gens[0].Items) != 1 || gens[0].Items[0].Resolution == nil || gens[0].Items[0].Resolution.Derivation != newResolution.Derivation {
		t.Fatalf("old requested target overwrote the installed target's provenance: %+v", gens)
	}
}

func TestProvisioningReviewMissingRetryHistoryKeepsOnlyObservedIdentity(t *testing.T) {
	for _, history := range []string{"missing", "unreadable"} {
		t.Run(history, func(t *testing.T) {
			f := newNativeFreezeFixture(t)
			f.activeHome = "/nix/store/home-output"
			profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
			if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("home-manager-3-link", profile); err != nil {
				t.Fatal(err)
			}
			f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
				return nil, []byte("settings retry failed"), 1, nil
			}
			if history == "unreadable" {
				previous := listGenerationsFn
				listGenerationsFn = func() ([]*provision.Generation, error) { return nil, fmt.Errorf("history unavailable") }
				t.Cleanup(func() { listGenerationsFn = previous })
			}
			mf := &manifest.Manifest{HomeManager: &manifest.HomeManagerConfig{Flake: "github:example/unused-config#profile"}}
			_, failure := runApplyRealizer(ApplyFlags{EnableRestore: true}, mf, f.backend, noopEmitter(), "unknown-prior-source", nil, nil, nil, nil, nil)
			if failure == nil {
				t.Fatal("failed retry was reported as complete")
			}
			gens, err := provision.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(gens) != 1 || !gens[0].Partial || gens[0].HomeManager == nil {
				t.Fatalf("observed failed attempt was lost: %+v", gens)
			}
			want := provision.HomeGenRef{Generation: 3, ActivationOutput: f.activeHome, ActivationFailed: true}
			if !reflect.DeepEqual(*gens[0].HomeManager, want) {
				t.Fatalf("unknown historical source acquired prepared identity: got %+v, want %+v", gens[0].HomeManager, want)
			}
			encoded, err := json.Marshal(gens[0].HomeManager)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if _, exists := fields["flake"]; exists {
				t.Fatalf("unknown historical source was serialized as a declaration: %s", encoded)
			}
		})
	}
}

func TestProvisioningReviewRollbackRetryWithoutMatchingHistoryKeepsOnlyObservedIdentity(t *testing.T) {
	f := newNativeFreezeFixture(t)
	f.activeHome = "/nix/store/home-output"
	profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
	if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("home-manager-3-link", profile); err != nil {
		t.Fatal(err)
	}
	f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
		return nil, []byte("settings retry failed"), 1, nil
	}
	target := &provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{
		Kind: "flake", Flake: "github:example/unused-config#profile", Generation: 1, ActivationOutput: f.activeHome,
		Resolution: &realizer.InputResolution{Derivation: "/nix/store/home.drv", Outputs: []string{"out"}, OutputPaths: map[string]string{"out": f.activeHome}},
	}}
	if err := provision.Write(target); err != nil {
		t.Fatal(err)
	}
	_, failure := runRealizerRollback(RollbackFlags{To: "1", EnableRestore: true, Confirm: true}, f.backend)
	if failure == nil {
		t.Fatal("failed rollback retry was reported as complete")
	}
	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 2 || !gens[0].Rollback || !gens[0].Partial || gens[0].HomeManager == nil {
		t.Fatalf("observed failed rollback attempt was lost: %+v", gens)
	}
	want := provision.HomeGenRef{Generation: 3, ActivationOutput: f.activeHome, ActivationFailed: true}
	if !reflect.DeepEqual(*gens[0].HomeManager, want) {
		t.Fatalf("unchanged generation acquired the requested rollback source: got %+v, want %+v", gens[0].HomeManager, want)
	}
}

func TestProvisioningReviewCaptureUpdateAfterUnknownSourceRollbackOmitsUnprovedDeclaration(t *testing.T) {
	f := newNativeFreezeFixture(t)
	f.activeHome = "/nix/store/home-output"
	profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
	if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("home-manager-3-link", profile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.activeHome, profile+"-3-link"); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{
		Generation: 3, ActivationOutput: f.activeHome, ActivationFailed: true,
	}}); err != nil {
		t.Fatal(err)
	}
	f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
		if path != f.activeHome+"/activate" || len(args) != 0 {
			t.Fatalf("rollback changed its native snapshot: %s %v", path, args)
		}
		if err := os.Remove(profile); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("home-manager-4-link", profile); err != nil {
			t.Fatal(err)
		}
		return nil, nil, 0, nil
	}
	if _, failure := runRealizerRollback(RollbackFlags{To: "1", EnableRestore: true, Confirm: true}, f.backend); failure != nil {
		t.Fatal(failure)
	}
	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	want := provision.HomeGenRef{Generation: 4, ActivationOutput: f.activeHome}
	if len(gens) != 2 || gens[0].HomeManager == nil || !reflect.DeepEqual(*gens[0].HomeManager, want) {
		t.Fatalf("successful snapshot rollback did not retain unknown source identity: %+v", gens)
	}
	existing := filepath.Join(t.TempDir(), "existing.jsonc")
	if err := os.WriteFile(existing, []byte(`{"version":1,"name":"existing","apps":[],"homeManager":{"flake":"github:example/unused-new-input#profile"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	raw, failure := runCaptureRealizer(CaptureFlags{Update: true, Manifest: existing, Out: out}, f.backend, noopEmitter())
	if failure != nil {
		t.Fatal(failure)
	}
	warnings := raw.(*CaptureResult).Warnings
	if captured := readCapturedManifest(t, out); captured.HomeManager != nil || len(warnings) != 1 || warnings[0].Code != "home_manager_provenance_unavailable" {
		t.Fatalf("capture update attributed an unused declaration after snapshot rollback: %+v; warnings=%v", captured.HomeManager, warnings)
	}
}
