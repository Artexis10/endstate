// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// storageBackend extends the auth-only fakeBackend with /api/backups/*
// routes. Each route can be overridden per test.
type storageBackend struct {
	srv               *httptest.Server
	listFn            http.HandlerFunc
	versionsFn        http.HandlerFunc
	deleteFn          http.HandlerFunc
	deleteVersionFn   http.HandlerFunc
	deleteAccountFn   http.HandlerFunc
	listHits          int32
	versionsHits      int32
	deleteHits        int32
	deleteVersionHits int32
	deleteAccountHits int32
}

func newStorageBackend(t *testing.T) *storageBackend {
	t.Helper()
	sb := &storageBackend{}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	sb.srv = srv
	t.Cleanup(srv.Close)

	// Reuse the auth-test discovery + jwks + login + logout + me handlers.
	addAuthRoutes(mux, srv)

	mux.HandleFunc("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		atomic.AddInt32(&sb.listHits, 1)
		if sb.listFn != nil {
			sb.listFn(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"backups": []map[string]interface{}{
				{"id": "b-1", "name": "default", "latestVersionId": "v-1", "versionCount": 2, "totalSize": 4096, "updatedAt": "2026-05-02T00:00:00Z"},
			},
		})
	})

	mux.HandleFunc("/api/backups/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		path := strings.TrimPrefix(r.URL.Path, "/api/backups/")
		segments := strings.Split(path, "/")

		if r.Method == http.MethodDelete && len(segments) == 1 {
			atomic.AddInt32(&sb.deleteHits, 1)
			if sb.deleteFn != nil {
				sb.deleteFn(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet && len(segments) == 2 && segments[1] == "versions" {
			atomic.AddInt32(&sb.versionsHits, 1)
			if sb.versionsFn != nil {
				sb.versionsFn(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"versions": []map[string]interface{}{
					{"versionId": "v-1", "createdAt": "2026-05-01T00:00:00Z", "size": 1024, "manifestSha256": "aa"},
					{"versionId": "v-2", "createdAt": "2026-05-02T00:00:00Z", "size": 2048, "manifestSha256": "bb"},
				},
			})
			return
		}
		if r.Method == http.MethodDelete && len(segments) == 3 && segments[1] == "versions" {
			atomic.AddInt32(&sb.deleteVersionHits, 1)
			if sb.deleteVersionFn != nil {
				sb.deleteVersionFn(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/api/account", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		if r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&sb.deleteAccountHits, 1)
		if sb.deleteAccountFn != nil {
			sb.deleteAccountFn(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return sb
}

// addAuthRoutes mounts the auth + me handlers on the supplied mux. Mirrors
// the fakeBackend in backup_test.go but on a fresh mux so storage tests
// don't depend on that file's wiring.
func addAuthRoutes(mux *http.ServeMux, srv *httptest.Server) {
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                            srv.URL,
			"jwks_uri":                          srv.URL + "/api/.well-known/jwks.json",
			"id_token_signing_alg_values_supported": []string{"EdDSA"},
			"endstate_extensions": map[string]interface{}{
				"auth_signup_endpoint":         srv.URL + "/api/auth/signup",
				"auth_login_endpoint":          srv.URL + "/api/auth/login",
				"auth_refresh_endpoint":        srv.URL + "/api/auth/refresh",
				"auth_logout_endpoint":         srv.URL + "/api/auth/logout",
				"auth_recover_endpoint":        srv.URL + "/api/auth/recover",
				"backup_api_base":              srv.URL + "/api/backups",
				"supported_kdf_algorithms":     []string{"argon2id"},
				"supported_envelope_versions":  []int{1},
				"min_kdf_params":               map[string]int{"memory": 65536, "iterations": 3, "parallelism": 4},
			},
		})
	})
	mux.HandleFunc("/api/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{}})
	})
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		f := loadFixture()
		var raw map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if _, hasPwd := raw["serverPassword"]; hasPwd {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"userId":             "user-1",
				"accessToken":        "access-1",
				"refreshToken":       "refresh-1",
				"wrappedDEK":         f.WrappedDEKB64,
				"subscriptionStatus": "active",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"salt":      f.SaltB64,
			"kdfParams": map[string]interface{}{"algorithm": "argon2id", "memory": 65536, "iterations": 3, "parallelism": 4},
		})
	})
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/account/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"userId":             "user-1",
			"email":              "user@example.com",
			"subscriptionStatus": "active",
			"createdAt":          "2026-05-02T00:00:00Z",
		})
	})
}

func stackForStorageBackend(sb *storageBackend) *backup.Stack {
	kc := keychain.NewMemory()
	st := stackForBackend(sb.srv, kc)
	// Pre-seed a session so the client carries a bearer token.
	st.Auth.Session().SetTokens("user-1", "user@example.com", "access-1", "refresh-1", "active", time.Time{})
	return st
}

