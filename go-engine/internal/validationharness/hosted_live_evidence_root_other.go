// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationharness

import "fmt"

type hostedLiveEvidenceResultRoot struct{}

func newHostedLiveEvidenceResultRoot(LiveCampaign, LiveDefinition) (hostedLiveEvidenceResultRoot, error) {
	return hostedLiveEvidenceResultRoot{}, fmt.Errorf("hosted live evidence is unavailable on this platform")
}

func persistHostedLiveEvidence(hostedLiveEvidenceResultRoot, hostedLiveEvidence) error {
	return fmt.Errorf("hosted live evidence is unavailable on this platform")
}
