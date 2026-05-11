// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package auth orchestrates the Hosted Backup login/logout/refresh/recover
// flows defined in docs/contracts/hosted-backup-contract.md §5–§6.
//
// The crypto operations (Argon2id KDF, DEK wrap/unwrap) live in
// internal/backup/crypto and are STUBS until PROMPT 3 lands. Until then,
// any flow that requires deriving keys (login, signup, recover, push,
// pull) returns a "crypto: not implemented" error and surfaces
// INTERNAL_ERROR to the user. The orchestration code is real; the
// cryptographic primitives are not.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
)

// accessSkewBuffer is how much wall-clock margin we add when deciding
// whether a cached access token is "still good enough to use". Keeps a
// freshly-hydrated token from being judged expired in flight just
// because the substrate clock is a few seconds ahead of ours.
const accessSkewBuffer = 30 * time.Second

// persistedAccess is the on-disk JSON shape of the `endstate-access-{userId}`
// keychain entry. Kept tiny on purpose: any growth here should be a
// versioned migration, not an additive struct field — the keychain has no
// schema-version channel of its own.
type persistedAccess struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SessionStore caches the access + refresh tokens for the current process
// invocation. The refresh token is also persisted to the OS keychain so
// it survives process restarts.
type SessionStore struct {
	mu             sync.Mutex
	userID         string
	email          string
	accessToken    string
	accessExpiry   time.Time
	refreshToken   string
	subscription   string
	keychainClient keychain.Keychain
	refreshFn      refreshFunc

	// lastHydrateErr is the most recent non-ErrNotFound error encountered
	// by HydrateFromCurrent when reading the current-user pointer, or nil
	// if the keychain is healthy. Surfaced to the user via
	// `backup status` so a flaky keychain doesn't read identically to "no
	// session" — the original review-endstate finding on the keychain-fix
	// commit. ErrNotFound at the pointer (i.e. genuinely signed out) is
	// not recorded; only access failures (permissions, locked store, etc.)
	// land here.
	lastHydrateErr error
}

// NewSessionStore constructs a SessionStore backed by the supplied
// keychain. Pass keychain.NewSystem() in production paths.
func NewSessionStore(kc keychain.Keychain) *SessionStore {
	return &SessionStore{keychainClient: kc}
}

// Hydrate loads the persisted refresh token for userID from the keychain
// into the in-memory session. Called by command handlers on startup so
// subsequent calls have a session to act on. Returns keychain.ErrNotFound
// if no refresh token is persisted (i.e. the user is signed out).
//
// Also opportunistically loads the persisted access token + expiry from
// the F4 entry: if present and not yet within accessSkewBuffer of
// expiry, the in-memory accessToken/accessExpiry fields are populated so
// the next request can skip the 401-refresh hop. Missing or expired
// access entries are silently ignored — the refresh path is the fallback.
func (s *SessionStore) Hydrate(userID string) error {
	rt, err := s.keychainClient.Load(keychain.AccountForUser(userID))
	if err != nil {
		return err
	}
	access, accessExp := s.loadCachedAccess(userID)
	s.mu.Lock()
	s.userID = userID
	s.refreshToken = string(rt)
	s.accessToken = access
	s.accessExpiry = accessExp
	s.mu.Unlock()
	return nil
}

// loadCachedAccess reads and validates the F4 access-token entry for the
// given userId. Returns ("", time.Time{}) when the entry is missing,
// unparseable, or within accessSkewBuffer of expiring — in all such
// cases the caller falls through to the refresh path.
func (s *SessionStore) loadCachedAccess(userID string) (string, time.Time) {
	raw, err := s.keychainClient.Load(keychain.AccountForAccessToken(userID))
	if err != nil {
		return "", time.Time{}
	}
	var entry persistedAccess
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", time.Time{}
	}
	if entry.Token == "" || entry.ExpiresAt.IsZero() {
		return "", time.Time{}
	}
	if time.Now().Add(accessSkewBuffer).After(entry.ExpiresAt) {
		return "", time.Time{}
	}
	return entry.Token, entry.ExpiresAt
}

