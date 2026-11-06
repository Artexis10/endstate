// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestApplyHomeDirectCollisionPreservesIndependentOutcomes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux capture workflow")
	}
	for _, test := range []struct {
		name    string
		preview bool
	}{
		{name: "exact target"},
		{name: "preview", preview: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifestPath, root, home, capture := configVerifyCaptureFixture(t)
			mf, err := manifest.LoadManifest(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(home, ".ripgreprc")
			if err := os.WriteFile(target, []byte("prior\n"), 0600); err != nil {
				t.Fatal(err)
			}
			independent := filepath.Join(root, "independent")
			payload := filepath.Join(filepath.Dir(manifestPath), "independent-source")
			if err := os.WriteFile(payload, []byte("restored\n"), 0600); err != nil {
				t.Fatal(err)
			}
			mf.Restore = append(mf.Restore, manifest.RestoreEntry{Type: "copy", Source: "independent-source", Target: independent})
			mf.HomeManager = &manifest.HomeManagerConfig{Settings: &manifest.HomeManagerSettings{Files: map[string]string{".ripgreprc": "independent-source"}}}
			mf.Apps = append(mf.Apps, nixApp("tool", "nixpkgs#tool"))
			body, _ := json.Marshal(mf)
			if err := os.WriteFile(manifestPath, body, 0600); err != nil {
				t.Fatal(err)
			}
			outputRoot := filepath.Join(root, "frozen-home")
			writeBuiltActivation(t, useFakeHomeStore(t), outputRoot, ".ripgreprc")
			if err := os.WriteFile(filepath.Join(outputRoot, "endstate-home-files.json"), []byte(`{"files":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			backend := &expectationHomeBackend{output: outputRoot, targets: []string{target}}
			item := realizer.Installable{ID: "tool", Ref: "nixpkgs#tool"}
			backend.planDiff.ToAdd = []realizer.Installable{item}
			backend.realizeResult = realizer.Result{Advanced: true, ToGeneration: 2}
			old := newRealizerFn
			newRealizerFn = func() (realizer.Realizer, error) { return backend, nil }
			t.Cleanup(func() { newRealizerFn = old })
			var raw interface{}
			var applyErr *envelope.Error
			output := captureStderr(t, func() {
				raw, applyErr = RunApply(ApplyFlags{Manifest: manifestPath, EnableRestore: true, NoBootstrap: true, DryRun: test.preview, Events: "jsonl"})
			})
			if applyErr == nil || applyErr.Code != envelope.ErrRestoreFailed || !strings.Contains(output, "target_collision") {
				t.Fatalf("collision result missing: result=%+v error=%+v events=%s", raw, applyErr, output)
			}
			result, ok := raw.(*ApplyResult)
			if !ok || result.HomeManager == nil || result.HomeManager.Activated || backend.homeObservation.Generation != 0 {
				t.Fatalf("conflicting activation ran or lost its result: %+v", raw)
			}
			if strings.Contains(output, "would_configure") {
				t.Fatalf("blocked preview promised activation: %s", output)
			}
			if got, err := os.ReadFile(target); err != nil || string(got) != "prior\n" {
				t.Fatalf("conflicting restore mutated target: %q %v", got, err)
			}
			if result.ConfigResultFields == nil {
				t.Fatal("captured config outcomes disappeared")
			}
			found := false
			for _, set := range result.ConfigResolutions {
				if set.CaptureID == capture.CaptureID && set.Reason != nil && *set.Reason == "target_collision" {
					found = true
				}
			}
			if !found {
				t.Fatalf("conflicting direct owner was not rejected: %+v", result.ConfigResolutions)
			}
			if test.preview {
				if backend.realizeCalls != 0 || backend.built {
					t.Fatal("preview built or committed packages")
				}
				if _, err := os.Stat(independent); !os.IsNotExist(err) {
					t.Fatalf("preview wrote independent target: %v", err)
				}
			} else {
				if got, err := os.ReadFile(independent); err != nil || string(got) != "restored\n" {
					t.Fatalf("independent restore lost: %q %v", got, err)
				}
				if backend.realizeCalls != 1 || result.Summary.Success != 1 {
					t.Fatalf("package result lost: %+v", result)
				}
				history, err := provision.List()
				if err != nil || len(history) != 1 || history[0].Native != "2" || history[0].HomeManager != nil {
					t.Fatalf("package provenance lost or activation fabricated: %+v %v", history, err)
				}
			}
		})
	}
}

func TestApplyHomeDisjointFromDirectCollisionAndExistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Home Manager symlink workflow")
	}
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	managed := filepath.Join(root, "managed")
	if err := os.Symlink(filepath.Join(root, "unbuilt-source"), managed); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(root, "shared")
	if err := os.WriteFile(shared, []byte("prior"), 0600); err != nil {
		t.Fatal(err)
	}
	inputs := emptyConfigRestoreInputs()
	inputs.ordinaryRestores = []manifest.RestoreEntry{
		{Type: "copy", Source: "first", Target: shared},
		{Type: "copy", Source: "second", Target: shared},
	}
	t.Setenv("HOME", root)
	backend := &expectationHomeBackend{output: builtActivation(t, useFakeHomeStore(t), "managed"), targets: []string{managed}}
	flags := ApplyFlags{Manifest: filepath.Join(root, "manifest.json"), EnableRestore: true,
		configRestoreRepoRoot: root, configRestoreRuntime: newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot())}
	raw, err := runApplyRealizer(flags, &manifest.Manifest{HomeManager: &manifest.HomeManagerConfig{Flake: "path:/fixture#profile"}}, backend, noopEmitter(), "disjoint", nil, nil, nil, nil, nil)
	if err != nil || raw.(*ApplyResult).HomeManager == nil || !raw.(*ApplyResult).HomeManager.Activated || backend.homeObservation.Generation != 1 {
		t.Fatalf("unrelated collision or legitimate symlink blocked Home Manager: %+v %+v", raw, err)
	}
	if got, err := os.ReadFile(shared); err != nil || string(got) != "prior" {
		t.Fatalf("colliding direct writers changed target: %q %v", got, err)
	}
	if got, err := os.Readlink(managed); err != nil || got != filepath.Join(root, "unbuilt-source") {
		t.Fatalf("identity lookup changed leaf: %q %v", got, err)
	}
}

func TestApplyHomeOrdinaryCollisionWithoutConfigResultFields(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Home Manager symlink workflow")
	}
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent=%t", parent), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("ENDSTATE_ROOT", root)
			target := filepath.Join(root, "settings")
			homeTarget := target
			if parent {
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				homeTarget = filepath.Join(target, "prefs")
			}
			if err := os.WriteFile(homeTarget, []byte("prior"), 0600); err != nil {
				t.Fatal(err)
			}
			inputs := emptyConfigRestoreInputs()
			inputs.ordinaryRestores = []manifest.RestoreEntry{{Type: "copy", Source: "settings-source", Target: target}}
			t.Setenv("HOME", root)
			leaf, _ := filepath.Rel(root, homeTarget)
			backend := &expectationHomeBackend{output: builtActivation(t, useFakeHomeStore(t), filepath.ToSlash(leaf)), targets: []string{homeTarget}}
			flags := ApplyFlags{Manifest: filepath.Join(root, "manifest.json"), EnableRestore: true,
				configRestoreRepoRoot: root, configRestoreRuntime: newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot())}
			raw, err := runApplyRealizer(flags, &manifest.Manifest{HomeManager: &manifest.HomeManagerConfig{Flake: "path:/fixture#profile"}}, backend, noopEmitter(), "ordinary-collision", nil, nil, nil, nil, nil)
			if err == nil || err.Code != envelope.ErrRestoreFailed || raw.(*ApplyResult).ConfigResultFields != nil || raw.(*ApplyResult).HomeManager.Activated || backend.homeObservation.Generation != 0 {
				t.Fatalf("private collision flag lost with absent config fields: %+v %+v", raw, err)
			}
			if got, err := os.ReadFile(homeTarget); err != nil || string(got) != "prior" {
				t.Fatalf("ordinary collision mutated target: %q %v", got, err)
			}
		})
	}
}

func TestApplyImportOnlyHomeManagerDoesNotClaimDirectTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated Home Manager workflow")
	}
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	t.Setenv("HOME", root)
	target := filepath.Join(root, "settings")
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("restored"), 0600); err != nil {
		t.Fatal(err)
	}
	inputs := emptyConfigRestoreInputs()
	inputs.ordinaryRestores = []manifest.RestoreEntry{{Type: "copy", Source: "payload", Target: target}}
	backend := &expectationHomeBackend{output: "/nix/store/frozen-home", activeOutput: "/nix/store/external-home", targets: []string{target}}
	backend.homeObservation.Generation = 4
	flags := ApplyFlags{Manifest: filepath.Join(root, "manifest.json"), EnableRestore: true,
		configRestoreRepoRoot: root, configRestoreRuntime: newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot())}
	mf := &manifest.Manifest{HomeManager: &manifest.HomeManagerConfig{Settings: &manifest.HomeManagerSettings{Files: map[string]string{"settings": "payload"}}}}
	var raw interface{}
	var applyErr *envelope.Error
	withFakeGenerations(nil, nil, func() {
		raw, applyErr = runApplyRealizer(flags, mf, backend, noopEmitter(), "import", nil, nil, nil, nil, nil)
	})
	if applyErr != nil || raw.(*ApplyResult).HomeManager.OwnershipAction == nil || raw.(*ApplyResult).HomeManager.Activated || backend.built || backend.homeObservation.Generation != 4 {
		t.Fatalf("import-only action selected a writer: %+v %+v", raw, applyErr)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "restored" {
		t.Fatalf("import-only action blocked direct restore: %q %v", got, err)
	}
}

func TestApplyHomeAncestorAliasCollision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Home Manager symlink workflow")
	}
	for _, preview := range []bool{true, false} {
		t.Run(map[bool]string{true: "preview", false: "live"}[preview], func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("ENDSTATE_ROOT", root)
			realDir := filepath.Join(root, "real")
			alias := filepath.Join(root, "alias")
			if err := os.Mkdir(realDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(realDir, alias); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(realDir, "settings")
			hmTarget := filepath.Join(alias, "settings")
			payload := filepath.Join(root, "payload")
			hmSource := filepath.Join(root, "native-source")
			for p, v := range map[string]string{target: "prior", payload: "direct", hmSource: "home-manager"} {
				if err := os.WriteFile(p, []byte(v), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("HOME", root)
			backend := &expectationHomeBackend{output: builtActivation(t, useFakeHomeStore(t), "alias/settings"), targets: []string{hmTarget}}
			directObserved := false
			backend.activate = func(string) {
				data, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				directObserved = string(data) == "direct"
				// Reproduce the pinned link stage's backup and symlink operations on disposable files.
				if err := os.Rename(hmTarget, hmTarget+".endstate-backup"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(hmSource, hmTarget); err != nil {
					t.Fatal(err)
				}
			}
			old := newRealizerFn
			newRealizerFn = func() (realizer.Realizer, error) { return backend, nil }
			t.Cleanup(func() { newRealizerFn = old })
			mf := nixManifest()
			mf.HomeManager = &manifest.HomeManagerConfig{Flake: "path:/fixture#profile"}
			mf.Restore = []manifest.RestoreEntry{{Type: "copy", Source: "payload", Target: target}}
			data, err := json.Marshal(mf)
			if err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(root, "manifest.json")
			if err := os.WriteFile(manifestPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			var raw interface{}
			var applyErr *envelope.Error
			output := captureStderr(t, func() {
				raw, applyErr = RunApply(ApplyFlags{Manifest: manifestPath, EnableRestore: true, NoBootstrap: true, DryRun: preview, Events: "jsonl"})
			})
			data, readErr := os.ReadFile(target)
			result, ok := raw.(*ApplyResult)
			if !ok {
				t.Fatalf("unexpected result=%+v error=%+v", raw, applyErr)
			}
			promised := strings.Contains(output, "would_configure")
			t.Logf("preview=%t error=%+v activated=%t directObserved=%t final=%q would_configure=%t readErr=%v", preview, applyErr, result.HomeManager.Activated, directObserved, data, promised, readErr)
			if applyErr == nil || applyErr.Code != envelope.ErrRestoreFailed || !strings.Contains(output, "target_collision") || result.HomeManager.Activated || directObserved || string(data) != "prior" || promised {
				t.Fatal("RunApply missed known overlap through ancestor symlink")
			}
		})
	}
}
