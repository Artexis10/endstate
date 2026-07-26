// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestLiveProcessRejectsZeroValueRequest(t *testing.T) {
	if err := validateLiveProcessRequest(LiveProcessRequest{}); err == nil {
		t.Fatal("validateLiveProcessRequest() accepted a zero-value request")
	}
}

func TestLiveProcessRejectsMutationWithoutTrustedPermit(t *testing.T) {
	_, err := runLiveProcess(context.Background(), newLiveMutationRequest(trustedLiveMutationPermit{}, LiveExecutionWinget, liveProbeExecutable(t), []string{"install", "Vendor.Fixture"}, nil, 0))
	if err == nil {
		t.Fatal("runLiveProcess() error = nil, want mutation denial")
	}
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionMutationDenied {
		t.Fatalf("runLiveProcess() error = %T %v, want mutation denial", err, err)
	}
}

func TestLiveProcessPermitIsInMemoryAndNonWindowsFailsClosed(t *testing.T) {
	request := newLiveEngineMutation(newTrustedLiveMutationPermit(), liveProbeExecutable(t), []string{"/d", "/c", "exit 0"}, liveTestEnvironment(t), 0)
	if runtime.GOOS != "windows" {
		_, err := runLiveProcess(context.Background(), request)
		var executionErr *LiveExecutionError
		if err == nil || !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionUnsupported {
			t.Fatalf("runLiveProcess() error = %T %v, want unsupported platform", err, err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runLiveProcess(ctx, request)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("runLiveProcess() = (%+v, %v), want zero exit", result, err)
	}
}

func TestLiveProcessEnvironmentDoesNotInheritSecrets(t *testing.T) {
	const secretName = "ENDSTATE_LIVE_TEST_SECRET"
	t.Setenv(secretName, "must-not-be-inherited")
	environment, err := liveProcessEnvironment(map[string]string{"PATH": `C:\Windows\System32`})
	if err != nil {
		t.Fatalf("liveProcessEnvironment() error = %v", err)
	}
	for _, value := range environment {
		if strings.HasPrefix(value, secretName+"=") {
			t.Fatalf("environment inherited secret %q", value)
		}
	}
	if !containsLiveEnvironment(environment, "PATH=C:\\Windows\\System32") {
		t.Fatalf("environment = %q, want explicit PATH", environment)
	}
	if _, err := liveProcessEnvironment(map[string]string{"AWS_SECRET_ACCESS_KEY": "nope"}); err == nil {
		t.Fatal("liveProcessEnvironment() accepted an unapproved environment key")
	}
	if os.Getenv(secretName) == "" {
		t.Fatal("test secret unexpectedly absent")
	}
}

func TestLiveProcessRejectsUnsafeRequestValues(t *testing.T) {
	for _, request := range []LiveProcessRequest{
		{executable: "bad\nname", class: LiveExecutionProbe},
		{executable: liveProbeExecutable(t), args: []string{"bad\x00arg"}, class: LiveExecutionProbe},
		{executable: liveProbeExecutable(t), outputLimit: maxLiveProcessOutputBytes + 1, class: LiveExecutionProbe},
		{executable: filepath.Base(liveProbeExecutable(t)), class: LiveExecutionProbe},
	} {
		if err := validateLiveProcessRequest(request); err == nil {
			t.Fatalf("validateLiveProcessRequest(%+v) error = nil", request)
		}
	}
}

func TestLiveProcessProbeRejectsArbitraryAndMutatingCommands(t *testing.T) {
	for _, request := range []LiveProcessRequest{
		{executable: liveProbeExecutable(t), args: []string{"/d", "/c", "echo mutation"}, class: LiveExecutionProbe},
		{executable: liveProbeExecutable(t), args: []string{"install", "Vendor.Fixture"}, class: LiveExecutionProbe},
		{executable: liveProbeExecutable(t), args: []string{"uninstall", "Vendor.Fixture"}, class: LiveExecutionProbe},
		{executable: liveProbeExecutable(t), args: []string{"list", "--id", "Vendor.Fixture"}, class: LiveExecutionProbe},
	} {
		if err := validateLiveProcessRequest(request); err == nil {
			t.Fatalf("validateLiveProcessRequest(%+v) accepted a non-reviewed probe", request)
		}
	}
}

func TestLiveProcessWingetListProbeHasExactReviewedArguments(t *testing.T) {
	request := newLiveWingetListProbe(liveProbeExecutable(t), "Vendor.Fixture", nil, 0)
	want := []string{"list", "--id", "Vendor.Fixture", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}
	if !reflect.DeepEqual(request.args, want) {
		t.Fatalf("probe args = %#v, want %#v", request.args, want)
	}
	if err := validateLiveProcessRequest(request); err != nil {
		t.Fatalf("validateLiveProcessRequest() error = %v", err)
	}
}

func TestLiveProcessStdinClosesBeforeChildWaitsForEOF(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object runner is unavailable on this platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := newLiveEngineMutation(newTrustedLiveMutationPermit(), liveProbeExecutable(t), []string{"/d", "/c", "set /p input=& exit /b 0"}, liveTestEnvironment(t), 0)
	result, err := runLiveProcess(ctx, request)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("runLiveProcess() = (%+v, %v), want stdin EOF and zero exit", result, err)
	}
}

func TestLiveProcessCancellationClosesJobAndReturnsBoundedly(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object runner is unavailable on this platform")
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := newLiveEngineMutation(newTrustedLiveMutationPermit(), liveProbeExecutable(t), []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, liveTestEnvironment(t), 0)
	result := make(chan error, 1)
	go func() {
		_, err := runLiveProcess(ctx, request)
		result <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		var executionErr *LiveExecutionError
		if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionCanceled {
			t.Fatalf("runLiveProcess() error = %T %v, want cancellation", err, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runLiveProcess() did not return after cancellation")
	}
}

func TestLiveProcessTimeoutClosesJobAndReturnsBoundedly(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object runner is unavailable on this platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveProbeExecutable(t), []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, liveTestEnvironment(t), 0))
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionTimeout {
		t.Fatalf("runLiveProcess() error = %T %v, want timeout", err, err)
	}
}

func TestLiveProcessCancellationKillsGrandchild(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object runner is unavailable on this platform")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "escaped.txt")
	script := filepath.Join(root, "spawn.cmd")
	content := "@echo off\r\nstart \"\" /b cmd.exe /d /c \"ping -n 3 127.0.0.1 >nul && echo escaped>" + marker + "\"\r\nping -n 20 127.0.0.1 >nul\r\n"
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		t.Fatalf("write child script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveProbeExecutable(t), []string{"/d", "/c", script}, liveTestEnvironment(t), 0))
		finished <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-finished:
		var executionErr *LiveExecutionError
		if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionCanceled {
			t.Fatalf("runLiveProcess() error = %T %v, want cancellation", err, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runLiveProcess() did not return after cancellation")
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("Job Object allowed a grandchild to survive cancellation")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat grandchild marker: %v", err)
	}
}

func TestLiveWindowsReaderClosesWithDuplicatedWriter(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows pipe behavior is unavailable on this platform")
	}
	read, write, err := newLiveWindowsPipe(false)
	if err != nil {
		t.Fatalf("newLiveWindowsPipe() error = %v", err)
	}
	defer windows.CloseHandle(write)
	reader := startLiveWindowsReader(read, 16, make(chan struct{}, 1))
	if !reader.closeAndWait(2 * time.Second) {
		t.Fatal("reader did not stop after its read handle was canceled")
	}
}

func TestLiveWindowsProcessImageMismatchFailsBeforeResume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process image inspection is unavailable on this platform")
	}
	actual := filepath.Join(os.Getenv("SystemRoot"), "System32", "notepad.exe")
	if _, err := os.Stat(actual); err != nil {
		t.Skipf("alternate system executable unavailable: %v", err)
	}
	original := liveWindowsProcessImagePath
	liveWindowsProcessImagePath = func(windows.Handle) (string, error) { return actual, nil }
	defer func() { liveWindowsProcessImagePath = original }()
	if err := verifyLiveWindowsProcessImage(0, liveProbeExecutable(t)); err == nil {
		t.Fatal("verifyLiveWindowsProcessImage() accepted a swapped process image")
	}
}

