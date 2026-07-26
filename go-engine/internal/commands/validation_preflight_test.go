// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestPreflightValidationProductionModuleRequiresCatalogAuthority(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]*modules.Module, **modules.Module)
		wantErr bool
	}{
		{name: "actual catalog pointer"},
		{name: "same-id clone", wantErr: true, mutate: func(catalog map[string]*modules.Module, candidate **modules.Module) {
			clone, err := modules.ParseModuleJSON(catalog["apps.notepad-plus-plus"].CanonicalSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			*candidate = clone
		}},
		{name: "same-id mutated clone", wantErr: true, mutate: func(catalog map[string]*modules.Module, candidate **modules.Module) {
			clone, err := modules.ParseModuleJSON(catalog["apps.notepad-plus-plus"].CanonicalSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			clone.Restore[0].Target = `%APPDATA%\forged\settings.xml`
			*candidate = clone
		}},
		{name: "mutated catalog pointer", wantErr: true, mutate: func(_ map[string]*modules.Module, candidate **modules.Module) {
			(*candidate).Restore[0].Target = `%APPDATA%\forged\settings.xml`
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog, err := modules.GetCatalog(filepath.Join("..", "..", ".."))
			if err != nil {
				t.Fatal(err)
			}
			candidate := catalog["apps.notepad-plus-plus"]
			if candidate == nil {
				t.Fatal("production catalog missing notepad-plus-plus")
			}
			if tt.mutate != nil {
				tt.mutate(catalog, &candidate)
			}
			context, session := validationPreflightSession(t)
			err = preflightValidationProductionModule(validationProductionModulePreflight{
				Context: context, Session: session, Catalog: catalog, Modules: []*modules.Module{candidate},
				Manifest: manifestForValidationModule(candidate), PortableRoot: context.Root(),
			})
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("catalog authority: %v", err)
				}
				return
			}
			if !errors.Is(err, validationmode.ErrUnsafePath) {
				t.Fatalf("error = %v, want unsafe path", err)
			}
			if got := session.IsolationError(); got == nil || !strings.Contains(got.Error(), "coordinate=modules") {
				t.Fatalf("isolation = %v", got)
			}
		})
	}
}

func TestPreflightValidationProductionModuleAcceptsEstablishedCaptureProjection(t *testing.T) {
	mod := loadValidationProductionModule(t, "notepad-plus-plus")
	for _, mixedV2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "mixed-v2"}[mixedV2], func(t *testing.T) {
			mf := manifestForValidationModule(mod)
			layoutID := "notepad-plus-plus"
			if mixedV2 {
				mf.Version = 2
				layoutID = bundle.LegacyCaptureID(mod.ID)
				mf.LegacyConfigLanes = []manifest.LegacyConfigLane{{
					CaptureID: layoutID, ModuleID: mod.ID, ModuleSchemaVersion: 1,
					PayloadRoot: path.Join("configs", layoutID),
				}}
			}
			for index := range mf.Restore {
				mf.Restore[index].Source = projectedLegacySource(mf.Restore[index].Source, layoutID)
				if mixedV2 {
					mf.Restore[index].LegacyCaptureID = layoutID
				}
			}
			context, session := validationPreflightSessionFor(t, "notepad-plus-plus")
			if err := preflightValidationProductionModule(validationProductionModulePreflight{
				Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: mf, PortableRoot: context.Root(),
			}); err != nil {
				t.Fatalf("capture projection preflight: %v", err)
			}
		})
	}
}

func TestPreflightValidationProductionModuleAcceptsReviewedCaptureOnlyProvenance(t *testing.T) {
	mod := loadValidationProductionModule(t, "mgba")
	valid := manifestForValidationModule(mod)
	context, session := validationPreflightSessionFor(t, "mgba")
	if err := preflightValidationProductionModule(validationProductionModulePreflight{
		Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: valid, PortableRoot: context.Root(),
	}); err != nil {
		t.Fatalf("capture-only projection preflight: %v; isolation=%v", err, session.IsolationError())
	}

	for _, test := range []struct {
		name, coordinate string
		mutate           func(*manifest.Manifest)
	}{
		{"missing provenance", "configModules", func(value *manifest.Manifest) { value.ConfigModules = nil }},
		{"foreign provenance", "configModules[0]", func(value *manifest.Manifest) { value.ConfigModules[0] = "apps.foreign" }},
		{"duplicate provenance", "configModules[1]", func(value *manifest.Manifest) { value.ConfigModules = append(value.ConfigModules, mod.ID) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := *valid
			candidate.ConfigModules = append([]string(nil), valid.ConfigModules...)
			test.mutate(&candidate)
			context, session := validationPreflightSessionFor(t, "mgba")
			err := preflightValidationProductionModule(validationProductionModulePreflight{
				Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: &candidate, PortableRoot: context.Root(),
			})
			if !errors.Is(err, validationmode.ErrUnsafePath) || session.IsolationError() == nil || !strings.Contains(session.IsolationError().Error(), "coordinate="+test.coordinate) {
				t.Fatalf("error=%v isolation=%v, want coordinate %s", err, session.IsolationError(), test.coordinate)
			}
		})
	}
}

func TestPreflightValidationProductionModuleAcceptsCaptureOnlyProvenanceWithoutVerifiers(t *testing.T) {
	mod := syntheticValidationModule(t, 1)
	mod.Restore = nil
	mod.Verify = nil
	mod = repinValidationModule(t, mod)
	mf := &manifest.Manifest{Version: 1, ConfigModules: []string{mod.ID}}
	context, session := validationPreflightSession(t)
	if err := preflightValidationProductionModule(validationProductionModulePreflight{
		Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: mf, PortableRoot: context.Root(),
	}); err != nil {
		t.Fatalf("verifier-free capture-only projection preflight: %v; isolation=%v", err, session.IsolationError())
	}
}

func TestPreflightValidationProductionModuleRejectsFabricatedCaptureOnlyProvenanceForCapturelessModule(t *testing.T) {
	mod := loadValidationProductionModule(t, "kubectl")
	mf := manifestForValidationModule(mod)
	context, session := validationPreflightSessionFor(t, "kubectl")
	err := preflightValidationProductionModule(validationProductionModulePreflight{
		Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: mf, PortableRoot: context.Root(),
	})
	if !errors.Is(err, validationmode.ErrUnsafePath) || session.IsolationError() == nil || !strings.Contains(session.IsolationError().Error(), "coordinate=configModules") {
		t.Fatalf("captureless fabricated provenance error=%v isolation=%v", err, session.IsolationError())
	}
}

