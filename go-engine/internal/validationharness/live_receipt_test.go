// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"
)

// Test-only raw handoff exercises receipt integrity; production decoders expose
// only fixed typed projections.
func liveReceiptDecoderHandoff(receipt *liveExecutionReceipt, operation liveOperation, sequence uint64, nonce [32]byte) ([]byte, []byte, error) {
	if receipt == nil {
		return nil, nil, errors.New("live receipt handoff rejected")
	}
	value, ok := liveReceiptIssuers.Load(receipt.issuerID)
	issuer, ok := value.(*liveReceiptIssuer)
	if !ok || !issuer.consumeFn(receipt, operation, sequence, nonce) {
		return nil, nil, errors.New("live receipt handoff rejected")
	}
	return append([]byte(nil), receipt.stdout...), append([]byte(nil), receipt.stderr...), nil
}

func TestLiveReceiptIssuerRejectsZeroReplayAndOutOfOrderAdmissions(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	var zero [32]byte
	if _, err := issuer.admit(liveOperationEngineApply, 1, zero); err == nil {
		t.Fatal("admit() accepted a zero nonce")
	}
	first := liveReceiptTestNonce(1)
	if _, err := issuer.admit(liveOperationEngineApply, 2, first); err == nil {
		t.Fatal("admit() accepted a sequence gap")
	}
	if _, err := issuer.admit(liveOperationEngineApply, 1, first); err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	if _, err := issuer.admit(liveOperationEngineApply, 2, first); err == nil {
		t.Fatal("admit() accepted a replayed nonce")
	}
}

func TestLiveReceiptAdmissionAdvancesOnlyAfterSealing(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(8))
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	admission.complete()
	if _, err := issuer.admit(liveOperationEngineApply, 2, liveReceiptTestNonce(9)); err == nil {
		t.Fatal("unsealed admission advanced issuer sequence")
	}
	retry, err := issuer.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(8))
	if err != nil {
		t.Fatalf("unsealed admission did not release its exact retry slot: %v", err)
	}
	retry.complete()
	sealed, err := issuer.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(9))
	if err != nil {
		t.Fatalf("unsealed admission did not release exact issuer state: %v", err)
	}
	_ = liveReceiptForTest(t, sealed, nil, nil)
	sealed.complete()
	if _, err := issuer.admit(liveOperationEngineApply, 2, liveReceiptTestNonce(10)); err != nil {
		t.Fatalf("sealed admission did not advance issuer sequence: %v", err)
	}
}

func TestLiveCaptureReceiptProjectionAuthenticatesWithoutConsuming(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationEngineCapture, 1, liveReceiptTestNonce(71))
	if err != nil {
		t.Fatal(err)
	}
	receipt := liveReceiptForTest(t, admission, []byte("capture"), []byte("events"))
	admission.complete()
	projected, ok := projectLiveCaptureReceipt(issuer, receipt, 1, admission.nonce)
	if !ok || projected == receipt || string(projected.stdout) != string(receipt.stdout) {
		t.Fatal("projectLiveCaptureReceipt() did not return a detached authenticated capture receipt")
	}
	if !issuer.consumeBatchFn([]liveReceiptExpectation{{receipt: receipt, operation: liveOperationEngineCapture, sequence: 1, nonce: admission.nonce}}) {
		t.Fatal("non-consuming projection consumed the final receipt")
	}
}

func TestLiveReceiptIssuerRejectsUncommittedMalformedSeal(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(18))
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	if err := issuer.sealFn(&liveExecutionReceipt{issuerID: issuer.id, operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce, admissionToken: admission.token}); err == nil {
		t.Fatal("sealFn accepted an uncommitted malformed receipt")
	}
	admission.complete()
	if _, err := issuer.admit(liveOperationEngineVerify, 1, liveReceiptTestNonce(19)); err != nil {
		t.Fatalf("malformed seal advanced issuer sequence: %v", err)
	}
}

