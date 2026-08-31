// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmregistry

import (
	"bytes"
	"encoding/json"
	"path/filepath"
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
			}},
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
	registry.Entries = append(registry.Entries, Entry{
		ID:          "starship",
		Program:     "starship",
		DisplayName: "Starship",
		PackageID:   "starship",
		Disposition: FileRoundTrip,
		Codec:       "bounded-regular-file-v1",
		Probe: &Probe{Values: map[string]any{
			"programs.starship.settings": map[string]any{"add_newline": false},
		}},
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
