//go:build windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
)

// RegistryFixture materializes registry verifier keys beneath one disposable
// validation namespace.
type RegistryFixture struct {
	mu      sync.Mutex
	context *Context
}

func NewRegistryFixture(context *Context) (*RegistryFixture, error) {
	if context == nil {
		return nil, fmt.Errorf("%w: registry fixture is inactive", ErrUnsafeRegistry)
	}
	return &RegistryFixture{context: context}, nil
}

func (fixture *RegistryFixture) Materialize(authored string) error {
	if fixture == nil || fixture.context == nil {
		return fmt.Errorf("%w: registry fixture is inactive", ErrUnsafeRegistry)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	mapped, err := fixture.context.MapHKCU(authored)
	if err != nil {
		return err
	}
	namespace := strings.ToLower(fixture.context.RegistryNamespace())
	if !strings.HasPrefix(strings.ToLower(mapped), namespace+`\`) {
		return fmt.Errorf("%w: registry fixture escaped validation namespace", ErrUnsafeRegistry)
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, mapped[len(`HKCU\`):], registry.WRITE)
	if err != nil {
		return fmt.Errorf("create registry verifier fixture: %w", err)
	}
	return key.Close()
}

func (fixture *RegistryFixture) Cleanup() error {
	if fixture == nil || fixture.context == nil {
		return fmt.Errorf("%w: registry fixture is inactive", ErrUnsafeRegistry)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	subkey := fixture.context.RegistryNamespace()[len(`HKCU\`):]
	if err := deleteRegistryFixtureTree(subkey); err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.READ)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify registry fixture cleanup: %w", err)
	}
	_ = key.Close()
	return fmt.Errorf("registry fixture namespace remains after cleanup")
}

func deleteRegistryFixtureTree(subkey string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.READ|registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open registry fixture namespace: %w", err)
	}
	children, err := key.ReadSubKeyNames(-1)
	closeErr := key.Close()
	if err != nil {
		return fmt.Errorf("enumerate registry fixture namespace: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close registry fixture namespace: %w", closeErr)
	}
	for _, child := range children {
		if err := deleteRegistryFixtureTree(subkey + `\` + child); err != nil {
			return err
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, subkey); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("delete registry fixture namespace: %w", err)
	}
	return nil
}
