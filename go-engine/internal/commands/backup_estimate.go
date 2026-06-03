// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/upload"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// EstimateResult is the data payload for `backup estimate`.
//
// It reports the exact number of bytes a `backup push` of the same profile
// would upload — computed entirely client-side with no network I/O — so the
// GUI can warn before a push that would approach or exceed the hosted-backup
// quota. Read-only: emits no events and creates no version.
//
//	{
//	  "estimatedUploadBytes": number,  // encrypted chunks + encrypted manifest
//	  "plaintextBytes":       number,  // tarred profile size before encryption
//	  "chunkCount":           number
//	}
type EstimateResult struct {
	EstimatedUploadBytes int64 `json:"estimatedUploadBytes"`
	PlaintextBytes       int64 `json:"plaintextBytes"`
	ChunkCount           int   `json:"chunkCount"`
}

func runBackupEstimate(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.Profile) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup estimate requires --profile <path>").
			WithRemediation("Pass --profile <path> pointing at the file or directory you want to size.")
	}

	st := newBackupStack()
	est, envErr := upload.EstimateSize(upload.Dependencies{
		Storage: st.Storage,
		Session: st.Session,
	}, flags.Profile)
	if envErr != nil {
		return nil, envErr
	}
	return &EstimateResult{
		EstimatedUploadBytes: est.EstimatedUploadBytes,
		PlaintextBytes:       est.PlaintextBytes,
		ChunkCount:           est.ChunkCount,
	}, nil
}
