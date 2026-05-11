// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// fakeBackend serves discovery + auth + account endpoints for the
// Authenticator integration tests. Each handler can be overridden per
// test.
type fakeBackend struct {
	srv               *httptest.Server
	loginPreFn        http.HandlerFunc
	loginCompleteFn   http.HandlerFunc
	refreshFn         http.HandlerFunc
	logoutFn          http.HandlerFunc
	meFn              http.HandlerFunc
	loginPreHits      int32
	loginCompleteHits int32
	refreshHits       int32
	logoutHits        int32
	meHits            int32
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	fb := &fakeBackend{}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	fb.srv = srv
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(oidc.Document{
			Issuer:                            srv.URL,
			JWKSURI:                           srv.URL + "/api/.well-known/jwks.json",
			IDTokenSigningAlgValuesSupported:  []string{"EdDSA"},
			EndstateExtensions: oidc.EndstateExtensions{
				AuthSignupEndpoint:        srv.URL + "/api/auth/signup",
				AuthLoginEndpoint:         srv.URL + "/api/auth/login",
				AuthRefreshEndpoint:       srv.URL + "/api/auth/refresh",
				AuthLogoutEndpoint:        srv.URL + "/api/auth/logout",
				AuthRecoverEndpoint:       srv.URL + "/api/auth/recover",
				BackupAPIBase:             srv.URL + "/api/backups",
				SupportedKDFAlgorithms:    []string{"argon2id"},
				SupportedEnvelopeVersions: []int{1},
				MinKDFParams:              oidc.MinKDFParams{Memory: 65536, Iterations: 3, Parallelism: 4},
			},
		})
	})
	mux.HandleFunc("/api/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(oidc.JWKS{Keys: []oidc.JWK{}})
	})
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		// Substrate distinguishes pre-handshake from complete via body shape.
		var raw map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if _, hasPwd := raw["serverPassword"]; hasPwd {
			atomic.AddInt32(&fb.loginCompleteHits, 1)
			if fb.loginCompleteFn != nil {
				fb.loginCompleteFn(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"userId":             "user-1",
				"accessToken":        "access-1",
				"refreshToken":       "refresh-1",
				"wrappedDEK":         "AAAA",
				"subscriptionStatus": "active",
			})
			return
		}
		atomic.AddInt32(&fb.loginPreHits, 1)
		if fb.loginPreFn != nil {
			fb.loginPreFn(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"salt": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"kdfParams": map[string]interface{}{
				"algorithm":   "argon2id",
				"memory":      65536,
				"iterations":  3,
				"parallelism": 4,
			},
		})
	})
	mux.HandleFunc("/api/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		atomic.AddInt32(&fb.refreshHits, 1)
		if fb.refreshFn != nil {
			fb.refreshFn(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  "access-2",
			"refreshToken": "refresh-2",
		})
	})
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		atomic.AddInt32(&fb.logoutHits, 1)
		if fb.logoutFn != nil {
			fb.logoutFn(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/account/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		atomic.AddInt32(&fb.meHits, 1)
		if fb.meFn != nil {
			fb.meFn(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"userId":             "user-1",
			"email":              "user@example.com",
			"subscriptionStatus": "active",
			"createdAt":          "2026-05-02T00:00:00Z",
		})
	})
	return fb
}

func (fb *fakeBackend) URL() string { return fb.srv.URL }

// newAuthForTest wires a fresh Authenticator + memory keychain bound to
// the supplied fakeBackend. Returns the authenticator and the underlying
// keychain so the caller can assert keychain state.
func newAuthForTest(t *testing.T, fb *fakeBackend) (*auth.Authenticator, keychain.Keychain) {
	t.Helper()
	kc := keychain.NewMemory()
	store := auth.NewSessionStore(kc)
	oc := oidc.NewClient(fb.URL(), fb.srv.Client())
	rp := client.RetryPolicy{MaxRetries: 1, InitialWait: time.Millisecond, Multiplier: 1, MaxWait: 5 * time.Millisecond}
	hc := client.New(client.Options{
		Tokens: store,
		Retry:  &rp,
	})
	a := auth.NewAuthenticator(auth.IssuerFromOIDC(oc, "endstate-backup"), oc, hc, store)
	return a, kc
}

