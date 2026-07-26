// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestLiveProcessPermitRunsWindowsCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", "exit 0"}, 0))
	if err != nil || result.exitCode != 0 {
		t.Fatalf("runLiveProcess() = (%+v, %v), want zero exit", result, err)
	}
}

func TestLiveProcessStdinClosesBeforeChildWaitsForEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", "set /p input=& exit /b 0"}, 0))
	if err != nil || result.exitCode != 0 {
		t.Fatalf("runLiveProcess() = (%+v, %v), want stdin EOF and zero exit", result, err)
	}
}

func TestLiveProcessCancellationClosesJobAndReturnsBoundedly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	request := liveWindowsEngineRequest(t, []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, 0)
	finished := make(chan error, 1)
	go func() { _, err := runLiveProcess(ctx, request); finished <- err }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	assertLiveWindowsCancellation(t, finished)
}

func TestLiveProcessCancellationSealsPartialOutputReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready.txt")
	finished := make(chan struct {
		receipt *liveExecutionReceipt
		err     error
	}, 1)
	go func() {
		command := "echo ready>" + ready + " & echo partial-output & ping -n 20 127.0.0.1 >nul"
		receipt, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", command}, 0))
		finished <- struct {
			receipt *liveExecutionReceipt
			err     error
		}{receipt, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not signal readiness")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	result := <-finished
	var execution *LiveExecutionError
	if !errors.As(result.err, &execution) || execution.Code != LiveExecutionCanceled || result.receipt == nil {
		t.Fatalf("runLiveProcess() = (%+v, %v), want canceled sealed receipt", result.receipt, result.err)
	}
	if result.receipt.failure != LiveExecutionCanceled || !bytes.Contains(result.receipt.stdout, []byte("partial-output")) || result.receipt.stdoutSHA256 != sha256.Sum256(result.receipt.stdout) {
		t.Fatalf("receipt = %+v, want sealed partial canceled output", result.receipt)
	}
	if err := result.receipt.validate(); err != nil {
		t.Fatalf("receipt.validate() error = %v", err)
	}
}

func TestLiveProcessReceiptBindsHeldImageAndProcessIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	receipt, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", "echo receipt-output"}, 0))
	if err != nil {
		t.Fatalf("runLiveProcess() error = %v", err)
	}
	if receipt.pid == 0 || receipt.created.IsZero() || receipt.image.canonical == "" || receipt.image.volume == 0 || receipt.image.indexHigh == 0 && receipt.image.indexLow == 0 || receipt.image.sha256 == ([32]byte{}) || receipt.stdoutSHA256 != sha256.Sum256(receipt.stdout) {
		t.Fatalf("receipt lacks launch identity: %+v", receipt)
	}
}

func TestLiveProcessTimeoutClosesJobAndReturnsBoundedly(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, 0))
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionTimeout {
		t.Fatalf("runLiveProcess() error = %T %v, want timeout", err, err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("runLiveProcess() did not return boundedly after timeout")
	}
}

func TestLiveProcessCancellationKillsGrandchild(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "escaped.txt")
	ready := filepath.Join(root, "ready.txt")
	script := filepath.Join(root, "spawn.cmd")
	content := "@echo off\r\nstart \"\" /b cmd.exe /d /c \"echo ready>" + ready + " & ping -n 3 127.0.0.1 >nul && echo escaped>" + marker + "\"\r\nping -n 20 127.0.0.1 >nul\r\n"
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		t.Fatalf("write child script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", script}, 0))
		finished <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("grandchild did not signal readiness")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	assertLiveWindowsCancellation(t, finished)
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("Job Object allowed a grandchild to survive cancellation")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat grandchild marker: %v", err)
	}
}

