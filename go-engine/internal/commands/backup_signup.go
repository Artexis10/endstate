// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bufio"
	"context"
	"crypto/rand"
	stdBase64Pkg "encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// SignupResult is the data payload for a successful `backup signup` call.
//
// `RecoveryKeySavedTo` echoes the file path the recovery mnemonic was
// written to so the GUI / stdout consumer can surface it to the user.
type SignupResult struct {
	UserID             string `json:"userId"`
	Email              string `json:"email"`
	SubscriptionStatus string `json:"subscriptionStatus,omitempty"`
	RecoveryKeySavedTo string `json:"recoveryKeySavedTo"`
}

// signupReader reads the passphrase and (optionally) the recovery phrase
// from stdin. The default implementation is line-based: line 1 is the
// passphrase; a non-empty line 2, if present, is treated as a
// caller-supplied BIP39 mnemonic. An empty line 2 (or EOF after line 1)
// signals "generate the mnemonic for me", which requires
// `--save-recovery-to <path>`.
//
// Tests override this via WithSignupReader.
var signupReader = readSignupFromStdin

// WithSignupReader installs a test reader; returns a deferred restore.
func WithSignupReader(fn func(io.Reader) (passphrase, recoveryPhrase string, err error)) func() {
	prev := signupReader
	signupReader = fn
	return func() { signupReader = prev }
}

func runBackupSignup(flags BackupFlags) (interface{}, *envelope.Error) {
	if strings.TrimSpace(flags.Email) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup signup requires --email <address>").
			WithRemediation("Pass --email <address>; the passphrase is read from stdin.")
	}

	passphrase, recoveryPhrase, ierr := signupReader(os.Stdin)
	if ierr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: read stdin: "+ierr.Error()).
			WithRemediation("Pipe the passphrase to stdin (and optionally a recovery phrase on the next line).")
	}
	if strings.TrimSpace(passphrase) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup signup: empty passphrase").
			WithRemediation("Provide a non-empty passphrase via stdin.")
	}

	// Decide between supplied vs generated mnemonic. If the caller did not
	// supply one, --save-recovery-to is mandatory: the contract requires
	// the client to ensure the user has the recovery key before signup
	// completes, and that means writing it to a path the user controls.
	mnemonicSupplied := strings.TrimSpace(recoveryPhrase) != ""
	if !mnemonicSupplied && strings.TrimSpace(flags.SaveRecoveryTo) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup signup: --save-recovery-to <path> is required when no recovery phrase is supplied on stdin").
			WithRemediation("Either pipe a 24-word BIP39 mnemonic on the second stdin line, or pass --save-recovery-to <path> so the engine can write the freshly generated mnemonic.")
	}

	var rkBytes [32]byte
	var mnemonic string

	if mnemonicSupplied {
		var perr error
		rkBytes, perr = crypto.ParseRecoveryPhrase(recoveryPhrase)
		if perr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: parse recovery phrase: "+perr.Error()).
				WithRemediation("Supply a valid 24-word BIP39 mnemonic on stdin (line 2). Order and spelling must match the standard wordlist.")
		}
		mnemonic = strings.TrimSpace(recoveryPhrase)
	} else {
		rk, gerr := crypto.GenerateRecoveryKey()
		if gerr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: generate recovery key: "+gerr.Error())
		}
		copy(rkBytes[:], rk.Bytes[:])
		mnemonic = rk.Phrase
	}

	// Generate per-user salt + DEK.
	salt := make([]byte, crypto.SaltSize)
	if _, rerr := rand.Read(salt); rerr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: generate salt: "+rerr.Error())
	}
	dek, derr := crypto.GenerateDEK()
	if derr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: generate DEK: "+derr.Error())
	}

	params := crypto.DefaultKDFParams()

	derived, kerr := crypto.DeriveKeys(passphrase, salt, params)
	if kerr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: derive keys: "+kerr.Error())
	}
	defer zero32(&derived.MasterKey)
	defer zero32(&derived.ServerPassword)

	wrappedDEK, werr := crypto.WrapDEK(dek, derived.MasterKey)
	if werr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: wrap DEK: "+werr.Error())
	}

	recoveryKey, rderr := crypto.DeriveRecoveryKey(rkBytes, salt, params)
	if rderr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: derive recovery key: "+rderr.Error())
	}
	defer zero32(&recoveryKey)

	recoveryKeyWrappedDEK, rwerr := crypto.WrapDEK(dek, recoveryKey)
	if rwerr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: wrap DEK with recovery key: "+rwerr.Error())
	}

	recoveryKeyVerifier, vverr := crypto.RecoveryKeyVerifier(recoveryKey, salt, params)
	if vverr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup signup: compute recovery verifier: "+vverr.Error())
	}

	// Write the recovery file BEFORE the network call. Per the contract,
	// the client must guarantee the user has the recovery key saved before
	// the account exists. If the disk write fails, no account is created.
	recoverySavedTo := ""
	if strings.TrimSpace(flags.SaveRecoveryTo) != "" {
		path, werr := writeRecoveryFile(flags.SaveRecoveryTo, mnemonic)
		if werr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError,
				"backup signup: write recovery file: "+werr.Error()).
				WithRemediation("Choose a path you can write to, or pipe a recovery phrase on stdin to skip writing.")
		}
		recoverySavedTo = path
	}

	// Submit signup to substrate.
	a := newBackupStack().Auth
	ctx := context.Background()

	body := auth.SignupBody{
		Email:                 flags.Email,
		ServerPassword:        b64.EncodeToString(derived.ServerPassword[:]),
		Salt:                  b64.EncodeToString(salt),
		KDFParams:             params,
		WrappedDEK:            b64.EncodeToString(wrappedDEK),
		RecoveryKeyVerifier:   b64.EncodeToString(recoveryKeyVerifier),
		RecoveryKeyWrappedDEK: b64.EncodeToString(recoveryKeyWrappedDEK),
	}

	resp, envErr := a.Signup(ctx, body)
	if envErr != nil {
		return nil, envErr
	}

	// Persist the unwrapped DEK + the masterKey-wrapped DEK alongside
	// the refresh token. After this returns, subsequent `backup push` /
	// `backup pull` can load both from the keychain without re-deriving.
	if serr := a.Session().StoreDEK(dek); serr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"signup succeeded but DEK could not be cached in the OS keychain: "+serr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	if werr := a.Session().StoreWrappedDEK(body.WrappedDEK); werr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"signup succeeded but wrappedDEK could not be cached in the OS keychain: "+werr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}

	// Best-effort wipe of the local DEK copy.
	for i := range dek {
		dek[i] = 0
	}

	return &SignupResult{
		UserID:             resp.UserID,
		Email:              strings.ToLower(flags.Email),
		SubscriptionStatus: resp.SubscriptionStatus,
		RecoveryKeySavedTo: recoverySavedTo,
	}, nil
}

