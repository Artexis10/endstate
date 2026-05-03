// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
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

	// The crypto module is real (PROMPT 3 onward); the chunked-download
	// orchestration on top of it is the next slice. Once that lands, the
	// flow is: request download URLs → download chunks → SHA-256 verify →
	// crypto.DecryptManifest + crypto.DecryptChunk → write to flags.To.
	return nil, envelope.NewError(envelope.ErrInternalError, "pull: post-crypto orchestration not yet implemented").
		WithRemediation("Wait for the engine release that wires the chunked-download orchestration on top of the crypto module.")
}
