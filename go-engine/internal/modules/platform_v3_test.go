// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import "testing"

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
	if linux == nil || linux.EffectiveSchemaVersion() != 1 || linux.SourceSchemaVersion != 3 || linux.Platform != "linux" {
		t.Fatalf("Linux projection = %+v", linux)
	}
	if linux.Realization != "home-manager" || linux.Capture == nil || len(linux.Matches.PathExists) != 1 {
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