// readSignupFromStdin reads up to two lines from r. The first is the
// passphrase; the second (optional) is the recovery phrase. Trailing CR
// / LF are stripped.
func readSignupFromStdin(r io.Reader) (string, string, error) {
	br := bufio.NewReader(r)
	pass, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", err
	}
	phrase, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", err
	}
	pass = strings.TrimRight(strings.TrimRight(pass, "\n"), "\r")
	phrase = strings.TrimRight(strings.TrimRight(phrase, "\n"), "\r")
	return pass, phrase, nil
}

// writeRecoveryFile writes the mnemonic + a short header to path with
// mode 0600. Creates parent directories if missing. Returns the path
// actually written (may be the input verbatim).
//
// The header text is a one-time message reminding the user that this
// file is the only way to recover the account if the passphrase is
// forgotten.
func writeRecoveryFile(path, mnemonic string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	contents := "# Endstate Hosted Backup recovery key\n" +
		"# Anyone with this phrase can reset your passphrase and decrypt your data.\n" +
		"# Store it somewhere offline; do not commit it to source control.\n\n" +
		mnemonic + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// b64 is the standard base64 encoding used on the wire for byte fields.
var b64 = stdBase64Pkg.StdEncoding

// zero32 wipes a 32-byte array. Defense-in-depth — see crypto.zeroBytes.
func zero32(b *[32]byte) {
	for i := range b {
		b[i] = 0
	}
}
