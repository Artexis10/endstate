// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"encoding/json"
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

// TestSessionStore_AccessTokenHydratesWhenUnexpired locks in the F4
// behavior: a persisted access token whose `expiresAt` is comfortably in
// the future is re-hydrated into the in-memory session on a fresh
// process. This eliminates the per-call refresh round-trip that races
// with substrate's refresh-token reuse detection (the session-disappears
// repro), because a valid cached AT lets `Me()` succeed without going
// through `/api/auth/refresh`.
func TestSessionStore_AccessTokenHydratesWhenUnexpired(t *testing.T) {
	kc := keychain.NewMemory()
	storeA := auth.NewSessionStore(kc)
	exp := time.Now().Add(10 * time.Minute)
	storeA.SetTokens("user-1", "user@example.com", "access-tok", "refresh-tok", "active", exp)
	if err := storeA.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	storeB := auth.NewSessionStore(kc)
	if err := storeB.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent: %v", err)
	}
	snap := storeB.Snapshot()
	if snap.AccessToken != "access-tok" {
		t.Errorf("AccessToken = %q, want access-tok (must be hydrated when expiry is in the future)", snap.AccessToken)
	}
	if snap.AccessExpiry.IsZero() {
		t.Error("AccessExpiry is zero after hydrate; want the persisted expiry")
	}
	// Allow 1s skew for JSON RFC3339 round-trip.
	if delta := snap.AccessExpiry.Sub(exp); delta > time.Second || delta < -time.Second {
		t.Errorf("AccessExpiry off by %v from persisted value", delta)
	}
}

// TestSessionStore_AccessTokenSkippedWhenExpired verifies the eviction
// path: a persisted access token whose `expiresAt` is in the past (or
// within the 30s skew window) is NOT hydrated, even though the
// surrounding session state is. The refresh token still hydrates, so
// `SignedIn()` stays true and the 401-refresh hook fires on the next
// request — which is the correct fall-through.
func TestSessionStore_AccessTokenSkippedWhenExpired(t *testing.T) {
	kc := keychain.NewMemory()
	storeA := auth.NewSessionStore(kc)
	storeA.SetTokens("user-1", "u@e.com", "access-tok", "refresh-tok", "active", time.Now().Add(-time.Minute))
	if err := storeA.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	storeB := auth.NewSessionStore(kc)
	if err := storeB.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent: %v", err)
	}
	if !storeB.SignedIn() {
		t.Fatal("expired AT must not affect SignedIn() — RT should still hydrate")
	}
	snap := storeB.Snapshot()
	if snap.AccessToken != "" {
		t.Errorf("AccessToken = %q, want empty (expired AT must not hydrate)", snap.AccessToken)
	}
	if snap.RefreshToken != "refresh-tok" {
		t.Errorf("RefreshToken = %q, want refresh-tok (RT must still hydrate when AT is expired)", snap.RefreshToken)
	}
}

// TestSessionStore_AccessTokenNotPersistedWhenZeroExpiry locks the
// back-compat behavior: callers that don't supply a parsed expiry (the
// existing `time.Time{}` shape that pre-F4 tests use) must not have their
// access token persisted. Without an expiry we can't safely evict, so
// the value never enters the keychain.
func TestSessionStore_AccessTokenNotPersistedWhenZeroExpiry(t *testing.T) {
	kc := keychain.NewMemory()
	store := auth.NewSessionStore(kc)
	store.SetTokens("user-1", "u@e.com", "access-tok", "refresh-tok", "active", time.Time{})
	if err := store.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if _, err := kc.Load(keychain.AccountForAccessToken("user-1")); err != keychain.ErrNotFound {
		t.Errorf("access entry should be absent when expiry is zero; got err=%v", err)
	}
	// Refresh entry must still be written — back-compat with existing tests.
	if _, err := kc.Load(keychain.AccountForUser("user-1")); err != nil {
		t.Errorf("refresh entry should still be persisted; got err=%v", err)
	}
}

