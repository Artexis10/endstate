// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestWindowsLiveVersionSourceReadsPEFixedFileVersion(t *testing.T) {
	path := os.Getenv("ComSpec")
	if path == "" {
		t.Fatal("ComSpec is unavailable")
	}
	version, err := (windowsLiveVersionSource{}).FileVersion(path)
	if err != nil || version == "" {
		t.Fatalf("FileVersion() = %q, %v", version, err)
	}
}

func TestNewWindowsLiveObserverFailsClosedWhenTrustedResolverIsUnavailable(t *testing.T) {
	original := newWindowsLiveWingetResolver
	newWindowsLiveWingetResolver = func() (liveTrustedWingetResolver, error) { return nil, errors.New("unavailable") }
	defer func() { newWindowsLiveWingetResolver = original }()
	if _, err := NewWindowsLiveObserver(fakeLiveFiles{}); err == nil {
		t.Fatal("NewWindowsLiveObserver() accepted an unavailable trusted resolver")
	}
}

func TestClassifyLiveWingetExitCodeAcceptsOnlyReviewedNoPackageResult(t *testing.T) {
	tests := []struct {
		exit int
		want LiveProcessClassification
	}{
		{0, LiveProcessCompleted},
		{liveWingetNoInstalledExitCode, LiveProcessNoInstalled},
		{2316632084, LiveProcessNoInstalled},
		{2316632083, ""},
		{-1978335211, ""},
		{1, ""},
		{-1, ""},
	}
	for _, test := range tests {
		if got := classifyLiveWingetExitCode(test.exit); got != test.want {
			t.Fatalf("classifyLiveWingetExitCode(%d) = %q, want %q", test.exit, got, test.want)
		}
	}
}

func TestWindowsLiveProcessRejectsBareOrUntrustedWingetResolution(t *testing.T) {
	if _, err := (windowsLiveProcess{}).Run(context.Background(), "winget", "list"); err == nil {
		t.Fatal("windowsLiveProcess accepted unresolved bare winget")
	}
	if _, err := (windowsLiveProcess{}).Run(context.Background(), `C:\shadow\winget.exe`, "list"); err == nil {
		t.Fatal("windowsLiveProcess accepted a caller-provided executable path")
	}
}