func TestPreflightValidationProductionModuleRejectsInvalidLegacyLaneProvenance(t *testing.T) {
	mod := loadValidationProductionModule(t, "notepad-plus-plus")
	valid := validationMixedLegacyManifest(mod)
	tests := []struct {
		name, coordinate string
		mutate           func(*manifest.Manifest)
	}{
		{name: "missing", coordinate: "legacyConfigLanes", mutate: func(mf *manifest.Manifest) { mf.LegacyConfigLanes = nil }},
		{name: "duplicate", coordinate: "legacyConfigLanes[1]", mutate: func(mf *manifest.Manifest) {
			mf.LegacyConfigLanes = append(mf.LegacyConfigLanes, mf.LegacyConfigLanes[0])
		}},
		{name: "wrong module", coordinate: "legacyConfigLanes[0].moduleId", mutate: func(mf *manifest.Manifest) { mf.LegacyConfigLanes[0].ModuleID = "apps.extra" }},
		{name: "wrong schema", coordinate: "legacyConfigLanes[0].moduleSchemaVersion", mutate: func(mf *manifest.Manifest) { mf.LegacyConfigLanes[0].ModuleSchemaVersion = 2 }},
		{name: "wrong id", coordinate: "legacyConfigLanes[0].captureId", mutate: func(mf *manifest.Manifest) { mf.LegacyConfigLanes[0].CaptureID = "legacy-arbitrary" }},
		{name: "wrong root", coordinate: "legacyConfigLanes[0].payloadRoot", mutate: func(mf *manifest.Manifest) { mf.LegacyConfigLanes[0].PayloadRoot = "configs/arbitrary" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mf := *valid
			mf.Restore = append([]manifest.RestoreEntry(nil), valid.Restore...)
			mf.LegacyConfigLanes = append([]manifest.LegacyConfigLane(nil), valid.LegacyConfigLanes...)
			tt.mutate(&mf)
			context, session := validationPreflightSession(t)
			err := preflightValidationProductionModule(validationProductionModulePreflight{Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: &mf, PortableRoot: context.Root()})
			if !errors.Is(err, validationmode.ErrUnsafePath) {
				t.Fatalf("error = %v", err)
			}
			if got := session.IsolationError(); got == nil || !strings.Contains(got.Error(), "coordinate="+tt.coordinate) {
				t.Fatalf("isolation = %v, want %s", got, tt.coordinate)
			}
		})
	}
}

func TestPreflightValidationProductionModuleValidatesV2CaptureProvenance(t *testing.T) {
	mod := loadValidationProductionModule(t, "owncloud")
	beforeSnapshot := mod.CanonicalSnapshot()
	beforeRevision := mod.Revision
	beforeFingerprints := validationGenerationFingerprints(mod)
	valid := validationConfigCaptureManifest(mod)
	tests := []struct {
		name       string
		coordinate string
		mutate     func(*manifest.Manifest)
	}{
		{name: "valid", mutate: func(*manifest.Manifest) {}},
		{name: "wrong module", coordinate: "configCaptures[0].moduleId", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].ModuleID = "apps.extra" }},
		{name: "unknown set", coordinate: "configCaptures[0].configSetId", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].ConfigSetID = "extra" }},
		{name: "unknown generation", coordinate: "configCaptures[0].sourceGeneration", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].SourceGeneration = "extra" }},
		{name: "wrong fingerprint", coordinate: "configCaptures[0].sourceGenerationFingerprint", mutate: func(mf *manifest.Manifest) {
			mf.ConfigCaptures[0].SourceGenerationFingerprint = strings.Repeat("0", 64)
		}},
		{name: "unknown detector", coordinate: "configCaptures[0].sourceInstance.detectorId", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].SourceInstance.DetectorID = "extra" }},
		{name: "wrong detector evidence", coordinate: "configCaptures[0].sourceInstance.evidence.type", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].SourceInstance.Evidence.Type = "path" }},
		{name: "wrong raw version", coordinate: "configCaptures[0].sourceInstance.rawVersion", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].SourceInstance.RawVersion = "2.5" }},
		{name: "wrong normalized version", coordinate: "configCaptures[0].sourceInstance.normalizedVersion", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].SourceInstance.NormalizedVersion = "2.5" }},
		{name: "wrong evidence backend", coordinate: "configCaptures[0].sourceInstance.evidence", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].SourceInstance.Evidence.Backend = "arbitrary" }},
		{name: "wrong capture id", coordinate: "configCaptures[0].captureId", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].CaptureID = "capture-arbitrary" }},
		{name: "wrong schema", coordinate: "configCaptures[0].captureModule.schemaVersion", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].CaptureModule.SchemaVersion = 1 }},
		{name: "wrong revision", coordinate: "configCaptures[0].captureModule.contentHash", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].CaptureModule.ContentHash = strings.Repeat("0", 64) }},
		{name: "wrong snapshot", coordinate: "configCaptures[0].captureModule.snapshotPath", mutate: func(mf *manifest.Manifest) {
			mf.ConfigCaptures[0].CaptureModule.SnapshotPath = "provenance/modules/arbitrary.json"
		}},
		{name: "wrong payload root", coordinate: "configCaptures[0].payloadRoot", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].PayloadRoot = "configs/arbitrary" }},
		{name: "unsafe payload path", coordinate: "configCaptures[0].payloadManifest[0].relativePath", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].PayloadManifest[0].RelativePath = "../escape" }},
		{name: "invalid payload size", coordinate: "configCaptures[0].payloadManifest[0].size", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].PayloadManifest[0].Size = -1 }},
		{name: "invalid payload hash", coordinate: "configCaptures[0].payloadManifest[0].sha256", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures[0].PayloadManifest[0].SHA256 = "secret-payload" }},
		{name: "duplicate", coordinate: "configCaptures[1].captureId", mutate: func(mf *manifest.Manifest) { mf.ConfigCaptures = append(mf.ConfigCaptures, mf.ConfigCaptures[0]) }},
		{name: "extra", coordinate: "configCaptures[1]", mutate: func(mf *manifest.Manifest) {
			extra := mf.ConfigCaptures[0]
			extra.SourceInstance.ID = "instance-extra"
			extra.CaptureID = bundle.CaptureID(extra.ModuleID, extra.ConfigSetID, extra.SourceInstance.ID)
			extra.PayloadRoot = path.Join("configs", extra.CaptureID)
			mf.ConfigCaptures = append(mf.ConfigCaptures, extra)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mf := *valid
			mf.ConfigCaptures = append([]manifest.ConfigCapture(nil), valid.ConfigCaptures...)
			evidence := *mf.ConfigCaptures[0].SourceInstance.Evidence
			mf.ConfigCaptures[0].SourceInstance.Evidence = &evidence
			mf.ConfigCaptures[0].PayloadManifest = append([]manifest.PayloadManifestEntry(nil), mf.ConfigCaptures[0].PayloadManifest...)
			tt.mutate(&mf)
			context, session := validationPreflightSessionFor(t, "owncloud")
			err := preflightValidationProductionModule(validationProductionModulePreflight{
				Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: &mf, PortableRoot: context.Root(),
				ConfigPlans: []validationProductionConfigPlan{validationConfigPlan(mod)},
			})
			if tt.coordinate == "" {
				if err != nil {
					t.Fatalf("valid v2 provenance: %v", err)
				}
				return
			}
			if !errors.Is(err, validationmode.ErrUnsafePath) {
				t.Fatalf("error = %v, want unsafe path", err)
			}
			if got := session.IsolationError(); got == nil || !strings.Contains(got.Error(), "coordinate="+tt.coordinate) {
				t.Fatalf("isolation error = %v, want coordinate %s", got, tt.coordinate)
			}
		})
	}
	if !bytes.Equal(beforeSnapshot, mod.CanonicalSnapshot()) || beforeRevision != mod.Revision || !validationEqualStrings(beforeFingerprints, validationGenerationFingerprints(mod)) {
		t.Fatal("preflight mutated module snapshot, revision, or generation fingerprints")
	}
}

