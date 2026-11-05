// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestMappedMaterializationChecksOnlyTransferredTargetMembers(t *testing.T) {
	for _, test := range []struct {
		name, secret, exclude, member, check, targetRoot, rootExclude string
		want                                                          Code
	}{
		{name: "combined recursive and filename wildcard", secret: "${instance.root}/app/**/token*.json", member: "nested/token1.json", check: "nested/token1.json", want: CodeUnsafePath},
		{name: "literal secret directory", secret: "${instance.root}/app/nested", member: "nested/settings.json", check: "nested/settings.json", want: CodeUnsafePath},
		{name: "basename secret", secret: "token*.json", member: "nested/token1.json", check: "nested/token1.json", want: CodeUnsafePath},
		{name: "malformed secret", secret: "${instance.root}/app/[", member: "settings.json", check: "settings.json", want: CodeUnsafePath},
		{name: "restore exclusion skips forbidden member", secret: "${instance.root}/app/**/token*.json", exclude: "**/nested/**", member: "nested/token1.json", check: "settings.json"},
		{name: "excluded check cannot use prior target", exclude: "**/nested/**", member: "nested/settings.json", check: "nested/settings.json", want: CodeValidationMapping},
		{name: "absent member cannot use prior target", member: "settings.json", check: "missing.json", want: CodeValidationMapping},
		{name: "capture root exclusion covers descendants", targetRoot: "app", rootExclude: "app", member: "nested/settings.json", check: "nested/settings.json", want: CodeUnsafePath},
		{name: "capture root exclusion keeps sibling", targetRoot: "sibling", rootExclude: "sibling", member: "settings.json", check: "settings.json"},
		{name: "capture relative exclusion", targetRoot: "app", rootExclude: "**/token*.json", member: "nested/token1.json", check: "nested/token1.json", want: CodeUnsafePath},
	} {
		t.Run(test.name, func(t *testing.T) {
			stage, host := t.TempDir(), t.TempDir()
			writeTestFile(t, filepath.Join(stage, "payload", "settings.json"), `{"theme":"dark"}`)
			writeTestFile(t, filepath.Join(stage, "payload", filepath.FromSlash(test.member)), `{"theme":"dark"}`)
			writeTestFile(t, filepath.Join(host, "app", filepath.FromSlash(test.check)), `{"preexisting":true}`)
			generation := modules.GenerationDef{ID: "g1", Restore: []modules.RestoreDef{{Type: "copy", Source: "payload", Target: instancePath("app")}}}
			if test.exclude != "" {
				generation.Restore[0].Exclude = []string{test.exclude}
			}
			if test.targetRoot != "" {
				generation.Capture = &modules.CaptureDef{Files: []modules.CaptureFile{{Source: instancePath(test.targetRoot), Dest: "payload"}}, ExcludeGlobs: []string{test.rootExclude}}
			}
			plan := testPlan(host, generation)
			plan.Mapping = &modules.PlatformMappingDef{ID: "bridge", Validate: []modules.ValidationDef{{Type: "json-parse", Path: "payload/" + test.check}}}
			plan.Resolution.Resolution = planner.ResolutionMigrate
			plan.Resolution.SourcePlatform = "linux"
			plan.Resolution.TargetPlatform = "darwin"
			if test.secret != "" {
				plan.TargetSecrets = &modules.SecretsDef{Files: []string{test.secret}}
			}
			materialized, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan})
			if CodeOf(err) != test.want {
				t.Fatalf("materialize error = %v; want %s", err, test.want)
			}
			if err == nil && len(materialized.Validations) != 1 {
				t.Fatalf("mapping validation missing: %+v", materialized)
			}
			assertTestFile(t, filepath.Join(host, "app", filepath.FromSlash(test.check)), `{"preexisting":true}`)
		})
	}
}

func TestMappedLinuxPolicyIsCaseSensitive(t *testing.T) {
	stage, host := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(stage, "Token.json"), `{}`)
	generation := modules.GenerationDef{ID: "g1", Restore: []modules.RestoreDef{{Type: "copy", Source: "Token.json", Target: instancePath("Token.json")}}}
	plan := testPlan(host, generation)
	plan.Mapping = &modules.PlatformMappingDef{ID: "bridge", Validate: []modules.ValidationDef{{Type: "json-parse", Path: "Token.json"}}}
	plan.TargetSecrets = &modules.SecretsDef{Files: []string{"token.json"}}
	plan.Resolution.TargetPlatform = "linux"
	if _, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan}); err != nil {
		t.Fatalf("Linux case distinction lost: %v", err)
	}
	plan.Resolution.TargetPlatform = "windows"
	if _, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan}); CodeOf(err) != CodeUnsafePath {
		t.Fatalf("Windows case policy lost: %v", err)
	}
}

