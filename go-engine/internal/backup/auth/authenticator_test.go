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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
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
//
// The refresh lock (F5) is pointed at t.TempDir() so each test gets an
// isolated lock file — without this two parallel tests would serialize
// through the real `%APPDATA%/Endstate/refresh.lock` and the second
// test would block on the first.
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
	a := auth.NewAuthenticator(auth.IssuerFromOIDC(oc, "endstate-backup"), oc, hc, store).
		WithRefreshLockDir(t.TempDir())
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
	a2 := auth.NewAuthenticator(auth.IssuerFromOIDC(oc, "endstate-backup"), oc, hc, storeB).
		WithRefreshLockDir(t.TempDir())
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

// newAuthOnSharedKeychain builds a second Authenticator backed by the
// supplied keychain (simulating a second process) and points its refresh
// lock at the same directory as the first. Together with newAuthForTest
// these compose the "two processes, one user" topology that the F5 lock
// guards.
func newAuthOnSharedKeychain(t *testing.T, fb *fakeBackend, kc keychain.Keychain, lockDir string) *auth.Authenticator {
	t.Helper()
	storeB := auth.NewSessionStore(kc)
	oc := oidc.NewClient(fb.URL(), fb.srv.Client())
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, Multiplier: 1, MaxWait: time.Millisecond}
	hc := client.New(client.Options{Tokens: storeB, Retry: &rp})
	a := auth.NewAuthenticator(auth.IssuerFromOIDC(oc, "endstate-backup"), oc, hc, storeB).
		WithRefreshLockDir(lockDir)
	// Hydrate so the new "process" sees the persisted RT.
	if err := storeB.HydrateFromCurrent(); err != nil {
		t.Fatalf("HydrateFromCurrent: %v", err)
	}
	return a
}

// TestAuthenticator_RefreshLock_SerializesConcurrentRotations is the
// F5 root assertion: two processes that both think it's time to refresh
// must converge on a single substrate round-trip, not race each other.
// Without the lock both POST the same RT, substrate burns it after the
// first call, the second persists a now-stale RT to the keychain, and
// the next subprocess surfaces AUTH_REQUIRED.
//
// Topology: two Authenticator instances backed by the same keychain
// (simulating two processes), pointing at the same refresh lock file
// (simulating shared per-user state on disk). Both goroutines call
// RefreshAccessToken concurrently against a mock substrate that
// counts hits.
func TestAuthenticator_RefreshLock_SerializesConcurrentRotations(t *testing.T) {
	fb := newFakeBackend(t)
	// Mint a JWT with future exp so the second waiter's
	// `session.AccessToken()` check sees a valid cached token after
	// the first goroutine persists. Without a parseable exp the F4
	// path can't trust the cached AT and falls through to a network
	// call, defeating the second-waiter short-circuit.
	_, priv, kerr := ed25519.GenerateKey(rand.Reader)
	if kerr != nil {
		t.Fatalf("GenerateKey: %v", kerr)
	}
	futureExp := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	rotatedAT := signTestToken(t, "kid-1", priv, func(c *auth.Claims) {
		c.ExpiresAt = jwt.NewNumericDate(futureExp)
	})
	// Slow down the refresh handler so the second goroutine reliably
	// contends for the lock while the first holds it. Without this the
	// first call can return before the second even tries to acquire,
	// which would pass for the wrong reason.
	fb.refreshFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  rotatedAT,
			"refreshToken": "refresh-2",
		})
	}

	lockDir := t.TempDir()
	kc := keychain.NewMemory()
	storeA := auth.NewSessionStore(kc)
	oc := oidc.NewClient(fb.URL(), fb.srv.Client())
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, Multiplier: 1, MaxWait: time.Millisecond}
	hcA := client.New(client.Options{Tokens: storeA, Retry: &rp})
	a1 := auth.NewAuthenticator(auth.IssuerFromOIDC(oc, "endstate-backup"), oc, hcA, storeA).
		WithRefreshLockDir(lockDir)
	if _, err := a1.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}

	// Sibling "process": separate Authenticator + SessionStore on the
	// same keychain and lock dir.
	a2 := newAuthOnSharedKeychain(t, fb, kc, lockDir)

	beforeRefresh := atomic.LoadInt32(&fb.refreshHits)

	var wg sync.WaitGroup
	type res struct {
		at  string
		err error
	}
	results := make([]res, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		at, err := a1.Session().RefreshAccessToken(context.Background())
		results[0] = res{at, err}
	}()
	go func() {
		defer wg.Done()
		// Tiny stagger so both goroutines enter the lock-acquire window
		// without the second sneaking in first on a fast scheduler.
		time.Sleep(5 * time.Millisecond)
		at, err := a2.Session().RefreshAccessToken(context.Background())
		results[1] = res{at, err}
	}()
	wg.Wait()

	// Both calls succeeded.
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d returned err: %v", i, r.err)
		}
		if r.at != rotatedAT {
			t.Errorf("goroutine %d: access token = %q, want the rotated JWT", i, r.at)
		}
	}

	// Only one substrate hit — the lock collapsed the race.
	hits := atomic.LoadInt32(&fb.refreshHits) - beforeRefresh
	if hits != 1 {
		t.Errorf("substrate refresh hits = %d, want 1 (lock should have serialised and second waiter should have short-circuited)", hits)
	}

	// Keychain holds the rotated RT — not a stale "second writer wrote
	// the old refresh-1 back" footgun.
	stored, kerr := kc.Load(keychain.AccountForUser("user-1"))
	if kerr != nil {
		t.Fatalf("keychain.Load: %v", kerr)
	}
	if string(stored) != "refresh-2" {
		t.Errorf("keychain RT = %q, want refresh-2 (rotated)", stored)
	}
}

