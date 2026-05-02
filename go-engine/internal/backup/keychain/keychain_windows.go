// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package keychain

import (
	"errors"
	"syscall"

	"github.com/danieljoos/wincred"
)

// NewSystem returns the platform-native Keychain — Windows Credential
// Manager via github.com/danieljoos/wincred.
//
// The contract requires no fallback to plaintext file storage: if the
// Credential Manager is unavailable the caller surfaces the error rather
// than silently degrading.
func NewSystem() Keychain {
	return &windowsKeychain{}
}

type windowsKeychain struct{}

func (*windowsKeychain) Store(account string, secret []byte) error {
	gc := wincred.NewGenericCredential(account)
	gc.CredentialBlob = append([]byte(nil), secret...)
	return gc.Write()
}

func (*windowsKeychain) Load(account string) ([]byte, error) {
	gc, err := wincred.GetGenericCredential(account)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out := make([]byte, len(gc.CredentialBlob))
	copy(out, gc.CredentialBlob)
	return out, nil
}

func (*windowsKeychain) Delete(account string) error {
	gc, err := wincred.GetGenericCredential(account)
	if err != nil {
		if isNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if err := gc.Delete(); err != nil {
		if isNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// isNotFound reports whether err is the Win32 "element not found" sentinel
// returned by Credential Manager when no entry exists for the target name.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// ERROR_NOT_FOUND = 1168 = 0x490
	const errNotFound syscall.Errno = 1168
	var en syscall.Errno
	if errors.As(err, &en) {
		return en == errNotFound
	}
	return false
}
