// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"slices"
	"strings"
	"testing"
)

func TestCatalogRegression_iTunesSecretsRemainPortableFileBoundaries(t *testing.T) {
	catalog, err := LoadCatalog(productionModulesRoot())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog["apps.itunes"]
	if mod == nil || mod.Secrets == nil {
		t.Fatalf("iTunes secret boundary is incomplete: %+v", mod)
	}
	for _, want := range []string{
		`%APPDATA%\Apple Computer\Preferences\ByHost`,
		`%USERPROFILE%\Apple`,
	} {
		if !slices.Contains(mod.Secrets.Files, want) {
			t.Errorf("iTunes secrets.files missing %q", want)
		}
	}
	for _, path := range mod.Secrets.Files {
		if strings.HasPrefix(path, "HKCU\\") {
			t.Errorf("iTunes secrets.files contains registry path %q", path)
		}
	}
	if !strings.Contains(mod.Secrets.Warning, "registry") {
		t.Errorf("iTunes secret warning no longer documents registry state: %q", mod.Secrets.Warning)
	}
}

func TestCatalogRegression_NextcloudSecretsDoNotTreatCredentialManagerProseAsFile(t *testing.T) {
	catalog, err := LoadCatalog(productionModulesRoot())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog["apps.nextcloud"]
	if mod == nil || mod.Secrets == nil {
		t.Fatalf("Nextcloud secret boundary is incomplete: %+v", mod)
	}
	for _, path := range mod.Secrets.Files {
		if strings.Contains(path, "Credential Manager") {
			t.Errorf("Nextcloud secrets.files contains explanatory prose %q", path)
		}
	}
	if !strings.Contains(mod.Secrets.Warning, "Windows Credential Manager") {
		t.Errorf("Nextcloud secret warning no longer documents Credential Manager state: %q", mod.Secrets.Warning)
	}
}

func TestCatalogRegression_AutoHotkeyVerifierUsesPortableProgramFilesRoot(t *testing.T) {
	catalog, err := LoadCatalog(productionModulesRoot())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog["apps.autohotkey"]
	if mod == nil || len(mod.Verify) != 1 {
		t.Fatalf("AutoHotkey verifier = %+v", mod)
	}
	if got, want := mod.Verify[0], (VerifyDef{Type: "file-exists", Path: `%ProgramFiles%\AutoHotkey\v2\AutoHotkey64.exe`}); got != want {
		t.Errorf("AutoHotkey verifier = %+v, want %+v", got, want)
	}
}
