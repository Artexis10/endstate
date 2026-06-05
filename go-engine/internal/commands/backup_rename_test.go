// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
)

// `backup rename` requires both an id (which backup) and a non-empty name
// (the new label). Both guards fire before any backend call, so they need no
// stack. The happy path (PATCH round-trip) is covered by the substrate route
// tests and the GUI live verification.
func TestBackupRename_RequiresBackupID(t *testing.T) {
	if _, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "rename",
		Name:       "Gaming Rig",
	}); err == nil {
		t.Fatal("expected an error when --backup-id is missing")
	}
}

func TestBackupRename_RequiresName(t *testing.T) {
	if _, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "rename",
		BackupID:   "b-123",
		Name:       "   ",
	}); err == nil {
		t.Fatal("expected an error when --name is blank")
	}
}