func TestPreflightValidationProductionModuleAcceptsExactPackageDetectorInstance(t *testing.T) {
	mod := loadValidationProductionModule(t, "owncloud")
	inventory := validationmode.Inventory{AppID: "owncloud", Driver: "winget", Ref: "ownCloud.ownCloudDesktop", DisplayName: "ownCloud", Version: "2.4", InitialState: "present"}
	context, session := validationPreflightSessionWithInventory(t, "owncloud", inventory)
	evidence := modules.PackageEvidence{AppID: inventory.AppID, Backend: inventory.Driver, Platform: "windows", Ref: inventory.Ref, Driver: inventory.Driver, RawVersion: inventory.Version}
	instances, err := modules.DiscoverInstances(mod, []modules.PackageEvidence{evidence}, modules.DiscoveryOptions{})
	if err != nil || len(instances) != 1 {
		t.Fatalf("discover package fixture = %+v, %v", instances, err)
	}
	policies, err := deriveValidationInstancePolicies(context, session, mod, instances)
	if err != nil || len(policies) != 1 || policies[instances[0].ID] != (validationmode.HostPathPolicy{}) {
		t.Fatalf("exact package detector instance rejected: policies=%+v err=%v", policies, err)
	}
}

func TestPreflightValidationProductionModuleRejectsPackageInstanceOutsideDescriptorInventory(t *testing.T) {
	mod := loadValidationProductionModule(t, "owncloud")
	validInventory := validationmode.Inventory{AppID: "owncloud", Driver: "winget", Ref: "ownCloud.ownCloudDesktop", DisplayName: "ownCloud", Version: "2.4", InitialState: "present"}
	validEvidence := modules.PackageEvidence{AppID: validInventory.AppID, Backend: validInventory.Driver, Platform: "windows", Ref: validInventory.Ref, Driver: validInventory.Driver, RawVersion: validInventory.Version}
	valid, err := modules.DiscoverInstances(mod, []modules.PackageEvidence{validEvidence}, modules.DiscoveryOptions{})
	if err != nil || len(valid) != 1 {
		t.Fatalf("discover exact package instance = %+v, %v", valid, err)
	}
	tests := []struct {
		name            string
		mutateInventory func(*validationmode.Inventory)
		mutateInstances func([]modules.ConfigInstance) []modules.ConfigInstance
	}{
		{name: "wrong app", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Evidence.AppID = "foreign"
			return values
		}},
		{name: "wrong backend", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Evidence.Backend = "chocolatey"
			return values
		}},
		{name: "wrong platform", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Evidence.Platform = "linux"
			return values
		}},
		{name: "wrong ref", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Evidence.Ref = "Foreign.App"
			return values
		}},
		{name: "wrong driver", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Evidence.Driver = "chocolatey"
			return values
		}},
		{name: "wrong version", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Version = modules.NewVersionEvidence("2.5")
			return values
		}},
		{name: "absent descriptor", mutateInventory: func(value *validationmode.Inventory) { value.InitialState = "absent" }},
		{name: "path evidence", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Evidence.Path = `C:\\foreign`
			return values
		}},
		{name: "nonempty root", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance {
			values[0].Root = `C:\\foreign`
			return values
		}},
		{name: "duplicate id", mutateInstances: func(values []modules.ConfigInstance) []modules.ConfigInstance { return append(values, values[0]) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory := validInventory
			if test.mutateInventory != nil {
				test.mutateInventory(&inventory)
			}
			instances := append([]modules.ConfigInstance(nil), valid...)
			if test.mutateInstances != nil {
				instances = test.mutateInstances(instances)
			}
			context, session := validationPreflightSessionWithInventory(t, "owncloud", inventory)
			if _, err := deriveValidationInstancePolicies(context, session, mod, instances); !errors.Is(err, validationmode.ErrUnsafePath) {
				t.Fatalf("descriptor mismatch error = %v, want unsafe path", err)
			}
		})
	}
}

func TestPreflightValidationProductionModuleRejectsUnsafePathAndRegistryDialectsDeterministically(t *testing.T) {
	tests := []struct {
		name       string
		authored   string
		coordinate string
		reason     error
		mutate     func(*modules.Module, string)
		setup      func(*testing.T, *validationmode.Context) string
	}{
		{name: "raw drive", authored: `C:\Users\unsafe\settings.json`, coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Matches.PathExists[0] = value }},
		{name: "unc", authored: `\\server\share\settings.json`, coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Matches.PathExists[0] = value }},
		{name: "device", authored: `\\?\C:\unsafe\settings.json`, coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Matches.PathExists[0] = value }},
		{name: "ads", authored: `%APPDATA%\Synthetic\settings.json:secret`, coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Matches.PathExists[0] = value }},
		{name: "traversal", authored: `%APPDATA%\..\escape.json`, coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Matches.PathExists[0] = value }},
		{name: "unresolved", authored: `%UNKNOWN%\settings.json`, coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Matches.PathExists[0] = value }},
		{name: "portable traversal", authored: `..\escape.json`, coordinate: "capture.files[0].dest", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Capture.Files[0].Dest = value }},
		{name: "wrong hive", authored: `HKLM\Software\Sensitive\token`, coordinate: "capture.registryKeys[0].key", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, value string) { mod.Capture.RegistryKeys[0].Key = value }},
		{name: "reparse escape", coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, value string) { mod.Matches.PathExists[0] = value }, setup: func(t *testing.T, context *validationmode.Context) string {
			appData, _ := context.VirtualRoot("APPDATA")
			outside := filepath.Join(filepath.Dir(context.Root()), "outside-reparse-"+filepath.Base(context.Root()))
			if err := os.MkdirAll(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(outside) })
			if err := os.Symlink(outside, filepath.Join(appData, "linked")); err != nil {
				t.Skipf("symlink unavailable in managed sandbox: %v", err)
			}
			return `%APPDATA%\linked\settings.json`
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var findings []string
			for attempt := 0; attempt < 2; attempt++ {
				mod := syntheticValidationModule(t, 1)
				context, session := validationPreflightSession(t)
				authored := tt.authored
				if tt.setup != nil {
					authored = tt.setup(t, context)
				}
				tt.mutate(mod, authored)
				mod = repinValidationModule(t, mod)
				err := preflightValidationProductionModule(validationProductionModulePreflight{
					Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: manifestForValidationModule(mod), PortableRoot: context.Root(),
				})
				if !errors.Is(err, tt.reason) {
					t.Fatalf("preflight error = %v, want %v", err, tt.reason)
				}
				finding := session.IsolationError()
				if finding == nil || !strings.Contains(finding.Error(), "coordinate="+tt.coordinate) {
					t.Fatalf("finding = %v", finding)
				}
				for _, forbidden := range []string{context.Root(), authored, "Sensitive", "token"} {
					if forbidden != "" && strings.Contains(strings.ToLower(finding.Error()), strings.ToLower(forbidden)) {
						t.Fatalf("finding leaked %q: %v", forbidden, finding)
					}
				}
				findings = append(findings, finding.Error())
			}
			if findings[0] != findings[1] {
				t.Fatalf("findings are not deterministic:\n%s\n%s", findings[0], findings[1])
			}
		})
	}
}

