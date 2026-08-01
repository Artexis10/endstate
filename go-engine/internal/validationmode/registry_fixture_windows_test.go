//go:build windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestRegistryFixtureMaterializesMappedHKCUKeysAndCleansNamespace(t *testing.T) {
	context := activeTestContext(t, "registry-fixture")
	fixture, err := NewRegistryFixture(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fixture.Cleanup(); err != nil {
			t.Error(err)
		}
	})

	for _, authored := range []string{`HKCU:\Software\Fixture\One`, `HKCU\Software\Fixture\Two`} {
		if err := fixture.Materialize(authored); err != nil {
			if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Skipf("registry fixture namespace is unavailable: %v", err)
			}
			t.Fatalf("Materialize(%q): %v", authored, err)
		}
		mapped, err := context.MapHKCU(authored)
		if err != nil {
			t.Fatal(err)
		}
		key, err := registry.OpenKey(registry.CURRENT_USER, mapped[len(`HKCU\`):], registry.READ)
		if err != nil {
			t.Fatalf("mapped key %q: %v", mapped, err)
		}
		_ = key.Close()
	}

	if err := fixture.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, context.RegistryNamespace()[len(`HKCU\`):], registry.READ); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("namespace remains after cleanup: %v", err)
	}
	if err := fixture.Cleanup(); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
}

func TestRegistryFixtureRejectsNonSemanticAndForeignMappedKeys(t *testing.T) {
	context := activeTestContext(t, "registry-fixture-reject")
	fixture, err := NewRegistryFixture(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fixture.Cleanup(); err != nil {
			t.Error(err)
		}
	})

	foreign := strings.Replace(context.RegistryNamespace(), context.Descriptor().Nonce, "foreign", 1) + `\Software\Fixture`
	for _, authored := range []string{`HKLM\Software\Fixture`, foreign} {
		if err := fixture.Materialize(authored); !errors.Is(err, ErrUnsafeRegistry) {
			t.Fatalf("Materialize(%q) error = %v, want ErrUnsafeRegistry", authored, err)
		}
	}
}
