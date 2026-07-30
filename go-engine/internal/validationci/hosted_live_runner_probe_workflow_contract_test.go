// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestHostedLiveRunnerProbeWorkflowContract(t *testing.T) {
	workflow := readRunnerProbeWorkflow(t)
	script, err := extractRunnerProbeScript(workflow)
	if err != nil {
		t.Fatal(err)
	}
	requirePowerShellAST(t, script)

	for _, wanted := range []string{
		"name: Hosted Live Runner Probe", "runs-on: windows-11-arm", "timeout-minutes: 5", "Set-StrictMode -Version Latest", "$ErrorActionPreference = 'Stop'", "WINGET_CAPABILITY_UNSUPPORTED:", "exit 1", "$env:ImageOS", "$env:ImageVersion", "OSArchitecture", "ProcessArchitecture",
		"Get-AppxPackage -Name Microsoft.DesktopAppInstaller -PackageTypeFilter Main", "IsFramework -eq $false", "IsResourcePackage -eq $false", "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe", "PublisherId -ne '8wekyb3d8bbwe'", "^Microsoft\\.DesktopAppInstaller_\\d+\\.\\d+\\.\\d+\\.\\d+_arm64__8wekyb3d8bbwe$", "$package.ResourceId",
		"Program Files\\WindowsApps", "Assert-NoReparsePath", "Get-FileHash -LiteralPath $canonicalWingetPath -Algorithm SHA256", "Get-AuthenticodeSignature -LiteralPath $canonicalWingetPath", "Status -ne 'Valid'", "SignerCertificate", "winget.exe",
		"Read-BoundedProcessOutput", "ReadAsync", "Task]::WhenAny", "Task]::Delay", "\"$stdoutOutput`n$stderrOutput\"", "ExitCodeDecimal", "ExitCodeHex", "[System.BitConverter]::ToUInt32", "--version", "--info", "ConvertTo-Json -Depth 5 -Compress", "Write-Output",
	} {
		if !strings.Contains(workflow, wanted) {
			t.Errorf("workflow missing %q", wanted)
		}
	}
	for _, forbidden := range []string{
		"push:", "pull_request_target:", "workflow_dispatch:", "schedule:", "branches:", "paths:", "paths-ignore:", "uses:", "actions/checkout", "${{", "secrets.", "GITHUB_TOKEN", "github.token", "self-hosted",
		"-AllUsers", "Get-Command winget", "$env:PATH", "App Execution Alias", "winget list", "winget search", "winget source", "winget install", "winget uninstall", "winget upgrade", "winget download", "winget export", "winget import", "winget configure", "winget settings",
		"Add-AppxPackage", "Remove-AppxPackage", "Repair-WinGetPackageManager", "Invoke-WebRequest", "Start-BitsTransfer", "curl ", "wget ", "Start-Process", "Out-File", "Set-Content", "Add-Content", "GITHUB_STEP_SUMMARY", "upload-artifact", "download-artifact", "New-Item", "Remove-Item", "ReadToEndAsync",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("workflow contains forbidden %q", forbidden)
		}
	}
}

func TestExtractRunnerProbeScriptRejectsStructuralEscapes(t *testing.T) {
	workflow := readRunnerProbeWorkflow(t)
	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{
			name: "filtered pull request",
			mutate: func(text string) string {
				return strings.Replace(text, "  pull_request:\n", "  pull_request:\n    branches: [main]\n", 1)
			},
		},
		{
			name: "nonempty permissions",
			mutate: func(text string) string {
				return strings.Replace(text, "permissions: {}", "permissions: {contents: read}", 1)
			},
		},
		{
			name: "folded run script",
			mutate: func(text string) string {
				return strings.Replace(text, "        run: |", "        run: >", 1)
			},
		},
		{
			name: "job permissions override",
			mutate: func(text string) string {
				return text + "    permissions: {contents: write}\n"
			},
		},
		{
			name: "second step",
			mutate: func(text string) string {
				return text + "      - name: Extra\n        shell: pwsh\n        run: |\n          Write-Output extra\n"
			},
		},
		{
			name: "second job",
			mutate: func(text string) string {
				return text + "  extra:\n    runs-on: windows-11-arm\n"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := extractRunnerProbeScript(tt.mutate(workflow)); err == nil {
				t.Fatal("extractRunnerProbeScript() error = nil, want structural rejection")
			}
		})
	}
}

func TestHostedLiveRunnerProbeDoesNotAddYAMLDependency(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	goMod, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(goMod), "gopkg.in/yaml.v3") {
		t.Fatal("runner probe contract must not add gopkg.in/yaml.v3")
	}
}

func readRunnerProbeWorkflow(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	workflow, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", ".github", "workflows", "hosted-live-runner-probe.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(workflow), "\r\n", "\n")
}

func extractRunnerProbeScript(workflow string) (string, error) {
	lines := strings.Split(workflow, "\n")
	header := []string{
		"name: Hosted Live Runner Probe", "", "on:", "  pull_request:", "", "permissions: {}", "", "jobs:", "  runner-probe:",
		"    runs-on: windows-11-arm", "    timeout-minutes: 5", "    steps:", "      - name: Probe Winget capability", "        shell: pwsh", "        run: |",
	}
	if len(lines) < len(header)+1 {
		return "", fmt.Errorf("workflow is shorter than the required runner probe shape")
	}
	for i, want := range header {
		if lines[i] != want {
			return "", fmt.Errorf("workflow line %d = %q, want %q", i+1, lines[i], want)
		}
	}

	var script strings.Builder
	for _, line := range lines[len(header):] {
		if line == "" {
			script.WriteByte('\n')
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			return "", fmt.Errorf("workflow contains nonblank content outside the literal run script: %q", line)
		}
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	if strings.TrimSpace(script.String()) == "" {
		return "", fmt.Errorf("workflow literal run script is empty")
	}
	return script.String(), nil
}

func requirePowerShellAST(t *testing.T, script string) {
	t.Helper()
	const parser = `$source = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($env:ENDSTATE_RUNNER_PROBE_SCRIPT))
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$errors)
$invocations = @($ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.CommandAst] -and $node.CommandElements.Count -gt 0 -and $node.CommandElements[0].Extent.Text -eq 'Invoke-ReadOnlyWinget' }, $true) | ForEach-Object { ,@($_.CommandElements | ForEach-Object { $_.Extent.Text }) })
[PSCustomObject]@{ errors = @($errors | ForEach-Object { $_.Message }); invocations = $invocations } | ConvertTo-Json -Depth 4 -Compress`
	command := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-Command", parser)
	command.Env = append(os.Environ(), "ENDSTATE_RUNNER_PROBE_SCRIPT="+base64.StdEncoding.EncodeToString([]byte(script)))
	output, err := command.Output()
	if err != nil {
		t.Fatalf("PowerShell AST parse failed: %v", err)
	}
	var result struct {
		Errors      []string   `json:"errors"`
		Invocations [][]string `json:"invocations"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode PowerShell AST result: %v\n%s", err, output)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("PowerShell syntax errors: %s", strings.Join(result.Errors, "; "))
	}
	want := [][]string{
		{"Invoke-ReadOnlyWinget", "-Argument", "'--version'"},
		{"Invoke-ReadOnlyWinget", "-Argument", "'--info'"},
	}
	if !reflect.DeepEqual(result.Invocations, want) {
		t.Fatalf("direct Winget helper invocations = %#v, want %#v", result.Invocations, want)
	}
}
