// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	mutated := strings.Replace(workflow, "Extract-BaseAuthority $parents[1]", "Extract-BaseAuthority $parents[2]", 1)
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

func TestGoCIAuthorityStepHandlesBaseLedgerAuthority(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("authority step is PowerShell for Windows runners")
	}
	t.Run("missing base ledger exits zero without authority file", func(t *testing.T) {
		fixture := newAuthorityFixture(t, nil, nil)
		result := runAuthorityStep(t, fixture, fixture.base, fixture.head, "")
		if result.exitCode != 0 {
			t.Fatalf("authority step exit = %d, output=%s", result.exitCode, result.output)
		}
		if _, err := os.Lstat(result.authorityPath); !os.IsNotExist(err) {
			t.Fatalf("missing base ledger produced authority file: %v", err)
		}
	})
	t.Run("base ledger is copied byte exact", func(t *testing.T) {
		ledger := []byte("{\n  \"schemaVersion\": 1\n}\n")
		fixture := newAuthorityFixture(t, ledger, nil)
		result := runAuthorityStep(t, fixture, fixture.base, fixture.head, "")
		if result.exitCode != 0 {
			t.Fatalf("authority step exit = %d, output=%s", result.exitCode, result.output)
		}
		got, err := os.ReadFile(result.authorityPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(ledger) {
			t.Fatalf("authority bytes = %q, want %q", got, ledger)
		}
	})
	t.Run("parent two cannot authorize", func(t *testing.T) {
		ledger := []byte("{\"schemaVersion\":1}\n")
		fixture := newAuthorityFixture(t, nil, ledger)
		result := runAuthorityStep(t, fixture, fixture.base, fixture.head, "")
		if result.exitCode != 0 {
			t.Fatalf("authority step exit = %d, output=%s", result.exitCode, result.output)
		}
		if _, err := os.Lstat(result.authorityPath); !os.IsNotExist(err) {
			t.Fatalf("parent-two ledger produced authority file: %v", err)
		}
	})
	t.Run("base and head event parents are enforced", func(t *testing.T) {
		fixture := newAuthorityFixture(t, nil, nil)
		for _, parents := range [][2]string{{fixture.head, fixture.head}, {fixture.base, fixture.base}} {
			result := runAuthorityStep(t, fixture, parents[0], parents[1], "")
			if result.exitCode == 0 {
				t.Fatalf("authority step accepted event parents %q, %q", parents[0], parents[1])
			}
		}
	})
	t.Run("unexpected git probe error fails closed", func(t *testing.T) {
		fixture := newAuthorityFixture(t, nil, nil)
		gitPath := failingGitCommand(t, "cat-file")
		result := runAuthorityStep(t, fixture, fixture.base, fixture.head, gitPath)
		if result.exitCode == 0 {
			t.Fatalf("authority step accepted git cat-file failure: %s", result.output)
		}
	})
	t.Run("git show error fails closed", func(t *testing.T) {
		fixture := newAuthorityFixture(t, []byte("{}\n"), nil)
		gitPath := failingGitCommand(t, "show")
		result := runAuthorityStep(t, fixture, fixture.base, fixture.head, gitPath)
		if result.exitCode == 0 {
			t.Fatalf("authority step accepted git show failure: %s", result.output)
		}
	})
	t.Run("authority write error fails closed", func(t *testing.T) {
		fixture := newAuthorityFixture(t, []byte("{}\n"), nil)
		result := runAuthorityStepWithOptions(t, fixture, fixture.base, fixture.head, authorityStepOptions{blockAuthorityWrite: true})
		if result.exitCode == 0 {
			t.Fatalf("authority step accepted authority write failure: %s", result.output)
		}
	})
}

type authorityFixture struct {
	repo  string
	base  string
	head  string
	merge string
}

type authorityStepResult struct {
	authorityPath string
	exitCode      int
	output        string
}

type authorityStepOptions struct {
	gitPath             string
	blockAuthorityWrite bool
}