func TestPreflightValidationProductionModuleDerivesInstanceRootsAndChecksSuppliedTargets(t *testing.T) {
	mod := syntheticValidationModule(t, 2)
	mod.Config.Sets[0].Generations[0].Capture.Files[0].Source = `${instance.root}\settings.json`
	mod.Config.Sets[0].Generations[0].Restore[0].Target = `${instance.root}\settings.json`
	mod = repinValidationModule(t, mod)
	context, session := validationPreflightSession(t)
	virtualAppData, _ := context.VirtualRoot("APPDATA")
	instanceRoot := filepath.Join(virtualAppData, "Synthetic-One")
	if err := os.MkdirAll(instanceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	originalAppData, set, managed := context.OriginalEnvironment("APPDATA")
	if !set || !managed {
		t.Fatal("APPDATA original provenance was not retained")
	}
	if err := os.MkdirAll(filepath.Join(originalAppData, "Synthetic-One"), 0o700); err != nil {
		t.Fatal(err)
	}
	materialized := filepath.Join(virtualAppData, "materialized.json")
	if err := os.WriteFile(materialized, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := &recordingValidationFilesystemGuard{}
	session.filesystemGuard = guard
	locator := validationPathInstanceLocator(instanceRoot)
	instance := modules.ConfigInstance{
		ID: modules.StableInstanceID(mod.ID, "profiles", locator), ModuleID: mod.ID, DetectorID: "profiles", Root: instanceRoot,
		Evidence: modules.InstanceEvidence{Type: "path", Path: instanceRoot}, CanonicalLocator: locator,
	}
	input := validationProductionModulePreflight{
		Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod}, Manifest: manifestForValidationModule(mod), PortableRoot: context.Root(),
		Instances:      []modules.ConfigInstance{instance},
		HostTargets:    []validationProductionHostTarget{{Coordinate: "planning.host", Authored: `${instance.root}\future.json`, InstanceID: instance.ID}},
		SandboxTargets: []validationProductionSandboxTarget{{Coordinate: "planning.materialized", Path: materialized}},
	}
	if err := preflightValidationProductionModule(input); err != nil {
		t.Fatalf("derived instance preflight: %v", err)
	}
	wantProtected := filepath.Join(originalAppData, "Synthetic-One", "settings.json")
	if !guard.protectedPath(wantProtected) {
		t.Fatalf("protected paths = %v, want %s", guard.paths, wantProtected)
	}

	t.Run("arbitrary instance root", func(t *testing.T) {
		outside := t.TempDir()
		copy := input
		copySession := newValidationModeSession(context, newValidationIsolationRecorder(context.Descriptor()))
		copy.Session = copySession
		copy.Instances = []modules.ConfigInstance{{ID: instance.ID, ModuleID: mod.ID, DetectorID: "profiles", Root: outside, Evidence: modules.InstanceEvidence{Type: "path", Path: outside}}}
		if err := preflightValidationProductionModule(copy); !errors.Is(err, validationmode.ErrUnsafePath) {
			t.Fatalf("error = %v", err)
		}
		if got := copySession.IsolationError(); got == nil || !strings.Contains(got.Error(), "coordinate=instances[0].root") {
			t.Fatalf("isolation = %v", got)
		}
	})

	t.Run("arbitrary instance identity", func(t *testing.T) {
		copy := input
		copySession := newValidationModeSession(context, newValidationIsolationRecorder(context.Descriptor()))
		copy.Session = copySession
		arbitrary := instance
		arbitrary.ID = "instance-arbitrary"
		copy.Instances = []modules.ConfigInstance{arbitrary}
		copy.HostTargets = nil
		if err := preflightValidationProductionModule(copy); !errors.Is(err, validationmode.ErrUnsafePath) {
			t.Fatalf("error = %v", err)
		}
		if got := copySession.IsolationError(); got == nil || !strings.Contains(got.Error(), "coordinate=instances[0].id") {
			t.Fatalf("isolation = %v", got)
		}
	})

	t.Run("materialized outside sandbox", func(t *testing.T) {
		copy := input
		copySession := newValidationModeSession(context, newValidationIsolationRecorder(context.Descriptor()))
		copy.Session = copySession
		copy.SandboxTargets = []validationProductionSandboxTarget{{Coordinate: "planning.materialized", Path: originalAppData}}
		if err := preflightValidationProductionModule(copy); !errors.Is(err, validationmode.ErrUnsafePath) {
			t.Fatalf("error = %v", err)
		}
		if got := copySession.IsolationError(); got == nil || !strings.Contains(got.Error(), "coordinate=planning.materialized") {
			t.Fatalf("isolation = %v", got)
		}
	})

	t.Run("guard registration after seal poisons session", func(t *testing.T) {
		copy := input
		copySession := newValidationModeSession(context, newValidationIsolationRecorder(context.Descriptor()))
		copySession.sealIsolation()
		copy.Session = copySession
		if err := preflightValidationProductionModule(copy); !errors.Is(err, validationmode.ErrUnsafePath) {
			t.Fatalf("error = %v", err)
		}
		if copySession.IsolationError() == nil {
			t.Fatal("sealed registration did not poison session")
		}
	})
}

func TestPreflightValidationProductionModuleUsesDiscoverInstancesVersionSemantics(t *testing.T) {
	tests := []struct {
		name, directory, coordinate string
		mutate                      func(*modules.ConfigInstance)
		wantDiscovered              bool
	}{
		{name: "exact discovered instance", directory: "Studio One 7", wantDiscovered: true},
		{name: "basename fails version pattern", directory: "Studio One current", coordinate: "instances[0].id"},
		{name: "forged raw version", directory: "Studio One 7", coordinate: "instances[0].version", wantDiscovered: true, mutate: func(instance *modules.ConfigInstance) {
			instance.Version = modules.NewVersionEvidence("8")
		}},
		{name: "forged normalized version", directory: "Studio One 7", coordinate: "instances[0].version", wantDiscovered: true, mutate: func(instance *modules.ConfigInstance) {
			instance.Version.Normalized = "07"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog, err := modules.GetCatalog(filepath.Join("..", "..", ".."))
			if err != nil {
				t.Fatal(err)
			}
			mod := catalog["apps.studio-one"]
			if mod == nil {
				t.Fatal("production catalog missing studio-one")
			}
			beforeSnapshot := mod.CanonicalSnapshot()
			context, session := validationPreflightSessionFor(t, "studio-one")
			virtualAppData, _ := context.VirtualRoot("APPDATA")
			root := filepath.Join(virtualAppData, "PreSonus", tt.directory)
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			originalAppData, _, _ := context.OriginalEnvironment("APPDATA")
			if err := os.MkdirAll(filepath.Join(originalAppData, "PreSonus", tt.directory), 0o700); err != nil {
				t.Fatal(err)
			}
			discovered, err := modules.DiscoverInstances(mod, nil, modules.DiscoveryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantDiscovered && len(discovered) != 1 {
				t.Fatalf("discovered = %+v", discovered)
			}
			if !tt.wantDiscovered && len(discovered) != 0 {
				t.Fatalf("discovered = %+v, want none", discovered)
			}
			var instance modules.ConfigInstance
			if len(discovered) == 1 {
				instance = discovered[0]
			} else {
				locator := validationPathInstanceLocator(root)
				instance = modules.ConfigInstance{ID: modules.StableInstanceID(mod.ID, "versions", locator), ModuleID: mod.ID, DetectorID: "versions", Root: root, CanonicalLocator: locator, Evidence: modules.InstanceEvidence{Type: "path", Path: root}}
			}
			if tt.mutate != nil {
				tt.mutate(&instance)
			}
			err = preflightValidationProductionModule(validationProductionModulePreflight{
				Context: context, Session: session, Catalog: catalog, Modules: []*modules.Module{mod}, Manifest: &manifest.Manifest{Version: 2},
				PortableRoot: context.Root(), Instances: []modules.ConfigInstance{instance},
			})
			if tt.coordinate == "" {
				if err != nil {
					t.Fatalf("exact discovered instance: %v", err)
				}
			} else {
				if !errors.Is(err, validationmode.ErrUnsafePath) {
					t.Fatalf("error = %v, want unsafe path", err)
				}
				if got := session.IsolationError(); got == nil || !strings.Contains(got.Error(), "coordinate="+tt.coordinate) {
					t.Fatalf("isolation = %v", got)
				}
			}
			if !bytes.Equal(beforeSnapshot, mod.CanonicalSnapshot()) {
				t.Fatal("preflight mutated production module")
			}
		})
	}
}

func TestPreflightValidationProductionModuleAcceptsTildeDetectorGlob(t *testing.T) {
	mod := syntheticValidationModule(t, 2)
	mod.Config.InstanceDetectors[0].Glob = `~/Vendor/*`
	mod.Config.Sets[0].Generations[0].Capture.Files[0].Source = `${instance.root}\settings.json`
	mod.Config.Sets[0].Generations[0].Restore[0].Target = `${instance.root}\settings.json`
	mod = repinValidationModule(t, mod)

	originalUserProfile := t.TempDir()
	t.Setenv("USERPROFILE", originalUserProfile)
	context, session := validationPreflightSession(t)
	virtualUserProfile, _ := context.VirtualRoot("USERPROFILE")
	instanceRoot := filepath.Join(virtualUserProfile, "Vendor", "Profile")
	if err := os.MkdirAll(instanceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(originalUserProfile, "Vendor", "Profile"), 0o700); err != nil {
		t.Fatal(err)
	}
	discovered, err := modules.DiscoverInstances(mod, nil, modules.DiscoveryOptions{Glob: func(pattern string) ([]string, error) {
		if pattern != `~/Vendor/*` {
			t.Fatalf("discovery glob = %q", pattern)
		}
		return []string{instanceRoot}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("discovered = %+v", discovered)
	}
	guard := &recordingValidationFilesystemGuard{}
	session.filesystemGuard = guard
	if err := preflightValidationProductionModule(validationProductionModulePreflight{
		Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod},
		Manifest: manifestForValidationModule(mod), PortableRoot: context.Root(), Instances: discovered,
	}); err != nil {
		t.Fatalf("tilde detector preflight: %v", err)
	}
	wantProtected := filepath.Join(originalUserProfile, "Vendor", "Profile", "settings.json")
	if !guard.protectedPath(wantProtected) {
		t.Fatalf("protected paths = %v, want %s", guard.paths, wantProtected)
	}
}

func TestPreflightValidationProductionModuleRejectsInvalidTildeDetectorGlobs(t *testing.T) {
	for _, glob := range []string{"~", `~/Vendor/[`, `~/Vendor/**`} {
		t.Run(glob, func(t *testing.T) {
			mod := syntheticValidationModule(t, 2)
			mod.Config.InstanceDetectors[0].Glob = glob
			mod.Config.Sets[0].Generations[0].Capture.Files[0].Source = `${instance.root}\settings.json`
			mod.Config.Sets[0].Generations[0].Restore[0].Target = `${instance.root}\settings.json`
			mod = repinValidationModule(t, mod)
			originalUserProfile := t.TempDir()
			t.Setenv("USERPROFILE", originalUserProfile)
			context, session := validationPreflightSession(t)
			virtualUserProfile, _ := context.VirtualRoot("USERPROFILE")
			root := filepath.Join(virtualUserProfile, "Vendor", "Profile")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			locator := validationPathInstanceLocator(root)
			instance := modules.ConfigInstance{
				ID: modules.StableInstanceID(mod.ID, "profiles", locator), ModuleID: mod.ID, DetectorID: "profiles", Root: root,
				Evidence: modules.InstanceEvidence{Type: "path", Path: root}, CanonicalLocator: locator,
			}
			err := preflightValidationProductionModule(validationProductionModulePreflight{
				Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod},
				Manifest: manifestForValidationModule(mod), PortableRoot: context.Root(), Instances: []modules.ConfigInstance{instance},
			})
			if !errors.Is(err, validationmode.ErrUnsafePath) {
				t.Fatalf("glob %q error = %v, want unsafe path", glob, err)
			}
		})
	}
}

type recordingValidationFilesystemGuard struct {
	paths  []validationmode.ProtectedPath
	sealed bool
}

func (guard *recordingValidationFilesystemGuard) Protect(paths []validationmode.ProtectedPath) error {
	if guard.sealed {
		return errors.New("sealed")
	}
	guard.paths = append(guard.paths, paths...)
	return nil
}
func (guard *recordingValidationFilesystemGuard) Seal()                             { guard.sealed = true }
func (*recordingValidationFilesystemGuard) Check() ([]validationmode.Change, error) { return nil, nil }
func (*recordingValidationFilesystemGuard) Label(string) string                     { return "" }
func (guard *recordingValidationFilesystemGuard) protectedPath(path string) bool {
	for _, protected := range guard.paths {
		if filepath.Clean(protected.Path) == filepath.Clean(path) {
			return true
		}
	}
	return false
}

func TestPreflightValidationProductionModuleRequiresExactTargetAndManifestContracts(t *testing.T) {
	mod := loadValidationProductionModule(t, "notepad-plus-plus")
	beforeSnapshot := mod.CanonicalSnapshot()
	beforeRevision := mod.Revision
	mf := manifestForValidationModule(mod)

	context, session := validationPreflightSession(t)
	err := preflightValidationProductionModule(validationProductionModulePreflight{
		Context:      context,
		Session:      session,
		Catalog:      validationCatalog(mod),
		Modules:      []*modules.Module{mod},
		Manifest:     mf,
		PortableRoot: context.Root(),
	})
	if err != nil {
		t.Fatalf("valid production module preflight: %v", err)
	}
	if !bytes.Equal(mod.CanonicalSnapshot(), beforeSnapshot) || mod.Revision != beforeRevision {
		t.Fatal("preflight mutated the production module snapshot or revision")
	}
}

func TestPreflightValidationProductionModuleRejectsNonExactProvenance(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*modules.Module, *validationProductionModulePreflight)
		wantCoordinate string
	}{
		{name: "missing selected module", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Modules = nil
		}, wantCoordinate: "modules"},
		{name: "duplicate selected module", mutate: func(mod *modules.Module, input *validationProductionModulePreflight) {
			input.Modules = []*modules.Module{mod, mod}
		}, wantCoordinate: "modules"},
		{name: "extra selected module", mutate: func(mod *modules.Module, input *validationProductionModulePreflight) {
			extra := *mod
			extra.ID = "apps.extra"
			input.Modules = []*modules.Module{mod, &extra}
		}, wantCoordinate: "modules"},
		{name: "wrong selected module", mutate: func(mod *modules.Module, input *validationProductionModulePreflight) {
			wrong := *mod
			wrong.ID = "apps.extra"
			input.Modules = []*modules.Module{&wrong}
		}, wantCoordinate: "modules"},
		{name: "missing config module", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.ConfigModules = nil
		}, wantCoordinate: "configModules"},
		{name: "duplicate config module", mutate: func(mod *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.ConfigModules = append(input.Manifest.ConfigModules, mod.ID)
		}, wantCoordinate: "configModules[1]"},
		{name: "extra config module", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.ConfigModules = append(input.Manifest.ConfigModules, "apps.extra")
		}, wantCoordinate: "configModules[1]"},
		{name: "missing restore", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.Restore = input.Manifest.Restore[1:]
		}, wantCoordinate: "restore"},
		{name: "duplicate restore", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.Restore = append(input.Manifest.Restore, input.Manifest.Restore[0])
		}, wantCoordinate: "restore[6]"},
		{name: "arbitrary restore", mutate: func(mod *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.Restore = append(input.Manifest.Restore, manifest.RestoreEntry{
				Type: "registry-set", FromModule: mod.ID, Key: `HKCU\Software\Arbitrary`,
				ValueName: "token", Data: "registry-value-secret",
			})
		}, wantCoordinate: "restore[6]"},
		{name: "wrong restore module", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.Restore[0].FromModule = "apps.extra"
		}, wantCoordinate: "restore[0].fromModule"},
		{name: "missing verify", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.Verify = nil
		}, wantCoordinate: "verify"},
		{name: "duplicate verify", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.Verify = append(input.Manifest.Verify, input.Manifest.Verify[0])
		}, wantCoordinate: "verify[1]"},
		{name: "arbitrary verify", mutate: func(_ *modules.Module, input *validationProductionModulePreflight) {
			input.Manifest.Verify = append(input.Manifest.Verify, manifest.VerifyEntry{
				Type: "registry-value-equals", Path: `HKCU\Software\Arbitrary`,
				ValueName: "token", Data: "registry-value-secret",
			})
		}, wantCoordinate: "verify[1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := loadValidationProductionModule(t, "notepad-plus-plus")
			context, session := validationPreflightSession(t)
			input := validationProductionModulePreflight{
				Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod},
				Manifest: manifestForValidationModule(mod), PortableRoot: context.Root(),
			}
			tt.mutate(mod, &input)
			err := preflightValidationProductionModule(input)
			if !errors.Is(err, validationmode.ErrUnsafePath) {
				t.Fatalf("preflight error = %v, want unsafe path", err)
			}
			isolationErr := session.IsolationError()
			if isolationErr == nil || !strings.Contains(isolationErr.Error(), "coordinate="+tt.wantCoordinate) {
				t.Fatalf("isolation error = %v, want coordinate %s", isolationErr, tt.wantCoordinate)
			}
			for _, forbidden := range []string{context.Root(), "registry-value-secret", `HKCU\Software\Arbitrary`} {
				if strings.Contains(strings.ToLower(isolationErr.Error()), strings.ToLower(forbidden)) {
					t.Fatalf("isolation error leaked %q: %v", forbidden, isolationErr)
				}
			}
		})
	}
}

