// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationharness

import "fmt"

func snapshotHostedLiveTargets(LiveDefinition, string) (hostedLiveTargets, error) {
	return hostedLiveTargets{}, fmt.Errorf("hosted live target proof is unavailable on this platform")
}