func TestAuthenticator_PreHandshake_HappyPath(t *testing.T) {
	fb := newFakeBackend(t)
	a, _ := newAuthForTest(t, fb)
	resp, err := a.PreHandshake(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("PreHandshake: %+v", err)
	}
	if resp.KDFParams.Memory != 65536 {
		t.Errorf("Memory = %d, want 65536", resp.KDFParams.Memory)
	}
}

func TestAuthenticator_PreHandshake_UnreachableMappedToBackendUnreachable(t *testing.T) {
	kc := keychain.NewMemory()
	store := auth.NewSessionStore(kc)
	oc := oidc.NewClient("http://127.0.0.1:1", &http.Client{Timeout: 100 * time.Millisecond})
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, MaxWait: time.Millisecond}
	hc := client.New(client.Options{Tokens: store, Retry: &rp})
	a := auth.NewAuthenticator(auth.Issuer{URL: "http://127.0.0.1:1"}, oc, hc, store)
	_, err := a.PreHandshake(context.Background(), "x@y.com")
	if err == nil || err.Code != envelope.ErrBackendUnreachable {
		t.Errorf("got %+v, want BACKEND_UNREACHABLE", err)
	}
}

func TestAuthenticator_CompleteLogin_PersistsRefreshToken(t *testing.T) {
	fb := newFakeBackend(t)
	a, kc := newAuthForTest(t, fb)
	resp, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("server-pw"))
	if err != nil {
		t.Fatalf("CompleteLogin: %+v", err)
	}
	if resp.RefreshToken != "refresh-1" {
		t.Errorf("RefreshToken = %q, want refresh-1", resp.RefreshToken)
	}
	stored, kerr := kc.Load(keychain.AccountForUser("user-1"))
	if kerr != nil {
		t.Fatalf("keychain.Load: %v", kerr)
	}
	if string(stored) != "refresh-1" {
		t.Errorf("keychain entry = %q, want refresh-1", stored)
	}
}

func TestAuthenticator_RefreshRotatesRefreshToken(t *testing.T) {
	fb := newFakeBackend(t)
	a, kc := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatal(err)
	}

	// Simulate the client's 401-refresh hook: ask the session for a
	// refreshed access token. The new refresh token should also be persisted.
	newAccess, err := a.Session().RefreshAccessToken(context.Background())
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if newAccess != "access-2" {
		t.Errorf("new access = %q, want access-2", newAccess)
	}
	stored, _ := kc.Load(keychain.AccountForUser("user-1"))
	if string(stored) != "refresh-2" {
		t.Errorf("keychain entry = %q, want refresh-2 (rotated)", stored)
	}
}

// TestAuthenticator_Refresh_RejectsMissingRefreshToken locks in the
// fail-fast guardrail for substrate's sliding-window rotation contract
// (hosted-backup-contract.md §5.3 line 207: "each refresh issues a new
// refresh token; the old one is invalidated"). If substrate returns a
// response without a fresh refreshToken, the previously persisted
// refresh token is now server-side invalid — silently keeping it in the
// keychain would surface as AUTH_REQUIRED on the next subprocess that
// tries to use it (the "session disappears between subprocess
// invocations" repro on engine 2.0.0).
//
// Required behavior: RefreshAccessToken returns an explicit error and
// neither the in-memory session nor the keychain are mutated, so the
// caller (the client's 401-refresh hook) propagates a clear error to
// the user instead of leaving a stale RT behind to fail on the next call.
func TestAuthenticator_Refresh_RejectsMissingRefreshToken(t *testing.T) {
	fb := newFakeBackend(t)
	a, kc := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	// Substrate returns an accessToken but omits the rotated refreshToken.
	fb.refreshFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken": "access-2",
		})
	}

	_, err := a.Session().RefreshAccessToken(context.Background())
	if err == nil {
		t.Fatal("RefreshAccessToken returned nil, want explicit error for missing refreshToken")
	}

	// The original refresh token must survive an invalid response so the
	// user can re-login without re-deriving keys. (More important: if the
	// in-memory rt is wiped, Persist's empty-rt early-return is the only
	// reason the keychain wasn't corrupted — defending in two places.)
	snap := a.Session().Snapshot()
	if snap.RefreshToken != "refresh-1" {
		t.Errorf("in-memory refreshToken = %q, want refresh-1 (must not be wiped on bad refresh response)", snap.RefreshToken)
	}
	stored, kerr := kc.Load(keychain.AccountForUser("user-1"))
	if kerr != nil {
		t.Fatalf("keychain.Load: %v", kerr)
	}
	if string(stored) != "refresh-1" {
		t.Errorf("keychain entry = %q, want refresh-1 (must not be overwritten on bad refresh response)", stored)
	}
}

