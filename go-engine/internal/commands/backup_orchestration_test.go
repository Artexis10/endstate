// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// TestBackupLogin_FullFlow drives the full login orchestration: pre-handshake
// returns the fixture salt + KDF params, the engine derives keys via real
// Argon2id, posts step 2, the server returns the fixture wrappedDEK, the
// engine unwraps and caches the DEK in the keychain.
func TestBackupLogin_FullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Argon2id v1 floor parameters are heavy; skipped in short mode")
	}
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})
	defer restore()
	defer commands.WithPassphraseReader(func(io.Reader) (string, error) { return "secret-pass", nil })()

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "login", Email: "user@example.com"})
	if err != nil {
		t.Fatalf("login: %+v", err)
	}
	res := data.(*commands.LoginResult)
	if res.UserID != "user-1" {
		t.Errorf("UserID = %q", res.UserID)
	}
	// DEK in keychain.
	storedDEK, kerr := kc.Load(keychain.AccountForDEK("user-1"))
	if kerr != nil {
		t.Fatalf("DEK not in keychain: %v", kerr)
	}
	f := loadFixture()
	if !bytes.Equal(storedDEK, f.DEK) {
		t.Errorf("stored DEK does not match fixture")
	}
	// Refresh token in keychain.
	if _, rerr := kc.Load(keychain.AccountForUser("user-1")); rerr != nil {
		t.Errorf("refresh token not in keychain: %v", rerr)
	}
}

