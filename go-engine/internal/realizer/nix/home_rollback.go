// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"os"
	"strconv"
	"strings"

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
	_, err := os.Readlink(link)
	if err != nil {
		return 0, realizer.ErrHomeSnapshotMissing
	}
	// Native inventory canonicalizes the snapshot; host symlink separators must
	// not become a different Nix store path or activation script.
	data, err := b.native("path-info", link)
	if err != nil {
		if failure, ok := err.(*realizer.Error); ok {
			return 0, failure
		}
		return 0, realizer.ErrHomeSnapshotMissing
	}
	store := strings.TrimSpace(string(data))
	if !storePath(store) {
		return 0, realizer.ErrHomeSnapshotMissing
	}

	generation, activationErr := b.activateHomeOutput(store)
	if failure, ok := activationErr.(*realizer.Error); ok && failure.Code == envelope.ErrInstallFailed {
		failure.Code = envelope.ErrRollbackFailed
		if failure.Stage != "commit" && failure.Stage != "activation" {
			failure.Stage = "rollback"
		}
	}
	return generation, activationErr
}
