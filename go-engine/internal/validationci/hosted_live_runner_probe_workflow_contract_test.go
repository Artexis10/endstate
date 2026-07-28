// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHostedLiveRunnerProbeWorkflowContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	workflow, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", ".github", "workflows", "hosted-live-runner-probe.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(workflow), "\r\n", "\n")
	for _, wanted := range []string{
		"name: Hosted Live Runner Probe", "on:\n  pull_request:\n", "permissions: {}", "runner-probe:", "runs-on: windows-11-arm", "timeout-minutes: 5", "shell: pwsh", "run: |",
		"Set-StrictMode -Version Latest", "$ErrorActionPreference = 'Stop'", "WINGET_CAPABILITY_UNSUPPORTED:", "exit 1", "$env:ImageOS", "$env:ImageVersion", "OSArchitecture", "ProcessArchitecture",
		"Get-AppxPackage -Name Microsoft.DesktopAppInstaller -PackageTypeFilter Main", "IsFramework -eq $false", "IsResourcePackage -eq $false", "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe", "PublisherId -ne '8wekyb3d8bbwe'", "^Microsoft\\.DesktopAppInstaller_\\d+\\.\\d+\\.\\d+\\.\\d+_arm64__8wekyb3d8bbwe$", "$package.ResourceId",
		"Program Files\\WindowsApps", "Assert-NoReparsePath", "Get-FileHash -LiteralPath $canonicalWingetPath -Algorithm SHA256", "Get-AuthenticodeSignature -LiteralPath $canonicalWingetPath", "Status -ne 'Valid'", "SignerCertificate", "winget.exe",
		"StandardOutputEncoding", "StandardErrorEncoding", "UTF8Encoding", "ExitCodeDecimal", "ExitCodeHex", "[System.BitConverter]::ToUInt32", "--version", "--info", "ConvertTo-Json -Depth 5 -Compress", "Write-Output",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("workflow missing %q", wanted)
		}
	}
	for _, forbidden := range []string{
		"push:", "pull_request_target:", "workflow_dispatch:", "schedule:", "branches:", "paths:", "paths-ignore:", "uses:", "actions/checkout", "${{", "secrets.", "GITHUB_TOKEN", "github.token", "self-hosted",
		"-AllUsers", "Get-Command winget", "$env:PATH", "App Execution Alias", "winget list", "winget search", "winget source", "winget install", "winget uninstall", "winget upgrade", "winget download", "winget export", "winget import", "winget configure", "winget settings",
		"Add-AppxPackage", "Remove-AppxPackage", "Repair-WinGetPackageManager", "Invoke-WebRequest", "Start-BitsTransfer", "curl ", "wget ", "Start-Process", "Out-File", "Set-Content", "Add-Content", "GITHUB_STEP_SUMMARY", "upload-artifact", "download-artifact", "New-Item", "Remove-Item",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("workflow contains forbidden %q", forbidden)
		}
	}
	if got := strings.Count(text, "runs-on:"); got != 1 {
		t.Errorf("workflow runs-on declarations = %d, want 1", got)
	}
	if got := strings.Count(text, "shell: pwsh"); got != 1 {
		t.Errorf("workflow pwsh steps = %d, want 1", got)
	}
	if got := strings.Count(text, "run: |"); got != 1 {
		t.Errorf("workflow inline run steps = %d, want 1", got)
	}
	if got := strings.Count(text, "Invoke-ReadOnlyWinget -Argument '--version'"); got != 1 {
		t.Errorf("workflow winget --version invocations = %d, want 1", got)
	}
	if got := strings.Count(text, "Invoke-ReadOnlyWinget -Argument '--info'"); got != 1 {
		t.Errorf("workflow winget --info invocations = %d, want 1", got)
	}
}
