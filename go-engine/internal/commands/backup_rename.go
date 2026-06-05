// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// RenameResult is the data payload for `backup rename`. It echoes the backup's
// id (the durable identity, unchanged) and its new label.
type RenameResult struct {
	BackupID  string `json:"backupId"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// runBackupRename changes a backup's display label by id. Identity is the
// backend id; only the human label moves. Today it sets `name`; it is the
// GUI's entry point for editing backup metadata.
func runBackupRename(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.BackupID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup rename requires --backup-id <id>")
	}
	name := strings.TrimSpace(flags.Name)
	if name == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup rename requires a non-empty --name <label>")
	}
	st := newBackupStack()
	updated, err := st.Storage.UpdateBackup(context.Background(), flags.BackupID, name)
	if err != nil {
		return nil, err
	}
	// The backend echoes the persisted row; report it verbatim (source of truth).
	return &RenameResult{
		BackupID:  updated.ID,
		Name:      updated.Name,
		UpdatedAt: updated.UpdatedAt,
	}, nil
}
