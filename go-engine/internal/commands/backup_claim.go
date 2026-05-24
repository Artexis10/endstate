// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"crypto/rand"
	"os"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// runBackupClaim attaches credentials to a Hosted Backup pre-account
// using a single-use bearer claim token from the buyer's purchase
// email. Structurally identical to runBackupSignup: the only deltas
// are at the HTTP boundary (bearer header + no email in the body,
// server-supplied email in the response).
//
// The token is validated client-side as a UX optimisation (length 43,
// URL-safe base64 alphabet) so an obviously-malformed paste is
// rejected before any network call. Substrate is the source of truth
// for token validity — it returns CLAIM_TOKEN_INVALID for a
// well-formed token that doesn't match a `claim_tokens` row.
//
// See `openspec/changes/add-backup-claim-subcommand/`.
func runBackupClaim(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.Token) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup claim requires --token <claim-token>").
			WithRemediation("Pass --token <43-char URL-safe base64 token>; the passphrase is read from stdin.")
	}
	if !validClaimToken(flags.Token) {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup claim: --token must be 43 characters of URL-safe base64 (alphabet [A-Za-z0-9_-])").
			WithRemediation("Re-copy the token from the link in your purchase claim email.")
	}

	passphrase, recoveryPhrase, ierr := signupReader(os.Stdin)
	if ierr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: read stdin: "+ierr.Error()).
			WithRemediation("Pipe the passphrase to stdin (and optionally a recovery phrase on the next line).")
	}
	if strings.TrimSpace(passphrase) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup claim: empty passphrase").
			WithRemediation("Provide a non-empty passphrase via stdin.")
	}

	// Decide between supplied vs generated mnemonic. Same rule as
	// signup: when not supplied, --save-recovery-to is mandatory.
	mnemonicSupplied := strings.TrimSpace(recoveryPhrase) != ""
	if !mnemonicSupplied && strings.TrimSpace(flags.SaveRecoveryTo) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup claim: --save-recovery-to <path> is required when no recovery phrase is supplied on stdin").
			WithRemediation("Either pipe a 24-word BIP39 mnemonic on the second stdin line, or pass --save-recovery-to <path> so the engine can write the freshly generated mnemonic.")
	}

	var rkBytes [32]byte
	var mnemonic string

	if mnemonicSupplied {
		var perr error
		rkBytes, perr = crypto.ParseRecoveryPhrase(recoveryPhrase)
		if perr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: parse recovery phrase: "+perr.Error()).
				WithRemediation("Supply a valid 24-word BIP39 mnemonic on stdin (line 2). Order and spelling must match the standard wordlist.")
		}
		mnemonic = strings.TrimSpace(recoveryPhrase)
	} else {
		rk, gerr := crypto.GenerateRecoveryKey()
		if gerr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: generate recovery key: "+gerr.Error())
		}
		copy(rkBytes[:], rk.Bytes[:])
		mnemonic = rk.Phrase
	}

	// Generate per-user salt + DEK.
	salt := make([]byte, crypto.SaltSize)
	if _, rerr := rand.Read(salt); rerr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: generate salt: "+rerr.Error())
	}
	dek, derr := crypto.GenerateDEK()
	if derr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: generate DEK: "+derr.Error())
	}

	params := crypto.DefaultKDFParams()

	derived, kerr := crypto.DeriveKeys(passphrase, salt, params)
	if kerr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: derive keys: "+kerr.Error())
	}
	defer zero32(&derived.MasterKey)
	defer zero32(&derived.ServerPassword)

	wrappedDEK, werr := crypto.WrapDEK(dek, derived.MasterKey)
	if werr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: wrap DEK: "+werr.Error())
	}

	recoveryKey, rderr := crypto.DeriveRecoveryKey(rkBytes, salt, params)
	if rderr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: derive recovery key: "+rderr.Error())
	}
	defer zero32(&recoveryKey)

	recoveryKeyWrappedDEK, rwerr := crypto.WrapDEK(dek, recoveryKey)
	if rwerr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: wrap DEK with recovery key: "+rwerr.Error())
	}

	recoveryKeyVerifier, vverr := crypto.RecoveryKeyVerifier(recoveryKey, salt, params)
	if vverr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup claim: compute recovery verifier: "+vverr.Error())
	}

	// Write the recovery file BEFORE the network call. Identical
	// to signup — the contract requires the user has the recovery
	// key on disk before the substrate-side credentials are written.
	recoverySavedTo := ""
	if strings.TrimSpace(flags.SaveRecoveryTo) != "" {
		path, werr := writeRecoveryFile(flags.SaveRecoveryTo, mnemonic)
		if werr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError,
				"backup claim: write recovery file: "+werr.Error()).
				WithRemediation("Choose a path you can write to, or pipe a recovery phrase on stdin to skip writing.")
		}
		recoverySavedTo = path
	}

	// Submit claim to substrate.
	a := newBackupStack().Auth
	ctx := context.Background()

	body := auth.ClaimBody{
		ServerPassword:        b64.EncodeToString(derived.ServerPassword[:]),
		Salt:                  b64.EncodeToString(salt),
		KDFParams:             params,
		WrappedDEK:            b64.EncodeToString(wrappedDEK),
		RecoveryKeyVerifier:   b64.EncodeToString(recoveryKeyVerifier),
		RecoveryKeyWrappedDEK: b64.EncodeToString(recoveryKeyWrappedDEK),
	}

	resp, envErr := a.Claim(ctx, flags.Token, body)
	if envErr != nil {
		return nil, envErr
	}

	// Persist the unwrapped DEK + the masterKey-wrapped DEK alongside
	// the refresh token. Identical to signup.
	if serr := a.Session().StoreDEK(dek); serr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"claim succeeded but DEK could not be cached in the OS keychain: "+serr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	if werr := a.Session().StoreWrappedDEK(body.WrappedDEK); werr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"claim succeeded but wrappedDEK could not be cached in the OS keychain: "+werr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}

	// Best-effort wipe of the local DEK copy.
	for i := range dek {
		dek[i] = 0
	}

	return &SignupResult{
		UserID:             resp.UserID,
		Email:              strings.ToLower(resp.Email),
		SubscriptionStatus: resp.SubscriptionStatus,
		RecoveryKeySavedTo: recoverySavedTo,
	}, nil
}

// validClaimToken returns true iff t is exactly 43 characters from the
// URL-safe base64 alphabet (RFC 4648 §5, unpadded). Substrate mints
// 32-byte random tokens encoded with base64url-no-padding → 43 chars.
//
// This is a UX gate — substrate is the source of truth for token
// validity. A well-formed token that doesn't match a `claim_tokens`
// row returns `CLAIM_TOKEN_INVALID` from the server side.
func validClaimToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	for _, r := range t {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			continue
		default:
			return false
		}
	}
	return true
}