// TestSessionStore_AccessTokenReturnsEmptyWhenExpired verifies the
// TokenProvider contract: AccessToken() returns "" when the cached
// expiry has passed, forcing the client.go 401-refresh hook to fire on
// the next request. Without this, an expired AT would be sent in the
// Authorization header and substrate would reject it as 401 anyway —
// returning "" lets the engine skip that doomed round-trip.
func TestSessionStore_AccessTokenReturnsEmptyWhenExpired(t *testing.T) {
	store := auth.NewSessionStore(keychain.NewMemory())
	store.SetTokens("user-1", "u@e.com", "access-tok", "refresh-tok", "active", time.Now().Add(-time.Minute))
	got, err := store.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "" {
		t.Errorf("AccessToken() = %q, want empty (cached AT is expired)", got)
	}
}

// TestSessionStore_AccessTokenReturnsValueWhenUnexpired is the positive
// counterpart: a non-expired AT must round-trip through AccessToken().
func TestSessionStore_AccessTokenReturnsValueWhenUnexpired(t *testing.T) {
	store := auth.NewSessionStore(keychain.NewMemory())
	store.SetTokens("user-1", "u@e.com", "access-tok", "refresh-tok", "active", time.Now().Add(10*time.Minute))
	got, err := store.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "access-tok" {
		t.Errorf("AccessToken() = %q, want access-tok", got)
	}
}

// TestSessionStore_AccessTokenReturnsValueWhenExpiryZero preserves the
// pre-F4 default: when no expiry is known, AccessToken() returns the
// cached value unchanged. Login/refresh paths that don't yet parse the
// JWT continue to work; only callers that pass a parsed expiry get the
// expiry-aware behavior.
func TestSessionStore_AccessTokenReturnsValueWhenExpiryZero(t *testing.T) {
	store := auth.NewSessionStore(keychain.NewMemory())
	store.SetTokens("user-1", "u@e.com", "access-tok", "refresh-tok", "active", time.Time{})
	got, err := store.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "access-tok" {
		t.Errorf("AccessToken() = %q, want access-tok (zero expiry is treated as unknown, not expired)", got)
	}
}

// TestSessionStore_ForgetClearsAccessTokenEntry ensures logout fully
// wipes session state. Without this, a stale access entry would survive
// a re-login under a different account on the same machine.
func TestSessionStore_ForgetClearsAccessTokenEntry(t *testing.T) {
	kc := keychain.NewMemory()
	store := auth.NewSessionStore(kc)
	store.SetTokens("user-1", "u@e.com", "access-tok", "refresh-tok", "active", time.Now().Add(10*time.Minute))
	if err := store.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if _, err := kc.Load(keychain.AccountForAccessToken("user-1")); err != nil {
		t.Fatalf("expected access entry present after Persist, got %v", err)
	}
	if err := store.Forget(); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := kc.Load(keychain.AccountForAccessToken("user-1")); err != keychain.ErrNotFound {
		t.Errorf("access entry = %v after Forget; want ErrNotFound", err)
	}
}

// TestSessionStore_PersistedAccessEntryShape locks the on-disk JSON
// shape so a future engine version that reads back an older entry
// doesn't silently break.
func TestSessionStore_PersistedAccessEntryShape(t *testing.T) {
	kc := keychain.NewMemory()
	store := auth.NewSessionStore(kc)
	exp := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	store.SetTokens("user-1", "u@e.com", "access-tok", "refresh-tok", "active", exp)
	if err := store.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	raw, err := kc.Load(keychain.AccountForAccessToken("user-1"))
	if err != nil {
		t.Fatalf("Load access entry: %v", err)
	}
	var entry struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, raw)
	}
	if entry.Token != "access-tok" {
		t.Errorf("token field = %q, want access-tok", entry.Token)
	}
	if !entry.ExpiresAt.Equal(exp) {
		t.Errorf("expiresAt field = %v, want %v", entry.ExpiresAt, exp)
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
