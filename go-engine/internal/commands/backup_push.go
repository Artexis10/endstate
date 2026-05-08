// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/upload"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// PushResult is the data payload for `backup push`.
type PushResult struct {
	BackupID  string `json:"backupId"`
	VersionID string `json:"versionId"`
}

func runBackupPush(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.Profile) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup push requires --profile <path>").
			WithRemediation("Pass --profile <path> pointing at the file or directory you want to back up.")
	}

	st := newBackupStack()
	em := events.NewEmitter(envelope.BuildRunID("backup-push", time.Now().UTC()), flags.Events == "jsonl")

	res, envErr := upload.PushVersion(context.Background(), upload.Dependencies{
		Storage: st.Storage,
		Session: st.Session,
		Events:  em,
	}, flags.BackupID, flags.Profile, flags.Name)
	if envErr != nil {
		return nil, envErr
	}
	return &PushResult{BackupID: res.BackupID, VersionID: res.VersionID}, nil
}
