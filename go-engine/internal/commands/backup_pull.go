// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/download"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// PullResult is the data payload for `backup pull`.
type PullResult struct {
	BackupID  string `json:"backupId"`
	VersionID string `json:"versionId"`
	WrittenTo string `json:"writtenTo"`
}

func runBackupPull(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.BackupID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup pull requires --backup-id <id>")
	}
	if strings.TrimSpace(flags.To) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup pull requires --to <path>")
	}

	st := newBackupStack()
	em := events.NewEmitter(envelope.BuildRunID("backup-pull", time.Now().UTC()), flags.Events == "jsonl")

	res, envErr := download.PullVersion(context.Background(), download.Dependencies{
		Storage: st.Storage,
		Session: st.Session,
		Events:  em,
	}, flags.BackupID, flags.VersionID, flags.To, flags.Overwrite)
	if envErr != nil {
		return nil, envErr
	}
	return &PullResult{
		BackupID:  res.BackupID,
		VersionID: res.VersionID,
		WrittenTo: res.WrittenTo,
	}, nil
}
