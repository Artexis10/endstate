// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

var _ provision.CapabilityReporter = (*Backend)(nil)

func TestCapabilities_NixIsAtomicAndRollbackable(t *testing.T) {
	c := New().Capabilities()
	if !c.AtomicSet || !c.NativeRollback || !c.Transactional || !c.BatchInstall {
		t.Fatalf("nix capabilities should all be true, got %+v", c)
	}
}
