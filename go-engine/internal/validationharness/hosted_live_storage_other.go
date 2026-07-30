// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationharness

import "fmt"

func bindHostedLiveStorageRoot(_ any) (hostedLiveStorageRoot, error) {
	return hostedLiveStorageRoot{}, fmt.Errorf("hosted live storage is unsupported on this platform")
}
