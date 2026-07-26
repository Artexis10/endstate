// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLiveProcessRequestDefaultsToProbe(t *testing.T) {
	request := LiveProcessRequest{Name: liveProbeExecutable(t)}
	if err := validateLiveProcessRequest(request); err != nil {
		t.Fatalf("validateLiveProcessRequest() error = %v", err)
	}
	if got := request.executionClass(); got != LiveExecutionProbe {
		t.Fatalf("request execution class = %q, want %q", got, LiveExecutionProbe)
	}
}

func TestLiveProcessRejectsMutationWithoutTrustedPermit(t *testing.T) {
	_, err := runLiveProcess(context.Background(), LiveProcessRequest{Name: liveProbeExecutable(t), Class: LiveExecutionWinget})
	if err == nil {
		t.Fatal("runLiveProcess() error = nil, want mutation denial")
	}
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionMutationDenied {
		t.Fatalf("runLiveProcess() error = %T %v, want mutation denial", err, err)
	}
}

func TestLiveProcessPermitIsInMemoryAndNonWindowsFailsClosed(t *testing.T) {
	request := LiveProcessRequest{Name: liveProbeExecutable(t), Class: LiveExecutionEngine, permit: newTrustedLiveMutationPermit()}
	if runtime.GOOS != "windows" {
		_, err := runLiveProcess(context.Background(), request)
		var executionErr *LiveExecutionError
		if err == nil || !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionUnsupported {
			t.Fatalf("runLiveProcess() error = %T %v, want unsupported platform", err, err)
		}
		return
	}
	request.Args = []string{"/d", "/c", "exit 0"}
	request.Environment = map[string]string{"COMSPEC": request.Name, "SYSTEMROOT": os.Getenv("SystemRoot")}
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
		{Name: "bad\nname"},
		{Name: "winget", Args: []string{"bad\x00arg"}},
		{Name: liveProbeExecutable(t), OutputLimit: maxLiveProcessOutputBytes + 1},
		{Name: filepath.Base(liveProbeExecutable(t))},
	} {
		if err := validateLiveProcessRequest(request); err == nil {
			t.Fatalf("validateLiveProcessRequest(%+v) error = nil", request)
		}
	}
}

func TestLiveProcessBoundsOutputWhileReading(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object runner is unavailable on this platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := runLiveProcess(ctx, LiveProcessRequest{
		Name: liveProbeExecutable(t), Args: []string{"/d", "/c", "echo output-exceeds-limit"}, OutputLimit: 1,
		Environment: map[string]string{"COMSPEC": liveProbeExecutable(t), "SYSTEMROOT": os.Getenv("SystemRoot")},
	})
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
	result, err := runLiveProcess(ctx, LiveProcessRequest{
		Name: liveProbeExecutable(t), Args: []string{"/d", "/c", "exit /b -1978335212"},
		Environment: map[string]string{"COMSPEC": liveProbeExecutable(t), "SYSTEMROOT": os.Getenv("SystemRoot")},
	})
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
	_, err := runLiveProcess(context.Background(), LiveProcessRequest{Name: filepath.Join(junction, filepath.Base(liveProbeExecutable(t)))})
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
