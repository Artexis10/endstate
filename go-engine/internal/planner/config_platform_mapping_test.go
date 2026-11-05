// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestPlatformMappingPinsTargetAuthorityAndCannotBeBypassed(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	mapping := modules.PlatformMappingDef{ID: "bridge", From: modules.PlatformMappingEndpoint{Platform: "linux", ConfigSetID: "source", GenerationID: "g1"}, To: modules.PlatformMappingEndpoint{Platform: "darwin", ConfigSetID: "renamed", GenerationID: "g1"}, AcceptsSourceFingerprints: []string{fingerprint}, Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.nested", Value: map[string]any{"value": "pinned"}}}, Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}}}
	source := SourceCapture{CaptureID: "capture", ModuleID: "apps.mapping", ConfigSetID: "source", SourcePlatform: "linux", Generation: "g1", GenerationFingerprint: fingerprint, CaptureModuleSchemaVersion: 3, SourceSnapshotLinked: true, SourcePayloadVerified: true}
	target := TargetInstance{ID: "instance", ModuleID: source.ModuleID}
	current := &modules.Module{ID: source.ModuleID, ModuleSchemaVersion: 2, SourceSchemaVersion: 3, Platform: "darwin", Revision: strings.Repeat("b", 64), Mappings: []modules.PlatformMappingDef{mapping}, Secrets: &modules.SecretsDef{Files: []string{"token.json"}}, Config: &modules.ConfigDef{Sets: []modules.ConfigSetDef{{ID: "renamed", Generations: []modules.GenerationDef{{ID: "g1", Order: 1, Capture: &modules.CaptureDef{ExcludeGlobs: []string{"private"}}}}}}}}
	current.Secrets.RegistryValues = []modules.RegistrySecretValue{{Key: `HKCU\Software\Fixture`, ValueName: "Token", Subkeys: true}}
	resolver := NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	current.Mappings[0].Operations[0].Value.(map[string]any)["value"] = "mutated"
	current.Secrets.Files[0] = "mutated"
	current.Secrets.RegistryValues[0].ValueName = "Other"
	current.Config.Sets[0].Generations[0].Capture.ExcludeGlobs[0] = "mutated"
	result := resolver.ResolveCandidate(source, target)
	if result.Resolution.Reason != nil || result.Resolution.Resolution != ResolutionMigrate || result.Resolution.TargetConfigSetID != "renamed" || result.Resolution.MappingID != "bridge" || result.TargetSecrets.Files[0] != "token.json" || result.TargetGenerationDef.Capture.ExcludeGlobs[0] != "private" || result.Mapping.Operations[0].Value.(map[string]any)["value"] != "pinned" {
		t.Fatalf("mapping authority not pinned: %+v", result)
	}
	scope, _ := modules.ResolveRegistrySecretScope(&modules.Module{Secrets: result.TargetSecrets})
	if !scope.ExcludesValue(`HKCU\Software\Fixture\Child`, "Token") {
		t.Fatal("catalog mutation weakened pinned registry secrets")
	}
	cloned := CloneConfigPlan(ConfigPlan{Sets: []PlanSet{result}})
	cloned.Sets[0].Mapping.Operations[0].Value.(map[string]any)["value"] = "caller"
	cloned.Sets[0].TargetSecrets.Files[0] = "caller"
	cloned.Sets[0].TargetSecrets.RegistryValues[0].ValueName = "Caller"
	cloned.Sets[0].TargetGenerationDef.Capture.ExcludeGlobs[0] = "caller"
	if result.Mapping.Operations[0].Value.(map[string]any)["value"] != "pinned" || result.TargetSecrets.Files[0] != "token.json" || result.TargetGenerationDef.Capture.ExcludeGlobs[0] != "private" {
		t.Fatal("caller mutated pinned mapping policy")
	}
	scope, _ = modules.ResolveRegistrySecretScope(&modules.Module{Secrets: result.TargetSecrets})
	if !scope.ExcludesValue(`HKCU\Software\Fixture\Child`, "Token") {
		t.Fatal("plan clone weakened original registry secrets")
	}
	for _, test := range []struct {
		name   string
		change func(*SourceCapture)
		reason ResolutionReason
	}{
		{"fingerprint not accepted", func(s *SourceCapture) { s.GenerationFingerprint = strings.Repeat("c", 64) }, ReasonPlatformMappingMissing},
		{"source set not accepted", func(s *SourceCapture) { s.ConfigSetID = "other" }, ReasonPlatformMappingMissing},
		{"unverified payload", func(s *SourceCapture) { s.SourcePayloadVerified = false }, ReasonPayloadIntegrityFailed},
		{"unlinked schema 3", func(s *SourceCapture) { s.SourceSnapshotLinked = false }, ReasonPayloadIntegrityFailed},
		{"unlinked legacy source", func(s *SourceCapture) { s.CaptureModuleSchemaVersion = 2; s.SourceSnapshotLinked = false }, ReasonPlatformMappingMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := source
			test.change(&candidate)
			for _, explicit := range []map[string]string{nil, {source.CaptureID: target.ID}} {
				plan := resolver.ResolveSources([]SourceCapture{candidate}, map[string][]TargetInstance{source.ModuleID: {target}}, explicit)
				resolution := plan.Sets[0].Resolution
				if resolution.Reason == nil || *resolution.Reason != test.reason {
					t.Fatalf("mapping bypass: %+v", resolution)
				}
			}
		})
	}
	// Two different source sets still compete for the same target set and instance.
	current.Mappings = modules.ClonePlatformMappings([]modules.PlatformMappingDef{mapping, mapping})
	current.Mappings[1].ID = "other-bridge"
	current.Mappings[1].From.ConfigSetID = "other"
	resolver = NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	other := source
	other.CaptureID = "other-capture"
	other.ConfigSetID = "other"
	plan := resolver.ResolveSources([]SourceCapture{source, other}, map[string][]TargetInstance{source.ModuleID: {target}}, nil)
	for _, set := range plan.Sets {
		if set.Resolution.Reason == nil || *set.Resolution.Reason != ReasonTargetCollision {
			t.Fatalf("renamed target ownership did not collide: %+v", set.Resolution)
		}
	}
	// Schema-v2 captures are implicitly Windows-scoped and need exact linkage.
	current.Mappings = modules.ClonePlatformMappings([]modules.PlatformMappingDef{mapping})
	current.Mappings[0].From.Platform = "windows"
	resolver = NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	legacy := source
	legacy.CaptureModuleSchemaVersion = 2
	legacy.SourcePlatform = ""
	if result := resolver.ResolveCandidate(legacy, target); result.Resolution.Reason != nil {
		t.Fatalf("verified legacy mapping rejected: %+v", result.Resolution)
	}
	legacy.SourceSnapshotLinked = false
	if result := resolver.ResolveCandidate(legacy, target); result.Resolution.Reason == nil || *result.Resolution.Reason != ReasonPayloadIntegrityFailed {
		t.Fatalf("legacy source linkage bypassed: %+v", result.Resolution)
	}

}
