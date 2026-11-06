// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

const validPlatformV3Module = `{
  "moduleSchemaVersion": 3,
  "id": "apps.ripgrep",
  "displayName": "ripgrep",
  "sensitivity": "low",
  "platforms": {
    "linux": {
      "realization": "home-manager",
      "matches": {"pathExists": ["${xdg.config}/ripgrep/ripgreprc"]},
      "capture": {"files": [{"source": "${xdg.config}/ripgrep/ripgreprc", "dest": "ripgreprc"}]},
      "restore": [{"type": "file-copy", "source": "ripgreprc", "target": "${xdg.config}/ripgrep/ripgreprc", "backup": true}],
      "verify": [{"type": "file-exists", "path": "${xdg.config}/ripgrep/ripgreprc"}]
    }
  }
}`

func TestParseAndProjectPlatformV3Module(t *testing.T) {
	mod, err := ParseAndValidateModuleJSON([]byte(validPlatformV3Module), "ripgrep.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	if mod.EffectiveSchemaVersion() != 3 || len(mod.Platforms) != 1 {
		t.Fatalf("parsed module = %+v", mod)
	}
	linux := FilterCatalogForPlatform(map[string]*Module{mod.ID: mod}, "linux")[mod.ID]
	if linux == nil || linux.EffectiveSchemaVersion() != 2 || linux.SourceSchemaVersion != 3 || linux.Platform != "linux" {
		t.Fatalf("Linux projection = %+v", linux)
	}
	if linux.Realization != "home-manager" || linux.Config == nil || linux.Config.Sets[0].Generations[0].Capture == nil || len(linux.Matches.PathExists) != 1 {
		t.Fatalf("Linux executable fields = %+v", linux)
	}
	if got := FilterCatalogForPlatform(map[string]*Module{mod.ID: mod}, "windows"); len(got) != 0 {
		t.Fatalf("unsupported Windows variant was executable: %+v", got)
	}
}

