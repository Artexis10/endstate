// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveWindowsLiveDeclaredTargetsAndWipeExactLeaves(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	config := filepath.Join(appData, "Notepad++", "config.xml")
	directory := filepath.Join(appData, "Notepad++", "userDefineLangs")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "language.xml"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := resolveWindowsLiveDeclaredTargets(definition, appData)
	if err != nil || len(targets) != 6 {
		t.Fatalf("resolveWindowsLiveDeclaredTargets() = %+v, %v", targets, err)
	}
	if err := wipeWindowsLiveDeclaredTargets(definition, appData); err != nil {
		t.Fatalf("wipeWindowsLiveDeclaredTargets() error = %v", err)
	}
	for _, path := range []string{config, directory} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("target %q remains after wipe: %v", path, err)
		}
	}
}

func TestResolveWindowsLiveDeclaredTargetsRejectsReparseParent(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData, target := t.TempDir(), t.TempDir()
	junction := filepath.Join(appData, "Notepad++")
	if output, err := exec.Command("cmd", "/d", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink junction: %v: %s", err, output)
	}
	if _, err := resolveWindowsLiveDeclaredTargets(definition, appData); err == nil {
		t.Fatal("resolveWindowsLiveDeclaredTargets() accepted a reparse-backed target")
	}
}

func TestWindowsLiveAttemptRootCleanupIsBoundedAndPropagatesFailure(t *testing.T) {
	parent := t.TempDir()
	withWindowsLiveTestRunnerTemp(t, parent)
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attempt.path, "receipt.json"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Lstat(attempt.path); !os.IsNotExist(err) {
		t.Fatalf("attempt root remains after cleanup: %v", err)
	}
	if err := (windowsLiveAttemptRoot{parent: parent, path: filepath.Join(parent, "foreign")}).Cleanup(); err == nil {
		t.Fatal("Cleanup() accepted a foreign attempt root")
	}
	if _, err := newWindowsLiveAttemptRoot(t.TempDir()); err == nil {
		t.Fatal("newWindowsLiveAttemptRoot() accepted a caller-selected parent")
	}
}

