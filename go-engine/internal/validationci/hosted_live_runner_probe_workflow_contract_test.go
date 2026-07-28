// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHostedLiveRunnerProbeWorkflowContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	workflow, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", ".github", "workflows", "hosted-live-runner-probe.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(workflow, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		t.Fatal("workflow must contain one top-level mapping")
	}
	root := document.Content[0]
	requirePullRequestOnly(t, mappingValue(root, "on"))
	requireEmptyPermissions(t, mappingValue(root, "permissions"))
	run := requireSingleProbeStep(t, mappingValue(root, "jobs"))
	requirePowerShellAST(t, run)

	text := strings.ReplaceAll(string(workflow), "\r\n", "\n")
	for _, wanted := range []string{
		"name: Hosted Live Runner Probe", "runs-on: windows-11-arm", "timeout-minutes: 5", "Set-StrictMode -Version Latest", "$ErrorActionPreference = 'Stop'", "WINGET_CAPABILITY_UNSUPPORTED:", "exit 1", "$env:ImageOS", "$env:ImageVersion", "OSArchitecture", "ProcessArchitecture",
		"Get-AppxPackage -Name Microsoft.DesktopAppInstaller -PackageTypeFilter Main", "IsFramework -eq $false", "IsResourcePackage -eq $false", "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe", "PublisherId -ne '8wekyb3d8bbwe'", "^Microsoft\\.DesktopAppInstaller_\\d+\\.\\d+\\.\\d+\\.\\d+_arm64__8wekyb3d8bbwe$", "$package.ResourceId",
		"Program Files\\WindowsApps", "Assert-NoReparsePath", "Get-FileHash -LiteralPath $canonicalWingetPath -Algorithm SHA256", "Get-AuthenticodeSignature -LiteralPath $canonicalWingetPath", "Status -ne 'Valid'", "SignerCertificate", "winget.exe",
		"Read-BoundedProcessOutput", "ReadAsync", "Task]::WhenAny", "Task]::Delay", "ExitCodeDecimal", "ExitCodeHex", "[System.BitConverter]::ToUInt32", "--version", "--info", "ConvertTo-Json -Depth 5 -Compress", "Write-Output",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("workflow missing %q", wanted)
		}
	}
	for _, forbidden := range []string{
		"push:", "pull_request_target:", "workflow_dispatch:", "schedule:", "branches:", "paths:", "paths-ignore:", "uses:", "actions/checkout", "${{", "secrets.", "GITHUB_TOKEN", "github.token", "self-hosted",
		"-AllUsers", "Get-Command winget", "$env:PATH", "App Execution Alias", "winget list", "winget search", "winget source", "winget install", "winget uninstall", "winget upgrade", "winget download", "winget export", "winget import", "winget configure", "winget settings",
		"Add-AppxPackage", "Remove-AppxPackage", "Repair-WinGetPackageManager", "Invoke-WebRequest", "Start-BitsTransfer", "curl ", "wget ", "Start-Process", "Out-File", "Set-Content", "Add-Content", "GITHUB_STEP_SUMMARY", "upload-artifact", "download-artifact", "New-Item", "Remove-Item", "ReadToEndAsync",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("workflow contains forbidden %q", forbidden)
		}
	}
}

func requirePullRequestOnly(t *testing.T, node *yaml.Node) {
	t.Helper()
	if node == nil || node.Kind != yaml.MappingNode || len(node.Content) != 2 || node.Content[0].Value != "pull_request" {
		t.Fatal("workflow must have exactly one pull_request trigger")
	}
	trigger := node.Content[1]
	if !((trigger.Kind == yaml.ScalarNode && trigger.Tag == "!!null") || (trigger.Kind == yaml.MappingNode && len(trigger.Content) == 0)) {
		t.Fatal("pull_request trigger must be unfiltered")
	}
}

func requireEmptyPermissions(t *testing.T, node *yaml.Node) {
	t.Helper()
	if node == nil || node.Kind != yaml.MappingNode || len(node.Content) != 0 {
		t.Fatal("workflow must declare top-level permissions: {}")
	}
}

func requireSingleProbeStep(t *testing.T, jobs *yaml.Node) string {
	t.Helper()
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) != 2 {
		t.Fatal("workflow must have exactly one job")
	}
	job := jobs.Content[1]
	if job.Kind != yaml.MappingNode || mappingValue(job, "permissions") != nil {
		t.Fatal("workflow job must not override permissions")
	}
	if value := mappingValue(job, "runs-on"); value == nil || value.Value != "windows-11-arm" {
		t.Fatal("workflow job must run on windows-11-arm")
	}
	if value := mappingValue(job, "timeout-minutes"); value == nil || value.Value != "5" {
		t.Fatal("workflow job must have the five-minute timeout")
	}
	steps := mappingValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode || len(steps.Content) != 1 || steps.Content[0].Kind != yaml.MappingNode {
		t.Fatal("workflow job must have exactly one step")
	}
	step := steps.Content[0]
	if mappingValue(step, "uses") != nil {
		t.Fatal("workflow step must not use an action")
	}
	if shell := mappingValue(step, "shell"); shell == nil || shell.Value != "pwsh" {
		t.Fatal("workflow step must use pwsh")
	}
	run := mappingValue(step, "run")
	if run == nil || run.Kind != yaml.ScalarNode || run.Style&yaml.LiteralStyle == 0 || strings.TrimSpace(run.Value) == "" {
		t.Fatal("workflow step must have one inline literal PowerShell script")
	}
	return run.Value
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
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
