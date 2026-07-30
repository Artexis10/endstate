// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationharness

import "fmt"

func newHostedLiveRunner(*LiveAuthoritySession, LiveDefinition, string, string) (hostedLiveRunner, error) {
	return nil, fmt.Errorf("hosted live Windows runner is unavailable on this platform")
}