func TestWindowsLiveAttemptRootRejectsRecreatedOwnedPath(t *testing.T) {
	parent := t.TempDir()
	withWindowsLiveTestRunnerTemp(t, parent)
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(attempt.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(attempt.path, 0o700); err != nil {
		t.Fatal(err)
	}
	if attempt.valid() || attempt.Cleanup() == nil {
		t.Fatal("attempt root accepted a recreated owned path")
	}
}

func TestWindowsLiveAttemptRootBindingDoesNotReopenOwnedRoot(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData, parent := t.TempDir(), t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	withWindowsLiveTestRunnerTemp(t, parent)
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	original := windowsLiveAttemptRootIdentityForPath
	var opens int
	windowsLiveAttemptRootIdentityForPath = func(path string, directory bool) (windowsLiveObjectIdentity, error) {
		opens++
		return original(path, directory)
	}
	t.Cleanup(func() { windowsLiveAttemptRootIdentityForPath = original })
	if _, err := windowsLiveHostMutationBinding(definition, appData, attempt); err != nil {
		t.Fatal(err)
	}
	if opens != 0 {
		t.Fatalf("attempt-root binding reopened the root %d times before held cleanup", opens)
	}
}

func TestWindowsLiveAttemptRootCleanupRejectsSwapAfterOwnershipReservationAndRetriesSafely(t *testing.T) {
	parent, outside := t.TempDir(), t.TempDir()
	withWindowsLiveTestRunnerTemp(t, parent)
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := attempt.path + ".saved"
	original := windowsLiveCleanupBeforeAttemptRootOpen
	windowsLiveCleanupBeforeAttemptRootOpen = func(path string) {
		if path != attempt.path || os.Rename(path, backup) != nil {
			return
		}
		_, _ = exec.Command("cmd", "/d", "/c", "mklink", "/J", path, outside).CombinedOutput()
	}
	t.Cleanup(func() { windowsLiveCleanupBeforeAttemptRootOpen = original })
	if err := attempt.Cleanup(); err == nil {
		t.Fatal("Cleanup() accepted a swapped attempt root")
	}
	if _, err := os.Lstat(sentinel); err != nil {
		t.Fatalf("swapped cleanup deleted outside sentinel: %v", err)
	}
	windowsLiveCleanupBeforeAttemptRootOpen = original
	if err := os.Remove(attempt.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, attempt.path); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Cleanup(); err != nil {
		t.Fatalf("Cleanup() did not restore ownership after a failed attempt: %v", err)
	}
}

func TestWindowsLiveDeclaredTargetWipeRequiresBoundHostPermitAndSealsReceipt(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	path := filepath.Join(appData, "Notepad++", "config.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	session.definition.operations[2] = LiveCampaignOperation{Sequence: 2, Operation: string(liveOperationDeclaredTargetWipe)}
	issuer := session.NewReceiptIssuer()
	admission, err := issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, trustedLiveHostMutationPermit{}, definition, appData); err == nil {
		t.Fatal("wipe accepted a missing host permit")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("missing permit mutated target: %v", err)
	}
	session = liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	issuer = session.NewReceiptIssuer()
	admission, err = issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, appData)
	if err != nil || receipt == nil || !receipt.sealed || !receipt.succeeded {
		t.Fatalf("bound wipe = %+v, %v", receipt, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("bound wipe did not remove target: %v", err)
	}
}

func TestWindowsLiveDeclaredTargetWipeRejectsWrongRootAndReplayWithoutMutation(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData, foreignAppData := t.TempDir(), t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	path := filepath.Join(appData, "Notepad++", "config.xml")
	foreignPath := filepath.Join(foreignAppData, "Notepad++", "config.xml")
	for _, candidate := range []string{path, foreignPath} {
		if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(candidate, []byte("seed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session := liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	issuer := session.NewReceiptIssuer()
	admission, err := issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, foreignAppData); err == nil {
		t.Fatal("wipe accepted a permit bound to a different APPDATA root")
	}
	if _, err := os.Lstat(foreignPath); err != nil {
		t.Fatalf("wrong-root permit mutated target: %v", err)
	}
	if _, err := issuer.admit(liveOperationDeclaredTargetWipe, 2, session.NonceFor(liveOperationDeclaredTargetWipe, 2)); err == nil {
		t.Fatal("wrong-root permit advanced receipt sequence")
	}

	session = liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	issuer = session.NewReceiptIssuer()
	admission, err = issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding, err = windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err = session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, appData); err != nil {
		t.Fatalf("initial wipe failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("recreated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, appData); err == nil {
		t.Fatal("wipe accepted a replayed permit")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replayed permit mutated target: %v", err)
	}
	if _, err := issuer.admit(liveOperationDeclaredTargetWipe, 3, liveReceiptTestNonce(72)); err == nil {
		t.Fatal("replayed permit advanced receipt sequence")
	}
}

func TestWindowsLiveAuthorityCleanupTransitionAbandonsFailedAdmissionAndRunsOnlyFinalSuffix(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData, parent := t.TempDir(), t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	withWindowsLiveTestRunnerTemp(t, parent)
	path := filepath.Join(appData, "Notepad++", "config.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attempt.path, "failed-attempt.txt"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := liveWindowsCleanupAuthoritySession(t, definition)
	issuer := session.NewReceiptIssuer()
	if err := issuer.skipDeclaredPreflight(); err != nil {
		t.Fatal(err)
	}
	sealNormalReceipt := func(admission liveReceiptAdmission) *liveExecutionReceipt {
		t.Helper()
		permit, err := session.MintMutationPermit(admission)
		if err != nil {
			t.Fatal(err)
		}
		expected := liveReceiptExpectedIdentity{definition: permit.capability.definition, engine: permit.capability.engine, seed: permit.capability.seed, packageRef: permit.capability.packageRef, comparator: permit.capability.comparator, targets: permit.capability.targets, observer: permit.capability.observer, workflow: permit.capability.workflow, runner: permit.capability.executableSHA256}
		request := newLiveTypedMutation(admission, permit, admission.operation, permit.capability.executable, permit.capability.arguments, permit.capability.directory, permit.capability.environment, expected, 1)
		if !permit.capability.finalize(request, expected.engine, time.Now().UTC()) {
			t.Fatal("normal fixture could not commit its authority permit")
		}
		receipt := liveUnsealedReceiptForTest(t, admission, nil, nil, "")
		receipt.executable, receipt.args, receipt.directory, receipt.environment, receipt.expected = request.executable, append([]string(nil), request.args...), request.dir, cloneLiveEnvironment(request.environment), expected
		receipt.image.sha256 = expected.engine
		receipt.requestSHA256 = receipt.requestDigest()
		receipt.resultSHA256 = receipt.resultDigest()
		if err := issuer.sealFn(receipt); err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	normalApply, err := issuer.admit(liveOperationEngineApply, 3, session.NonceFor(liveOperationEngineApply, 3))
	if err != nil {
		t.Fatal(err)
	}
	normalApplyReceipt := sealNormalReceipt(normalApply)
	normalApply.complete()
	normalCapture, err := issuer.admit(liveOperationEngineVerify, 4, session.NonceFor(liveOperationEngineVerify, 4))
	if err != nil {
		t.Fatal(err)
	}
	normalCaptureReceipt := sealNormalReceipt(normalCapture)
	normalCapture.complete()
	failed, err := issuer.admit(liveOperationHashBoundSeed, 5, session.NonceFor(liveOperationHashBoundSeed, 5))
	if err != nil {
		t.Fatal(err)
	}
	if failed.issuer == nil {
		t.Fatal("failed prelaunch admission was not reserved")
	}
	if err := session.EnterCleanup(newLiveReceiptIssuer()); err == nil {
		t.Fatal("cleanup transition accepted a foreign issuer")
	}
	if err := liveWindowsCleanupAuthoritySession(t, definition).EnterCleanup(issuer); err == nil {
		t.Fatal("cleanup transition accepted an issuer from another session")
	}
	malformed := liveWindowsCleanupAuthoritySession(t, definition)
	malformed.definition.operations[15] = LiveCampaignOperation{Sequence: 15, Operation: string(liveOperationEngineApply)}
	malformedIssuer := malformed.NewReceiptIssuer()
	if err := malformed.EnterCleanup(malformedIssuer); err == nil {
		t.Fatal("cleanup transition accepted a malformed final suffix")
	}
	if err := session.EnterCleanup(issuer); err != nil {
		t.Fatalf("EnterCleanup() after failed prelaunch admission error = %v", err)
	}
	if _, _, err := liveReceiptDecoderHandoff(normalApplyReceipt, liveOperationEngineApply, 3, normalApply.nonce); err == nil {
		t.Fatal("cleanup transition left an earlier normal receipt publicly decodable")
	}
	if issuer.consumeBatchFn([]liveReceiptExpectation{{receipt: normalCaptureReceipt, operation: liveOperationEngineVerify, sequence: 4, nonce: normalCapture.nonce}}) {
		t.Fatal("cleanup transition left an earlier normal receipt batch-consumable")
	}
	if _, err := issuer.admit(liveOperationEngineCapture, 6, session.NonceFor(liveOperationEngineCapture, 6)); err == nil {
		t.Fatal("cleanup transition admitted a normal proof operation")
	}
	if err := session.EnterCleanup(issuer); err == nil {
		t.Fatal("cleanup transition accepted a replay")
	}

	failedUninstall, err := issuer.admit(liveOperationWingetExactUninstall, 13, session.NonceFor(liveOperationWingetExactUninstall, 13))
	if err != nil {
		t.Fatalf("admit final uninstall: %v", err)
	}
	originalResolver := newWindowsLiveWingetResolver
	newWindowsLiveWingetResolver = func() (liveTrustedWingetResolver, error) { return nil, fmt.Errorf("resolver unavailable") }
	if receipt, err := runWindowsLiveWingetExactUninstall(context.Background(), failedUninstall, trustedLiveMutationPermit{}, maxLiveObserverOutputBytes); err == nil || receipt != nil {
		t.Fatalf("failed final uninstall = %+v, %v, want no receipt", receipt, err)
	}
	newWindowsLiveWingetResolver = originalResolver
	t.Cleanup(func() { newWindowsLiveWingetResolver = originalResolver })
	uninstall, err := issuer.admit(liveOperationWingetExactUninstall, 13, session.NonceFor(liveOperationWingetExactUninstall, 13))
	if err != nil {
		t.Fatalf("resolver failure left final uninstall admission unavailable: %v", err)
	}
	uninstallPermit, err := session.MintMutationPermit(uninstall)
	if err != nil {
		t.Fatalf("MintMutationPermit(final uninstall) error = %v", err)
	}
	expected := liveReceiptExpectedIdentity{definition: uninstallPermit.capability.definition, engine: uninstallPermit.capability.engine, seed: uninstallPermit.capability.seed, packageRef: uninstallPermit.capability.packageRef, comparator: uninstallPermit.capability.comparator, targets: uninstallPermit.capability.targets, observer: uninstallPermit.capability.observer, workflow: uninstallPermit.capability.workflow, runner: uninstallPermit.capability.executableSHA256}
	uninstallRequest := newLiveTypedMutation(uninstall, uninstallPermit, liveOperationWingetExactUninstall, uninstallPermit.capability.executable, uninstallPermit.capability.arguments, "", nil, expected, 1)
	if !uninstallPermit.capability.finalize(uninstallRequest, expected.runner, time.Now().UTC()) {
		t.Fatal("final uninstall fixture could not commit its authority permit")
	}
	receipt := liveUnsealedReceiptForTest(t, uninstall, nil, nil, "")
	receipt.executable, receipt.args, receipt.environment, receipt.expected = uninstallRequest.executable, append([]string(nil), uninstallRequest.args...), nil, expected
	receipt.image.sha256 = expected.runner
	receipt.requestSHA256 = receipt.requestDigest()
	receipt.resultSHA256 = receipt.resultDigest()
	if err := issuer.sealFn(receipt); err != nil {
		t.Fatalf("seal final uninstall fixture: %v", err)
	}
	if !receipt.sealed || receipt.failure != "" {
		t.Fatalf("final uninstall fixture = %+v", receipt)
	}
	uninstall.complete()

	failedWipe, err := issuer.admit(liveOperationDeclaredTargetWipe, 14, session.NonceFor(liveOperationDeclaredTargetWipe, 14))
	if err != nil {
		t.Fatalf("admit final wipe: %v", err)
	}
	originalAppData := windowsLiveRoamingAppData
	windowsLiveRoamingAppData = func() (string, error) { return "", fmt.Errorf("APPDATA unavailable") }
	if receipt, err := runWindowsLiveDeclaredTargetWipe(failedWipe, trustedLiveHostMutationPermit{}, definition, appData); err == nil || receipt != nil {
		t.Fatalf("failed final wipe = %+v, %v, want no receipt", receipt, err)
	}
	windowsLiveRoamingAppData = originalAppData
	t.Cleanup(func() { windowsLiveRoamingAppData = originalAppData })
	wipe, err := issuer.admit(liveOperationDeclaredTargetWipe, 14, session.NonceFor(liveOperationDeclaredTargetWipe, 14))
	if err != nil {
		t.Fatalf("known-folder failure left final wipe admission unavailable: %v", err)
	}
	wipeBinding, err := windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		t.Fatal(err)
	}
	wipePermit, err := session.MintHostMutationPermit(wipe, wipeBinding)
	if err != nil {
		t.Fatal(err)
	}
	wipeReceipt, err := runWindowsLiveDeclaredTargetWipe(wipe, wipePermit, definition, appData)
	if err != nil || wipeReceipt == nil || !wipeReceipt.sealed || !wipeReceipt.succeeded {
		t.Fatalf("final declared-target wipe = %+v, %v", wipeReceipt, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("final declared-target wipe left target: %v", err)
	}

	cleanup, err := issuer.admit(liveOperationAttemptRootCleanup, 15, session.NonceFor(liveOperationAttemptRootCleanup, 15))
	if err != nil {
		t.Fatalf("admit attempt-root cleanup: %v", err)
	}
	cleanupBinding, err := windowsLiveHostMutationBinding(definition, appData, attempt)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPermit, err := session.MintHostMutationPermit(cleanup, cleanupBinding)
	if err != nil {
		t.Fatal(err)
	}
	cleanupReceipt, err := runWindowsLiveAttemptRootCleanup(cleanup, cleanupPermit, definition, appData, attempt)
	if err != nil || cleanupReceipt == nil || !cleanupReceipt.sealed || !cleanupReceipt.succeeded {
		t.Fatalf("final attempt-root cleanup = %+v, %v", cleanupReceipt, err)
	}
	if _, err := os.Lstat(attempt.path); !os.IsNotExist(err) {
		t.Fatalf("final attempt-root cleanup left root: %v", err)
	}
}

func TestWindowsLiveAttemptRootCleanupFailureReturnsNoSuccessReceipt(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData, parent := t.TempDir(), t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	withWindowsLiveTestRunnerTemp(t, parent)
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	session := liveWindowsHostMutationSession(t, definition, 1, liveOperationAttemptRootCleanup)
	issuer := session.NewReceiptIssuer()
	admission, err := issuer.admit(liveOperationAttemptRootCleanup, 1, session.NonceFor(liveOperationAttemptRootCleanup, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := windowsLiveHostMutationBinding(definition, appData, attempt)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(attempt.path); err != nil {
		t.Fatal(err)
	}
	receipt, err := runWindowsLiveAttemptRootCleanup(admission, permit, definition, appData, attempt)
	if err == nil || receipt != nil {
		t.Fatalf("failed attempt-root cleanup = %+v, %v, want no success receipt", receipt, err)
	}
	retry, err := issuer.admit(liveOperationAttemptRootCleanup, 1, session.NonceFor(liveOperationAttemptRootCleanup, 1))
	if err != nil {
		t.Fatalf("prepare failure left cleanup admission active: %v", err)
	}
	retry.complete()
	if _, err := issuer.admit(liveOperationAttemptRootCleanup, 2, session.NonceFor(liveOperationAttemptRootCleanup, 2)); err == nil {
		t.Fatal("failed cleanup advanced the issuer sequence")
	}
}

func TestWindowsLiveHandleCleanupRejectsBudgetExhaustionWithoutTraversal(t *testing.T) {
	root := t.TempDir()
	deep := root
	for range 4 {
		deep = filepath.Join(deep, "nested")
		if err := os.Mkdir(deep, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeWindowsLiveDirectoryWithBudget(context.Background(), root, windowsLiveCleanupBudget{maxDepth: 2, maxEntries: 32}); err == nil {
		t.Fatal("handle cleanup accepted a depth-exhausting tree")
	}
	if _, err := os.Lstat(root); err != nil {
		t.Fatalf("budget failure removed root: %v", err)
	}
	deadline, cancel := context.WithCancel(context.Background())
	cancel()
	if err := removeWindowsLiveDirectoryWithBudget(deadline, root, windowsLiveCleanupBudget{maxDepth: 32, maxEntries: 32}); err == nil {
		t.Fatal("handle cleanup accepted an expired context")
	}
}

func TestWindowsLiveHandleCleanupRejectsChildJunctionSwap(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := windowsLiveCleanupBeforeChildOpen
	windowsLiveCleanupBeforeChildOpen = func(path string) {
		if path != child || os.Remove(path) != nil {
			return
		}
		_, _ = exec.Command("cmd", "/d", "/c", "mklink", "/J", path, outside).CombinedOutput()
	}
	t.Cleanup(func() { windowsLiveCleanupBeforeChildOpen = original })
	if err := removeWindowsLiveDirectoryWithBudget(context.Background(), root, windowsLiveCleanupBudget{maxDepth: 8, maxEntries: 8}); err == nil {
		t.Fatal("handle cleanup accepted a child junction swap")
	}
	if _, err := os.Lstat(sentinel); err != nil {
		t.Fatalf("junction swap deleted outside sentinel: %v", err)
	}
}

func TestWindowsLiveHandleCleanupPreventsPostOpenChildReplacement(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	child, backup := filepath.Join(root, "child.txt"), filepath.Join(root, "backup.txt")
	if err := os.WriteFile(child, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := windowsLiveCleanupAfterChildOpen
	var replacementErr error
	windowsLiveCleanupAfterChildOpen = func(path string) {
		if path == child {
			replacementErr = os.Rename(child, backup)
		}
	}
	t.Cleanup(func() { windowsLiveCleanupAfterChildOpen = original })
	if err := removeWindowsLiveDirectoryWithBudget(context.Background(), root, windowsLiveCleanupBudget{maxDepth: 8, maxEntries: 8}); err != nil {
		t.Fatal(err)
	}
	if replacementErr == nil {
		t.Fatal("exclusive child handle allowed replacement")
	}
	if _, err := os.Lstat(sentinel); err != nil {
		t.Fatalf("post-open replacement deleted outside sentinel: %v", err)
	}
}

func TestWindowsLiveHandleCleanupReadsLargeFanoutInBoundedChunks(t *testing.T) {
	for _, test := range []struct {
		name      string
		entries   int
		wantError bool
	}{
		{name: "exact entry budget", entries: maxWindowsLiveCleanupEntries},
		{name: "one entry over budget", entries: maxWindowsLiveCleanupEntries + 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for index := 0; index < test.entries; index++ {
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("entry-%04d", index)), []byte("inside"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			original := windowsLiveCleanupReadDir
			var chunks []int
			windowsLiveCleanupReadDir = func(file *os.File, count int) ([]os.DirEntry, error) {
				chunks = append(chunks, count)
				return file.ReadDir(count)
			}
			t.Cleanup(func() { windowsLiveCleanupReadDir = original })
			err := removeWindowsLiveDirectoryWithBudget(context.Background(), root, windowsLiveCleanupBudget{maxDepth: 8, maxEntries: maxWindowsLiveCleanupEntries})
			if (err != nil) != test.wantError {
				t.Fatalf("removeWindowsLiveDirectoryWithBudget() error = %v, wantError %v", err, test.wantError)
			}
			if len(chunks) == 0 {
				t.Fatal("handle cleanup did not enumerate the fanout")
			}
			for _, count := range chunks {
				if count < 1 || count > 64 {
					t.Fatalf("handle cleanup requested an unbounded directory read of %d entries", count)
				}
			}
			if test.wantError {
				if _, err := os.Lstat(root); err != nil {
					t.Fatalf("fanout budget failure removed root: %v", err)
				}
				if _, err := os.Lstat(filepath.Join(root, fmt.Sprintf("entry-%04d", test.entries-1))); err != nil {
					t.Fatalf("fanout budget failure processed the over-budget entry: %v", err)
				}
			} else if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("exact fanout cleanup left root: %v", err)
			}
		})
	}
}

func TestWindowsLiveHandleCleanupCancellationAfterOpenLeavesObjectsIntact(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(string) string
	}{
		{name: "leaf", make: func(root string) string {
			path := filepath.Join(root, "child.txt")
			if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "directory", make: func(root string) string {
			path := filepath.Join(root, "child")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			child := test.make(root)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := windowsLiveCleanupAfterChildOpen
			windowsLiveCleanupAfterChildOpen = func(path string) {
				if path == child {
					cancel()
				}
			}
			t.Cleanup(func() { windowsLiveCleanupAfterChildOpen = original })
			if err := removeWindowsLiveDirectoryWithBudget(ctx, root, windowsLiveCleanupBudget{maxDepth: 8, maxEntries: 8}); err == nil {
				t.Fatal("handle cleanup accepted cancellation after opening a child")
			}
			if _, err := os.Lstat(child); err != nil {
				t.Fatalf("cancellation after child open removed %q: %v", child, err)
			}
		})
	}
}

func liveWindowsHostMutationSession(t *testing.T, definition LiveDefinition, sequence uint64, operation liveOperation) *LiveAuthoritySession {
	t.Helper()
	digest, err := CanonicalLiveDefinitionSHA256(definition)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	campaign := sha256.Sum256([]byte("windows-host-mutation"))
	return &LiveAuthoritySession{campaignID: campaign, campaign: LiveCampaign{PhaseNonce: "windows-host-mutation", ExpiresAt: now.Add(time.Hour)}, now: now, minted: make(map[liveAuthorityPermitKey]struct{}), definition: liveAuthorityDefinition{
		definition: liveSHA256Bytes(digest), targets: liveSHA256Bytes(liveSHA256Hex(definition.DeclaredTargets)), observer: liveSHA256Bytes(liveSHA256Hex(definition.Observer)), workflow: sha256.Sum256([]byte("workflow")), operations: map[uint64]LiveCampaignOperation{sequence: {Sequence: sequence, Operation: string(operation)}},
	}}
}

func liveWindowsCleanupAuthoritySession(t *testing.T, definition LiveDefinition) *LiveAuthoritySession {
	t.Helper()
	campaign := liveTestCampaign()
	now := time.Now().UTC()
	campaign.ExpiresAt = now.Add(time.Hour)
	campaign.ModuleRevision = definition.ModuleRevision
	campaign.ValidationSourceSHA256 = definition.ValidationSourceSHA256
	campaign.SeedSHA256 = definition.SeedSHA256
	campaign.PackageRef = definition.WingetRef
	campaign.PackageArguments = []string{"uninstall", "--id", campaign.PackageRef, "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}
	campaign.ComparatorSHA256 = liveSHA256Hex(definition.Comparator)
	campaign.TargetsSHA256 = liveSHA256Hex(definition.DeclaredTargets)
	campaign.ObserverSHA256 = liveSHA256Hex(definition.Observer)
	campaign.EngineSHA256 = strings.Repeat("1", 64)
	campaign.ValidatorSHA256 = strings.Repeat("2", 64)
	liveTestBindEngineOperations(&campaign)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(mustLiveWorkflowRunJSON(t, campaign))
	}))
	t.Cleanup(server.Close)
	session, err := NewLiveAuthoritySession(context.Background(), newLiveWorkflowRunClient(server.Client(), server.URL), LiveAuthoritySessionRequest{
		Campaign: campaign, Definition: definition, DefinitionSHA256: canonicalLiveDefinitionSHA256(t, definition), EngineSHA256: campaign.EngineSHA256, ValidatorSHA256: campaign.ValidatorSHA256, ControllerCheckoutCommit: campaign.ControllerCommit, TestedCheckoutCommit: campaign.TestedCheckoutCommit, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func withWindowsLiveTestAppData(t *testing.T, root string) {
	t.Helper()
	original := windowsLiveRoamingAppData
	windowsLiveRoamingAppData = func() (string, error) { return root, nil }
	t.Cleanup(func() { windowsLiveRoamingAppData = original })
	t.Setenv("APPDATA", root)
}

func withWindowsLiveTestRunnerTemp(t *testing.T, root string) {
	t.Helper()
	original := windowsLiveRunnerTemp
	windowsLiveRunnerTemp = func() (string, error) { return root, nil }
	t.Cleanup(func() { windowsLiveRunnerTemp = original })
}

func TestResolveWindowsLiveDeclaredTargetsRejectsForeignAndOverlappingTemplates(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*LiveDefinition){
		func(value *LiveDefinition) { value.DeclaredTargets[0].Template = `%LOCALAPPDATA%\Notepad++\config.xml` },
		func(value *LiveDefinition) {
			value.DeclaredTargets[0].Template = `%APPDATA%\Notepad++\userDefineLangs\config.xml`
		},
	} {
		candidate := definition
		candidate.DeclaredTargets = append([]LiveDeclaredTarget(nil), definition.DeclaredTargets...)
		mutate(&candidate)
		if _, err := resolveWindowsLiveDeclaredTargets(candidate, t.TempDir()); err == nil {
			t.Fatal("resolveWindowsLiveDeclaredTargets() accepted an unsafe template")
		}
	}
}
