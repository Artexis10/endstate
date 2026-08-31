// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

func TestRefreshBindsDocsDeclarationsSourcesAndReview(t *testing.T) {
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot := t.TempDir()
	declaration := filepath.Join(sourceRoot, "modules", "programs", "ripgrep.nix")
	if err := os.MkdirAll(filepath.Dir(declaration), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(declaration, []byte("{ config = {}; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	docs := []byte(`{"programs.ripgrep.arguments":{"type":"list of string","declarations":[{"name":"<home-manager/modules/programs/ripgrep.nix>"}]}}`)
	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "ripgrep", Program: "ripgrep", DisplayName: "ripgrep",
		PackageID: "ripgrep", ModuleID: "apps.ripgrep",
		Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1",
		Probe: &hmregistry.Probe{Values: map[string]any{
			"programs.ripgrep.arguments": []any{"--hidden"},
		}},
		Options:           []hmregistry.Option{{Name: "programs.ripgrep.arguments", Type: "stale", Declarations: []string{"modules/stale.nix"}}},
		Targets:           []hmregistry.Target{{Coordinate: "${xdg.config}/ripgrep/ripgreprc"}},
		SourceHashes:      map[string]string{"modules/stale.nix": strings.Repeat("0", 64)},
		ReviewFingerprint: strings.Repeat("0", 64),
	}}}

	prober := fakeTargetProber{targets: map[string][]string{
		"ripgrep": {"/home/endstate-probe/.config/ripgrep/ripgreprc"},
	}}
	if err := Refresh(registry, docs, sourceRoot, inputs, prober); err != nil {
		t.Fatal(err)
	}
	if registry.Inputs.NixpkgsRevision != inputs.Nixpkgs.Revision || registry.DocsJSONSHA256 == "" {
		t.Fatalf("release identity = %+v docs=%q", registry.Inputs, registry.DocsJSONSHA256)
	}
	option := registry.Entries[0].Options[0]
	if option.Type != "list of string" || len(option.Declarations) != 1 || option.Declarations[0] != "modules/programs/ripgrep.nix" {
		t.Fatalf("option = %+v", option)
	}
	if hash := registry.Entries[0].SourceHashes[option.Declarations[0]]; len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
		t.Fatalf("source hash = %q", hash)
	}
	if registry.Entries[0].ReviewFingerprint == strings.Repeat("0", 64) {
		t.Fatal("stale review fingerprint was retained")
	}
	if err := hmregistry.Validate(registry, inputs); err != nil {
		t.Fatalf("refreshed registry did not validate: %v", err)
	}
}

func TestRefreshRejectsOptionMissingFromPinnedDocs(t *testing.T) {
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "missing", Program: "missing", DisplayName: "Missing",
		Disposition: hmregistry.Excluded, ExclusionReason: "not portable",
		Options: []hmregistry.Option{{Name: "programs.missing.enable"}},
	}}}
	if err := Refresh(registry, []byte(`{}`), t.TempDir(), inputs, fakeTargetProber{}); err == nil {
		t.Fatal("missing pinned option was accepted")
	}
}

type fakeTargetProber struct {
	targets map[string][]string
	err     error
}

func (p fakeTargetProber) ProbeTargets(program string, _ map[string]any) ([]string, error) {
	if p.err != nil {
		return nil, p.err
	}
	return append([]string(nil), p.targets[program]...), nil
}

func TestRefreshDerivesTargetsFromPureProbeOutput(t *testing.T) {
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot := t.TempDir()
	declaration := filepath.Join(sourceRoot, "modules", "programs", "helix.nix")
	if err := os.MkdirAll(filepath.Dir(declaration), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(declaration, []byte("{ config = {}; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	docs := []byte(`{
  "programs.helix.ignores":{"type":"list of string","declarations":[{"name":"<home-manager/modules/programs/helix.nix>"}]},
  "programs.helix.settings":{"type":"TOML value","declarations":[{"name":"<home-manager/modules/programs/helix.nix>"}]}
}`)
	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "helix", Program: "helix", DisplayName: "Helix",
		Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1",
		Probe: &hmregistry.Probe{Values: map[string]any{
			"programs.helix.ignores":  []any{".git"},
			"programs.helix.settings": map[string]any{"theme": "base16"},
		}},
		Options: []hmregistry.Option{
			{Name: "programs.helix.settings"},
			{Name: "programs.helix.ignores"},
		},
		Targets: []hmregistry.Target{{Coordinate: "${xdg.config}/stale", Optional: true}},
	}}}
	prober := fakeTargetProber{targets: map[string][]string{
		"helix": {
			"/home/endstate-probe/.config/helix/ignore",
			"/home/endstate-probe/.config/helix/config.toml",
		},
	}}

	if err := Refresh(registry, docs, sourceRoot, inputs, prober); err != nil {
		t.Fatal(err)
	}
	want := []hmregistry.Target{
		{Coordinate: "${xdg.config}/helix/config.toml", Optional: true},
		{Coordinate: "${xdg.config}/helix/ignore", Optional: true},
	}
	if got := registry.Entries[0].Targets; !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}
}

func TestNormalizeProbeTargetRejectsEscape(t *testing.T) {
	if _, err := normalizeProbeTargets([]string{"/etc/passwd"}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("escape error = %v", err)
	}
}
