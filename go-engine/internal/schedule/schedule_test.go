// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := ConfigPath(dir)

	want := &Config{
		SchemaVersion:   "1.0",
		Enabled:         true,
		Manifest:        "/some/manifest.jsonc",
		Interval:        "daily",
		Time:            "08:30",
		AutoPush:        true,
		TaskName:        TaskName,
		Root:            dir,
		PendingArtifact: filepath.Join(dir, "schedule", "pending", "abc.endstate"),
		PendingSHA256:   "abc",
	}
	if err := WriteConfig(path, want); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	got, err := ReadConfig(path)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if got.Enabled != want.Enabled {
		t.Errorf("Enabled = %v, want %v", got.Enabled, want.Enabled)
	}
	if got.Manifest != want.Manifest {
		t.Errorf("Manifest = %q, want %q", got.Manifest, want.Manifest)
	}
	if got.AutoPush != want.AutoPush {
		t.Errorf("AutoPush = %v, want %v", got.AutoPush, want.AutoPush)
	}
	if got.PendingArtifact != want.PendingArtifact || got.PendingSHA256 != want.PendingSHA256 {
		t.Errorf("pending capture = (%q, %q), want (%q, %q)", got.PendingArtifact, got.PendingSHA256, want.PendingArtifact, want.PendingSHA256)
	}
}

func TestReadConfig_MissingFile_ReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := ReadConfig(ConfigPath(dir))
	if err != nil {
		t.Fatalf("ReadConfig on missing file: %v", err)
	}
	if cfg == nil {
		t.Fatal("ReadConfig returned nil, want default")
	}
	if cfg.Enabled {
		t.Error("default config.Enabled = true, want false")
	}
	if cfg.SchemaVersion != "1.0" {
		t.Errorf("default SchemaVersion = %q, want 1.0", cfg.SchemaVersion)
	}
}

func TestReadConfig_MigratesLegacyPendingCaptureIntoOrderedQueue(t *testing.T) {
	dir := t.TempDir()
	path := ConfigPath(dir)
	legacy := `{"schemaVersion":"1.0","pendingArtifact":"C:/pending/first.endstate","pendingSha256":"first"}`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PendingUploads) != 1 || cfg.PendingUploads[0].Artifact != "C:/pending/first.endstate" || cfg.PendingUploads[0].SHA256 != "first" {
		t.Fatalf("PendingUploads = %#v, want migrated legacy capture", cfg.PendingUploads)
	}
}

func TestWriteReadLastRun_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := LastRunPath(dir)

	want := &LastRun{
		SchemaVersion: "1.0",
		RunID:         "schedule-20260710-090000",
		TimestampUTC:  "2026-07-10T09:00:00Z",
		Verify: &LastRunVerify{
			Summary: LastRunVerifySummary{Total: 5, Pass: 3, Fail: 2},
			Drifted: []LastRunDriftItem{
				{ID: "vscode", Name: "Visual Studio Code", Status: "fail", Reason: "missing"},
				{ID: "git", Name: "Git", Status: "fail", Reason: "version_drift"},
			},
		},
	}
	if err := WriteLastRun(path, want); err != nil {
		t.Fatalf("WriteLastRun: %v", err)
	}

	got, err := ReadLastRun(path)
	if err != nil {
		t.Fatalf("ReadLastRun: %v", err)
	}
	if got == nil {
		t.Fatal("ReadLastRun returned nil")
	}
	if got.RunID != want.RunID {
		t.Errorf("RunID = %q, want %q", got.RunID, want.RunID)
	}
	if got.Verify.Summary.Fail != 2 {
		t.Errorf("Verify.Summary.Fail = %d, want 2", got.Verify.Summary.Fail)
	}
	if len(got.Verify.Drifted) != 2 {
		t.Errorf("Drifted len = %d, want 2", len(got.Verify.Drifted))
	}
}

func TestReadLastRun_MissingFile_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	lr, err := ReadLastRun(LastRunPath(dir))
	if err != nil {
		t.Fatalf("ReadLastRun on missing file: %v", err)
	}
	if lr != nil {
		t.Errorf("ReadLastRun returned %v, want nil", lr)
	}
}