// Persist writes the refresh token to the keychain under the canonical
// account name for the current userID, and records the userID in the
// fixed current-user pointer so a subsequent fresh process can find it
// via HydrateFromCurrent. Called after a successful login, signup,
// recover-finalize, or refresh. Idempotent.
//
// If writing the refresh token succeeds but writing the current-user
// pointer fails, the first error is returned and the pointer is left
// unwritten — the next invocation will report signed-out, which the
// caller can recover from with `endstate backup login`. We do not roll
// back the refresh-token write on pointer failure: it is harmless data
// (encrypted at rest by the OS keychain) and rolling back would risk
// leaving an inconsistent set of three entries on subsequent logins.
func (s *SessionStore) Persist() error {
	s.mu.Lock()
	uid := s.userID
	rt := s.refreshToken
	access := s.accessToken
	accessExp := s.accessExpiry
	s.mu.Unlock()
	if uid == "" || rt == "" {
		return nil
	}
	if err := s.keychainClient.Store(keychain.AccountForUser(uid), []byte(rt)); err != nil {
		return err
	}
	// F4: persist the access token alongside the refresh token when the
	// caller supplied a parsed expiry. With no expiry we can't decide
	// when to evict, so we skip rather than store an entry we can't
	// trust. Callers (login/refresh) that parse the JWT pass a real
	// expiry; pre-F4 callers fall through harmlessly.
	if access != "" && !accessExp.IsZero() {
		entry, mErr := json.Marshal(persistedAccess{Token: access, ExpiresAt: accessExp})
		if mErr != nil {
			return mErr
		}
		if err := s.keychainClient.Store(keychain.AccountForAccessToken(uid), entry); err != nil {
			return err
		}
	}
	return s.keychainClient.Store(keychain.AccountForCurrentUser(), []byte(uid))
}

// HydrateFromCurrent loads the active userID from the fixed current-user
// pointer and then loads the refresh token for that userID into the
// in-memory session. Called by the stack factory before returning to a
// command handler so every command starts hydrated.
//
// Always returns nil so the stack factory does not need to special-case
// signed-out vs broken-keychain. Two distinct outcomes are encoded:
//
//   - keychain.ErrNotFound at the pointer → signed-out; lastHydrateErr
//     is cleared.
//   - any other error at the pointer → keychain access failure;
//     lastHydrateErr is set and `backup status` surfaces it via the
//     KeychainError field. Session stays empty.
//
// A successful pointer read followed by a Hydrate failure is treated as
// signed-out without recording the error — that case is "stale pointer"
// (pointer present, refresh entry absent), rare in practice, and self-
// heals on the next login.
func (s *SessionStore) HydrateFromCurrent() error {
	uidBytes, err := s.keychainClient.Load(keychain.AccountForCurrentUser())
	if err != nil {
		s.mu.Lock()
		if err == keychain.ErrNotFound {
			s.lastHydrateErr = nil
		} else {
			s.lastHydrateErr = err
		}
		s.mu.Unlock()
		return nil
	}
	s.mu.Lock()
	s.lastHydrateErr = nil
	s.mu.Unlock()
	uid := string(uidBytes)
	if uid == "" {
		return nil
	}
	if err := s.Hydrate(uid); err != nil {
		return nil
	}
	return nil
}

// LastHydrateError returns the most recent non-ErrNotFound error from
// HydrateFromCurrent's pointer load, or nil if the last hydration was
// healthy or genuinely signed out. `backup status` reads this to populate
// the StatusResult.KeychainError field so users can tell "no session"
// apart from "keychain is broken".
func (s *SessionStore) LastHydrateError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHydrateErr
}

