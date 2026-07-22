// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func validationContext(t *testing.T, inventory validationmode.Inventory) *validationmode.Context {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "endstate-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	nonce := strings.TrimPrefix(filepath.Base(root), "endstate-validation-")
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1,
		ScenarioID:    "commands-validation",
		Nonce:         nonce,
		ModuleID:      "apps.notepad-plus-plus",
		Inventory:     inventory,
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
		t.Fatalf("LoadFromEnvironment: %v", err)
	}
	return context
}

func TestActivateValidationModeRoutesAllPackageBoundariesToOneDriver(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "present",
	})

	originalDefault, originalNamed := newDriverFn, newNamedDriverFn
	originalRealizer, originalBrew := newRealizerFn, newBrewDriverFn
	originalBootstrap, originalCapture := bootstrapBackendsFn, enumerateCapturePackagesFn
	originalCaptureGOOS, originalRollback := captureGOOSFn, rollbackDriverFn
	restored := false
	newDriverFn = func() (driver.Driver, error) { restored = true; return nil, errors.New("original default") }
	newNamedDriverFn = func(string) (driver.Driver, error) { return nil, errors.New("original named") }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, errors.New("original realizer") }
	newBrewDriverFn = func() (driver.Driver, error) { return nil, errors.New("original brew") }
	bootstrapBackendsFn = func([]bootstrap.Backend, bool, Consent, *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		return nil, envelope.NewError(envelope.ErrInternalError, "original bootstrap")
	}
	enumerateCapturePackagesFn = func(CaptureFlags) ([]enumeratedCapturePackage, []CommandWarning, *envelope.Error) {
		return nil, nil, envelope.NewError(envelope.ErrCaptureFailed, "original capture")
	}
	captureGOOSFn = func() string { return "plan9" }
	rollbackDriverFn = func(string) (driver.Driver, error) { return nil, errors.New("original rollback") }
	t.Cleanup(func() {
		newDriverFn, newNamedDriverFn = originalDefault, originalNamed
		newRealizerFn, newBrewDriverFn = originalRealizer, originalBrew
		bootstrapBackendsFn, enumerateCapturePackagesFn = originalBootstrap, originalCapture
		captureGOOSFn, rollbackDriverFn = originalCaptureGOOS, originalRollback
	})

	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatalf("ActivateValidationMode: %v", err)
	}
	if currentValidationMode != context {
		t.Fatal("command layer did not retain the active validation context")
	}
	first, err := newDriverFn()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newNamedDriverFn("WINGET")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("factories returned different stateful drivers: %p != %p", first, second)
	}
	if _, err := newRealizerFn(); !errors.Is(err, ErrNoRealizer) {
		t.Fatalf("realizer error = %v, want ErrNoRealizer", err)
	}
	available, eerr := bootstrapBackendsFn([]bootstrap.Backend{bootstrap.BackendChocolatey}, true, Consent{Granted: true}, events.NewEmitter("test", false))
	if eerr != nil || !available[bootstrap.BackendChocolatey] {
		t.Fatalf("bootstrap replacement = (%v, %v), want available without host probe", available, eerr)
	}
	packages, _, captureErr := enumerateCapturePackagesFn(CaptureFlags{})
	if captureErr != nil || len(packages) != 1 || packages[0].Package.Ref != "Notepad++.Notepad++" {
		t.Fatalf("capture inventory = (%+v, %+v)", packages, captureErr)
	}
	if captureGOOSFn() != "windows" {
		t.Fatalf("capture GOOS = %q, want windows", captureGOOSFn())
	}
	if session.IsolationError() != nil {
		t.Fatalf("unexpected isolation error: %v", session.IsolationError())
	}
	if err := session.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if currentValidationMode != nil {
		t.Fatal("command layer retained validation context after restore")
	}
	if err := session.Restore(); err != nil {
		t.Fatalf("second Restore: %v", err)
	}
	_, _ = newDriverFn()
	if !restored {
		t.Fatal("original command seam was not restored")
	}
}

func TestActivateValidationModeRejectsWrongDriverAndPreservesState(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Source: "winget", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })

	if _, err := newNamedDriverFn("chocolatey"); !errors.Is(err, validationmode.ErrPackageIdentity) {
		t.Fatalf("wrong driver error = %v, want package identity", err)
	}
	if !errors.Is(session.IsolationError(), validationmode.ErrPackageIdentity) {
		t.Fatalf("session isolation error = %v", session.IsolationError())
	}
	drv, err := newDriverFn()
	if err != nil {
		t.Fatal(err)
	}
	present, _, err := drv.(driver.SourceDriver).DetectSource("Notepad++.Notepad++", "winget")
	if err != nil || present {
		t.Fatalf("state changed after rejected driver: present=%v err=%v", present, err)
	}
}

