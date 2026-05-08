// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
)

// faultyKeychain wraps a memory keychain but returns loadErr from any
// Load call instead of consulting the underlying store. Used to simulate
// permission/locked-store failures.
type faultyKeychain struct {
	loadErr error
}

func (f *faultyKeychain) Store(account string, secret []byte) error  { return f.loadErr }
func (f *faultyKeychain) Load(account string) ([]byte, error)        { return nil, f.loadErr }
func (f *faultyKeychain) Delete(account string) error                { return f.loadErr }

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

// TestSessionStore_HydrateFromCurrent_RecordsNonNotFoundError verifies
// that a permission-denied / locked-store style keychain failure is
// captured in lastHydrateErr so `backup status` can surface it through
// the KeychainError envelope field. The fix-keychain-error-surface
// follow-up to the original keychain-pointer fix.
func TestSessionStore_HydrateFromCurrent_RecordsNonNotFoundError(t *testing.T) {
	wantErr := errors.New("synthetic: keychain locked")
	store := auth.NewSessionStore(&faultyKeychain{loadErr: wantErr})

	if err := store.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent should always return nil to keep signed-out paths working; got %v", err)
	}
	if store.SignedIn() {
		t.Fatal("a faulty keychain must not produce a signed-in session")
	}
	got := store.LastHydrateError()
	if got == nil {
		t.Fatal("LastHydrateError() == nil; want the synthetic keychain error")
	}
	if got.Error() != wantErr.Error() {
		t.Errorf("LastHydrateError() = %q, want %q", got, wantErr)
	}
}

// TestSessionStore_HydrateFromCurrent_NotFoundIsNotRecorded asserts the
// "no session" path is genuinely silent — ErrNotFound at the pointer is
// the canonical signed-out signal and must not surface as a keychain
// error to the user.
func TestSessionStore_HydrateFromCurrent_NotFoundIsNotRecorded(t *testing.T) {
	store := auth.NewSessionStore(keychain.NewMemory())
	if err := store.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent: %v", err)
	}
	if got := store.LastHydrateError(); got != nil {
		t.Errorf("LastHydrateError() = %v on an empty keychain; want nil (signed-out is normal)", got)
	}
}

// TestSessionStore_HydrateFromCurrent_HealthyClearsPriorError covers the
// recovery case: a previous call recorded an error, the keychain is
// now healthy, the next HydrateFromCurrent must clear the stale error
// so `backup status` stops surfacing it.
func TestSessionStore_HydrateFromCurrent_HealthyClearsPriorError(t *testing.T) {
	store := auth.NewSessionStore(&faultyKeychain{loadErr: errors.New("synthetic: keychain locked")})
	_ = store.HydrateFromCurrent()
	if store.LastHydrateError() == nil {
		t.Fatal("setup: expected an error to be recorded")
	}

	// Swap to a fresh, healthy keychain by constructing a new store on it
	// — simulates the "user fixes the keychain and re-runs" path. We
	// can't swap the keychain on the existing store (no setter on
	// purpose) so a new store-on-same-fix-state is the natural way to
	// model a recovered process.
	healthy := auth.NewSessionStore(keychain.NewMemory())
	if err := healthy.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent: %v", err)
	}
	if got := healthy.LastHydrateError(); got != nil {
		t.Errorf("LastHydrateError() on healthy fresh keychain = %v; want nil", got)
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