func TestMappedJournalRejectsPreexistingMemberSubstitution(t *testing.T) {
	lineage := testJournalLineage()
	lineage.MappingID = "bridge"
	lineage.SourcePlatform = "linux"
	lineage.TargetPlatform = "darwin"
	lineage.TargetConfigSetID = "renamed"
	target := filepath.Join(t.TempDir(), "app")
	action := JournalAction{Kind: ActionCopy, Target: target, TransferredPaths: []string{".", "settings.json"}, Desired: JournalActionState{Kind: StateDirectory, Entries: []JournalFilesystemEntry{{Path: "settings.json"}, {Path: "prior.json"}}}}
	validation := JournalValidation{Type: "json-parse", Path: "payload/prior.json", HostPath: filepath.Join(target, "prior.json")}
	if _, _, err := newJournalIntentDisk(lineage, []JournalAction{action}, []JournalValidation{validation}); err == nil || !strings.Contains(err.Error(), "transferred") {
		t.Fatalf("preexisting member accepted: %v", err)
	}
	validation.Path = "payload/settings.json"
	validation.HostPath = filepath.Join(target, "settings.json")
	if _, _, err := newJournalIntentDisk(lineage, []JournalAction{action}, []JournalValidation{validation}); err != nil {
		t.Fatalf("transferred check rejected: %v", err)
	}
	lineage.SourceGeneration = "g1"
	lineage.TargetGeneration = "g1"
	lineage.MigrationPath = []string{"g1", "g1"}
	if err := validateJournalLineage(lineage); err != nil {
		t.Fatalf("equal local IDs are a valid bridge: %v", err)
	}
	lineage.MappingID = ""
	if err := validateJournalLineage(lineage); err == nil {
		t.Fatal("cross-platform lineage lost mapping identity")
	}
}

func TestMappingTransformedForbiddenMemberFailsBeforeSnapshots(t *testing.T) {
	payload, host := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(payload, "app", "settings.json"), `{"theme":"dark"}`)
	writeTestFile(t, filepath.Join(host, "app", "settings.json"), `{"prior":true}`)
	manifest, err := bundle.BuildPayloadManifest(payload)
	if err != nil {
		t.Fatal(err)
	}
	mapping := &modules.PlatformMappingDef{ID: "bridge", From: modules.PlatformMappingEndpoint{Platform: "linux", GenerationID: "g1"}, To: modules.PlatformMappingEndpoint{Platform: "darwin", GenerationID: "g1"}, Operations: []modules.MigrationOperationDef{{Type: "file-copy", Source: "app/settings.json", Target: "app/nested/token1.json"}}, Validate: []modules.ValidationDef{{Type: "json-parse", Path: "app/settings.json"}}}
	generation := modules.GenerationDef{ID: "g1", Restore: []modules.RestoreDef{{Type: "copy", Source: "app", Target: instancePath("app")}}}
	stage, err := migration.NewEngine().Stage(context.Background(), migration.StageRequest{CaptureID: "capture", PayloadRoot: payload, PayloadManifest: manifest, SourceGeneration: "g1", TargetGeneration: &generation, Mapping: mapping, TempParent: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	plan := testPlan(host, generation)
	plan.Mapping = mapping
	plan.Resolution.TargetPlatform = "darwin"
	plan.TargetSecrets = &modules.SecretsDef{Files: []string{"${instance.root}/app/**/token*.json"}}
	if materialized, err := Materialize(context.Background(), Request{Stage: stage, Plan: plan}); CodeOf(err) != CodeUnsafePath || materialized != nil {
		t.Fatalf("transformed forbidden member accepted: %+v, %v", materialized, err)
	}
	assertTestFile(t, filepath.Join(host, "app", "settings.json"), `{"prior":true}`)
	if _, err := os.Stat(filepath.Join(host, "app", "nested", "token1.json")); !os.IsNotExist(err) {
		t.Fatalf("forbidden target written: %v", err)
	}
}

func TestMappedSecretCoordinateRootKeepsLiteralMetacharacters(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("literal Linux coordinate roots")
	}
	home := filepath.Join(t.TempDir(), "home[fixture]{one,two}")
	xdg := filepath.Join(home, "config[fixture]{three,four}")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	for _, test := range []struct {
		name, member string
		want         Code
	}{{"ordinary member", "settings.json", ""}, {"forbidden nested member", "nested/token1.json", CodeUnsafePath}} {
		t.Run(test.name, func(t *testing.T) {
			stage := t.TempDir()
			writeTestFile(t, filepath.Join(stage, "app", filepath.FromSlash(test.member)), `{}`)
			generation := modules.GenerationDef{ID: "g1", Restore: []modules.RestoreDef{{Type: "copy", Source: "app", Target: "${xdg.config}/app"}}}
			plan := testPlan(home, generation)
			plan.Mapping = &modules.PlatformMappingDef{ID: "bridge", Validate: []modules.ValidationDef{{Type: "json-parse", Path: "app/" + test.member}}}
			plan.TargetSecrets = &modules.SecretsDef{Files: []string{"${xdg.config}/app/**/token*.json"}}
			plan.Resolution.TargetPlatform = "linux"
			if _, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan}); CodeOf(err) != test.want {
				t.Fatalf("literal root changed secret glob meaning: %v; want %s", err, test.want)
			}
		})
	}
}

