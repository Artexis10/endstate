// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"strconv"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// compile-time assertion: the Nix backend implements provision.Rollbacker, so
// the rollback command can discover eligibility by type-assertion.
var _ provision.Rollbacker = (*Backend)(nil)

// Rollback reverts the Endstate-managed profile to a prior version via
// `nix profile rollback`. A positive `to` selects a specific Nix profile
// version (`--to <to>`); a non-positive `to` rolls back to the immediately
// previous version (bare `nix profile rollback`). The mapping from an
// engine-owned Provisioning Generation number to this Nix version is the
// caller's responsibility (the command layer reads the generation's Native
// anchor) — Rollback speaks only Nix versions.
//
// Failures are classified through the same anchor path as Realize: spawn and
// daemon failures surface REALIZER_UNAVAILABLE, permission failures
// PERMISSION_DENIED, and any other non-zero exit ROLLBACK_FAILED (classify's
// INSTALL_FAILED fallback is remapped, since this is the rollback verb). Raw
// Nix text is retained ONLY in Error.Raw, destined for envelope error.detail.
func (b *Backend) Rollback(to int) error {
	args := []string{"profile", "rollback", "--profile", b.Profile}
	if to > 0 {
		args = append(args, "--to", strconv.Itoa(to))
	}
	args = append(args, "--log-format", "internal-json")

	_, stderr, exit, err := b.Run(args...)
	p := parseInternalJSON(stderr)
	if err != nil { // spawn failure (binary missing/unrunnable)
		return classify(-1, p, false)
	}
	if exit == 0 {
		return nil
	}

	// Non-zero exit: reuse the locked anchor table for systemic classes
	// (daemon -> REALIZER_UNAVAILABLE, permission -> PERMISSION_DENIED), but
	// remap the generic INSTALL_FAILED fallback to ROLLBACK_FAILED so the error
	// names the rollback verb rather than an install.
	rerr := classify(exit, p, false)
	if rerr != nil && rerr.Code == envelope.ErrInstallFailed {
		rerr.Code = envelope.ErrRollbackFailed
		rerr.Stage = "rollback"
	}
	return rerr
}
