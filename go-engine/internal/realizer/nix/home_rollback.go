// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// compile-time assertion: the Nix backend implements realizer.HomeRollbacker, so
// the rollback command can discover home-manager config-rollback support by
// type-assertion.
var _ realizer.HomeRollbacker = (*Backend)(nil)

// RollbackHome reverts the home-manager configuration to a prior provisioning
// generation by re-activating the home-manager generation that generation
// recorded. home-manager has no arbitrary version pointer-move (`switch
// --rollback` is one step only), so re-activation is done by running the target
// generation's built `activate` script — which mints a NEW FORWARD home-manager
// generation reproducing that config (confirmed empirically). This is append-only,
// exactly like native package rollback: the newest generation is the active one,
// and the user never references a backend-native generation number.
//
// `generation` is the home-manager generation number recorded in the Provisioning
// Generation (HomeGenRef.Generation). RollbackHome resolves the
// `home-manager-<generation>-link` snapshot under the home-manager profile dir and
// runs its `activate` script. When that link no longer exists (the snapshot was
// garbage-collected from the store) it returns realizer.ErrHomeSnapshotMissing so
// the caller can fall back to re-activating the recorded configuration source.
//
// Failures are classified through the same anchor path as ActivateHome: spawn ->
// REALIZER_UNAVAILABLE, permission -> PERMISSION_DENIED, daemon ->
// REALIZER_UNAVAILABLE, and any other non-zero exit -> ROLLBACK_FAILED (classify's
// INSTALL_FAILED fallback is remapped, since this is the rollback verb). The
// `activate` script prints plain text (not nix internal-json), so classification
// anchors against the raw stderr via parsePlainLog. Raw text is retained ONLY in
// Error.Raw, destined for envelope error.detail (the moat).
func (b *Backend) RollbackHome(generation int) (int, error) {
	link := homeProfilePath() + "-" + strconv.Itoa(generation) + "-link"
	store, err := os.Readlink(link)
	if err != nil {
		return 0, realizer.ErrHomeSnapshotMissing
	}

	activate := filepath.Join(store, "activate")
	_, stderr, exit, runErr := b.execScript(activate)
	if runErr != nil { // spawn failure (script missing/unrunnable)
		return 0, classify(-1, parsePlainLog(stderr), false)
	}
	if exit != 0 {
		rerr := classify(exit, parsePlainLog(stderr), false)
		if rerr != nil && rerr.Code == envelope.ErrInstallFailed {
			rerr.Code = envelope.ErrRollbackFailed
			rerr.Stage = "rollback"
		}
		return 0, rerr
	}
	return b.homeGen(), nil
}