func TestLiveProcessBoundsOutputWhileReading(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object runner is unavailable on this platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveProbeExecutable(t), []string{"/d", "/c", "echo output-exceeds-limit"}, liveTestEnvironment(t), 1))
	var executionErr *LiveExecutionError
	if err == nil || !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionOutputLimit {
		t.Fatalf("runLiveProcess() error = %T %v, want output limit", err, err)
	}
}

func TestLiveProcessRetainsNumericWindowsExitCode(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object runner is unavailable on this platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveProbeExecutable(t), []string{"/d", "/c", "exit /b -1978335212"}, liveTestEnvironment(t), 0))
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionProcessExit {
		t.Fatalf("runLiveProcess() error = %T %v, want process exit", err, err)
	}
	if result.ExitCode != -1978335212 {
		t.Fatalf("runLiveProcess() exit code = %d, want -1978335212", result.ExitCode)
	}
}

func TestLiveProcessRejectsReparseExecutablePath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows reparse points are unavailable on this platform")
	}
	junction := filepath.Join(t.TempDir(), "system32-link")
	target := filepath.Dir(liveProbeExecutable(t))
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink junction: %v: %s", err, output)
	}
	defer os.Remove(junction)
	_, err := runLiveProcess(context.Background(), newLiveEngineMutation(newTrustedLiveMutationPermit(), filepath.Join(junction, filepath.Base(liveProbeExecutable(t))), []string{"/d", "/c", "exit 0"}, liveTestEnvironment(t), 0))
	var executionErr *LiveExecutionError
	if err == nil || !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionInvalidRequest {
		t.Fatalf("runLiveProcess() error = %T %v, want invalid reparse path", err, err)
	}
}

func liveProbeExecutable(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		if value := os.Getenv("ComSpec"); value != "" {
			return value
		}
		t.Fatal("ComSpec is unavailable")
	}
	return "/bin/echo"
}

func liveTestEnvironment(t *testing.T) map[string]string {
	t.Helper()
	systemRoot := os.Getenv("SystemRoot")
	return map[string]string{"COMSPEC": liveProbeExecutable(t), "PATH": filepath.Join(systemRoot, "System32"), "SYSTEMROOT": systemRoot}
}
