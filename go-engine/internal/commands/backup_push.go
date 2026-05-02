// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// PushResult is the data payload for `backup push`.
type PushResult struct {
	BackupID  string `json:"backupId"`
	VersionID string `json:"versionId"`
}

func runBackupPush(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.Profile) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup push requires --profile <path>")
	}

	// PR 2 ships the orchestration scaffolding only. The actual encrypt
	// path goes through internal/backup/upload, which calls
	// crypto.EncryptChunk + crypto.EncryptManifest — both stubs until
	// PROMPT 3. Surface the same documented "crypto not yet implemented"
	// error login uses, so the GUI can present a single consistent
	// message.
	if _, err := crypto.GenerateDEK(); err != nil {
		if errors.Is(err, crypto.ErrNotImplemented) {
			return nil, envelope.NewError(envelope.ErrInternalError,
				"crypto module not yet implemented; backup push orchestration ready, encryption lands in a follow-up change").
				WithDetail(map[string]string{"phase": "encrypt"}).
				WithRemediation("Wait for the engine release that includes the crypto module (PROMPT 3).")
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "backup push: generate DEK: "+err.Error())
	}

	// Unreachable in PR 2 — kept here for the implementer who wires the
	// real path in PROMPT 3.
	return nil, envelope.NewError(envelope.ErrInternalError, "push: unreachable post-stub")
}
