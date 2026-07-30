// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"os"
	"os/exec"
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
		"validate-v1", "run-v1-lane", "aggregate-v1", "--dispatch-commit", "$GITHUB_SHA", "$env:GITHUB_SHA", "--role baseline", "--role detector", "--role comparator", "if: always()", "if-no-files-found: error",
		"--runner-root", "--runner-image-os", "--runner-image-version", "ImageOS", "ImageVersion",
		"GOCACHE: ${{ runner.temp }}/v1-owned/go-build", "GOMODCACHE: ${{ runner.temp }}/v1-owned/go-mod", "--go-cache", "--go-mod-cache",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("workflow missing %q", wanted)
		}
	}
	for _, forbidden := range []string{
		"pull_request", "push:", "schedule:", "actions/checkout", "GITHUB_TOKEN", "GH_TOKEN", "secrets.", "ConvertTo-Json", "ConvertFrom-Json", "jq", "notepad", "winget", "choco", "brew", "apt-get", "Remove-Item", "rm -rf", "git config", "--depth=1",
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
	for wanted, count := range map[string]int{
		"GOCACHE: ${{ runner.temp }}/v1-owned/go-build":  5,
		"GOMODCACHE: ${{ runner.temp }}/v1-owned/go-mod": 5,
		"--go-cache":     5,
		"--go-mod-cache": 5,
	} {
		if got := strings.Count(text, wanted); got != count {
			t.Errorf("workflow has %d occurrences of %q, want %d", got, wanted, count)
		}
	}
}

func TestEfficacyAuditV1CorpusIsPinnedToLF(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	const wanted = "validation/ci-efficacy/pilot-v1/** text eol=lf"
	if !hasActiveAttributeRule(text, wanted) {
		t.Errorf(".gitattributes missing %q", wanted)
	}

	paths := []string{
		"validation/ci-efficacy/pilot-v1/manifest.json",
		"validation/ci-efficacy/pilot-v1/review.md",
		"validation/ci-efficacy/pilot-v1/nested/patch.diff",
		"validation/ci-efficacy/pilot-v0/manifest.json",
	}
	args := append([]string{"-C", repoRoot, "check-attr", "--cached", "text", "eol", "--"}, paths...)
	attrs, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{
		"validation/ci-efficacy/pilot-v1/manifest.json: text: set",
		"validation/ci-efficacy/pilot-v1/manifest.json: eol: lf",
		"validation/ci-efficacy/pilot-v1/review.md: text: set",
		"validation/ci-efficacy/pilot-v1/review.md: eol: lf",
		"validation/ci-efficacy/pilot-v1/nested/patch.diff: text: set",
		"validation/ci-efficacy/pilot-v1/nested/patch.diff: eol: lf",
		"validation/ci-efficacy/pilot-v0/manifest.json: text: auto",
		"validation/ci-efficacy/pilot-v0/manifest.json: eol: unspecified",
	} {
		if !strings.Contains(string(attrs), wanted) {
			t.Errorf("git check-attr output missing %q:\n%s", wanted, attrs)
		}
	}
}

func TestHasActiveAttributeRuleRejectsCommentsAndExtraAttributes(t *testing.T) {
	const rule = "validation/ci-efficacy/pilot-v1/** text eol=lf"
	for name, test := range map[string]struct {
		text string
		want bool
	}{
		"active exact rule": {text: rule, want: true},
		"commented rule":    {text: "# " + rule, want: false},
		"extra attribute":   {text: rule + " -diff", want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := hasActiveAttributeRule(test.text, rule); got != test.want {
				t.Errorf("hasActiveAttributeRule(%q) = %t, want %t", test.text, got, test.want)
			}
		})
	}
}

func hasActiveAttributeRule(text, rule string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Join(strings.Fields(line), " ") == rule {
			return true
		}
	}
	return false
}