// TestAuthenticator_Refresh_RejectsMissingAccessToken locks in the
// symmetric guardrail: a refresh response with an empty accessToken is
// also a contract violation. Without the guardrail the client's
// 401-refresh hook would happily retry the original request with an
// empty bearer, producing another 401 and a confusing AUTH_REQUIRED.
func TestAuthenticator_Refresh_RejectsMissingAccessToken(t *testing.T) {
	fb := newFakeBackend(t)
	a, kc := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	fb.refreshFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"refreshToken": "refresh-2",
		})
	}

	_, err := a.Session().RefreshAccessToken(context.Background())
	if err == nil {
		t.Fatal("RefreshAccessToken returned nil, want explicit error for missing accessToken")
	}

	stored, kerr := kc.Load(keychain.AccountForUser("user-1"))
	if kerr != nil {
		t.Fatalf("keychain.Load: %v", kerr)
	}
	if string(stored) != "refresh-1" {
		t.Errorf("keychain entry = %q, want refresh-1 (must not be overwritten when accessToken is missing)", stored)
	}
}

// TestAuthenticator_CompleteLogin_PersistsAccessTokenWithExpiry locks
// the F4 fix: login parses the JWT's `exp` claim and persists the access
// token to the keychain so subsequent subprocesses skip the per-call
// refresh round-trip that was racing with substrate's reuse detection.
func TestAuthenticator_CompleteLogin_PersistsAccessTokenWithExpiry(t *testing.T) {
	fb := newFakeBackend(t)

	// Mint a real EdDSA JWT carrying a known exp so we can decode the
	// persisted entry and compare. The signature is not validated by
	// parseAccessExpiry (we trust substrate's TLS), so the keypair is
	// only here to produce a parseable token.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	_ = pub
	exp := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)
	jwtTok := signTestToken(t, "kid-1", priv, func(c *auth.Claims) {
		c.ExpiresAt = jwt.NewNumericDate(exp)
	})

	fb.loginCompleteFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"userId":             "user-1",
			"accessToken":        jwtTok,
			"refreshToken":       "refresh-1",
			"wrappedDEK":         "AAAA",
			"subscriptionStatus": "active",
		})
	}

	a, kc := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	raw, kerr := kc.Load(keychain.AccountForAccessToken("user-1"))
	if kerr != nil {
		t.Fatalf("expected access entry persisted after login, got %v", kerr)
	}
	var entry struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Token != jwtTok {
		t.Errorf("persisted token mismatch")
	}
	if !entry.ExpiresAt.Equal(exp) {
		t.Errorf("persisted exp = %v, want %v (must come from JWT exp claim)", entry.ExpiresAt, exp)
	}
}

// TestAuthenticator_Refresh_PersistsAccessTokenWithExpiry verifies the
// rotated access token also lands in the keychain with the new exp.
func TestAuthenticator_Refresh_PersistsAccessTokenWithExpiry(t *testing.T) {
	fb := newFakeBackend(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	newExp := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)
	newJWT := signTestToken(t, "kid-1", priv, func(c *auth.Claims) {
		c.ExpiresAt = jwt.NewNumericDate(newExp)
	})

	fb.refreshFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  newJWT,
			"refreshToken": "refresh-2",
		})
	}

	a, kc := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if _, err := a.Session().RefreshAccessToken(context.Background()); err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	raw, kerr := kc.Load(keychain.AccountForAccessToken("user-1"))
	if kerr != nil {
		t.Fatalf("expected access entry persisted after refresh, got %v", kerr)
	}
	var entry struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Token != newJWT {
		t.Errorf("persisted token didn't rotate after refresh")
	}
	if !entry.ExpiresAt.Equal(newExp) {
		t.Errorf("persisted exp = %v, want %v", entry.ExpiresAt, newExp)
	}
}