func TestLiveReceiptIssuerRejectsMalformedApplySkipVerifyAttack(t *testing.T) {
	session := &LiveAuthoritySession{
		campaignID: sha256.Sum256([]byte("campaign")),
		campaign:   LiveCampaign{PhaseNonce: "phase"},
		definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{
			1: {Sequence: 1, Operation: string(liveOperationWingetExactUninstall)},
			2: {Sequence: 2, Operation: string(liveOperationDeclaredTargetWipe)},
			3: {Sequence: 3, Operation: string(liveOperationEngineApply)},
			4: {Sequence: 4, Operation: string(liveOperationEngineVerify)},
		}},
	}
	issuer := session.NewReceiptIssuer()
	if err := issuer.skipDeclaredPreflight(); err != nil {
		t.Fatal(err)
	}
	apply, err := issuer.admit(liveOperationEngineApply, 3, session.NonceFor(liveOperationEngineApply, 3))
	if err != nil {
		t.Fatalf("admit apply: %v", err)
	}
	if err := issuer.sealFn(&liveExecutionReceipt{issuerID: issuer.id, operation: apply.operation, sequence: apply.sequence, nonce: apply.nonce, admissionToken: apply.token}); err == nil {
		t.Fatal("sealFn accepted malformed apply evidence")
	}
	apply.complete()
	if _, err := issuer.admit(liveOperationEngineVerify, 4, session.NonceFor(liveOperationEngineVerify, 4)); err == nil {
		t.Fatal("malformed apply evidence advanced to verify")
	}
}

func TestLiveReceiptIssuerRejectsDirectMutationLaunchCommitAttack(t *testing.T) {
	session := &LiveAuthoritySession{
		campaignID: sha256.Sum256([]byte("campaign")),
		campaign:   LiveCampaign{PhaseNonce: "phase"},
		definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{
			1: {Sequence: 1, Operation: string(liveOperationWingetExactUninstall)},
			2: {Sequence: 2, Operation: string(liveOperationDeclaredTargetWipe)},
			3: {Sequence: 3, Operation: string(liveOperationEngineApply)},
			4: {Sequence: 4, Operation: string(liveOperationEngineVerify)},
		}},
	}
	issuer := session.NewReceiptIssuer()
	if err := issuer.skipDeclaredPreflight(); err != nil {
		t.Fatal(err)
	}
	apply, err := issuer.admit(liveOperationEngineApply, 3, session.NonceFor(liveOperationEngineApply, 3))
	if err != nil {
		t.Fatalf("admit apply: %v", err)
	}
	committed := issuer.commitLaunchFn(apply)
	sealed := issuer.sealFn(liveUnsealedReceiptForTest(t, apply, nil, nil, "")) == nil
	apply.complete()
	_, advanced := issuer.admit(liveOperationEngineVerify, 4, session.NonceFor(liveOperationEngineVerify, 4))
	if committed || sealed || advanced == nil {
		t.Fatal("direct mutation launch commit advanced the campaign")
	}
}

func TestLiveReceiptIssuerProbeLaunchCommitRejectsMutations(t *testing.T) {
	for _, operation := range []liveOperation{liveOperationEngineApply, liveOperationEngineVerify, liveOperationEngineCapture, liveOperationEngineRebuild, liveOperationEngineRevert, liveOperationHashBoundSeed, liveOperationWingetExactInstall, liveOperationWingetExactUninstall, liveOperationDeclaredTargetWipe, liveOperationAttemptRootCleanup} {
		issuer := newLiveReceiptIssuer()
		admission, err := issuer.admit(operation, 1, liveReceiptTestNonce(byte(len(operation))))
		if err != nil {
			t.Fatalf("admit %s: %v", operation, err)
		}
		if issuer.commitLaunchFn(admission) {
			t.Fatalf("probe launch commit accepted mutation %s", operation)
		}
	}
	issuer := newLiveReceiptIssuer()
	probe, err := issuer.admit(liveOperationWingetExactList, 1, liveReceiptTestNonce(24))
	if err != nil {
		t.Fatal(err)
	}
	if !issuer.commitLaunchFn(probe) {
		t.Fatal("probe launch commit rejected winget list")
	}
	if err := issuer.sealFn(liveUnsealedReceiptForTest(t, probe, nil, nil, "")); err != nil {
		t.Fatalf("probe receipt did not seal: %v", err)
	}
}

