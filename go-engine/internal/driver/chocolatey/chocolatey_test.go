// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

type scriptedResponse struct {
	exitCode int
	stdout   string
	stderr   string
}

func scriptedCommand(responses map[string]scriptedResponse, calls *[][]string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		if calls != nil {
			*calls = append(*calls, append([]string{name}, args...))
		}
		key := strings.Join(args, " ")
		response, ok := responses[key]
		if !ok {
			response = scriptedResponse{exitCode: 99, stderr: "UNSCRIPTED COMMAND: " + name + " " + key}
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"FAKE_EXIT_CODE="+strconv.Itoa(response.exitCode),
			"FAKE_STDOUT="+response.stdout,
			"FAKE_STDERR="+response.stderr,
		)
		return cmd
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, os.Getenv("FAKE_STDOUT"))
	fmt.Fprint(os.Stderr, os.Getenv("FAKE_STDERR"))
	exitCode, _ := strconv.Atoi(os.Getenv("FAKE_EXIT_CODE"))
	os.Exit(exitCode)
}

func TestChocolateyDriverCapabilities(t *testing.T) {
	var d any = &ChocolateyDriver{}
	if _, ok := d.(driver.Driver); !ok {
		t.Fatal("ChocolateyDriver must implement driver.Driver")
	}
	if _, ok := d.(driver.BatchDetector); !ok {
		t.Fatal("ChocolateyDriver must implement driver.BatchDetector")
	}
	if _, ok := d.(driver.VersionedInstaller); !ok {
		t.Fatal("ChocolateyDriver must implement driver.VersionedInstaller")
	}
	if _, ok := d.(driver.Uninstaller); !ok {
		t.Fatal("ChocolateyDriver must implement driver.Uninstaller")
	}
	if _, ok := d.(driver.InstalledEnumerator); !ok {
		t.Fatal("ChocolateyDriver must implement driver.InstalledEnumerator")
	}
}

func TestName(t *testing.T) {
	if got := New().Name(); got != "chocolatey" {
		t.Fatalf("Name() = %q, want chocolatey", got)
	}
}

func TestFindChocolateyExecutablePrefersPATH(t *testing.T) {
	const pathExecutable = `C:\tools\choco.exe`
	knownPathChecked := false

	got, err := findChocolateyExecutable(
		func(name string) (string, error) {
			if name != executable {
				t.Fatalf("LookPath name = %q, want %q", name, executable)
			}
			return pathExecutable, nil
		},
		func(string) string { return `C:\ProgramData` },
		func(string) bool {
			knownPathChecked = true
			return true
		},
	)
	if err != nil || got != pathExecutable {
		t.Fatalf("findChocolateyExecutable = (%q, %v), want (%q, nil)", got, err, pathExecutable)
	}
	if knownPathChecked {
		t.Fatal("known ProgramData path must not be checked when PATH resolves Chocolatey")
	}
}

func TestFindChocolateyExecutableFallsBackToProgramData(t *testing.T) {
	programData := `C:\ProgramData`
	want := filepath.Join(programData, "chocolatey", "bin", "choco.exe")

	got, err := findChocolateyExecutable(
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(name string) string {
			if name != "ProgramData" {
				t.Fatalf("Getenv name = %q, want ProgramData", name)
			}
			return programData
		},
		func(path string) bool { return path == want },
	)
	if err != nil || got != want {
		t.Fatalf("findChocolateyExecutable = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestFindChocolateyExecutableReportsMissing(t *testing.T) {
	got, err := findChocolateyExecutable(
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(string) string { return `C:\ProgramData` },
		func(string) bool { return false },
	)
	if got != "" || !errors.Is(err, ErrChocolateyNotAvailable) {
		t.Fatalf("findChocolateyExecutable = (%q, %v), want empty path and ErrChocolateyNotAvailable", got, err)
	}
}

func TestOperationsUseResolvedExecutable(t *testing.T) {
	const resolvedExecutable = `C:\ProgramData\chocolatey\bin\choco.exe`
	var calls [][]string
	d := &ChocolateyDriver{
		Executable: resolvedExecutable,
		ExecCommand: scriptedCommand(map[string]scriptedResponse{
			"--version":           {stdout: "2.4.1\n"},
			"list --limit-output": {stdout: "git|2.46.0\n"},
		}, &calls),
	}

	if _, err := d.DetectBatch([]string{"git"}); err != nil {
		t.Fatalf("DetectBatch returned error: %v", err)
	}
	for _, call := range calls {
		if call[0] != resolvedExecutable {
			t.Fatalf("command executable = %q, want %q (calls: %v)", call[0], resolvedExecutable, calls)
		}
	}
}

func TestResolvedExecutableMissingDoesNotInvokeCommand(t *testing.T) {
	commandInvoked := false
	d := newWithExecutableResolver(
		func(string, ...string) *exec.Cmd {
			commandInvoked = true
			return exec.Command(os.Args[0])
		},
		func() (string, error) { return "", ErrChocolateyNotAvailable },
	)

	_, err := d.DetectBatch([]string{"git"})
	if !errors.Is(err, ErrChocolateyNotAvailable) {
		t.Fatalf("DetectBatch error = %v, want ErrChocolateyNotAvailable", err)
	}
	if commandInvoked {
		t.Fatal("command runner must not be invoked when executable resolution failed")
	}
}

func TestInstallLatest(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":                           {stdout: "2.4.1\n"},
		"list ripgrep --exact --limit-output": {stdout: ""},
		"install ripgrep --yes --no-progress --limit-output": {stdout: "installed\n"},
	}, &calls)}

	result, err := d.Install("ripgrep")
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if result.Status != driver.StatusInstalled || result.RebootRequired {
		t.Fatalf("Install result = %+v, want installed without reboot", result)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %v, want version, exact local list, install", calls)
	}
	assertConfiguredSourcesUntouched(t, calls)
}

