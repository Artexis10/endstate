// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// validDiscovery returns a discovery document whose backup_api_base is
// configurable per test. issuer is the OIDC issuer URL; backupBase is
// what the test wants advertised under endstate_extensions.
func validDiscovery(issuer, backupBase string) map[string]interface{} {
	return map[string]interface{}{
		"issuer":                                issuer,
		"jwks_uri":                              issuer + "/api/.well-known/jwks.json",
		"id_token_signing_alg_values_supported": []string{"EdDSA"},
		"endstate_extensions": map[string]interface{}{
			"auth_signup_endpoint":        issuer + "/api/auth/signup",
			"auth_login_endpoint":         issuer + "/api/auth/login",
			"auth_refresh_endpoint":       issuer + "/api/auth/refresh",
			"auth_logout_endpoint":        issuer + "/api/auth/logout",
			"auth_recover_endpoint":       issuer + "/api/auth/recover",
			"backup_api_base":             backupBase,
			"supported_kdf_algorithms":    []string{"argon2id"},
			"supported_envelope_versions": []int{1},
			"min_kdf_params":              map[string]int{"memory": 65536, "iterations": 3, "parallelism": 4},
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

// TestCommitVersion_HonorsBackupAPIBase confirms the commit call is routed
// through the discovery-advertised backup_api_base exactly like its sibling
// calls (contract §9), and advertises the engine's schema version so the
// backend can decide whether the version needs an explicit commit.
func TestCommitVersion_HonorsBackupAPIBase(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	customPath := "/v1/private/backups"
	customBase := srv.URL + customPath

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, customBase))
	})

	var seenPath, seenMethod, seenClientVersion string
	mux.HandleFunc(customPath+"/b-1/versions/v-1/commit", func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenMethod = r.Method
		seenClientVersion = r.Header.Get("X-Endstate-API-Version")
		w.Header().Set("X-Endstate-API-Version", "2.1")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"versionId": "v-1", "committedAt": "2026-06-01T00:00:00Z"})
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	hc := client.New(client.Options{Tokens: client.Anonymous{}})
	st := storage.New(srv.URL, oc, hc)

	acked, err := st.CommitVersion(context.Background(), "b-1", "v-1")
	if err != nil {
		t.Fatalf("CommitVersion: %+v", err)
	}
	if !acked {
		t.Error("acknowledged = false, want true on a 2xx commit")
	}
	if seenPath != customPath+"/b-1/versions/v-1/commit" {
		t.Errorf("commit hit %q, want it under %q (backup_api_base must be honored)", seenPath, customBase)
	}
	if seenMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", seenMethod)
	}
	if seenClientVersion != client.EngineSchemaVersion() {
		t.Errorf("request X-Endstate-API-Version = %q, want %q", seenClientVersion, client.EngineSchemaVersion())
	}
}

// TestCreateVersion_AdvertisesEngineSchemaVersion: substrate decides
// whether a version requires a commit from the client's advertised minor on
// the request that CREATES it, and its parser fails closed — an absent or
// unparseable header means "publish at creation", i.e. the pre-2.1
// behaviour. If this header ever goes missing here, the two-phase commit is
// inert while every other test still passes, so it is pinned at the call
// site substrate actually reads and not only in the shared client.
func TestCreateVersion_AdvertisesEngineSchemaVersion(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, srv.URL+"/api/backups"))
	})

	var seenClientVersion string
	mux.HandleFunc("/api/backups/b-1/versions", func(w http.ResponseWriter, r *http.Request) {
		seenClientVersion = r.Header.Get("X-Endstate-API-Version")
		w.Header().Set("X-Endstate-API-Version", "2.1")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"versionId": "v-1",
			"uploadUrls": []map[string]interface{}{
				{"chunkIndex": -1, "presignedUrl": srv.URL + "/blob/manifest", "expiresAt": "2026-06-01T00:00:00Z"},
				{"chunkIndex": 0, "presignedUrl": srv.URL + "/blob/0", "expiresAt": "2026-06-01T00:00:00Z"},
			},
		})
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	hc := client.New(client.Options{Tokens: client.Anonymous{}})
	st := storage.New(srv.URL, oc, hc)

	meta := []storage.ChunkMetaWire{{Index: 0, EncryptedSize: 16, SHA256: strings.Repeat("a", 64)}}
	if _, err := st.CreateVersion(context.Background(), "b-1", []byte("enc-manifest"), meta); err != nil {
		t.Fatalf("CreateVersion: %+v", err)
	}
	if seenClientVersion != client.EngineSchemaVersion() {
		t.Errorf("CreateVersion request X-Endstate-API-Version = %q, want %q — substrate fails closed without it and never gates the version",
			seenClientVersion, client.EngineSchemaVersion())
	}
}