// TestAuthenticator_Refresh_NotCalledWhenCachedAccessTokenIsValid is the
// behavioral root of the F4 fix: with a cached access token, calling Me()
// in a fresh-session-from-keychain scenario must NOT trigger a refresh.
// That's what eliminates the race with substrate's reuse-detection.
func TestAuthenticator_Refresh_NotCalledWhenCachedAccessTokenIsValid(t *testing.T) {
	fb := newFakeBackend(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	exp := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)
	jwtTok := signTestToken(t, "kid-1", priv, func(c *auth.Claims) {
		c.ExpiresAt = jwt.NewNumericDate(exp)
	})

	fb.loginCompleteFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"userId":             "user-1",
			"accessToken":        jwtTok,
			"refreshToken":       "refresh-1",
			"wrappedDEK":         "AAAA",
			"subscriptionStatus": "active",
		})
	}
	// /api/account/me must require auth or the test passes vacuously —
	// pre-F4 the engine would send no bearer (empty AT in a fresh
	// subprocess) but the default fakeBackend /me accepts that, so the
	// refresh-not-fired assertion needs teeth.
	fb.meFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   map[string]string{"code": "AUTH_REQUIRED", "message": "no bearer"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"userId":             "user-1",
			"email":              "user@example.com",
			"subscriptionStatus": "active",
			"createdAt":          "2026-05-02T00:00:00Z",
		})
	}

	// Login on one session (writes the access entry to the shared
	// keychain), then create a fresh session backed by the same keychain
	// and call Me() — that simulates a `backup status` subprocess.
	a1, kc := newAuthForTest(t, fb)
	if _, err := a1.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}

	// Fresh stack on the same keychain — the real-world "second
	// subprocess" case. Reuses the fakeBackend so /api/account/me still
	// works, and tracks refresh hits to assert none fire.
	storeB := auth.NewSessionStore(kc)
	oc := oidc.NewClient(fb.URL(), fb.srv.Client())
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, Multiplier: 1, MaxWait: time.Millisecond}
	hc := client.New(client.Options{Tokens: storeB, Retry: &rp})
	a2 := auth.NewAuthenticator(auth.IssuerFromOIDC(oc, "endstate-backup"), oc, hc, storeB)
	if err := storeB.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent: %v", err)
	}

	before := atomic.LoadInt32(&fb.refreshHits)
	if _, err := a2.Me(context.Background()); err != nil {
		t.Fatalf("Me on fresh stack: %v", err)
	}
	after := atomic.LoadInt32(&fb.refreshHits)
	if after != before {
		t.Errorf("refresh fired %d times on the second subprocess; want 0 (cached AT should be sufficient)", after-before)
	}
}

func TestAuthenticator_LogoutClearsKeychainEvenIfBackendDown(t *testing.T) {
	fb := newFakeBackend(t)
	a, kc := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatal(err)
	}

	// Backend returns 500: logout should still wipe local state.
	fb.logoutFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		w.WriteHeader(http.StatusInternalServerError)
	}
	if err := a.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %+v", err)
	}
	if _, err := kc.Load(keychain.AccountForUser("user-1")); !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("keychain entry remained after logout: %v", err)
	}
	if a.Session().SignedIn() {
		t.Error("session still signed in after logout")
	}
}

func TestAuthenticator_Me_ReturnsAccountInfo(t *testing.T) {
	fb := newFakeBackend(t)
	a, _ := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	me, err := a.Me(context.Background())
	if err != nil {
		t.Fatalf("Me: %+v", err)
	}
	if me.Email != "user@example.com" {
		t.Errorf("email = %q", me.Email)
	}
	if me.SubscriptionStatus != "active" {
		t.Errorf("subscription = %q", me.SubscriptionStatus)
	}
}

func TestAuthenticator_PreHandshake_404MapsToNotFound(t *testing.T) {
	fb := newFakeBackend(t)
	fb.loginPreFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		w.WriteHeader(http.StatusNotFound)
	}
	a, _ := newAuthForTest(t, fb)
	_, err := a.PreHandshake(context.Background(), "missing@example.com")
	if err == nil || err.Code != envelope.ErrNotFound {
		t.Errorf("got %+v, want NOT_FOUND", err)
	}
}
