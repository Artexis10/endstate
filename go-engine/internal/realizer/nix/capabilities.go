// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import "github.com/Artexis10/endstate/go-engine/internal/provision"

// Capabilities reports the Nix realizer's capabilities. Nix realizes a whole set
// as one atomic profile generation, supports native rollback (`nix profile
// rollback`), and installs the set in a single transaction. It satisfies
// provision.CapabilityReporter so a later phase can discover rollback
// eligibility by type-assertion.
func (b *Backend) Capabilities() provision.Capabilities {
	return provision.Capabilities{
		AtomicSet:      true,
		NativeRollback: true,
		Transactional:  true,
		BatchInstall:   true,
	}
}