func TestCreateVersion_ParsesRequiresCommit(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, srv.URL+"/api/backups"))
	})
	mux.HandleFunc("/api/backups/b-1/versions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.1")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"versionId": "v-1", "requiresCommit": true,
			"uploadUrls": []map[string]interface{}{{"chunkIndex": -1, "presignedUrl": srv.URL + "/blob/manifest"}, {"chunkIndex": 0, "presignedUrl": srv.URL + "/blob/0"}},
		})
	})

	st := storage.New(srv.URL, oidc.NewClient(srv.URL, srv.Client()), client.New(client.Options{Tokens: client.Anonymous{}}))
	got, err := st.CreateVersion(context.Background(), "b-1", []byte("enc-manifest"), []storage.ChunkMetaWire{{Index: 0}})
	if err != nil {
		t.Fatalf("CreateVersion: %+v", err)
	}
	if !got.RequiresCommit {
		t.Error("RequiresCommit = false, want true")
	}
}

// TestCreateVersionWithReplay_AbsentCapabilityRefusesPersistedOperation
// protects a scheduled replay from being sent to a backend that did not
// advertise the operation-replay protocol. A persisted operation ID must
// never turn into an ordinary create POST after discovery changes or loses
// that optional capability.
func TestCreateVersionWithReplay_AbsentCapabilityRefusesPersistedOperation(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// validDiscovery intentionally omits backup_api_capabilities.
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, srv.URL+"/api/backups"))
	})

	createPosts := 0
	mux.HandleFunc("/api/backups/b-1/versions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			createPosts++
		}
		http.Error(w, "create should not be sent", http.StatusInternalServerError)
	})

	st := storage.New(srv.URL, oidc.NewClient(srv.URL, srv.Client()), client.New(client.Options{Tokens: client.Anonymous{}}))
	_, err := st.CreateVersionWithReplay(context.Background(), "b-1", []byte("enc-manifest"), []storage.ChunkMetaWire{{Index: 0}}, "persisted-operation-id")
	if err == nil {
		t.Fatal("CreateVersionWithReplay unexpectedly accepted a persisted operation without replay capability")
	}
	if err.Code != envelope.ErrBackendIncompatible {
		t.Errorf("CreateVersionWithReplay error code = %q, want %q", err.Code, envelope.ErrBackendIncompatible)
	}
	if createPosts != 0 {
		t.Errorf("create POSTs = %d, want 0 when discovery omits replay capability", createPosts)
	}
}

// TestCommitVersion_404IsFailure: a call to commit is only made after the
// create response explicitly required it. A missing route in that state is a
// failed durability transition, not evidence that an older backend accepted
// create-as-durable semantics.
func TestCommitVersion_404IsFailure(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, srv.URL+"/api/backups"))
	})
	// No /commit handler registered → net/http answers 404.
	mux.HandleFunc("/api/backups/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		http.Error(w, `{"success":false,"error":{"code":"NOT_FOUND","message":"no such route"}}`, http.StatusNotFound)
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	hc := client.New(client.Options{Tokens: client.Anonymous{}})
	st := storage.New(srv.URL, oc, hc)

	acked, err := st.CommitVersion(context.Background(), "b-1", "v-1")
	if err == nil {
		t.Fatal("a required commit 404 must be an error")
	}
	if acked {
		t.Error("acknowledged = true, want false when commit failed")
	}
}

// TestCommitVersion_ServerErrorSurfaces: any non-404 failure is a real
// error. The caller must not report the generation as protected.
func TestCommitVersion_ServerErrorSurfaces(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, srv.URL+"/api/backups"))
	})
	mux.HandleFunc("/api/backups/b-1/versions/v-1/commit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.1")
		http.Error(w, `{"success":false,"error":{"code":"BACKEND_ERROR","message":"commit failed"}}`, http.StatusInternalServerError)
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	rp := client.RetryPolicy{MaxRetries: 0, InitialWait: time.Millisecond, MaxWait: time.Millisecond}
	hc := client.New(client.Options{Tokens: client.Anonymous{}, Retry: &rp})
	st := storage.New(srv.URL, oc, hc)

	acked, err := st.CommitVersion(context.Background(), "b-1", "v-1")
	if err == nil {
		t.Fatal("expected a 500 commit to surface an error")
	}
	if acked {
		t.Error("acknowledged = true, want false on a failed commit")
	}
}

