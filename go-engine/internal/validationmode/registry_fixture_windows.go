//go:build windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows/registry"
)

type registryFixtureOperations interface {
	ensure(string) error
	replace(string, RegistryState) error
	read(string) (RegistryState, error)
	remove(string) error
	exists(string) (bool, error)
}

// RegistryFixture materializes typed registry state beneath one disposable
// validation namespace.
type RegistryFixture struct {
	mu         sync.Mutex
	context    *Context
	operations registryFixtureOperations
}

func NewRegistryFixture(context *Context) (*RegistryFixture, error) {
	if context == nil {
		return nil, fmt.Errorf("%w: registry fixture is inactive", ErrUnsafeRegistry)
	}
	return newRegistryFixtureWithOperations(context, windowsRegistryFixtureOperations{}), nil
}

func newRegistryFixtureWithOperations(context *Context, operations registryFixtureOperations) *RegistryFixture {
	return &RegistryFixture{context: context, operations: operations}
}

// Materialize preserves the key-only fixture behavior used by registry
// verifier scenarios.
func (fixture *RegistryFixture) Materialize(authored string) error {
	return fixture.withAuthoredSubkey(authored, func(subkey string) error {
		return fixture.operations.ensure(subkey)
	})
}

// Replace makes an authored key and all descendants exactly match state.
func (fixture *RegistryFixture) Replace(authored string, state RegistryState) error {
	return fixture.withAuthoredSubkey(authored, func(subkey string) error {
		return fixture.operations.replace(subkey, state)
	})
}