func TestPreflightValidationProductionModuleRoutesEveryDeclarationField(t *testing.T) {
	host := `relative\host`
	portable := `%APPDATA%\not-portable`
	wrongHive := `HKLM\Software\Unsafe`
	tests := []struct {
		name       string
		schema     int
		coordinate string
		reason     error
		mutate     func(*modules.Module, *manifest.Manifest)
	}{
		{name: "v1 match path", coordinate: "matches.pathExists[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Matches.PathExists[0] = host }},
		{name: "v1 capture file source", coordinate: "capture.files[0].source", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Capture.Files[0].Source = host }},
		{name: "v1 capture file destination", coordinate: "capture.files[0].dest", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Capture.Files[0].Dest = portable }},
		{name: "v1 capture registry key", coordinate: "capture.registryKeys[0].key", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Capture.RegistryKeys[0].Key = wrongHive }},
		{name: "v1 capture registry destination", coordinate: "capture.registryKeys[0].dest", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Capture.RegistryKeys[0].Dest = portable }},
		{name: "v1 capture registry value", coordinate: "capture.registryValues[0].key", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Capture.RegistryValues[0].Key = wrongHive }},
		{name: "v1 secret file", coordinate: "secrets.files[0]", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Secrets.Files[0] = host }},
		{name: "v1 restore source", coordinate: "restore[0].source", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, mf *manifest.Manifest) {
			mod.Restore[0].Source = portable
			mf.Restore = manifestForValidationModule(mod).Restore
		}},
		{name: "v1 restore target", coordinate: "restore[0].target", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, mf *manifest.Manifest) {
			mod.Restore[0].Target = host
			mf.Restore = manifestForValidationModule(mod).Restore
		}},
		{name: "v1 restore registry", coordinate: "restore[1].key", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, mf *manifest.Manifest) {
			mod.Restore[1].Key = wrongHive
			mf.Restore = manifestForValidationModule(mod).Restore
		}},
		{name: "v1 verify file", coordinate: "verify[0].path", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, mf *manifest.Manifest) {
			mod.Verify[0].Path = host
			mf.Verify = manifestForValidationModule(mod).Verify
		}},
		{name: "v1 verify registry", coordinate: "verify[1].path", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, mf *manifest.Manifest) {
			mod.Verify[1].Path = wrongHive
			mf.Verify = manifestForValidationModule(mod).Verify
		}},
		{name: "manifest manual verify", coordinate: "apps[0].manual.verifyPath", reason: validationmode.ErrUnsafePath, mutate: func(_ *modules.Module, mf *manifest.Manifest) {
			mf.Apps = []manifest.App{{ID: "manual", Manual: &manifest.ManualApp{VerifyPath: host}}}
		}},

		{name: "v2 detector glob", schema: 2, coordinate: "config.instanceDetectors[0].glob", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) { mod.Config.InstanceDetectors[0].Glob = host }},
		{name: "v2 capture file source", schema: 2, coordinate: "config.sets[0].generations[0].capture.files[0].source", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Capture.Files[0].Source = host
		}},
		{name: "v2 capture file destination", schema: 2, coordinate: "config.sets[0].generations[0].capture.files[0].dest", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Capture.Files[0].Dest = portable
		}},
		{name: "v2 capture registry key", schema: 2, coordinate: "config.sets[0].generations[0].capture.registryKeys[0].key", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Capture.RegistryKeys[0].Key = wrongHive
		}},
		{name: "v2 capture registry destination", schema: 2, coordinate: "config.sets[0].generations[0].capture.registryKeys[0].dest", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Capture.RegistryKeys[0].Dest = portable
		}},
		{name: "v2 capture registry value", schema: 2, coordinate: "config.sets[0].generations[0].capture.registryValues[0].key", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Capture.RegistryValues[0].Key = wrongHive
		}},
		{name: "v2 restore source", schema: 2, coordinate: "config.sets[0].generations[0].restore[0].source", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Restore[0].Source = portable
		}},
		{name: "v2 restore target", schema: 2, coordinate: "config.sets[0].generations[0].restore[0].target", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Restore[0].Target = host
		}},
		{name: "v2 restore registry", schema: 2, coordinate: "config.sets[0].generations[0].restore[1].key", reason: validationmode.ErrUnsafeRegistry, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Restore[1].Key = wrongHive
		}},
		{name: "v2 generation validation stays portable", schema: 2, coordinate: "config.sets[0].generations[0].validate[0].path", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Generations[0].Validate[0].Path = portable
		}},
		{name: "v2 migration file source", schema: 2, coordinate: "config.sets[0].migrations[0].operations[0].source", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Migrations[0].Operations[0].Source = portable
		}},
		{name: "v2 migration file target", schema: 2, coordinate: "config.sets[0].migrations[0].operations[0].target", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Migrations[0].Operations[0].Target = portable
		}},
		{name: "v2 migration file delete path", schema: 2, coordinate: "config.sets[0].migrations[0].operations[1].path", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Migrations[0].Operations[1].Path = portable
		}},
		{name: "v2 migration json path", schema: 2, coordinate: "config.sets[0].migrations[0].operations[2].path", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Migrations[0].Operations[2].Path = portable
		}},
		{name: "v2 migration ini path", schema: 2, coordinate: "config.sets[0].migrations[0].operations[3].path", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Migrations[0].Operations[3].Path = portable
		}},
		{name: "v2 migration validation stays portable", schema: 2, coordinate: "config.sets[0].migrations[0].validate[0].path", reason: validationmode.ErrUnsafePath, mutate: func(mod *modules.Module, _ *manifest.Manifest) {
			mod.Config.Sets[0].Migrations[0].Validate[0].Path = portable
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := syntheticValidationModule(t, tt.schema)
			mf := manifestForValidationModule(mod)
			tt.mutate(mod, mf)
			mod = repinValidationModule(t, mod)
			context, session := validationPreflightSession(t)
			err := preflightValidationProductionModule(validationProductionModulePreflight{
				Context: context, Session: session, Catalog: validationCatalog(mod), Modules: []*modules.Module{mod},
				Manifest: mf, PortableRoot: context.Root(),
			})
			if !errors.Is(err, tt.reason) {
				t.Fatalf("preflight error = %v, want %v", err, tt.reason)
			}
			if isolationErr := session.IsolationError(); isolationErr == nil || !strings.Contains(isolationErr.Error(), "coordinate="+tt.coordinate) {
				t.Fatalf("isolation error = %v, want coordinate %s", isolationErr, tt.coordinate)
			}
		})
	}
}

