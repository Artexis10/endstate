// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// LoginResult is the data payload for a successful `backup login` call.
type LoginResult struct {
	UserID             string `json:"userId"`
	Email              string `json:"email"`
	SubscriptionStatus string `json:"subscriptionStatus,omitempty"`
}

// loginPassphraseReader is the function used to read the passphrase. Tests
// override it via WithPassphraseReader.
var loginPassphraseReader = readPassphraseFromStdin

// WithPassphraseReader returns a deferred function that restores the
// previous reader. Tests use this seam to inject a deterministic
// passphrase without touching os.Stdin.
func WithPassphraseReader(fn func(io.Reader) (string, error)) func() {
	prev := loginPassphraseReader
	loginPassphraseReader = fn
	return func() { loginPassphraseReader = prev }
}

func runBackupLogin(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.Email) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup login requires --email <address>").
			WithRemediation("Pass --email <address>; the passphrase is read from stdin.")
	}

	passphrase, err := loginPassphraseReader(os.Stdin)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup login: read passphrase: "+err.Error()).
			WithRemediation("Pipe the passphrase to stdin or run from an interactive terminal.")
	}
	if passphrase == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup login: empty passphrase").
			WithRemediation("Provide a non-empty passphrase via stdin.")
	}

	a := newBackupStack().Auth
	ctx := context.Background()

	pre, envErr := a.PreHandshake(ctx, flags.Email)
	if envErr != nil {
		return nil, envErr
	}

	// Derive serverPassword + masterKey via Argon2id. STUB until PROMPT 3
	// — returns crypto.ErrNotImplemented.
	if _, kerr := crypto.DeriveKeys(passphrase, []byte(pre.Salt), pre.KDFParams); kerr != nil {
		if errors.Is(kerr, crypto.ErrNotImplemented) {
			return nil, envelope.NewError(envelope.ErrInternalError,
				"crypto module not yet implemented; login orchestration ready, key derivation lands in a follow-up change").
				WithDetail(map[string]string{"phase": "kdf"}).
				WithRemediation("Wait for the engine release that includes the crypto module (PROMPT 3).")
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "derive keys: "+kerr.Error())
	}

	// (Unreachable in PR 1 — the lines below describe the post-PROMPT 3
	// flow.) Once DeriveKeys returns real bytes, the orchestration is:
	//
	//   resp, envErr := a.CompleteLogin(ctx, flags.Email, derived.ServerPassword[:])
	//   if envErr != nil { return nil, envErr }
	//   dek, _ := crypto.UnwrapDEK([]byte(resp.WrappedDEK), derived.MasterKey)
	//   _ = dek // cached on session for push/pull
	//   return &LoginResult{UserID: resp.UserID, Email: flags.Email,
	//       SubscriptionStatus: resp.SubscriptionStatus}, nil
	//
	// The ErrNotImplemented branch above returns before reaching here, so
	// no compile-time dead code.
	return nil, envelope.NewError(envelope.ErrInternalError, "login: unreachable post-stub")
}

// readPassphraseFromStdin reads a single line (terminated by \n) from r
// and returns it with the trailing newline stripped. Suitable for both
// interactive use (Enter terminates) and piped input.
func readPassphraseFromStdin(r io.Reader) (string, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(strings.TrimRight(line, "\n"), "\r"), nil
}