func TestLiveReceiptIssuerSealsOnlyExactCommittedEvidenceOnce(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(21))
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	liveTestCommitReceipt(t, admission, liveUnsealedReceiptForTest(t, admission, nil, nil, ""))
	for _, mutate := range []func(*liveExecutionReceipt){
		func(receipt *liveExecutionReceipt) { receipt.operation = liveOperationEngineVerify },
		func(receipt *liveExecutionReceipt) { receipt.expected.definition[0]++ },
		func(receipt *liveExecutionReceipt) { receipt.image.sha256 = [32]byte{} },
		func(receipt *liveExecutionReceipt) { receipt.started = time.Time{} },
		func(receipt *liveExecutionReceipt) { receipt.requestSHA256 = [32]byte{} },
		func(receipt *liveExecutionReceipt) { receipt.resultSHA256 = [32]byte{} },
	} {
		receipt := liveUnsealedReceiptForTest(t, admission, nil, nil, "")
		mutate(receipt)
		if err := issuer.sealFn(receipt); err == nil {
			t.Fatal("sealFn accepted mismatched committed evidence")
		}
	}
	receipt := liveUnsealedReceiptForTest(t, admission, nil, nil, "")
	if err := issuer.sealFn(receipt); err != nil {
		t.Fatalf("seal exact committed evidence: %v", err)
	}
	if err := issuer.sealFn(receipt); err == nil {
		t.Fatal("sealFn accepted a second seal")
	}
	admission.complete()
	if err := issuer.sealFn(receipt); err == nil {
		t.Fatal("sealFn accepted a released admission")
	}
}

func TestLiveReceiptDecoderRejectsForgedOrMismatchedReceipt(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	nonce := liveReceiptTestNonce(2)
	admission, err := issuer.admit(liveOperationEngineCapture, 1, nonce)
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	receipt := liveReceiptForTest(t, admission, []byte("stdout"), []byte("stderr"))
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineApply, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted the wrong operation")
	}
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineCapture, 2, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted the wrong sequence")
	}
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineCapture, 1, liveReceiptTestNonce(3)); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted the wrong nonce")
	}
	forged := *receipt
	forged.issuerID++
	if _, _, err := liveReceiptDecoderHandoff(&forged, liveOperationEngineCapture, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted a distinct capability")
	}
	zero := *receipt
	zero.issuerID = 0
	if _, _, err := liveReceiptDecoderHandoff(&zero, liveOperationEngineCapture, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted a zero capability")
	}
}

func TestLiveReceiptDecoderDefensivelyCopiesAndVerifiesOutput(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	nonce := liveReceiptTestNonce(4)
	admission, err := issuer.admit(liveOperationEngineVerify, 1, nonce)
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	receipt := liveReceiptForTest(t, admission, []byte("stdout"), []byte("stderr"))
	stdout, stderr, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineVerify, 1, nonce)
	if err != nil {
		t.Fatalf("liveReceiptDecoderHandoff() error = %v", err)
	}
	stdout[0], stderr[0] = 'X', 'Y'
	if string(receipt.stdout) != "stdout" || string(receipt.stderr) != "stderr" {
		t.Fatal("liveReceiptDecoderHandoff() exposed receipt output storage")
	}
	receipt.stdout[0] = 'Z'
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineVerify, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted mutated output")
	}
}

func TestLiveReceiptDecoderRejectsRequestDigestMismatch(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	nonce := liveReceiptTestNonce(5)
	admission, err := issuer.admit(liveOperationHashBoundSeed, 1, nonce)
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	receipt := liveReceiptForTest(t, admission, []byte("stdout"), nil)
	for _, mutate := range []func(*liveExecutionReceipt){
		func(receipt *liveExecutionReceipt) { receipt.args[0] = "different" },
		func(receipt *liveExecutionReceipt) { receipt.environment["PATH"] = "different" },
		func(receipt *liveExecutionReceipt) { receipt.directory = `C:\different` },
		func(receipt *liveExecutionReceipt) { receipt.expected.seed[0]++ },
	} {
		copy := *receipt
		copy.args = append([]string(nil), receipt.args...)
		copy.environment = cloneLiveEnvironment(receipt.environment)
		mutate(&copy)
		if _, _, err := liveReceiptDecoderHandoff(&copy, liveOperationHashBoundSeed, 1, nonce); err == nil {
			t.Fatal("liveReceiptDecoderHandoff() accepted a request digest mismatch")
		}
	}
}

