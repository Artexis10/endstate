// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// TestBackupBrowserSession_SignedIn locks the happy path: a hydrated session
// reaches the issuer-derived /api/auth/browser-session endpoint and the
// envelope data carries sessionToken + accountUrl verbatim.
func TestBackupBrowserSession_SignedIn(t *testing.T) {
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	if err := kc.Store(keychain.AccountForUser("user-1"), []byte("refresh-1")); err != nil {
		t.Fatal(err)
	}

	st := stackForBackend(srv, kc)
	if err := st.Auth.Session().Hydrate("user-1"); err != nil {
		t.Fatal(err)
	}

	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })
	defer restore()

	data, envErr := commands.RunBackup(commands.BackupFlags{Subcommand: "browser-session"})
	if envErr != nil {
		t.Fatalf("browser-session signed-in: %+v", envErr)
	}
	res, ok := data.(*commands.BrowserSessionResult)
	if !ok {
		t.Fatalf("data type = %T, want *BrowserSessionResult", data)
	}
	if res.SessionToken != "synthetic.jwt.body" {
		t.Errorf("sessionToken = %q, want synthetic.jwt.body", res.SessionToken)
	}
	if res.AccountURL != srv.URL+"/account/start" {
		t.Errorf("accountUrl = %q, want %q", res.AccountURL, srv.URL+"/account/start")
	}
}

// TestBackupBrowserSession_SignedOut asserts the command refuses without a
// session and makes no network call — the browser-session route would
// 404/panic the test if hit, but the signed-out guard returns before any
// request. Mirrors backup_subscribe_test.go's pattern.
func TestBackupBrowserSession_SignedOut(t *testing.T) {
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})
	defer restore()

	data, envErr := commands.RunBackup(commands.BackupFlags{Subcommand: "browser-session"})
	if envErr == nil {
		t.Fatalf("expected AUTH_REQUIRED when signed out, got data %+v", data)
	}
	if envErr.Code != envelope.ErrAuthRequired {
		t.Errorf("code = %q, want %q", envErr.Code, envelope.ErrAuthRequired)
	}
}

// TestBackupBrowserSession_Unauthorized maps a 401 from the browser-session
// endpoint to AUTH_REQUIRED. Uses a self-contained backend so the shared
// fakeBackend's success route is not disturbed. Covers the case where the
// local session looks signed-in but substrate has invalidated the access
// token (e.g. account deleted, kid rotation, refresh chain revoked).
func TestBackupBrowserSession_Unauthorized(t *testing.T) {
	srv := browserSession401Backend(t)
	kc := keychain.NewMemory()
	if err := kc.Store(keychain.AccountForUser("user-1"), []byte("refresh-1")); err != nil {
		t.Fatal(err)
	}

	st := stackForBackend(srv, kc)
	if err := st.Auth.Session().Hydrate("user-1"); err != nil {
		t.Fatal(err)
	}

	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })
	defer restore()

	data, envErr := commands.RunBackup(commands.BackupFlags{Subcommand: "browser-session"})
	if envErr == nil {
		t.Fatalf("expected AUTH_REQUIRED on 401, got data %+v", data)
	}
	if envErr.Code != envelope.ErrAuthRequired {
		t.Errorf("code = %q, want %q", envErr.Code, envelope.ErrAuthRequired)
	}
}

// browserSession401Backend stands up a minimal discovery + browser-session
// backend whose mint route returns HTTP 401.
func browserSession401Backend(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(oidc.Document{
			Issuer:                           srv.URL,
			JWKSURI:                          srv.URL + "/api/.well-known/jwks.json",
			IDTokenSigningAlgValuesSupported: []string{"EdDSA"},
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
	mux.HandleFunc("/api/auth/browser-session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   map[string]string{"code": "UNAUTHENTICATED", "message": "session invalid"},
		})
	})
	t.Cleanup(srv.Close)
	return srv
}
