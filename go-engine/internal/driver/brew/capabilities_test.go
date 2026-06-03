// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

var _ provision.CapabilityReporter = (*BrewDriver)(nil)

func TestCapabilities_BrewIsAllFalse(t *testing.T) {
	c := New().Capabilities()
	if c.AtomicSet || c.NativeRollback || c.Transactional || c.BatchInstall {
		t.Fatalf("brew capabilities should all be false, got %+v", c)
	}
}
