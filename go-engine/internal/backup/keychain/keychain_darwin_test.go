// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package keychain

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// Compile-time assertion that the darwin backend satisfies the interface.
var _ Keychain = (*keyringKeychain)(nil)

// useMockKeyring swaps go-keyring's provider for an in-memory mock so these
// tests never touch the real macOS Keychain or shell out to /usr/bin/security.
// CI runs `go test ./...` on a macos runner without an unlocked login
// keychain, so hitting the real store would hang or fail.
func useMockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
}

func TestDarwin_StoreLoadRoundTrip(t *testing.T) {
	useMockKeyring(t)
	kc := NewSystem()
	account := AccountForUser("user-1")
	secret := []byte("rt-1234567890")

	if err := kc.Store(account, secret); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := kc.Load(account)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("Load = %q, want %q", got, secret)
	}
}

func TestDarwin_StoreLoadBinaryDEK(t *testing.T) {
	// The DEK is 32 raw bytes (non-UTF-8). go-keyring base64-encodes on
	// darwin internally, so binary round-trips must survive string<->[]byte.
	useMockKeyring(t)
	kc := NewSystem()
	account := AccountForDEK("user-1")
	secret := []byte{0x00, 0xff, 0x10, 0x80, 0x7f, 0x00, 0xaa}

	if err := kc.Store(account, secret); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := kc.Load(account)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("Load = %v, want %v", got, secret)
	}
}

func TestDarwin_LoadMissingMapsToErrNotFound(t *testing.T) {
	useMockKeyring(t)
	kc := NewSystem()
	if _, err := kc.Load(AccountForUser("absent")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load missing: expected ErrNotFound, got %v", err)
	}
}

func TestDarwin_DeleteMissingMapsToErrNotFound(t *testing.T) {
	useMockKeyring(t)
	kc := NewSystem()
	if err := kc.Delete(AccountForUser("absent")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing: expected ErrNotFound, got %v", err)
	}
}

func TestDarwin_DeleteRemoves(t *testing.T) {
	useMockKeyring(t)
	kc := NewSystem()
	account := AccountForUser("user-1")
	if err := kc.Store(account, []byte("rt")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := kc.Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := kc.Load(account); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load after Delete: expected ErrNotFound, got %v", err)
	}
}

func TestDarwin_StoreOverwrites(t *testing.T) {
	useMockKeyring(t)
	kc := NewSystem()
	account := AccountForUser("user-1")
	_ = kc.Store(account, []byte("first"))
	if err := kc.Store(account, []byte("second")); err != nil {
		t.Fatalf("Store overwrite: %v", err)
	}
	got, err := kc.Load(account)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("Load = %q, want %q", got, "second")
	}
}
