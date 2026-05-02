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
	"sync"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
)

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
func (s *SessionStore) Hydrate(userID string) error {
	rt, err := s.keychainClient.Load(keychain.AccountForUser(userID))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.userID = userID
	s.refreshToken = string(rt)
	s.mu.Unlock()
	return nil
}

// Persist writes the refresh token to the keychain under the canonical
// account name for the current userID. Called after a successful login
// or refresh. Idempotent.
func (s *SessionStore) Persist() error {
	s.mu.Lock()
	uid := s.userID
	rt := s.refreshToken
	s.mu.Unlock()
	if uid == "" || rt == "" {
		return nil
	}
	return s.keychainClient.Store(keychain.AccountForUser(uid), []byte(rt))
}

// Forget clears the in-memory session and removes the refresh token from
// the keychain. Idempotent: returns nil even if no entry was present.
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
		return nil
	}
	if err := s.keychainClient.Delete(keychain.AccountForUser(uid)); err != nil {
		if err == keychain.ErrNotFound {
			return nil
		}
		return err
	}
	return nil
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
// If the cached access token is expired (or expires within 30s), the
// caller is expected to invoke a refresh before this returns. For the
// integration scaffold we return whatever we have; the client package's
// 401-refresh hook handles the rotation when needed.
func (s *SessionStore) AccessToken(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