func newAuthorityFixture(t *testing.T, baseLedger, headLedger []byte) authorityFixture {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "validation@example.test")
	runGit(t, repo, "config", "user.name", "Validation Test")
	writeAuthorityFixtureFile(t, repo, "base.txt", []byte("base\n"))
	if baseLedger != nil {
		writeAuthorityFixtureFile(t, repo, filepath.Join(".github", "validation", "synthetic-known-failures.json"), baseLedger)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	base := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "switch", "-c", "feature")
	writeAuthorityFixtureFile(t, repo, "feature.txt", []byte("feature\n"))
	if headLedger != nil {
		writeAuthorityFixtureFile(t, repo, filepath.Join(".github", "validation", "synthetic-known-failures.json"), headLedger)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "feature")
	head := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "switch", "main")
	runGit(t, repo, "merge", "--no-ff", "feature", "-m", "merge feature")
	return authorityFixture{repo: repo, base: base, head: head, merge: runGit(t, repo, "rev-parse", "HEAD")}
}

func writeAuthorityFixtureFile(t *testing.T, repo, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(repo, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func runAuthorityStep(t *testing.T, fixture authorityFixture, base, head, gitPath string) authorityStepResult {
	return runAuthorityStepWithOptions(t, fixture, base, head, authorityStepOptions{gitPath: gitPath})
}

func runAuthorityStepWithOptions(t *testing.T, fixture authorityFixture, base, head string, options authorityStepOptions) authorityStepResult {
	t.Helper()
	runnerTemp := filepath.Join(t.TempDir(), "runner-temp")
	if err := os.Mkdir(runnerTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(runnerTemp, "base-known-failures.json")
	if options.blockAuthorityWrite {
		if err := os.Mkdir(authorityPath, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	eventPath := filepath.Join(t.TempDir(), "event.json")
	event, err := json.Marshal(map[string]any{"pull_request": map[string]any{"base": map[string]string{"sha": base}, "head": map[string]string{"sha": head}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventPath, event, 0o600); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(t.TempDir(), "authority.ps1")
	if err := os.WriteFile(scriptPath, []byte(authorityStepScript(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-Command", "& '"+scriptPath+"'; exit $LASTEXITCODE")
	command.Dir = fixture.repo
	command.Env = append(os.Environ(), "GITHUB_SHA="+fixture.merge, "GITHUB_EVENT_NAME=pull_request", "GITHUB_REF=refs/pull/1/merge", "GITHUB_EVENT_PATH="+eventPath, "RUNNER_TEMP="+runnerTemp)
	if options.gitPath != "" {
		command.Env = append(command.Env, "PATH="+filepath.Dir(options.gitPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	output, err := command.CombinedOutput()
	result := authorityStepResult{authorityPath: authorityPath, output: string(output)}
	if err == nil {
		return result
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		result.exitCode = exitError.ExitCode()
		return result
	}
	t.Fatalf("run authority step: %v", err)
	return authorityStepResult{}
}

func authorityStepScript(t *testing.T) string {
	t.Helper()
	aggregateJob, found := workflowJob(readGoCIWorkflow(t), "verified-module-matrix")
	if !found {
		t.Fatal("missing aggregate job")
	}
	step, found := workflowNamedStep(aggregateJob, "Extract known-failure authority")
	if !found {
		t.Fatal("missing authority step")
	}
	const marker = "        run: |\n"
	start := strings.Index(step, marker)
	if start < 0 {
		t.Fatal("authority step has no PowerShell script")
	}
	lines := strings.Split(step[start+len(marker):], "\n")
	for index, line := range lines {
		lines[index] = strings.TrimPrefix(line, "          ")
	}
	return strings.Join(lines, "\n")
}

func failingGitCommand(t *testing.T, commandName string) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "git.cmd")
	script := "@echo off\r\nif /I \"%~1\"==\"" + commandName + "\" exit /b 2\r\n\"" + realGit + "\" %*\r\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
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
		"function Copy-GitBlob",
		"$process.StartInfo.ArgumentList.Add(\"git show $reference\")",
		"function Extract-BaseAuthority",
		"git cat-file -e $reference",
		"Extract-BaseAuthority $parents[1]",
		"$env:GITHUB_EVENT_NAME -eq 'pull_request'",
		"$env:GITHUB_EVENT_NAME -eq 'push' -and $env:GITHUB_REF -eq 'refs/heads/main'",
		"Extract-BaseAuthority $env:GITHUB_SHA",
	} {
		if !strings.Contains(authorityStep, wanted) {
			violations = append(violations, fmt.Sprintf("authority step missing %q", wanted))
		}
	}
	if strings.Contains(authorityStep, "Extract-BaseAuthority $parents[2]") {
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
