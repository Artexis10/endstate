// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package verifier

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"golang.org/x/sys/windows/registry"
)

func TestValidationRegistryVerifierMapsOnlyAtNativeOpenAndRedactsValueData(t *testing.T) {
	context := verifierValidationContext(t)
	originalOpen := registryOpenKeyNative
	originalClose := registryCloseKeyNative
	originalRead := registryReadValueDataNative
	openCalls := 0
	registryOpenKeyNative = func(hive registry.Key, subkey string, access uint32) (registry.Key, error) {
		openCalls++
		if hive != registry.CURRENT_USER || !strings.Contains(subkey, `Software\Endstate\Validation\`+context.Descriptor().Nonce+`\Software\Vendor`) {
			t.Fatalf("native identity = (%v, %q), want mapped HKCU", hive, subkey)
		}
		return registry.Key(123), nil
	}
	registryCloseKeyNative = func(registry.Key) error { return nil }
	registryReadValueDataNative = func(registry.Key, string) (string, string, bool) {
		return "REG_SZ", "registry-value-secret", true
	}
	t.Cleanup(func() {
		registryOpenKeyNative = originalOpen
		registryCloseKeyNative = originalClose
		registryReadValueDataNative = originalRead
	})

	results, runErr := RunVerifyWithValidation([]manifest.VerifyEntry{{
		Type: "registry-value-equals", Path: `HKCU\Software\Vendor`,
		ValueName: "Setting", ValueType: "REG_SZ", Data: "registry-value-secret",
	}}, context)
	if runErr != nil || len(results) != 1 || !results[0].Pass || openCalls != 1 {
		t.Fatalf("RunVerifyWithValidation() = (%+v, %v), openCalls=%d", results, runErr, openCalls)
	}
	if results[0].Path != `HKCU\Software\Vendor` || !strings.Contains(results[0].Message, `HKCU\Software\Vendor\Setting`) {
		t.Fatalf("result lost semantic registry identity: %+v", results[0])
	}
	serialized, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{context.Root(), context.Descriptor().Nonce, "registry-value-secret"} {
		if strings.Contains(strings.ToLower(string(serialized)), strings.ToLower(forbidden)) {
			t.Fatalf("registry result leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestValidationRegistryVerifierRejectsHKLMBeforeNativeOpen(t *testing.T) {
	context := verifierValidationContext(t)
	originalOpen := registryOpenKeyNative
	registryOpenKeyNative = func(registry.Key, string, uint32) (registry.Key, error) {
		panic("native registry open reached")
	}
	t.Cleanup(func() { registryOpenKeyNative = originalOpen })

	results, runErr := RunVerifyWithValidation([]manifest.VerifyEntry{{
		Type: "registry-key-exists", Path: `HKLM\Software\Vendor`,
	}}, context)
	if !errors.Is(runErr, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("error = %v, want ErrUnsafeRegistry", runErr)
	}
	if len(results) != 1 || results[0].Pass || results[0].Path != `HKLM\Software\Vendor` {
		t.Fatalf("safe failure result = %+v", results)
	}
}

func TestValidationRegistryVerifierOrdinaryMissingIsAssertionNotIsolation(t *testing.T) {
	context := verifierValidationContext(t)
	originalOpen := registryOpenKeyNative
	registryOpenKeyNative = func(registry.Key, string, uint32) (registry.Key, error) {
		return 0, errors.New("native key not found")
	}
	t.Cleanup(func() { registryOpenKeyNative = originalOpen })

	results, runErr := RunVerifyWithValidation([]manifest.VerifyEntry{{
		Type: "registry-key-exists", Path: `HKCU\Software\Missing`,
	}}, context)
	if runErr != nil {
		t.Fatalf("ordinary missing registry key returned isolation error: %v", runErr)
	}
	if len(results) != 1 || results[0].Pass || !strings.Contains(strings.ToLower(results[0].Message), "not found") {
		t.Fatalf("ordinary missing result = %+v", results)
	}
}

func TestValidationRegistryKeyValueExistenceUsesNativeSeamAndSemanticResult(t *testing.T) {
	context := verifierValidationContext(t)
	originalOpen := registryOpenKeyNative
	originalClose := registryCloseKeyNative
	originalExists := registryValueExistsNative
	registryOpenKeyNative = func(registry.Key, string, uint32) (registry.Key, error) { return registry.Key(123), nil }
	registryCloseKeyNative = func(registry.Key) error { return nil }
	registryValueExistsNative = func(registry.Key, string) bool { return true }
	t.Cleanup(func() {
		registryOpenKeyNative = originalOpen
		registryCloseKeyNative = originalClose
		registryValueExistsNative = originalExists
	})

	results, runErr := RunVerifyWithValidation([]manifest.VerifyEntry{{
		Type: "registry-key-exists", Path: `HKCU\Software\Vendor`, ValueName: "Setting",
	}}, context)
	if runErr != nil || len(results) != 1 || !results[0].Pass || results[0].Path != `HKCU\Software\Vendor` {
		t.Fatalf("value existence result = %+v err=%v", results, runErr)
	}
}