func TestLiveReceiptFailureIsSealedButNotDecodable(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	nonce := liveReceiptTestNonce(6)
	admission, err := issuer.admit(liveOperationEngineRebuild, 1, nonce)
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	receipt := liveReceiptFailureForTest(t, admission, []byte("partial"), nil, LiveExecutionCanceled)
	if err := receipt.validate(); err != nil {
		t.Fatalf("failure receipt validate() error = %v", err)
	}
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineRebuild, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted a failure receipt")
	}
	admission.complete()
	if _, err := issuer.admit(liveOperationEngineRebuild, 2, liveReceiptTestNonce(17)); err != nil {
		t.Fatalf("sealed failure did not advance issuer sequence: %v", err)
	}
}

func TestLiveReceiptDecoderRejectsResultIdentityMutation(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	nonce := liveReceiptTestNonce(7)
	admission, err := issuer.admit(liveOperationEngineApply, 1, nonce)
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	receipt := liveReceiptForTest(t, admission, nil, nil)
	receipt.image.sha256[0]++
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineApply, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted a result identity mismatch")
	}
}

func TestLiveReceiptDecoderRejectsForgedRecomputedDigests(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	nonce := liveReceiptTestNonce(10)
	admission, err := issuer.admit(liveOperationEngineApply, 1, nonce)
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	receipt := liveReceiptForTest(t, admission, []byte("original"), nil)
	receipt.stdout = []byte("substituted")
	receipt.stdoutSHA256 = sha256.Sum256(receipt.stdout)
	receipt.resultSHA256 = receipt.resultDigest()
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineApply, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted a forged recomputed receipt")
	}
}

func TestLiveReceiptBatchConsumptionIsAtomicAndSingleUse(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	first, err := issuer.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(13))
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	firstReceipt := liveReceiptForTest(t, first, nil, nil)
	first.complete()
	second, err := issuer.admit(liveOperationEngineCapture, 2, liveReceiptTestNonce(14))
	if err != nil {
		t.Fatalf("second admit: %v", err)
	}
	secondReceipt := liveReceiptForTest(t, second, nil, nil)
	second.complete()
	batch := []liveReceiptExpectation{{receipt: firstReceipt, operation: liveOperationEngineApply, sequence: 1, nonce: first.nonce}, {receipt: secondReceipt, operation: liveOperationEngineCapture, sequence: 2, nonce: second.nonce}}
	if !issuer.consumeBatchFn(batch) {
		t.Fatal("consumeBatchFn() rejected valid receipt batch")
	}
	if issuer.consumeBatchFn(batch) {
		t.Fatal("consumeBatchFn() accepted replayed receipt batch")
	}
	third, err := issuer.admit(liveOperationEngineRebuild, 3, liveReceiptTestNonce(15))
	if err != nil {
		t.Fatalf("third admit: %v", err)
	}
	thirdReceipt := liveReceiptForTest(t, third, nil, nil)
	third.complete()
	if issuer.consumeBatchFn([]liveReceiptExpectation{{receipt: thirdReceipt, operation: liveOperationEngineApply, sequence: 3, nonce: third.nonce}}) {
		t.Fatal("consumeBatchFn() accepted wrong operation")
	}
	if issuer.consumeBatchFn([]liveReceiptExpectation{{receipt: thirdReceipt, operation: liveOperationEngineRebuild, sequence: 3, nonce: liveReceiptTestNonce(16)}}) {
		t.Fatal("consumeBatchFn() accepted wrong nonce")
	}
}

func TestLiveReceiptCleanupAdmissionsAdvanceAfterFailedCleanupWork(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	cleanup := []liveOperation{liveOperationWingetExactUninstall, liveOperationDeclaredTargetWipe, liveOperationAttemptRootCleanup}
	if err := issuer.enterCleanupFn(3, cleanup); err != nil {
		t.Fatalf("enterCleanupFn() error = %v", err)
	}
	if _, err := issuer.admit(liveOperationEngineApply, 3, liveReceiptTestNonce(30)); err == nil {
		t.Fatal("cleanup admitted a proof operation")
	}
	if _, err := issuer.admit(liveOperationDeclaredTargetWipe, 4, liveReceiptTestNonce(31)); err == nil {
		t.Fatal("cleanup admitted an out-of-order slot")
	}
	first, err := issuer.admit(liveOperationWingetExactUninstall, 3, liveReceiptTestNonce(32))
	if err != nil {
		t.Fatalf("admit final uninstall: %v", err)
	}
	first.complete()
	retry, err := issuer.admit(liveOperationWingetExactUninstall, 3, liveReceiptTestNonce(33))
	if err != nil {
		t.Fatalf("retry final uninstall after prelaunch failure: %v", err)
	}
	_ = liveReceiptForTest(t, retry, nil, nil)
	retry.complete()
	second, err := issuer.admit(liveOperationDeclaredTargetWipe, 4, liveReceiptTestNonce(34))
	if err != nil {
		t.Fatalf("admit final wipe after failed uninstall: %v", err)
	}
	if err := issuer.sealHostMutationFn(&liveHostMutationReceipt{issuerID: issuer.id, operation: second.operation, sequence: second.sequence, nonce: second.nonce, admissionToken: second.token}); err == nil {
		t.Fatal("sealHostMutationFn() accepted malformed cleanup receipt")
	}
	second.complete()
	if _, err := issuer.admit(liveOperationEngineRebuild, 5, liveReceiptTestNonce(35)); err == nil {
		t.Fatal("cleanup admitted a proof operation after a failed cleanup seal")
	}
	wipeRetry, err := issuer.admit(liveOperationDeclaredTargetWipe, 4, liveReceiptTestNonce(36))
	if err != nil {
		t.Fatalf("retry final wipe after failed seal: %v", err)
	}
	wipeRetry.complete()
}

