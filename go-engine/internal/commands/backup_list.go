// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// ListResult is the data payload for `backup list`. Field shape mirrors
// substrate's GET /api/backups response (contract §7) verbatim — no
// client-side renaming, per the locked plan envelope shape.
type ListResult struct {
	Backups []storage.Backup `json:"backups"`
}

func runBackupList(flags BackupFlags) (interface{}, *envelope.Error) {
	st := newBackupStack()
	backups, err := st.Storage.ListBackups(context.Background())
	if err != nil {
		return nil, err
	}
	return &ListResult{Backups: backups}, nil
}
