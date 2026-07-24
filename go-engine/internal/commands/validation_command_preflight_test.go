// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestRunCaptureValidationPreflightPrecedesIntermediateWriteAndBundleMutation(t *testing.T) {
	fixture := commandPreflightFixture(t, "owncloud")
	originalLoad, originalCreate := loadCaptureModuleCatalogFn, createCaptureBundleFn
	loadCaptureModuleCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return fixture.catalog, nil, nil
	}
	createCaptureBundleFn = func(request bundle.CaptureBundleRequest) (*bundle.CaptureBundleResult, error) {
		panic("capture bundle mutation reached")
	}
	t.Cleanup(func() {
		loadCaptureModuleCatalogFn, createCaptureBundleFn = originalLoad, originalCreate
	})

	intermediate := filepath.Join(fixture.context.Root(), "manifests", "capture.jsonc")
	_, commandErr := RunCapture(CaptureFlags{Out: intermediate})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
	if _, err := os.Stat(intermediate); !os.IsNotExist(err) {
		t.Fatalf("intermediate manifest exists after preflight failure: %v", err)
	}
}

func TestRunCaptureValidationRejectsEscapedDurablePathsBeforeWriting(t *testing.T) {
	for _, coordinate := range []string{"output", "update"} {
		t.Run(coordinate, func(t *testing.T) {
			fixture := commandPreflightFixture(t, "notepad-plus-plus")
			escaped := filepath.Join(filepath.Dir(fixture.context.Root()), filepath.Base(fixture.context.Root())+"-escaped.jsonc")
			t.Cleanup(func() { _ = os.Remove(escaped) })
			flags := CaptureFlags{Out: filepath.Join(fixture.context.Root(), "manifests", "safe.jsonc")}
			if coordinate == "output" {
				flags.Out = escaped
			} else {
				flags.Update = true
				flags.Manifest = escaped
			}
			_, commandErr := RunCapture(flags)
			assertValidationCommandPreflightFailure(t, fixture, commandErr)
			if _, err := os.Stat(escaped); !os.IsNotExist(err) {
				t.Fatalf("escaped %s path exists after preflight failure: %v", coordinate, err)
			}
			if _, err := os.Stat(flags.Out); !os.IsNotExist(err) {
				t.Fatalf("capture output exists after escaped %s failure: %v", coordinate, err)
			}
		})
	}
}

func TestRunCaptureSanitizeValidationGatePrecedesManifestWrite(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	_ = fixture.session.recordIsolationFinding("test.coordinate", "test-target", isolationReasonUnsafePath)
	output := filepath.Join(fixture.context.Root(), "manifests", "sanitized.jsonc")

	_, commandErr := RunCapture(CaptureFlags{Out: output, Sanitize: true})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("sanitized manifest exists after isolation failure: %v", err)
	}
}

func TestRunApplyValidationPreflightPrecedesRealizerAndDriverResolution(t *testing.T) {
	fixture := commandPreflightFixture(t, "owncloud")
	originalLoad, originalRealizer := loadModuleCatalogFn, newRealizerFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	newRealizerFn = func() (realizer.Realizer, error) { panic("apply realizer reached") }
	t.Cleanup(func() { loadModuleCatalogFn, newRealizerFn = originalLoad, originalRealizer })

	_, commandErr := RunApply(ApplyFlags{Manifest: fixture.manifestPath})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
}

func TestRunApplyValidationAllowsExactMatchedProductionModule(t *testing.T) {
	fixture := commandPreflightFixtureWithState(t, "notepad-plus-plus", "absent")
	originalLoad := loadModuleCatalogFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	t.Cleanup(func() { loadModuleCatalogFn = originalLoad })

	data, commandErr := RunApply(ApplyFlags{Manifest: fixture.manifestPath, NoBootstrap: true})
	if commandErr != nil || data == nil {
		t.Fatalf("valid production-module apply = (%T, %v); isolation=%v", data, commandErr, fixture.session.IsolationError())
	}
}

func TestRunApplyValidationRejectsEscapedExportBeforeMutation(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	originalLoad, originalRealizer := loadModuleCatalogFn, newRealizerFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	newRealizerFn = func() (realizer.Realizer, error) { panic("apply realizer reached") }
	t.Cleanup(func() { loadModuleCatalogFn, newRealizerFn = originalLoad, originalRealizer })
	escaped := filepath.Join(filepath.Dir(fixture.context.Root()), filepath.Base(fixture.context.Root())+"-export")

	_, commandErr := RunApply(ApplyFlags{Manifest: fixture.manifestPath, Export: escaped})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Fatalf("escaped export exists after preflight failure: %v", err)
	}
}

