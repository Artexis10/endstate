// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// fakeBackend mounts the discovery + auth + account routes on an httptest
// server. Subset of the auth-package fixture, replicated here so the
// commands tests stay independent.
func fakeBackend(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
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
		w.Header().Set("X-Endstate-API-Version", "1.0")
		var raw map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if _, hasPwd := raw["serverPassword"]; hasPwd {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"userId":             "user-1",
				"accessToken":        "access-1",
				"refreshToken":       "refresh-1",
				"wrappedDEK":         "AAAA",
				"subscriptionStatus": "active",
			})
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
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/account/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"userId":             "user-1",
			"email":              "user@example.com",
			"subscriptionStatus": "active",
			"createdAt":          "2026-05-02T00:00:00Z",
		})
	})
	t.Cleanup(srv.Close)
	return srv
}

// stackForBackend builds a Stack wired to the fake backend with the
// supplied keychain. Returned so tests can pre-seed or assert state.
func stackForBackend(srv *httptest.Server, kc keychain.Keychain) *backup.Stack {
	store := auth.NewSessionStore(kc)
	oc := oidc.NewClient(srv.URL, srv.Client())
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, MaxWait: time.Millisecond}
	hc := client.New(client.Options{Tokens: store, Retry: &rp})
	a := auth.NewAuthenticator(auth.Issuer{URL: srv.URL, Audience: "endstate-backup"}, oc, hc, store)
	st := storage.New(srv.URL, hc)
	return &backup.Stack{
		Auth:    a,
		Storage: st,
		Issuer:  srv.URL,
		OIDC:    oc,
		HTTP:    hc,
		Session: store,
	}
}

func TestBackupStatus_SignedOut(t *testing.T) {
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})
	defer restore()

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "status"})
	if err != nil {
		t.Fatalf("status signed-out: %+v", err)
	}
	res, ok := data.(*commands.StatusResult)
	if !ok {
		t.Fatalf("data type = %T, want *StatusResult", data)
	}
	if res.SignedIn {
		t.Error("expected signedIn=false")
	}
	if res.IssuerURL != srv.URL {
		t.Errorf("issuerUrl = %q, want %q", res.IssuerURL, srv.URL)
	}
	if res.Email != "" || res.UserID != "" || res.SubscriptionStatus != "" {
		t.Errorf("expected empty optional fields when signed out, got %+v", res)
	}
}

func TestBackupStatus_SignedIn(t *testing.T) {
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	// Pre-seed: a session for user-1 with a refresh token in the keychain.
	if err := kc.Store(keychain.AccountForUser("user-1"), []byte("refresh-1")); err != nil {
		t.Fatal(err)
	}

	st := stackForBackend(srv, kc)
	if err := st.Auth.Session().Hydrate("user-1"); err != nil {
		t.Fatal(err)
	}

	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })
	defer restore()

	data, envErr := commands.RunBackup(commands.BackupFlags{Subcommand: "status"})
	if envErr != nil {
		t.Fatalf("status signed-in: %+v", envErr)
	}
	res := data.(*commands.StatusResult)
	if !res.SignedIn {
		t.Error("expected signedIn=true after Hydrate")
	}
	if res.Email != "user@example.com" {
		t.Errorf("email = %q, want user@example.com", res.Email)
	}
	if res.SubscriptionStatus != "active" {
		t.Errorf("subscription = %q, want active", res.SubscriptionStatus)
	}
}

func TestBackupLogin_RequiresEmail(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "login"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--email") {
		t.Errorf("message %q should mention --email", err.Message)
	}
}

func TestBackupLogin_EmptyPassphrase(t *testing.T) {
	defer commands.WithPassphraseReader(func(io.Reader) (string, error) { return "", nil })()
	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "login", Email: "user@example.com"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "passphrase") {
		t.Errorf("message %q should mention passphrase", err.Message)
	}
}

