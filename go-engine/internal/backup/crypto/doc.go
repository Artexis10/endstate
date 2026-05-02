// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package crypto defines the cryptographic primitives required by Endstate
// Hosted Backup as locked in docs/contracts/hosted-backup-contract.md
// (sections 2, 3 and 6).
//
// Scope boundary: this package's bodies are intentionally STUB
// implementations. Every operation returns ErrNotImplemented and carries a
// // TODO(prompt-3) comment. The cryptographic implementation is delivered
// in PROMPT 3 (see .claude/scratch/hosted-backup-v2/PROMPT_3_engine_crypto.md).
//
// The interface defined here is the integration surface the rest of the
// engine compiles against (auth, upload, download). PROMPT 3 fills in the
// bodies without changing the surface.
package crypto

import "errors"

// ErrNotImplemented is returned by every stub function until PROMPT 3 lands.
var ErrNotImplemented = errors.New("crypto: not implemented; see PROMPT_3_engine_crypto")
