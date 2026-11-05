// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlatformMappingsValidateAuthorityWithoutChangingGenerationMeaning(t *testing.T) {
	base := Module{ModuleSchemaVersion: 3, ID: "apps.mapping", DisplayName: "Mapping fixture", Platforms: map[string]PlatformVariant{"darwin": {Realization: "endstate-restore", Config: &ConfigDef{InstanceDetectors: []InstanceDetectorDef{{ID: "default", Type: "path", Glob: "${home}"}}, Sets: []ConfigSetDef{{ID: "renamed", Generations: []GenerationDef{{ID: "g1", Order: 1, Restore: []RestoreDef{{Type: "copy", Source: "settings.json", Target: "${home}/settings.json"}}}}}}}}}}
	mapping := PlatformMappingDef{ID: "bridge", From: PlatformMappingEndpoint{"linux", "preferences", "g1"}, To: PlatformMappingEndpoint{"darwin", "renamed", "g1"}, AcceptsSourceFingerprints: []string{strings.Repeat("a", 64)}, Operations: []MigrationOperationDef{}, Validate: []ValidationDef{{Type: "json-parse", Path: "settings.json"}}}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	variant := base.Platforms["darwin"]
	variant.Mappings = []PlatformMappingDef{mapping}
	base.Platforms["darwin"] = variant
	raw, err = json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateModule(after, ""); err != nil {
		t.Fatal(err)
	}
	if before.Revision == after.Revision {
		t.Fatal("mapping authority did not change module revision")
	}
	if ProjectModuleForPlatform(before, "darwin").Config.Sets[0].Generations[0].Fingerprint != ProjectModuleForPlatform(after, "darwin").Config.Sets[0].Generations[0].Fingerprint {
		t.Fatal("inbound mapping changed generation meaning")
	}
	for _, test := range []struct {
		name   string
		change func(*PlatformMappingDef)
	}{
		{"implicit operations", func(m *PlatformMappingDef) { m.Operations = nil }},
		{"missing validation", func(m *PlatformMappingDef) { m.Validate = nil }},
		{"unowned target", func(m *PlatformMappingDef) { m.To.Platform = "windows" }},
		{"same platform", func(m *PlatformMappingDef) { m.From.Platform = "darwin" }},
		{"missing fingerprint", func(m *PlatformMappingDef) { m.AcceptsSourceFingerprints = nil }},
		{"duplicate fingerprint", func(m *PlatformMappingDef) {
			m.AcceptsSourceFingerprints = append(m.AcceptsSourceFingerprints, m.AcceptsSourceFingerprints[0])
		}},
		{"unmapped validation", func(m *PlatformMappingDef) { m.Validate[0].Path = "untransferred.json" }},
		{"invalid operation", func(m *PlatformMappingDef) { m.Operations = []MigrationOperationDef{{Type: "shell"}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := ClonePlatformMappings([]PlatformMappingDef{mapping})[0]
			test.change(&candidate)
			target := ProjectModuleForPlatform(after, "darwin")
			target.Mappings = []PlatformMappingDef{candidate}
			if err := validatePlatformMappings(after, "darwin", target, ""); DiagnosticCode(err) != DiagnosticInvalidPlatformMapping {
				t.Fatalf("invalid mapping accepted: %v", err)
			}
		})
	}
	target := ProjectModuleForPlatform(after, "darwin")
	target.Mappings = append(target.Mappings, target.Mappings[0])
	target.Mappings[1].ID = "duplicate-bridge"
	if err := validatePlatformMappings(after, "darwin", target, ""); err == nil {
		t.Fatal("ambiguous bridges accepted")
	}
	history := &GenerationHistory{SchemaVersion: 2, Generations: []GenerationHistoryEntry{{ModuleID: after.ID, Platform: "darwin", ConfigSetID: "renamed", GenerationID: "g1", Fingerprints: []string{target.Config.Sets[0].Generations[0].Fingerprint}}}}
	if err := ValidateRepositoryGenerationHistory(map[string]*Module{after.ID: after}, history); GenerationHistoryDiagnosticCode(err) != HistoryDiagnosticAcceptedFingerprintNotRecorded {
		t.Fatalf("unrecorded source acceptance: %v", err)
	}
}