// TestBackupLogin_WrongPassphrase: server returns 401 on step 2; engine
// surfaces the auth error and does NOT cache a DEK.
func TestBackupLogin_WrongPassphrase(t *testing.T) {
	if testing.Short() {
		t.Skip("Argon2id v1 floor parameters are heavy; skipped in short mode")
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                            srv.URL,
			"jwks_uri":                          srv.URL + "/api/.well-known/jwks.json",
			"id_token_signing_alg_values_supported": []string{"EdDSA"},
			"endstate_extensions": map[string]interface{}{
				"auth_login_endpoint":          srv.URL + "/api/auth/login",
				"auth_signup_endpoint":         srv.URL + "/api/auth/signup",
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
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		f := loadFixture()
		var raw map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if _, hasPwd := raw["serverPassword"]; hasPwd {
			http.Error(w, `{"success":false,"error":{"code":"AUTH_REQUIRED","message":"invalid credentials"}}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"salt": f.SaltB64,
			"kdfParams": map[string]interface{}{"algorithm": "argon2id", "memory": 65536, "iterations": 3, "parallelism": 4},
		})
	})

	kc := keychain.NewMemory()
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})
	defer restore()
	defer commands.WithPassphraseReader(func(io.Reader) (string, error) { return "secret-pass", nil })()

	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "login", Email: "user@example.com"})
	if err == nil {
		t.Fatal("expected an error from the 401 step-2 response")
	}
	// No DEK should be in the keychain.
	if _, kerr := kc.Load(keychain.AccountForDEK("user-1")); kerr == nil {
		t.Error("DEK should not be cached on a 401 login")
	}
}

// TestBackupLogout_ClearsRefreshAndDEK: after a successful login, logout
// clears both the refresh-token entry and the DEK entry in the keychain.
//
// We share the stack across login + logout so the SessionStore stays
// hydrated; the production CLI gets the same effect because it runs each
// command in a fresh process that hydrates from the keychain on startup.
func TestBackupLogout_ClearsRefreshAndDEK(t *testing.T) {
	if testing.Short() {
		t.Skip("Argon2id v1 floor parameters are heavy; skipped in short mode")
	}
	srv := fakeBackend(t)
	kc := keychain.NewMemory()
	stack := stackForBackend(srv, kc)
	restore := commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return stack })
	defer restore()
	defer commands.WithPassphraseReader(func(io.Reader) (string, error) { return "secret-pass", nil })()

	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "login", Email: "user@example.com"}); err != nil {
		t.Fatalf("login: %+v", err)
	}
	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "logout"}); err != nil {
		t.Fatalf("logout: %+v", err)
	}
	if _, kerr := kc.Load(keychain.AccountForUser("user-1")); kerr == nil {
		t.Error("refresh token should be gone after logout")
	}
	if _, kerr := kc.Load(keychain.AccountForDEK("user-1")); kerr == nil {
		t.Error("DEK should be gone after logout")
	}
}

// pushPullBackend extends storageBackend with R2-mock + create-version
// + download-urls handlers, used by the push and pull tests.
type pushPullBackend struct {
	srv     *httptest.Server
	r2      *httptest.Server
	createVersionFn   http.HandlerFunc
	downloadURLsFn    http.HandlerFunc
	versionsFn        http.HandlerFunc
	listFn            http.HandlerFunc
	createBackupFn    http.HandlerFunc

	mu          sync.Mutex
	r2Stored    map[string][]byte // key = chunk index ("manifest" or "0", "1"...)
	r2Latency   atomic.Int32
	r2FailFirst map[string]int // key → number of remaining 5xx attempts before success
	r2TamperOn  map[string]bool
}

func newPushPullBackend(t *testing.T) *pushPullBackend {
	t.Helper()
	pp := &pushPullBackend{
		r2Stored:    make(map[string][]byte),
		r2FailFirst: make(map[string]int),
		r2TamperOn:  make(map[string]bool),
	}

	// R2 mock — separate server so URLs are clearly distinct.
	r2mux := http.NewServeMux()
	r2 := httptest.NewServer(r2mux)
	pp.r2 = r2
	t.Cleanup(r2.Close)

	r2mux.HandleFunc("/r2/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/r2/")
		switch r.Method {
		case http.MethodPut:
			pp.mu.Lock()
			if remaining, ok := pp.r2FailFirst[key]; ok && remaining > 0 {
				pp.r2FailFirst[key] = remaining - 1
				pp.mu.Unlock()
				http.Error(w, "synthetic 5xx", http.StatusServiceUnavailable)
				return
			}
			body, _ := io.ReadAll(r.Body)
			cp := make([]byte, len(body))
			copy(cp, body)
			pp.r2Stored[key] = cp
			pp.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			pp.mu.Lock()
			data, ok := pp.r2Stored[key]
			tamper := pp.r2TamperOn[key]
			pp.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			if tamper {
				// Flip one byte (avoids zero-length ciphertexts).
				if len(data) > 0 {
					data = append([]byte(nil), data...)
					data[len(data)/2] ^= 0xFF
				}
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Substrate mock.
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	pp.srv = srv
	t.Cleanup(srv.Close)
	addAuthRoutes(mux, srv)

	// list backups
	mux.HandleFunc("/api/backups", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		switch r.Method {
		case http.MethodGet:
			if pp.listFn != nil {
				pp.listFn(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"backups": []map[string]interface{}{
					{"id": "b-1", "name": "default", "latestVersionId": "", "versionCount": 0, "totalSize": 0, "updatedAt": "2026-05-02T00:00:00Z"},
				},
			})
		case http.MethodPost:
			if pp.createBackupFn != nil {
				pp.createBackupFn(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"backupId": "b-new"})
		default:
			http.NotFound(w, r)
		}
	})

	// versions / create-version / download-urls / single-backup
	mux.HandleFunc("/api/backups/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		path := strings.TrimPrefix(r.URL.Path, "/api/backups/")
		segments := strings.Split(path, "/")

		// /api/backups/{id}/versions GET / POST
		if len(segments) == 2 && segments[1] == "versions" {
			switch r.Method {
			case http.MethodGet:
				if pp.versionsFn != nil {
					pp.versionsFn(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"versions": []map[string]interface{}{
						{"versionId": "v-1", "createdAt": "2026-05-02T00:00:00Z", "size": 0, "manifestSha256": ""},
					},
				})
				return
			case http.MethodPost:
				if pp.createVersionFn != nil {
					pp.createVersionFn(w, r)
					return
				}
				// Default: read posted chunkMetadata, mint URLs pointing
				// at our R2 mock. Manifest at chunkIndex=-1.
				var body struct {
					ChunkMetadata []struct {
						Index uint32 `json:"index"`
					} `json:"chunkMetadata"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				urls := []map[string]interface{}{
					{"chunkIndex": -1, "presignedUrl": pp.r2.URL + "/r2/manifest", "expiresAt": "2026-05-02T01:00:00Z"},
				}
				for _, c := range body.ChunkMetadata {
					urls = append(urls, map[string]interface{}{
						"chunkIndex":   c.Index,
						"presignedUrl": fmt.Sprintf("%s/r2/%d", pp.r2.URL, c.Index),
						"expiresAt":    "2026-05-02T01:00:00Z",
					})
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"versionId":  "v-pushed",
					"uploadUrls": urls,
				})
				return
			}
		}

		// /api/backups/{id}/versions/{vid}/download-urls
		if len(segments) == 4 && segments[1] == "versions" && segments[3] == "download-urls" && r.Method == http.MethodPost {
			if pp.downloadURLsFn != nil {
				pp.downloadURLsFn(w, r)
				return
			}
			var body struct {
				ChunkIndices []int `json:"chunkIndices"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			urls := make([]map[string]interface{}, 0, len(body.ChunkIndices))
			for _, idx := range body.ChunkIndices {
				if idx == storage.ManifestChunkIndex {
					urls = append(urls, map[string]interface{}{
						"chunkIndex": -1, "presignedUrl": pp.r2.URL + "/r2/manifest", "expiresAt": "2026-05-02T01:00:00Z",
					})
					continue
				}
				urls = append(urls, map[string]interface{}{
					"chunkIndex": idx, "presignedUrl": fmt.Sprintf("%s/r2/%d", pp.r2.URL, idx), "expiresAt": "2026-05-02T01:00:00Z",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"urls": urls})
			return
		}

		// /api/backups/{id} DELETE — we don't need it for these tests.
		http.NotFound(w, r)
	})

	return pp
}

func stackForPushPull(pp *pushPullBackend) (*backup.Stack, keychain.Keychain) {
	kc := keychain.NewMemory()
	st := stackForBackend(pp.srv, kc)
	// Pre-seed a session with the fixture's user + a real DEK + the
	// matching wrappedDEK so push can populate the manifest field.
	st.Auth.Session().SetTokens("user-1", "user@example.com", "access-1", "refresh-1", "active", noTime())
	f := loadFixture()
	if err := st.Session.StoreDEK(f.DEK); err != nil {
		panic("test setup: storeDEK: " + err.Error())
	}
	if err := st.Session.StoreWrappedDEK(f.WrappedDEKB64); err != nil {
		panic("test setup: storeWrappedDEK: " + err.Error())
	}
	return st, kc
}

func TestBackupPush_HappyPath(t *testing.T) {
	pp := newPushPullBackend(t)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	tmp := t.TempDir()
	profile := filepath.Join(tmp, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "manifest.jsonc"), []byte(`{"name":"smoke"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "extra.txt"), bytes.Repeat([]byte("A"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1"})
	if err != nil {
		t.Fatalf("push: %+v", err)
	}
	res := data.(*commands.PushResult)
	if res.VersionID != "v-pushed" {
		t.Errorf("VersionID = %q, want v-pushed", res.VersionID)
	}
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if _, ok := pp.r2Stored["manifest"]; !ok {
		t.Error("manifest not received by R2 mock")
	}
	if _, ok := pp.r2Stored["0"]; !ok {
		t.Error("chunk 0 not received by R2 mock")
	}
}

func TestBackupPush_RetryOn5xx(t *testing.T) {
	pp := newPushPullBackend(t)
	pp.r2FailFirst["0"] = 1 // first PUT to chunk 0 returns 5xx; second succeeds
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	tmp := t.TempDir()
	profile := filepath.Join(tmp, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "f"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1"})
	if err != nil {
		t.Fatalf("push: %+v", err)
	}
	if data == nil {
		t.Fatal("expected push result")
	}
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if _, ok := pp.r2Stored["0"]; !ok {
		t.Error("chunk 0 not received after retry")
	}
}

func TestBackupPull_HappyRoundtrip(t *testing.T) {
	pp := newPushPullBackend(t)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	tmp := t.TempDir()
	profile := filepath.Join(tmp, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "manifest.jsonc"), []byte(`{"name":"smoke"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(profile, "configs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte{0xAB}, 4096)
	if err := os.WriteFile(filepath.Join(subdir, "blob.bin"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1"}); err != nil {
		t.Fatalf("push: %+v", err)
	}

	// Override versionsFn so list returns the version we just pushed.
	pp.versionsFn = func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"versions": []map[string]interface{}{
				{"versionId": "v-pushed", "createdAt": "2026-05-02T00:00:00Z", "size": 0, "manifestSha256": ""},
			},
		})
	}

	target := filepath.Join(tmp, "restored")
	data, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "pull", BackupID: "b-1", VersionID: "v-pushed", To: target, Overwrite: true,
	})
	if err != nil {
		t.Fatalf("pull: %+v", err)
	}
	res := data.(*commands.PullResult)
	if res.WrittenTo != target {
		t.Errorf("WrittenTo = %q, want %q", res.WrittenTo, target)
	}

	got, gerr := os.ReadFile(filepath.Join(target, "manifest.jsonc"))
	if gerr != nil {
		t.Fatalf("read restored manifest.jsonc: %v", gerr)
	}
	if !bytes.Equal(got, []byte(`{"name":"smoke"}`)) {
		t.Errorf("restored manifest.jsonc bytes mismatch")
	}
	gotBlob, _ := os.ReadFile(filepath.Join(target, "configs", "blob.bin"))
	if !bytes.Equal(gotBlob, body) {
		t.Errorf("restored blob.bin bytes mismatch")
	}
}

func TestBackupPull_RefusesOverwriteWithoutFlag(t *testing.T) {
	pp := newPushPullBackend(t)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "restored")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "pull", BackupID: "b-1", VersionID: "v-pushed", To: target,
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "already exists") {
		t.Errorf("message %q should mention 'already exists'", err.Message)
	}
}

func TestBackupPull_TamperedChunkRefusesDecrypt(t *testing.T) {
	pp := newPushPullBackend(t)
	st, _ := stackForPushPull(pp)
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	tmp := t.TempDir()
	profile := filepath.Join(tmp, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "f"), []byte("important data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commands.RunBackup(commands.BackupFlags{Subcommand: "push", Profile: profile, BackupID: "b-1"}); err != nil {
		t.Fatalf("push: %+v", err)
	}

	// Configure R2 mock to flip a byte in chunk 0 on the next GET.
	pp.mu.Lock()
	pp.r2TamperOn["0"] = true
	pp.mu.Unlock()

	target := filepath.Join(tmp, "restored")
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "pull", BackupID: "b-1", VersionID: "v-pushed", To: target, Overwrite: true,
	})
	if err == nil {
		t.Fatal("expected an integrity error on tampered chunk")
	}
	if !strings.Contains(err.Message, "SHA-256 mismatch") {
		t.Errorf("message %q should mention SHA-256 mismatch", err.Message)
	}
	// No restored content on disk.
	if _, statErr := os.Stat(filepath.Join(target, "f")); statErr == nil {
		t.Error("restored file should not exist after a SHA-256 mismatch")
	}
}

// TestBackupRecover_FullFlow drives Recover + RecoverFinalize end-to-end
// using a deterministic mnemonic and a minimal substrate mock that
// returns the fixture's recoveryKeyWrappedDEK.
func TestBackupRecover_FullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Argon2id v1 floor parameters are heavy; skipped in short mode")
	}
	f := loadFixture()

	// Compute the recovery materials matching the well-known test mnemonic.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	rkBytes, perr := crypto.ParseRecoveryPhrase(mnemonic)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	recoveryKey, drErr := crypto.DeriveRecoveryKey(rkBytes, f.Salt, crypto.DefaultKDFParams())
	if drErr != nil {
		t.Fatalf("derive recovery: %v", drErr)
	}
	rkWrappedDEK, rwErr := crypto.WrapDEK(f.DEK, recoveryKey)
	if rwErr != nil {
		t.Fatalf("wrap: %v", rwErr)
	}

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addAuthRoutes(mux, srv)

	mux.HandleFunc("/api/auth/recover", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"recoveryKeyWrappedDEK": base64.StdEncoding.EncodeToString(rkWrappedDEK),
		})
	})
	mux.HandleFunc("/api/auth/recover/finalize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"userId":             "user-1",
			"accessToken":        "access-recovered",
			"refreshToken":       "refresh-recovered",
			"subscriptionStatus": "active",
		})
	})

	kc := keychain.NewMemory()
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(srv, kc)
	})()
	defer commands.WithRecoveryReader(func(io.Reader) (string, string, error) {
		return mnemonic, "new-passphrase-secret", nil
	})()

	data, err := commands.RunBackup(commands.BackupFlags{Subcommand: "recover", Email: "user@example.com"})
	if err != nil {
		t.Fatalf("recover: %+v", err)
	}
	res := data.(*commands.RecoverResult)
	if res.UserID != "user-1" {
		t.Errorf("UserID = %q", res.UserID)
	}
	if _, kerr := kc.Load(keychain.AccountForDEK("user-1")); kerr != nil {
		t.Error("DEK should be cached after recovery")
	}
	if rt, kerr := kc.Load(keychain.AccountForUser("user-1")); kerr != nil || string(rt) != "refresh-recovered" {
		t.Errorf("refresh token in keychain = %q, %v; want refresh-recovered", rt, kerr)
	}
}

// TestBackupPull_NoDEKSurfacesAuthRequired asserts a clean envelope when
// the user has no cached DEK (e.g. after logout). Cheap; skipped in
// short mode is unnecessary because no Argon2id is invoked.
func TestBackupPull_NoDEKSurfacesAuthRequired(t *testing.T) {
	pp := newPushPullBackend(t)
	kc := keychain.NewMemory()
	st := stackForBackend(pp.srv, kc)
	st.Auth.Session().SetTokens("user-1", "user@example.com", "access-1", "refresh-1", "active", noTime())
	// Note: no StoreDEK call.
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack { return st })()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "restored")
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "pull", BackupID: "b-1", VersionID: "v-1", To: target,
	})
	if err == nil || err.Code != envelope.ErrAuthRequired {
		t.Fatalf("got %+v, want AUTH_REQUIRED", err)
	}
}

// noTime returns a zero time; signals "no expiry tracking" in SetTokens.
func noTime() (z time.Time) { return z }