func TestPlatformV3RejectsMixedFlatDeclarationsAndMissingStrategy(t *testing.T) {
	tests := map[string]string{
		"mixed matches":         `{"moduleSchemaVersion":3,"id":"apps.x","displayName":"X","matches":{"pathExists":["x"]},"platforms":{"linux":{"realization":"home-manager","matches":{"pathExists":["x"]}}}}`,
		"missing strategy":      `{"moduleSchemaVersion":3,"id":"apps.x","displayName":"X","platforms":{"linux":{"matches":{"pathExists":["x"]},"capture":{"files":[{"source":"x","dest":"x"}]}}}}`,
		"unknown variant field": `{"moduleSchemaVersion":3,"id":"apps.x","displayName":"X","platforms":{"linux":{"realization":"home-manager","matches":{"pathExists":["x"]},"surprise":true}}}`,
		"shell path":            `{"moduleSchemaVersion":3,"id":"apps.x","displayName":"X","platforms":{"linux":{"realization":"home-manager","matches":{"pathExists":["${XDG_CONFIG_HOME:-$HOME/.config}/x"]}}}}`,
		"relative host path":    `{"moduleSchemaVersion":3,"id":"apps.x","displayName":"X","platforms":{"linux":{"realization":"home-manager","matches":{"pathExists":["config/x"]}}}}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAndValidateModuleJSON([]byte(data), "x.jsonc"); err == nil {
				t.Fatal("invalid schema-v3 module was accepted")
			}
		})
	}
}

// Whole dconf databases never travel: the desktop session's writer undoes the
// restore. The catalog must refuse a module that captures or restores the user
// database or a directory above it, and still accept its neighbours.
// The verdict belongs to the module: it holds on every host, whatever the
// validating process's home, XDG_CONFIG_HOME or OS.
func TestPlatformV3RejectsWholeDconfDatabases(t *testing.T) {
	for _, host := range []struct{ name, home, configHome string }{
		{"default layout", "/home/me", ""},
		{"custom config directory", "/home/me", "/home/me/work/cfg"},
		{"another user", "/srv/other", "/srv/other/.cfg"},
	} {
		t.Run(host.name, func(t *testing.T) {
			t.Setenv("HOME", host.home)
			t.Setenv("XDG_CONFIG_HOME", host.configHome)
			for _, test := range []struct {
				path     string
				rejected bool
			}{
				{"${xdg.config}/dconf/user", true},
				{"${xdg.config}/dconf", true},
				{"${xdg.config}", true},
				{"${home}/.config/dconf/user", true},
				{"${home}/.config", true},
				// Not an engine coordinate, so a Linux variant never reaches the database check with it.
				{"${instance.root}/dconf", true},
				// Holds the database only where a host relocates it; the verdict must not follow the host.
				{"${home}/work", false},
				{"${xdg.config}/dconf-keyfiles/clock.ini", false},
				{"${xdg.config}/dconf/user.backup", false},
			} {
				for field, data := range map[string]string{
					"capture source": strings.Replace(validPlatformV3Module, `"source": "${xdg.config}/ripgrep/ripgreprc", "dest"`, `"source": "`+test.path+`", "dest"`, 1),
					"restore target": strings.Replace(validPlatformV3Module, `"target": "${xdg.config}/ripgrep/ripgreprc"`, `"target": "`+test.path+`"`, 1),
				} {
					_, err := ParseAndValidateModuleJSON([]byte(data), "module.jsonc")
					if rejected := err != nil; rejected != test.rejected {
						t.Errorf("%s %s: rejected=%v (%v), want %v", field, test.path, rejected, err, test.rejected)
					}
				}
			}
		})
	}
}

// Projection must retain the catalog tier so OS settings never become apps.
func TestProjectedSystemSettingsDoNotBecomeApplications(t *testing.T) {
	mod := &Module{
		ModuleSchemaVersion: 3, ID: "windows-settings.display", Tier: "windows-settings",
		Platforms: map[string]PlatformVariant{"windows": {
			Realization: "endstate-restore", Matches: MatchCriteria{PathExists: []string{"${home}/display.json"}},
		}},
	}
	projected := FilterCatalogForPlatform(map[string]*Module{mod.ID: mod}, "windows")
	mf := &manifest.Manifest{ConfigModules: []string{mod.ID}}
	SynthesizeAppsFromModules(mf, projected)
	if len(mf.Apps) != 0 {
		t.Fatalf("OS settings became installable applications: %+v", mf.Apps)
	}
}

func TestAllLegacyModulesInertOnLinuxAndDarwin(t *testing.T) {
	catalog, diagnostics, err := GetCatalogWithDiagnostics(productionRepoRoot())
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("catalog: %v, diagnostics: %+v", err, diagnostics)
	}
	windows := FilterCatalogForPlatform(catalog, "windows")
	linux := FilterCatalogForPlatform(catalog, "linux")
	darwin := FilterCatalogForPlatform(catalog, "darwin")
	legacy := 0
	for id, mod := range catalog {
		if mod.EffectiveSchemaVersion() > 2 {
			continue
		}
		legacy++
		if windows[id] != mod {
			t.Errorf("Windows changed legacy module %s", id)
		}
		if linux[id] != nil || darwin[id] != nil {
			t.Errorf("legacy Windows module %s became executable on another platform", id)
		}
	}
	if legacy == 0 {
		t.Fatal("production catalog contains no legacy modules")
	}
}

func TestHomeManagerBindingProjectionAndFingerprint(t *testing.T) {
	data := strings.Replace(validPlatformV3Module, `"realization": "home-manager",`, `"realization": "home-manager", "homeManagerAdapter":{"id":"ripgrep","reviewFingerprint":"`+strings.Repeat("a", 64)+`","configSetId":"default","generationId":"g1"},`, 1)
	mod, err := ParseAndValidateModuleJSON([]byte(data), "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	projected := ProjectModuleForPlatform(mod, "linux")
	if projected.HomeManagerAdapter == nil || projected.HomeManagerAdapter.GenerationID != "g1" {
		t.Fatalf("lost binding: %+v", projected)
	}
	fingerprint := projected.Config.Sets[0].Generations[0].Fingerprint
	projected.HomeManagerAdapter.ID = "changed"
	if mod.Platforms["linux"].HomeManagerAdapter.ID != "ripgrep" {
		t.Fatal("projection mutated authored binding")
	}
	changed, err := ParseAndValidateModuleJSON([]byte(strings.Replace(data, strings.Repeat("a", 64), strings.Repeat("b", 64), 1)), "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	if ProjectModuleForPlatform(changed, "linux").Config.Sets[0].Generations[0].Fingerprint == fingerprint {
		t.Fatal("binding omitted from generation fingerprint")
	}
	for name, invalid := range map[string]string{
		"unknown field":      strings.Replace(data, `"generationId":"g1"`, `"generationId":"g1","actions":["capture"]`, 1),
		"missing generation": strings.Replace(data, `"generationId":"g1"`, `"generationId":""`, 1),
		"invalid hash":       strings.Replace(data, strings.Repeat("a", 64), "stale", 1),
		"non Linux binding":  strings.ReplaceAll(data, `"linux"`, `"darwin"`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAndValidateModuleJSON([]byte(invalid), "module.jsonc"); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
}

func TestLinuxAdapterBindingDoesNotChangeWindowsFingerprint(t *testing.T) {
	mod, err := ParseAndValidateModuleJSON([]byte(validPlatformV3Module), "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	mod.Platforms["windows"] = PlatformVariant{Realization: "endstate-restore", Matches: MatchCriteria{PathExists: []string{"${home}/prefs"}}, Capture: &CaptureDef{Files: []CaptureFile{{Source: "${home}/prefs", Dest: "prefs"}}}}
	before, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	parsedBefore, err := ParseAndValidateModuleJSON(before, "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	variant := mod.Platforms["linux"]
	variant.HomeManagerAdapter = &HomeManagerAdapterBinding{ID: "ripgrep", ReviewFingerprint: strings.Repeat("a", 64), ConfigSetID: "default", GenerationID: "g1"}
	mod.Platforms["linux"] = variant
	after, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	parsedAfter, err := ParseAndValidateModuleJSON(after, "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	if ProjectModuleForPlatform(parsedBefore, "windows").Config.Sets[0].Generations[0].Fingerprint != ProjectModuleForPlatform(parsedAfter, "windows").Config.Sets[0].Generations[0].Fingerprint {
		t.Fatal("Linux binding changed Windows fingerprint")
	}
}
