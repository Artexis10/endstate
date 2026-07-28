// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBindWindowsLiveOperationTemplateResolvesOnlyFixedSemanticSlots(t *testing.T) {
	template := liveTestCampaignOperations()[2]
	root := filepath.Join(t.TempDir(), "attempt")
	checkout := t.TempDir()
	bound, err := bindWindowsLiveOperationTemplate(template, root, checkout, liveTrustedAppXBinding{}, liveTrustedPowerShellBinding{}, map[string]string{"PATH": `C:\\Windows\\System32`, "ENDSTATE_ROOT": root})
	if err != nil {
		t.Fatalf("bindWindowsLiveOperationTemplate() error = %v", err)
	}
	if bound.Executable != filepath.Join(checkout, "go-engine", "endstate.exe") || bound.Directory != filepath.Join(checkout, "go-engine") || !sameLiveArguments(bound.Arguments, []string{"apply", "--manifest", filepath.Join(root, "manifests", "install.jsonc"), "--events", "jsonl", "--json"}) || bound.Environment["ENDSTATE_ROOT"] != root {
		t.Fatalf("bound operation = %+v", bound)
	}
	for _, mutate := range []func(*LiveCampaignOperation){
		func(value *LiveCampaignOperation) { value.Arguments[2] = root + `\\foreign` },
		func(value *LiveCampaignOperation) { value.Executable = "$CHECKOUT_ROOT\\..\\endstate.exe" },
		func(value *LiveCampaignOperation) { value.Environment["ENDSTATE_ROOT"] = "$CHECKOUT_ROOT" },
	} {
		candidate := template
		candidate.Arguments = append([]string(nil), template.Arguments...)
		candidate.Environment = cloneLiveEnvironment(template.Environment)
		mutate(&candidate)
		if _, err := bindWindowsLiveOperationTemplate(candidate, root, checkout, liveTrustedAppXBinding{}, liveTrustedPowerShellBinding{}, map[string]string{"PATH": `C:\\Windows\\System32`, "ENDSTATE_ROOT": root}); err == nil {
			t.Fatal("runtime binder accepted a substituted template")
		}
	}
}

func TestBindWindowsLiveOperationTemplateBindsWingetFromTrustedBinding(t *testing.T) {
	template := liveTestCampaignOperations()[0]
	binding := liveTrustedAppXBinding{metadata: liveAppXPackageMetadata{packageRoot: `C:\\Program Files\\WindowsApps\\Microsoft.DesktopAppInstaller`, executableName: "winget.exe", receipt: liveTrustedAppXReceipt{valid: true}}}
	bound, err := bindWindowsLiveOperationTemplate(template, `C:\\attempt`, `C:\\checkout`, binding, liveTrustedPowerShellBinding{}, map[string]string{"PATH": `C:\\Windows\\System32`})
	if err != nil {
		t.Fatalf("bindWindowsLiveOperationTemplate() error = %v", err)
	}
	if !strings.EqualFold(bound.Executable, filepath.Join(binding.metadata.packageRoot, binding.metadata.executableName)) || bound.ExecutableSHA256 == liveTemplateWinget {
		t.Fatalf("winget binding was not concrete: %+v", bound)
	}
}

func TestBindWindowsLiveOperationTemplateStagesSeedBehindPowerShellOnly(t *testing.T) {
	template := liveTestCampaignOperations()[4]
	root := filepath.Join(t.TempDir(), "attempt")
	powershell := liveTrustedPowerShellBinding{path: `C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe`, sha256: [32]byte{1}}
	bound, err := bindWindowsLiveOperationTemplate(template, root, t.TempDir(), liveTrustedAppXBinding{}, powershell, map[string]string{"APPDATA": `C:\\Users\\runner\\AppData\\Roaming`, "PATH": `C:\\Windows\\System32`, "ENDSTATE_ROOT": root})
	if err != nil {
		t.Fatalf("bindWindowsLiveOperationTemplate(seed) error = %v", err)
	}
	if bound.Executable != powershell.path || bound.Directory != filepath.Join(root, "seed") || !sameLiveArguments(bound.Arguments, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "seed", "seed.ps1")}) || len(bound.Environment) != 1 || bound.Environment["APPDATA"] == "" {
		t.Fatalf("bound seed = %+v", bound)
	}
	template.Arguments[0] = "-Command"
	if _, err := bindWindowsLiveOperationTemplate(template, root, t.TempDir(), liveTrustedAppXBinding{}, powershell, map[string]string{"APPDATA": `C:\\Users\\runner\\AppData\\Roaming`}); err == nil {
		t.Fatal("seed authority accepted an arbitrary PowerShell command")
	}
}
