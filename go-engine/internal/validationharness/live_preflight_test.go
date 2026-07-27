// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import "testing"

func TestLiveReceiptIssuerOptionalPreflightSkipPreservesApplyOrdering(t *testing.T) {
	issuer := newLiveReceiptIssuer()
	preflightNonce := liveReceiptTestNonce(71)
	if err := issuer.skipDeclaredPreflight(); err == nil {
		t.Fatal("generic issuer exposed preflight skip")
	}
	admission, err := issuer.admit(liveOperationWingetExactUninstall, 1, preflightNonce)
	if err != nil {
		t.Fatalf("admit preflight error = %v", err)
	}
	admission.complete()
	if _, err := issuer.admit(liveOperationEngineApply, 2, liveReceiptTestNonce(72)); err != nil {
		t.Fatalf("admit apply after explicit skip: %v", err)
	}

	withoutPreflight := newLiveReceiptIssuer()
	if _, err := withoutPreflight.admit(liveOperationEngineApply, 1, liveReceiptTestNonce(73)); err != nil {
		t.Fatalf("admit contiguous apply without preflight: %v", err)
	}
}
