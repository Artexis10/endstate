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

// TestBackupSubscribe_SignedIn locks the happy path: a hydrated session
// reaches the issuer-derived /api/billing/checkout endpoint and the
// envelope data carries the checkoutUrl + transactionId verbatim.
func TestBackupSubscribe_SignedIn(t *testing.T) {
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

	data, envErr := commands.RunBackup(commands.BackupFlags{Subcommand: "subscribe"})
	if envErr != nil {
		t.Fatalf("subscribe signed-in: %+v", envErr)
	}
	res, ok := data.(*commands.SubscribeResult)
	if !ok {
		t.Fatalf("data type = %T, want *SubscribeResult", data)
	}
	if res.TransactionID != "txn_abc123" {
		t.Errorf("transactionId = %q, want txn_abc123", res.TransactionID)
	}
	if res.CheckoutURL != srv.URL+"/endstate?_ptxn=txn_abc123" {
		t.Errorf("checkoutUrl = %q, want %q", res.CheckoutURL, srv.URL+"/endstate?_ptxn=txn_abc123")
	}
}

// TestBackupSubscribe_SignedOut asserts the command refuses without a
// session and makes no network call — the billing route would 404/panic
// the test if hit, but the signed-out guard returns before any request.
func TestBackupSubscribe_SignedOut(t *testing.T) {
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})
	defer restore()

	data, envErr := commands.RunBackup(commands.BackupFlags{Subcommand: "subscribe"})
	if envErr == nil {
		t.Fatalf("expected AUTH_REQUIRED when signed out, got data %+v", data)
	}
	if envErr.Code != envelope.ErrAuthRequired {
		t.Errorf("code = %q, want %q", envErr.Code, envelope.ErrAuthRequired)
	}
}

// TestBackupSubscribe_PaymentRequired maps a 402 from the billing endpoint
// to SUBSCRIPTION_REQUIRED. Uses a self-contained backend so the shared
// fakeBackend's success route is not disturbed.
func TestBackupSubscribe_PaymentRequired(t *testing.T) {
	srv := checkout402Backend(t)
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

	data, envErr := commands.RunBackup(commands.BackupFlags{Subcommand: "subscribe"})
	if envErr == nil {
		t.Fatalf("expected SUBSCRIPTION_REQUIRED on 402, got data %+v", data)
	}
	if envErr.Code != envelope.ErrSubscriptionRequired {
		t.Errorf("code = %q, want %q", envErr.Code, envelope.ErrSubscriptionRequired)
	}
}

// checkout402Backend stands up a minimal discovery + billing backend whose
// checkout route returns HTTP 402.
func checkout402Backend(t *testing.T) *httptest.Server {
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
	mux.HandleFunc("/api/billing/checkout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   map[string]string{"code": "SUBSCRIPTION_REQUIRED", "message": "payment required"},
		})
	})
	t.Cleanup(srv.Close)
	return srv
}
