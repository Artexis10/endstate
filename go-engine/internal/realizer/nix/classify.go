// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// anchor maps a stable Nix message-text substring to an engine error class.
type anchor struct {
	needle  string // lowercase substring
	code    envelope.ErrorCode
	subcode string
}

// anchorTable carries the TOP-LEVEL error class for the failure classes the
// validation spike proved are NOT structurally separable (daemon, permission,
// eval). Every needle is HARVESTED FROM REAL Determinate Nix 3.21.0 output and
// locked by classify_contract_test.go against captured stderr fixtures — never
// reasoned (the spike got 0/3 collision anchors right by reasoning). Order
// matters: most specific / most operator-actionable first.
var anchorTable = []anchor{
	{"opening a connection to remote store", envelope.ErrRealizerUnavailable, "daemon"},
	{"cannot connect to socket", envelope.ErrRealizerUnavailable, "daemon"},
	{"connection refused", envelope.ErrRealizerUnavailable, "daemon"},
	{"permission denied", envelope.ErrPermissionDenied, "permission"},
	{"read-only file system", envelope.ErrPermissionDenied, "permission"},
	{"does not provide attribute", envelope.ErrInstallFailed, "eval"},
	{"undefined variable", envelope.ErrInstallFailed, "eval"},
	{"unable to download", envelope.ErrInstallFailed, "network"},
	{"http error", envelope.ErrInstallFailed, "network"},
	{"while fetching", envelope.ErrInstallFailed, "network"},
	{"couldn't resolve host", envelope.ErrInstallFailed, "network"},
	{"an existing package already provides the following file", envelope.ErrInstallFailed, "collision"},
	{"the conflicting packages have a priority of", envelope.ErrInstallFailed, "collision"},
	{"no space left", envelope.ErrInstallFailed, "store"},
}

// classify maps a nix invocation outcome to an engine error. It is the SINGLE
// source of the error code:
//   - exitCode < 0  -> spawn failure (nix missing/unrunnable) -> REALIZER_UNAVAILABLE
//   - exit 0 + generation advanced -> success (nil)
//   - otherwise: anchor table carries the top-level class; structural signals
//     refine the subcode; unrecognised -> INSTALL_FAILED with raw text retained
//     in Err.Raw (which only ever lands in envelope error.detail).
func classify(exitCode int, p parsedLog, generationAdvanced bool) *realizer.Error {
	raw := strings.Join(p.errorMsgs, "\n")

	if exitCode < 0 {
		return &realizer.Error{Code: envelope.ErrRealizerUnavailable, Subcode: "spawn", Stage: "spawn", Raw: raw}
	}
	if exitCode == 0 && generationAdvanced {
		return nil
	}

	for _, a := range anchorTable {
		if strings.Contains(p.blob, a.needle) {
			return &realizer.Error{Code: a.code, Subcode: a.subcode, Stage: stageOf(p, generationAdvanced), Raw: raw}
		}
	}

	// No anchor matched: fall back to INSTALL_FAILED. Use structural signals for
	// a best-effort subcode (never for the top-level class).
	subcode := ""
	switch {
	case p.sawBuild() && !generationAdvanced:
		subcode = "collision"
	case p.sawDownload() && !p.sawBuild():
		subcode = "network"
	}
	return &realizer.Error{Code: envelope.ErrInstallFailed, Subcode: subcode, Stage: stageOf(p, generationAdvanced), Raw: raw}
}