func TestLiveWindowsReaderClosesWithDuplicatedWriter(t *testing.T) {
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

func TestLiveWindowsProcessImageIdentityMismatchFailsWithSameSpelling(t *testing.T) {
	binding, err := bindLiveWindowsExecutable(liveWindowsProbeExecutable(t))
	if err != nil {
		t.Fatalf("bindLiveWindowsExecutable() error = %v", err)
	}
	defer binding.Close()
	originalPath := liveWindowsProcessImagePath
	liveWindowsProcessImagePath = func(windows.Handle) (string, error) { return binding.path, nil }
	defer func() { liveWindowsProcessImagePath = originalPath }()
	if err := verifyLiveWindowsProcessImage(0, binding); err != nil {
		t.Fatalf("verifyLiveWindowsProcessImage() error = %v", err)
	}
}

func TestLiveTrustedAppXBindingRejectsUntrustedMetadata(t *testing.T) {
	if _, err := newLiveTrustedAppXBinding(liveAppXPackageMetadata{familyName: "Vendor.Package_123", fullName: "Vendor.Package_1.0.0.0_x64__123", packageRoot: `C:\temp\package`, executableName: "winget.exe"}); err == nil {
		t.Fatal("newLiveTrustedAppXBinding() accepted a non-WindowsApps package root")
	}
}

func TestLiveTrustedAppXBindingDesktopAppInstallerWhenAvailable(t *testing.T) {
	binding := liveResolvedDesktopAppInstaller(t)
	bound, err := bindLiveTrustedAppXExecutable(binding)
	if err != nil {
		t.Fatalf("bindLiveTrustedAppXExecutable() error = %v", err)
	}
	bound.Close()
}

func TestLiveTrustedAppXBindingVerifiesImageWithoutGenericTraversal(t *testing.T) {
	trusted := liveResolvedDesktopAppInstaller(t)
	binding, err := bindLiveTrustedAppXExecutable(trusted)
	if err != nil {
		t.Fatalf("bindLiveTrustedAppXExecutable() error = %v", err)
	}
	defer binding.Close()
	originalPath := liveWindowsProcessImagePath
	liveWindowsProcessImagePath = func(windows.Handle) (string, error) { return binding.path, nil }
	defer func() { liveWindowsProcessImagePath = originalPath }()
	if err := verifyLiveWindowsProcessImage(0, binding); err != nil {
		t.Fatalf("verifyLiveWindowsProcessImage() error = %v", err)
	}
}

func liveResolvedDesktopAppInstaller(t *testing.T) liveTrustedAppXBinding {
	t.Helper()
	appmodel, err := newLiveWindowsAppModel()
	if err != nil {
		t.Fatalf("Desktop App Installer AppModel API unavailable: %v", err)
	}
	packages, err := appmodel.Packages(context.Background())
	if err != nil {
		t.Fatalf("Desktop App Installer AppModel query failed: %v", err)
	}
	if len(packages) == 0 {
		t.Skip("Desktop App Installer is not installed")
	}
	resolver, err := newLiveWingetResolver()
	if err != nil {
		t.Fatalf("newLiveWingetResolver() error = %v", err)
	}
	target, err := resolver.ResolveLiveWinget(context.Background())
	if err != nil {
		t.Fatalf("ResolveLiveWinget() error = %v", err)
	}
	return target.binding
}

func TestLiveProcessBoundsOutputWhileReading(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", "echo output-exceeds-limit"}, 1))
	var executionErr *LiveExecutionError
	if err == nil || !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionOutputLimit {
		t.Fatalf("runLiveProcess() error = %T %v, want output limit", err, err)
	}
}

func TestLiveProcessRetainsNumericWindowsExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runLiveProcess(ctx, liveWindowsEngineRequest(t, []string{"/d", "/c", "exit /b -1978335212"}, 0))
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionProcessExit {
		t.Fatalf("runLiveProcess() error = %T %v, want process exit", err, err)
	}
	if result.exitCode != -1978335212 {
		t.Fatalf("runLiveProcess() exit code = %d, want -1978335212", result.exitCode)
	}
}

func TestLiveProcessRejectsReparseExecutablePath(t *testing.T) {
	junction := filepath.Join(t.TempDir(), "system32-link")
	target := filepath.Dir(liveWindowsProbeExecutable(t))
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink junction: %v: %s", err, output)
	}
	defer os.Remove(junction)
	request := liveWindowsEngineRequest(t, []string{"/d", "/c", "exit 0"}, 0)
	request.executable = filepath.Join(junction, filepath.Base(liveWindowsProbeExecutable(t)))
	_, err := runLiveProcess(context.Background(), request)
	var executionErr *LiveExecutionError
	if err == nil || !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionInvalidRequest {
		t.Fatalf("runLiveProcess() error = %T %v, want invalid reparse path", err, err)
	}
}

func assertLiveWindowsCancellation(t *testing.T, finished <-chan error) {
	t.Helper()
	select {
	case err := <-finished:
		var executionErr *LiveExecutionError
		if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionCanceled {
			t.Fatalf("runLiveProcess() error = %T %v, want cancellation", err, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runLiveProcess() did not return after cancellation")
	}
}

func liveWindowsProbeExecutable(t *testing.T) string {
	t.Helper()
	if value := os.Getenv("ComSpec"); value != "" {
		return value
	}
	t.Fatal("ComSpec is unavailable")
	return ""
}

func liveWindowsTestEnvironment(t *testing.T) map[string]string {
	t.Helper()
	systemRoot := os.Getenv("SystemRoot")
	return map[string]string{"COMSPEC": liveWindowsProbeExecutable(t), "PATH": filepath.Join(systemRoot, "System32"), "SYSTEMROOT": systemRoot}
}

func liveWindowsEngineRequest(t *testing.T, args []string, outputLimit int) LiveProcessRequest {
	t.Helper()
	executable := liveWindowsProbeExecutable(t)
	expected := liveTestExpectedIdentity()
	var err error
	expected.engine, err = liveWindowsFileSHA256(executable)
	if err != nil {
		t.Fatalf("liveWindowsFileSHA256() error = %v", err)
	}
	return newLiveEngineApply(liveTestAdmission(t, liveOperationEngineApply), newTrustedLiveMutationPermit(), executable, args, "", liveWindowsTestEnvironment(t), expected, outputLimit)
}