func TestBackupList_HappyPath(t *testing.T) {
	sb := newStorageBackend(t)
	st := stackForStorageBackend(sb)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "list"})
	if err != nil {
		t.Fatalf("list: %+v", err)
	}
	res, ok := data.(*commands.ListResult)
	if !ok {
		t.Fatalf("data type = %T", data)
	}
	if len(res.Backups) != 1 || res.Backups[0].ID != "b-1" {
		t.Errorf("backups = %+v", res.Backups)
	}
}

func TestBackupVersions_RequiresBackupID(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "versions"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Errorf("got %+v, want INTERNAL_ERROR", err)
	}
}

func TestBackupVersions_HappyPath(t *testing.T) {
	sb := newStorageBackend(t)
	st := stackForStorageBackend(sb)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "versions", BackupID: "b-1"})
	if err != nil {
		t.Fatalf("versions: %+v", err)
	}
	res := data.(*commands.VersionsResult)
	if len(res.Versions) != 2 {
		t.Errorf("versions: len = %d, want 2", len(res.Versions))
	}
	if res.BackupID != "b-1" {
		t.Errorf("BackupID = %q", res.BackupID)
	}
}

func TestBackupDelete_RequiresConfirm(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "delete", BackupID: "b-1"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--confirm") {
		t.Errorf("message %q should mention --confirm", err.Message)
	}
}

func TestBackupDelete_HappyPath(t *testing.T) {
	sb := newStorageBackend(t)
	st := stackForStorageBackend(sb)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "delete", BackupID: "b-1", Confirm: true})
	if err != nil {
		t.Fatalf("delete: %+v", err)
	}
	if !data.(*commands.DeleteResult).Deleted {
		t.Errorf("expected Deleted=true")
	}
	if atomic.LoadInt32(&sb.deleteHits) != 1 {
		t.Errorf("delete hits = %d, want 1", sb.deleteHits)
	}
}

func TestBackupDeleteVersion_RequiresIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    commands.BackupFlags
	}{
		{"missing both", commands.BackupFlags{Subcommand: "delete-version"}},
		{"missing version", commands.BackupFlags{Subcommand: "delete-version", BackupID: "b"}},
		{"missing backup", commands.BackupFlags{Subcommand: "delete-version", VersionID: "v"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := commands.RunBackup(tc.f)
			if err == nil || err.Code != envelope.ErrInternalError {
				t.Errorf("got %+v, want INTERNAL_ERROR", err)
			}
		})
	}
}

func TestBackupDeleteVersion_HappyPath(t *testing.T) {
	sb := newStorageBackend(t)
	st := stackForStorageBackend(sb)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	data, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "delete-version", BackupID: "b-1", VersionID: "v-1", Confirm: true,
	})
	if err != nil {
		t.Fatalf("delete-version: %+v", err)
	}
	if !data.(*commands.DeleteVersionResult).Deleted {
		t.Errorf("expected Deleted=true")
	}
	if atomic.LoadInt32(&sb.deleteVersionHits) != 1 {
		t.Errorf("delete-version hits = %d, want 1", sb.deleteVersionHits)
	}
}

func TestBackupPush_RequiresProfile(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Errorf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--profile") {
		t.Errorf("message should mention --profile, got %q", err.Message)
	}
}

func TestAccountDelete_HappyPath(t *testing.T) {
	sb := newStorageBackend(t)
	st := stackForStorageBackend(sb)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	data, err := commands.RunAccount(commands.AccountFlags{Subcommand: "delete", Confirm: true})
	if err != nil {
		t.Fatalf("account delete: %+v", err)
	}
	if !data.(*commands.AccountDeleteResult).Deleted {
		t.Errorf("expected Deleted=true")
	}
	if atomic.LoadInt32(&sb.deleteAccountHits) != 1 {
		t.Errorf("delete account hits = %d, want 1", sb.deleteAccountHits)
	}
	// Local session should be wiped.
	if st.Auth.Session().SignedIn() {
		t.Error("expected session cleared after account delete")
	}
}

func TestStorage_FindManifestURL(t *testing.T) {
	urls := []storage.PresignedURL{
		{ChunkIndex: 0, PresignedURL: "https://r2/c0"},
		{ChunkIndex: -1, PresignedURL: "https://r2/manifest"},
		{ChunkIndex: 1, PresignedURL: "https://r2/c1"},
	}
	if got := storage.FindManifestURL(urls); got == nil || got.PresignedURL != "https://r2/manifest" {
		t.Errorf("FindManifestURL = %+v, want manifest entry", got)
	}
	if got := storage.FindChunkURL(urls, 1); got == nil || got.PresignedURL != "https://r2/c1" {
		t.Errorf("FindChunkURL(1) = %+v, want chunk-1 entry", got)
	}
}
