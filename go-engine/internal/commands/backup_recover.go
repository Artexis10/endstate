// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bufio"
	"context"
	"crypto/rand"
	"io"
	"os"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// recoveryReader is the function used to read the recovery key + new
// passphrase. Tests override it via WithRecoveryReader.
var recoveryReader = readRecoveryFromStdin

// WithRecoveryReader installs a test reader and returns a deferred
// restore. The returned reader is given os.Stdin; tests can ignore it
// and supply pre-canned strings.
func WithRecoveryReader(fn func(io.Reader) (recoveryPhrase, newPassphrase string, err error)) func() {
	prev := recoveryReader
	recoveryReader = fn
	return func() { recoveryReader = prev }
}

// RecoverResult is the data payload for `backup recover`.
type RecoverResult struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

func runBackupRecover(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.Email) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup recover requires --email <address>")
	}

	phrase, newPass, err := recoveryReader(os.Stdin)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: read input: "+err.Error())
	}
	if strings.TrimSpace(phrase) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup recover: empty recovery key").
			WithRemediation("Provide the 24-word BIP39 recovery phrase via stdin (line 1).")
	}
	if strings.TrimSpace(newPass) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup recover: empty new passphrase").
			WithRemediation("Provide the new passphrase on the second line of stdin (after the recovery phrase).")
	}

	rkBytes, perr := crypto.ParseRecoveryPhrase(phrase)
	if perr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: parse phrase: "+perr.Error()).
			WithRemediation("Supply a valid 24-word BIP39 mnemonic. Order, spelling, and case must match the standard wordlist.")
	}
	defer zero32(&rkBytes)

	a := newBackupStack().Auth
	ctx := context.Background()

	pre, envErr := a.PreHandshake(ctx, flags.Email)
	if envErr != nil {
		return nil, envErr
	}
	saltBytes, sderr := loginBase64.DecodeString(pre.Salt)
	if sderr != nil {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			"backup recover: server returned a salt that is not valid base64").
			WithRemediation("Update the engine; this typically means a substrate response shape changed.")
	}

	recoveryKey, drErr := crypto.DeriveRecoveryKey(rkBytes, saltBytes, pre.KDFParams)
	if drErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: derive recovery key: "+drErr.Error())
	}
	defer zero32(&recoveryKey)

	proof, prErr := crypto.RecoveryKeyVerifier(recoveryKey, saltBytes, pre.KDFParams)
	if prErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: compute proof: "+prErr.Error())
	}

	recResp, envErr := a.Recover(ctx, auth.RecoverBody{
		Email:            flags.Email,
		RecoveryKeyProof: loginBase64.EncodeToString(proof),
	})
	if envErr != nil {
		return nil, envErr
	}

	wrappedDEK, wdErr := loginBase64.DecodeString(recResp.RecoveryKeyWrappedDEK)
	if wdErr != nil {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			"backup recover: server returned a recoveryKeyWrappedDEK that is not valid base64")
	}
	dek, ueErr := crypto.UnwrapDEK(wrappedDEK, recoveryKey)
	if ueErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup recover: unwrap DEK with recovery key: "+ueErr.Error()).
			WithRemediation("Verify the recovery phrase matches the one shown at signup. If you no longer have it, your data is unrecoverable per the contract's structural guarantee.")
	}

	// Generate a fresh salt for the new passphrase derivation. The new
	// salt is sent to the server as `newSalt` so future logins can derive
	// the same serverPassword + masterKey from the new passphrase.
	newSaltBytes := make([]byte, crypto.SaltSize)
	if _, sErr := rand.Read(newSaltBytes); sErr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: generate new salt: "+sErr.Error())
	}

	derived, kerr := crypto.DeriveKeys(newPass, newSaltBytes, pre.KDFParams)
	if kerr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: derive new keys: "+kerr.Error())
	}
	defer zero32(&derived.MasterKey)
	defer zero32(&derived.ServerPassword)

	newWrappedDEK, nwErr := crypto.WrapDEK(dek, derived.MasterKey)
	if nwErr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: re-wrap DEK: "+nwErr.Error())
	}

	newWrappedDEKB64 := loginBase64.EncodeToString(newWrappedDEK)
	finResp, envErr := a.RecoverFinalize(ctx, recResp.RecoveryToken, flags.Email, auth.RecoverFinalizeBody{
		NewServerPassword: loginBase64.EncodeToString(derived.ServerPassword[:]),
		NewSalt:           loginBase64.EncodeToString(newSaltBytes),
		NewKDFParams:      pre.KDFParams,
		NewWrappedDEK:     newWrappedDEKB64,
	})
	if envErr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envErr
	}

	if serr := a.Session().StoreDEK(dek); serr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envelope.NewError(envelope.ErrInternalError,
			"recover finalize succeeded but DEK could not be cached in the OS keychain: "+serr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	if werr := a.Session().StoreWrappedDEK(newWrappedDEKB64); werr != nil {
		for i := range dek {
			dek[i] = 0
		}
		return nil, envelope.NewError(envelope.ErrInternalError,
			"recover finalize succeeded but wrappedDEK could not be cached in the OS keychain: "+werr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	for i := range dek {
		dek[i] = 0
	}

	return &RecoverResult{
		UserID: finResp.UserID,
		Email:  strings.ToLower(flags.Email),
	}, nil
}

// readRecoveryFromStdin reads two lines: the first is the recovery
// phrase (BIP39 24-word mnemonic), the second is the new passphrase. The
// trailing newline on each is stripped.
func readRecoveryFromStdin(r io.Reader) (string, string, error) {
	br := bufio.NewReader(r)
	phrase, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", err
	}
	pass, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", err
	}
	return strings.TrimRight(strings.TrimRight(phrase, "\n"), "\r"),
		strings.TrimRight(strings.TrimRight(pass, "\n"), "\r"),
		nil
}

// recoveryPhrase is a named string for clarity in the WithRecoveryReader
// signature. No type-system magic — it's a documentation aid.
type recoveryPhrase = string
type newPassphrase = string
