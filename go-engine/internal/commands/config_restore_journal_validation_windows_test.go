// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package commands

import (
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestValidationSameRunLegacyJournalsRemainSafe(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "present",
	})
	options := configRestoreExecutionOptions{
		RunID: "apply-same-second", JournalLogsDir: filepath.Join(context.Root(), "logs"),
		ManifestDir: filepath.Join(context.Root(), "manifests"), ValidationContext: context,
	}
	first, err := writeLegacyConfigRestoreJournal(options, []restore.RestoreResult{{
		ID: "first", Target: `%APPDATA%\Endstate\settings.json`, Status: "restored", RestoreType: "copy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := writeLegacyConfigRestoreJournal(options, []restore.RestoreResult{{
		ID: "second", Target: `%APPDATA%\Endstate\settings.json`, Status: "restored", RestoreType: "copy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("collision reused immutable path %q", first)
	}
	for _, journal := range []string{first, second} {
		if err := context.ValidateSandboxPath(journal); err != nil {
			t.Fatalf("published journal %q failed validation: %v", filepath.Base(journal), err)
		}
	}
}
