// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package keychain

import "sync"

// memoryKeychain is a process-local in-memory Keychain used by tests and
// the rare CLI flow that wants to opt out of platform persistence.
type memoryKeychain struct {
	mu      sync.Mutex
	entries map[string][]byte
}

// NewMemory returns an in-memory Keychain. Useful in tests; never use in
// production paths — secrets vanish when the process exits.
func NewMemory() Keychain {
	return &memoryKeychain{entries: map[string][]byte{}}
}

func (m *memoryKeychain) Store(account string, secret []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(secret))
	copy(cp, secret)
	m.entries[account] = cp
	return nil
}

func (m *memoryKeychain) Load(account string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.entries[account]
	if !ok {
		return nil, ErrNotFound
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *memoryKeychain) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[account]; !ok {
		return ErrNotFound
	}
	delete(m.entries, account)
	return nil
}
