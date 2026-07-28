// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"crypto/sha256"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/wingetauthority"
)

func TestBindWindowsLiveOperationTemplateResolvesOnlyFixedSemanticSlots(t *testing.T) {
	template := liveTestCampaignOperations()[2]
	root := filepath.Join(t.TempDir(), "attempt")
	checkout := t.TempDir()
	bound, err := bindWindowsLiveOperationTemplate(template, root, checkout, liveTestTrustedAppXBinding(t, [32]byte{1}), liveTrustedPowerShellBinding{}, map[string]string{"PATH": `C:\\Windows\\System32`, "ENDSTATE_ROOT": root})
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
		if _, err := bindWindowsLiveOperationTemplate(candidate, root, checkout, liveTestTrustedAppXBinding(t, [32]byte{1}), liveTrustedPowerShellBinding{}, map[string]string{"PATH": `C:\\Windows\\System32`, "ENDSTATE_ROOT": root}); err == nil {
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

func TestBindWindowsLiveOperationTemplateBindsHostedWingetAuthorityOnlyToEngineOperations(t *testing.T) {
	root, checkout := `C:\\attempt`, `C:\\checkout`
	digest := sha256.Sum256([]byte("trusted-winget"))
	binding := liveTestTrustedAppXBinding(t, digest)
	encoded, err := wingetauthority.Encode(filepath.Join(binding.metadata.packageRoot, binding.metadata.executableName), digest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	input := map[string]string{
		"APPDATA": `C:\\Users\\runner\\AppData\\Roaming`,
		strings.ToLower(wingetauthority.StrictEnvironment):    "ambient-spoof",
		strings.ToLower(wingetauthority.AuthorityEnvironment): "ambient-spoof",
	}

	for _, template := range liveTestCampaignOperations() {
		bound, err := bindWindowsLiveOperationTemplate(template, root, checkout, binding, liveTrustedPowerShellBinding{path: `C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe`, sha256: [32]byte{1}}, input)
		if err != nil {
			t.Fatalf("bindWindowsLiveOperationTemplate(%s) error = %v", template.Operation, err)
		}
		strict, hasStrict := bound.Environment[wingetauthority.StrictEnvironment]
		authority, hasAuthority := bound.Environment[wingetauthority.AuthorityEnvironment]
		if liveCampaignEngineOperation(liveOperation(template.Operation)) {
			if !hasStrict || strict != wingetauthority.StrictValue || !hasAuthority || authority != encoded {
				t.Fatalf("engine %s authority = %#v, want strict marker and exact capability", template.Operation, bound.Environment)
			}
			path, gotDigest, err := wingetauthority.Decode(authority)
			if err != nil || path != filepath.Join(binding.metadata.packageRoot, binding.metadata.executableName) || gotDigest != digest {
				t.Fatalf("engine %s authority decode = (%q, %x, %v)", template.Operation, path, gotDigest, err)
			}
			continue
		}
		if hasStrict || hasAuthority || liveHostedWingetAuthorityCount(bound.Environment) != 0 {
			t.Fatalf("non-engine %s received hosted authority: %#v", template.Operation, bound.Environment)
		}
	}
}

func liveTestTrustedAppXBinding(t *testing.T, digest [32]byte) liveTrustedAppXBinding {
	t.Helper()
	appsRoot, err := liveWindowsAppsRoot()
	if err != nil {
		t.Fatalf("liveWindowsAppsRoot() error = %v", err)
	}
	metadata := liveAppXPackageMetadata{
		familyName: "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe", fullName: "Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe",
		packageRoot: filepath.Join(appsRoot, "Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe"), executableName: "winget.exe", receipt: liveTrustedAppXReceipt{sha256: digest, valid: true},
	}
	binding, err := newLiveTrustedAppXBinding(metadata)
	if err != nil {
		t.Fatalf("newLiveTrustedAppXBinding() error = %v", err)
	}
	return binding
}

func liveHostedWingetAuthorityCount(environment map[string]string) int {
	count := 0
	for name := range environment {
		if strings.EqualFold(name, wingetauthority.StrictEnvironment) || strings.EqualFold(name, wingetauthority.AuthorityEnvironment) {
			count++
		}
	}
	return count
}

func TestBindWindowsLiveOperationTemplateRejectsInvalidWingetReceiptForEngine(t *testing.T) {
	template := liveTestCampaignOperations()[2]
	if _, err := bindWindowsLiveOperationTemplate(template, `C:\\attempt`, `C:\\checkout`, liveTrustedAppXBinding{}, liveTrustedPowerShellBinding{}, nil); err == nil {
		t.Fatal("engine binding accepted an invalid trusted Winget receipt")
	}
}

func TestBindWindowsLiveOperationTemplateRejectsDriftedTrustedWingetMetadataForEngine(t *testing.T) {
	template := liveTestCampaignOperations()[2]
	binding := liveTestTrustedAppXBinding(t, [32]byte{1})
	binding.metadata.executableName = "winget-shadow.exe"
	if _, err := bindWindowsLiveOperationTemplate(template, `C:\\attempt`, `C:\\checkout`, binding, liveTrustedPowerShellBinding{}, nil); err == nil {
		t.Fatal("engine binding accepted drifted trusted Winget metadata")
	}
}

func TestTrustedWindowsLiveRuntimeEnvironmentDoesNotCopyHostedAuthority(t *testing.T) {
	t.Setenv(wingetauthority.StrictEnvironment, "ambient-spoof")
	t.Setenv(wingetauthority.AuthorityEnvironment, "ambient-spoof")
	environment, err := trustedWindowsLiveRuntimeEnvironment(`C:\\attempt`)
	if err != nil {
		t.Fatalf("trustedWindowsLiveRuntimeEnvironment() error = %v", err)
	}
	if _, exists := environment[wingetauthority.StrictEnvironment]; exists {
		t.Fatalf("runtime environment copied ambient strict marker: %#v", environment)
	}
	if _, exists := environment[wingetauthority.AuthorityEnvironment]; exists {
		t.Fatalf("runtime environment copied ambient authority: %#v", environment)
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
