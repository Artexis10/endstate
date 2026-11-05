// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformFingerprintCoversOnlySelectedVariantWithoutLosingNumbers(t *testing.T) {
	const source = `{"moduleSchemaVersion":3,"id":"apps.fixture","displayName":"Fixture","sensitivity":"low","platforms":{"linux":{"realization":"endstate-restore","matches":{},"config":{"instanceDetectors":[{"id":"user","type":"path","glob":"${home}"}],"sets":[{"id":"preferences","generations":[{"id":"g1","order":1,"capture":{"files":[{"source":"${home}/prefs","dest":"prefs"}]}},{"id":"g2","order":2}],"migrations":[{"from":"g1","to":"g2","operations":[{"type":"json-set","path":"prefs","jsonPath":"$.count","value":{"count":9007199254740992,"fingerprint":"data"}}],"validate":[{"type":"file-exists","path":"prefs"}]}]}]}},"windows":{"realization":"endstate-restore","matches":{},"capture":{"files":[{"source":"${home}/prefs","dest":"prefs"}]}}}}`
	parse := func(raw string) *Module {
		t.Helper()
		mod, err := ParseModuleJSON([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		return mod
	}
	fingerprint := func(mod *Module, platform string) string {
		t.Helper()
		return ProjectModuleForPlatform(mod, platform).Config.Sets[0].Generations[0].Fingerprint
	}
	baseline := parse(source)
	want := fingerprint(baseline, "linux")
	cases := []struct {
		name, raw string
		changed   bool
	}{
		{"other platform", strings.Replace(source, `"source":"${home}/prefs","dest":"prefs"}]}}}`, `"source":"${home}/different","dest":"prefs"}]}}}`, 1), false},
		{"historical acceptance", strings.Replace(source, `"id":"g2","order":2`, `"id":"g2","order":2,"acceptsSourceFingerprints":["`+strings.Repeat("a", 64)+`"]`, 1), false},
		{"large exact number", strings.Replace(source, "9007199254740992", "9007199254740993", 1), true},
		{"authored data named fingerprint", strings.Replace(source, `"fingerprint":"data"`, `"fingerprint":"changed"`, 1), true},
		{"selected capture", strings.Replace(source, `"source":"${home}/prefs"`, `"source":"${home}/changed"`, 1), true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := fingerprint(parse(tt.raw), "linux")
			if (got != want) != tt.changed {
				t.Fatalf("fingerprint changed=%v, want %v", got != want, tt.changed)
			}
		})
	}
	if fingerprint(baseline, "windows") == want {
		t.Fatal("platform-qualified identities share a fingerprint")
	}
	projected := ProjectModuleForPlatform(baseline, "linux")
	projected.Config.Sets[0].Migrations[0].Operations[0].Value.(map[string]any)["count"] = json.Number("1")
	projected.Config.Sets[0].Generations[0].Capture.Files[0].Source = "${home}/mutated"
	if !bytes.Equal(projected.CanonicalSnapshot(), baseline.CanonicalSnapshot()) || fingerprint(baseline, "linux") != want || baseline.Platforms["linux"].Config.Sets[0].Generations[0].Capture.Files[0].Source != "${home}/prefs" {
		t.Fatal("projection mutated authored declarations")
	}
}

func TestFlatPlatformHistoryRegistersBothQualifiedIdentities(t *testing.T) {
	raw, err := os.ReadFile("../../../modules/apps/ripgrep/module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	current := collectCurrentGenerations(map[string]*Module{mod.ID: mod})
	if len(current) != 2 {
		t.Fatalf("qualified generations=%d", len(current))
	}
	for _, declaration := range current {
		t.Logf("%s %s", declaration.Identity(), declaration.Generation.Fingerprint)
	}
	history := &GenerationHistory{SchemaVersion: 2, Generations: []GenerationHistoryEntry{}}
	for _, declaration := range current {
		history.Generations = append(history.Generations, GenerationHistoryEntry{ModuleID: mod.ID, Platform: declaration.Platform, ConfigSetID: declaration.ConfigSetID, GenerationID: declaration.GenerationID, Fingerprints: []string{declaration.Generation.Fingerprint}})
	}
	if err := ValidateRepositoryGenerationHistory(map[string]*Module{mod.ID: mod}, history); err != nil {
		t.Fatal(err)
	}
	linux := ProjectModuleForPlatform(mod, "linux")
	windowsFingerprint := ProjectModuleForPlatform(mod, "windows").Config.Sets[0].Generations[0].Fingerprint
	linux.Config.Sets[0].Generations[0].AcceptsSourceFingerprints = []string{windowsFingerprint}
	if err := ValidateRepositoryGenerationHistory(map[string]*Module{linux.ID: linux}, history); GenerationHistoryDiagnosticCode(err) != HistoryDiagnosticAcceptedFingerprintNotRecorded {
		t.Fatalf("cross-platform history accepted: %v", err)
	}
}

func TestGenerationHistoryWritesV2AndRetainsLegacyIdentity(t *testing.T) {
	history := GenerationHistory{SchemaVersion: 1, Generations: []GenerationHistoryEntry{{ModuleID: "apps.fixture", ConfigSetID: "preferences", GenerationID: "g1", Fingerprints: []string{strings.Repeat("a", 64)}}}}
	raw, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	var got GenerationHistory
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 2 || got.Generations[0].Identity() != "apps.fixture/preferences/g1" || got.Generations[0].Fingerprints[0] != history.Generations[0].Fingerprints[0] {
		t.Fatalf("legacy history changed: %s", raw)
	}
}

func TestPlatformCoordinateDiscoveryDoesNotReexpandResolvedHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home-$UNTRUSTED-%UNTRUSTED%")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("UNTRUSTED", "different-root")
	mod := &Module{ModuleSchemaVersion: 2, SourceSchemaVersion: 3, Platform: "linux", ID: "apps.fixture", Config: &ConfigDef{InstanceDetectors: []InstanceDetectorDef{{ID: "user", Type: "path", Glob: "${home}"}}}}
	instances, err := DiscoverInstances(mod, nil, DiscoveryOptions{})
	if err != nil || len(instances) != 1 || instances[0].Root != root {
		t.Fatalf("resolved coordinate was reinterpreted: %+v, %v", instances, err)
	}
}
