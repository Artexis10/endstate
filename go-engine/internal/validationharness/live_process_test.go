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
	"time"
)

func newTrustedLiveMutationPermit(admission liveReceiptAdmission, expected liveReceiptExpectedIdentity, executable string, arguments []string, directory string, environment map[string]string) trustedLiveMutationPermit {
	return trustedLiveMutationPermit{capability: &liveMutationCapability{
		serial: 1, campaign: sha256.Sum256([]byte("campaign")), operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce,
		issuedAt: time.Now().UTC().Add(-time.Minute), expiresAt: time.Now().UTC().Add(time.Minute), definition: expected.definition, engine: expected.engine, seed: expected.seed, packageRef: expected.packageRef,
		comparator: sha256.Sum256([]byte("comparator")), targets: sha256.Sum256([]byte("targets")), observer: sha256.Sum256([]byte("observer")), workflow: sha256.Sum256([]byte("workflow")), packageArguments: append([]string(nil), arguments...), executable: executable, executableSHA256: expected.engine, arguments: append([]string(nil), arguments...), directory: directory, environment: cloneLiveEnvironment(environment),
	}}
}

func TestLiveProcessRejectsZeroValueRequest(t *testing.T) {
	if err := validateLiveProcessRequest(LiveProcessRequest{}); err == nil {
		t.Fatal("validateLiveProcessRequest() accepted a zero-value request")
	}
}

func TestLiveProcessRejectsMutationWithoutTrustedPermit(t *testing.T) {
	_, err := runLiveProcess(context.Background(), newLiveTypedMutation(liveTestAdmission(t, liveOperationWingetExactInstall), trustedLiveMutationPermit{}, liveOperationWingetExactInstall, liveTestExecutable(t), []string{"install", "Vendor.Fixture"}, "", nil, liveTestExpectedIdentity(), 0))
	if err == nil {
		t.Fatal("runLiveProcess() error = nil, want mutation denial")
	}
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionMutationDenied {
		t.Fatalf("runLiveProcess() error = %T %v, want mutation denial", err, err)
	}
}

func TestLiveProcessRejectsMutationWithoutProofIdentity(t *testing.T) {
	request := newLiveTypedMutation(liveTestAdmission(t, liveOperationWingetExactInstall), trustedLiveMutationPermit{capability: &liveMutationCapability{}}, liveOperationWingetExactInstall, liveTestExecutable(t), []string{"install", "Vendor.Fixture"}, "", nil, liveReceiptExpectedIdentity{}, 0)
	err := validateLiveProcessRequest(request)
	var executionErr *LiveExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != LiveExecutionInvalidRequest {
		t.Fatalf("validateLiveProcessRequest() error = %T %v, want invalid request", err, err)
	}
}

func TestLiveProcessRejectsMalformedBoundInstallWithoutPanicking(t *testing.T) {
	admission := liveTestAdmission(t, liveOperationWingetExactInstall)
	expected := liveTestExpectedIdentity()
	request := newLiveTypedMutation(admission, newTrustedLiveMutationPermit(admission, expected, liveTestExecutable(t), nil, "", nil), liveOperationWingetExactInstall, liveTestExecutable(t), nil, "", nil, expected, 0)
	if err := validateLiveProcessRequest(request); err == nil {
		t.Fatal("validateLiveProcessRequest() accepted malformed install request")
	}
}

