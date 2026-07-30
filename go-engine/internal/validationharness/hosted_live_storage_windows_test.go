// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"os"
	"testing"
)

func TestBindHostedLiveStorageRootRequiresRegisteredAttemptIdentity(t *testing.T) {
	parent := t.TempDir()
	withWindowsLiveTestRunnerTemp(t, parent)
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	root, err := bindHostedLiveStorageRoot(attempt)
	if err != nil {
		t.Fatalf("bindHostedLiveStorageRoot() error = %v", err)
	}
	if _, err := snapshotHostedLiveStorage(root); err != nil {
		t.Fatalf("snapshotHostedLiveStorage() error = %v", err)
	}
	forged := attempt
	forged.nonce[0]++
	if _, err := bindHostedLiveStorageRoot(forged); err == nil {
		t.Fatal("bindHostedLiveStorageRoot() accepted a forged attempt root")
	}
	if err := os.Remove(attempt.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(attempt.path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotHostedLiveStorage(root); err == nil {
		t.Fatal("snapshotHostedLiveStorage() accepted a replaced attempt root")
	}
}
