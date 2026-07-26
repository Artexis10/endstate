// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLiveProcessRejectsZeroValueRequest(t *testing.T) {
	if err := validateLiveProcessRequest(LiveProcessRequest{}); err == nil {
		t.Fatal("validateLiveProcessRequest() accepted a zero-value request")
	}
}

func TestLiveProcessRejectsMutationWithoutTrustedPermit(t *testing.T) {
	_, err := runLiveProcess(context.Background(), newLiveMutationRequest(trustedLiveMutationPermit{}, LiveExecutionWinget, liveTestExecutable(t), []string{"install", "Vendor.Fixture"}, nil, 0))
	if err == nil {
		t.Fatal("runLiveProcess() error = nil, want mutation denial")
	}
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionMutationDenied {
		t.Fatalf("runLiveProcess() error = %T %v, want mutation denial", err, err)
	}
}

func TestLiveProcessRejectsSeparatelyConstructedMutationCapability(t *testing.T) {
	request := newLiveMutationRequest(trustedLiveMutationPermit{capability: &liveMutationCapability{}}, LiveExecutionWinget, liveTestExecutable(t), []string{"install", "Vendor.Fixture"}, nil, 0)
	err := validateLiveProcessRequest(request)
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionMutationDenied {
		t.Fatalf("validateLiveProcessRequest() error = %T %v, want mutation denial", err, err)
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
		{executable: liveTestExecutable(t), args: []string{"bad\x00arg"}, class: LiveExecutionProbe},
		{executable: liveTestExecutable(t), outputLimit: maxLiveProcessOutputBytes + 1, class: LiveExecutionProbe},
		{executable: filepath.Base(liveTestExecutable(t)), class: LiveExecutionProbe},
	} {
		if err := validateLiveProcessRequest(request); err == nil {
			t.Fatalf("validateLiveProcessRequest(%+v) error = nil", request)
		}
	}
}

func TestLiveProcessProbeRejectsArbitraryAndMutatingCommands(t *testing.T) {
	for _, request := range []LiveProcessRequest{
		{executable: liveTestExecutable(t), args: []string{"/d", "/c", "echo mutation"}, class: LiveExecutionProbe},
		{executable: liveTestExecutable(t), args: []string{"install", "Vendor.Fixture"}, class: LiveExecutionProbe},
		{executable: liveTestExecutable(t), args: []string{"uninstall", "Vendor.Fixture"}, class: LiveExecutionProbe},
		{executable: liveTestExecutable(t), args: []string{"list", "--id", "Vendor.Fixture"}, class: LiveExecutionProbe},
	} {
		if err := validateLiveProcessRequest(request); err == nil {
			t.Fatalf("validateLiveProcessRequest(%+v) accepted a non-reviewed probe", request)
		}
	}
}

func TestLiveProcessWingetListProbeHasExactReviewedArguments(t *testing.T) {
	request := newLiveWingetListProbe(liveTestExecutable(t), "Vendor.Fixture", nil, 0)
	want := []string{"list", "--id", "Vendor.Fixture", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}
	if !reflect.DeepEqual(request.args, want) {
		t.Fatalf("probe args = %#v, want %#v", request.args, want)
	}
	if err := validateLiveProcessRequest(request); err != nil {
		t.Fatalf("validateLiveProcessRequest() error = %v", err)
	}
}

func liveTestExecutable(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.TempDir(), "endstate-live-process.exe")
}
