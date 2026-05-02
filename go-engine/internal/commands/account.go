// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

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
func RunAccount(flags AccountFlags) (interface{}, *envelope.Error) {
	switch flags.Subcommand {
	case "delete":
		return runAccountDelete(flags)
	case "":
		return nil, envelope.NewError(envelope.ErrInternalError,
			"account requires a subcommand (delete)")
	default:
		return nil, envelope.NewError(envelope.ErrInternalError,
			"unknown account subcommand: "+flags.Subcommand)
	}
}

// AccountDeleteResult is the data payload for `account delete`.
type AccountDeleteResult struct {
	Deleted bool `json:"deleted"`
}

func runAccountDelete(flags AccountFlags) (interface{}, *envelope.Error) {
	if !flags.Confirm {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"account delete requires --confirm to acknowledge that this destroys your account, subscription, and all backed-up data permanently").
			WithRemediation("Re-run with --confirm if you really mean to delete your account.")
	}
	st := newBackupStack()

	// Hard delete on the backend first; clearing local state is a
	// follow-up best-effort cleanup. The order matters: if we cleared
	// local state first and the backend call failed, we'd lose the
	// session needed to authenticate the delete.
	if err := st.Storage.DeleteAccount(context.Background()); err != nil {
		return nil, err
	}
	if err := st.Auth.Session().Forget(); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"account delete: backend purge succeeded but local session could not be cleared: "+err.Error())
	}
	return &AccountDeleteResult{Deleted: true}, nil
}
