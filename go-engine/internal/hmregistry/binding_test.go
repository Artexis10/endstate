// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmregistry

import (
	"encoding/json"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

// A coherent reviewed file mapping is structural evidence, not semantic policy.
func TestJoinBindsReviewedGenerationWithoutGrantingByteCodecActions(t *testing.T) {
	registry, mod, packages := bindingFixture(t)
	authorities := Join(registry, map[string]*modules.Module{mod.ID: mod}, packages)
	if len(authorities) != 1 || !authorities[0].Bound || authorities[0].ConfigSetID != "default" || authorities[0].GenerationID != "g1" || len(authorities[0].Actions) != 0 || authorities[0].Reason != "settings_content_policy_unavailable" {
		t.Fatalf("authority = %+v", authorities)
	}
}

func TestJoinRejectsContradictoryAdapter(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Registry, *modules.Module, []*packagecatalog.Entry)
		reason string
	}{
		{"stale review", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) {
			v := m.Platforms["linux"]
			v.HomeManagerAdapter.ReviewFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			m.Platforms["linux"] = v
		}, "settings_adapter_review_stale"},
		{"missing Linux", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) { delete(m.Platforms, "linux") }, "settings_linux_variant_missing"},
		{"missing generation", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) {
			v := m.Platforms["linux"]
			v.HomeManagerAdapter.GenerationID = "g9"
			m.Platforms["linux"] = v
		}, "settings_generation_missing"},
		{"package mismatch", func(_ *Registry, _ *modules.Module, p []*packagecatalog.Entry) { p[0].ConfigModule = "apps.other" }, "settings_package_mismatch"},
		{"swapped payloads", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) {
			v := m.Platforms["linux"]
			v.Capture.Files[0].Dest, v.Capture.Files[1].Dest = v.Capture.Files[1].Dest, v.Capture.Files[0].Dest
			m.Platforms["linux"] = v
		}, "settings_target_mismatch"},
		{"optional mismatch", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) {
			v := m.Platforms["linux"]
			v.Capture.Files[0].Optional = true
			m.Platforms["linux"] = v
		}, "settings_target_mismatch"},
		{"secret target", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) {
			v := m.Platforms["linux"]
			v.Secrets = &modules.SecretsDef{Files: []string{"${home}/.ripgreprc"}, Restorer: "warn-only"}
			m.Platforms["linux"] = v
		}, "settings_secret_boundary"},
		{"capture exclusion", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) {
			v := m.Platforms["linux"]
			v.Capture.ExcludeGlobs = []string{"**/ripgreprc"}
			m.Platforms["linux"] = v
		}, "settings_target_excluded"},
		{"restore exclusion", func(_ *Registry, m *modules.Module, _ []*packagecatalog.Entry) {
			v := m.Platforms["linux"]
			v.Restore[1].Exclude = []string{"**/ripgreprc"}
			m.Platforms["linux"] = v
		}, "settings_target_excluded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, mod, packages := bindingFixture(t)
			test.mutate(registry, mod, packages)
			got := Join(registry, map[string]*modules.Module{mod.ID: mod}, packages)
			if len(got) != 1 || got[0].Bound || len(got[0].Actions) != 0 || got[0].Reason != test.reason {
				t.Fatalf("authority = %+v, want %s", got, test.reason)
			}
		})
	}
}

func bindingFixture(t *testing.T) (*Registry, *modules.Module, []*packagecatalog.Entry) {
	t.Helper()
	registry := validRegistry(t)
	registry.Entries[0].Targets = []Target{{Coordinate: "${home}/.ripgreprc"}, {Coordinate: "${xdg.config}/ripgrep/ripgreprc"}}
	registry.Entries[0].Probe.Targets = []string{"${home}/.ripgreprc", "${xdg.config}/ripgrep/ripgreprc"}
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	parsedRegistry, err := Parse(marshalRegistry(t, registry), testInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	mod := modules.Module{ModuleSchemaVersion: 3, ID: "apps.ripgrep", DisplayName: "ripgrep", Sensitivity: "low", Platforms: map[string]modules.PlatformVariant{"linux": {
		Realization: "endstate-restore", Matches: modules.MatchCriteria{PathExists: []string{"${home}/.ripgreprc"}},
		HomeManagerAdapter: &modules.HomeManagerAdapterBinding{ID: "ripgrep", ReviewFingerprint: registry.Entries[0].ReviewFingerprint, ConfigSetID: "default", GenerationID: "g1"},
		Capture:            &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "${home}/.ripgreprc", Dest: "apps/ripgrep/home"}, {Source: "${xdg.config}/ripgrep/ripgreprc", Dest: "apps/ripgrep/ripgreprc"}}},
		Restore:            []modules.RestoreDef{{Type: "copy", Source: "./payload/apps/ripgrep/home", Target: "${home}/.ripgreprc", Backup: true}, {Type: "copy", Source: "./payload/apps/ripgrep/ripgreprc", Target: "${xdg.config}/ripgrep/ripgreprc", Backup: true}},
	}}}
	body, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := modules.ParseAndValidateModuleJSON(body, "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := packagecatalog.ParseEntry([]byte(`{"schemaVersion":1,"id":"ripgrep","displayName":"ripgrep","nix":{"attribute":"ripgrep"},"aliases":{"executable":["rg"]},"configModule":"apps.ripgrep"}`), "ripgrep.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	return parsedRegistry, parsed, []*packagecatalog.Entry{pkg}
}

