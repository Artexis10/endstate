// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// compile-time assertion: the Nix backend implements the optional
// realizer.Pruner capability, so the apply path can discover convergence
// eligibility by type-assertion.
var _ realizer.Pruner = (*Backend)(nil)

// Remove uninstalls the named profile elements in one `nix profile remove`, an
// atomic generation switch. It is the convergence (prune) half of `apply
// --prune`: the caller passes the drift set (installed-but-undeclared element
// names, as they appear in Current().Elements). Remove(nil) is a no-op.
//
// It mirrors Realize's contract exactly: it returns (Result, nil) and carries any
// classified failure in Result.Err (never the second return value), so the apply
// layer consumes prune and install failures through one code path. Failures are
// classified through the same anchor table as Realize — spawn/daemon surface
// REALIZER_UNAVAILABLE, permission PERMISSION_DENIED, anything else INSTALL_FAILED
// — with raw Nix text confined to Error.Raw (destined for envelope error.detail).
func (b *Backend) Remove(names []string) (realizer.Result, error) {
	res := realizer.Result{FromGeneration: -1, ToGeneration: -1}
	if len(names) == 0 {
		cur, _ := b.Current()
		res.After = cur
		res.FromGeneration, res.ToGeneration = cur.Generation, cur.Generation
		return res, nil
	}

	before := b.gen()
	res.FromGeneration = before

	args := []string{"profile", "remove", "--profile", b.Profile}
	args = append(args, names...)
	args = append(args, "--log-format", "internal-json")

	_, stderr, exit, err := b.Run(args...)
	p := parseInternalJSON(stderr)
	if err != nil { // spawn failure (binary missing/unrunnable)
		res.Err = classify(-1, p, false)
		return res, nil
	}

	after := b.gen()
	res.ToGeneration = after
	res.Advanced = after > before
	if exit != 0 || !res.Advanced {
		res.Err = classify(exit, p, res.Advanced)
	}

	cur, _ := b.Current()
	res.After = cur
	return res, nil
}