func TestLiveReceiptMarkedCleanupPrelaunchFailureAdvancesOnlyOneFinalSlot(t *testing.T) {
	operations := []liveOperation{liveOperationWingetExactUninstall, liveOperationDeclaredTargetWipe, liveOperationAttemptRootCleanup}
	for failedIndex, failedOperation := range operations {
		t.Run(string(failedOperation), func(t *testing.T) {
			issuer := newLiveReceiptIssuer()
			if err := issuer.enterCleanupFn(3, operations); err != nil {
				t.Fatal(err)
			}
			for index := 0; index < failedIndex; index++ {
				admission, err := issuer.admit(operations[index], uint64(3+index), liveReceiptTestNonce(byte(110+index)))
				if err != nil {
					t.Fatal(err)
				}
				_ = liveReceiptForTest(t, admission, nil, nil)
				admission.complete()
			}
			failed, err := issuer.admit(failedOperation, uint64(3+failedIndex), liveReceiptTestNonce(byte(120+failedIndex)))
			if err != nil {
				t.Fatal(err)
			}
			if !issuer.markCleanupPrelaunchFailureFn(failed) {
				t.Fatal("cleanup marker rejected the active final-suffix admission")
			}
			failed.complete()
			if _, err := issuer.admit(failedOperation, uint64(3+failedIndex), failed.nonce); err == nil {
				t.Fatal("marked prelaunch failure admitted its retired nonce")
			}
			if failedIndex+1 == len(operations) {
				if _, err := issuer.admit(liveOperationAttemptRootCleanup, 6, liveReceiptTestNonce(127)); err == nil {
					t.Fatal("marked final cleanup failure admitted another slot")
				}
				return
			}
			nextSequence, nextOperation := uint64(4+failedIndex), operations[failedIndex+1]
			if _, err := issuer.admit(failedOperation, uint64(3+failedIndex), liveReceiptTestNonce(byte(130+failedIndex))); err == nil {
				t.Fatal("marked prelaunch failure retried its cleanup slot")
			}
			if _, err := issuer.admit(nextOperation, nextSequence+1, liveReceiptTestNonce(byte(140+failedIndex))); err == nil {
				t.Fatal("marked prelaunch failure skipped more than one cleanup slot")
			}
			next, err := issuer.admit(nextOperation, nextSequence, liveReceiptTestNonce(byte(150+failedIndex)))
			if err != nil {
				t.Fatalf("marked prelaunch failure did not admit the next cleanup slot: %v", err)
			}
			next.complete()
		})
	}
}