func syntheticValidationModule(t *testing.T, schema int) *modules.Module {
	mod := &modules.Module{
		ID: "apps.notepad-plus-plus", DisplayName: "Synthetic", Matches: modules.MatchCriteria{
			PathExists: []string{`%APPDATA%\Synthetic\settings.json`},
		},
		Capture: &modules.CaptureDef{
			Files:          []modules.CaptureFile{{Source: `%APPDATA%\Synthetic\settings.json`, Dest: "payload/settings.json"}},
			RegistryKeys:   []modules.CaptureRegistryKey{{Key: `HKCU\Software\Synthetic`, Dest: "payload/settings.reg"}},
			RegistryValues: []modules.CaptureRegistryValue{{Key: `HKCU\Software\Synthetic`, ValueName: "Theme"}},
		},
		Secrets: &modules.SecretsDef{Files: []string{`%APPDATA%\Synthetic\secret.txt`}},
		Restore: []modules.RestoreDef{
			{Type: "copy", Source: "payload/settings.json", Target: `%APPDATA%\Synthetic\settings.json`},
			{Type: "registry-set", Key: `HKCU\Software\Synthetic`, ValueName: "Theme"},
		},
		Verify: []modules.VerifyDef{
			{Type: "file-exists", Path: `%APPDATA%\Synthetic\settings.json`},
			{Type: "registry-key-exists", Path: `HKCU\Software\Synthetic`},
		},
	}
	if schema != 2 {
		return repinValidationModule(t, mod)
	}
	mod.ModuleSchemaVersion = 2
	mod.Config = &modules.ConfigDef{
		InstanceDetectors: []modules.InstanceDetectorDef{{ID: "profiles", Type: "path", Glob: `%APPDATA%\Synthetic-*`}},
		Sets: []modules.ConfigSetDef{{
			ID: "preferences",
			Generations: []modules.GenerationDef{{
				ID: "g1", Order: 1,
				Capture: &modules.CaptureDef{
					Files:          []modules.CaptureFile{{Source: `%APPDATA%\Synthetic\v2.json`, Dest: "settings/v2.json"}},
					RegistryKeys:   []modules.CaptureRegistryKey{{Key: `HKCU\Software\SyntheticV2`, Dest: "settings/v2.reg"}},
					RegistryValues: []modules.CaptureRegistryValue{{Key: `HKCU\Software\SyntheticV2`, ValueName: "Theme"}},
				},
				Restore: []modules.RestoreDef{
					{Type: "copy", Source: "settings/v2.json", Target: `%APPDATA%\Synthetic\v2.json`},
					{Type: "registry-set", Key: `HKCU\Software\SyntheticV2`, ValueName: "Theme"},
				},
				Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings/v2.json"}},
			}},
			Migrations: []modules.MigrationEdgeDef{{
				From: "g0", To: "g1",
				Operations: []modules.MigrationOperationDef{
					{Type: "file-copy", Source: "old.json", Target: "settings/v2.json"},
					{Type: "file-delete", Path: "obsolete.json"},
					{Type: "json-set", Path: "settings/v2.json"},
					{Type: "ini-set", Path: "settings/v2.ini"},
				},
				Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings/v2.json"}},
			}},
		}},
	}
	return repinValidationModule(t, mod)
}