func TestInstallAlreadyPresentSkipsInstall(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":                       {stdout: "2.4.1\n"},
		"list git --exact --limit-output": {stdout: "git|2.46.0\n"},
	}, &calls)}

	result, err := d.Install("git")
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if result.Status != driver.StatusPresent || result.Reason != driver.ReasonAlreadyInstalled {
		t.Fatalf("Install result = %+v, want already present", result)
	}
	for _, call := range calls {
		if len(call) > 1 && call[1] == "install" {
			t.Fatalf("already-present package invoked install: %v", calls)
		}
	}
}

func TestInstallFailureIsItemFailure(t *testing.T) {
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":                           {stdout: "2.4.1\n"},
		"list missing --exact --limit-output": {stdout: ""},
		"install missing --yes --no-progress --limit-output": {exitCode: 1, stderr: "package not found"},
	}, nil)}

	result, err := d.Install("missing")
	if err != nil {
		t.Fatalf("expected item failure, got infrastructure error: %v", err)
	}
	if result.Status != driver.StatusFailed || result.Reason != driver.ReasonInstallFailed {
		t.Fatalf("Install result = %+v, want failed/install_failed", result)
	}
}

func TestInstallMissingBinaryIsInfrastructureError(t *testing.T) {
	d := &ChocolateyDriver{ExecCommand: func(string, ...string) *exec.Cmd {
		return exec.Command("endstate-no-such-choco-binary-xyz")
	}}

	result, err := d.Install("git")
	if result != nil || err != ErrChocolateyNotAvailable {
		t.Fatalf("Install = (%+v, %v), want (nil, ErrChocolateyNotAvailable)", result, err)
	}
}

func TestDetectBatchReturnsVersionsAndMisses(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":           {stdout: "2.4.1\n"},
		"list --limit-output": {stdout: "ripgrep|14.1.0\nGit|2.46.0\n"},
	}, &calls)}

	results, err := d.DetectBatch([]string{"RIPGREP", "git", "missing"})
	if err != nil {
		t.Fatalf("DetectBatch returned error: %v", err)
	}
	if got := results["RIPGREP"]; !got.Installed || got.DisplayName != "ripgrep" || got.Version != "14.1.0" {
		t.Errorf("RIPGREP = %+v", got)
	}
	if got := results["git"]; !got.Installed || got.DisplayName != "Git" || got.Version != "2.46.0" {
		t.Errorf("git = %+v", got)
	}
	if got := results["missing"]; got.Installed || got.DisplayName != "" || got.Version != "" {
		t.Errorf("missing = %+v", got)
	}
	if len(calls) != 2 {
		t.Fatalf("DetectBatch calls = %v, want one version check and one list", calls)
	}
}

func assertConfiguredSourcesUntouched(t *testing.T, calls [][]string) {
	t.Helper()
	for _, call := range calls {
		for _, arg := range call {
			lower := strings.ToLower(arg)
			if lower == "--source" || lower == "-s" || strings.HasPrefix(lower, "--source=") {
				t.Fatalf("driver must use configured sources without overriding them: %v", calls)
			}
		}
	}
}
