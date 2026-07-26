// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"encoding/json"
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
	result, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveWindowsProbeExecutable(t), []string{"/d", "/c", "exit 0"}, liveWindowsTestEnvironment(t), 0))
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("runLiveProcess() = (%+v, %v), want zero exit", result, err)
	}
}

func TestLiveProcessStdinClosesBeforeChildWaitsForEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveWindowsProbeExecutable(t), []string{"/d", "/c", "set /p input=& exit /b 0"}, liveWindowsTestEnvironment(t), 0))
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("runLiveProcess() = (%+v, %v), want stdin EOF and zero exit", result, err)
	}
}

func TestLiveProcessCancellationClosesJobAndReturnsBoundedly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	request := newLiveEngineMutation(newTrustedLiveMutationPermit(), liveWindowsProbeExecutable(t), []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, liveWindowsTestEnvironment(t), 0)
	finished := make(chan error, 1)
	go func() { _, err := runLiveProcess(ctx, request); finished <- err }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	assertLiveWindowsCancellation(t, finished)
}

func TestLiveProcessTimeoutClosesJobAndReturnsBoundedly(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveWindowsProbeExecutable(t), []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, liveWindowsTestEnvironment(t), 0))
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
		_, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveWindowsProbeExecutable(t), []string{"/d", "/c", script}, liveWindowsTestEnvironment(t), 0))
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
	originalPath, originalIdentity := liveWindowsProcessImagePath, liveWindowsProcessImageIdentity
	liveWindowsProcessImagePath = func(windows.Handle) (string, error) { return binding.path, nil }
	liveWindowsProcessImageIdentity = func(string) (liveWindowsFileIdentity, error) {
		return liveWindowsFileIdentity{volume: binding.identity.volume, indexHigh: binding.identity.indexHigh, indexLow: binding.identity.indexLow + 1}, nil
	}
	defer func() { liveWindowsProcessImagePath, liveWindowsProcessImageIdentity = originalPath, originalIdentity }()
	if err := verifyLiveWindowsProcessImage(0, binding); err == nil {
		t.Fatal("verifyLiveWindowsProcessImage() accepted a different file identity with the same spelling")
	}
}

func TestLiveTrustedAppXBindingRejectsUntrustedMetadata(t *testing.T) {
	if _, err := newLiveTrustedAppXBinding(liveAppXPackageMetadata{familyName: "Vendor.Package_123", fullName: "Vendor.Package_1.0.0.0_x64__123", packageRoot: `C:\temp\package`, executableName: "winget.exe"}); err == nil {
		t.Fatal("newLiveTrustedAppXBinding() accepted a non-WindowsApps package root")
	}
}

func TestLiveTrustedAppXBindingDesktopAppInstallerWhenAvailable(t *testing.T) {
	output, err := exec.Command("powershell", "-NoProfile", "-Command", "$p=Get-AppxPackage -Name Microsoft.DesktopAppInstaller | Select-Object -First 1; if ($p) { $p | Select-Object PackageFamilyName,PackageFullName,InstallLocation | ConvertTo-Json -Compress }").Output()
	if err != nil {
		t.Skipf("Desktop App Installer metadata is unavailable: %v", err)
	}
	if len(output) == 0 {
		t.Skip("Desktop App Installer is not installed")
	}
	var metadata struct {
		FamilyName  string `json:"PackageFamilyName"`
		FullName    string `json:"PackageFullName"`
		PackageRoot string `json:"InstallLocation"`
	}
	if err := json.Unmarshal(output, &metadata); err != nil {
		t.Fatalf("decode Desktop App Installer metadata: %v", err)
	}
	binding, err := newLiveTrustedAppXBinding(liveAppXPackageMetadata{familyName: metadata.FamilyName, fullName: metadata.FullName, packageRoot: metadata.PackageRoot, executableName: "winget.exe"})
	if err != nil {
		t.Fatalf("newLiveTrustedAppXBinding() error = %v", err)
	}
	bound, err := bindLiveTrustedAppXExecutable(binding)
	if err != nil {
		t.Fatalf("bindLiveTrustedAppXExecutable() error = %v", err)
	}
	bound.Close()
}

