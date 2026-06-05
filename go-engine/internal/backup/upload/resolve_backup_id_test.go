// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package upload

import (
	"context"
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// fakeResolverStore is a minimal in-memory backupResolverStore for unit-testing
// resolveBackupID without a live backend.
type fakeResolverStore struct {
	backups      []storage.Backup
	createCalls  int
	lastCreated  string
	listCalls    int
	newID        string
}

func (f *fakeResolverStore) ListBackups(ctx context.Context) ([]storage.Backup, *envelope.Error) {
	f.listCalls++
	return f.backups, nil
}

func (f *fakeResolverStore) CreateBackup(ctx context.Context, name string) (string, *envelope.Error) {
	f.createCalls++
	f.lastCreated = name
	id := f.newID
	if id == "" {
		id = "new-backup-id"
	}
	return id, nil
}

// A named push with no --backup-id must CREATE a distinct backup, even when the
// account already has backups. Previously the engine returned backups[0] and
// silently ignored --name, so per-profile hosting was impossible.
func TestResolveBackupID_NamedPushWithExistingBackupsCreatesNew(t *testing.T) {
	store := &fakeResolverStore{
		backups: []storage.Backup{{ID: "existing-1", Name: "This computer"}},
		newID:   "created-gaming",
	}

	id, err := resolveBackupID(context.Background(), store, "", "gaming-rig", "MACHINE-X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "created-gaming" {
		t.Fatalf("expected a newly created backup id, got %q (did it append to backups[0]?)", id)
	}
	if store.createCalls != 1 {
		t.Fatalf("expected CreateBackup called once, got %d", store.createCalls)
	}
	if store.lastCreated != "gaming-rig" {
		t.Fatalf("expected new backup labeled %q, got %q", "gaming-rig", store.lastCreated)
	}
}

// An explicit --backup-id is honored verbatim, without touching storage.
func TestResolveBackupID_ExplicitIDUsedVerbatim(t *testing.T) {
	store := &fakeResolverStore{backups: []storage.Backup{{ID: "existing-1"}}}

	id, err := resolveBackupID(context.Background(), store, "b-123", "ignored-name", "MACHINE-X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "b-123" {
		t.Fatalf("expected explicit id b-123, got %q", id)
	}
	if store.createCalls != 0 || store.listCalls != 0 {
		t.Fatalf("explicit id must not call storage; create=%d list=%d", store.createCalls, store.listCalls)
	}
}

// No id and no name: legacy convenience — append to the first existing backup.
func TestResolveBackupID_NoNameNoIDAppendsToFirst(t *testing.T) {
	store := &fakeResolverStore{backups: []storage.Backup{{ID: "first"}, {ID: "second"}}}

	id, err := resolveBackupID(context.Background(), store, "", "", "MACHINE-X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "first" {
		t.Fatalf("expected first backup id, got %q", id)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no CreateBackup, got %d", store.createCalls)
	}
}

// No id, no name, no existing backups: create a backup labeled with the
// injected default (the device label), not the literal "default".
func TestResolveBackupID_NoNameNoIDNoBackupsCreatesDeviceLabeled(t *testing.T) {
	store := &fakeResolverStore{backups: nil, newID: "created-default"}

	id, err := resolveBackupID(context.Background(), store, "", "", "DESKTOP-HUGO")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "created-default" {
		t.Fatalf("expected created default id, got %q", id)
	}
	if store.lastCreated != "DESKTOP-HUGO" {
		t.Fatalf("expected the device label as the create name, got %q", store.lastCreated)
	}
}

// deviceLabelFrom trims a present host name and falls back to "default" on
// empty/error — so a missing host name never fails a push.
func TestDeviceLabelFrom(t *testing.T) {
	cases := []struct {
		host string
		err  error
		want string
	}{
		{"HUGO-DESKTOP", nil, "HUGO-DESKTOP"},
		{"  Work-Laptop  ", nil, "Work-Laptop"},
		{"", nil, "default"},
		{"   ", nil, "default"},
		{"ignored", errors.New("hostname unavailable"), "default"},
	}
	for _, c := range cases {
		if got := deviceLabelFrom(c.host, c.err); got != c.want {
			t.Fatalf("deviceLabelFrom(%q, %v) = %q, want %q", c.host, c.err, got, c.want)
		}
	}
}
