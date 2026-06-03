// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/commands"
)

// `backup estimate` reports the would-be upload size of a profile by running
// the identical client-side bundling path push uses (shared buildBundle), so
// the result must exceed the plaintext by the AES-256-GCM overhead and include
// the encrypted manifest. No network call is made.
func TestBackupEstimate_ReportsBundleSize(t *testing.T) {
	pp := newPushPullBackend(t)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"name":"estimate-me","apps":["a","b","c"]}`)
	if err := os.WriteFile(filepath.Join(profile, "manifest.jsonc"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "estimate", Profile: profile})
	if err != nil {
		t.Fatalf("estimate: %+v", err)
	}
	res := data.(*commands.EstimateResult)

	if res.ChunkCount != 1 {
		t.Errorf("ChunkCount = %d, want 1 (tiny profile fits one 4 MiB chunk)", res.ChunkCount)
	}
	// PlaintextBytes is the tar size — tar wraps the file in a 512-byte header
	// plus padding, so it strictly exceeds the raw file content.
	if res.PlaintextBytes <= int64(len(content)) {
		t.Errorf("PlaintextBytes = %d, want > raw content (%d)", res.PlaintextBytes, len(content))
	}
	// Upload = chunk ciphertext (plaintext + 12-byte nonce + 16-byte tag) +
	// encrypted manifest. So it must exceed plaintext by more than one chunk's
	// 28-byte AEAD overhead alone (the manifest adds the rest).
	if res.EstimatedUploadBytes <= res.PlaintextBytes+28 {
		t.Errorf("EstimatedUploadBytes = %d, want > PlaintextBytes(%d)+28", res.EstimatedUploadBytes, res.PlaintextBytes)
	}
}

// Empty --profile is rejected before any work is done.
func TestBackupEstimate_RequiresProfile(t *testing.T) {
	pp := newPushPullBackend(t)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "estimate", Profile: ""}); err == nil {
		t.Fatal("estimate with empty --profile: want error, got nil")
	}
}
