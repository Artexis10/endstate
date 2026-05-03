// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bufio"
	"context"
	stdBase64Pkg "encoding/base64"
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

	saltBytes, sderr := loginBase64.DecodeString(pre.Salt)
	if sderr != nil {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			"backup login: server returned a salt that is not valid base64").
			WithRemediation("Update the engine; this typically means a substrate response shape changed.")
	}

	derived, kerr := crypto.DeriveKeys(passphrase, saltBytes, pre.KDFParams)
	if kerr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup login: derive keys: "+kerr.Error())
	}
	defer zero32(&derived.MasterKey)
	defer zero32(&derived.ServerPassword)

	resp, envErr := a.CompleteLogin(ctx, flags.Email, derived.ServerPassword[:])
	if envErr != nil {
		return nil, envErr
	}

	wrappedDEK, wderr := loginBase64.DecodeString(resp.WrappedDEK)
	if wderr != nil {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			"backup login: server returned a wrappedDEK that is not valid base64").
			WithRemediation("Update the engine; this typically means a substrate response shape changed.")
	}

	dek, uderr := crypto.UnwrapDEK(wrappedDEK, derived.MasterKey)
	if uderr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup login: unwrap DEK: "+uderr.Error()).
			WithRemediation("If you recently changed your passphrase out-of-band, run `endstate backup recover` instead.")
	}

	if serr := a.Session().StoreDEK(dek); serr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envelope.NewError(envelope.ErrInternalError,
			"login succeeded but DEK could not be cached in the OS keychain: "+serr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	if werr := a.Session().StoreWrappedDEK(resp.WrappedDEK); werr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envelope.NewError(envelope.ErrInternalError,
			"login succeeded but wrappedDEK could not be cached in the OS keychain: "+werr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	for i := range dek {
		dek[i] = 0
	}

	return &LoginResult{
		UserID:             resp.UserID,
		Email:              strings.ToLower(flags.Email),
		SubscriptionStatus: resp.SubscriptionStatus,
	}, nil
}

// loginBase64 is the standard base64 encoding used by substrate for byte
// fields on the wire (salt, wrappedDEK, etc.).
var loginBase64 = stdBase64Pkg.StdEncoding

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
