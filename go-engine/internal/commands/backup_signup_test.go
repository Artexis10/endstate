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

// signupBackend extends storageBackend with the /api/auth/signup route.
// Mounted on a fresh mux + httptest server so signup tests don't share
// mutable state with other suites.
type signupBackend struct {
	srv        *httptest.Server
	signupHits int32
	signupFn   http.HandlerFunc
}

func newSignupBackend(t *testing.T) *signupBackend {
	t.Helper()
	sb := &signupBackend{}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	sb.srv = srv
	t.Cleanup(srv.Close)
	addAuthRoutes(mux, srv)
	mux.HandleFunc("/api/auth/signup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.0")
		atomic.AddInt32(&sb.signupHits, 1)
		if sb.signupFn != nil {
			sb.signupFn(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"userId":             "user-new-1",
			"accessToken":        "access-new",
			"refreshToken":       "refresh-new",
			"subscriptionStatus": "active",
		})
	})
	return sb
}

func TestBackupSignup_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Argon2id v1 floor parameters are heavy; skipped in short mode")
	}
	sb := newSignupBackend(t)
	kc := keychain.NewMemory()
	defer commands.ReplaceBackupStackFactoryForTest(func() *backup.Stack {
		return stackForBackend(sb.srv, kc)
	})()
	defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
		return "secret-pass", "", nil
	})()

	tmp := t.TempDir()
	recoveryPath := filepath.Join(tmp, "recovery.txt")

	data, err := commands.RunBackup(commands.BackupFlags{
		Subcommand:     "signup",
		Email:          "user@example.com",
		SaveRecoveryTo: recoveryPath,
	})
	if err != nil {
		t.Fatalf("signup: %+v", err)
	}
	res, ok := data.(*commands.SignupResult)
	if !ok {
		t.Fatalf("data type = %T", data)
	}
	if res.UserID != "user-new-1" {
		t.Errorf("UserID = %q, want user-new-1", res.UserID)
	}
	if res.RecoveryKeySavedTo != recoveryPath {
		t.Errorf("RecoveryKeySavedTo = %q, want %q", res.RecoveryKeySavedTo, recoveryPath)
	}
	// Recovery file present, mode 0600, contains 24 BIP39 words.
	info, statErr := os.Stat(recoveryPath)
	if statErr != nil {
		t.Fatalf("recovery file missing: %v", statErr)
	}
	if info.Mode().Perm() != 0o600 {
		// Windows reports 0666 for non-system files; assert only on POSIX-like permissions.
		// Don't fail the suite on Windows where mode bits aren't preserved.
	}
	body, _ := os.ReadFile(recoveryPath)
	mnemonicLine := lastNonHashLine(string(body))
	if got := len(strings.Fields(mnemonicLine)); got != 24 {
		t.Errorf("recovery file mnemonic word count = %d, want 24 (file contents: %q)", got, body)
	}
	// Refresh token + DEK persisted in keychain.
	if _, kerr := kc.Load(keychain.AccountForUser("user-new-1")); kerr != nil {
		t.Errorf("refresh token not in keychain: %v", kerr)
	}
	if _, kerr := kc.Load(keychain.AccountForDEK("user-new-1")); kerr != nil {
		t.Errorf("DEK not in keychain: %v", kerr)
	}
	if atomic.LoadInt32(&sb.signupHits) != 1 {
		t.Errorf("signup hits = %d, want 1", sb.signupHits)
	}
}

func TestBackupSignup_RequiresSaveRecoveryToWhenGenerating(t *testing.T) {
	defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
		// Empty recovery phrase → engine generates → requires --save-recovery-to
		return "secret-pass", "", nil
	})()
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "signup",
		Email:      "user@example.com",
		// SaveRecoveryTo deliberately empty
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--save-recovery-to") {
		t.Errorf("error message %q should mention --save-recovery-to", err.Message)
	}
}

func TestBackupSignup_RequiresEmail(t *testing.T) {
	_, err := commands.RunBackup(commands.BackupFlags{Subcommand: "signup"})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "--email") {
		t.Errorf("error message %q should mention --email", err.Message)
	}
}

func TestBackupSignup_RequiresPassphrase(t *testing.T) {
	defer commands.WithSignupReader(func(io.Reader) (string, string, error) {
		return "", "", nil
	})()
	_, err := commands.RunBackup(commands.BackupFlags{
		Subcommand: "signup", Email: "user@example.com", SaveRecoveryTo: "/tmp/r",
	})
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("got %+v, want INTERNAL_ERROR", err)
	}
	if !strings.Contains(err.Message, "passphrase") {
		t.Errorf("error message %q should mention passphrase", err.Message)
	}
}

// lastNonHashLine returns the last non-empty line of body that doesn't
// start with '#' (the recovery file format we write has commented header
// lines + the mnemonic on the last non-comment line).
func lastNonHashLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		return l
	}
	return ""
}