func TestValidationModePlanApplyVerifyShareDisposableState(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })

	manifestPath := filepath.Join(context.Root(), "validation-manifest.jsonc")
	manifestJSON := `{
  "version": 1,
  "name": "validation-engine-path",
  "apps": [{
    "id": "notepad-plus-plus",
    "displayName": "Notepad++",
    "driver": "winget",
    "source": "winget",
    "version": "8.8.2",
    "refs": {"windows": "Notepad++.Notepad++"}
  }]
}`
	if err := os.WriteFile(manifestPath, []byte(manifestJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	rawPlan, planErr := RunPlan(PlanFlags{Manifest: manifestPath})
	if planErr != nil {
		t.Fatalf("RunPlan: %v", planErr)
	}
	planResult := rawPlan.(*PlanResult)
	if planResult.Plan.ToInstall != 1 || len(planResult.Actions) != 1 || planResult.Actions[0].PlannedAction != "install" {
		t.Fatalf("initial plan = %+v", planResult)
	}

	rawApply, applyErr := RunApply(ApplyFlags{Manifest: manifestPath, NoBootstrap: true})
	if applyErr != nil {
		t.Fatalf("RunApply: %v", applyErr)
	}
	applyResult := rawApply.(*ApplyResult)
	if applyResult.Summary.Success != 1 || len(applyResult.Actions) != 1 || applyResult.Actions[0].Status != driver.StatusInstalled {
		t.Fatalf("apply result = %+v", applyResult)
	}

	rawVerify, verifyErr := RunVerify(VerifyFlags{Manifest: manifestPath})
	if verifyErr != nil {
		t.Fatalf("RunVerify: %v", verifyErr)
	}
	verifyResult := rawVerify.(*VerifyResult)
	if verifyResult.Summary.Pass != 1 || verifyResult.Summary.Fail != 0 || len(verifyResult.Results) != 1 || verifyResult.Results[0].Status != "pass" {
		t.Fatalf("verify result = %+v", verifyResult)
	}
	if session.IsolationError() != nil {
		t.Fatalf("unexpected isolation error: %v", session.IsolationError())
	}
}

func TestValidationModeCaptureUsesOrdinarySelectionAndModuleMatching(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "vendor-notepadplusplus", Driver: "winget", Ref: "Vendor.NotepadPlusPlus",
		DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "present",
	})
	restoreEnvironment, err := context.Activate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoreEnvironment() })

	originalResolver := resolveCaptureEnumeratorFn
	originalCatalogLoader := loadCaptureModuleCatalogFn
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) {
		t.Fatal("validation capture constructed a production enumerator")
		return nil, nil
	}
	loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = originalResolver
		loadCaptureModuleCatalogFn = originalCatalogLoader
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })

	moduleDir := filepath.Join(context.Root(), "modules", "apps", "notepad-plus-plus")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleJSON := `{
  "id":"apps.notepad-plus-plus",
  "displayName":"Notepad++",
  "sensitivity":"low",
  "matches":{"winget":["Vendor.NotepadPlusPlus"]},
  "verify":[],
  "restore":[{"type":"copy","source":"./payload/apps/notepad-plus-plus/config.xml","target":"%APPDATA%\\Notepad++\\config.xml","backup":true,"optional":true}],
  "capture":{"files":[{"source":"%APPDATA%\\Notepad++\\config.xml","dest":"apps/notepad-plus-plus/config.xml","optional":true}],"excludeGlobs":[]}
}`
	if err := os.WriteFile(filepath.Join(moduleDir, "module.jsonc"), []byte(moduleJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, diagnostics, catalogErr := loadCaptureModuleCatalogFn(context.Root())
	if catalogErr != nil || len(catalog) != 1 {
		t.Fatalf("catalog load = %v diagnostics=%+v catalog=%+v", catalogErr, diagnostics, catalog)
	}
	appData, _ := context.VirtualRoot("APPDATA")
	configPath := filepath.Join(appData, "Notepad++", "config.xml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("<NotepadPlusPlus/>"), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, captureErr := RunCapture(CaptureFlags{
		Out:  filepath.Join(context.Root(), "capture.jsonc"),
		Only: "vendor-notepadplusplus,apps.notepad-plus-plus",
		Pin:  true,
	})
	if captureErr != nil {
		t.Fatalf("RunCapture: %+v", captureErr)
	}
	result := raw.(*CaptureResult)
	if len(result.AppsIncluded) != 1 || result.AppsIncluded[0].ID != "Vendor.NotepadPlusPlus" || result.AppsIncluded[0].ManifestID != "vendor-notepadplusplus" {
		t.Fatalf("captured apps = %+v", result.AppsIncluded)
	}
	if len(result.ConfigModules) != 1 || result.ConfigModules[0].ID != "apps.notepad-plus-plus" {
		t.Fatalf("matched config modules = %+v", result.ConfigModules)
	}
	if got := result.PackageModuleMap["winget:Vendor.NotepadPlusPlus"]; len(got) != 1 || got[0] != "apps.notepad-plus-plus" {
		t.Fatalf("packageModuleMap = %+v", result.PackageModuleMap)
	}
	if result.OutputFormat != "zip" {
		t.Fatalf("output format = %q, want zip", result.OutputFormat)
	}
	if session.IsolationError() != nil {
		t.Fatalf("unexpected isolation error: %v", session.IsolationError())
	}
}
