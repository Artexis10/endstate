// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoCIWorkflowKeepsVerifiedModuleMatrixContract(t *testing.T) {
	for _, violation := range workflowContractViolations(t, readGoCIWorkflow(t)) {
		t.Error(violation)
	}
}

func TestGoCIWorkflowContractRejectsParentTwoAuthorityExtraction(t *testing.T) {
	workflow := readGoCIWorkflow(t)
	mutated := strings.Replace(workflow, `git show "$($parents[1]):$ledgerPath"`, `git show "$($parents[2]):$ledgerPath"`, 1)
	if mutated == workflow {
		t.Fatal("parent-one extraction mutation did not apply")
	}
	for _, violation := range workflowContractViolations(t, mutated) {
		if strings.Contains(violation, "parent-one ledger extraction") {
			return
		}
	}
	t.Fatal("workflow contract accepted parent-two authority extraction")
}

func readGoCIWorkflow(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	workflow, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", ".github", "workflows", "go-ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(workflow), "\r\n", "\n")
}

func workflowContractViolations(t *testing.T, workflow string) []string {
	t.Helper()
	var violations []string
	for _, wanted := range []string{
		"name: Go Tests", "permissions:\n  contents: read", "pull_request:",
		"actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09", "actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16",
		"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02", "actions/download-artifact@634f93cb2916e3fdff6788551b99b062d0335ce0",
		"fail-fast: false", "shard: [0, 1, 2, 3, 4, 5, 6, 7]", "timeout-minutes: 15", "name: Verified Module Matrix", "if: always()",
		"Run synthetic Notepad++ engine-contract canary", "name: Upload compact aggregate evidence", "name: verified-module-matrix-aggregate", "if-no-files-found: ignore", "name: notepad-engine-contract", "retention-days: 7",
	} {
		if !strings.Contains(workflow, wanted) {
			violations = append(violations, fmt.Sprintf("workflow missing %q", wanted))
		}
	}
	for _, forbidden := range []string{"pull_request_target", "self-hosted", "continue-on-error", "winget install", "secrets.", "GITHUB_TOKEN", "github.token", "gh api", "git checkout", "synthetic-known-failures.json' | Set-Content"} {
		if strings.Contains(workflow, forbidden) {
			violations = append(violations, fmt.Sprintf("workflow contains forbidden %q", forbidden))
		}
	}
	if got := strings.Count(workflow, "path: ${{ runner.temp }}/endstate-bin"); got != 4 {
		violations = append(violations, fmt.Sprintf("validation binary downloads outside the repository = %d, want 4", got))
	}
	for _, invocation := range []string{"& $validator shard", "& $validator catalog", "& $validator canary", "& $validator aggregate"} {
		if !strings.Contains(workflow, invocation) {
			violations = append(violations, fmt.Sprintf("workflow does not use isolated validation binary invocation %q", invocation))
		}
	}

	checkouts := workflowCheckoutSteps(workflow)
	if len(checkouts) == 0 {
		violations = append(violations, "workflow has no checkout steps")
	}
	for index, checkout := range checkouts {
		if !strings.Contains(checkout, "persist-credentials: false") {
			violations = append(violations, fmt.Sprintf("checkout step %d persists credentials", index+1))
		}
	}

	aggregateJob, found := workflowJob(workflow, "verified-module-matrix")
	if !found {
		return append(violations, "missing verified-module-matrix job")
	}
	aggregateCheckouts := workflowCheckoutSteps(aggregateJob)
	if len(aggregateCheckouts) != 1 {
		violations = append(violations, fmt.Sprintf("aggregate job checkout count = %d, want 1", len(aggregateCheckouts)))
	} else if !strings.Contains(aggregateCheckouts[0], "fetch-depth: 2") {
		violations = append(violations, "aggregate checkout does not use fetch-depth: 2")
	}
	if got := strings.Count(workflow, "fetch-depth: 2"); got != 1 {
		violations = append(violations, fmt.Sprintf("fetch-depth: 2 count = %d, want 1 on aggregate checkout", got))
	}

	authorityStep, found := workflowNamedStep(aggregateJob, "Extract known-failure authority")
	if !found {
		return append(violations, "aggregate job has no known-failure authority step")
	}
	for _, wanted := range []string{
		"$head = (git rev-parse HEAD).Trim().ToLowerInvariant()",
		"$head -ne $env:GITHUB_SHA.ToLowerInvariant()",
		"$parents = ((git rev-list --parents -n 1 HEAD).Trim() -split '\\s+')",
		"$parents.Count -ne 3",
		"$parents[1].ToLowerInvariant() -ne $event.pull_request.base.sha.ToLowerInvariant()",
		"$parents[2].ToLowerInvariant() -ne $event.pull_request.head.sha.ToLowerInvariant()",
		"git cat-file -e \"$($parents[1]):$ledgerPath\"",
		"git show \"$($parents[1]):$ledgerPath\"",
		"$env:GITHUB_EVENT_NAME -eq 'pull_request'",
		"$env:GITHUB_EVENT_NAME -eq 'push' -and $env:GITHUB_REF -eq 'refs/heads/main'",
		"git show \"$env:GITHUB_SHA`:$ledgerPath\"",
	} {
		if !strings.Contains(authorityStep, wanted) {
			violations = append(violations, fmt.Sprintf("authority step missing %q", wanted))
		}
	}
	if strings.Contains(authorityStep, `git cat-file -e "$($parents[2]):$ledgerPath"`) || strings.Contains(authorityStep, `git show "$($parents[2]):$ledgerPath"`) {
		violations = append(violations, "authority step does not use only parent-one ledger extraction")
	}
	aggregateStep, found := workflowNamedStep(aggregateJob, "Aggregate compact evidence")
	if !found || !strings.Contains(aggregateStep, "--base-authority $baseAuthority --head-candidate $headCandidate") || !strings.Contains(aggregateStep, "'.github\\validation\\synthetic-known-failures.json'") {
		violations = append(violations, "aggregate step does not pass separate base authority and fixed head candidate")
	}
	return violations
}

func workflowCheckoutSteps(workflow string) []string {
	var steps []string
	for _, step := range workflowSteps(workflow) {
		if strings.Contains(step, "uses: actions/checkout@") {
			steps = append(steps, step)
		}
	}
	return steps
}

func workflowNamedStep(job, name string) (string, bool) {
	for _, step := range workflowSteps(job) {
		if strings.HasPrefix(strings.TrimSpace(step), "- name: "+name+"\n") {
			return step, true
		}
	}
	return "", false
}

func workflowSteps(workflow string) []string {
	lines := strings.Split(workflow, "\n")
	var steps []string
	start := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "      - ") {
			if start >= 0 {
				steps = append(steps, strings.Join(lines[start:index], "\n"))
			}
			start = index
		}
	}
	if start >= 0 {
		steps = append(steps, strings.Join(lines[start:], "\n"))
	}
	return steps
}

func workflowJob(workflow, name string) (string, bool) {
	lines := strings.Split(workflow, "\n")
	start := -1
	for index, line := range lines {
		if line == "  "+name+":" {
			start = index
			continue
		}
		if start >= 0 && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			return strings.Join(lines[start:index], "\n"), true
		}
	}
	if start < 0 {
		return "", false
	}
	return strings.Join(lines[start:], "\n"), true
}
