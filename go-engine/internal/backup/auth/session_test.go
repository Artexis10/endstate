// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
)

// TestSessionStore_PersistThenHydrateAcrossInstances locks the cross-process
// invariant: a SessionStore that has been Persisted on one instance is
// re-discoverable from a fresh instance backed by the same keychain. This
// is the direct failure mode of the original orchestration smoke test on
// production substrate (signup persisted entries keyed by userID, but no
// later process knew the userID, so SignedIn always returned false).
//
// Memory keychain is sufficient: the production keychain has the same
// Store/Load/Delete contract.
func TestSessionStore_PersistThenHydrateAcrossInstances(t *testing.T) {
	kc := keychain.NewMemory()

	storeA := auth.NewSessionStore(kc)
	storeA.SetTokens("user-1", "user@example.com", "access-1", "refresh-1", "active", time.Time{})
	if err := storeA.Persist(); err != nil {
		t.Fatalf("storeA.Persist: %v", err)
	}

	// Fresh store sharing only the keychain — simulates a new CLI process.
	storeB := auth.NewSessionStore(kc)
	if storeB.SignedIn() {
		t.Fatal("storeB starts signed-in before hydration; expected empty")
	}
	if err := storeB.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent: %v", err)
	}
	if !storeB.SignedIn() {
		t.Fatal("storeB.SignedIn() == false after HydrateFromCurrent; the current-user pointer was not set or not resolved")
	}
	snap := storeB.Snapshot()
	if snap.UserID != "user-1" {
		t.Errorf("hydrated UserID = %q, want user-1", snap.UserID)
	}
	if snap.RefreshToken != "refresh-1" {
		t.Errorf("hydrated RefreshToken = %q, want refresh-1", snap.RefreshToken)
	}
}

// TestSessionStore_HydrateFromCurrent_NoEntryIsSignedOut verifies that a
// fresh OS user with no Endstate entries hydrates to a signed-out state
// without surfacing an error — keychain.ErrNotFound at the pointer
// lookup is the signed-out signal, not a failure.
func TestSessionStore_HydrateFromCurrent_NoEntryIsSignedOut(t *testing.T) {
	kc := keychain.NewMemory()
	store := auth.NewSessionStore(kc)
	if err := store.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent on empty keychain returned %v; want nil", err)
	}
	if store.SignedIn() {
		t.Fatal("empty-keychain hydrate left store SignedIn=true")
	}
}

// TestSessionStore_ForgetClearsCurrentUserPointer ensures Logout/Forget
// removes the current-user pointer so a subsequent process correctly
// reports signed-out.
func TestSessionStore_ForgetClearsCurrentUserPointer(t *testing.T) {
	kc := keychain.NewMemory()
	store := auth.NewSessionStore(kc)
	store.SetTokens("user-1", "u@e.com", "a", "r", "active", time.Time{})
	if err := store.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if _, err := kc.Load(keychain.AccountForCurrentUser()); err != nil {
		t.Fatalf("expected current-user pointer present after Persist, got %v", err)
	}
	if err := store.Forget(); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := kc.Load(keychain.AccountForCurrentUser()); err != keychain.ErrNotFound {
		t.Fatalf("current-user pointer = %v after Forget; want ErrNotFound", err)
	}
}