func TestLiveReceiptCleanupPrelaunchMarkerRejectsForeignStaleAndOutOfOrderAdmissions(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	if err := issuer.enterCleanupFn(3, []liveOperation{liveOperationWingetExactUninstall, liveOperationDeclaredTargetWipe, liveOperationAttemptRootCleanup}); err != nil {
		t.Fatal(err)
	}
	admission, err := issuer.admit(liveOperationWingetExactUninstall, 3, liveReceiptTestNonce(160))
	if err != nil {
		t.Fatal(err)
	}
	for index, forged := range []liveReceiptAdmission{
		{issuer: newLiveReceiptIssuer(), operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce, token: admission.token},
		{issuer: issuer, operation: liveOperationDeclaredTargetWipe, sequence: admission.sequence, nonce: admission.nonce, token: admission.token},
		{issuer: issuer, operation: admission.operation, sequence: admission.sequence + 1, nonce: admission.nonce, token: admission.token},
		{issuer: issuer, operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce, token: [32]byte{}},
	} {
		if issuer.markCleanupPrelaunchFailureFn(forged) {
			t.Fatalf("cleanup marker accepted a foreign or out-of-order admission %d", index)
		}
	}
	if !issuer.markCleanupPrelaunchFailureFn(admission) {
		t.Fatal("cleanup marker rejected the active admission")
	}
	admission.complete()
	if issuer.markCleanupPrelaunchFailureFn(admission) {
		t.Fatal("cleanup marker accepted a stale/replayed admission")
	}
}

func TestDecodeLiveJourneyReceiptsDerivesRevertJournalFromAuthenticatedOutput(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	definition, set := liveJourneyReceiptSetForTest(t, issuer, `C:\trusted\restore\journal.json`)
	projection, proof, failure := decodeLiveJourneyReceiptProof(issuer, definition, set)
	if failure != nil {
		t.Fatalf("decodeLiveJourneyReceipts() failure = %+v", failure)
	}
	if proof.revertJournal != `C:\trusted\restore\journal.json` || proof.revert.JournalUsed != proof.revertJournal || proof.restoreRebuild.envelopeRunID != "rebuild-restore" || proof.restoreRebuild.applyRunID != "apply-rebuild-restore" || !proof.restoreRebuild.bundle.Extracted || len(proof.restoreRebuild.restoreItems) == 0 || projection.ModuleID != definition.ModuleID {
		t.Fatalf("journey proof = %+v", proof)
	}
	for _, arg := range set.Revert.receipt.args {
		if arg == "--journal" || strings.HasPrefix(arg, "--journal=") {
			t.Fatalf("production revert invocation unexpectedly selects a journal: %q", set.Revert.receipt.args)
		}
	}
	set.RestoreRebuild.receipt.stdout[0] = 'X'
	if proof.restoreRebuild.envelopeRunID != "rebuild-restore" || len(proof.restoreRebuild.restoreItems) == 0 {
		t.Fatal("journey proof aliases mutable receipt output")
	}
	if _, failure := decodeLiveJourneyReceipts(issuer, definition, set); failure == nil {
		t.Fatal("decodeLiveJourneyReceipts() accepted replayed receipt batch")
	}
}

func TestDecodeLiveJourneyReceiptsRejectsCallerSelectedJournalArguments(t *testing.T) {
	journal := `C:\trusted\restore\journal.json`
	for _, test := range []struct {
		name string
		args []string
	}{
		{"missing", []string{"revert"}},
		{"duplicate", []string{"revert", "--journal", journal, "--journal", `C:\trusted\restore\other.json`}},
		{"relative", []string{"revert", "--journal", `restore\journal.json`}},
		{"equals form", []string{"revert", "--journal=" + journal}},
	} {
		t.Run(test.name, func(t *testing.T) {
			issuer := newLiveReceiptIssuer()
			definition, set := liveJourneyReceiptSetForTestWithRevert(t, issuer, journal, test.args)
			projection, failure := decodeLiveJourneyReceipts(issuer, definition, set)
			if test.name == "missing" {
				if failure != nil || projection.ModuleID != definition.ModuleID {
					t.Fatalf("decodeLiveJourneyReceipts() rejected production revert arguments: %+v", failure)
				}
				return
			}
			if failure == nil {
				t.Fatal("decodeLiveJourneyReceipts() accepted a caller-selected revert journal")
			}
		})
	}
}

func TestDecodeLiveJourneyReceiptsRejectsUntrustedOrNonCanonicalOutputJournal(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	definition, set := liveJourneyReceiptSetForTestWithRevert(t, issuer, `relative\journal.json`, []string{"revert"})
	if _, failure := decodeLiveJourneyReceipts(issuer, definition, set); failure == nil {
		t.Fatal("decodeLiveJourneyReceipts() accepted a noncanonical output journal")
	}
}