func TestMappedRegistrySetRetainsTargetSecretPolicy(t *testing.T) {
	stage, host := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(stage, "settings.json"), `{}`)
	generation := modules.GenerationDef{ID: "g1", Restore: []modules.RestoreDef{{Type: "copy", Source: "settings.json", Target: instancePath("settings.json")}, {Type: "registry-set", Key: `HKCU\Software\Fixture`, ValueName: "Token", ValueType: "REG_SZ", Data: "fixture"}}}
	plan := testPlan(host, generation)
	plan.Mapping = &modules.PlatformMappingDef{ID: "bridge", Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}}}
	plan.TargetSecrets = &modules.SecretsDef{RegistryKeys: []string{`HKCU\Software\Fixture\Token`}}
	plan.Resolution.TargetPlatform = "windows"
	if _, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan}); CodeOf(err) != CodeUnsafePath {
		t.Fatalf("mapped registry secret accepted: %v", err)
	}
}

func TestMappedTargetPoliciesResolveThroughTheHostBoundary(t *testing.T) {
	for _, policy := range []string{"secrets", "capture exclusions"} {
		t.Run(policy, func(t *testing.T) {
			authority := t.TempDir()
			stage := filepath.Join(authority, "stage")
			host := filepath.Join(authority, "host[fixture]{one,two}")
			writeTestFile(t, filepath.Join(stage, "app", "nested", "token1.json"), `{}`)
			boundary := &recordingHostBoundary{root: authority, resolved: map[string]string{"${home}": host, "${home}/app": filepath.Join(host, "app"), "${xdg.config}/app": filepath.Join(host, "app"), "${xdg.config}": host}}
			generation := modules.GenerationDef{ID: "g1", Restore: []modules.RestoreDef{{Type: "copy", Source: "app", Target: "${home}/app"}}}
			if policy == "capture exclusions" {
				generation.Capture = &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "${xdg.config}/app", Dest: "app"}}, ExcludeGlobs: []string{"**/token*.json"}}
			}
			plan := testPlan(host, generation)
			plan.Mapping = &modules.PlatformMappingDef{ID: "bridge", Validate: []modules.ValidationDef{{Type: "json-parse", Path: "app/nested/token1.json"}}}
			plan.Resolution.TargetPlatform = "linux"
			if policy == "secrets" {
				plan.TargetSecrets = &modules.SecretsDef{Files: []string{"${home}/app/**/token*.json"}}
			}
			_, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan, Boundary: boundary})
			var failure *Error
			if CodeOf(err) != CodeUnsafePath || !errors.As(err, &failure) || failure.Target != filepath.Join(host, "app", "nested", "token1.json") {
				t.Fatalf("target policy escaped the host boundary: %v", err)
			}
		})
	}
}

