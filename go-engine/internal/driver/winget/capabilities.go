// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import "github.com/Artexis10/endstate/go-engine/internal/provision"

// Capabilities reports the winget driver's capabilities. winget installs
// per-package and non-atomically, with no native rollback and no whole-set
// transaction, so every capability is false. It satisfies
// provision.CapabilityReporter for symmetry with the realizer.
func (w *WingetDriver) Capabilities() provision.Capabilities {
	return provision.Capabilities{}
}
