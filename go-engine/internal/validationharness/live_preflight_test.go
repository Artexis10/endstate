// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"testing"
)

func TestLiveReceiptIssuerOptionalPreflightSkipIsAuthorityBoundAndAtomic(t *testing.T) {
	generic := newLiveReceiptIssuer()
	if err := generic.skipDeclaredPreflight(); err == nil {
		t.Fatal("generic issuer exposed preflight skip")
	}

	session := &LiveAuthoritySession{
		campaignID: sha256.Sum256([]byte("campaign")),
		campaign:   LiveCampaign{PhaseNonce: "phase"},
		definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{
			1: {Sequence: 1, Operation: string(liveOperationWingetExactUninstall)},
			2: {Sequence: 2, Operation: string(liveOperationDeclaredTargetWipe)},
			3: {Sequence: 3, Operation: string(liveOperationEngineApply)},
		}},
	}
	issuer := session.NewReceiptIssuer()
	active, err := issuer.admit(liveOperationWingetExactUninstall, 1, session.NonceFor(liveOperationWingetExactUninstall, 1))
	if err != nil {
		t.Fatalf("admit preflight: %v", err)
	}
	if err := issuer.skipDeclaredPreflight(); err == nil {
		t.Fatal("skip accepted while admission was active")
	}
	active.complete()
	session = &LiveAuthoritySession{
		campaignID: sha256.Sum256([]byte("campaign")),
		campaign:   LiveCampaign{PhaseNonce: "phase"},
		definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{
			1: {Sequence: 1, Operation: string(liveOperationWingetExactUninstall)},
			2: {Sequence: 2, Operation: string(liveOperationDeclaredTargetWipe)},
			3: {Sequence: 3, Operation: string(liveOperationEngineApply)},
		}},
	}
	issuer = session.NewReceiptIssuer()
	if err := issuer.skipDeclaredPreflight(); err != nil {
		t.Fatalf("skip declared preflight: %v", err)
	}
	if err := issuer.skipDeclaredPreflight(); err == nil {
		t.Fatal("skip replay advanced optional pair twice")
	}
	if _, err := issuer.admit(liveOperationEngineApply, 3, session.NonceFor(liveOperationEngineApply, 3)); err != nil {
		t.Fatalf("apply did not admit after atomic preflight skip: %v", err)
	}
}

func TestLiveReceiptIssuerSealedPreflightAdvancesOnlyOneSlot(t *testing.T) {
	session := &LiveAuthoritySession{
		campaignID: sha256.Sum256([]byte("campaign")),
		campaign:   LiveCampaign{PhaseNonce: "phase"},
		definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{
			1: {Sequence: 1, Operation: string(liveOperationWingetExactUninstall)},
			2: {Sequence: 2, Operation: string(liveOperationDeclaredTargetWipe)},
			3: {Sequence: 3, Operation: string(liveOperationEngineApply)},
		}},
	}
	issuer := session.NewReceiptIssuer()
	preflight, err := issuer.admit(liveOperationWingetExactUninstall, 1, session.NonceFor(liveOperationWingetExactUninstall, 1))
	if err != nil {
		t.Fatalf("admit preflight: %v", err)
	}
	_ = liveReceiptForTest(t, preflight, nil, nil)
	preflight.complete()
	if err := issuer.skipDeclaredPreflight(); err == nil {
		t.Fatal("skip accepted after executing preflight")
	}
	if _, err := issuer.admit(liveOperationDeclaredTargetWipe, 2, session.NonceFor(liveOperationDeclaredTargetWipe, 2)); err != nil {
		t.Fatalf("declared wipe did not follow sealed preflight: %v", err)
	}
}

func TestLiveReceiptIssuerWithoutPreflightAdmitsApplyFirst(t *testing.T) {
	session := &LiveAuthoritySession{
		campaignID: sha256.Sum256([]byte("campaign")),
		campaign:   LiveCampaign{PhaseNonce: "phase"},
		definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{
			1: {Sequence: 1, Operation: string(liveOperationEngineApply)},
		}},
	}
	issuer := session.NewReceiptIssuer()
	if err := issuer.skipDeclaredPreflight(); err == nil {
		t.Fatal("no-preflight plan exposed skip")
	}
	if _, err := issuer.admit(liveOperationEngineApply, 1, session.NonceFor(liveOperationEngineApply, 1)); err != nil {
		t.Fatalf("apply did not admit first without preflight: %v", err)
	}
}
