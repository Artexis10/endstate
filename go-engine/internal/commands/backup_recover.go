// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"

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
			WithRemediation("Provide the 24-word BIP39 recovery phrase via stdin.")
	}
	if strings.TrimSpace(newPass) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup recover: empty new passphrase").
			WithRemediation("Provide the new passphrase on the second line of stdin (after the recovery phrase).")
	}
	// TODO(prompt-3): use newPass to derive new serverPassword + masterKey
	// and re-wrap the DEK after recovery completes; the orchestration
	// scaffold here only validates inputs and surfaces the crypto stub.
	_ = newPass

	// As with login: orchestration is ready, but the recovery key parse +
	// KDF + DEK rewrap path needs the crypto module. Surface the
	// consistent "crypto not yet implemented" envelope.
	if _, kerr := crypto.ParseRecoveryPhrase(phrase); kerr != nil {
		if errors.Is(kerr, crypto.ErrNotImplemented) {
			return nil, envelope.NewError(envelope.ErrInternalError,
				"crypto module not yet implemented; recovery orchestration ready, recovery key derivation lands in a follow-up change").
				WithDetail(map[string]string{"phase": "recovery-kdf"}).
				WithRemediation("Wait for the engine release that includes the crypto module (PROMPT 3).")
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "backup recover: parse phrase: "+kerr.Error())
	}

	return nil, envelope.NewError(envelope.ErrInternalError, "recover: unreachable post-stub")
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
