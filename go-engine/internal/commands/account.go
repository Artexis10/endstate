// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// AccountFlags holds the parsed CLI flags for `endstate account`.
//
// All `account *` subcommands operate on the Hosted Backup account
// represented by the cached refresh token (contract §12).
type AccountFlags struct {
	Subcommand string
	Args       []string
	Confirm    bool
	Events     string
}

// RunAccount dispatches to the appropriate account subcommand handler.
//
// PR 1 (auth-client) wires the dispatcher only; the `delete` handler
// ships with `add-backup-storage-client` so account purge can coordinate
// with backup deletion in a single change.
func RunAccount(flags AccountFlags) (interface{}, *envelope.Error) {
	switch flags.Subcommand {
	case "delete":
		return nil, envelope.NewError(envelope.ErrInternalError,
			"account delete is not yet implemented in this engine build").
			WithRemediation("Update the engine; this subcommand ships with add-backup-storage-client.")
	case "":
		return nil, envelope.NewError(envelope.ErrInternalError,
			"account requires a subcommand (delete)")
	default:
		return nil, envelope.NewError(envelope.ErrInternalError,
			"unknown account subcommand: "+flags.Subcommand)
	}
}