// Snapshot returns the canonical typed state below an authored key.
func (fixture *RegistryFixture) Snapshot(authored string) (RegistryState, error) {
	if fixture == nil || fixture.context == nil || fixture.operations == nil {
		return RegistryState{}, fmt.Errorf("%w: registry fixture is inactive", ErrUnsafeRegistry)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	subkey, err := fixture.authoredSubkey(authored)
	if err != nil {
		return RegistryState{}, err
	}
	state, err := fixture.operations.read(subkey)
	if err != nil {
		return RegistryState{}, fmt.Errorf("snapshot registry fixture state: %w", err)
	}
	return NewRegistryState(state.Keys())
}

// Remove removes one authored key and its descendants.
func (fixture *RegistryFixture) Remove(authored string) error {
	return fixture.withAuthoredSubkey(authored, func(subkey string) error {
		if err := fixture.operations.remove(subkey); err != nil {
			return fmt.Errorf("remove registry fixture state: %w", err)
		}
		return nil
	})
}

// ProveAbsent independently verifies that an authored key is absent.
func (fixture *RegistryFixture) ProveAbsent(authored string) error {
	return fixture.withAuthoredSubkey(authored, func(subkey string) error {
		exists, err := fixture.operations.exists(subkey)
		if err != nil {
			return fmt.Errorf("verify registry fixture absence: %w", err)
		}
		if exists {
			return fmt.Errorf("registry fixture key remains")
		}
		return nil
	})
}

// Cleanup removes the entire nonce namespace and independently proves absence.
func (fixture *RegistryFixture) Cleanup() error {
	if fixture == nil || fixture.context == nil || fixture.operations == nil {
		return fmt.Errorf("%w: registry fixture is inactive", ErrUnsafeRegistry)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	subkey := fixture.context.RegistryNamespace()[len(`HKCU\`):]
	deleteErr := fixture.operations.remove(subkey)
	exists, probeErr := fixture.operations.exists(subkey)
	if probeErr == nil && exists {
		probeErr = errors.New("registry fixture namespace remains after cleanup")
	}
	if deleteErr != nil {
		deleteErr = fmt.Errorf("remove registry fixture namespace: %w", deleteErr)
	}
	if probeErr != nil {
		probeErr = fmt.Errorf("verify registry fixture cleanup: %w", probeErr)
	}
	return errors.Join(deleteErr, probeErr)
}

func (fixture *RegistryFixture) withAuthoredSubkey(authored string, operation func(string) error) error {
	if fixture == nil || fixture.context == nil || fixture.operations == nil {
		return fmt.Errorf("%w: registry fixture is inactive", ErrUnsafeRegistry)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	subkey, err := fixture.authoredSubkey(authored)
	if err != nil {
		return err
	}
	return operation(subkey)
}

func (fixture *RegistryFixture) authoredSubkey(authored string) (string, error) {
	mapped, err := fixture.context.MapHKCU(authored)
	if err != nil {
		return "", err
	}
	namespace := fixture.context.RegistryNamespace()
	if !strings.HasPrefix(strings.ToLower(mapped), strings.ToLower(namespace)+`\`) {
		return "", fmt.Errorf("%w: registry fixture escaped validation namespace", ErrUnsafeRegistry)
	}
	return mapped[len(`HKCU\`):], nil
}

type windowsRegistryFixtureOperations struct{}

func (windowsRegistryFixtureOperations) ensure(subkey string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, subkey, registry.WRITE)
	if err != nil {
		return err
	}
	return key.Close()
}

func (operations windowsRegistryFixtureOperations) replace(subkey string, state RegistryState) error {
	if err := operations.remove(subkey); err != nil {
		return err
	}
	for _, relative := range state.Keys() {
		path := subkey
		if relative.Path != "" {
			path += `\` + relative.Path
		}
		key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.WRITE)
		if err != nil {
			return err
		}
		for _, value := range relative.Values {
			if err := setRawRegistryValue(key, value); err != nil {
				_ = key.Close()
				return err
			}
		}
		if err := key.Close(); err != nil {
			return err
		}
	}
	return operations.ensure(subkey)
}

func (windowsRegistryFixtureOperations) read(subkey string) (RegistryState, error) {
	keys := make([]RegistryKey, 0)
	if err := readRegistryState(subkey, "", &keys); err != nil {
		return RegistryState{}, err
	}
	return NewRegistryState(keys)
}

func (windowsRegistryFixtureOperations) remove(subkey string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.READ|registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	children, readErr := key.ReadSubKeyNames(-1)
	closeErr := key.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	for _, child := range children {
		if err := (windowsRegistryFixtureOperations{}).remove(subkey + `\` + child); err != nil {
			return err
		}
	}
	err = registry.DeleteKey(registry.CURRENT_USER, subkey)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func (windowsRegistryFixtureOperations) exists(subkey string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.READ)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, key.Close()
}

func readRegistryState(subkey, relative string, keys *[]RegistryKey) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.READ|registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	values, err := readRegistryValues(key)
	if err == nil {
		*keys = append(*keys, RegistryKey{Path: relative, Values: values})
	}
	children, childErr := key.ReadSubKeyNames(-1)
	closeErr := key.Close()
	if err != nil {
		return err
	}
	if childErr != nil {
		return childErr
	}
	if closeErr != nil {
		return closeErr
	}
	for _, child := range children {
		childRelative := child
		if relative != "" {
			childRelative = relative + `\` + child
		}
		if err := readRegistryState(subkey+`\`+child, childRelative, keys); err != nil {
			return err
		}
	}
	return nil
}

func readRegistryValues(key registry.Key) ([]RegistryValue, error) {
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}
	return collectRegistryValues(names, func(name string) (RegistryValue, bool, error) {
		return readRawRegistryValue(key, name)
	})
}

func collectRegistryValues(names []string, read func(string) (RegistryValue, bool, error)) ([]RegistryValue, error) {
	values := make([]RegistryValue, 0, len(names)+1)
	if value, exists, err := read(""); err != nil {
		return nil, err
	} else if exists {
		values = append(values, value)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		value, exists, err := read(name)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("registry value disappeared during snapshot")
		}
		values = append(values, value)
	}
	return values, nil
}

func readRawRegistryValue(key registry.Key, name string) (RegistryValue, bool, error) {
	size, _, err := key.GetValue(name, nil)
	if errors.Is(err, registry.ErrNotExist) {
		return RegistryValue{}, false, nil
	}
	if err != nil {
		return RegistryValue{}, false, err
	}
	data := make([]byte, size)
	for {
		read, actualType, err := key.GetValue(name, data)
		if errors.Is(err, registry.ErrShortBuffer) {
			data = make([]byte, read)
			continue
		}
		if err != nil {
			return RegistryValue{}, false, err
		}
		return RegistryValue{Name: name, Type: actualType, Data: append([]byte(nil), data[:read]...)}, true, nil
	}
}

func setRawRegistryValue(key registry.Key, value RegistryValue) error {
	switch value.Type {
	case RegistryTypeString:
		units := make([]uint16, len(value.Data)/2-1)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(value.Data[index*2:])
		}
		return key.SetStringValue(value.Name, string(utf16.Decode(units)))
	case RegistryTypeDWORD:
		return key.SetDWordValue(value.Name, binary.LittleEndian.Uint32(value.Data))
	case RegistryTypeBinary:
		return key.SetBinaryValue(value.Name, value.Data)
	default:
		return fmt.Errorf("%w: registry value type is unsupported", ErrUnsafeRegistry)
	}
}
