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

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"github.com/Artexis10/endstate/go-engine/internal/verifier"
)

func commandFixtureWithSingleTestVerifier(t *testing.T, fixture *commandPreflightTestFixture, testVerifier modules.VerifyDef) {
	t.Helper()
	originalModule := fixture.catalog["apps.notepad-plus-plus"]
	if originalModule == nil {
		t.Fatal("notepad-plus-plus production module missing from fixture catalog")
	}
	if len(originalModule.Verify) != 0 {
		t.Fatalf("notepad-plus-plus production verify declarations = %d, want 0", len(originalModule.Verify))
	}

	moduleCopy := *originalModule
	moduleCopy.Verify = append([]modules.VerifyDef(nil), originalModule.Verify...)
	moduleCopy.Verify = append(moduleCopy.Verify, testVerifier)
	if len(moduleCopy.Verify) != 1 {
		t.Fatalf("test verifier declarations = %d, want 1", len(moduleCopy.Verify))
	}
	activeModule := repinValidationModule(t, &moduleCopy)
	fixture.catalog = map[string]*modules.Module{activeModule.ID: activeModule}

	mf := manifestForValidationModule(activeModule)
	descriptor := fixture.context.Descriptor()
	mf.Apps = []manifest.App{{
		ID: descriptor.Inventory.AppID, Driver: descriptor.Inventory.Driver, Source: descriptor.Inventory.Source,
		Refs: map[string]string{"windows": descriptor.Inventory.Ref},
	}}
	data, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestValidationRunVerifyUsesMappedVerifierAndKeepsResultSemantic(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	testVerifier := modules.VerifyDef{Type: "file-exists", Path: `%USERPROFILE%\.logseq\preferences.json`}
	commandFixtureWithSingleTestVerifier(t, &fixture, testVerifier)
	originalLoad := loadModuleCatalogFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	t.Cleanup(func() { loadModuleCatalogFn = originalLoad })

	userProfile, ok := fixture.context.VirtualRoot("USERPROFILE")
	if !ok {
		t.Fatal("USERPROFILE virtual root missing")
	}
	mapped := filepath.Join(userProfile, ".logseq", "preferences.json")
	if err := os.MkdirAll(filepath.Dir(mapped), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapped, []byte("sandbox-verifier-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, commandErr := RunVerify(VerifyFlags{Manifest: fixture.manifestPath})
	if commandErr != nil {
		t.Fatalf("RunVerify() error = %v", commandErr)
	}
	result := raw.(*VerifyResult)
	var found bool
	for _, item := range result.Results {
		if item.Type != "file-exists" {
			continue
		}
		found = true
		if item.Status != "pass" || !strings.Contains(item.Message, `%USERPROFILE%\.logseq\preferences.json`) {
			t.Fatalf("mapped verifier result = %+v", item)
		}
		for _, forbidden := range []string{fixture.context.Root(), fixture.context.Descriptor().Nonce, "sandbox-verifier-sentinel"} {
			if strings.Contains(strings.ToLower(item.Message), strings.ToLower(forbidden)) {
				t.Fatalf("verifier result leaked %q: %+v", forbidden, item)
			}
		}
	}
	if !found {
		t.Fatalf("file verifier result missing: %+v", result.Results)
	}
}

func TestValidationRunVerifyRecordsVerifierIsolationWithoutPublicCodeMapping(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	testVerifier := modules.VerifyDef{Type: "file-exists", Path: `%APPDATA%\Vendor\settings.json`}
	commandFixtureWithSingleTestVerifier(t, &fixture, testVerifier)
	originalLoad := loadModuleCatalogFn
	originalRun := runVerifierFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	called := false
	runVerifierFn = func(entries []manifest.VerifyEntry, _ *validationmode.Context) ([]verifier.VerifyResult, error) {
		called = true
		if len(entries) != 1 || entries[0].Type != testVerifier.Type || entries[0].Path != testVerifier.Path {
			t.Fatalf("verifier entries = %#v, want exactly %#v", entries, testVerifier)
		}
		return []verifier.VerifyResult{{Type: "file-exists", Path: `%APPDATA%\Vendor\settings.json`, Message: "File check rejected"}},
			errors.Join(errors.New("wrapped verifier failure"), validationmode.ErrUnsafePath)
	}
	t.Cleanup(func() {
		loadModuleCatalogFn = originalLoad
		runVerifierFn = originalRun
	})

	_, commandErr := RunVerify(VerifyFlags{Manifest: fixture.manifestPath})
	if !called {
		t.Fatal("test verifier was not called")
	}
	if commandErr == nil || commandErr.Code != envelope.ErrInternalError {
		t.Fatalf("command error = %+v, want generic internal isolation failure", commandErr)
	}
	if commandErr.Code == envelope.ErrTestModeIsolationViolation {
		t.Fatalf("command layer mapped public validation code: %+v", commandErr)
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr == nil || !errors.Is(isolationErr, validationmode.ErrUnsafePath) {
		t.Fatalf("session isolation error = %v", isolationErr)
	}
}

func TestValidationManualVerifyPathUsesMappedStateAndRejectsOutsideBeforeStat(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "manual", Driver: "winget", Ref: "Vendor.Manual", DisplayName: "Manual", InitialState: "present",
	})
	restoreEnvironment, err := context.Activate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoreEnvironment() })
	appData, _ := context.VirtualRoot("APPDATA")
	mapped := filepath.Join(appData, "Vendor", "manual.exe")
	if err := os.MkdirAll(filepath.Dir(mapped), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapped, []byte("manual"), 0o700); err != nil {
		t.Fatal(err)
	}

	display, exists, checkErr := checkVerifyPathWithValidation(`%APPDATA%\Vendor\manual.exe`, context)
	if checkErr != nil || !exists || display != `%APPDATA%\Vendor\manual.exe` {
		t.Fatalf("mapped manual check = (%q, %v, %v)", display, exists, checkErr)
	}

	originalStat := manualVerifyStatNative
	manualVerifyStatNative = func(string) (os.FileInfo, error) { panic("native manual stat reached") }
	t.Cleanup(func() { manualVerifyStatNative = originalStat })
	if _, _, checkErr := checkVerifyPathWithValidation(filepath.Join(t.TempDir(), "outside.exe"), context); !errors.Is(checkErr, validationmode.ErrUnsafePath) {
		t.Fatalf("outside manual error = %v, want ErrUnsafePath", checkErr)
	}
}

