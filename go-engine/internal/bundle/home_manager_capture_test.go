// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func TestCreateCaptureBundleStagesReviewedHomeManagerFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "live", "ripgreprc")
	writeCaptureFile(t, source, []byte("--hidden\n"))
	request := testCaptureBundleRequest(t, dir, nil, nil)
	request.HomeManagerFiles = []HomeManagerFileCapturePlan{{
		CandidateID: "apps.ripgrep", Codec: "bounded-regular-file-v1", Target: "${xdg.config}/ripgrep/ripgreprc",
		Source: source, ObservedSize: int64(len("--hidden\n")),
	}}

	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.HomeManagerFilesIncluded != 1 {
		t.Fatalf("included files = %d", result.HomeManagerFilesIncluded)
	}
	manifestPath := extractCaptureBundle(t, request.OutputPath)
	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HomeManager == nil || loaded.HomeManager.Settings == nil {
		t.Fatalf("homeManager = %+v", loaded.HomeManager)
	}
	reference := loaded.HomeManager.Settings.Files["${xdg.config}/ripgrep/ripgreprc"]
	if !strings.HasPrefix(reference, "./configs/home-manager/ripgrep/") {
		t.Fatalf("portable source reference = %q", reference)
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(strings.TrimPrefix(reference, "./"))))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "--hidden\n" {
		t.Fatalf("captured content = %q", content)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifestBytes), source) {
		t.Fatalf("live host path leaked into manifest: %s", manifestBytes)
	}
}

func TestCreateCaptureBundleRejectsHomeManagerContentChangedAfterDiscovery(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "settings")
	original := []byte("one\n")
	writeCaptureFile(t, source, original)
	sum := sha256.Sum256(original)
	request := testCaptureBundleRequest(t, dir, nil, nil)
	request.HomeManagerFiles = []HomeManagerFileCapturePlan{{
		CandidateID: "apps.ripgrep", Codec: "bounded-regular-file-v1",
		Target: "${xdg.config}/ripgrep/ripgreprc", Source: source,
		ObservedSize: int64(len(original)), ObservedSHA256: hex.EncodeToString(sum[:]),
	}}
	// Same-size replacement defeats a size-only observation and must still be
	// rejected rather than publishing state different from the discovery result.
	if err := os.WriteFile(source, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCaptureBundle(request); err == nil || !strings.Contains(err.Error(), "changed after discovery") {
		t.Fatalf("changed settings source error = %v", err)
	}
}

func TestCreateCaptureBundleAppliesReviewedGitCodecBeforePackaging(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, ".gitconfig")
	content := []byte("[user]\nname = Example\n[credential]\npassword = literal-secret\n")
	writeCaptureFile(t, source, content)
	request := testCaptureBundleRequest(t, dir, nil, nil)
	request.HomeManagerFiles = []HomeManagerFileCapturePlan{{
		CandidateID: "apps.git", Target: "${home}/.gitconfig", Source: source,
		ObservedSize: int64(len(content)), Codec: "git-config-safe-v1",
	}}

	if _, err := CreateCaptureBundle(request); err != nil {
		t.Fatal(err)
	}
	manifestPath := extractCaptureBundle(t, request.OutputPath)
	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	reference := loaded.HomeManager.Settings.Files["${home}/.gitconfig"]
	staged, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(strings.TrimPrefix(reference, "./"))))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(staged), "name = Example") || strings.Contains(string(staged), "literal-secret") || strings.Contains(string(staged), "credential") {
		t.Fatalf("staged Git config = %q", staged)
	}
}

func TestCreateCaptureBundleRejectsLinkedOrDuplicateHomeManagerSources(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, string, *CaptureBundleRequest){
		"missing codec": func(t *testing.T, dir string, request *CaptureBundleRequest) {
			source := filepath.Join(dir, "settings")
			writeCaptureFile(t, source, []byte("settings"))
			request.HomeManagerFiles = []HomeManagerFileCapturePlan{{CandidateID: "apps.one", Target: "${home}/.one", Source: source}}
		},
		"linked source": func(t *testing.T, dir string, request *CaptureBundleRequest) {
			realSource := filepath.Join(dir, "real")
			writeCaptureFile(t, realSource, []byte("settings"))
			link := filepath.Join(dir, "link")
			if err := os.Symlink(realSource, link); err != nil {
				t.Fatal(err)
			}
			request.HomeManagerFiles = []HomeManagerFileCapturePlan{{CandidateID: "apps.one", Codec: "bounded-regular-file-v1", Target: "${home}/.one", Source: link}}
		},
		"duplicate target": func(t *testing.T, dir string, request *CaptureBundleRequest) {
			one, two := filepath.Join(dir, "one"), filepath.Join(dir, "two")
			writeCaptureFile(t, one, []byte("one"))
			writeCaptureFile(t, two, []byte("two"))
			request.HomeManagerFiles = []HomeManagerFileCapturePlan{
				{CandidateID: "apps.one", Codec: "bounded-regular-file-v1", Target: "${home}/.shared", Source: one},
				{CandidateID: "apps.two", Codec: "bounded-regular-file-v1", Target: "${home}/.shared", Source: two},
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			request := testCaptureBundleRequest(t, dir, nil, nil)
			mutate(t, dir, &request)
			if _, err := CreateCaptureBundle(request); err == nil {
				t.Fatal("unsafe Home Manager capture plan was accepted")
			}
		})
	}
}

func TestCreateCaptureBundlePreservesDeclaredExternalHomeManagerOwner(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "input.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"capture","apps":[],"homeManager":{"flake":"github:owner/dots#me"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "settings")
	writeCaptureFile(t, source, []byte("live"))
	request := CaptureBundleRequest{
		ManifestPath: manifestPath, OutputPath: filepath.Join(dir, "capture.zip"),
		HomeManagerFiles: []HomeManagerFileCapturePlan{{
			CandidateID: "apps.ripgrep", Codec: "bounded-regular-file-v1", Target: "${home}/.ripgreprc", Source: source,
		}},
	}
	result, err := CreateCaptureBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.HomeManagerFilesIncluded != 0 || len(result.CaptureWarnings) != 1 {
		t.Fatalf("result = %+v", result)
	}
	loaded, _ := loadCaptureBundle(t, request.OutputPath)
	if loaded.HomeManager == nil || loaded.HomeManager.Flake != "github:owner/dots#me" || loaded.HomeManager.Settings != nil {
		t.Fatalf("external owner was changed: %+v", loaded.HomeManager)
	}
}
