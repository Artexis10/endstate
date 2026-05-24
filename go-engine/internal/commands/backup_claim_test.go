// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

const validClaimTokenFixture = "AAAABBBBCCCCDDDDEEEEFFFFGGGGHHHHIIIIJJJJKKK" // 43 chars, URL-safe base64 alphabet

// claimBackend is the test fixture for `/api/auth/claim`. Mounts the
// auth-discovery routes (issuer, jwks, login, etc.) on a fresh mux +
// httptest.Server so claim tests don't share mutable state with other
// suites. The claim handler captures inbound requests so tests can
// assert on the Authorization header, body shape, and hit count.
type claimBackend struct {
	srv       *httptest.Server
	hits      int32
	claimFn   http.HandlerFunc
	lastAuth  string
	lastBody  []byte
	captureMu chan struct{} // single-slot mutex; tests don't need finer concurrency
}

func newClaimBackend(t *testing.T) *claimBackend {
	t.Helper()
	cb := &claimBackend{captureMu: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	cb.srv = srv
	t.Cleanup(srv.Close)
	addAuthRoutes(mux, srv)
	mux.HandleFunc("/api/auth/claim", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		atomic.AddInt32(&cb.hits, 1)
		body, _ := io.ReadAll(r.Body)
		cb.captureMu <- struct{}{}
		cb.lastAuth = r.Header.Get("Authorization")
		cb.lastBody = body
		<-cb.captureMu
		if cb.claimFn != nil {
			cb.claimFn(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"userId":             "user-claim-1",
			"email":              "buyer@example.com",
			"accessToken":        "access-claim",
			"refreshToken":       "refresh-claim",
			"subscriptionStatus": "active",
		})
	})
	return cb
}

func TestBackupClaim_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Argon2id v1 floor parameters are heavy; skipped in short mode")
	}
	cb := newClaimBackend(t)
	kc := keychain.NewMemory()
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(cb.srv, kc)
	})()
	defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
		return "secret-pass", "", nil
	})()

	tmp := t.TempDir()
	recoveryPath := filepath.Join(tmp, "recovery.txt")

	data, err := commands.RunBackup(commands.BackupFlags{
		Subcommand:     "claim",
		Token:          validClaimTokenFixture,
		SaveRecoveryTo: recoveryPath,
	})
	if err != nil {
		t.Fatalf("claim: %+v", err)
	}
	res, ok := data.(*commands.SignupResult)
	if !ok {
		t.Fatalf("data type = %T", data)
	}
	// Server-supplied email is what the engine surfaces, lowercased.
	if res.Email != "buyer@example.com" {
		t.Errorf("Email = %q, want buyer@example.com (server-supplied)", res.Email)
	}
	if res.UserID != "user-claim-1" {
		t.Errorf("UserID = %q, want user-claim-1", res.UserID)
	}
	if res.SubscriptionStatus != "active" {
		t.Errorf("SubscriptionStatus = %q, want active", res.SubscriptionStatus)
	}
	if res.RecoveryKeySavedTo != recoveryPath {
		t.Errorf("RecoveryKeySavedTo = %q, want %q", res.RecoveryKeySavedTo, recoveryPath)
	}
	// Recovery file present, contains 24 BIP39 words.
	body, statErr := os.ReadFile(recoveryPath)
	if statErr != nil {
		t.Fatalf("recovery file missing: %v", statErr)
	}
	mnemonicLine := lastNonHashLine(string(body))
	if got := len(strings.Fields(mnemonicLine)); got != 24 {
		t.Errorf("recovery file mnemonic word count = %d, want 24", got)
	}
	// Refresh token + DEK persisted in keychain under the server-supplied userId.
	if _, kerr := kc.Load(keychain.AccountForUser("user-claim-1")); kerr != nil {
		t.Errorf("refresh token not in keychain: %v", kerr)
	}
	if _, kerr := kc.Load(keychain.AccountForDEK("user-claim-1")); kerr != nil {
		t.Errorf("DEK not in keychain: %v", kerr)
	}
	// Exactly one claim call, bearer header set, body has no email field.
	if atomic.LoadInt32(&cb.hits) != 1 {
		t.Errorf("claim hits = %d, want 1", cb.hits)
	}
	wantAuth := "Bearer " + validClaimTokenFixture
	if cb.lastAuth != wantAuth {
		t.Errorf("Authorization header = %q, want %q", cb.lastAuth, wantAuth)
	}
	var bodyMap map[string]interface{}
	if jerr := json.Unmarshal(cb.lastBody, &bodyMap); jerr != nil {
		t.Fatalf("request body decode: %v (body=%q)", jerr, cb.lastBody)
	}
	if _, present := bodyMap["email"]; present {
		t.Errorf("request body should NOT include email; got: %v", bodyMap)
	}
	for _, want := range []string{"serverPassword", "salt", "kdfParams", "wrappedDEK", "recoveryKeyVerifier", "recoveryKeyWrappedDEK"} {
		if _, present := bodyMap[want]; !present {
			t.Errorf("request body missing %q (full body: %v)", want, bodyMap)
		}
	}
}