func TestBackupLogin_PreHandshakeOK_CryptoStubBlocks(t *testing.T) {
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})
	defer restore()
	defer commands.WithPassphraseReader(func(io.Reader) (string, error) { return "secret-pass", nil })()

	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "login", Email: "user@example.com"})
	if err == nil {
		t.Fatal("expected an error from the crypto stub")
	}
	if err.Code != envelope.ErrInternalError {
		t.Errorf("code = %q, want INTERNAL_ERROR", err.Code)
	}
	if !strings.Contains(err.Message, "crypto") || !strings.Contains(err.Message, "not yet implemented") {
		t.Errorf("message %q should reference the crypto stub", err.Message)
	}
}

func TestBackupLogin_BackendUnreachable(t *testing.T) {
	// Authenticator pointing at a closed port → BACKEND_UNREACHABLE.
	store := auth.NewSessionStore(keychain.NewMemory())
	oc := oidc.NewClient("http://127.0.0.1:1", &http.Client{Timeout: 100 * time.Millisecond})
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, MaxWait: time.Millisecond}
	hc := client.New(client.Options{Tokens: store, Retry: &rp})
	a := auth.NewAuthenticator(auth.Issuer{URL: "http://127.0.0.1:1"}, oc, hc, store)
	st := storage.New("http://127.0.0.1:1", hc)
	stack := &backup.Stack{Auth: a, Storage: st, Issuer: "http://127.0.0.1:1", OIDC: oc, HTTP: hc, Session: store}

	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return stack })
	defer restore()
	defer commands.WithPassphraseReader(func(io.Reader) (string, error) { return "secret-pass", nil })()

	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "login", Email: "user@example.com"})
	if err == nil || err.Code != envelope.ErrBackendUnreachable {
		t.Errorf("got %+v, want BACKEND_UNREACHABLE", err)
	}
}

func TestBackupLogout_NothingPersistedIsIdempotent(t *testing.T) {
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})
	defer restore()

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "logout"})
	if err != nil {
		t.Fatalf("logout when signed-out: %+v", err)
	}
	if !data.(*commands.LogoutResult).SignedOut {
		t.Errorf("expected signedOut=true")
	}
}

func TestBackupLogout_ClearsPersistedSession(t *testing.T) {
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

	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "logout"}); err != nil {
		t.Fatalf("logout: %+v", err)
	}
	if _, err := kc.Load(keychain.AccountForUser("user-1")); !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("keychain entry remained after logout: %v", err)
	}
}

func TestRunBackup_RequiresSubcommand(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Errorf("got %+v, want INTERNAL_ERROR", err)
	}
}

func TestRunAccount_DeleteRequiresConfirm(t *testing.T) {
	_, err := commands.RunAccount(commands.AccountFlags{Subcommand: "delete"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--confirm") {
		t.Errorf("message %q should mention --confirm", err.Message)
	}
}

func TestCapabilities_AdvertisesHostedBackup(t *testing.T) {
	t.Setenv("ENDSTATE_OIDC_ISSUER_URL", "https://example-host.test")
	t.Setenv("ENDSTATE_OIDC_AUDIENCE", "endstate-backup-test")

	data, err := commands.RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities: %+v", err)
	}
	caps := data.(commands.CapabilitiesData)
	if !caps.Features.HostedBackup.Supported {
		t.Error("hostedBackup.supported = false, want true")
	}
	if caps.Features.HostedBackup.IssuerURL != "https://example-host.test" {
		t.Errorf("issuerUrl = %q (want env-overridden value)", caps.Features.HostedBackup.IssuerURL)
	}
	if caps.Features.HostedBackup.Audience != "endstate-backup-test" {
		t.Errorf("audience = %q (want env-overridden value)", caps.Features.HostedBackup.Audience)
	}
	if caps.Features.HostedBackup.MinSchemaVersion != "1.0" {
		t.Errorf("minSchemaVersion = %q, want 1.0", caps.Features.HostedBackup.MinSchemaVersion)
	}
	if _, ok := caps.Commands["backup"]; !ok {
		t.Error("Commands map missing 'backup'")
	}
	if _, ok := caps.Commands["account"]; !ok {
		t.Error("Commands map missing 'account'")
	}
}
