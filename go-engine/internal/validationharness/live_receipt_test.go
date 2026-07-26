// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"testing"
	"time"
)

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

func TestLiveReceiptAdmissionCannotBeReusedAfterCompletion(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(8))
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	admission.complete()
	if _, err := issuer.admit(liveOperationEngineApply, 2, liveReceiptTestNonce(9)); err != nil {
		t.Fatalf("completed admission did not release exact issuer state: %v", err)
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
	receipt := liveReceiptForTest(t, admission, []byte("partial"), nil)
	receipt.failure = LiveExecutionCanceled
	receipt.resultSHA256 = receipt.resultDigest()
	if err := receipt.validate(); err != nil {
		t.Fatalf("failure receipt validate() error = %v", err)
	}
	if _, _, err := liveReceiptDecoderHandoff(receipt, liveOperationEngineRebuild, 1, nonce); err == nil {
		t.Fatal("liveReceiptDecoderHandoff() accepted a failure receipt")
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

func liveReceiptForTest(t *testing.T, admission liveReceiptAdmission, stdout, stderr []byte) *liveExecutionReceipt {
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
		},
		image:    liveReceiptImageIdentity{canonical: `C:\trusted\engine.exe`, volume: 1, indexHigh: 2, indexLow: 3, sha256: sha256.Sum256([]byte("image"))},
		pid:      42,
		created:  started,
		started:  started,
		finished: started.Add(time.Second),
		exitCode: 0,
		stdout:   append([]byte(nil), stdout...),
		stderr:   append([]byte(nil), stderr...),
	}
	receipt.requestSHA256 = receipt.requestDigest()
	receipt.stdoutSHA256 = sha256.Sum256(receipt.stdout)
	receipt.stderrSHA256 = sha256.Sum256(receipt.stderr)
	receipt.resultSHA256 = receipt.resultDigest()
	if err := admission.issuer.sealFn(receipt); err != nil {
		t.Fatalf("seal receipt: %v", err)
	}
	return receipt
}

func liveReceiptTestNonce(value byte) [32]byte {
	var nonce [32]byte
	nonce[0] = value
	return nonce
}
