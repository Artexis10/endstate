// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package backup

import (
	"testing"
	"time"
)

func TestTransferTimeout_PrefersDependencyThenEnvironment(t *testing.T) {
	t.Setenv("ENDSTATE_BACKUP_TRANSFER_TIMEOUT", "3s")
	if got := TransferTimeout(25 * time.Millisecond); got != 25*time.Millisecond {
		t.Fatalf("dependency timeout = %v, want 25ms", got)
	}
	if got := TransferTimeout(0); got != 3*time.Second {
		t.Fatalf("environment timeout = %v, want 3s", got)
	}
}

func TestTransferTimeout_InvalidEnvironmentUsesSafeDefault(t *testing.T) {
	t.Setenv("ENDSTATE_BACKUP_TRANSFER_TIMEOUT", "not-a-duration")
	if got := TransferTimeout(0); got != defaultTransferTimeout {
		t.Fatalf("invalid environment timeout = %v, want %v", got, defaultTransferTimeout)
	}
}
