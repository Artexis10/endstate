// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package storage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
)

// validDiscovery returns a discovery document whose backup_api_base is
// configurable per test. issuer is the OIDC issuer URL; backupBase is
// what the test wants advertised under endstate_extensions.
func validDiscovery(issuer, backupBase string) map[string]interface{} {
	return map[string]interface{}{
		"issuer":                            issuer,
		"jwks_uri":                          issuer + "/api/.well-known/jwks.json",
		"id_token_signing_alg_values_supported": []string{"EdDSA"},
		"endstate_extensions": map[string]interface{}{
			"auth_signup_endpoint":         issuer + "/api/auth/signup",
			"auth_login_endpoint":          issuer + "/api/auth/login",
			"auth_refresh_endpoint":        issuer + "/api/auth/refresh",
			"auth_logout_endpoint":         issuer + "/api/auth/logout",
			"auth_recover_endpoint":        issuer + "/api/auth/recover",
			"backup_api_base":              backupBase,
			"supported_kdf_algorithms":     []string{"argon2id"},
			"supported_envelope_versions":  []int{1},
			"min_kdf_params":               map[string]int{"memory": 65536, "iterations": 3, "parallelism": 4},
		},
	}
}

// TestListBackups_HonorsBackupAPIBase confirms the storage client uses
// the discovery-advertised backup_api_base, not the issuer + "/api/backups"
// fallback. Self-hosters who relocate the backup endpoints rely on this.
func TestListBackups_HonorsBackupAPIBase(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	customPath := "/v1/private/backups"
	customBase := srv.URL + customPath

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, customBase))
	})

	var seenPath string
	mux.HandleFunc(customPath, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"backups": []interface{}{}})
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	hc := client.New(client.Options{Tokens: client.Anonymous{}})
	st := storage.New(srv.URL, oc, hc)

	if _, err := st.ListBackups(context.Background()); err != nil {
		t.Fatalf("ListBackups: %+v", err)
	}
	if !strings.HasPrefix(seenPath, customPath) {
		t.Errorf("ListBackups hit %q, want a path under %q (backup_api_base must be honored)", seenPath, customPath)
	}
}

// TestListBackups_FallsBackToIssuerWhenDiscoveryFails covers the
// degraded-discovery path: when the discovery doc is invalid (here, the
// OIDC validator rejects an empty backup_api_base, but transport errors
// and JSON parse errors trigger the same fallback), storage falls back
// to ${issuer}/api/backups so individual storage calls don't block on
// transient discovery hiccups.
func TestListBackups_FallsBackToIssuerWhenDiscoveryFails(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// Discovery doc with backup_api_base intentionally empty so the
		// oidc validator rejects it. storage.backupBaseURL sees the
		// non-nil error from Discovery and falls back to issuer + /api/backups.
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, ""))
	})

	var seenPath string
	mux.HandleFunc("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"backups": []interface{}{}})
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	hc := client.New(client.Options{Tokens: client.Anonymous{}})
	st := storage.New(srv.URL, oc, hc)

	if _, err := st.ListBackups(context.Background()); err != nil {
		t.Fatalf("ListBackups: %+v", err)
	}
	if seenPath != "/api/backups" {
		t.Errorf("fallback path = %q, want /api/backups", seenPath)
	}
}
