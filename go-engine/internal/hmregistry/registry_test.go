// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmregistry

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

func testInputs(t *testing.T) releaseinputs.Inputs {
	t.Helper()
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	return inputs
}

func validRegistry(t *testing.T) Registry {
	t.Helper()
	inputs := testInputs(t)
	registry := Registry{
		SchemaVersion:  SchemaVersion,
		Inputs:         InputIdentity{NixpkgsRevision: inputs.Nixpkgs.Revision, HomeManagerRevision: inputs.HomeManager.Revision},
		DocsJSONSHA256: strings.Repeat("a", 64),
		Programs:       []string{"ripgrep"},
		Entries: []Entry{{
			ID:          "ripgrep",
			Program:     "ripgrep",
			DisplayName: "ripgrep",
			PackageID:   "ripgrep",
			ModuleID:    "apps.ripgrep",
			Disposition: FileRoundTrip,
			Codec:       "bounded-regular-file-v1",
			Probe: &Probe{Values: map[string]any{
				"programs.ripgrep.arguments": []any{"--hidden"},
			}, Targets: []string{"${xdg.config}/ripgrep/ripgreprc"}},
			Options: []Option{{
				Name:         "programs.ripgrep.arguments",
				Type:         "list of string",
				Declarations: []string{"modules/programs/ripgrep.nix"},
			}},
			Targets: []Target{{Coordinate: "${xdg.config}/ripgrep/ripgreprc"}},
			SourceHashes: map[string]string{
				"modules/programs/ripgrep.nix": strings.Repeat("b", 64),
			},
		}},
	}
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

func marshalRegistry(t *testing.T, registry Registry) []byte {
	t.Helper()
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseAcceptsFrozenReviewedRegistry(t *testing.T) {
	registry := validRegistry(t)
	parsed, err := Parse(marshalRegistry(t, registry), testInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Entries) != 1 || parsed.Entries[0].ID != "ripgrep" {
		t.Fatalf("entries = %+v", parsed.Entries)
	}
	if parsed.Entries[0].ReviewFingerprint == "" {
		t.Fatal("review fingerprint was not retained")
	}
}

func TestReleaseRegistryLoadsAgainstEmbeddedInputs(t *testing.T) {
	registry, err := Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) == 0 {
		t.Fatal("release Home Manager registry is empty")
	}
}

func TestReleaseRegistryIncludesReviewedLinuxBeyondWindowsAdapters(t *testing.T) {
	registry, err := Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"btop":       "${xdg.config}/btop/btop.conf",
		"fastfetch":  "${xdg.config}/fastfetch/config.jsonc",
		"keepassxc":  "${xdg.config}/keepassxc/keepassxc.ini",
		"lazydocker": "${xdg.config}/lazydocker/config.yml",
		"poetry":     "${xdg.config}/pypoetry/config.toml",
	}
	for _, entry := range registry.Entries {
		target, ok := want[entry.ID]
		if !ok {
			continue
		}
		if entry.Disposition != FileRoundTrip || len(entry.Targets) != 1 || entry.Targets[0].Coordinate != target {
			t.Fatalf("adapter %s = %+v", entry.ID, entry)
		}
		delete(want, entry.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing reviewed adapters: %+v", want)
	}
}

func TestReleaseRegistryUsesCuratedGitCodecForCommonLiveLayouts(t *testing.T) {
	registry, err := Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range registry.Entries {
		if entry.ID != "git" {
			continue
		}
		if entry.Disposition != CuratedCodec || entry.Codec != "git-config-safe-v1" {
			t.Fatalf("Git adapter = %+v", entry)
		}
		wantTargets := []string{
			"${home}/.gitattributes",
			"${home}/.gitconfig",
			"${xdg.config}/git/attributes",
			"${xdg.config}/git/config",
			"${xdg.config}/git/ignore",
		}
		for index, target := range entry.Targets {
			if index >= len(wantTargets) || target.Coordinate != wantTargets[index] {
				t.Fatalf("Git targets = %+v, want %v", entry.Targets, wantTargets)
			}
		}
		if len(entry.Targets) != len(wantTargets) {
			t.Fatalf("Git targets = %+v, want %v", entry.Targets, wantTargets)
		}
		return
	}
	t.Fatal("release registry omitted Git adapter")
}

func TestReleaseRegistryFreezesBroadHomeManagerProgramIndex(t *testing.T) {
	registry, err := Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Programs) < 300 {
		t.Fatalf("Home Manager program index has only %d programs", len(registry.Programs))
	}
	for _, want := range []string{"btop", "git", "vscode"} {
		index := sort.SearchStrings(registry.Programs, want)
		if index == len(registry.Programs) || registry.Programs[index] != want {
			t.Fatalf("Home Manager program index omitted %q", want)
		}
	}
}