func TestListBackups_RejectsIssuerMismatchBeforeStorageRequest(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := validDiscovery(srv.URL, srv.URL+"/api/backups")
		doc["issuer"] = "https://wrong-issuer.example.com"
		_ = json.NewEncoder(w).Encode(doc)
	})

	storageRequests := 0
	mux.HandleFunc("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		storageRequests++
		http.Error(w, "storage must not be called", http.StatusInternalServerError)
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	hc := client.New(client.Options{Tokens: client.Anonymous{}})
	st := storage.New(srv.URL, oc, hc)

	_, err := st.ListBackups(context.Background())
	if err == nil {
		t.Fatal("ListBackups() error = nil, want BACKEND_INCOMPATIBLE")
	}
	if err.Code != envelope.ErrBackendIncompatible {
		t.Errorf("ListBackups() error code = %q, want %q", err.Code, envelope.ErrBackendIncompatible)
	}
	if storageRequests != 0 {
		t.Errorf("storage requests = %d, want 0", storageRequests)
	}
}

func TestListBackups_RejectsBelowFloorDiscoveryBeforeStorageRequest(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := validDiscovery(srv.URL, srv.URL+"/api/backups")
		doc["endstate_extensions"].(map[string]interface{})["min_kdf_params"] = map[string]int{"memory": 32768, "iterations": 3, "parallelism": 4}
		_ = json.NewEncoder(w).Encode(doc)
	})

	storageRequests := 0
	mux.HandleFunc("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		storageRequests++
		http.Error(w, "storage must not be called", http.StatusInternalServerError)
	})

	st := storage.New(srv.URL, oidc.NewClient(srv.URL, srv.Client()), client.New(client.Options{Tokens: client.Anonymous{}}))
	_, err := st.ListBackups(context.Background())
	if err == nil {
		t.Fatal("ListBackups() error = nil, want BACKEND_INCOMPATIBLE")
	}
	if err.Code != envelope.ErrBackendIncompatible {
		t.Errorf("ListBackups() error code = %q, want %q", err.Code, envelope.ErrBackendIncompatible)
	}
	if storageRequests != 0 {
		t.Errorf("storage requests = %d, want 0", storageRequests)
	}
}

func TestListBackups_RejectsCachedDiscoveryHTTPFailureBeforeStorageRequest(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	discoveryAvailable := true
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryAvailable {
			http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, srv.URL+"/custom/backups"))
	})

	storageRequests := 0
	mux.HandleFunc("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		storageRequests++
		http.Error(w, "storage must not be called", http.StatusInternalServerError)
	})

	oc := oidc.NewClient(srv.URL, srv.Client())
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	oc.SetClock(func() time.Time { return now })
	if _, err := oc.Discovery(context.Background()); err != nil {
		t.Fatalf("warm discovery: %v", err)
	}
	discoveryAvailable = false
	now = now.Add(oidc.DiscoveryTTL + time.Second)

	st := storage.New(srv.URL, oc, client.New(client.Options{Tokens: client.Anonymous{}}))
	_, err := st.ListBackups(context.Background())
	if err == nil {
		t.Fatal("ListBackups() error = nil, want BACKEND_UNREACHABLE")
	}
	if err.Code != envelope.ErrBackendUnreachable {
		t.Errorf("ListBackups() error code = %q, want %q", err.Code, envelope.ErrBackendUnreachable)
	}
	if storageRequests != 0 {
		t.Errorf("storage requests = %d, want 0", storageRequests)
	}
}

func TestListBackups_FallsBackToIssuerAfterCachedDiscoveryTransportFailure(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(validDiscovery(srv.URL, srv.URL+"/custom/backups"))
	})

	var seenPath string
	mux.HandleFunc("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"backups": []interface{}{}})
	})

	discoveryReachable := true
	httpc := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/.well-known/openid-configuration" && !discoveryReachable {
			return nil, errors.New("discovery unavailable")
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
	oc := oidc.NewClient(srv.URL, httpc)
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	oc.SetClock(func() time.Time { return now })
	if _, err := oc.Discovery(context.Background()); err != nil {
		t.Fatalf("warm discovery: %v", err)
	}
	discoveryReachable = false
	now = now.Add(oidc.DiscoveryTTL + time.Second)

	st := storage.New(srv.URL, oc, client.New(client.Options{Tokens: client.Anonymous{}}))
	if _, err := st.ListBackups(context.Background()); err != nil {
		t.Fatalf("ListBackups() fallback error: %+v", err)
	}
	if seenPath != "/api/backups" {
		t.Errorf("fallback path = %q, want /api/backups", seenPath)
	}
}
