// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

// These structs intentionally freeze the legacy decoder's known field set.
// They are local definitions, not aliases to the evolving engine Manifest.
type frozenLegacyManifest struct {
	Version        interface{}            `json:"version"`
	Name           string                 `json:"name,omitempty"`
	Captured       string                 `json:"captured,omitempty"`
	Apps           []frozenLegacyApp      `json:"apps"`
	Includes       []string               `json:"includes,omitempty"`
	Restore        []frozenLegacyRestore  `json:"restore,omitempty"`
	Verify         []json.RawMessage      `json:"verify,omitempty"`
	ConfigModules  []string               `json:"configModules,omitempty"`
	ExcludeConfigs []string               `json:"excludeConfigs,omitempty"`
	HomeManager    map[string]interface{} `json:"homeManager,omitempty"`
}

type frozenLegacyApp struct {
	ID      string            `json:"id"`
	Refs    map[string]string `json:"refs"`
	Driver  string            `json:"driver,omitempty"`
	Version string            `json:"version,omitempty"`
}

type frozenLegacyRestore struct {
	Type       string `json:"type"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	FromModule string `json:"fromModule,omitempty"`
}

func TestFrozenLegacyDecoderCannotReachGenerationPayloads(t *testing.T) {
	tests := []struct {
		name        string
		restore     []any
		wantRestore int
	}{
		{"pure v2", nil, 0},
		{"mixed v2 explicit legacy lane", []any{map[string]any{
			"type": "copy", "source": "./configs/legacy-a/prefs.json", "target": "%APPDATA%/Legacy/prefs.json", "fromModule": "legacy.example", "legacyCaptureId": "legacy-a",
		}}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validManifestV2Value("capture-a")
			value["apps"] = []any{map[string]any{"id": "vendor-app", "refs": map[string]any{"windows": "Vendor.App"}}}
			if tt.restore != nil {
				value["restore"] = tt.restore
				value["configModules"] = []any{"legacy.example"}
				value["legacyConfigLanes"] = []any{map[string]any{
					"captureId": "legacy-a", "moduleId": "legacy.example", "moduleSchemaVersion": 1, "payloadRoot": "configs/legacy-a",
				}}
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			jsonc := append([]byte("// decoded by a frozen v1 reader\n"), encoded...)

			var legacy frozenLegacyManifest
			if err := json.Unmarshal(StripJsoncComments(jsonc), &legacy); err != nil {
				t.Fatal(err)
			}
			if len(legacy.Apps) != 1 || legacy.Apps[0].Refs["windows"] != "Vendor.App" {
				t.Fatalf("legacy decoder lost visible apps: %+v", legacy.Apps)
			}
			if len(legacy.Restore) != tt.wantRestore {
				t.Fatalf("legacy restore actions = %+v, want %d", legacy.Restore, tt.wantRestore)
			}
			generationRoot := "configs/capture-a"
			for _, action := range legacy.Restore {
				if action.FromModule != "legacy.example" {
					t.Fatalf("legacy action lost explicit module association: %+v", action)
				}
				source := strings.TrimPrefix(strings.ReplaceAll(action.Source, `\`, "/"), "./")
				if source == generationRoot || strings.HasPrefix(source, generationRoot+"/") {
					t.Fatalf("legacy action reaches generation payload root %q: %+v", generationRoot, action)
				}
			}
		})
	}
}
