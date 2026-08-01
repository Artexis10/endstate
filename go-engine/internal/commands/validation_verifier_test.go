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

func TestValidationRunVerifyUsesMappedVerifierAndKeepsResultSemantic(t *testing.T) {
	fixture := commandPreflightFixture(t, "notepad-plus-plus")
	originalModule := fixture.catalog["apps.notepad-plus-plus"]
	moduleCopy := *originalModule
	moduleCopy.Verify = []modules.VerifyDef{{Type: "file-exists", Path: `%USERPROFILE%\.logseq\preferences.json`}}
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
	originalLoad := loadModuleCatalogFn
	originalRun := runVerifierFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	runVerifierFn = func([]manifest.VerifyEntry, *validationmode.Context) ([]verifier.VerifyResult, error) {
		return []verifier.VerifyResult{{Type: "file-exists", Path: `%APPDATA%\Vendor\settings.json`, Message: "File check rejected"}},
			errors.Join(errors.New("wrapped verifier failure"), validationmode.ErrUnsafePath)
	}
	t.Cleanup(func() {
		loadModuleCatalogFn = originalLoad
		runVerifierFn = originalRun
	})

	_, commandErr := RunVerify(VerifyFlags{Manifest: fixture.manifestPath})
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
	originalLoad := loadModuleCatalogFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	t.Cleanup(func() { loadModuleCatalogFn = originalLoad })

	raw, commandErr := RunRebuild(RebuildFlags{From: fixture.manifestPath, NoRestore: true})
	if commandErr != nil {
		t.Fatalf("nested rebuild error = %+v isolation=%v", commandErr, fixture.session.IsolationError())
	}
	result := raw.(*RebuildResult)
	verifyResult, ok := result.Verify.(*VerifyResult)
	if !ok || verifyResult.Summary.Fail != 1 || verifyResult.Summary.Pass != 1 {
		t.Fatalf("nested verifier result = %#v", result.Verify)
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr != nil {
		t.Fatalf("ordinary nested assertion poisoned session: %v", isolationErr)
	}
}

func TestValidationDriverRebuildBindsManifestModuleWithoutPackageMatcher(t *testing.T) {
	fixture := validationDriverCommandFixture(t, []string{"apps.notepad-plus-plus"})
	installValidationDriverCommandCatalog(t, fixture)

	raw, commandErr := RunRebuild(RebuildFlags{From: fixture.manifestPath, NoRestore: true})
	if commandErr != nil {
		t.Fatalf("validation-driver rebuild error = %+v isolation=%v", commandErr, fixture.session.IsolationError())
	}
	result := raw.(*RebuildResult)
	applyResult, ok := result.Apply.(*ApplyResult)
	if !ok || applyResult.ConfigModuleMap["notepad-plus-plus"] != "apps.notepad-plus-plus" || len(applyResult.PackageModuleMap) != 0 {
		t.Fatalf("nested apply ownership = %#v", result.Apply)
	}
	if _, ok := result.Verify.(*VerifyResult); !ok {
		t.Fatalf("nested verify result = %#v", result.Verify)
	}
	if isolationErr := fixture.session.IsolationError(); isolationErr != nil {
		t.Fatalf("validation-driver rebuild poisoned session: %v", isolationErr)
	}
}

func TestValidationDriverVerifyRejectsNonExactConfigModuleAuthority(t *testing.T) {
	for _, configModules := range [][]string{
		nil,
		{"apps.foreign"},
		{"apps.notepad-plus-plus", "apps.foreign"},
	} {
		t.Run(strings.Join(configModules, ","), func(t *testing.T) {
			fixture := validationDriverCommandFixture(t, configModules)
			installValidationDriverCommandCatalog(t, fixture)

			_, commandErr := RunVerify(VerifyFlags{Manifest: fixture.manifestPath})
			assertValidationCommandPreflightFailure(t, fixture, commandErr)
		})
	}
}

func TestValidationDriverVerifyRejectsMalformedAuthorityBeforePathExistsFallback(t *testing.T) {
	fixture := validationDriverCommandFixtureWithPathExists(t, []string{"apps.foreign"}, true)
	installValidationDriverCommandCatalog(t, fixture)
	if ambient := modules.MatchModulesForAppsIncludingInstall(fixture.catalog, []manifest.App{{Driver: "validation"}}); len(ambient) != 1 {
		t.Fatalf("hostile ambient matcher = %+v", ambient)
	}

	_, commandErr := RunVerify(VerifyFlags{Manifest: fixture.manifestPath})
	assertValidationCommandPreflightFailure(t, fixture, commandErr)
}

func validationDriverCommandFixture(t *testing.T, configModules []string) commandPreflightTestFixture {
	return validationDriverCommandFixtureWithOptions(t, configModules, "", false)
}

func validationDriverCommandFixtureWithPathExists(t *testing.T, configModules []string, pathExists bool) commandPreflightTestFixture {
	return validationDriverCommandFixtureWithOptions(t, configModules, "", pathExists)
}

func validationDriverCommandFixtureWithInventoryVersion(t *testing.T, configModules []string, version string) commandPreflightTestFixture {
	return validationDriverCommandFixtureWithOptions(t, configModules, version, false)
}

func validationDriverCommandFixtureWithOptions(t *testing.T, configModules []string, version string, pathExists bool) commandPreflightTestFixture {
	t.Helper()
	mod := loadValidationProductionModule(t, "notepad-plus-plus")
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "validation", Ref: "notepad-plus-plus",
		DisplayName: mod.DisplayName, Version: version, InitialState: "present",
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
	moduleCopy := *mod
	moduleCopy.Matches.Winget = nil
	moduleCopy.Matches.Chocolatey = nil
	moduleCopy.Matches.PathExists = nil
	if pathExists {
		appData, ok := context.VirtualRoot("APPDATA")
		if !ok {
			t.Fatal("APPDATA virtual root missing")
		}
		matchPath := filepath.Join(appData, "ambient-bypass.txt")
		if err := os.WriteFile(matchPath, []byte("present"), 0o600); err != nil {
			t.Fatal(err)
		}
		moduleCopy.Matches.PathExists = []string{matchPath}
	}
	mod = repinValidationModule(t, &moduleCopy)

	mf := manifestForValidationModule(mod)
	mf.ConfigModules = append([]string(nil), configModules...)
	mf.Apps = []manifest.App{{
		ID: "notepad-plus-plus", Driver: "validation", DisplayName: mod.DisplayName, Version: version,
		Refs: map[string]string{"windows": "notepad-plus-plus"},
	}}
	manifestPath := filepath.Join(context.Root(), "manifests", "validation-driver.jsonc")
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

func installValidationDriverCommandCatalog(t *testing.T, fixture commandPreflightTestFixture) {
	t.Helper()
	originalLoad := loadModuleCatalogFn
	loadModuleCatalogFn = func(string) (map[string]*modules.Module, error) { return fixture.catalog, nil }
	t.Cleanup(func() { loadModuleCatalogFn = originalLoad })
}