// TestAuthenticator_RefreshLock_SecondWaiterUsesFreshAccessToken locks
// in the second-waiter optimisation: when goroutine B blocks on the
// lock while goroutine A's refresh completes, B should re-hydrate,
// observe the now-valid cached access token, and return without
// hitting substrate. Otherwise N concurrent subprocesses would still
// produce N serial substrate round-trips even though the lock prevents
// the rotation race.
func TestAuthenticator_RefreshLock_SecondWaiterUsesFreshAccessToken(t *testing.T) {
	fb := newFakeBackend(t)
	// Mint a JWT with a long-into-the-future exp so the cached AT
	// passes the AccessToken() expiry check inside refreshAccessToken's
	// second-waiter short-circuit.
	_, priv, kerr := ed25519.GenerateKey(rand.Reader)
	if kerr != nil {
		t.Fatalf("GenerateKey: %v", kerr)
	}
	futureExp := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	rotatedAT := signTestToken(t, "kid-1", priv, func(c *auth.Claims) {
		c.ExpiresAt = jwt.NewNumericDate(futureExp)
	})

	gateOpen := make(chan struct{})
	gateClose := make(chan struct{})
	fb.refreshFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		close(gateOpen)
		<-gateClose
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  rotatedAT,
			"refreshToken": "refresh-2",
		})
	}

	lockDir := t.TempDir()
	kc := keychain.NewMemory()
	storeA := auth.NewSessionStore(kc)
	oc := oidc.NewClient(fb.URL(), fb.srv.Client())
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, Multiplier: 1, MaxWait: time.Millisecond}
	hcA := client.New(client.Options{Tokens: storeA, Retry: &rp})
	a1 := auth.NewAuthenticator(auth.IssuerFromOIDC(oc, "endstate-backup"), oc, hcA, storeA).
		WithRefreshLockDir(lockDir)
	if _, err := a1.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	a2 := newAuthOnSharedKeychain(t, fb, kc, lockDir)

	beforeRefresh := atomic.LoadInt32(&fb.refreshHits)

	var wg sync.WaitGroup
	wg.Add(2)
	var atA, atB string
	var errA, errB error
	go func() {
		defer wg.Done()
		atA, errA = a1.Session().RefreshAccessToken(context.Background())
	}()
	go func() {
		defer wg.Done()
		// Wait until A is mid-refresh before B starts so B is
		// guaranteed to block on the lock rather than win the race.
		<-gateOpen
		atB, errB = a2.Session().RefreshAccessToken(context.Background())
		// B should not be the one to release A's gate — only A is
		// waiting on it. So close it from here once B has acquired
		// the lock (which it can't until A releases). Wait... B
		// blocks on the lock until A finishes. So we need to release
		// A's gate first, then let B run.
	}()
	// Release A so it can complete the network call, persist, and
	// drop the lock. Then B unblocks and short-circuits on the cached AT.
	close(gateClose)
	wg.Wait()

	if errA != nil {
		t.Fatalf("goroutine A err: %v", errA)
	}
	if errB != nil {
		t.Fatalf("goroutine B err: %v", errB)
	}
	if atA != rotatedAT {
		t.Errorf("A access token mismatch")
	}
	if atB != rotatedAT {
		t.Errorf("B access token = %q, want the rotated JWT (second waiter must reuse the cached value)", atB)
	}

	hits := atomic.LoadInt32(&fb.refreshHits) - beforeRefresh
	if hits != 1 {
		t.Errorf("substrate refresh hits = %d, want exactly 1 (second waiter must short-circuit on the cached AT)", hits)
	}
}

// TestAuthenticator_RefreshLock_CtxCancel asserts that a ctx deadline
// expiring while the lock is held by an external holder produces a
// clean, wrapped error rather than panicking or blocking forever.
// This is the "transient" failure mode the 401-refresh hook can retry.
func TestAuthenticator_RefreshLock_CtxCancel(t *testing.T) {
	fb := newFakeBackend(t)
	a, _ := newAuthForTest(t, fb)
	if _, err := a.CompleteLogin(context.Background(), "user@example.com", []byte("pw")); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}

	// Grab the lock file path that the test helper wired up via
	// WithRefreshLockDir, then hold it from outside the Authenticator
	// for the duration of the cancel window.
	//
	// newAuthForTest passes a per-test t.TempDir() to WithRefreshLockDir;
	// we don't have a getter for that path here, so reproduce the
	// directory inference via a second WithRefreshLockDir call into a
	// fresh temp dir and have the Authenticator + the foreign holder
	// share it.
	lockDir := t.TempDir()
	a.WithRefreshLockDir(lockDir)
	foreign := flock.New(lockDir + "/refresh.lock")
	if _, ferr := foreign.TryLock(); ferr != nil {
		t.Fatalf("foreign TryLock setup: %v", ferr)
	}
	t.Cleanup(func() { _ = foreign.Unlock() })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := a.Session().RefreshAccessToken(ctx)
	if err == nil {
		t.Fatal("RefreshAccessToken returned nil; want ctx-cancel-wrapped error while foreign holder pins the lock")
	}
	// Acceptable failure modes: wrapped context.DeadlineExceeded or our
	// "could not acquire" sentinel. The exact wording is not what we
	// guard — we guard that it returns rather than panics/blocks.
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "refresh") {
		t.Errorf("err = %v; want one that references ctx deadline or refresh lock", err)
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