func TestJoinKeepsIndependentBindingAndExplicitGeneration(t *testing.T) {
	registry, mod, packages := bindingFixture(t)
	variant := mod.Platforms["linux"]
	variant.HomeManagerAdapter.ConfigSetID = "preferences"
	variant.HomeManagerAdapter.GenerationID = "g2"
	variant.Config = &modules.ConfigDef{InstanceDetectors: []modules.InstanceDetectorDef{{ID: "user", Type: "path", Glob: "${home}"}}, Sets: []modules.ConfigSetDef{{ID: "preferences", Generations: []modules.GenerationDef{{ID: "g2", Order: 2, Capture: variant.Capture, Restore: []modules.RestoreDef{{Type: "copy", Source: "apps/ripgrep/home", Target: "${home}/.ripgreprc", Backup: true}, {Type: "copy", Source: "apps/ripgrep/ripgreprc", Target: "${xdg.config}/ripgrep/ripgreprc", Backup: true}}}}}}}
	variant.Capture = nil
	variant.Restore = nil
	mod.Platforms["linux"] = variant
	body, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := modules.ParseAndValidateModuleJSON(body, "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	registry.Entries = append(registry.Entries, Entry{ID: "unbound", ModuleID: "apps.unbound", Disposition: FileRoundTrip})
	authorities := Join(registry, map[string]*modules.Module{parsed.ID: parsed}, packages)
	if len(authorities) != 2 || !authorities[0].Bound || authorities[0].GenerationID != "g2" || authorities[1].Bound || len(authorities[1].Actions) != 0 {
		t.Fatalf("independent adapter affected explicit generation: %+v", authorities)
	}
}

func TestJoinMissingRegistryCannotBeBypassedByModuleBinding(t *testing.T) {
	_, mod, packages := bindingFixture(t)
	got := Join(nil, map[string]*modules.Module{mod.ID: mod}, packages)
	if len(got) != 1 || got[0].ModuleID != mod.ID || got[0].Reason != "settings_adapter_missing" || got[0].Bound || len(got[0].Actions) != 0 {
		t.Fatalf("missing registry authority = %+v", got)
	}
}

func typedBindingFixture(t *testing.T, realization, packageType string) (*Registry, *modules.Module, []*packagecatalog.Entry) {
	t.Helper()
	registry := typedPoetryRegistry(t)
	registry.Entries[0].Options[0].Type = packageType
	if err := AcceptReviews(&registry); err != nil {
		t.Fatal(err)
	}
	parsedRegistry, err := Parse(marshalRegistry(t, registry), testInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	capture := &modules.CaptureDef{Files: []modules.CaptureFile{{Source: poetryTarget, Dest: "apps/poetry/config.toml", Optional: true}}}
	restore := []modules.RestoreDef{{Type: "copy", Source: "apps/poetry/config.toml", Target: poetryTarget, Backup: true, Optional: true}}
	mod := modules.Module{ModuleSchemaVersion: 3, ID: "apps.poetry", DisplayName: "Poetry", Sensitivity: "medium", Platforms: map[string]modules.PlatformVariant{"linux": {
		Realization: realization, Matches: modules.MatchCriteria{PathExists: []string{poetryTarget}},
		HomeManagerAdapter: &modules.HomeManagerAdapterBinding{ID: "poetry", ReviewFingerprint: registry.Entries[0].ReviewFingerprint, ConfigSetID: "default", GenerationID: "g1"},
		Config: &modules.ConfigDef{InstanceDetectors: []modules.InstanceDetectorDef{{ID: "default", Type: "path", Glob: "${home}"}}, Sets: []modules.ConfigSetDef{{ID: "default", Generations: []modules.GenerationDef{{
			ID: "g1", Order: 1, Capture: capture, Restore: restore,
			Validate: []modules.ValidationDef{{Type: "settings-codec", Path: "apps/poetry/config.toml", Codec: "poetry-settings-v1", Optional: true}},
		}}}}},
	}}}
	body, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := modules.ParseAndValidateModuleJSON(body, "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := packagecatalog.ParseEntry([]byte(`{"schemaVersion":1,"id":"poetry","displayName":"Poetry","nix":{"attribute":"poetry"},"aliases":{"executable":["poetry"]},"configModule":"apps.poetry"}`), "poetry.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	return parsedRegistry, parsed, []*packagecatalog.Entry{pkg}
}

func TestJoinGrantsTypedActionsOnlyToTheHomeManagerStrategy(t *testing.T) {
	registry, mod, packages := typedBindingFixture(t, "home-manager", "null or package")
	got := Join(registry, map[string]*modules.Module{mod.ID: mod}, packages)
	if len(got) != 1 || !got[0].Bound || got[0].Reason != "" || !got[0].Supports(Restore) || got[0].Disposition != TypedRoundTrip || got[0].Codec != "poetry-settings-v1" || got[0].Program != "poetry" || got[0].Option != "programs.poetry.settings" {
		t.Fatalf("home-manager typed authority = %+v", got)
	}

	registry, mod, packages = typedBindingFixture(t, "endstate-restore", "null or package")
	got = Join(registry, map[string]*modules.Module{mod.ID: mod}, packages)
	if len(got) != 1 || len(got[0].Actions) != 0 || got[0].Reason != "settings_strategy_unavailable" {
		t.Fatalf("a typed value on endstate-restore must not be copied as a file: %+v", got)
	}
}

func TestJoinReportsNullPackageFromTheHarvestedOptionType(t *testing.T) {
	for _, test := range []struct {
		packageType string
		want        bool
	}{{"null or package", true}, {"package", false}} {
		registry, mod, packages := typedBindingFixture(t, "home-manager", test.packageType)
		got := Join(registry, map[string]*modules.Module{mod.ID: mod}, packages)
		if len(got) != 1 || !got[0].Supports(Restore) || got[0].NullPackage != test.want {
			t.Fatalf("package option type %q: authority = %+v, want NullPackage %v", test.packageType, got, test.want)
		}
	}
}