func loadValidationProductionModule(t *testing.T, shortID string) *modules.Module {
	t.Helper()
	path := filepath.Join("..", "..", "..", "modules", "apps", shortID, "module.jsonc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	mod.FilePath = path
	mod.ModuleDir = filepath.Dir(path)
	return mod
}

func validationCatalog(mod *modules.Module) map[string]*modules.Module {
	return map[string]*modules.Module{mod.ID: mod}
}

func repinValidationModule(t *testing.T, mod *modules.Module) *modules.Module {
	t.Helper()
	data, err := json.Marshal(mod)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := modules.ParseModuleJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return pinned
}

func manifestForValidationModule(mod *modules.Module) *manifest.Manifest {
	mf := &manifest.Manifest{Version: 1, ConfigModules: []string{mod.ID}}
	for _, restore := range mod.Restore {
		mf.Restore = append(mf.Restore, manifest.RestoreEntry{
			Type: restore.Type, Source: restore.Source, Target: restore.Target,
			Pattern: restore.Pattern, Reason: restore.Reason, Backup: restore.Backup,
			Optional: restore.Optional, Exclude: append([]string(nil), restore.Exclude...),
			FromModule: mod.ID, Key: restore.Key, ValueName: restore.ValueName,
			ValueType: restore.ValueType, Data: restore.Data,
		})
	}
	for _, verify := range mod.Verify {
		mf.Verify = append(mf.Verify, manifest.VerifyEntry{
			Type: verify.Type, Command: verify.Command, Path: verify.Path,
			ValueName: verify.ValueName, ValueType: verify.ValueType, Data: verify.Data,
		})
	}
	return mf
}

func validationConfigCaptureManifest(mod *modules.Module) *manifest.Manifest {
	set := &mod.Config.Sets[0]
	generation := &set.Generations[0]
	instanceID := "instance-installed"
	captureID := bundle.CaptureID(mod.ID, set.ID, instanceID)
	return &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{{
		CaptureID: captureID, ModuleID: mod.ID, ConfigSetID: set.ID,
		SourceInstance: manifest.ConfigSourceInstance{
			ID: instanceID, DetectorID: "installed", RawVersion: "2.4", NormalizedVersion: "2.4",
			Evidence: &manifest.ConfigSourceInstanceEvidence{Type: "package", Backend: "winget", Ref: "ownCloud.ownCloudDesktop"},
		},
		SourceGeneration: generation.ID, SourceGenerationFingerprint: generation.Fingerprint,
		CaptureModule: manifest.CaptureModuleProvenance{
			SchemaVersion: 2, ContentHash: mod.Revision,
			SnapshotPath: path.Join("provenance", "modules", mod.ID+"-"+mod.Revision+".json"),
		},
		PayloadRoot:     path.Join("configs", captureID),
		PayloadManifest: []manifest.PayloadManifestEntry{{RelativePath: "owncloud.cfg", Size: 1, SHA256: strings.Repeat("a", 64)}},
	}}}
}