func TestValidationRebuildReusesSessionAcrossNestedApplyAndFailedVerifierAssertion(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	testVerifier := modules.VerifyDef{Type: "file-exists", Path: `%APPDATA%\Vendor\missing.json`}
	commandFixtureWithSingleTestVerifier(t, &fixture, testVerifier)
	originalLoad := loadModuleCatalogFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	t.Cleanup(func() { loadModuleCatalogFn = originalLoad })

	raw, commandErr := RunRebuild(RebuildFlags{From: fixture.manifestPath, NoRestore: true})
	if commandErr != nil {
		t.Fatalf("nested rebuild error = %+v isolation=%v", commandErr, fixture.session.IsolationError())
	}
	result := raw.(*RebuildResult)
	verifyResult, ok := result.Verify.(*VerifyResult)
	if !ok || verifyResult.Summary.Total != 2 || verifyResult.Summary.Pass != 1 || verifyResult.Summary.Fail != 1 || verifyResult.Summary.Skipped != 0 {
		t.Fatalf("nested verifier result = %#v", result.Verify)
	}
	var appPass, fileExistsFail bool
	for _, item := range verifyResult.Results {
		switch {
		case item.Type == "app" && item.Status == "pass":
			appPass = true
		case item.Type == "file-exists" && item.Status == "fail" && strings.Contains(item.Message, testVerifier.Path):
			fileExistsFail = true
		}
	}
	if !appPass || !fileExistsFail {
		t.Fatalf("nested verifier semantics = %#v, want app pass and missing file-exists fail", verifyResult.Results)
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr != nil {
		t.Fatalf("ordinary nested assertion poisoned session: %v", isolationErr)
	}
}