func TestBackupClaim_RequiresToken(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "claim"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--token") {
		t.Errorf("error message %q should mention --token", err.Message)
	}
}

func TestBackupClaim_RejectsMalformedToken(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "claim",
		Token:      "too-short",
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "43 characters") {
		t.Errorf("error message %q should mention '43 characters'", err.Message)
	}
}

func TestBackupClaim_RejectsTokenWithBadAlphabet(t *testing.T) {
	// Length 43 but includes '+' (not in the URL-safe alphabet).
	bad := strings.Repeat("A", 42) + "+"
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "claim",
		Token:      bad,
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "URL-safe base64") {
		t.Errorf("error message %q should mention URL-safe base64", err.Message)
	}
}

func TestBackupClaim_RequiresSaveRecoveryToWhenGenerating(t *testing.T) {
	defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
		return "secret-pass", "", nil
	})()
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "claim",
		Token:      validClaimTokenFixture,
		// SaveRecoveryTo deliberately empty
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--save-recovery-to") {
		t.Errorf("error message %q should mention --save-recovery-to", err.Message)
	}
}

func TestBackupClaim_RequiresPassphrase(t *testing.T) {
	defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
		return "", "", nil
	})()
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand:     "claim",
		Token:          validClaimTokenFixture,
		SaveRecoveryTo: filepath.Join(t.TempDir(), "r.txt"),
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "passphrase") {
		t.Errorf("error message %q should mention passphrase", err.Message)
	}
}

func TestBackupClaim_RecoveryFileWriteFailureNoNetworkCall(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises real KDF before the failing write; heavy in short mode")
	}
	cb := newClaimBackend(t)
	kc := keychain.NewMemory()
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(cb.srv, kc)
	})()
	defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
		return "secret-pass", "", nil
	})()

	// Make the parent path unwritable by pointing it at an existing
	// file rather than a directory — writeRecoveryFile's MkdirAll
	// will fail because the parent path component is a regular file.
	tmp := t.TempDir()
	regularFile := filepath.Join(tmp, "blocking-file")
	if werr := os.WriteFile(regularFile, []byte("blocker"), 0o600); werr != nil {
		t.Fatalf("seed blocker file: %v", werr)
	}
	recoveryPath := filepath.Join(regularFile, "child", "recovery.txt")

	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand:     "claim",
		Token:          validClaimTokenFixture,
		SaveRecoveryTo: recoveryPath,
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if atomic.LoadInt32(&cb.hits) != 0 {
		t.Errorf("claim hits = %d, want 0 (write-before-network invariant)", cb.hits)
	}
}

// errorCodePassthrough exercises the four substrate claim error codes
// via the engine envelope. Each fixture sets the response status, the
// body code, and asserts the engine surfaces the code verbatim in
// envelope.error.code.
func TestBackupClaim_SubstrateErrorCodesSurfaceVerbatim(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy KDF runs precede the error path; skipped in short mode")
	}
	cases := []struct {
		name     string
		status   int
		bodyCode string
	}{
		{"ClaimTokenInvalid_401", http.StatusUnauthorized, "CLAIM_TOKEN_INVALID"},
		{"ClaimTokenExpired_401", http.StatusUnauthorized, "CLAIM_TOKEN_EXPIRED"},
		{"ClaimTokenConsumed_409", http.StatusConflict, "CLAIM_TOKEN_CONSUMED"},
		{"KdfTooWeak_400", http.StatusBadRequest, "KDF_TOO_WEAK"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cb := newClaimBackend(t)
			cb.claimFn = func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error": map[string]interface{}{
						"code":    tc.bodyCode,
						"message": "substrate raw: " + tc.bodyCode,
					},
				})
			}
			kc := keychain.NewMemory()
			defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
				return stackForBackend(cb.srv, kc)
			})()
			defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
				return "secret-pass", "", nil
			})()

			tmp := t.TempDir()
			_, err := commands.RunBackup(commands.BackupFlags{
				Subcommand:     "claim",
				Token:          validClaimTokenFixture,
				SaveRecoveryTo: filepath.Join(tmp, "recovery.txt"),
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if string(err.Code) != tc.bodyCode {
				t.Errorf("envelope.error.code = %q, want %q (status %d)", err.Code, tc.bodyCode, tc.status)
			}
		})
	}
}