func TestRunRebuildValidationPreflightPrecedesNestedMutation(t *testing.T) {
	fixture := commandPreflightFixture(t, "owncloud")
	originalLoad, originalRealizer := loadModuleCatalogFn, newRealizerFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	newRealizerFn = func() (realizer.Realizer, error) { panic("rebuild realizer reached") }
	t.Cleanup(func() { loadModuleCatalogFn, newRealizerFn = originalLoad, originalRealizer })

	_, commandErr := RunRebuild(RebuildFlags{From: fixture.manifestPath, NoRestore: true})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
}

func TestRunRebuildValidationRejectsEscapedInputBeforeOpening(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	escaped := filepath.Join(filepath.Dir(fixture.context.Root()), filepath.Base(fixture.context.Root())+"-missing.jsonc")

	_, commandErr := RunRebuild(RebuildFlags{From: escaped, NoRestore: true})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
}

func TestRunRebuildValidationRejectsBundleBeforeExtractionMutation(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	originalExtract := extractRebuildBundleFn
	extractRebuildBundleFn = func(string) (string, error) { panic("bundle extraction reached") }
	t.Cleanup(func() { extractRebuildBundleFn = originalExtract })
	bundlePath := filepath.Join(fixture.context.Root(), "manifests", "capture.zip")
	if err := os.WriteFile(bundlePath, []byte("not opened"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, commandErr := RunRebuild(RebuildFlags{From: bundlePath, NoRestore: true})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
}

func TestRunRebuildValidationKeepsMissingBundleAsOrdinaryNotFound(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	bundlePath := filepath.Join(fixture.context.Root(), "manifests", "missing.zip")

	_, commandErr := RunRebuild(RebuildFlags{From: bundlePath, NoRestore: true})
	if commandErr == nil || commandErr.Code != envelope.ErrManifestNotFound {
		t.Fatalf("RunRebuild error = %v, want %s", commandErr, envelope.ErrManifestNotFound)
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr != nil {
		t.Fatalf("ordinary missing bundle was mislabeled as isolation: %v", isolationErr)
	}
}

func TestRunRebuildValidationKeepsLiveBundleConfirmationGateBeforeRefusal(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	originalExtract := extractRebuildBundleFn
	extractRebuildBundleFn = func(string) (string, error) { panic("bundle extraction reached") }
	t.Cleanup(func() { extractRebuildBundleFn = originalExtract })
	bundlePath := filepath.Join(fixture.context.Root(), "manifests", "capture.zip")
	if err := os.WriteFile(bundlePath, []byte("not opened"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, commandErr := RunRebuild(RebuildFlags{From: bundlePath})
	if commandErr == nil || commandErr.Code != envelope.ErrConfirmationRequired {
		t.Fatalf("RunRebuild error = %v, want %s", commandErr, envelope.ErrConfirmationRequired)
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr != nil {
		t.Fatalf("ordinary confirmation requirement was mislabeled as isolation: %v", isolationErr)
	}
}

func TestRunRebuildValidationRepreflightsReloadedModuleBeforeVerify(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	unsafeModule := validationUnsafeModuleClone(t, fixture.catalog["apps.notepad-plus-plus"])
	originalLoad, originalRealizer := loadModuleCatalogFn, newRealizerFn
	catalogLoads := 0
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) {
		catalogLoads++
		if catalogLoads == 1 {
			return fixture.catalog, nil
		}
		return map[string]*modules.Module{unsafeModule.ID: unsafeModule}, nil
	}
	realizerCalls := 0
	newRealizerFn = func() (realizer.Realizer, error) {
		realizerCalls++
		if realizerCalls > 1 {
			panic("nested verify realizer reached")
		}
		return nil, ErrNoRealizer
	}
	t.Cleanup(func() { loadModuleCatalogFn, newRealizerFn = originalLoad, originalRealizer })

	_, commandErr := RunRebuild(RebuildFlags{From: fixture.manifestPath, NoRestore: true})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
	if catalogLoads < 2 {
		t.Fatalf("rebuild catalog loads = %d, want apply and verify reloads", catalogLoads)
	}
}

func TestRunVerifyValidationPreflightPrecedesRealizerAndVerifier(t *testing.T) {
	fixture := commandPreflightFixture(t, "owncloud")
	originalLoad, originalRealizer := loadModuleCatalogFn, newRealizerFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	newRealizerFn = func() (realizer.Realizer, error) { panic("verify realizer reached") }
	t.Cleanup(func() { loadModuleCatalogFn, newRealizerFn = originalLoad, originalRealizer })

	_, commandErr := RunVerify(VerifyFlags{Manifest: fixture.manifestPath})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
}

func TestRunVerifyValidationPreflightsExactMatchedProductionModuleAndKeepsAssertionFailureOrdinary(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	originalLoad := loadModuleCatalogFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	t.Cleanup(func() { loadModuleCatalogFn = originalLoad })

	data, commandErr := RunVerify(VerifyFlags{Manifest: fixture.manifestPath})
	if commandErr != nil || data == nil {
		t.Fatalf("valid production-module verify = (%T, %v)", data, commandErr)
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr != nil {
		t.Fatalf("ordinary failed assertion was mislabeled as isolation: %v", isolationErr)
	}
}

func TestRunRestoreValidationPreflightPrecedesExecutionAndJournalMutation(t *testing.T) {
	fixture := commandPreflightFixture(t, "vscode")
	originalLoad, originalBegin := loadModuleCatalogFn, beginLiveConfigRestoreFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	beginLiveConfigRestoreFn = func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error) {
		panic("restore execution reached")
	}
	t.Cleanup(func() { loadModuleCatalogFn, beginLiveConfigRestoreFn = originalLoad, originalBegin })

	_, commandErr := RunRestore(RestoreFlags{Manifest: fixture.manifestPath, EnableRestore: true})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
}

type commandPreflightTestFixture struct {
	context      *validationmode.Context
	session      *ValidationModeSession
	catalog      map[string]*modules.Module
	manifestPath string
	packageState []byte
}

func commandPreflightFixture(t *testing.T, shortID string) commandPreflightTestFixture {
	return commandPreflightFixtureWithState(t, shortID, "present")
}

func commandPreflightFixtureWithState(t *testing.T, shortID, initialState string) commandPreflightTestFixture {
	t.Helper()
	mod := loadValidationProductionModule(t, shortID)
	if len(mod.Matches.Winget) == 0 {
		t.Fatalf("production module %s has no winget identity", mod.ID)
	}
	appID := shortID + "-validation"
	context := validationContext(t, validationmode.Inventory{
		AppID: appID, Driver: "winget", Ref: mod.Matches.Winget[0], Source: "winget",
		DisplayName: mod.DisplayName, InitialState: initialState,
	})
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

	mf := manifestForValidationModule(mod)
	mf.Apps = []manifest.App{{
		ID: appID, Driver: "winget", Source: "winget",
		Refs: map[string]string{"windows": mod.Matches.Winget[0]},
	}}
	manifestPath := filepath.Join(context.Root(), "manifests", shortID+".jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	packageState, err := os.ReadFile(filepath.Join(context.Root(), ".endstate", "validation-package-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return commandPreflightTestFixture{
		context: context, session: session, catalog: map[string]*modules.Module{mod.ID: mod}, manifestPath: manifestPath,
		packageState: packageState,
	}
}

func assertValidationCommandPreflightFailure(t *testing.T, fixture commandPreflightTestFixture, commandErr *envelope.Error) {
	t.Helper()
	if commandErr == nil {
		t.Fatal("command returned without a preflight error")
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr == nil {
		t.Fatal("command preflight failure was not recorded in the shared session")
	}
	after, err := os.ReadFile(filepath.Join(fixture.context.Root(), ".endstate", "validation-package-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, fixture.packageState) {
		t.Fatal("disposable package state changed after preflight failure")
	}
}

func validationUnsafeModuleClone(t *testing.T, mod *modules.Module) *modules.Module {
	t.Helper()
	var raw map[string]interface{}
	if err := json.Unmarshal(mod.CanonicalSnapshot(), &raw); err != nil {
		t.Fatal(err)
	}
	capture, ok := raw["capture"].(map[string]interface{})
	if !ok {
		t.Fatal("production module has no capture declaration")
	}
	files, ok := capture["files"].([]interface{})
	if !ok || len(files) == 0 {
		t.Fatal("production module has no capture file declaration")
	}
	files[0].(map[string]interface{})["source"] = `C:\host\escape\settings.json`
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := modules.ParseModuleJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return clone
}