func TestMappedCaptureSourceGlobsEnforceScopedExclusions(t *testing.T) {
	for _, test := range []struct {
		name, source, exclude, member string
		want                          Code
	}{
		{"globbed source member", "${home}/app*", "token*.json", "token.json", CodeUnsafePath},
		{"globbed source basename", "${home}/app?", "app1", "settings.json", CodeUnsafePath},
		{"globbed source nested root", "${home}/app*/nested", "token*.json", "nested/token.json", CodeUnsafePath},
		{"sibling stays outside source", "${home}/sibling*", "token*.json", "token.json", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home[fixture]{one,two}")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			fingerprint := strings.Repeat("a", 64)
			generation := modules.GenerationDef{ID: "g1", Order: 1, Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: test.source, Dest: "payload"}}, ExcludeGlobs: []string{test.exclude}}, Restore: []modules.RestoreDef{{Type: "copy", Source: "payload", Target: "${home}/app1"}}}
			authored := modules.Module{ModuleSchemaVersion: 3, ID: "apps.mapping", DisplayName: "Mapping fixture", Sensitivity: "low", Platforms: map[string]modules.PlatformVariant{"linux": {Realization: "endstate-restore", Config: &modules.ConfigDef{InstanceDetectors: []modules.InstanceDetectorDef{{ID: "default", Type: "path", Glob: "${home}"}}, Sets: []modules.ConfigSetDef{{ID: "target", Generations: []modules.GenerationDef{generation}}}}, Mappings: []modules.PlatformMappingDef{{ID: "bridge", From: modules.PlatformMappingEndpoint{Platform: "darwin", ConfigSetID: "source", GenerationID: "g1"}, To: modules.PlatformMappingEndpoint{Platform: "linux", ConfigSetID: "target", GenerationID: "g1"}, AcceptsSourceFingerprints: []string{fingerprint}, Operations: []modules.MigrationOperationDef{}, Validate: []modules.ValidationDef{{Type: "json-parse", Path: "payload/settings.json"}}}}}}}
			raw, err := json.Marshal(authored)
			if err != nil {
				t.Fatal(err)
			}
			validated, err := modules.ParseAndValidateModuleJSON(raw, "")
			if err != nil {
				t.Fatal(err)
			}
			current := modules.ProjectModuleForPlatform(validated, "linux")
			resolver := planner.NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
			source := planner.SourceCapture{CaptureID: "capture", ModuleID: current.ID, ConfigSetID: "source", SourcePlatform: "darwin", Generation: "g1", GenerationFingerprint: fingerprint, CaptureModuleSchemaVersion: 3, SourceSnapshotLinked: true, SourcePayloadVerified: true}
			plan := resolver.ResolveCandidate(source, planner.TargetInstance{ID: "target", ModuleID: current.ID, Root: home})
			if plan.Resolution.Reason != nil {
				t.Fatalf("mapping: %+v", plan.Resolution)
			}
			stage := t.TempDir()
			writeTestFile(t, filepath.Join(stage, "payload", "settings.json"), `{}`)
			writeTestFile(t, filepath.Join(stage, "payload", filepath.FromSlash(test.member)), `{}`)
			materialized, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan})
			if CodeOf(err) != test.want {
				t.Fatalf("capture-source glob changed exclusion scope: materialized=%v, err=%v, want=%s", materialized, err, test.want)
			}
			if err == nil && (materialized == nil || len(materialized.Actions) != 1) {
				t.Fatalf("sibling source lost its restore action: %+v", materialized)
			}
			if _, err := os.Stat(filepath.Join(home, "app1")); !os.IsNotExist(err) {
				t.Fatalf("policy preflight wrote target: %v", err)
			}
		})
	}
}

func TestMappedRegistryValuePolicySurvivesCatalogAndPlanMutation(t *testing.T) {
	stage, host := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(stage, "settings.json"), `{}`)
	fingerprint := strings.Repeat("a", 64)
	generation := modules.GenerationDef{ID: "g1", Order: 1, Restore: []modules.RestoreDef{{Type: "copy", Source: "settings.json", Target: instancePath("settings.json")}, {Type: "registry-set", Key: `HKCU\Software\Fixture`, ValueName: "Token", ValueType: "REG_SZ", Data: "fixture"}}}
	current := &modules.Module{ID: "apps.mapping", ModuleSchemaVersion: 2, SourceSchemaVersion: 3, Platform: "windows", Secrets: &modules.SecretsDef{RegistryValues: []modules.RegistrySecretValue{{Key: `HKCU\Software\Fixture`, ValueName: "Token"}}}, Config: &modules.ConfigDef{Sets: []modules.ConfigSetDef{{ID: "target", Generations: []modules.GenerationDef{generation}}}}, Mappings: []modules.PlatformMappingDef{{ID: "bridge", From: modules.PlatformMappingEndpoint{Platform: "linux", ConfigSetID: "source", GenerationID: "g1"}, To: modules.PlatformMappingEndpoint{Platform: "windows", ConfigSetID: "target", GenerationID: "g1"}, AcceptsSourceFingerprints: []string{fingerprint}, Operations: []modules.MigrationOperationDef{}, Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}}}}}
	resolver := planner.NewCompatibilityResolver(map[string]*modules.Module{current.ID: current}, nil)
	source := planner.SourceCapture{CaptureID: "capture", ModuleID: current.ID, ConfigSetID: "source", SourcePlatform: "linux", Generation: "g1", GenerationFingerprint: fingerprint, CaptureModuleSchemaVersion: 3, SourceSnapshotLinked: true, SourcePayloadVerified: true}
	plan := resolver.ResolveCandidate(source, planner.TargetInstance{ID: "target", ModuleID: current.ID, Root: host})
	if plan.Resolution.Reason != nil {
		t.Fatalf("mapping: %+v", plan.Resolution)
	}
	current.Secrets.RegistryValues[0].ValueName = "Other"
	copied := planner.CloneConfigPlan(planner.ConfigPlan{Sets: []planner.PlanSet{plan}})
	copied.Sets[0].TargetSecrets.RegistryValues[0].ValueName = "Caller"
	if _, err := Materialize(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g1"}, Plan: plan}); CodeOf(err) != CodeUnsafePath {
		t.Fatalf("mutation allowed mapped secret registry write: %v", err)
	}
}
