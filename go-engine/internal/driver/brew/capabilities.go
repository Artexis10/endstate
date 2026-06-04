// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import "github.com/Artexis10/endstate/go-engine/internal/provision"

// Capabilities reports the brew driver's capabilities. Like winget, Homebrew
// installs per-package and non-atomically, with no native rollback and no
// whole-set transaction, so every capability is false (AtomicSet=false,
// NativeRollback=false, Transactional=false, BatchInstall=false). It satisfies
// provision.CapabilityReporter for symmetry with the realizer and winget.
func (b *BrewDriver) Capabilities() provision.Capabilities {
	return provision.Capabilities{}
}
