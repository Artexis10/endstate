// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
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

func validationCommandManifestPath(t *testing.T, context *validationmode.Context, name string) string {
	t.Helper()
	directory := filepath.Join(context.Root(), "manifests")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, name)
}

func TestSharedManifestLoaderRejectsPrivateValidationDriverEvenWhenValidationModeIsActive(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "studio-one", Driver: "validation", Ref: "studio-one", DisplayName: "PreSonus Studio One",
		Version: "7", InitialState: "present",
	})
	path := validationCommandManifestPath(t, context, "private-validation.jsonc")
	if err := os.WriteFile(path, []byte(`{"version":1,"apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.LoadManifest(path); err == nil {
		t.Fatal("ordinary manifest loader accepted private validation driver")
	}
	original := currentValidationMode
	currentValidationMode = context
	t.Cleanup(func() { currentValidationMode = original })
	if _, envelopeErr := loadManifest(path); envelopeErr == nil {
		t.Fatal("shared command loader admitted the private validation driver")
	}
}

func TestRunExportCannotUseValidationModeToReadPrivateManifestTargets(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "studio-one", Driver: "validation", Ref: "studio-one", DisplayName: "PreSonus Studio One",
		Version: "7", InitialState: "present",
	})
	external := t.TempDir()
	secret := filepath.Join(external, "foreign-settings.txt")
	if err := os.WriteFile(secret, []byte("must-not-export"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := validationCommandManifestPath(t, context, "private-export.jsonc")
	raw := fmt.Sprintf(`{"version":1,"apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation"}],"restore":[{"type":"copy","source":"stolen.txt","target":%q}]}`, secret)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	original := currentValidationMode
	currentValidationMode = context
	t.Cleanup(func() { currentValidationMode = original })
	exportRoot := filepath.Join(context.Root(), "export")
	if _, envelopeErr := RunExport(ExportFlags{Manifest: path, Export: exportRoot}); envelopeErr == nil {
		t.Fatal("export-config admitted a private validation manifest")
	}
	if _, err := os.Stat(filepath.Join(exportRoot, "stolen.txt")); !os.IsNotExist(err) {
		t.Fatalf("export-config wrote through private-manifest seam: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exportRoot, "manifest.snapshot.jsonc")); !os.IsNotExist(err) {
		t.Fatalf("export-config wrote a manifest snapshot before refusal: %v", err)
	}
}

func TestDurableLegacyRevertDispatchUsesCurrentValidationContext(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "present",
	})
	originalContext := currentValidationMode
	originalRevert := runDurableLegacyRevertFn
	currentValidationMode = context
	called := false
	runDurableLegacyRevertFn = func(_ *restore.Journal, _, _ string, got *validationmode.Context) ([]restore.RevertResult, error) {
		called = true
		if got != context {
			t.Fatalf("validation context = %p, want %p", got, context)
		}
		return nil, nil
	}
	t.Cleanup(func() {
		currentValidationMode = originalContext
		runDurableLegacyRevertFn = originalRevert
	})
	if _, err := runDurableLegacyRevert(&restore.Journal{}, "", filepath.Join(context.Root(), "state", "revert")); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("durable legacy revert adapter was not called")
	}
}

func writeValidationPackageOnlyModule(t *testing.T, context *validationmode.Context, ref string) {
	t.Helper()
	moduleDir := filepath.Join(context.Root(), "modules", "apps", "notepad-plus-plus")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleJSON := `{"id":"apps.notepad-plus-plus","displayName":"Notepad++","sensitivity":"low","matches":{"winget":["` + ref + `"]},"verify":[],"restore":[],"capture":{"files":[{"source":"%APPDATA%\\Notepad++\\config.xml","dest":"apps/notepad-plus-plus/config.xml","optional":true}],"excludeGlobs":[]}}`
	if err := os.WriteFile(filepath.Join(moduleDir, "module.jsonc"), []byte(moduleJSON), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if _, err := drv.(driver.SourceDriver).InstallSource("Notepad++.Notepad++", "winget"); !errors.Is(err, validationmode.ErrPackageIdentity) {
		t.Fatalf("poisoned mutator error = %v, want package identity", err)
	}
	present, _, err := drv.(driver.SourceDriver).DetectSource("Notepad++.Notepad++", "winget")
	if err != nil || present {
		t.Fatalf("state changed after rejected driver: present=%v err=%v", present, err)
	}
}

func TestValidationModeMixedManifestFailsBeforePackageMutation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		validFirst bool
		extra      string
	}{
		{name: "valid then wrong driver", validFirst: true, extra: `{"id":"foreign","displayName":"Foreign","driver":"chocolatey","refs":{"windows":"foreign-package"}}`},
		{name: "wrong driver then valid", validFirst: false, extra: `{"id":"foreign","displayName":"Foreign","driver":"chocolatey","refs":{"windows":"foreign-package"}}`},
		{name: "valid then wrong source", validFirst: true, extra: `{"id":"foreign","displayName":"Foreign","driver":"winget","source":"msstore","refs":{"windows":"9NFOREIGN"}}`},
		{name: "wrong source then valid", validFirst: false, extra: `{"id":"foreign","displayName":"Foreign","driver":"winget","source":"msstore","refs":{"windows":"9NFOREIGN"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context := validationContext(t, validationmode.Inventory{
				AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
				DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "absent",
			})
			session, err := ActivateValidationMode(context)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Restore() })

			valid := `{"id":"notepad-plus-plus","displayName":"Notepad++","driver":"winget","source":"winget","version":"8.8.2","refs":{"windows":"Notepad++.Notepad++"}}`
			apps := valid + "," + tc.extra
			if !tc.validFirst {
				apps = tc.extra + "," + valid
			}
			manifestPath := validationCommandManifestPath(t, context, "mixed.jsonc")
			if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"mixed","apps":[`+apps+`]}`), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, applyErr := RunApply(ApplyFlags{Manifest: manifestPath, NoBootstrap: true}); applyErr == nil {
				t.Fatal("mixed validation manifest unexpectedly applied")
			}
			if !errors.Is(session.IsolationError(), validationmode.ErrPackageIdentity) {
				t.Fatalf("isolation error = %v", session.IsolationError())
			}
			drv, err := newDriverFn()
			if err != nil {
				t.Fatal(err)
			}
			present, _, err := drv.(driver.SourceDriver).DetectSource("Notepad++.Notepad++", "winget")
			if err != nil || present {
				t.Fatalf("mixed manifest mutated valid inventory: present=%v err=%v", present, err)
			}
		})
	}
}

func TestValidationModeManifestPreflightCoversPlanVerifyAndRebuild(t *testing.T) {
	for _, command := range []string{"plan", "verify", "rebuild"} {
		t.Run(command, func(t *testing.T) {
			context := validationContext(t, validationmode.Inventory{
				AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
				DisplayName: "Notepad++", Source: "winget", InitialState: "absent",
			})
			session, err := ActivateValidationMode(context)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Restore() })
			manifestPath := validationCommandManifestPath(t, context, command+"-mixed.jsonc")
			manifestJSON := `{"version":1,"name":"mixed","apps":[{"id":"notepad-plus-plus","driver":"winget","source":"winget","refs":{"windows":"Notepad++.Notepad++"}},{"id":"foreign","driver":"chocolatey","refs":{"windows":"foreign"}}]}`
			if err := os.WriteFile(manifestPath, []byte(manifestJSON), 0o600); err != nil {
				t.Fatal(err)
			}
			var commandErr *envelope.Error
			switch command {
			case "plan":
				_, commandErr = RunPlan(PlanFlags{Manifest: manifestPath})
			case "verify":
				_, commandErr = RunVerify(VerifyFlags{Manifest: manifestPath})
			case "rebuild":
				_, commandErr = RunRebuild(RebuildFlags{From: manifestPath, NoRestore: true, NoBootstrap: true})
			}
			if commandErr == nil || !errors.Is(session.IsolationError(), validationmode.ErrPackageIdentity) {
				t.Fatalf("%s error=%v isolation=%v", command, commandErr, session.IsolationError())
			}
		})
	}
}

func TestValidationModeOmittedDriverDoesNotAuthorizeNonWingetInventory(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "tool", Driver: "chocolatey", Ref: "vendor-tool",
		DisplayName: "Tool", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })
	manifestPath := validationCommandManifestPath(t, context, "omitted-driver.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"omitted","apps":[{"id":"tool","refs":{"windows":"vendor-tool"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, planErr := RunPlan(PlanFlags{Manifest: manifestPath}); planErr == nil {
		t.Fatal("omitted driver authorized non-winget descriptor inventory")
	}
	if !errors.Is(session.IsolationError(), validationmode.ErrPackageIdentity) {
		t.Fatalf("isolation error = %v", session.IsolationError())
	}
}

func TestValidationModeStoreCaptureNeverCallsProductionNameResolver(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "store-tool", Driver: "winget", Ref: "9NSTORETOOL",
		DisplayName: "9NSTORETOOL", Source: "msstore", InitialState: "present",
	})
	originalResolver := resolveStoreDisplayNamesFn
	restored := false
	resolveStoreDisplayNamesFn = func() map[string]string {
		restored = true
		panic("production Store name resolver was called")
	}
	t.Cleanup(func() { resolveStoreDisplayNamesFn = originalResolver })
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}

	raw, captureErr := RunCapture(CaptureFlags{
		Out: validationCommandManifestPath(t, context, "store-capture.jsonc"), Drivers: []string{"winget"}, Sanitize: true,
	})
	if captureErr != nil {
		t.Fatalf("RunCapture: %v", captureErr)
	}
	result := raw.(*CaptureResult)
	if len(result.AppsIncluded) != 1 || result.AppsIncluded[0].ID != "9NSTORETOOL" {
		t.Fatalf("captured Store apps = %+v", result.AppsIncluded)
	}
	if err := session.Restore(); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("restored production Store resolver did not run sentinel")
			}
		}()
		_ = resolveStoreDisplayNamesFn()
	}()
	if !restored {
		t.Fatal("production Store resolver was not restored")
	}
}

func TestValidationModePlanApplyVerifyShareDisposableState(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "absent",
	})
	writeValidationPackageOnlyModule(t, context, "Notepad++.Notepad++")
	restoreEnvironment, err := context.Activate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoreEnvironment() })
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })

	manifestPath := validationCommandManifestPath(t, context, "validation-manifest.jsonc")
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
		t.Fatalf("RunApply: %v isolation=%v", applyErr, session.IsolationError())
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

func TestValidationManifestAcceptsProductionUnpinnedCaptureButRejectsWrongPin(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "unpinned capture", version: ""},
		{name: "matching pin", version: "8.8.2"},
		{name: "wrong pin", version: "8.7.0", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			context := validationContext(t, validationmode.Inventory{
				AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
				DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "present",
			})
			session, err := ActivateValidationMode(context)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Restore() })

			commandErr := preflightValidationManifest(&manifest.Manifest{Version: 1, Apps: []manifest.App{{
				ID: "notepad-plus-plus", Driver: "winget", Source: "winget", Version: test.version,
				Refs: map[string]string{"windows": "Notepad++.Notepad++"},
			}}})
			if (commandErr != nil) != test.wantErr {
				t.Fatalf("preflight error = %v, wantErr=%v", commandErr, test.wantErr)
			}
			if test.wantErr != (session.IsolationError() != nil) {
				t.Fatalf("isolation = %v, wantErr=%v", session.IsolationError(), test.wantErr)
			}
		})
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
		Out:  validationCommandManifestPath(t, context, "capture.jsonc"),
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
