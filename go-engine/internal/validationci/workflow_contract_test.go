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
}

func TestEfficacyAuditWorkflowKeepsTypedV1ProofContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", ".github", "workflows", "ci-efficacy-audit.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	for _, wanted := range []string{
		"workflow_dispatch:", "permissions: {}", "prepare:", "windows:", "ubuntu:", "macos:", "aggregate:",
		"actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16", "go-version: '1.26'", "cache-dependency-path: audit/go-engine/go.sum",
		"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02", "actions/download-artifact@634f93cb2916e3fdff6788551b99b062d0335ce0",
		"validate-v1", "run-v1-lane", "aggregate-v1", "--role baseline", "--role detector", "--role comparator", "if: always()", "if-no-files-found: error",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("workflow missing %q", wanted)
		}
	}
	for _, forbidden := range []string{
		"pull_request", "push:", "schedule:", "actions/checkout", "GITHUB_TOKEN", "GH_TOKEN", "secrets.", "ConvertTo-Json", "ConvertFrom-Json", "jq", "notepad", "winget", "choco", "brew", "apt-get", "Remove-Item", "rm -rf", "git config",
	} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Errorf("workflow contains forbidden %q", forbidden)
		}
	}
	if got := strings.Count(text, "runs-on:"); got != 5 {
		t.Errorf("workflow job count = %d, want 5", got)
	}
	if got := strings.Count(text, "timeout-minutes:"); got != 5 {
		t.Errorf("workflow timeout count = %d, want 5", got)
	}
}
