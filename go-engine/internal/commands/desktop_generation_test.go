// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

const desktopClockModule = `{"moduleSchemaVersion":3,"id":"desktop.clock","displayName":"Clock preference","sensitivity":"low","platforms":{"linux":{"realization":"endstate-restore","config":{"instanceDetectors":[{"id":"session","type":"desktop","desktop":{"schema":"org.gnome.desktop.interface","path":"/org/gnome/desktop/interface/","key":"clock-show-date","type":"boolean"}}],"sets":[{"id":"preferences","generations":[{"id":"g1","order":1,"capture":{"desktopValues":[{"target":{"schema":"org.gnome.desktop.interface","path":"/org/gnome/desktop/interface/","key":"clock-show-date","type":"boolean"},"dest":"clock.json"}]},"restore":[{"type":"desktop-value","source":"clock.json","desktop":{"schema":"org.gnome.desktop.interface","path":"/org/gnome/desktop/interface/","key":"clock-show-date","type":"boolean"}}]}]}]}}}}`

// This workflow catches desktop declarations being lost before generation collection.
func TestDesktopClockGenerationCaptureWorkflow(t *testing.T) {
	for _, capturePresent := range []bool{true, false} {
		t.Run(map[bool]string{true: "explicit-false", false: "inherited"}[capturePresent], func(t *testing.T) {

			root := t.TempDir()
			target := desktopsettings.Target{Schema: "org.gnome.desktop.interface", Path: "/org/gnome/desktop/interface/", Key: "clock-show-date", Type: "boolean"}
			falseValue := false
			desired := desktopsettings.Value{Type: "boolean", Present: capturePresent}
			if capturePresent {
				desired.Value = &falseValue
			}
			prior := desktopsettings.Value{Type: "boolean", Present: !capturePresent}
			if !capturePresent {
				prior.Value = &falseValue
			}
			backend := &clockBackend{scope: "fixture/source", values: map[string]desktopsettings.Value{target.Identity(): desired, "unrelated": {Type: "boolean", Present: true, Value: &falseValue}}}
			oldBackend := desktopSettingsBackend
			desktopSettingsBackend = backend
			t.Cleanup(func() { desktopSettingsBackend = oldBackend })
			dir := filepath.Join(root, "modules", "desktop", "clock")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "module.jsonc"), []byte(desktopClockModule), 0600); err != nil {
				t.Fatal(err)
			}
			oldRoot, oldGOOS, oldRealizer, oldDiscovery, oldLoader := resolveRepoRootFn, captureGOOSFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn
			t.Cleanup(func() {
				resolveRepoRootFn, captureGOOSFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn = oldRoot, oldGOOS, oldRealizer, oldDiscovery, oldLoader
			})
			resolveRepoRootFn = func() string { return root }
			captureGOOSFn = func() string { return "linux" }
			newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
			discoverLinuxFn = func(context.Context, discovery.Request, realizer.Realizer) (discovery.Result, error) {
				return discovery.Result{Counts: discovery.Counts{Discovered: 1, Selected: 1, Settings: 1, SettingsOnly: 1}, Settings: []discovery.SettingsCandidate{{ID: "desktop.clock", ModuleID: "desktop.clock", DisplayName: "Clock preference", Portability: discovery.PortabilityConfigOnly, Actions: []discovery.SettingsAction{discovery.SettingsCapture}}}}, nil
			}
			loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
			catalog, diagnostics, catalogErr := modules.GetCatalogWithDiagnostics(root)
			if catalogErr != nil || len(diagnostics) != 0 || len(catalog) != 1 {
				t.Fatalf("desktop catalog: %v %+v %+v", catalogErr, diagnostics, catalog)
			}
			raw, err := RunCapture(CaptureFlags{Out: filepath.Join(root, "clock.endstate"), Only: "desktop.clock"})
			if err != nil {
				t.Fatalf("RunCapture desktop generation: %+v", err)
			}
			result := raw.(*CaptureResult)
			if result.ConfigCapture == nil || len(result.ConfigCapture.ConfigSets) != 1 {
				t.Fatalf("desktop generation missing: %+v", result)
			}
			extracted, extractErr := bundle.ExtractBundle(result.OutputPath)
			if extractErr != nil {
				t.Fatal(extractErr)
			}
			t.Cleanup(func() { os.RemoveAll(filepath.Dir(extracted)) })
			mf, loadErr := manifest.LoadManifest(extracted)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if len(mf.Apps) != 0 || len(mf.ConfigCaptures) != 1 {
				t.Fatalf("desktop generation fabricated package or lost payload: %+v", mf)
			}
			capture := mf.ConfigCaptures[0]
			if capture.SourcePlatform != "linux" || capture.CaptureModule.SchemaVersion != 3 {
				t.Fatalf("desktop provenance: %+v", capture)
			}
			data, readErr := os.ReadFile(filepath.Join(filepath.Dir(extracted), capture.PayloadRoot, "clock.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			captured, decodeErr := desktopsettings.Decode(data)
			if decodeErr != nil || !captured.Equal(desired) {
				t.Fatalf("portable user state %q: %v", data, decodeErr)
			}
			backend.scope = "fixture/target"
			backend.values[target.Identity()] = prior
			restore := func(flags RestoreFlags) *RestoreData {
				t.Helper()
				raw, err := RunRestore(flags)
				if err != nil {
					t.Fatalf("RunRestore: %+v", err)
				}
				return raw.(*RestoreData)
			}
			preview := restore(RestoreFlags{Manifest: extracted, EnableRestore: true, DryRun: true})
			if len(preview.ConfigResolutions) != 1 || preview.ConfigResolutions[0].Status != planner.StatusPlanned || backend.writes != 0 {
				t.Fatalf("preview mutated or dropped desktop: %+v writes=%d", preview, backend.writes)
			}
			restore(RestoreFlags{Manifest: extracted})
			if backend.writes != 0 {
				t.Fatal("restore bypassed explicit consent")
			}
			apply := func() {
				t.Helper()
				raw, err := RunApply(ApplyFlags{Manifest: extracted, EnableRestore: true, RestoreFilter: "desktop.clock", NoBootstrap: true})
				if err != nil {
					t.Fatalf("RunApply: %+v", err)
				}
				got := raw.(*ApplyResult)
				if len(got.ConfigResolutions) != 1 || got.ConfigResolutions[0].Status != planner.StatusRestored {
					t.Fatalf("desktop apply: %+v", got.ConfigResolutions)
				}
				if !backend.values[target.Identity()].Equal(desired) {
					t.Fatal("desktop apply did not converge")
				}
			}
			verify := func(want string) {
				t.Helper()
				raw, err := RunVerify(VerifyFlags{Manifest: extracted})
				if err != nil {
					t.Fatalf("RunVerify: %+v", err)
				}
				items := raw.(*VerifyResult).Results
				if len(items) != 1 || items[0].Type != "config" || items[0].Status != want {
					t.Fatalf("desktop content verify: %+v", items)
				}
			}
			revert := func(want desktopsettings.Value) {
				t.Helper()
				if _, err := RunRevert(RevertFlags{}); err != nil {
					t.Fatalf("RunRevert: %+v", err)
				}
				if !backend.values[target.Identity()].Equal(want) {
					t.Fatalf("revert lost prior override/absence: %+v", backend.values[target.Identity()])
				}
			}
			apply()
			guard, beginErr := configrestore.BeginLive(context.Background(), filepath.Join(root, "state"), "offline-inspect", nil)
			if beginErr != nil {
				t.Fatalf("committed desktop required runtime: %v", beginErr)
			}
			_, inspectErr := configrestore.InspectStore(filepath.Join(root, "state", "config-restore", "v1"), true)
			guard.Close()
			if inspectErr != nil {
				t.Fatalf("committed desktop offline inspection: %v", inspectErr)
			}
			verify("pass")
			revert(prior)
			apply()
			trueValue := true
			drift := desktopsettings.Value{Type: "boolean", Present: true, Value: &trueValue}
			backend.values[target.Identity()] = drift
			verify("fail")
			apply()
			verify("pass")
			revert(drift)
			if unrelated := backend.values["unrelated"]; !unrelated.Present || *unrelated.Value {
				t.Fatal("unrelated preference changed")
			}
		})
	}

}

type clockBackend struct {
	scope  string
	values map[string]desktopsettings.Value
	writes int
}

func (b *clockBackend) Read(_ context.Context, target desktopsettings.Target) (desktopsettings.Observation, error) {
	v, ok := b.values[target.Identity()]
	if !ok {
		v = desktopsettings.Value{Type: "boolean"}
	}
	effective := true
	return desktopsettings.Observation{User: v.Clone(), Effective: &effective, Scope: b.scope}, nil
}
func (b *clockBackend) Write(_ context.Context, target desktopsettings.Target, scope string, value desktopsettings.Value) error {
	if scope != b.scope {
		return desktopsettings.ErrConflict
	}
	b.writes++
	b.values[target.Identity()] = value.Clone()
	return nil
}