func TestParseRejectsUnknownFieldsAndWrongReleaseInputs(t *testing.T) {
	registry := validRegistry(t)
	data := marshalRegistry(t, registry)
	unknown := bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"surprise":true`), 1)
	if _, err := Parse(unknown, testInputs(t)); err == nil {
		t.Fatal("registry with an unknown field was accepted")
	}

	registry.Inputs.NixpkgsRevision = strings.Repeat("f", 40)
	if _, err := Parse(marshalRegistry(t, registry), testInputs(t)); err == nil {
		t.Fatal("registry pinned to another release was accepted")
	}
}

func TestDecodeAllowsStaleReviewOnlyForHarvesterRefresh(t *testing.T) {
	registry := validRegistry(t)
	registry.Entries[0].Options[0].Type = "changed upstream type"
	data := marshalRegistry(t, registry)
	if _, err := Decode(data); err != nil {
		t.Fatalf("Decode rejected refreshable stale metadata: %v", err)
	}
	if _, err := Parse(data, testInputs(t)); err == nil {
		t.Fatal("Parse accepted stale reviewed metadata")
	}
}

func TestValidateRejectsUnsafeTargetAndStaleReview(t *testing.T) {
	registry := validRegistry(t)
	registry.Entries[0].Targets[0].Coordinate = "${xdg.config}/../.ssh/config"
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("unsafe target error = %v", err)
	}

	registry = validRegistry(t)
	registry.Entries[0].Options[0].Type = "string"
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "review is stale") {
		t.Fatalf("stale review error = %v", err)
	}
}

func TestValidateRejectsCodecThatHasNoRuntimeImplementation(t *testing.T) {
	registry := validRegistry(t)
	registry.Entries[0].Codec = "wishful-parser-v1"
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("unsupported codec error = %v", err)
	}
}

func TestValidateAcceptsCuratedGitCodecWithReviewedLiveAliases(t *testing.T) {
	registry := validRegistry(t)
	entry := &registry.Entries[0]
	entry.ID = "git"
	entry.Program = "git"
	entry.DisplayName = "Git"
	entry.PackageID = "git"
	entry.ModuleID = "apps.git"
	entry.Disposition = CuratedCodec
	entry.Codec = "git-config-safe-v1"
	entry.Options = []Option{{
		Name: "programs.git.settings", Type: "Git settings",
		Declarations: []string{"modules/programs/git.nix"},
	}}
	entry.Probe = &Probe{
		Values:  map[string]any{"programs.git.settings": map[string]any{"user": map[string]any{"name": "Endstate Probe"}}},
		Targets: []string{"${xdg.config}/git/config"},
	}
	entry.Targets = []Target{
		{Coordinate: "${home}/.gitconfig", Optional: true},
		{Coordinate: "${xdg.config}/git/config", Optional: true},
	}
	entry.SourceHashes = map[string]string{"modules/programs/git.nix": strings.Repeat("b", 64)}
	registry.Programs = []string{"git"}
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err != nil {
		t.Fatalf("curated Git registry was rejected: %v", err)
	}
}

func TestValidateRejectsCuratedCodecTargetOutsideItsImplementation(t *testing.T) {
	registry := validRegistry(t)
	entry := &registry.Entries[0]
	entry.Disposition = CuratedCodec
	entry.Codec = "git-config-safe-v1"
	entry.Targets = []Target{{Coordinate: "${xdg.config}/other/config"}}
	entry.Probe.Targets = []string{"${xdg.config}/other/config"}
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "does not own") {
		t.Fatalf("unowned codec target error = %v", err)
	}
}

func TestValidateRequiresReviewedProbeForSupportedAdapter(t *testing.T) {
	registry := validRegistry(t)
	registry.Entries[0].Probe = nil
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("missing probe error = %v", err)
	}

	registry = validRegistry(t)
	registry.Entries[0].Probe.Values["programs.ripgrep.enable"] = true
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "not a harvested option") {
		t.Fatalf("unknown probe option error = %v", err)
	}
}

func TestValidateBindsObservedProbeTargetsToRuntimeTargets(t *testing.T) {
	registry := validRegistry(t)
	registry.Entries[0].Probe.Targets = []string{"${xdg.config}/ripgrep/changed"}
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "probe targets") {
		t.Fatalf("probe target mismatch error = %v", err)
	}
}

func TestValidateRejectsProbeOnExcludedAdapter(t *testing.T) {
	registry := validRegistry(t)
	entry := &registry.Entries[0]
	entry.Disposition = Excluded
	entry.Codec = ""
	entry.ExclusionReason = "unsafe executable configuration"
	entry.Targets = nil
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "excluded disposition cannot declare a probe") {
		t.Fatalf("excluded probe error = %v", err)
	}
}

func TestValidateRejectsDuplicateTargetOwnership(t *testing.T) {
	registry := validRegistry(t)
	registry.Programs = []string{"ripgrep", "starship"}
	registry.Entries = append(registry.Entries, Entry{
		ID:          "starship",
		Program:     "starship",
		DisplayName: "Starship",
		PackageID:   "starship",
		Disposition: FileRoundTrip,
		Codec:       "bounded-regular-file-v1",
		Probe: &Probe{Values: map[string]any{
			"programs.starship.settings": map[string]any{"add_newline": false},
		}, Targets: []string{"${xdg.config}/ripgrep/ripgreprc"}},
		Options: []Option{{
			Name:         "programs.starship.settings",
			Type:         "TOML value",
			Declarations: []string{"modules/programs/starship.nix"},
		}},
		Targets: []Target{{Coordinate: "${xdg.config}/ripgrep/ripgreprc"}},
		SourceHashes: map[string]string{
			"modules/programs/starship.nix": strings.Repeat("c", 64),
		},
	})
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&registry, testInputs(t)); err == nil || !strings.Contains(err.Error(), "also owned") {
		t.Fatalf("duplicate ownership error = %v", err)
	}
}
