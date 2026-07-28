// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestLiveAuthorityMintsOnlyBoundDeclaredTargetWipePermit(t *testing.T) {
	now := time.Now().UTC()
	session := liveHostMutationSession(now, liveOperationDeclaredTargetWipe, 1)
	issuer := session.NewReceiptIssuer()
	admission, err := issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding := liveHostMutationBinding{appData: sha256.Sum256([]byte("appdata"))}
	permit, err := session.MintHostMutationPermit(admission, binding)
	if err != nil || !permit.capability.validFor(admission, binding, now) {
		t.Fatalf("MintHostMutationPermit() = %+v, %v", permit, err)
	}
	if _, err := session.MintHostMutationPermit(admission, binding); err == nil {
		t.Fatal("host mutation permit replay was accepted")
	}
	wrong := binding
	wrong.appData[0]++
	if permit.capability.validFor(admission, wrong, now) {
		t.Fatal("host mutation permit accepted a foreign APPDATA root")
	}
	permit.capability.operation = liveOperationAttemptRootCleanup
	if permit.capability.validFor(admission, binding, now) {
		t.Fatal("host mutation permit accepted a substituted operation")
	}
	foreignIssuer := newLiveReceiptIssuer()
	foreign, err := foreignIssuer.admit(liveOperationDeclaredTargetWipe, 1, admission.nonce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.MintHostMutationPermit(foreign, binding); err == nil {
		t.Fatal("host mutation permit accepted a foreign issuer")
	}
	expired := liveHostMutationSession(now, liveOperationDeclaredTargetWipe, 1)
	expired.campaign.ExpiresAt = now.Add(-time.Second)
	expiredIssuer := expired.NewReceiptIssuer()
	expiredAdmission, err := expiredIssuer.admit(liveOperationDeclaredTargetWipe, 1, expired.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expired.MintHostMutationPermit(expiredAdmission, binding); err == nil {
		t.Fatal("expired host mutation permit was minted")
	}
}

func TestLiveHostMutationReceiptSealerBindsFinalizedTarget(t *testing.T) {
	now := time.Now().UTC()
	session := liveHostMutationSession(now, liveOperationDeclaredTargetWipe, 1)
	issuer := session.NewReceiptIssuer()
	admission, err := issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding := liveHostMutationBinding{appData: sha256.Sum256([]byte("appdata"))}
	permit, err := session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	if !issuer.finalizeHostMutationFn(admission, permit.capability, binding, now) {
		t.Fatal("host mutation finalizer rejected its exact permit")
	}
	wrong := binding
	wrong.appData[0]++
	receipt := &liveHostMutationReceipt{issuerID: issuer.id, operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce, admissionToken: admission.token, binding: wrong, succeeded: true}
	if err := issuer.sealHostMutationFn(receipt); err == nil {
		t.Fatal("host mutation receipt sealer accepted a substituted target")
	}
}

func liveHostMutationSession(now time.Time, operation liveOperation, sequence uint64) *LiveAuthoritySession {
	value := sha256.Sum256([]byte("host-mutation"))
	return &LiveAuthoritySession{
		campaignID: value, campaign: LiveCampaign{PhaseNonce: "host-mutation", ExpiresAt: now.Add(time.Hour)}, now: now,
		minted:     make(map[liveAuthorityPermitKey]struct{}),
		definition: liveAuthorityDefinition{definition: value, targets: value, observer: value, workflow: value, operations: map[uint64]LiveCampaignOperation{sequence: {Sequence: sequence, Operation: string(operation)}}},
	}
}
