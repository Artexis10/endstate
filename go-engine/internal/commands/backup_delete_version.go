// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// DeleteVersionResult is the data payload for `backup delete-version`.
type DeleteVersionResult struct {
	BackupID  string `json:"backupId"`
	VersionID string `json:"versionId"`
	Deleted   bool   `json:"deleted"`
}

func runBackupDeleteVersion(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.BackupID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup delete-version requires --backup-id <id>")
	}
	if strings.TrimSpace(flags.VersionID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup delete-version requires --version-id <id>")
	}
	if !flags.Confirm {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup delete-version requires --confirm to acknowledge the destructive action").
			WithRemediation("Re-run with --confirm if you really mean to delete this version.")
	}
	st := newBackupStack()
	if err := st.Storage.DeleteVersion(context.Background(), flags.BackupID, flags.VersionID); err != nil {
		return nil, err
	}
	return &DeleteVersionResult{BackupID: flags.BackupID, VersionID: flags.VersionID, Deleted: true}, nil
}
