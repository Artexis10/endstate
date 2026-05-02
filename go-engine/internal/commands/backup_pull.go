// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
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

	// As with push: orchestration is ready in this change but the actual
	// decrypt path is gated on PROMPT 3. Surface the same documented
	// "crypto not yet implemented" message.
	if _, err := crypto.UnwrapDEK(nil, [crypto.MasterKeySize]byte{}); err != nil {
		if errors.Is(err, crypto.ErrNotImplemented) {
			return nil, envelope.NewError(envelope.ErrInternalError,
				"crypto module not yet implemented; backup pull orchestration ready, decryption lands in a follow-up change").
				WithDetail(map[string]string{"phase": "decrypt"}).
				WithRemediation("Wait for the engine release that includes the crypto module (PROMPT 3).")
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: unwrap DEK: "+err.Error())
	}

	return nil, envelope.NewError(envelope.ErrInternalError, "pull: unreachable post-stub")
}
