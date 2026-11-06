// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type captureHomeOutputRealizer struct {
	*fakeRealizer
	output string
}

func (r *captureHomeOutputRealizer) ObserveHome() (realizer.HomeObservation, error) {
	return realizer.HomeObservation{Present: true, Generation: 3, ActivationOutput: r.output}, nil
}

func TestLinuxCaptureProjectsMatchingHistoricalSettingsBeforePublication(t *testing.T) {
	for _, test := range []struct {
		name, only    string
		update, share bool
	}{
		{name: "selected typed declaration", only: "git"},
		{name: "selected registry association", only: "github-cli"},
		{name: "update retains apps without their settings", only: "git", update: true},
		{name: "share omits unprocessed inline declarations", only: "git", share: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repo, err := filepath.Abs("../../..")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("ENDSTATE_ROOT", repo)
			oldRoot, oldGOOS := resolveRepoRootFn, captureGOOSFn
			resolveRepoRootFn = func() string { return repo }
			captureGOOSFn = func() string { return "linux" }
			t.Cleanup(func() { resolveRepoRootFn, captureGOOSFn = oldRoot, oldGOOS })
			settings := &manifest.HomeManagerSettings{
				Git:      &manifest.GitSettings{DefaultBranch: "main"},
				Bat:      &manifest.BatSettings{Enable: true},
				Programs: map[string]any{"gh": map[string]any{"enable": true}},
				Files:    map[string]string{"${home}/.unselected": filepath.Join(root, "unopened-source")},
			}
			prior := &manifest.Manifest{Version: 1, Name: "prior", Apps: []manifest.App{
				{ID: "bat", Refs: map[string]string{"linux": "github:example/packages#bat"}},
			}, HomeManager: &manifest.HomeManagerConfig{Settings: settings}}
			source, err := json.Marshal(prior)
			if err != nil {
				t.Fatal(err)
			}
			sourcePath := filepath.Join(root, "prior.jsonc")
			if err := os.WriteFile(sourcePath, source, 0600); err != nil {
				t.Fatal(err)
			}
			home := &provision.HomeGenRef{
				Kind: "settings", Generated: true, Generation: 3,
				SourceManifest: sourcePath, SettingsDigest: settingsDigest(settings),
				ActivationOutput: "/nix/store/recorded-home-output",
				Secrets:          []manifest.HomeManagerSecret{{Name: "~/.credential", Path: filepath.Join(root, "credential-reference")}},
			}
			backend := &captureHomeOutputRealizer{fakeRealizer: &fakeRealizer{currentSet: nixSet("git", "github-cli", "bat")}, output: home.ActivationOutput}
			flags := CaptureFlags{Out: filepath.Join(root, "capture.endstate"), Only: test.only, Share: test.share, Update: test.update}
			if test.update {
				flags.Manifest = sourcePath
			}
			withFakeRealizer(backend.fakeRealizer, func() {
				newRealizerFn = func() (realizer.Realizer, error) { return backend, nil }
				withFakeGenerations([]*provision.Generation{{HomeManager: home}}, nil, func() {
					emptyCatalog(func() {
						resolveRepoRootFn = func() string { return repo }
						if _, eerr := RunCapture(flags); eerr != nil {
							t.Fatalf("capture: %+v", eerr)
						}
					})
				})
			})
			mf := readCapturedManifest(t, flags.Out)
			if test.share {
				if mf.HomeManager != nil {
					t.Fatal("share included historical settings outside its redaction pipeline")
				}
			} else {
				if mf.HomeManager == nil {
					t.Fatal("ordinary discovery lost matching Home Manager output provenance")
				}
				var got manifest.HomeManagerSettings
				if err := json.Unmarshal(mf.HomeManager.Settings, &got); err != nil {
					t.Fatal(err)
				}
				if (got.Git != nil) != (test.only == "git") || (got.Programs["gh"] != nil) != (test.only == "github-cli") {
					t.Fatalf("selection did not follow registry program associations: %+v", got)
				}
				if got.Bat != nil || len(got.Files) != 0 || len(mf.HomeManager.Secrets) != 0 {
					t.Fatal("selected historical capture leaked unrelated settings, paths or secret references")
				}
			}
			if test.update && len(mf.Apps) != 2 {
				t.Fatalf("update did not preserve the unrelated package declaration: %+v", mf.Apps)
			}
			unchanged, err := os.ReadFile(sourcePath)
			if err != nil || !bytes.Equal(unchanged, source) {
				t.Fatal("capture changed the historical source manifest")
			}
		})
	}
}
