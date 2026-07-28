// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationharness

import "testing"

func TestHostedLiveEvidenceRootFailsClosedOffWindows(t *testing.T) {
	if _, err := newHostedLiveEvidenceResultRoot(LiveCampaign{}, LiveDefinition{}); err == nil {
		t.Fatal("newHostedLiveEvidenceResultRoot() succeeded off Windows")
	}
	if err := persistHostedLiveEvidence(hostedLiveEvidenceResultRoot{}, hostedLiveEvidence{}); err == nil {
		t.Fatal("persistHostedLiveEvidence() succeeded off Windows")
	}
}
