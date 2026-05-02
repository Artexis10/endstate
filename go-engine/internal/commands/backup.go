// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// newBackupStack is the factory each backup command handler calls to
// build its component stack. Replaceable from tests via
// ReplaceBackupStackFactoryForTest to inject a memory keychain and a
// test-controlled issuer.
var newBackupStack func() *backup.Stack = backup.NewStack

// ReplaceBackupStackFactoryForTest swaps in a test factory and returns
// a cleanup func that restores the previous one.
func ReplaceBackupStackFactoryForTest(f func() *backup.Stack) func() {
	prev := newBackupStack
	newBackupStack = f
	return func() { newBackupStack = prev }
}

// BackupFlags holds the parsed CLI flags for the `endstate backup` command tree.
//
// The command tree is positional: `endstate backup <subcommand> [flags]`.
// `Subcommand` carries which leaf is being invoked; `Args` carries any
// remaining positional arguments. Stdin is the canonical secret-input
// channel — passphrases and recovery keys are never accepted as flags.
type BackupFlags struct {
	Subcommand string
	Args       []string

	// Email is the --email flag (login, recover).
	Email string

	// BackupID identifies an existing backup (versions, push, pull, delete).
	BackupID string

	// VersionID identifies a specific version of a backup (pull, delete-version).
	VersionID string

	// Profile is the --profile flag (push), pointing at the manifest/zip to upload.
	Profile string

	// Name is the --name flag (push), the human-readable label for the backup.
	Name string

	// To is the --to flag (pull), the directory the decrypted profile is
	// written into.
	To string

	// Confirm is the --confirm flag required for destructive operations
	// (delete, delete-version).
	Confirm bool

	// Events controls streaming event output ("jsonl" enables it).
	Events string
}

// RunBackup dispatches to the appropriate backup subcommand handler.
//
// PR 1 (auth-client) wires login, logout, and status. The remaining
// subcommands — list, versions, push, pull, delete, delete-version,
// recover — ship in `add-backup-storage-client`.
func RunBackup(flags BackupFlags) (interface{}, *envelope.Error) {
	switch flags.Subcommand {
	case "login":
		return runBackupLogin(flags)
	case "logout":
		return runBackupLogout(flags)
	case "status":
		return runBackupStatus(flags)
	case "":
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup requires a subcommand (login, logout, status)")
	case "list", "versions", "push", "pull", "delete", "delete-version", "recover":
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup subcommand not yet implemented in this engine build: "+flags.Subcommand).
			WithRemediation("Update the engine; this subcommand ships with add-backup-storage-client.")
	default:
		return nil, envelope.NewError(envelope.ErrInternalError,
			"unknown backup subcommand: "+flags.Subcommand).
			WithRemediation("Run `endstate backup --help` for the supported list.")
	}
}
