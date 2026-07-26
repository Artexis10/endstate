// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"crypto/sha256"
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
	_, err := runLiveProcess(context.Background(), newLiveTypedMutation(liveTestAdmission(t, liveOperationWingetExactInstall), trustedLiveMutationPermit{}, liveOperationWingetExactInstall, liveTestExecutable(t), []string{"install", "Vendor.Fixture"}, "", nil, liveReceiptExpectedIdentity{}, 0))
	if err == nil {
		t.Fatal("runLiveProcess() error = nil, want mutation denial")
	}
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionMutationDenied {
		t.Fatalf("runLiveProcess() error = %T %v, want mutation denial", err, err)
	}
}

func TestLiveProcessRejectsSeparatelyConstructedMutationCapability(t *testing.T) {
	request := newLiveTypedMutation(liveTestAdmission(t, liveOperationWingetExactInstall), trustedLiveMutationPermit{capability: &liveMutationCapability{}}, liveOperationWingetExactInstall, liveTestExecutable(t), []string{"install", "Vendor.Fixture"}, "", nil, liveReceiptExpectedIdentity{}, 0)
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
		{executable: "bad\nname", operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
		{executable: liveTestExecutable(t), args: []string{"bad\x00arg"}, operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
		{executable: liveTestExecutable(t), outputLimit: maxLiveProcessOutputBytes + 1, operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
		{executable: filepath.Base(liveTestExecutable(t)), operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
	} {
		if err := validateLiveProcessRequest(request); err == nil {
			t.Fatalf("validateLiveProcessRequest(%+v) error = nil", request)
		}
	}
}

func TestLiveProcessProbeRejectsArbitraryAndMutatingCommands(t *testing.T) {
	for _, request := range []LiveProcessRequest{
		{executable: liveTestExecutable(t), args: []string{"/d", "/c", "echo mutation"}, operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
		{executable: liveTestExecutable(t), args: []string{"install", "Vendor.Fixture"}, operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
		{executable: liveTestExecutable(t), args: []string{"uninstall", "Vendor.Fixture"}, operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
		{executable: liveTestExecutable(t), args: []string{"list", "--id", "Vendor.Fixture"}, operation: liveOperationWingetExactList, admission: liveTestAdmission(t, liveOperationWingetExactList)},
	} {
		if err := validateLiveProcessRequest(request); err == nil {
			t.Fatalf("validateLiveProcessRequest(%+v) accepted a non-reviewed probe", request)
		}
	}
}

func TestLiveProcessWingetListProbeHasExactReviewedArguments(t *testing.T) {
	request := newLiveWingetListProbe(liveTestAdmission(t, liveOperationWingetExactList), liveTestExecutable(t), "Vendor.Fixture", nil, 0)
	request.expected.packageRef = sha256.Sum256([]byte("package"))
	want := []string{"list", "--id", "Vendor.Fixture", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}
	if !reflect.DeepEqual(request.args, want) {
		t.Fatalf("probe args = %#v, want %#v", request.args, want)
	}
	if err := validateLiveProcessRequest(request); err != nil {
		t.Fatalf("validateLiveProcessRequest() error = %v", err)
	}
}

func TestLiveProcessRejectsMissingProofIdentity(t *testing.T) {
	request := newLiveTypedMutation(liveTestAdmission(t, liveOperationEngineApply), newTrustedLiveMutationPermit(), liveOperationEngineApply, liveTestExecutable(t), []string{"apply"}, "", nil, liveReceiptExpectedIdentity{}, 0)
	if err := validateLiveProcessRequest(request); err == nil {
		t.Fatal("validateLiveProcessRequest() accepted missing proof identities")
	}
}

func liveTestExpectedIdentity() liveReceiptExpectedIdentity {
	return liveReceiptExpectedIdentity{
		definition: sha256.Sum256([]byte("definition")),
		engine:     sha256.Sum256([]byte("engine")),
		seed:       sha256.Sum256([]byte("seed")),
		packageRef: sha256.Sum256([]byte("package")),
	}
}

func liveTestAdmission(t *testing.T, operation liveOperation) liveReceiptAdmission {
	t.Helper()
	issuer := newLiveReceiptIssuer()
	nonce := liveReceiptTestNonce(byte(t.Name()[0]))
	admission, err := issuer.admit(operation, 1, nonce)
	if err != nil {
		t.Fatalf("issuer.admit() error = %v", err)
	}
	return admission
}

func liveTestExecutable(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.TempDir(), "endstate-live-process.exe")
}
