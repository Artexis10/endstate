// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// LogoutResult is the data payload for a successful `backup logout` call.
type LogoutResult struct {
	SignedOut bool `json:"signedOut"`
}

func runBackupLogout(flags BackupFlags) (interface{}, *envelope.Error) {
	a := newBackupStack().Auth
	if err := a.Logout(context.Background()); err != nil {
		return nil, err
	}
	return &LogoutResult{SignedOut: true}, nil
}
