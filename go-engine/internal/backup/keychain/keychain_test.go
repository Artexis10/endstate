// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package keychain_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
)

func TestMemory_StoreLoadRoundTrip(t *testing.T) {
	kc := keychain.NewMemory()
	secret := []byte("rt-1234567890")
	account := keychain.AccountForUser("user-1")

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

func TestMemory_LoadMissing(t *testing.T) {
	kc := keychain.NewMemory()
	_, err := kc.Load("missing-account")
	if !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("Load missing: expected ErrNotFound, got %v", err)
	}
}

func TestMemory_StoreOverwrites(t *testing.T) {
	kc := keychain.NewMemory()
	account := keychain.AccountForUser("user-1")

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

func TestMemory_DeleteRemoves(t *testing.T) {
	kc := keychain.NewMemory()
	account := keychain.AccountForUser("user-1")
	_ = kc.Store(account, []byte("rt"))

	if err := kc.Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := kc.Load(account); !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("Load after Delete: expected ErrNotFound, got %v", err)
	}
}

func TestMemory_DeleteMissingErrNotFound(t *testing.T) {
	kc := keychain.NewMemory()
	if err := kc.Delete("missing-account"); !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("Delete missing: expected ErrNotFound, got %v", err)
	}
}

func TestMemory_StoreCopiesInput(t *testing.T) {
	// Tampering with the caller's slice after Store must not leak into the
	// stored value — defensive copy is part of the contract.
	kc := keychain.NewMemory()
	account := keychain.AccountForUser("user-1")
	secret := []byte("rt-original")
	_ = kc.Store(account, secret)

	for i := range secret {
		secret[i] = 0
	}

	got, _ := kc.Load(account)
	if string(got) != "rt-original" {
		t.Errorf("Load = %q, want %q (Store must defensively copy)", got, "rt-original")
	}
}

func TestAccountForUser_Stable(t *testing.T) {
	if got, want := keychain.AccountForUser("abc"), "endstate-refresh-abc"; got != want {
		t.Errorf("AccountForUser(abc) = %q, want %q", got, want)
	}
}

func TestAccountForDEK_Stable(t *testing.T) {
	if got, want := keychain.AccountForDEK("abc"), "endstate-dek-abc"; got != want {
		t.Errorf("AccountForDEK(abc) = %q, want %q", got, want)
	}
}

func TestAccountForUser_AndDEK_AreDistinct(t *testing.T) {
	// Both entries live in the same keychain; the engine relies on the
	// account names not colliding so logout can clear them independently.
	if keychain.AccountForUser("u") == keychain.AccountForDEK("u") {
		t.Error("AccountForUser and AccountForDEK must produce distinct account names for the same userId")
	}
}