func TestLiveTrustedAppXBindingVerifiesImageWithoutGenericTraversal(t *testing.T) {
	output, err := exec.Command("powershell", "-NoProfile", "-Command", "$p=Get-AppxPackage -Name Microsoft.DesktopAppInstaller | Select-Object -First 1; if ($p) { $p | Select-Object PackageFamilyName,PackageFullName,InstallLocation | ConvertTo-Json -Compress }").Output()
	if err != nil {
		t.Skipf("Desktop App Installer metadata is unavailable: %v", err)
	}
	if len(output) == 0 {
		t.Skip("Desktop App Installer is not installed")
	}
	var metadata struct {
		FamilyName  string `json:"PackageFamilyName"`
		FullName    string `json:"PackageFullName"`
		PackageRoot string `json:"InstallLocation"`
	}
	if err := json.Unmarshal(output, &metadata); err != nil {
		t.Fatalf("decode Desktop App Installer metadata: %v", err)
	}
	trusted, err := newLiveTrustedAppXBinding(liveAppXPackageMetadata{familyName: metadata.FamilyName, fullName: metadata.FullName, packageRoot: metadata.PackageRoot, executableName: "winget.exe"})
	if err != nil {
		t.Fatalf("newLiveTrustedAppXBinding() error = %v", err)
	}
	binding, err := bindLiveTrustedAppXExecutable(trusted)
	if err != nil {
		t.Fatalf("bindLiveTrustedAppXExecutable() error = %v", err)
	}
	defer binding.Close()
	originalPath, originalIdentity := liveWindowsProcessImagePath, liveWindowsProcessImageIdentity
	liveWindowsProcessImagePath = func(windows.Handle) (string, error) { return binding.path, nil }
	calledGenericIdentity := false
	liveWindowsProcessImageIdentity = func(string) (liveWindowsFileIdentity, error) {
		calledGenericIdentity = true
		return liveWindowsFileIdentity{}, errors.New("generic binding must not traverse WindowsApps")
	}
	defer func() { liveWindowsProcessImagePath, liveWindowsProcessImageIdentity = originalPath, originalIdentity }()
	if err := verifyLiveWindowsProcessImage(0, binding); err != nil {
		t.Fatalf("verifyLiveWindowsProcessImage() error = %v", err)
	}
	if calledGenericIdentity {
		t.Fatal("verifyLiveWindowsProcessImage() re-entered the generic executable binder")
	}
}

func TestLiveProcessBoundsOutputWhileReading(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveWindowsProbeExecutable(t), []string{"/d", "/c", "echo output-exceeds-limit"}, liveWindowsTestEnvironment(t), 1))
	var executionErr *LiveExecutionError
	if err == nil || !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionOutputLimit {
		t.Fatalf("runLiveProcess() error = %T %v, want output limit", err, err)
	}
}

func TestLiveProcessRetainsNumericWindowsExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runLiveProcess(ctx, newLiveEngineMutation(newTrustedLiveMutationPermit(), liveWindowsProbeExecutable(t), []string{"/d", "/c", "exit /b -1978335212"}, liveWindowsTestEnvironment(t), 0))
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionProcessExit {
		t.Fatalf("runLiveProcess() error = %T %v, want process exit", err, err)
	}
	if result.ExitCode != -1978335212 {
		t.Fatalf("runLiveProcess() exit code = %d, want -1978335212", result.ExitCode)
	}
}

func TestLiveProcessRejectsReparseExecutablePath(t *testing.T) {
	junction := filepath.Join(t.TempDir(), "system32-link")
	target := filepath.Dir(liveWindowsProbeExecutable(t))
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink junction: %v: %s", err, output)
	}
	defer os.Remove(junction)
	_, err := runLiveProcess(context.Background(), newLiveEngineMutation(newTrustedLiveMutationPermit(), filepath.Join(junction, filepath.Base(liveWindowsProbeExecutable(t))), []string{"/d", "/c", "exit 0"}, liveWindowsTestEnvironment(t), 0))
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
