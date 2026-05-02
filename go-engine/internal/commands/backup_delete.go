// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// DeleteResult is the data payload for `backup delete`.
type DeleteResult struct {
	BackupID string `json:"backupId"`
	Deleted  bool   `json:"deleted"`
}

func runBackupDelete(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.BackupID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup delete requires --backup-id <id>")
	}
	if !flags.Confirm {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup delete requires --confirm to acknowledge that this permanently destroys all versions of the backup").
			WithRemediation("Re-run with --confirm if you really mean to delete this backup.")
	}
	st := newBackupStack()
	if err := st.Storage.DeleteBackup(context.Background(), flags.BackupID); err != nil {
		return nil, err
	}
	return &DeleteResult{BackupID: flags.BackupID, Deleted: true}, nil
}