func TestDecodeLiveJourneyReceiptsRejectsInvalidRuntimeRestoreAuthorityBeforeProofConsumption(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	definition, set := liveJourneyReceiptSetForTest(t, issuer, `C:\trusted\restore\journal.json`)
	set.runtimeRestoreTargets = map[string]string{"apps/notepad-plus-plus/config.xml": `C:\Users\runner\AppData\Roaming\Notepad++\config.xml`}
	if _, failure := decodeLiveJourneyReceipts(issuer, definition, set); failure == nil {
		t.Fatal("decodeLiveJourneyReceipts() accepted an incomplete runtime restore authority")
	}
	set.runtimeRestoreTargets = nil
	if _, failure := decodeLiveJourneyReceipts(issuer, definition, set); failure != nil {
		t.Fatalf("invalid runtime restore authority consumed authenticated proof: %+v", failure)
	}
}

func liveJourneyReceiptSetForTest(t *testing.T, issuer *liveReceiptIssuer, journal string) (LiveDefinition, liveJourneyReceiptSet) {
	return liveJourneyReceiptSetForTestWithRevert(t, issuer, journal, []string{"revert", "--json", "--events", "jsonl"})
}

func liveJourneyReceiptSetForTestWithRevert(t *testing.T, issuer *liveReceiptIssuer, journal string, revertArgs []string) (LiveDefinition, liveJourneyReceiptSet) {
	t.Helper()
	definition := productionLiveDecoderDefinition(t)
	outputs := []struct {
		operation liveOperation
		output    liveCommandOutput
	}{
		{liveOperationEngineApply, liveCommandOutput{Stdout: liveTestEnvelope("apply", "apply-initial", liveApplyData("installed")), Stderr: liveEvents("apply", "apply-initial")}},
		{liveOperationEngineVerify, liveCommandOutput{Stdout: liveTestEnvelope("verify", "verify-initial", liveVerifyData()), Stderr: liveEvents("verify", "verify-initial")}},
		{liveOperationEngineCapture, liveCommandOutput{Stdout: liveTestEnvelope("capture", "capture-initial", liveCaptureData()), Stderr: liveEvents("capture", "capture-initial")}},
		{liveOperationEngineRebuild, liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-restore", liveRebuildData("installed", "restored")), Stderr: liveEvents("rebuild", "rebuild-restore")}},
		{liveOperationEngineRevert, liveCommandOutput{Stdout: liveTestEnvelope("revert", "revert-config", strings.Replace(liveRevertData(), `C:\\trusted\\restore\\journal.json`, strings.ReplaceAll(journal, `\`, `\\`), 1)), Stderr: liveEvents("revert", "revert-config")}},
		{liveOperationEngineRebuild, liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-recovery", liveRebuildData("present", "restored")), Stderr: liveEvents("rebuild", "rebuild-recovery")}},
		{liveOperationEngineRebuild, liveCommandOutput{Stdout: liveTestEnvelope("rebuild", "rebuild-converged", liveRebuildData("present", "skipped_up_to_date")), Stderr: liveEvents("rebuild", "rebuild-converged")}},
	}
	expectations := make([]liveReceiptExpectation, len(outputs))
	for index, phase := range outputs {
		sequence := uint64(index + 1)
		admission, err := issuer.admit(phase.operation, sequence, liveReceiptTestNonce(byte(80+index)))
		if err != nil {
			t.Fatalf("admit %s: %v", phase.operation, err)
		}
		receipt := liveUnsealedReceiptForTest(t, admission, phase.output.Stdout, phase.output.Stderr, "")
		if phase.operation == liveOperationEngineRevert {
			receipt.args = append([]string(nil), revertArgs...)
			receipt.requestSHA256 = receipt.requestDigest()
		}
		liveTestCommitReceipt(t, admission, receipt)
		if err := issuer.sealFn(receipt); err != nil {
			t.Fatalf("seal %s: %v", phase.operation, err)
		}
		admission.complete()
		expectations[index] = liveReceiptExpectation{receipt: receipt, operation: phase.operation, sequence: sequence, nonce: admission.nonce}
	}
	return definition, liveJourneyReceiptSet{
		ScenarioID:         liveConfigRoundtripScenarioID,
		InitialApply:       expectations[0],
		Verify:             expectations[1],
		Capture:            expectations[2],
		RestoreRebuild:     expectations[3],
		Revert:             expectations[4],
		RecoveryRebuild:    expectations[5],
		ConvergenceRebuild: expectations[6],
		PackageAfterRevert: PackageObservation{Ref: definition.WingetRef, Version: "8.7.1", Status: "present"},
	}
}

func liveReceiptForTest(t *testing.T, admission liveReceiptAdmission, stdout, stderr []byte) *liveExecutionReceipt {
	return liveReceiptFailureForTest(t, admission, stdout, stderr, "")
}

func liveReceiptFailureForTest(t *testing.T, admission liveReceiptAdmission, stdout, stderr []byte, failure LiveExecutionFailureCode) *liveExecutionReceipt {
	receipt := liveUnsealedReceiptForTest(t, admission, stdout, stderr, failure)
	liveTestCommitReceipt(t, admission, receipt)
	if err := admission.issuer.sealFn(receipt); err != nil {
		t.Fatalf("seal receipt: %v", err)
	}
	return receipt
}

// liveTestCommitReceipt is a test-only stand-in for the suspended-process
// finalizer. Production mutations can reach launch-committed only there.
func liveTestCommitReceipt(t *testing.T, admission liveReceiptAdmission, receipt *liveExecutionReceipt) {
	t.Helper()
	if admission.operation == liveOperationWingetExactList {
		if !admission.issuer.commitLaunchFn(admission) {
			t.Fatal("commit probe launch")
		}
		return
	}
	arguments := append([]string(nil), receipt.args...)
	if admission.operation == liveOperationWingetExactUninstall {
		arguments = []string{"uninstall", "--id", "apps.fixture", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}
		receipt.args = append([]string(nil), arguments...)
		receipt.requestSHA256 = receipt.requestDigest()
	}
	permit := newTrustedLiveMutationPermit(admission, receipt.expected, receipt.executable, arguments, receipt.directory, receipt.environment)
	image := receipt.expected.engine
	if admission.operation == liveOperationHashBoundSeed || admission.operation == liveOperationWingetExactInstall || admission.operation == liveOperationWingetExactUninstall {
		permit.capability.executableSHA256 = receipt.expected.runner
		image = receipt.expected.runner
	}
	request := newLiveTypedMutation(admission, permit, admission.operation, receipt.executable, arguments, receipt.directory, receipt.environment, receipt.expected, 0)
	if !permit.capability.finalize(request, image, time.Now().UTC()) {
		t.Fatal("commit mutation launch")
	}
}

func liveUnsealedReceiptForTest(t *testing.T, admission liveReceiptAdmission, stdout, stderr []byte, failure LiveExecutionFailureCode) *liveExecutionReceipt {
	t.Helper()
	started := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	receipt := &liveExecutionReceipt{
		issuerID:       admission.issuer.id,
		operation:      admission.operation,
		sequence:       admission.sequence,
		nonce:          admission.nonce,
		admissionToken: admission.token,
		executable:     `C:\trusted\engine.exe`,
		args:           []string{"capture", "apps.fixture"},
		directory:      `C:\trusted`,
		environment:    map[string]string{"PATH": `C:\Windows\System32`},
		expected: liveReceiptExpectedIdentity{
			definition: sha256.Sum256([]byte("definition")),
			engine:     sha256.Sum256([]byte("engine")),
			seed:       sha256.Sum256([]byte("seed")),
			packageRef: sha256.Sum256([]byte("package")),
			comparator: sha256.Sum256([]byte("comparator")),
			targets:    sha256.Sum256([]byte("targets")),
			observer:   sha256.Sum256([]byte("observer")),
			workflow:   sha256.Sum256([]byte("workflow")),
			runner:     sha256.Sum256([]byte("runner")),
		},
		image:    liveReceiptImageIdentity{canonical: `C:\trusted\engine.exe`, volume: 1, indexHigh: 2, indexLow: 3, sha256: sha256.Sum256([]byte("image"))},
		pid:      42,
		created:  started,
		started:  started,
		finished: started.Add(time.Second),
		exitCode: 0,
		failure:  failure,
		stdout:   append([]byte(nil), stdout...),
		stderr:   append([]byte(nil), stderr...),
	}
	receipt.requestSHA256 = receipt.requestDigest()
	receipt.stdoutSHA256 = sha256.Sum256(receipt.stdout)
	receipt.stderrSHA256 = sha256.Sum256(receipt.stderr)
	receipt.resultSHA256 = receipt.resultDigest()
	return receipt
}

func liveReceiptTestNonce(value byte) [32]byte {
	var nonce [32]byte
	nonce[0] = value
	return nonce
}
