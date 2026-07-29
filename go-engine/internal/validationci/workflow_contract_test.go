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

func TestGoCIWorkflowKeepsVerifiedModuleMatrixContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	workflow, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", ".github", "workflows", "go-ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(workflow), "\r\n", "\n")
	for _, wanted := range []string{
		"name: Go Tests", "permissions:\n  contents: read", "pull_request:", "persist-credentials: false",
		"actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09", "actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16",
		"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02", "actions/download-artifact@634f93cb2916e3fdff6788551b99b062d0335ce0",
		"fail-fast: false", "shard: [0, 1, 2, 3, 4, 5, 6, 7]", "timeout-minutes: 15", "name: Verified Module Matrix", "if: always()",
		"Run synthetic Notepad++ engine-contract canary", "name: Upload compact aggregate evidence", "name: verified-module-matrix-aggregate", "if-no-files-found: ignore", "name: notepad-engine-contract", "retention-days: 7",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("workflow missing %q", wanted)
		}
	}
	for _, forbidden := range []string{"pull_request_target", "self-hosted", "continue-on-error", "winget install", "secrets."} {
		if strings.Contains(text, forbidden) {
			t.Errorf("workflow contains forbidden %q", forbidden)
		}
	}
	if got := strings.Count(text, "path: ${{ runner.temp }}/endstate-bin"); got != 4 {
		t.Errorf("validation binary downloads outside the repository = %d, want 4", got)
	}
	for _, invocation := range []string{"& $validator shard", "& $validator catalog", "& $validator canary", "& $validator aggregate"} {
		if !strings.Contains(text, invocation) {
			t.Errorf("workflow does not use isolated validation binary invocation %q", invocation)
		}
	}
}

func TestEfficacyAuditWorkflowKeepsFixedHostedPreflightContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	workflow, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", ".github", "workflows", "ci-efficacy-audit.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(workflow), "\r\n", "\n")
	for _, wanted := range []string{
		"workflow_dispatch:", "permissions: {}", "fail-fast: false", "max-parallel: 6",
		"ab8065cd67ab3f4e9e876e07a25facf3100c28c7", "437c0ca4167c09bc9f2de515daa6d55d35257d4f",
		"ea165f8d65b6e75b540449e92b4886f43607fa02", "634f93cb2916e3fdff6788551b99b062d0335ce0",
		"windows-latest", "ubuntu-latest", "macos-latest", "Invoke-Native go @('vet','./...')", "Invoke-Native go @('test','./...')", "./integration-test.ps1",
		"bundle-duplicate", "bundle-missing", "bundle-id-drift", "vlc-backup-off", "alacritty-source-drift", "obs-target-drift",
		"$GITHUB_SHA", "$GITHUB_SHA:refs/audit/dispatch", "ab8065cd67ab3f4e9e876e07a25facf3100c28c7:refs/audit/legacy", "437c0ca4167c09bc9f2de515daa6d55d35257d4f:refs/audit/detector", "audit-kit", " detector --", " infrastructure --", "Get-FileHash $patchPath -Algorithm SHA256", "candidateId", "patchSha256", "windows-go", "windows-integration", "ubuntu-go", "macos-go", "LOCALAPPDATA", "Endstate\\bin", "try { Invoke-Native", "finally { Pop-Location }", "exit 0",
		"Join-Path $env:RUNNER_TEMP 'endstate-validation-results'", "Join-Path $env:RUNNER_TEMP 'endstate.exe'", "Join-Path $env:RUNNER_TEMP \"endstate-$n.exe\"", "Join-Path $resultRoot \"catalog-$n.json\"", "Join-Path $resultRoot \"detector-$n.json\"",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("workflow missing %q", wanted)
		}
	}
	for _, forbidden := range []string{
		"pull_request", "push:", "schedule:", "actions/checkout", "GITHUB_TOKEN", "GH_TOKEN", "secrets.", "winget install", "choco install", "brew install", "apt-get install", "rm -rf", "Remove-Item", "setx", "with: {", "for ref in \"$GITHUB_SHA\"", "$env:RUNNER_TEMP/endstate",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("workflow contains forbidden %q", forbidden)
		}
	}
	for _, wanted := range []string{
		"apply --check", " apply ", "../audit/validation/ci-efficacy/pilot-v0/patches/${{ matrix.id }}/legacy.patch", "patches/${{ matrix.id }}/detector.patch", "../$patchPath", "detector_setup", "$pushed",
		"--catalog", "--module $module --scenario default-v1", "detector-$n", "find evidence -mindepth 1 -maxdepth 1 -type d -name 'efficacy-*'",
		"endstate-validation-pilot aggregate", "aggregate.json", "efficacy-baseline", "efficacy-${{ matrix.id }}-${{ matrix.os }}",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("workflow missing proof binding %q", wanted)
		}
	}
	if strings.Index(text, "Run two fresh patched detector attempts") > strings.Index(text, "Write bounded candidate evidence") {
		t.Error("candidate evidence is written before both detector attempts")
	}
	if got := strings.Count(text, "          exit 0"); got != 2 {
		t.Errorf("intentional evidence-authority exits = %d, want 2", got)
	}
}
