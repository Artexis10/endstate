// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package keychain

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// Compile-time assertion that the linux backend satisfies the interface.
var _ Keychain = (*keyringKeychain)(nil)

// useMockKeyring swaps go-keyring's provider for an in-memory mock so these
// tests never dial D-Bus. The repo's Linux CI (and this dev box) often have
// no Secret Service daemon; MockInit guarantees `go test ./...` is hermetic
// regardless.
func useMockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
}

func TestLinux_StoreLoadRoundTrip(t *testing.T) {
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

func TestLinux_StoreLoadBinaryDEK(t *testing.T) {
	// The DEK is 32 raw bytes (non-UTF-8); ensure binary round-trips
	// survive the string<->[]byte conversion the wrapper performs.
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

func TestLinux_LoadMissingMapsToErrNotFound(t *testing.T) {
	useMockKeyring(t)
	kc := NewSystem()
	if _, err := kc.Load(AccountForUser("absent")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load missing: expected ErrNotFound, got %v", err)
	}
}

func TestLinux_DeleteMissingMapsToErrNotFound(t *testing.T) {
	useMockKeyring(t)
	kc := NewSystem()
	if err := kc.Delete(AccountForUser("absent")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing: expected ErrNotFound, got %v", err)
	}
}

func TestLinux_DeleteRemoves(t *testing.T) {
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

func TestLinux_StoreOverwrites(t *testing.T) {
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

// TestLinux_DaemonFailureCarriesRemediationHint verifies that a non-not-found
// Secret Service failure (the headless / WSL / locked-keyring case) is wrapped
// with the actionable hint, not silently degraded. MockInitWithError simulates
// the unreachable-daemon path without needing a real D-Bus session.
func TestLinux_DaemonFailureCarriesRemediationHint(t *testing.T) {
	keyring.MockInitWithError(errors.New("dbus: couldn't determine address of session bus"))
	t.Cleanup(func() { keyring.MockInit() })

	kc := NewSystem()
	err := kc.Store(AccountForUser("user-1"), []byte("rt"))
	if err == nil {
		t.Fatal("Store: expected error when Secret Service is unreachable, got nil")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("Store: daemon failure must not map to ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "Secret Service") {
		t.Errorf("Store error %q: expected an actionable Secret Service remediation hint", err.Error())
	}
}