// Forget clears the in-memory session and removes both the refresh token
// and the cached DEK from the keychain. Idempotent: returns nil even if
// no entries were present. If both deletes fail, the first error is
// returned and the second is silently dropped — local state is what
// matters and we want callers to be able to retry without surprises.
func (s *SessionStore) Forget() error {
	s.mu.Lock()
	uid := s.userID
	s.userID = ""
	s.email = ""
	s.accessToken = ""
	s.accessExpiry = time.Time{}
	s.refreshToken = ""
	s.subscription = ""
	s.mu.Unlock()
	if uid == "" {
		// No userID in memory but the current-user pointer may still be
		// set from a prior process (e.g. user invoked `logout` without
		// any preceding command in this process). Clear it best-effort
		// so a subsequent process doesn't see a stale pointer.
		if err := s.keychainClient.Delete(keychain.AccountForCurrentUser()); err != nil && err != keychain.ErrNotFound {
			return err
		}
		return nil
	}
	var firstErr error
	if err := s.keychainClient.Delete(keychain.AccountForUser(uid)); err != nil && err != keychain.ErrNotFound {
		firstErr = err
	}
	if err := s.keychainClient.Delete(keychain.AccountForDEK(uid)); err != nil && err != keychain.ErrNotFound {
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := s.keychainClient.Delete(keychain.AccountForWrappedDEK(uid)); err != nil && err != keychain.ErrNotFound {
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := s.keychainClient.Delete(keychain.AccountForAccessToken(uid)); err != nil && err != keychain.ErrNotFound {
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := s.keychainClient.Delete(keychain.AccountForCurrentUser()); err != nil && err != keychain.ErrNotFound {
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StoreDEK persists the unwrapped DEK to the keychain under the canonical
// account for the current userId. Returns an error if the session has no
// userId (caller should set tokens first) or the keychain write fails.
//
// The DEK never appears on stdout, in logs, or in error messages. Callers
// SHOULD zero their local copy after StoreDEK returns; the keychain is
// the only place the DEK lives long-term.
func (s *SessionStore) StoreDEK(dek []byte) error {
	s.mu.Lock()
	uid := s.userID
	s.mu.Unlock()
	if uid == "" {
		return errors.New("session: cannot store DEK without a userId; call SetTokens first")
	}
	return s.keychainClient.Store(keychain.AccountForDEK(uid), dek)
}

// LoadDEK reads the unwrapped DEK from the keychain. Returns
// keychain.ErrNotFound if no DEK is persisted (e.g. the user logged out
// or has not signed in since this engine version). The returned slice is
// a fresh copy the caller may zero.
func (s *SessionStore) LoadDEK() ([]byte, error) {
	s.mu.Lock()
	uid := s.userID
	s.mu.Unlock()
	if uid == "" {
		return nil, errors.New("session: cannot load DEK without a userId; hydrate first")
	}
	return s.keychainClient.Load(keychain.AccountForDEK(uid))
}

// ClearDEK removes the DEK keychain entry for the current userId.
// Idempotent: returns nil if the entry was already absent.
func (s *SessionStore) ClearDEK() error {
	s.mu.Lock()
	uid := s.userID
	s.mu.Unlock()
	if uid == "" {
		return nil
	}
	if err := s.keychainClient.Delete(keychain.AccountForDEK(uid)); err != nil && err != keychain.ErrNotFound {
		return err
	}
	return nil
}

// StoreWrappedDEK persists the masterKey-wrapped DEK (60 bytes, supplied
// as a base64 string from substrate's signup / login / recover-finalize
// responses) so subsequent push calls can populate the manifest's
// `wrappedDEK` field per contract §3 without rederiving the masterKey.
//
// Stored as raw bytes in the keychain; the base64 encoding is restored
// in LoadWrappedDEK so callers see the same string substrate returned.
func (s *SessionStore) StoreWrappedDEK(b64 string) error {
	s.mu.Lock()
	uid := s.userID
	s.mu.Unlock()
	if uid == "" {
		return errors.New("session: cannot store wrappedDEK without a userId; call SetTokens first")
	}
	return s.keychainClient.Store(keychain.AccountForWrappedDEK(uid), []byte(b64))
}

// LoadWrappedDEK reads the cached wrappedDEK for the current userId,
// returning the same base64 string previously passed to StoreWrappedDEK.
// Returns keychain.ErrNotFound if no entry is present.
func (s *SessionStore) LoadWrappedDEK() (string, error) {
	s.mu.Lock()
	uid := s.userID
	s.mu.Unlock()
	if uid == "" {
		return "", errors.New("session: cannot load wrappedDEK without a userId; hydrate first")
	}
	b, err := s.keychainClient.Load(keychain.AccountForWrappedDEK(uid))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SetTokens updates the cached access + refresh tokens and the subscription
// hint. Called after every successful login/refresh response.
func (s *SessionStore) SetTokens(userID, email, access, refresh, subscription string, accessExpiry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userID = userID
	s.email = email
	s.accessToken = access
	s.accessExpiry = accessExpiry
	s.refreshToken = refresh
	s.subscription = subscription
}

// Snapshot returns a read-only view of the session state.
type Snapshot struct {
	UserID             string
	Email              string
	AccessToken        string
	AccessExpiry       time.Time
	RefreshToken       string
	SubscriptionStatus string
}

// Snapshot returns a copy of the current session state. Nil-safe for the
// "not signed in" case; the returned Snapshot has empty fields.
func (s *SessionStore) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{
		UserID:             s.userID,
		Email:              s.email,
		AccessToken:        s.accessToken,
		AccessExpiry:       s.accessExpiry,
		RefreshToken:       s.refreshToken,
		SubscriptionStatus: s.subscription,
	}
}

// SignedIn reports whether the store currently has a refresh token. A
// stale or expired access token still counts as signed in — the next
// request will refresh.
func (s *SessionStore) SignedIn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshToken != ""
}

// AccessToken implements client.TokenProvider.
//
// F4: when the cached expiry is non-zero and within accessSkewBuffer of
// expiring, return "" so the client.go 401-refresh hook fires on the
// next request instead of sending a doomed bearer. When the expiry is
// zero ("unknown") we trust the cached token — keeps pre-F4 callers
// (and the recovery-finalize bearer, which has no parsed exp) working
// unchanged.
func (s *SessionStore) AccessToken(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.accessExpiry.IsZero() && time.Now().Add(accessSkewBuffer).After(s.accessExpiry) {
		return "", nil
	}
	return s.accessToken, nil
}

// RefreshAccessToken implements client.TokenProvider. Invoked by the
// client wrapper after a 401 to refresh the access token using the cached
// refresh token. The actual HTTP call lives on the high-level Authenticator
// (added in subsequent commits) — this default returns the cached value
// to keep the wiring compilable; an Authenticator implementation provided
// to a SessionStore via WithRefreshFn replaces it.
func (s *SessionStore) RefreshAccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	fn := s.refreshFn
	s.mu.Unlock()
	if fn == nil {
		return s.AccessToken(ctx)
	}
	return fn(ctx)
}

// refreshFn is set by SessionStore.WithRefreshFn so the high-level
// Authenticator can wire its substrate /api/auth/refresh call.
type refreshFunc func(ctx context.Context) (string, error)

// WithRefreshFn installs the refresh callback. Returns the receiver for
// chaining.
func (s *SessionStore) WithRefreshFn(fn refreshFunc) *SessionStore {
	s.mu.Lock()
	s.refreshFn = fn
	s.mu.Unlock()
	return s
}

func (s *SessionStore) refreshFnSlot() refreshFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshFn
}

// Issuer is the canonical name of the configured backend, surfaced in
// status output. Stored next to the session so command handlers don't
// need to plumb it separately.
type Issuer struct {
	URL      string
	Audience string
}

// IssuerFromOIDC builds an Issuer from an oidc.Client.
func IssuerFromOIDC(c *oidc.Client, audience string) Issuer {
	return Issuer{URL: c.IssuerURL(), Audience: audience}
}
