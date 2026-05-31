// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/commands"
)

// ifcWriteProfile (re)writes a single-file profile directory.
func ifcWriteProfile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.jsonc"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// countingCreateVersion mirrors the default create-version handler but counts
// POSTs, so a skip (which returns before creating a version) is observable.
func countingCreateVersion(pp *pushPullBackend, counter *int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(counter, 1)
		var body struct {
			ChunkMetadata []struct {
				Index uint32 `json:"index"`
			} `json:"chunkMetadata"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		urls := []map[string]interface{}{
			{"chunkIndex": -1, "presignedUrl": pp.r2.URL + "/r2/manifest", "expiresAt": "2026-05-02T01:00:00Z"},
		}
		for _, c := range body.ChunkMetadata {
			urls = append(urls, map[string]interface{}{
				"chunkIndex":   c.Index,
				"presignedUrl": fmt.Sprintf("%s/r2/%d", pp.r2.URL, c.Index),
				"expiresAt":    "2026-05-02T01:00:00Z",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"versionId": "v-pushed", "uploadUrls": urls})
	}
}

// Unchanged content + --if-changed → no new version is created (skipped).
func TestBackupPush_IfChanged_SkipsUnchanged(t *testing.T) {
	pp := newPushPullBackend(t)
	var creates int32
	pp.createVersionFn = countingCreateVersion(pp, &creates)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	profile := filepath.Join(t.TempDir(), "profile")
	ifcWriteProfile(t, profile, `{"name":"unchanged"}`)

	// First push uploads and stores the manifest (carrying ContentSHA256).
	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1"}); err != nil {
		t.Fatalf("first push: %+v", err)
	}
	// Second push of byte-identical content with --if-changed must skip.
	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1", IfChanged: true})
	if err != nil {
		t.Fatalf("if-changed push: %+v", err)
	}
	res := data.(*commands.PushResult)
	if !res.Skipped {
		t.Errorf("Skipped = false, want true (content unchanged)")
	}
	if got := atomic.LoadInt32(&creates); got != 1 {
		t.Errorf("createVersion called %d times, want 1 (no upload on skip)", got)
	}
}

// Changed content + --if-changed → a new version is uploaded as normal.
func TestBackupPush_IfChanged_UploadsChanged(t *testing.T) {
	pp := newPushPullBackend(t)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	profile := filepath.Join(t.TempDir(), "profile")
	ifcWriteProfile(t, profile, `{"name":"v1"}`)
	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1"}); err != nil {
		t.Fatalf("first push: %+v", err)
	}
	// Mutate the content, then push with --if-changed: must upload.
	ifcWriteProfile(t, profile, `{"name":"v2-different-content"}`)
	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1", IfChanged: true})
	if err != nil {
		t.Fatalf("if-changed push: %+v", err)
	}
	res := data.(*commands.PushResult)
	if res.Skipped {
		t.Errorf("Skipped = true, want false (content changed)")
	}
	if res.VersionID != "v-pushed" {
		t.Errorf("VersionID = %q, want v-pushed", res.VersionID)
	}
}

// --if-changed on a backup with no prior versions → uploads (nothing to dedup).
func TestBackupPush_IfChanged_FirstPushUploads(t *testing.T) {
	pp := newPushPullBackend(t)
	pp.versionsFn = func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"versions": []interface{}{}})
	}
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	profile := filepath.Join(t.TempDir(), "profile")
	ifcWriteProfile(t, profile, `{"name":"first"}`)
	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1", IfChanged: true})
	if err != nil {
		t.Fatalf("if-changed first push: %+v", err)
	}
	res := data.(*commands.PushResult)
	if res.Skipped {
		t.Errorf("Skipped = true, want false (no prior version)")
	}
	if res.VersionID != "v-pushed" {
		t.Errorf("VersionID = %q, want v-pushed", res.VersionID)
	}
}
