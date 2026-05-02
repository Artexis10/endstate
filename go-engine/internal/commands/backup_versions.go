// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// VersionsResult is the data payload for `backup versions`. Mirrors
// substrate's GET /api/backups/:id/versions response.
type VersionsResult struct {
	BackupID string                 `json:"backupId"`
	Versions []storage.VersionInfo  `json:"versions"`
}

func runBackupVersions(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.BackupID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup versions requires --backup-id <id>")
	}
	st := newBackupStack()
	versions, err := st.Storage.ListVersions(context.Background(), flags.BackupID)
	if err != nil {
		return nil, err
	}
	return &VersionsResult{BackupID: flags.BackupID, Versions: versions}, nil
}