func TestLiveProcessRejectsHostilePermitInvocationSubstitution(t *testing.T) {
	expected := liveTestExpectedIdentity()
	admission := liveTestAdmission(t, liveOperationEngineApply)
	executable := liveTestExecutable(t)
	permit := newTrustedLiveMutationPermit(admission, expected, executable, []string{"apply"}, `C:\reviewed`, map[string]string{"PATH": `C:\Windows\System32`})
	request := newLiveTypedMutation(admission, permit, liveOperationEngineApply, executable, []string{"apply"}, `C:\reviewed`, map[string]string{"PATH": `C:\Windows\System32`}, expected, 0)
	if err := validateLiveProcessRequest(request); err != nil {
		t.Fatalf("bound request rejected: %v", err)
	}
	for _, mutate := range []func(*LiveProcessRequest){
		func(value *LiveProcessRequest) { value.executable = filepath.Join(os.TempDir(), "substituted.exe") },
		func(value *LiveProcessRequest) { value.args = []string{"apply", "--foreign"} },
		func(value *LiveProcessRequest) { value.dir = `C:\foreign` },
		func(value *LiveProcessRequest) { value.environment = map[string]string{"PATH": `C:\foreign`} },
		func(value *LiveProcessRequest) { value.expected.comparator = sha256.Sum256([]byte("foreign")) },
	} {
		candidate := request
		candidate.args = append([]string(nil), request.args...)
		candidate.environment = cloneLiveEnvironment(request.environment)
		mutate(&candidate)
		if err := validateLiveProcessRequest(candidate); err == nil {
			t.Fatal("validateLiveProcessRequest accepted a substituted invocation")
		}
	}
}

func TestLiveMutationPermitFinalGateBindsImageAndExpiry(t *testing.T) {
	expected := liveTestExpectedIdentity()
	admission := liveTestAdmission(t, liveOperationEngineApply)
	permit := newTrustedLiveMutationPermit(admission, expected, liveTestExecutable(t), []string{"apply"}, "", nil)
	request := newLiveTypedMutation(admission, permit, liveOperationEngineApply, liveTestExecutable(t), []string{"apply"}, "", nil, expected, 0)
	if permit.capability.finalize(request, sha256.Sum256([]byte("substituted")), time.Now().UTC()) {
		t.Fatal("final gate accepted substituted image")
	}
	if permit.capability.consumed.Load() {
		t.Fatal("substituted image consumed permit")
	}
	permit = newTrustedLiveMutationPermit(admission, expected, liveTestExecutable(t), []string{"apply"}, "", nil)
	permit.capability.expiresAt = time.Now().UTC().Add(-time.Second)
	if permit.capability.finalize(request, expected.engine, time.Now().UTC()) {
		t.Fatal("final gate accepted expired permit")
	}
	permit = newTrustedLiveMutationPermit(admission, expected, liveTestExecutable(t), []string{"apply"}, "", nil)
	if !permit.capability.finalize(request, expected.engine, time.Now().UTC()) || permit.capability.finalize(request, expected.engine, time.Now().UTC()) {
		t.Fatal("final gate did not consume exactly once")
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
	admission := liveTestAdmission(t, liveOperationEngineApply)
	request := newLiveTypedMutation(admission, newTrustedLiveMutationPermit(admission, liveReceiptExpectedIdentity{}, liveTestExecutable(t), []string{"apply"}, "", nil), liveOperationEngineApply, liveTestExecutable(t), []string{"apply"}, "", nil, liveReceiptExpectedIdentity{}, 0)
	if err := validateLiveProcessRequest(request); err == nil {
		t.Fatal("validateLiveProcessRequest() accepted missing proof identities")
	}
}

func TestLiveProcessInvalidRequestReleasesAdmission(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationWingetExactList, 1, liveReceiptTestNonce(11))
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	_, _ = runLiveProcess(context.Background(), LiveProcessRequest{operation: liveOperationWingetExactList, admission: admission})
	if _, err := issuer.admit(liveOperationWingetExactList, 2, liveReceiptTestNonce(12)); err != nil {
		t.Fatalf("invalid request retained admission: %v", err)
	}
}

func liveTestExpectedIdentity() liveReceiptExpectedIdentity {
	return liveReceiptExpectedIdentity{
		definition: sha256.Sum256([]byte("definition")),
		engine:     sha256.Sum256([]byte("engine")),
		seed:       sha256.Sum256([]byte("seed")),
		packageRef: sha256.Sum256([]byte("package")),
		comparator: sha256.Sum256([]byte("comparator")),
		targets:    sha256.Sum256([]byte("targets")),
		observer:   sha256.Sum256([]byte("observer")),
		workflow:   sha256.Sum256([]byte("workflow")),
		runner:     sha256.Sum256([]byte("runner")),
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