func validationMixedLegacyManifest(mod *modules.Module) *manifest.Manifest {
	mf := manifestForValidationModule(mod)
	mf.Version = 2
	layoutID := bundle.LegacyCaptureID(mod.ID)
	mf.LegacyConfigLanes = []manifest.LegacyConfigLane{{CaptureID: layoutID, ModuleID: mod.ID, ModuleSchemaVersion: 1, PayloadRoot: path.Join("configs", layoutID)}}
	for index := range mf.Restore {
		mf.Restore[index].Source = projectedLegacySource(mf.Restore[index].Source, layoutID)
		mf.Restore[index].LegacyCaptureID = layoutID
	}
	return mf
}

func validationConfigPlan(mod *modules.Module) validationProductionConfigPlan {
	return validationProductionConfigPlan{
		SetID: "preferences", GenerationID: "g1",
		Instance: modules.ConfigInstance{
			ID: "instance-installed", ModuleID: mod.ID, DetectorID: "installed",
			Version:  modules.NewVersionEvidence("2.4"),
			Evidence: modules.InstanceEvidence{Type: "package", Backend: "winget", Ref: "ownCloud.ownCloudDesktop"},
		},
	}
}

func validationGenerationFingerprints(mod *modules.Module) []string {
	var result []string
	if mod.Config == nil {
		return result
	}
	for _, set := range mod.Config.Sets {
		for _, generation := range set.Generations {
			result = append(result, generation.Fingerprint)
		}
	}
	return result
}

func validationEqualStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func projectedLegacySource(source, layoutID string) string {
	normalized := strings.ReplaceAll(source, `\`, "/")
	prefix := "./payload/apps/"
	if !strings.HasPrefix(normalized, prefix) {
		return source
	}
	remainder := strings.TrimPrefix(normalized, prefix)
	parts := strings.SplitN(remainder, "/", 2)
	leaf := parts[0]
	if len(parts) == 2 {
		leaf = path.Base(parts[1])
	}
	return "./configs/" + layoutID + "/" + leaf
}

func validationPreflightSession(t *testing.T) (*validationmode.Context, *ValidationModeSession) {
	return validationPreflightSessionFor(t, "notepad-plus-plus")
}

func validationPreflightSessionFor(t *testing.T, shortID string) (*validationmode.Context, *ValidationModeSession) {
	return validationPreflightSessionWithInventory(t, shortID, validationmode.Inventory{AppID: shortID, Driver: "winget", Ref: "Synthetic.Ref", DisplayName: shortID, InitialState: "absent"})
}

func validationPreflightSessionWithInventory(t *testing.T, shortID string, inventory validationmode.Inventory) (*validationmode.Context, *ValidationModeSession) {
	t.Helper()
	originalAppData := t.TempDir()
	t.Setenv("APPDATA", originalAppData)
	root, err := os.MkdirTemp(os.TempDir(), "endstate-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: "commands-validation",
		Nonce: strings.TrimPrefix(filepath.Base(root), "endstate-validation-"), ModuleID: "apps." + shortID,
		Inventory: inventory,
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	context, err := validationmode.LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	restore, err := context.Activate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restore() })
	recorder := newValidationIsolationRecorder(context.Descriptor())
	session := newValidationModeSession(context, recorder)
	// These tests prove declaration routing and provenance, not native HKCU
	// availability. Keep them deterministic on restricted Windows sandboxes;
	// RegistryGuard itself has dedicated native-boundary coverage.
	session.registryGuard = &countingRegistryIsolationGuard{}
	return context, session
}
