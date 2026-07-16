// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package restore

import "fmt"

func durableLegacyRegistryStates(JournalEntry, string) (durableLegacyRevertState, durableLegacyRevertState, error) {
	return durableLegacyRevertState{}, durableLegacyRevertState{}, fmt.Errorf("durable legacy registry revert is only supported on Windows")
}

func applyDurableLegacyRegistryRevert(entry JournalEntry) error {
	return fmt.Errorf("durable legacy %s revert is only supported on Windows", entry.RestoreType)
}
