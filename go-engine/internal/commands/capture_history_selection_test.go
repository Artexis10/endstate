// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLinuxCaptureRetainedProgramCombinedSelection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux discovery workflow")
	}
	for _, history := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_history", true: "retained_history"}[history], func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "source-home")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".ripgreprc"), []byte("--hidden\n"), 0600); err != nil {
				t.Fatal(err)
			}
			repo, err := filepath.Abs("../../..")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("ENDSTATE_ROOT", repo)
			oldRoot, oldGOOS, oldRealizer, oldDiscovery, oldCatalog := resolveRepoRootFn, captureGOOSFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn
			t.Cleanup(func() {
				resolveRepoRootFn, captureGOOSFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn = oldRoot, oldGOOS, oldRealizer, oldDiscovery, oldCatalog
			})
			resolveRepoRootFn = func() string { return repo }
			captureGOOSFn = func() string { return "linux" }
			loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
			backend := &fakeRealizer{currentSet: nixSet("ripgrep")}
			newRealizerFn = func() (realizer.Realizer, error) { return backend, nil }
			discoverLinuxFn = func(ctx context.Context, request discovery.Request, r realizer.Realizer) (discovery.Result, error) {
				request.SelectedSources = []string{discovery.HomeManagerSettingsSource, "endstate-profile"}
				inputs, err := releaseinputs.Load()
				if err != nil {
					return discovery.Result{}, err
				}
				return discovery.DiscoverWithAdapters(ctx, request, discovery.RealizerInventoryAdapter{Realizer: r, DefaultInput: inputs.Nixpkgs.FlakeRef})
			}
			var gens []*provision.Generation
			if history {
				gens = []*provision.Generation{hmGenSettingsSecrets(&manifest.HomeManagerSettings{Ripgrep: &manifest.RipgrepSettings{Enable: true, Arguments: []string{"--hidden"}}}, nil, "", 3)}
			}
			withFakeGenerations(gens, nil, func() {
				raw, eerr := RunCapture(CaptureFlags{Out: filepath.Join(root, "capture.endstate"), Only: "ripgrep,apps.ripgrep"})
				if eerr != nil {
					t.Fatalf("selected installed package and its retained settings should capture successfully: %+v", eerr)
				}
				result := raw.(*CaptureResult)
				if len(result.AppsIncluded) != 1 || result.Discovery.Counts.Selected != 1 {
					t.Fatalf("selected candidate accounting changed: %+v", result)
				}
				captured := loadManifestFromCaptureZip(t, result.OutputPath)
				if history {
					if captured.HomeManager == nil || captured.HomeManager.Settings == nil || captured.HomeManager.Settings.Ripgrep == nil || len(captured.ConfigCaptures) != 0 {
						t.Fatal("retained program lost ownership or gained competing native payload")
					}
				} else if captured.HomeManager != nil || len(captured.ConfigCaptures) != 1 {
					t.Fatal("independent observed native configuration was not captured")
				}

			})
		})
	}
}
