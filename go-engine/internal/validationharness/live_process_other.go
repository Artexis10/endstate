// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationharness

import "context"

func runLiveProcessPlatform(context.Context, LiveProcessRequest, func(liveReceiptImageIdentity) error) (liveProcessOutput, error) {
	return liveProcessOutput{}, liveExecutionError(LiveExecutionUnsupported, nil)
}
